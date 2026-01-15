# ADR-016: Ollama Removal and EmbeddingGemma as Default

**Status:** Superseded
**Date:** 2025-12-31
**Deciders:** AmanMCP Team
**Supersedes:** ADR-005 implementation notes (Ollama kept as fallback)
**Superseded By:** ADR-023 (Ollama HTTP API re-introduced as default)

---

## Context

Following ADR-005's implementation of Hugot as the default embedder, we identified that:

1. **Ollama code creates architectural debt** - ~1,150 lines of code for a rarely-used optional feature
2. **EmbeddingGemma is superior to MiniLM** - 4x larger context window (2048 vs 512 tokens), explicit code search training
3. **"It Just Works" philosophy violations** - Ollama still appears in documentation, help text, and preflight checks

### Analysis Documents

- `EmbeddingGemma_analysis.md` - Detailed comparison of embedding models
- `model_pivot.md` - Decision to pivot from MiniLM to EmbeddingGemma

### Current State (Post ADR-005)

| Aspect | Current | Desired |
|--------|---------|---------|
| Default Provider | Hugot | Hugot |
| Default Model | **MiniLM** (384 dims, 512 ctx) | **EmbeddingGemma** (768 dims, 2048 ctx) |
| Ollama Code | ~1,150 lines present | **Removed** |
| Documentation | References Ollama | Updated |

---

## Decision

1. **Remove all Ollama code** from the codebase
2. **Change default model to EmbeddingGemma** (onnx-community/embeddinggemma-300m-ONNX)
3. **Simplify embedder hierarchy** to: Hugot → Static (no Ollama)

---

## Rationale

### Why Remove Ollama?

| Factor | Impact |
|--------|--------|
| Code complexity | -1,150 lines deleted |
| Architecture clarity | 1 provider (Hugot) vs 3 |
| "It Just Works" | True zero-config now |
| Maintenance burden | No external service dependency |
| Documentation | No confusing Ollama references |

### Why EmbeddingGemma over MiniLM?

| Factor | MiniLM | EmbeddingGemma |
|--------|--------|----------------|
| Context Window | 512 tokens | **2048 tokens (4x)** |
| Dimensions | 384 | **768** |
| Code Search Training | Generic | **Explicit code search** |
| Model Size | ~22MB | ~300MB |
| MTEB Code Score | N/A | **68.14%** |

The 4x larger context window is critical for code search where functions often exceed 512 tokens.

---

## Consequences

### Positive

1. **Simpler Architecture** - One embedding provider path
2. **Better Code Search** - EmbeddingGemma trained for code
3. **Larger Context** - 2048 tokens captures full functions
4. **Clean Documentation** - No Ollama confusion
5. **Reduced Binary** - No Ollama HTTP client code

### Negative

1. **Breaking Change** - `--embedder=ollama` no longer works
2. **Larger Download** - ~300MB vs ~22MB for first run
3. **Higher Memory** - ~200MB vs ~50MB runtime

### Mitigations

1. **Clear Error Message** - If user specifies `--embedder=ollama`, show helpful migration message
2. **MiniLM Still Available** - `--model=minilm` for lower resource usage
3. **Static Fallback** - Always available for offline/minimal use

---

## Implementation

### Files Deleted

| File | Lines |
|------|-------|
| `internal/embed/ollama.go` | 375 |
| `internal/embed/ollama_test.go` | ~250 |
| `internal/setup/ollama.go` | ~427 |
| `internal/setup/ollama_test.go` | ~80 |
| **Total** | **~1,150** |

### Files Modified

| File | Change |
|------|--------|
| `internal/config/config.go` | Default model: "embeddinggemma", dimensions: 768 |
| `internal/embed/types.go` | Removed Ollama types/constants |
| `internal/embed/factory.go` | Removed ProviderOllama, updated ValidProviders |
| `internal/embed/hugot.go` | Updated default constants |
| `cmd/amanmcp/cmd/root.go` | Removed Ollama check |
| `cmd/amanmcp/cmd/setup.go` | Repurposed for Hugot model setup |
| `cmd/amanmcp/cmd/doctor.go` | Removed Ollama check description |
| `cmd/amanmcp/cmd/status.go` | Removed Ollama status check |
| `internal/preflight/check.go` | Removed CheckOllama function |

### New Embedder Hierarchy

```
1. User Override (--embedder flag or AMANMCP_EMBEDDER env)
   └── "hugot"  → HugotEmbedder (768 dims, EmbeddingGemma)
   └── "static" → StaticEmbedder (256 dims, offline)

2. Default: Hugot + EmbeddingGemma
   └── Try HugotEmbedder (768 dims, auto-download ~300MB)
   └── If fails → Fall back to Static

3. Fallback: Static
   └── Always works (256 dims, hash-based)
   └── Log warning about degraded quality
```

### Configuration Changes

| Setting | Before (ADR-005) | After (ADR-016) |
|---------|------------------|-----------------|
| Default Model | `minilm` | `embeddinggemma` |
| Default Dimensions | 384 | 768 |
| Ollama Provider | Available | Removed |
| Valid Providers | hugot, ollama, static | hugot, static |

---

## Migration Guide

### For Users with --embedder=ollama

If you were using `--embedder=ollama`:

```bash
# Old (no longer works)
amanmcp index . --embedder=ollama

# New equivalent (Hugot with larger model)
amanmcp index .
# or explicitly:
amanmcp index . --embedder=hugot --model=embeddinggemma
```

### For Lower Resource Usage

```bash
# Use MiniLM for smaller footprint
amanmcp setup --model=minilm
amanmcp index .
```

---

## Validation Criteria

This ADR is successfully implemented when:

- [x] `grep -r "ollama" internal/ cmd/` returns no Go code
- [x] `make test` passes
- [x] `make ci-check` passes
- [x] Fresh install auto-downloads EmbeddingGemma (~300MB)
- [x] `amanmcp status` shows embedder type "hugot", model "embeddinggemma"
- [x] ADR-016 created and indexed

---

## References

- [ADR-005 - Hugot as Default Embedding Provider](./ADR-005-hugot-embedder.md)
- [EmbeddingGemma ONNX Model](https://huggingface.co/onnx-community/embeddinggemma-300m-ONNX)
- [F25a - Hugot Embedder](../specs/features/F25a-hugot-embedder.md)
- [EmbeddingGemma Analysis](../../EmbeddingGemma_analysis.md)

---

## Changelog

| Date | Change |
|------|--------|
| 2025-12-31 | Initial acceptance and implementation |
