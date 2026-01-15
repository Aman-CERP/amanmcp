# ADR-035: MLX as Default Embedder on Apple Silicon

**Status:** Superseded by ADR-037
**Date:** 2026-01-08
**Supersedes:** ADR-023 (Ollama HTTP API Embedder) for Apple Silicon users
**Superseded By:** ADR-037 (Ollama as Default on All Platforms) - MLX RAM usage too high

## Context

Embedding generation consumes 80% of indexing time (~48 minutes for 6,500 chunks with Ollama). This makes iteration slow and degrades developer experience, especially when tuning search quality.

### Investigation Summary

| Backend | Qwen3-8B Status | Speed vs Ollama | Notes |
|---------|-----------------|-----------------|-------|
| **MLX** | Works perfectly | **55x faster** | Native Apple Silicon, 60ms vs 3300ms |
| TEI | Crashes on warm-up | N/A | Metal support incomplete for Qwen3 |
| Ollama | Works | Baseline | Sequential processing bottleneck |

TEI (Text Embeddings Inference) was evaluated first due to proven 5x speedup in benchmarks. However, TEI crashes with all Qwen3 models on Apple Silicon Metal (see `archive/research/embedding_optimization_analysis.md` for details).

MLX-based embedding server (`qwen3-embeddings-mlx`) works perfectly and provides dramatic speedup.

## Decision

1. **Bundle MLX server** in the repository at `mlx-server/`
2. **Make MLX the default** embedder on Apple Silicon (arm64 + darwin) when the server is available
3. **Change default port** from 8000 to 9659 to avoid conflicts with common dev servers
4. **Store models** in `~/.amanmcp/models/mlx/` for consistency with other AmanMCP data

### Detection Logic

```go
func SelectEmbedder(cfg Config) (Embedder, error) {
    // 1. Check if running on Apple Silicon
    if runtime.GOARCH == "arm64" && runtime.GOOS == "darwin" {
        // 2. Check if MLX endpoint is healthy
        if isEndpointHealthy(cfg.MLX.Endpoint) {
            return NewMLXEmbedder(cfg.MLX)
        }
    }
    // 3. Fallback to Ollama
    return NewOllamaEmbedder(cfg.Ollama)
}
```

### Fallback Chain

MLX → Ollama → Static768

## Consequences

### Positive

- **Index time**: 48 min → 3 min (16x faster)
- **Same embedding quality**: Uses same Qwen3-8B model
- **Enables rapid iteration**: Can reindex quickly when tuning search quality
- **Self-contained**: MLX server bundled, no external repo clone needed

### Negative

- **Requires external process**: MLX server must be running separately
- **Python dependency**: Users need Python 3.9+ to run the MLX server
- **Additional setup**: Users must install and start the MLX server

### Mitigation

- Auto-fallback to Ollama when MLX unavailable (seamless degradation)
- Documentation for MLX setup including LaunchAgent for auto-start
- `amanmcp doctor` checks MLX health and provides guidance
- Models stored in standard location (`~/.amanmcp/models/mlx/`)

## Implementation

### Files Added

| File | Purpose |
|------|---------|
| `mlx-server/server.py` | Modified MLX embedding server (port 9659) |
| `mlx-server/requirements.txt` | Python dependencies |
| `mlx-server/README.md` | Setup instructions |
| `mlx-server/LICENSE` | MIT license attribution |

### Files Modified

| File | Change |
|------|--------|
| `internal/embed/mlx.go` | Default port → 9659 |
| `internal/config/config.go` | Default port → 9659 |
| `README.md` | MLX in tech stack, quick start |
| `docs/guides/first-time-user-guide.md` | MLX setup option |
| `docs/reference/configuration.md` | MLX config section |

### Configuration

```yaml
# config.yaml
embeddings:
  provider: mlx
  mlx_endpoint: http://localhost:9659
  mlx_model: large  # small (1024d), medium (2560d), large (4096d)
```

```bash
# Environment variables
AMANMCP_EMBEDDER=mlx
AMANMCP_MLX_ENDPOINT=http://localhost:9659
AMANMCP_MLX_MODEL=large
AMANMCP_MLX_MODELS_DIR=~/.amanmcp/models/mlx
```

## Performance Benchmarks

| Model | Backend | Batch (32 texts) | Dimensions |
|-------|---------|------------------|------------|
| Qwen3-8B-4bit | **MLX** | **~60ms** | 4096 |
| Qwen3-8B (Q4_K_M) | Ollama | ~3300ms | 4096 |

**Result**: 55x speedup with identical embedding quality.

## References

- `archive/research/embedding_optimization_analysis.md` - Full research and benchmarks
- `archive/changelog/v0.1.63.md` - Initial MLX implementation release
- `mlx-server/README.md` - MLX server setup instructions
- [jakedahn/qwen3-embeddings-mlx](https://github.com/jakedahn/qwen3-embeddings-mlx) - Original MLX server

## Review

- [x] ADR follows template
- [x] Decision is reversible (fallback to Ollama always works)
- [x] Implementation complete
- [x] Documentation updated
