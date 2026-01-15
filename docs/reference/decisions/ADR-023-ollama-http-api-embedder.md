# ADR-023: Ollama HTTP API Embedder Re-introduction

**Status:** Implemented
**Date:** 2026-01-04
**Deciders:** AmanMCP Team
**Amends:** ADR-016 (Ollama now available as optional provider via HTTP API)

---

## Context

### Problem Statement

ADR-016 removed Ollama support to simplify the architecture and achieve "It Just Works" with Hugot/EmbeddingGemma. However, this created limitations:

1. **Quality ceiling** - Static embedder (768 dims) provides basic semantic search but cannot match neural embeddings for:
   - Complex code understanding
   - Documentation search (70+ articles in OptimaFusion)
   - Cross-referencing between code and docs

2. **llama.cpp Metal bug (BUG-021)** - Both gollama.cpp and yzma use llama.cpp bindings which crash on Apple Silicon Metal due to [llama.cpp#18568](https://github.com/ggml-org/llama.cpp/issues/18568)

3. **Users with Ollama** - Many developers already have Ollama installed and running, providing a high-quality embedding source that we're ignoring

### Research Findings

Extensive research on embedding models for hybrid code+documentation search:

| Model | MTEB-Code Score | Context | Languages | Verdict |
|-------|----------------|---------|-----------|---------|
| qwen3-embedding:8b | **80.68** | 8K | 100+ incl. Go | **Best for hybrid** |
| nomic-embed-code | 77.2 | 8K | Code-focused | Good for code |
| nomic-embed-text-v2-moe | ~76 | 8K | General | Documentation |
| EmbeddingGemma | 68.14 | 2K | Limited | Too short context |

**Key Insight:** qwen3-embedding:8b excels at BOTH code AND natural language, critical for projects like OptimaFusion with extensive documentation.

### Why HTTP API Instead of Embedded?

| Aspect | Embedded (gollama.cpp/yzma) | HTTP API (Ollama) |
|--------|----------------------------|-------------------|
| Metal GPU Support | **Broken** (BUG-021) | **Works** (Ollama handles) |
| Binary Size | +50MB CGO deps | 0 (HTTP client only) |
| Model Management | Complex | Ollama handles |
| User Control | Limited | Full (model choice, GPU, etc.) |
| Maintenance | High (llama.cpp updates) | Low (Ollama maintains) |

---

## Decision

**We will re-introduce Ollama as an optional embedding provider via HTTP API**, while maintaining the graceful fallback chain.

### Provider Hierarchy

```
1. ProviderOllama  (opt-in via AMANMCP_EMBEDDER=ollama)
   └── qwen3-embedding:8b (default, auto-fallback to nomic models)
   └── HTTP API to Ollama server
   └── Falls back to Static768 on failure

2. ProviderYzma (opt-in, CPU-only due to BUG-021)
   └── nomic-embed-text-v1.5
   └── Falls back to Static768 on failure

3. ProviderStatic (default, always works)
   └── StaticEmbedder768 (hash-based, 768 dims)
```

### Key Principles

1. **Opt-in only** - Static remains default for zero-config
2. **Graceful degradation** - Always falls back to dimension-compatible static
3. **No CGO for Ollama** - Pure HTTP client, no binary bloat
4. **User's Ollama** - Leverage existing installation, user manages models

---

## Rationale

### Why Re-introduce Now?

1. **BUG-021 blocks neural embeddings** - llama.cpp Metal bug means embedded approaches don't work
2. **HTTP API bypasses the bug** - Ollama's own Metal support works fine
3. **Minimal implementation** - ~500 lines (vs ~1,150 removed in ADR-016)
4. **Clear value proposition** - Users with Ollama get 80+ MTEB-Code quality

### Alternatives Considered

| Option | Pros | Cons |
|--------|------|------|
| Wait for llama.cpp fix | No new code | Indefinite wait, Metal bug complex |
| Remote API (OpenAI/Voyage) | High quality | Cost, privacy, network dependency |
| **Ollama HTTP API** | User's hardware, free, high quality, local | Requires Ollama installation |
| Keep static only | Zero dependencies | Poor semantic search quality |

### Comparison with ADR-016 Removal

| Factor | ADR-016 (Removed) | ADR-023 (Re-introduced) |
|--------|-------------------|------------------------|
| Code Volume | ~1,150 lines | ~500 lines (simpler) |
| CGO Dependencies | None | None |
| Default Provider | Yes | No (opt-in) |
| Model Management | Auto-download | User via Ollama |
| Setup Complexity | Low | Low (env var) |

---

## Consequences

### Positive

1. **Best-in-class embeddings** - qwen3-embedding 80+ MTEB-Code
2. **Code + Docs search** - True semantic understanding for hybrid projects
3. **GPU acceleration** - Ollama handles Metal/CUDA correctly
4. **User choice** - Can select model based on needs
5. **No CGO** - Pure Go HTTP client, cross-compiles easily

### Negative

1. **External dependency** - Requires Ollama running
2. **Larger models** - qwen3:8b is ~4.7GB
3. **Documentation** - Must explain opt-in process

### Mitigations

1. **Clear documentation** - Setup instructions in README
2. **Graceful fallback** - Static768 always works
3. **Environment variable** - Simple opt-in (`AMANMCP_EMBEDDER=ollama`)

---

## Implementation

### Files Created

| File | Lines | Purpose |
|------|-------|---------|
| `internal/embed/ollama.go` | ~300 | OllamaEmbedder implementation |
| `internal/embed/ollama_types.go` | ~98 | Config, API types, constants |
| `internal/embed/ollama_test.go` | ~500 | Unit tests with mock HTTP server |

### Files Modified

| File | Change |
|------|--------|
| `internal/embed/factory.go` | Add ProviderOllama, newOllamaWithFallback() |
| `internal/config/config.go` | Add OllamaHost field, AMANMCP_OLLAMA_HOST env |
| `docs/changelog/unreleased.md` | Document new feature |

### OllamaEmbedder Features

| Feature | Implementation |
|---------|---------------|
| HTTP Connection Pooling | `http.Transport` with 4 connections |
| Dimension Auto-detection | First embedding call detects dims |
| Model Fallback | qwen3 → nomic-text → nomic-code |
| Retry Logic | Exponential backoff (100ms × 2^n) |
| Native Batching | Ollama /api/embed array input |
| Context Cancellation | Full context.Context support |

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `AMANMCP_EMBEDDER` | `static` | Set to `ollama` to enable |
| `AMANMCP_OLLAMA_HOST` | `http://localhost:11434` | Ollama API endpoint |
| `AMANMCP_EMBEDDINGS_MODEL` | `qwen3-embedding:8b` | Model to use |

---

## User Guide

### Quick Start

```bash
# 1. Install Ollama (if not already)
brew install ollama

# 2. Pull the recommended model
ollama pull qwen3-embedding:8b

# 3. Run Ollama server
ollama serve &

# 4. Use with amanmcp
AMANMCP_EMBEDDER=ollama amanmcp index .
```

### Custom Host

```bash
# Remote Ollama server
AMANMCP_OLLAMA_HOST=http://192.168.1.100:11434 AMANMCP_EMBEDDER=ollama amanmcp index .
```

### Fallback Behavior

If Ollama is unavailable, amanmcp automatically falls back to StaticEmbedder768:

```
[INFO] OllamaEmbedder unavailable: connection refused, using static fallback
[INFO] Using StaticEmbedder768 (hash-based, 768 dims)
```

---

## Validation Criteria

This ADR is successfully implemented when:

- [x] `AMANMCP_EMBEDDER=ollama` selects OllamaEmbedder
- [x] OllamaEmbedder auto-detects dimensions from model
- [x] Fallback to Static768 works when Ollama unavailable
- [x] All unit tests pass with mock HTTP server
- [x] `make ci-check` passes
- [x] ADR-023 created and indexed
- [x] F30 feature spec created

---

## References

- [ADR-016 - Ollama Removal](./ADR-016-ollama-removal-embeddinggemma-default.md)
- [BUG-021 - llama.cpp Metal Crash](../bugs/BUG-021-llamacpp-metal-crash.md)
- [llama.cpp#18568](https://github.com/ggml-org/llama.cpp/issues/18568)
- [qwen3-embedding Model](https://ollama.com/library/qwen3-embedding)
- [Ollama API Documentation](https://github.com/ollama/ollama/blob/main/docs/api.md)
- [F30 - Ollama HTTP Embedder](../specs/features/F30-ollama-http-embedder.md)

---

## Changelog

| Date | Change |
|------|--------|
| 2026-01-04 | Initial implementation |
