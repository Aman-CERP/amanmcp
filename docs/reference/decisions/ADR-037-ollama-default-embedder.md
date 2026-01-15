# ADR-037: Ollama as Default Embedder on All Platforms

**Status:** Accepted
**Date:** 2026-01-14
**Supersedes:** ADR-035 (MLX as Default on Apple Silicon)

## Context

ADR-035 made MLX the default embedder on Apple Silicon due to its ~1.7x speed advantage. However, real-world usage revealed a significant drawback: **MLX consumes substantially more RAM** than Ollama.

### Issue Discovered

During development sessions with Claude Code:
- MLX server + Ollama = combined RAM pressure
- System becomes sluggish during sustained usage
- Users with 24GB RAM hit limits during indexing + search
- The speed advantage doesn't justify the RAM cost for most use cases

### Usage Pattern Analysis

| Use Case | Typical Frequency | MLX Benefit |
|----------|-------------------|-------------|
| Initial indexing | Once per project | Significant (16x faster) |
| Incremental reindex | Rare (file watcher) | Minimal |
| Search queries | Very frequent | None (same latency) |
| Development sessions | Hours/day | RAM overhead is net negative |

For typical development workflows, **search latency matters more than indexing speed**, and both MLX and Ollama have similar search latency.

## Decision

1. **Make Ollama the default** embedder on ALL platforms (including Apple Silicon)
2. **Keep MLX as opt-in** via `AMANMCP_EMBEDDER=mlx` or config file
3. **Document MLX as the "performance" option** for users who prioritize speed over RAM
4. **Recommend running only ONE backend** at a time (not both simultaneously)

### Configuration

```yaml
# amanmcp.yaml - Ollama (default, recommended)
embeddings:
  provider: ollama

# amanmcp.yaml - MLX (opt-in for speed)
embeddings:
  provider: mlx
```

```bash
# Environment override
AMANMCP_EMBEDDER=ollama  # default
AMANMCP_EMBEDDER=mlx     # opt-in for speed
```

## Consequences

### Positive

- **Lower RAM usage**: Ollama has smaller memory footprint
- **Simpler setup**: No need to start MLX server
- **Cross-platform consistency**: Same default on all platforms
- **Better for long sessions**: Sustainable RAM usage during development

### Negative

- **Slower initial indexing**: 16x slower than MLX for full reindex
- **Users expecting MLX default** (per ADR-035) may be surprised

### Mitigation

- Document that MLX is available for users who need speed
- Provide clear instructions for switching: `AMANMCP_EMBEDDER=mlx`
- Recommend MLX only for large initial indexing, then switch back

## Implementation

### Files Modified

| File | Change |
|------|--------|
| `internal/embed/factory.go` | `newDefaultWithFallback()` now always uses Ollama |
| `internal/embed/factory.go` | `ParseProvider()` defaults to Ollama |
| `internal/embed/factory.go` | Updated doc comments |
| `docs/reference/decisions/ADR-037-*.md` | This ADR |

### Code Change

```go
// Before (ADR-035)
func newDefaultWithFallback(ctx context.Context, model string) (Embedder, error) {
    if isAppleSilicon() {
        return newMLXWithFallback(ctx, false)  // MLX on Apple Silicon
    }
    return newOllamaWithFallback(ctx, model, false)
}

// After (ADR-037)
func newDefaultWithFallback(ctx context.Context, model string) (Embedder, error) {
    // Always default to Ollama - MLX is opt-in only
    return newOllamaWithFallback(ctx, model, false)
}
```

## Recommendation for Users

| Scenario | Recommended Backend |
|----------|---------------------|
| Day-to-day development | Ollama (default) |
| Large initial indexing (>10k files) | MLX (then switch back) |
| RAM-constrained system (<16GB) | Ollama |
| Speed-critical batch operations | MLX |

## References

- ADR-035: MLX as Default Embedder on Apple Silicon (superseded)
- `internal/embed/factory.go`: Provider selection logic
- User feedback: RAM pressure during development sessions

## Review

- [x] ADR follows template
- [x] Decision is reversible (MLX still available via config)
- [x] Implementation complete
- [x] Documentation updated
