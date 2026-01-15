# ADR-012: BM25 Implementation Approach

**Status:** Accepted
**Date:** 2025-12-28
**Supersedes:** None
**Superseded by:** None

---

## Context

F11 requires a BM25 keyword search index for hybrid search. The original spec assumed a custom implementation with manual inverted index, term frequency, and document frequency calculations.

During Phase 1B review, we evaluated this decision against available Go libraries:

1. **Custom Implementation** - Build BM25 from scratch (~500+ lines)
2. **Bleve v2** - Full-text search library with native BM25 support
3. **Bluge** - Evolution of Bleve with better scoring control

Key considerations:

- AmanMCP philosophy: "It Just Works" - reliability is critical
- Time-to-market for Phase 1B
- Maintenance burden for custom solutions
- Future extensibility (phrase queries, fuzzy matching)

---

## Decision

We will use **Bleve v2** as the BM25 implementation, wrapped behind an interface for future replacement.

```go
// BM25Index interface (from specification.md)
type BM25Index interface {
    Index(ctx context.Context, id string, content string) error
    Delete(ctx context.Context, id string) error
    Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
    Stats() BM25Stats
    Save(path string) error
    Load(path string) error
    Close() error
}

// Implementation uses Bleve v2 internally
type bleveBM25Index struct {
    index bleve.Index
    // ...
}
```

---

## Rationale

### Alternatives Considered

| Option | Pros | Cons |
|--------|------|------|
| Custom | Full control, learning opportunity, no dependencies | 500+ lines of code, edge cases (tokenization, IDF calculation), maintenance burden, reinventing well-solved problems |
| **Bleve v2** | Battle-tested (329+ production users), native BM25 scoring, RRF fusion built-in (Dec 2025), active maintenance, comprehensive documentation | External dependency, less fine-grained control |
| Bluge | Better scoring at clause level, evolved from Bleve | Smaller community, less documentation |

### Why Bleve v2?

1. **Production Ready**: Used by 329+ packages in production
2. **Native BM25**: `indexMapping.ScoringModel = index.BM25Scoring`
3. **RRF Built-in**: December 2025 release includes Reciprocal Rank Fusion
4. **Active Development**: Recent releases address performance issues
5. **Interface Abstraction**: We wrap it, so we can replace later if needed

### Why Not Custom?

- BM25 is a well-understood algorithm with subtle edge cases
- Tokenization for code (camelCase, snake_case) is non-trivial
- IDF calculation with edge cases (zero document frequency)
- Thread-safe concurrent access is complex
- Persistence format design and maintenance
- Time better spent on unique value (hybrid search, MCP integration)

---

## Consequences

### Positive

- Faster implementation (days vs weeks)
- Production-grade reliability from day one
- Built-in RRF fusion simplifies F14 implementation
- Access to phrase queries, fuzzy matching in future
- Active community for bug fixes and improvements

### Negative

- External dependency (github.com/blevesearch/bleve/v2)
- Less fine-grained control over scoring
- Must learn Bleve API patterns
- Larger binary size

### Neutral

- Interface abstraction allows future migration to Bluge or custom
- Bleve's tokenization may differ from our ideal (can customize)

---

## Implementation Notes

### Bleve v2 Configuration for BM25

```go
import (
    "github.com/blevesearch/bleve/v2"
    "github.com/blevesearch/bleve/v2/mapping"
    "github.com/blevesearch/bleve/v2/index"
)

func NewBleveBM25Index(path string) (BM25Index, error) {
    // Create mapping with BM25 scoring
    indexMapping := mapping.NewIndexMapping()
    indexMapping.ScoringModel = index.BM25Scoring

    // Configure BM25 parameters (k1=1.2, b=0.75 are defaults)
    // Bleve uses these by default, which matches our spec

    // Create or open index
    idx, err := bleve.New(path, indexMapping)
    if err == bleve.ErrorIndexPathExists {
        idx, err = bleve.Open(path)
    }

    return &bleveBM25Index{index: idx}, err
}
```

### Code-Aware Tokenization

```go
// Custom analyzer for code
import "github.com/blevesearch/bleve/v2/analysis"

func codeTokenizer() analysis.Tokenizer {
    // Bleve supports custom tokenizers
    // Split on camelCase, snake_case, punctuation
    // Filter programming stop words
}
```

---

## Related

- [Feature F11](../specs/features/F11-bm25-index.md) - BM25 Index specification
- [Feature F13](../specs/features/F13-hybrid-search.md) - Hybrid Search (uses BM25)
- [Feature F14](../specs/features/F14-rrf-fusion.md) - RRF Fusion
- [Bleve Documentation](https://blevesearch.com/)
- [Bleve GitHub](https://github.com/blevesearch/bleve)

---

## Changelog

| Date | Change |
|------|--------|
| 2025-12-28 | Initial proposal during Phase 1B review |
| 2025-12-28 | Accepted - Bleve v2 with interface abstraction |
