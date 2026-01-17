# Hybrid Search Overview

What hybrid search is, why it matters, and when to use it.

**Reading time:** 3 minutes
**Audience:** Everyone
**Prerequisites:** None (but [Search Fundamentals](../../learning/search-fundamentals.md) provides helpful background)

---

## Quick Summary

- Hybrid search combines **keyword search** (exact matches) with **semantic search** (meaning matches)
- Neither approach alone is sufficient for code search
- AmanMCP uses both and merges their results intelligently
- You get the best of both worlds without thinking about it

---

## The Problem

There are two ways to search text:

### Keyword Search (BM25)

Finds documents containing your exact words.

**Great for:**
- Function names: `ValidateToken` finds exactly that
- Error codes: `ERR_CONNECTION_REFUSED`
- Specific identifiers

**Misses:**
- Synonyms: searching "auth" won't find "authentication"
- Related concepts: searching "login" won't find "credentials"

### Semantic Search (Vector)

Finds documents with similar *meaning*, even if words differ.

**Great for:**
- Natural questions: "how does user login work?"
- Concepts: "authentication flow" finds `VerifyCredentials`
- Synonyms automatically handled

**Misses:**
- Exact identifiers: `ERR_001` might not match precisely
- Rare technical terms
- Code variable names (they don't have "meaning")

---

## The Solution: Use Both

Hybrid search runs both searches in parallel and combines the results:

```
Your Query
    │
    ├──→ Keyword Search ──→ [Results ranked by exact match]
    │                              │
    │                              ▼
    │                        ┌──────────┐
    │                        │  Merge   │──→ Best Results
    │                        │ Results  │
    ├──→ Semantic Search ──→ └──────────┘
    │         [Results ranked by meaning]
```

**The key insight:** Documents that rank highly in *both* searches are almost certainly relevant. Documents that rank highly in only one might still be useful.

---

## Why This Matters for Code Search

Code is unique:

| Challenge | Keyword Alone | Semantic Alone | Hybrid |
|-----------|---------------|----------------|--------|
| Find `ProcessPayment` function | Works | Might miss | Works |
| Find "payment handling" concept | Misses related code | Works | Works |
| Find error code `E1001` | Works | Unreliable | Works |
| Find "how auth works" | Misses synonyms | Works | Works |

Hybrid search handles all these cases.

---

## How AmanMCP Uses Hybrid Search

When you search in AmanMCP:

1. Your query goes to both BM25 (keyword) and Vector (semantic) engines
2. Each returns ranked results
3. Results are merged using a technique called RRF (Reciprocal Rank Fusion)
4. You see the combined, best results

**Default weights:**
- BM25 (keyword): 35%
- Vector (semantic): 65%

The semantic weight is higher because natural language queries benefit more from meaning-based search. But keyword search is crucial for exact matches.

---

## When Each Shines

| Query Type | Best Approach | Example |
|------------|---------------|---------|
| Exact function name | Keyword-heavy | `func HandlePayment` |
| Error code lookup | Keyword-heavy | `ERR_AUTH_FAILED` |
| Concept exploration | Semantic-heavy | "how does caching work" |
| Mixed query | Balanced | "retry logic in PaymentService" |

AmanMCP automatically adjusts weights based on query type.

---

## Next Steps

| Want to... | Read |
|------------|------|
| Understand how it works with examples | [How It Works](how-it-works.md) |
| Learn the algorithms and formulas | [Advanced](advanced.md) |
| Try it yourself | [Your First Search](../../tutorials/your-first-search.md) |

---

*Hybrid search: precision AND recall. You don't have to choose.*
