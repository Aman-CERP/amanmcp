# ADR-014: MCP Capability Signaling for Embedder State

**Status:** Implemented
**Date:** 2025-12-30
**Supersedes:** None
**Superseded by:** None
**Deciders:** Development Team
**Category:** Infrastructure & Tooling

---

## Context

AmanMCP uses Ollama with nomic-embed-text-v2-moe for high-quality semantic embeddings (768 dimensions). When Ollama is unavailable, it falls back to a static hash-based embedder (256 dimensions) with reduced semantic quality.

The problem: AI clients using MCP had no way to know whether Ollama was available or if the static fallback was active. The `index_status` tool returned config values (what was configured) rather than runtime state (what was actually being used).

This meant AI clients couldn't adjust their search strategies based on embedder capability:
- With Ollama: Use natural language queries, trust semantic results
- With Static: Use specific keywords, rely more on BM25

## Decision

Extend the `index_status` MCP tool to expose runtime embedder capability through the `EmbeddingInfo` struct:

```go
type EmbeddingInfo struct {
    // Config values
    Provider string `json:"provider"`
    Model    string `json:"model"`
    Status   string `json:"status"`

    // Runtime state (NEW)
    ActualProvider   string `json:"actual_provider"`    // "ollama" or "static"
    ActualModel      string `json:"actual_model"`       // e.g., "nomic-embed-text-v2-moe" or "static"
    Dimensions       int    `json:"dimensions"`         // 768 or 256
    IsFallbackActive bool   `json:"is_fallback_active"` // true if using static
    SemanticQuality  string `json:"semantic_quality"`   // "high" or "low"
}
```

Pass the embedder instance to the MCP server so it can call `embedder.Dimensions()`, `embedder.ModelName()`, and `embedder.Available()` at runtime.

## Rationale

### Why expose runtime state?

1. **Search Strategy Optimization**: AI clients can adjust query formulation based on capability
2. **User Transparency**: Users understand why search quality may vary
3. **Debugging**: Easy to diagnose embedding-related issues
4. **Future Extensibility**: Foundation for dynamic weight adjustment

### Why these specific fields?

| Field | Purpose |
|-------|---------|
| `actual_provider` | Distinguish between configured vs actual provider |
| `dimensions` | Critical for understanding embedding quality (768 vs 256) |
| `is_fallback_active` | Boolean flag for simple conditional logic |
| `semantic_quality` | Human-readable quality indicator ("high"/"low"/"none") |

### Alternatives Considered

1. **Separate capability endpoint**: Rejected - unnecessary complexity
2. **Include in search results**: Rejected - adds overhead to every search
3. **Webhook on state change**: Rejected - MCP doesn't support push notifications

## Consequences

### Positive

- AI clients can optimize search strategies based on embedder capability
- Transparent about search quality degradation
- No breaking changes (additive to existing API)
- Enables future dynamic weight adjustment

### Negative

- Slightly larger `index_status` response
- Server must track embedder reference (minor memory overhead)

### Neutral

- Requires AI client code changes to use new fields

## Implementation

### Files Modified

| File | Change |
|------|--------|
| `internal/mcp/tools.go` | Extended `EmbeddingInfo` struct |
| `internal/mcp/server.go` | Added embedder field, updated `handleIndexStatusTool` |
| `cmd/amanmcp/cmd/serve.go` | Pass embedder to MCP server |
| `internal/mcp/tools_test.go` | Added capability signaling tests |

### Example Response

**With Ollama:**
```json
{
  "embeddings": {
    "provider": "ollama",
    "model": "nomic-embed-text:v2",
    "status": "ready",
    "actual_provider": "ollama",
    "actual_model": "nomic-embed-text-v2-moe",
    "dimensions": 768,
    "is_fallback_active": false,
    "semantic_quality": "high"
  }
}
```

**With Static Fallback:**
```json
{
  "embeddings": {
    "provider": "ollama",
    "model": "nomic-embed-text:v2",
    "status": "ready",
    "actual_provider": "static",
    "actual_model": "static",
    "dimensions": 256,
    "is_fallback_active": true,
    "semantic_quality": "low"
  }
}
```

## AI Client Strategy Guidance

```
if embeddings.is_fallback_active:
    # Static embeddings - semantic search unreliable
    # Strategy: Use specific keywords, exact function names
    # Trust: BM25 results more than semantic
else:
    # Ollama available - semantic search reliable
    # Strategy: Use natural language, conceptual queries
    # Trust: Default balance (BM25: 0.35, Semantic: 0.65)
```

## References

- [F30 Ollama HTTP Embedder](../specs/features/F30-ollama-http-embedder.md) - Current Ollama integration
- [F17 MCP Tools Spec](../specs/features/F17-mcp-tools.md)
- [BUG-001](../bugs/BUG-001-dimension-mismatch-serve.md) - Related dimension handling bug

---

## Changelog

| Date | Change |
|------|--------|
| 2025-12-30 | Initial implementation |
