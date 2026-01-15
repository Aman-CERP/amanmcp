# ADR-004: Hybrid Search with RRF Fusion

**Status:** Implemented
**Date:** 2025-12-28
**Supersedes:** None
**Superseded by:** None

---

## Context

AmanMCP implements hybrid search combining BM25 (keyword) and semantic (vector) search. The challenge is merging results from these two fundamentally different ranking systems:

1. **BM25** returns scores based on term frequency (typically 0-25 range)
2. **Vector search** returns cosine similarity scores (0-1 range)

Direct score combination doesn't work because:
- Scores are on different scales
- Score distributions vary by query type
- One method can dominate results unfairly

---

## Decision

We will use **Reciprocal Rank Fusion (RRF)** with k=60 to combine BM25 and semantic search results.

Formula:
```
RRF_score(d) = Σ weight_i / (k + rank_i)
```

Configuration:
- Default k: 60 (empirically validated)
- Default weights: BM25: 0.35, Semantic: 0.65
- Tie-breaking: InBothLists → BM25Score → ChunkID

---

## Rationale

### Alternatives Considered

| Option | Pros | Cons |
|--------|------|------|
| Score normalization | Simple concept | Requires knowing score distributions, brittle |
| Linear combination | Easy to implement | Scores incomparable across methods |
| **Chosen: RRF (k=60)** | Rank-based (ignores score scale), proven effective, simple | Loses fine-grained score info |
| CombMNZ | Good for multi-source | More complex, needs tuning |

### Why RRF with k=60

1. **Rank-based**: Uses position, not score values - immune to scale differences
2. **Proven**: Widely used in information retrieval research
3. **k=60 sweet spot**:
   - Low k (e.g., 10): Top ranks dominate too heavily
   - High k (e.g., 100): Rankings matter less
   - k=60: Good balance, empirically validated
4. **Simple**: Easy to implement, understand, and debug

---

## Consequences

### Positive

- Robust fusion regardless of score distributions
- No need to normalize or calibrate scores
- Consistent behavior across query types
- Well-understood algorithm with research backing

### Negative

- Loses granular score information
- k=60 is a compromise (not optimal for all cases)
- Requires both searches to return ranked results

### Neutral

- Weights (0.35/0.65) can be adjusted per use case
- Performance is O(n) for n results per list

---

## Implementation Notes

```go
// internal/search/fusion.go
const DefaultRRFConstant = 60

type RRFFusion struct {
    k int
}

func NewRRFFusion() *RRFFusion {
    return &RRFFusion{k: DefaultRRFConstant}
}

func (f *RRFFusion) Fuse(bm25Results, vecResults []Result, weights Weights) []Result {
    // Calculate RRF score for each document
    // RRF_score = Σ weight / (k + rank)

    // Tie-breaking priority:
    // 1. InBothLists (documents in both result sets rank higher)
    // 2. BM25Score (prefer keyword matches when tied)
    // 3. ChunkID (deterministic ordering)
}
```

Integration in search engine:
```go
// internal/search/engine.go
rrfResults := e.fusion.Fuse(bm25Results, vecResults, *weights)
```

---

## Related

- [ADR-012](./ADR-012-bm25-implementation.md) - BM25 implementation
- [ADR-001](./ADR-001-vector-database-usearch.md) - Vector search backend
- [F13](../specs/features/F13-hybrid-search.md) - Hybrid search feature
- [F14](../specs/features/F14-rrf-fusion.md) - RRF fusion feature
- [Hybrid Search Guide](../guides/hybrid-search.md) - Conceptual guide

---

## Changelog

| Date | Change |
|------|--------|
| 2025-12-28 | Initial implementation |
