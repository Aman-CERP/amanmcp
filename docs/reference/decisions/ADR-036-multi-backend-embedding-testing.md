# ADR-036: Multi-Backend Embedding Model Testing Framework

**Status:** Accepted
**Date:** 2026-01-11
**Related:** ADR-035 (MLX Default Embedder), ADR-023 (Ollama HTTP API)

---

## Context

### The Problem

Search quality validation at 75% Tier 1 revealed a fundamental issue: the 0.6B embedding model cannot distinguish between test files and implementation files when they contain identical keywords. Cross-encoder reranking (FEAT-RR1) helps but cannot solve this semantic confusion.

### The Constraint

Hardware constraint is real and non-negotiable:

- 24GB RAM laptop
- Larger models (4B, 8B) make the system unusable
- 0.6B parameter ceiling is permanent for this hardware class

### The Temptation (Rejected)

The obvious approach is "use a bigger model." However:

1. Bigger models don't fit in available RAM
2. System becomes unresponsive with larger models
3. Users have confirmed: "System (OS) became useless, I did not have any RAM left"

### The Insight

During research for search quality improvements, we discovered:

1. **EmbeddingGemma 308M** (Google, Sept 2025) - Half the size of Qwen3 0.6B, MTEB leader under 500M params, MLX native support
2. **Ollama still works** - 684 lines of production code, fully functional, just not tested recently
3. **The architecture supports this** - Embedder interface, dimension auto-detection, and `--force` reindex were designed for backend switching

This suggests: **We might get better quality with LESS memory, not more.**

---

## Decision

We will create a **multi-backend embedding model testing framework** that:

1. **Validates 4 tracks** systematically:

   | Track | Backend | Model | Dimensions |
   | ----- | ------- | ----- | ---------- |
   | 1 | Ollama | Qwen3 0.6B | 1024 |
   | 2 | Ollama | EmbeddingGemma | 768 |
   | 3 | MLX | Qwen3 0.6B | 1024 |
   | 4 | MLX | EmbeddingGemma | 768 |

2. **Uses empirical testing** on our own codebase queries (dogfooding), not external benchmarks

3. **Tracks results** in a structured validation matrix

4. **Keeps all options working** - no removal of functional backends

5. **Makes switching trivial**:

   ```bash
   export AMANMCP_EMBEDDER=ollama
   export AMANMCP_OLLAMA_MODEL=qwen3-embedding:0.6b
   amanmcp index --force .
   ```

---

## Rationale

### Why Not Just Pick One Model?

| Approach | Problem |
| -------- | ------- |
| Stick with current | 75% Tier 1 ceiling, known limitations |
| Switch based on benchmarks | MTEB ≠ code search on YOUR codebase |
| Try larger models | Hardware constraint violated |

### Why Empirical Testing?

1. **Benchmarks lie** - MTEB scores are on generic datasets, not code
2. **Your codebase is unique** - Symbol patterns, doc style, test conventions
3. **Tradeoffs vary** - Model X may excel at code, Model Y at docs
4. **Evidence > theory** - Actual Tier 1 pass rates beat speculation

### Why Multiple Backends?

| Backend | Advantage |
| ------- | --------- |
| **MLX** | 55x faster, ideal for rapid iteration |
| **Ollama** | More models available, cross-platform fallback |

Keeping both enables:

- Testing models only available in one backend
- Cross-platform deployment (Ollama works on Linux/Windows)
- Fallback reliability when primary fails

### Alternatives Considered

| Option | Pros | Cons |
| ------ | ---- | ---- |
| Upgrade to 8B model | Better semantics | System unusable |
| BM25-only (drop vectors) | No model dependency | Lose semantic search |
| External API (OpenAI) | Best quality | Privacy, cost, latency |
| **Multi-backend testing** | Find best small model | Testing overhead |

**Chosen: Multi-backend testing** - Addresses root cause (find optimal model) without violating constraints.

---

## Consequences

### Positive

- **Data-driven decisions** - Choose model based on actual performance
- **May find better default** - EmbeddingGemma could beat Qwen3 with half the memory
- **Future-proofed** - New models can be tested in <30 minutes
- **Cross-platform ready** - Ollama works everywhere, MLX is Apple-only
- **No vendor lock-in** - Multiple backends, multiple models

### Negative

- **Testing overhead** - Each model requires full reindex
- **Documentation complexity** - Multiple supported configurations
- **User confusion** - "Which model should I use?"

### Mitigation

- Automation script for 4-track testing
- Clear recommendations based on use case (code/docs/mixed)
- Smart defaults that "just work"

### Neutral

- Dimension handling already exists (auto-detection, validation)
- Reindexing already required when switching models
- Embedder abstraction already in place

---

## Implementation

### Artifacts Created

| Artifact | Purpose |
| -------- | ------- |
| `SPIKE-003.md` | Multi-backend testing framework specification |
| `DEBT-025.md` | Ollama validation and recap |
| `embedder-matrix.md` | Validation results tracking |

### Test Workflow

```bash
# Track 1: Ollama + Qwen3 0.6B
export AMANMCP_EMBEDDER=ollama
export AMANMCP_OLLAMA_MODEL=qwen3-embedding:0.6b
amanmcp index --force .
# Record Tier 1 results

# Track 2: Ollama + EmbeddingGemma
export AMANMCP_OLLAMA_MODEL=embeddinggemma
amanmcp index --force .
# Record Tier 1 results

# Track 3: MLX + Qwen3 0.6B (baseline)
export AMANMCP_EMBEDDER=mlx
amanmcp index --force .
# Record Tier 1 results

# Track 4: MLX + EmbeddingGemma
# Add to mlx-server AVAILABLE_MODELS first
amanmcp index --force .
# Record Tier 1 results
```

### Success Criteria

| Criterion | Threshold | Current |
| --------- | --------- | ------- |
| Tier 1 validation | > 83% | 75% |
| Memory footprint | ≤ current | baseline |
| Query latency p95 | ≤ 200ms | ~100ms |

---

## Philosophy

### Constraint as Feature

The 24GB RAM constraint forces us to:

1. **Think harder** about model efficiency
2. **Test empirically** rather than assume bigger = better
3. **Build infrastructure** that enables rapid experimentation
4. **Focus on the user** who has the same hardware constraints

### Testing Bed Mindset

This framework turns AmanMCP into a **testing bed for embedding models**:

- New model released? Test it in 30 minutes
- Different use case? Find the optimal model
- Hardware changed? Re-run validation matrix

---

## References

- [EmbeddingGemma - Google Developers Blog](https://developers.googleblog.com/en/introducing-embeddinggemma/)
- [Sourcegraph BM25F Implementation](https://sourcegraph.com/blog/keeping-it-boring-and-relevant-with-bm25f)
- [ExCS: Accelerating Code Search](https://www.nature.com/articles/s41598-024-73907-6)
- SPIKE-003: `.aman-pm/backlog/spikes/SPIKE-003.md`
- DEBT-025: `.aman-pm/backlog/debt/active/DEBT-025-ollama-validation.md`
- Validation Matrix: `.aman-pm/validation/embedder-matrix.md`

---

## Changelog

| Date | Change |
| ---- | ------ |
| 2026-01-11 | Initial acceptance |
