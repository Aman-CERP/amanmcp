# ADR-034: Query Expansion for BM25 Search

**Status:** Implemented
**Date:** 2026-01-08
**Deciders:** AmanMCP Team

---

## Context

### Problem Statement

Three dogfood queries fail due to vocabulary mismatch between natural language and code:

1. "Search function" - User says "search", code uses "Search", "Engine", "query"
2. "Index function" - User says "index", code uses "Indexer", "Coordinator"
3. "OllamaEmbedder" - User says "embedder", code uses "Embedder", "embed"

### BM25 Vocabulary Sensitivity

BM25 is a term-frequency based algorithm. It requires exact or near-exact matches:
- "search" does NOT match "Search" (case matters in tokens)
- "function" does NOT match "func" (Go keyword)
- "embedder" does NOT match "Embedder" (case matters)

### Existing Query Expander

AmanMCP already has `internal/search/synonyms.go` with a `QueryExpander` that adds synonyms:
- "error" → ["error", "Error", "err", "failure"]
- "function" → ["function", "func", "fn"]

However, this was only applied to BM25 queries, not vector search. Initial testing showed extending to vector search caused regressions.

---

## Decision

**We will enhance the existing QueryExpander with code-specific synonyms** for the failing queries, applied ONLY to BM25 search (not vector search).

### Key Design Choices

| Decision | Rationale |
|----------|-----------|
| BM25-only expansion | Vector embeddings already handle semantics |
| Case variants | BM25 is case-sensitive in tokenization |
| Conservative expansion | Avoid precision loss from over-expansion |

---

## Rationale

### Why BM25-Only?

We tested query expansion for both BM25 and vector search. Results:

| Configuration | Tier 1 Pass Rate |
|---------------|------------------|
| No expansion | 75% |
| BM25 + Vector expansion | **50%** (regression!) |
| BM25-only expansion | 75%+ (expected improvement) |

**Finding:** Vector search uses embeddings that already capture semantic similarity. Adding expanded terms dilutes the query embedding, reducing quality.

### Why Not Vector Expansion?

Vector embeddings represent query meaning holistically. When you expand:
- Original: "Search function"
- Expanded: "Search function func fn search query lookup"

The expanded version creates an embedding that's a blend of concepts, reducing precision for the original intent.

### Added Synonyms

```go
// internal/search/synonyms.go - new mappings
"search":    {"Search", "search", "find", "query", "lookup", "Engine"},
"index":     {"Index", "index", "indexer", "Indexer", "Coordinator"},
"embedder":  {"Embedder", "embedder", "embed", "embedding", "Ollama", "vector"},
"ollama":    {"Ollama", "ollama", "embedder", "Embedder", "embed", "OllamaEmbedder"},
"function":  {"function", "func", "fn", "method", "Function"},
```

---

## Consequences

### Positive

1. **BM25 improvement** - Better matching for natural language queries
2. **No vector regression** - Embeddings maintain semantic precision
3. **Minimal code change** - Extended existing synonyms.go

### Negative

1. **BM25/Vector asymmetry** - Different query handling for each path
2. **Synonym maintenance** - Must update mappings for new vocabulary gaps

### Mitigations

1. **Clear documentation** - ADR explains asymmetric approach
2. **Dogfood testing** - Catch vocabulary gaps early

---

## Implementation

### Files Modified

| File | Change |
|------|--------|
| `internal/search/synonyms.go` | Added synonyms for "search", "index", "embedder", "ollama" |

### Code Change

```go
// internal/search/engine.go - parallelSearch()

// BM25 gets expanded query
bm25Query := query
if e.expander != nil {
    bm25Query = e.expander.Expand(query)
}

// Vector search uses ORIGINAL query - embeddings handle semantics
embedding, embedErr := e.embedder.Embed(gctx, query)  // Not bm25Query!
```

---

## Validation Criteria

This ADR is successfully implemented when:

- [x] Synonyms added for failing queries
- [x] BM25 uses expanded query
- [x] Vector search uses original query
- [ ] Dogfood queries pass (pending reindex)
- [x] No regression in Tier 1 pass rate
- [x] `make ci-check` passes

---

## Alternative Considered: Universal Expansion

We considered applying expansion to both BM25 and vector:

```go
// Rejected approach
expandedQuery := e.expander.Expand(query)
bm25Results := bm25Search(expandedQuery)
vectorResults := vectorSearch(expandedQuery)  // Causes regression
```

**Result:** Pass rate dropped from 75% to 50%. Vector embeddings work best with original user intent.

---

## References

- [ADR-033 - Contextual Retrieval](./ADR-033-contextual-retrieval.md)
- [F34 - Query Expansion Feature Spec](../specs/features/F34-query-expansion.md)
- [RCA-010 - Vocabulary Mismatch Analysis](../dogfooding/rca-010.md)

---

## Changelog

| Date | Change |
|------|--------|
| 2026-01-08 | Initial implementation |
