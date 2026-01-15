# ADR-022: CGO-Minimal Standalone Architecture

**Status:** Implemented
**Date:** 2026-01-03
**Supersedes:** ADR-001 (USearch), ADR-005 (Hugot)
**Related Bug:** BUG-018 (CLI search hangs)

---

## Context

### The Problem

AmanMCP CLI commands hang indefinitely when the binary is installed to `~/.local/bin/`:

```bash
./bin/amanmcp version        # ✓ Works (from build directory)
~/.local/bin/amanmcp version # ✗ Hangs indefinitely (zombie process)
```

This blocks Phase 2 dogfooding and makes the tool unusable for its intended purpose.

### Root Cause Analysis

Investigation revealed the root cause is **CGO dynamic library resolution failures**:

1. **USearch (vector store)** - CGO library with complex linking requirements
2. **Hugot ONNX Runtime** - 86MB shared library for neural embeddings
3. **macOS dyld** - Cannot find libraries when binary is moved from build directory

The binary builds with hardcoded rpath pointing to build directory:
```
<project-dir>/lib
```

When copied to `~/.local/bin/`, dyld can't resolve library paths, causing the process to hang in uninterruptible sleep state (UE).

### Forces at Play

1. **User Experience Goal:** "It Just Works" - zero configuration, single binary install
2. **Technical Constraint:** CGO libraries require careful bundling for distribution
3. **Project Philosophy:** Privacy-first, local-only, no external services
4. **Quality Bar:** Premium engineering - must work flawlessly

---

## Decision

We will replace CGO-heavy dependencies with pure Go or purego alternatives:

| Component | Current | New |
|-----------|---------|-----|
| Vector Store | USearch (CGO) | **coder/hnsw (pure Go)** |
| Embeddings | Hugot (ONNX CGO) | **gollama.cpp (purego)** |
| Model | bge-small-en-v1.5 (ONNX) | **nomic-embed-text-v1.5 (GGUF)** |
| Chunking | tree-sitter (CGO) | tree-sitter (keep, static link) |
| BM25 Index | Bleve (pure Go) | Bleve (keep) |

### New Architecture

```
┌────────────────────────────────────────────────────────────────┐
│                     amanmcp binary (~60MB)                     │
├────────────────────────────────────────────────────────────────┤
│ Chunking     │ tree-sitter (CGO, statically linked)            │
│ BM25 Index   │ Bleve (pure Go) ✓                               │
│ Vector Store │ coder/hnsw (pure Go) ← replaces USearch         │
│ Embeddings   │ gollama.cpp (purego) ← replaces Hugot           │
│ GGUF Model   │ auto-download on first use (~138MB)             │
└────────────────────────────────────────────────────────────────┘
```

---

## Rationale

### Research Conducted

Extensive web research (January 2026) investigated alternatives:

#### 1. gollama.cpp - Embeddings via purego

**Source:** github.com/dianlight/gollama.cpp

**Key Discovery:** Uses purego library to call llama.cpp without CGO. This completely eliminates the CGO/dyld issue.

**How it works:**
- Purego enables calling C functions from Go without CGO compilation
- Auto-downloads pre-built llama.cpp binaries on first use
- Caches libraries in user's home directory
- Platform support: macOS (arm64, x86_64), Linux, Windows

**Embedding Mode:**
```c
llama_set_embeddings(ctx, true)  // Enable embedding extraction
```

**Code Pattern:**
```go
import "github.com/dianlight/gollama.cpp/pkg/llama"

model, _ := llama.NewLlamaModel(modelPath)
ctx := model.NewContext(llama.WithEmbeddings())
embedding := ctx.Embed(text)  // Returns []float32
```

#### 2. Nomic Embed Models (GGUF Format)

**Available Models:**

| Model | Size | Dimensions | Use Case |
|-------|------|------------|----------|
| nomic-embed-text-v1.5-GGUF F16 | ~138 MiB | 768 | Full precision (chosen) |
| nomic-embed-text-v1.5-GGUF Q4_K_M | ~92 MiB | 768 | Quantized alternative |

**Chosen:** nomic-embed-text-v1.5-GGUF F16 for best quality, used for both code and documentation search.

**Quality Notes:**
- Nomic models are well-regarded in the embedding community
- Code-specific model trained on code corpora
- 768 dimensions matches our current Hugot output

#### 3. coder/hnsw - Pure Go Vector Store

**Source:** github.com/coder/hnsw

**Characteristics:**
- Pure Go implementation of HNSW algorithm
- Same algorithm as USearch (Hierarchical Navigable Small World)
- Performance: ~1232 MB/s export speed
- No CGO, no library dependencies

**API:**
```go
import "github.com/coder/hnsw"

graph := hnsw.NewGraph[uint64]()
graph.Add(id, vector)
results := graph.Search(queryVector, k)
```

#### 4. 2024 Search Research Findings

Academic and industry research confirms our hybrid search architecture:

- **Hybrid search (BM25 + semantic) outperforms either alone**
- RRF fusion with k=60 is optimal (we already use this)
- Dense retrieval alone misses keyword matches
- Sparse retrieval alone misses semantic relationships

Our architecture is correct; only the CGO dependencies need replacement.

### Alternatives Considered

| Option | Pros | Cons |
|--------|------|------|
| Fix CGO library bundling | Keep existing code | Complex, fragile, macOS-specific issues |
| Use Ollama for embeddings | Well-tested | Requires separate install, violates "It Just Works" |
| Pure Go regex chunker | No CGO at all | User rejected - limits extensibility |
| Daemon mode (implemented) | Keeps Hugot quality | Adds complexity, still has CGO issues |
| **Chosen: Pure Go + purego** | Standalone binary | Re-index required, new dependencies |

### Why Not Fix CGO Bundling?

CGO library bundling on macOS is notoriously difficult:

1. **rpath resolution** - Requires @executable_path or @loader_path tricks
2. **Code signing** - Breaks Gatekeeper when libraries are bundled
3. **Universal binaries** - Need fat binaries for arm64 + x86_64
4. **Dynamic linker** - Each macOS version behaves differently

These issues have plagued many Go projects (e.g., cgo-based SQLite, image processing libraries).

### Why Not Ollama?

User explicitly rejected Ollama:
> "we did not want ollama - remember? User has to run multiple commands to configure the system. I wanted amanmcp to be standalone drop in install, and forget."

Ollama violates the core "It Just Works" philosophy.

---

## Consequences

### Positive

1. **Standalone binary works everywhere** - No CGO library path issues
2. **Simpler distribution** - Single binary, optional model download
3. **Faster startup** - No ONNX runtime initialization (was 2-3s)
4. **Smaller binary** - Remove ONNX runtime (~86MB saved)
5. **Cross-platform** - purego works on all platforms
6. **Maintainable** - Fewer moving parts, easier debugging

### Negative

1. **Re-index required** - HNSW format differs from USearch
2. **New dependencies** - gollama.cpp and coder/hnsw are less mature
3. **Model download** - First index downloads ~138MB model (one-time)
4. **Slight quality difference** - Different embedding model, should be comparable

### Neutral

1. **Same search quality** - Hybrid search architecture unchanged
2. **Same API** - CLI interface unchanged
3. **Same data location** - Still uses `.amanmcp/` directory

---

## Implementation Notes

### Phase 1: Replace USearch with coder/hnsw

**Files to modify:**
- `internal/store/usearch.go` → Delete
- `internal/store/hnsw.go` → Create (new implementation)
- `internal/store/vector_test.go` → Update
- `go.mod` → Remove usearch, add coder/hnsw

**Interface stays the same:**
```go
type VectorStore interface {
    Add(id uint64, vector []float32) error
    Search(vector []float32, k int) ([]VectorResult, error)
    Save(path string) error
    Load(path string) error
    Close() error
}
```

### Phase 2: Replace Hugot with gollama.cpp

**Files to modify:**
- `internal/embed/hugot.go` → Delete
- `internal/embed/llama.go` → Create (new implementation)
- `internal/embed/model.go` → Create (model download/cache)
- `internal/embed/embed.go` → Update factory
- `go.mod` → Remove hugot, add gollama.cpp

**Embedder interface unchanged:**
```go
type Embedder interface {
    Embed(ctx context.Context, text string) ([]float32, error)
    Dimensions() int
    Close() error
}
```

### Phase 3: Model Management

**Auto-download strategy:**
```go
// On first index:
// 1. Check ~/.amanmcp/models/nomic-embed-code.gguf exists
// 2. If not, download from HuggingFace
// 3. Show progress: "Downloading embedding model (48 MiB)..."
// 4. Cache forever (model is immutable)
```

### Phase 4: Static Tree-sitter Linking

Tree-sitter CGO is simpler than USearch - just needs static linking flags:
```makefile
build:
    CGO_ENABLED=1 go build -ldflags '-extldflags "-static"' ...
```

### Migration Strategy

```go
func (i *Indexer) Index(root string) error {
    // Check for old USearch format
    if isOldFormat(dataDir) {
        log.Info("Detected old index format, re-indexing...")
        os.RemoveAll(filepath.Join(dataDir, "vectors.usearch"))
    }
    // Continue with new HNSW format
}
```

---

## Related

- [ADR-001](./ADR-001-vector-database-usearch.md) - Original USearch decision (superseded)
- [ADR-005](./ADR-005-hugot-embedder.md) - Original Hugot decision (superseded)
- [ADR-013](./ADR-013-cgo-environment-setup.md) - CGO environment (partially superseded)
- [BUG-018](../bugs/BUG-018-cli-search-hangs.md) - The bug this fixes
- [gollama.cpp](https://github.com/dianlight/gollama.cpp) - purego llama.cpp bindings
- [coder/hnsw](https://github.com/coder/hnsw) - Pure Go HNSW implementation
- [Nomic Embed Code](https://huggingface.co/nomic-ai/nomic-embed-code-v1) - Embedding model

---

## Research Sources

### gollama.cpp
- GitHub: https://github.com/dianlight/gollama.cpp
- Uses purego for no-CGO C interop
- Auto-downloads pre-built llama.cpp libraries
- Active development (2024-2025)

### coder/hnsw
- GitHub: https://github.com/coder/hnsw
- Pure Go HNSW implementation
- Used in production by Coder
- 1232 MB/s export speed

### Nomic Embed Models
- HuggingFace: nomic-ai/nomic-embed-code-v1
- HuggingFace: nomic-ai/nomic-embed-text-v1.5
- GGUF quantized versions available
- Code-specific training for code search

### Hybrid Search Research (2024)
- Dense + sparse retrieval outperforms either alone
- RRF fusion recommended (k=60 optimal)
- Validates our existing architecture

---

## Decisions Made (2026-01-03)

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Model distribution | **Auto-download on first use** | ~60MB binary, downloads to ~/.amanmcp/models/, shows progress |
| Daemon infrastructure | **Keep for future use** | Retained in codebase, enables background search service |
| Migration | **Auto-detect and rebuild** | amanmcp index detects old format, removes, rebuilds automatically |

---

## Changelog

| Date | Change |
|------|--------|
| 2026-01-03 | Initial proposal based on BUG-018 investigation |
| 2026-01-03 | **Implemented in v0.1.38**: USearch→coder/hnsw, Hugot→gollama.cpp v0.2.2, nomic-embed-text-v1.5 |
