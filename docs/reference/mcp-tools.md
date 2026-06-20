# MCP Tools

AmanMCP exposes structured tools through the MCP Go SDK. These SDK-registered
tools are the canonical public contract for agent integrations.

| Tool | Status | Output contract |
|------|--------|-----------------|
| `search` | Canonical | Structured `SearchOutput` with `results`, `search_quality`, optional `search_explain`, and profile mismatch details |
| `search_code` | Canonical | Structured `SearchOutput` filtered for code results by default |
| `search_docs` | Canonical | Structured `SearchOutput` filtered for documentation and project-memory results by default |
| `index_status` | Canonical | Structured index health and embedding status output |
| `graph.query` | Canonical, graph-data dependent | Structured graph query output with status, warnings, relationship evidence, and explicit stale-edge opt-in |

SDK-registered tools are not deprecated and must not carry deprecation metadata.

`graph.query` excludes stale graph edges by default so deleted or replaced
source relationships do not appear current. Set `include_stale: true` only when
debugging graph maintenance; stale results include `stale: true` in the result
payload.

Graph freshness uses the named default `graph.DefaultStaleAfter` of 24 hours
when no caller-specific value is supplied. Serve-mode graph maintenance purges
stale edges older than the named default `graph.DefaultStalePurgeAfter` of 7
days during startup reconciliation and refresh.

## MCP Resources

| Resource URI | Status | Output contract |
|--------------|--------|-----------------|
| `amanmcp://graph_status` | Canonical, read-only | Compact JSON `StatusSnapshot` with availability, schema version, status/freshness, last full and incremental timestamps, canonical node counts, active edge counts, stale edge counts, confidence counts, extractor summaries, and release-gate warnings |

Agents should inspect `amanmcp://graph_status` before relying on
`graph.query`. A healthy populated graph reports `available: true`, a
non-empty canonical node/edge distribution, and no partial/stale/schema
warnings. `last_full_build` identifies the last complete graph rebuild, while
`last_incremental_update` identifies the last watcher/coordinator update. The
resource is observational only; it never mutates, repairs, or dumps node-level
file content.

## Deprecated Compatibility Wrapper

The in-process `Server.CallTool` wrapper predates the SDK structured-output
path. It still routes calls to the same conceptual tools, but search-family
calls return markdown strings for compatibility with older callers.

`Server.CallTool` is now a deprecated compatibility wrapper:

- `deprecated: true`
- `deprecation_notice`: use the structured SDK registered tool instead
- `removal_target`: `post-v1.0.0`
- `replacement`: `SDK registered tool <tool_name>`

This deprecation applies only to the markdown wrapper surface. It does not
deprecate `search`, `search_code`, `search_docs`, `index_status`, or
`graph.query` as SDK-registered tools.

## Migration

Callers that still use `Server.CallTool(ctx, name, args)` should move to the MCP
SDK session/tool path and consume structured outputs. Search responses should be
read from the typed `SearchOutput` shape rather than parsing markdown text.

Use `results[].language_support_tier`, `search_quality`, source metadata, and
optional `search_explain` directly from structured output. Do not infer these
fields from markdown formatting.
