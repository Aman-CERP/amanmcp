# Graph-Default Augmentation Policy

> **Status:** Guard kernel implemented; no runtime integration yet.
> **Owner ticket:** TASK-GRA17 (FEAT-GRA2, Sprint 20).
> **Code:** `internal/eval/graph_default_policy.go`.

## Purpose

AmanMCP does **not** currently let search default to graph-augmented retrieval.
`graph.query` is an explicit, opt-in MCP tool. Before any future change makes
graph augmentation a default search path, that change must prove — with fresh,
direct `graph.query` eval evidence — that graph quality has not regressed.

This document defines the policy that gates that decision and the contract a
future graph-default integration must honor. The policy is implemented today as
a **pure decision kernel** (`EvaluateGraphDefaultPolicy`) with no runtime wiring,
no persisted state, and no control over explicit `graph.query`.

## Why this is a guard, not a feature (current-state audit)

There is no default-graph-augmentation switch to disable. Verified at
implementation time:

- `internal/config` exposes only `DefaultGraphEvalModeThresholds` and
  `GraphEvalConfig` (eval floors). There is no `search.graph.default_enabled` or
  equivalent field.
- `internal/search` and `internal/mcp` contain no default-graph-augmentation
  code path; graph results reach clients only through the explicit `graph.query`
  tool.

Because the switch does not exist, this ticket ships a **design note + a pure
policy evaluator + a regression tripwire**, not a runtime gate. The tripwire
(`TestNoUnguardedGraphDefaultEnablePath`) fails if a future change adds a
config field that looks like a default-graph-augmentation enable, forcing the
author back to this document and to `EvaluateGraphDefaultPolicy`.

## Policy inputs

The evaluator consumes the direct graph eval report (`DirectGraphEvalReport`,
the GRA10–GRA16 output) plus options:

| Input | Source | Meaning |
|-------|--------|---------|
| `GraphToolMeasured` | report (GRA11) | The run actually measured `graph.query`, not a search fallback. |
| `Run.GitSHA` | report | The **source version** the evidence was produced against. |
| `ByMode[*]` relevance | report (GRA12/13/14/15) | Per-mode recall@10 / precision@10 / hit-rate, vs configured floors. |
| `Summary.DegradationBlockingRate` | report (GRA16) | Blocking-degradation rate, gated against the threshold. |
| `Queries[*]` contract state | report | Transport errors / disallowed status / missing-warning contract failures. |
| `CurrentSourceVersion` | option | The version the caller wants to enable augmentation for. **Required to reach `allowed`**; empty defers (freshness uncertifiable). |
| `BlockingDegradationThreshold` | option / `config.DefaultEvalGraphBlockingDegradationThreshold` (0.10) | GRA16 ceiling. |
| `Thresholds` | option / `DefaultGraphEvalModeThresholds()` | GRA12/13/14/15 per-mode floors. |
| `RequiredConsecutivePasses` | option | Distinct-version passing-run requirement. |
| `Previous` | option | Prior gate state for consecutive-run accounting. |

## Decision: fail-closed

`EvaluateGraphDefaultPolicy` blocks default augmentation unless **every** gate
passes. It blocks when any of the following hold:

1. No report (`nil`) — no evidence.
2. The report is not honestly measured — `GraphToolMeasured == false`, **or**
   the report is structurally unmeasured (wrong tool/scope, zero selected or zero
   measured cases). Measurement honesty is **re-derived** via
   `directGraphMeasurementReason`, not taken on trust from the precomputed flag,
   so a malformed report cannot claim measurement.
3. `CurrentSourceVersion` is empty — freshness cannot be certified, so the policy
   defers. A default-enable decision requires a known current source version.
4. `CurrentSourceVersion` is set and the report's `Run.GitSHA` differs — the
   evidence is stale for the version being enabled.
5. Any per-mode relevance floor is unmet (GRA12/13/14/15).
6. Any quality-class contract failure is present.
7. `Summary.DegradationBlockingRate` exceeds the blocking-degradation threshold
   (GRA16).
8. The run passed but has not accumulated `RequiredConsecutivePasses` passing
   runs on **distinct** source versions.

Silence is never success: a missing, empty, or malformed report blocks.

## Gate-state schema

`GraphDefaultGateState` is JSON-serialisable and doubles as the schema for any
future persisted state:

| Field | Purpose |
|-------|---------|
| `decision` | `allowed` \| `blocked`. |
| `allow_default_augmentation` | The actionable boolean a future integration reads. |
| `recommendation` | `keep` \| `tune` \| `defer` \| `kill` (severity-ranked). |
| `source_version` | The evidence source version (`Run.GitSHA`). |
| `measured_tool` | The measured tool (`graph.query`). |
| `graph_tool_measured` | GRA11 measurement honesty flag. |
| `report_path` | Path to the eval report the decision is based on. |
| `failing_modes` / `failing_metrics` | Which modes/metrics fell below floor. |
| `blocking_degradation_rate` / `blocking_degradation_threshold` | GRA16 numerator vs ceiling. |
| `degradation_breakdown` | Per-label blocking degradation counts. |
| `passing_source_versions` | The set of distinct source versions that have passed in sequence (reset on any failure). |
| `consecutive_passes` | Length of `passing_source_versions` — the distinct-version pass count. |
| `evaluated_at` | Evaluation timestamp. |
| `reasons` | Human-readable explanation of the decision. |

### Recommendation values

- `keep` — quality holds; default augmentation may be enabled (decision `allowed`).
- `tune` — relevance below target floors; do not default-enable, fix relevance.
- `defer` — evidence stale / unmeasured / insufficient distinct passes; re-run eval.
- `kill` — hard contract failure or blocking degradation over threshold; do not
  default-enable.

`kill` outranks `defer`, which outranks `tune`, which outranks `keep`
(`strongerGraphRecommendation`). The strongest triggered recommendation wins.

## Re-enable semantics

- A **current `source_version` is required** to enable: the decision needs to
  know what "current" is before it can certify freshness.
- The latest direct graph eval must **pass against the current `source_version`**.
  A pass recorded against an older `Run.GitSHA` does not unblock the current one.
- When `RequiredConsecutivePasses > 1`, distinctness is over the **full set** of
  passing source versions (`passing_source_versions`), not just the adjacent prior
  one: a sequence `A → B → A` counts **2** distinct versions, not 3. A repeated
  pass on a version already in the set adds nothing. **Any failing run resets the
  set to empty.**

## Storage ownership (future, not implemented here)

This ticket persists nothing. When a runtime integration adds persisted gate
state, it must store it at a **config-owned path** — `graph.default_policy_state_path`
— and must not hardcode `.amanmcp/graph_gate.json`. Restart behavior must be
covered by tests at that time.

## Explicit `graph.query` is never disabled

The policy governs **default augmentation only**. `GraphDefaultGateState` exposes
no field that disables explicit `graph.query`; even a hard block leaves the
explicit tool fully callable for diagnostics and agent opt-in use. This is
enforced structurally by `TestEvaluateGraphDefaultPolicy_ExplicitGraphQueryUnaffected`.

## Future runtime integration note (import direction)

Package `internal/eval` imports `internal/search` (one-directional; the import
lives in `internal/eval/corpus.go`, not in the policy file). A future
graph-default integration living in `internal/search` therefore **cannot** import
the policy from `internal/eval` without creating an import cycle. The policy file
itself (`graph_default_policy.go`) imports only `fmt`, `strings`, `time`, and
`internal/config`, so moving *that file* to a leaf package is mechanical — the
rest of package `eval` keeps its `search` dependency. The integration could
alternatively pass a plain input projection of the report. The kernel is
deliberately free of I/O and report-construction code to keep either path simple.

## Driving blocked vs. pass states with `make eval-graph-quick`

`make eval-graph-quick` runs `amanmcp eval graph --subset quick --fail-on-regression
--blocking-degradation-threshold <T>` and writes `DirectGraphEvalReport` JSON to
`.aman-pm/validation/graph-eval/latest.json`. Feeding that report to
`EvaluateGraphDefaultPolicy`:

- **Pass → allowed:** a clean run on the current checkout (`GraphToolMeasured=true`,
  all per-mode floors met, blocking degradation `<= T`) with
  `CurrentSourceVersion` set to the run's `Run.GitSHA` yields
  `decision=allowed`, `recommendation=keep`.
- **Blocked:** drop a mode below floor (→ `tune`), raise blocking degradation
  above `T` (→ `kill`), run against a different checkout than `CurrentSourceVersion`
  (→ `defer`/stale), or supply an unmeasured report (→ `defer`). Each yields
  `decision=blocked` with reasons naming the failed gate.

See `internal/eval/graph_default_policy_test.go` for the executable form of each
of these states.
