# ADR-038: Black Box Indexer and Searcher Architecture

**Status:** Accepted
**Date:** 2026-01-14
**Supersedes:** None
**Superseded by:** None

---

## Context

The search engine (`internal/search/engine.go`) contained tightly coupled indexing logic that combined BM25 indexing, vector indexing, metadata storage, and embedding generation in a single monolithic `Index()` method (lines 349-412).

This coupling caused several problems:

1. **Testing Difficulty**: Cannot test BM25 indexing without setting up embedders, vector stores, and metadata stores
2. **Change Amplification**: Modifying BM25 implementation (e.g., SQLite FTS5 → Tantivy) requires touching search engine code
3. **Cognitive Load**: Understanding the Index() method requires understanding 6 different subsystems
4. **SPIKE-006 Blocked**: Cannot benchmark Tantivy as alternative BM25 backend without clean interface

We evaluated options following Eskil Steenberg's Black Box Design principles from his "Architecting Large Software Projects" talk.

---

## Decision

Extract indexing and search logic into standalone modules with clean interfaces:

```
pkg/indexer/                      pkg/searcher/
├── interface.go    # Indexer     ├── interface.go    # Searcher
├── bm25.go        # BM25         ├── bm25.go        # BM25
├── vector.go      # Vector       ├── vector.go      # Vector
└── hybrid.go      # Hybrid       └── fusion.go      # RRF Fusion
```

### Interface Design

```go
// pkg/indexer/interface.go
type Indexer interface {
    Index(ctx context.Context, chunks []*store.Chunk) error
    Delete(ctx context.Context, ids []string) error
    Clear(ctx context.Context) error
    Stats() IndexStats
    Close() error
}
```

### BM25Indexer Implementation

```go
// pkg/indexer/bm25.go
type BM25Indexer struct {
    store store.BM25Index  // Injected dependency
    mu    sync.RWMutex     // Thread safety
}

func NewBM25Indexer(opts ...Option) (*BM25Indexer, error)
```

### VectorIndexer Implementation

```go
// pkg/indexer/vector.go
type VectorIndexer struct {
    embedder embed.Embedder      // Generates embeddings
    store    store.VectorStore   // Stores vectors
    mu       sync.RWMutex        // Thread safety
}

func NewVectorIndexer(opts ...VectorOption) (*VectorIndexer, error)
```

**Key Difference from BM25Indexer**: VectorIndexer requires two dependencies:
1. `embed.Embedder` - Converts text → embeddings
2. `store.VectorStore` - Stores vector embeddings

**Stats Adaptation**: VectorStore has `Count()` but not full stats:
- `DocumentCount` = store.Count()
- `TermCount` = 0 (N/A for vectors)
- `AvgDocLength` = 0 (N/A for vectors)

### HybridIndexer Implementation

```go
// pkg/indexer/hybrid.go
type HybridIndexer struct {
    bm25   Indexer      // May be nil for vector-only
    vector Indexer      // May be nil for BM25-only
    mu     sync.RWMutex
}

func NewHybridIndexer(opts ...HybridOption) (*HybridIndexer, error)
```

**Composition Pattern**: HybridIndexer composes two Indexer implementations:
- `WithBM25(idx)` - Sets BM25 component
- `WithVector(idx)` - Sets Vector component
- At least one must be provided (returns `ErrNoIndexers` otherwise)

**Coordination Patterns**:
| Operation | Pattern | Rationale |
|-----------|---------|-----------|
| Index | Sequential, fail-fast | Atomic behavior - both succeed or neither |
| Delete | Best-effort, continue on failure | Orphans are harmless, cleaned during compaction |
| Close | Accumulate errors, return joined | Ensures both are attempted |

**Stats Aggregation**:
- `DocumentCount` = max(bm25, vector) for consistency check
- `TermCount` = from BM25 only (vectors don't have terms)
- `AvgDocLength` = from BM25 only

### Searcher Interface Design

```go
// pkg/searcher/interface.go
type Searcher interface {
    Search(ctx context.Context, query string, limit int) ([]Result, error)
}

type Result struct {
    ID           string    // Chunk ID
    Score        float64   // Normalized score (0-1)
    MatchedTerms []string  // Terms that matched (BM25 only)
}
```

### BM25Searcher Implementation

```go
// pkg/searcher/bm25.go
type BM25Searcher struct {
    store store.BM25Index  // Injected dependency
    mu    sync.RWMutex     // Thread safety
}

func NewBM25Searcher(opts ...BM25Option) (*BM25Searcher, error)
```

### VectorSearcher Implementation

```go
// pkg/searcher/vector.go
type VectorSearcher struct {
    embedder embed.Embedder      // Embeds queries
    store    store.VectorStore   // Searches vectors
    mu       sync.RWMutex        // Thread safety
}

func NewVectorSearcher(opts ...VectorOption) (*VectorSearcher, error)
```

**Key Feature**: VectorSearcher handles query embedding internally:
1. Formats query with Qwen3 instruction prefix
2. Embeds query via embedder.Embed()
3. Searches vector store with embedding

### FusionSearcher Implementation

```go
// pkg/searcher/fusion.go
type FusionSearcher struct {
    bm25   Searcher        // May be nil for vector-only
    vector Searcher        // May be nil for BM25-only
    config FusionConfig
    mu     sync.RWMutex
}

type FusionConfig struct {
    BM25Weight     float64  // Default: 0.35
    SemanticWeight float64  // Default: 0.65
    RRFConstant    int      // Default: 60
}

func NewFusionSearcher(opts ...FusionOption) (*FusionSearcher, error)
```

**RRF Fusion**: `score(d) = Σ weight_i / (k + rank_i)`
- Parallel search with errgroup
- Graceful degradation: if one fails, returns other's results
- Returns error only if both fail

---

## Rationale

### Why `pkg/` Instead of `internal/`?

| Consideration | `internal/` | `pkg/` |
|---------------|-------------|--------|
| Reusability | Internal only | External tools can import |
| API Stability | Can change freely | Implies stable API |
| Black Box Principle | Partial | Full (replaceable by external code) |

**Decision**: `pkg/` because Black Box Design emphasizes replaceability - external tools should be able to provide alternative implementations.

### Why Wrap `store.BM25Index`?

The `Indexer` interface operates on `Chunk` (domain model), while `store.BM25Index` operates on `Document` (storage model). The wrapper:

1. Converts between domain and storage models
2. Provides a cleaner API for callers
3. Enables future composition (HybridIndexer)
4. Allows swapping storage backends without interface changes

### Alternatives Considered

| Option | Pros | Cons |
|--------|------|------|
| Keep coupled in Engine | No refactoring needed | Testing remains hard, change amplification continues |
| **Extract to pkg/indexer/** | Clean interfaces, testable, replaceable | Requires refactoring, new abstraction layer |
| Extract to internal/indexer/ | Less API commitment | Cannot be replaced by external code |

---

## Consequences

### Positive

- **Testability**: All modules tested with mocks (110 unit tests total: Indexers 69 + Searchers 41)
- **Replaceability**: Can swap SQLite FTS5 → Tantivy by implementing Indexer interface
- **Single Responsibility**: Each module has one clear purpose
- **Parallel Development**: BB2, BB3 can be developed independently
- **SPIKE-006 Enabled**: Clean interface for Tantivy benchmark comparison

### Negative

- **Abstraction Layer**: One more layer between caller and store
- **Migration Effort**: Future work needed for BB3, BB4, BB5

### Neutral

- **Facade Pattern**: Engine can delegate to Indexer while maintaining current API
- **No Breaking Changes**: Existing callers (coordinator, runner) unchanged

---

## Implementation Notes

### Phase 1 (FEAT-BB2) - Completed

- `pkg/indexer/interface.go` - Indexer interface
- `pkg/indexer/bm25.go` - BM25Indexer implementation
- `pkg/indexer/bm25_test.go` - 19 unit tests with mocks

### Phase 2 (FEAT-BB3) - Completed

- `pkg/indexer/vector.go` - VectorIndexer wrapping embed.Embedder + store.VectorStore
- `pkg/indexer/vector_test.go` - 21 unit tests with mocks
- Uses `VectorOption` type (separate from BM25 `Option` to avoid conflicts)
- Index flow: Chunks → Extract texts → EmbedBatch() → Add(ids, embeddings)

### Phase 3 (FEAT-BB4) - Completed

- `pkg/indexer/hybrid.go` - Composes BM25Indexer + VectorIndexer
- `pkg/indexer/hybrid_test.go` - 29 unit tests with mocks
- Uses sequential indexing (safer), best-effort deletion
- Supports BM25-only, Vector-only, or full hybrid modes via nil indexers

### Phase 4 (Integration) - Future

- Engine.Index() delegates to HybridIndexer
- Backward compatible via facade pattern

### Phase 5 (FEAT-BB5) - Completed

- `pkg/searcher/interface.go` - Searcher interface, Result, FusionConfig
- `pkg/searcher/bm25.go` - BM25Searcher wrapping store.BM25Index
- `pkg/searcher/vector.go` - VectorSearcher with query embedding
- `pkg/searcher/fusion.go` - FusionSearcher with RRF algorithm
- Tests: 41 unit tests (9 BM25 + 13 Vector + 19 Fusion)

---

## Test Strategy

Following existing patterns from `internal/store/bm25_test.go`:

- **Mock Pattern**: Function pointer injection (`MockBM25Store`)
- **Structure**: Given-When-Then comments
- **Categories**: Basic ops, edge cases, errors, concurrency, idempotency
- **Race Detection**: All tests pass with `-race` flag

---

## Related

- **FEAT-BB2**: BM25Indexer module extraction (completed)
- **FEAT-BB3**: VectorIndexer module extraction (completed)
- **FEAT-BB4**: HybridIndexer composition (completed)
- **FEAT-BB5**: Searcher modules (completed)
- **SPIKE-006**: Tantivy benchmark (enabled by this work)
- **ADR-012**: BM25 Implementation Approach (Bleve selection)
- **Skill**: `.claude/skills/black-box-design/SKILL.md`

---

## Changelog

| Date | Change |
|------|--------|
| 2026-01-14 | Initial proposal and implementation (FEAT-BB2) |
| 2026-01-14 | Accepted - pkg/indexer/bm25.go with 19 passing tests |
| 2026-01-14 | VectorIndexer added (FEAT-BB3) - pkg/indexer/vector.go with 21 passing tests |
| 2026-01-14 | HybridIndexer added (FEAT-BB4) - pkg/indexer/hybrid.go with 29 passing tests |
| 2026-01-14 | Searcher modules added (FEAT-BB5) - pkg/searcher/ with 41 passing tests |
