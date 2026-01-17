# Hybrid Search: How It Works

A practical explanation of how AmanMCP combines keyword and semantic search, with examples.

**Reading time:** 8 minutes
**Audience:** Users who want to understand the search process
**Prerequisites:** [Overview](overview.md)

---

## Quick Summary

- BM25 scores documents by how often your search terms appear
- Vector search scores by how similar the meaning is to your query
- RRF (Reciprocal Rank Fusion) combines rankings from both
- Query classification adjusts weights based on query type

---

## How BM25 (Keyword Search) Works

BM25 is like a smarter version of grep. It finds documents containing your words and ranks them.

### The Core Idea

BM25 asks three questions:

1. **Does this document contain my search terms?**
2. **How often do they appear?** (more = higher score)
3. **How rare is this term overall?** (rare terms matter more)

### Example

```
Query: "authentication middleware"

Document A: "Authentication middleware validates tokens"
Document B: "The middleware checks authentication status"
Document C: "Database connection pool"

Scoring:
- Doc A: Contains both "authentication" AND "middleware" → High score
- Doc B: Contains both, but further apart → Medium score
- Doc C: Contains neither → Zero score
```

### Why Rare Terms Matter More

If you search for "the authentication", the word "the" appears in almost every document - it's not helpful. But "authentication" is specific - it's a strong signal.

BM25 automatically weights rare terms higher:

```
"the"            → appears in 95% of docs → low weight
"authentication" → appears in 2% of docs  → high weight
```

### What BM25 Is Good At

| Scenario | Why It Works |
|----------|--------------|
| Function names | Exact match: `ValidateToken` |
| Error codes | Unique strings: `ERR_AUTH_001` |
| Variable names | Technical identifiers |
| Quoted phrases | Exact sequence matching |

### What BM25 Misses

| Scenario | Why It Fails |
|----------|--------------|
| "auth" vs "authentication" | Different words |
| "login" vs "sign in" | Synonyms |
| "handle errors" | Doesn't find `RecoverPanic()` |

---

## How Vector Search (Semantic) Works

Vector search finds documents with similar *meaning*, even when words differ.

### The Core Idea

Every piece of text gets converted to a list of numbers (an "embedding") that captures its meaning:

```
"authentication"    → [0.8, 0.3, 0.9, ...]
"login credentials" → [0.8, 0.3, 0.8, ...]  ← Similar numbers!
"weather forecast"  → [0.1, 0.9, 0.2, ...]  ← Different numbers
```

Similar meanings → Similar numbers → Found together in search.

### Example

```
Query: "user login"

The query becomes: [0.7, 0.4, 0.8, ...]

Documents in the index:
- auth.go      → [0.7, 0.4, 0.7, ...] ← Very similar!
- session.go   → [0.6, 0.5, 0.7, ...] ← Somewhat similar
- database.go  → [0.1, 0.8, 0.2, ...] ← Not similar

Results: auth.go, then session.go
```

### What Vector Search Is Good At

| Scenario | Why It Works |
|----------|--------------|
| Natural language | "how does auth work?" |
| Synonyms | "login" finds "authenticate" |
| Concept exploration | "error handling" finds `RecoverPanic()` |

### What Vector Search Misses

| Scenario | Why It Fails |
|----------|--------------|
| Exact identifiers | `ProcessPayment_v2` might match wrong version |
| Rare technical terms | Domain-specific vocabulary |
| Code variables | Variable names don't have semantic meaning |

---

## How RRF Combines Results

RRF (Reciprocal Rank Fusion) is how we merge two ranked lists into one.

### The Problem

BM25 returns: `[A, B, C, D]` (by keyword match)
Vector returns: `[C, A, D, B]` (by meaning)

How do we combine them? We can't average scores - they're on different scales.

### The Solution: Use Rankings, Not Scores

RRF only looks at *position* in each list:

```
Document A:
  - BM25 rank: 1 (top of keyword results)
  - Vector rank: 2 (second in semantic results)
  - Combined: High (good in both!)

Document C:
  - BM25 rank: 3 (lower in keywords)
  - Vector rank: 1 (top of semantic)
  - Combined: High (best semantic match)

Document D:
  - BM25 rank: 4 (low)
  - Vector rank: 3 (medium)
  - Combined: Medium (not great in either)
```

### The Key Insight

**Documents that rank well in BOTH searches are almost certainly relevant.**

If a document is #1 in keyword search AND #2 in semantic search, it's probably exactly what you want.

### Visual Example

```
Query: "validate user token"

BM25 Results:           Vector Results:
1. validate_token.go    1. auth_middleware.go
2. user_auth.go         2. validate_token.go
3. token_service.go     3. session_handler.go
4. config.go            4. user_auth.go

RRF Combined:
1. validate_token.go  ← #1 in BM25, #2 in Vector = Best
2. user_auth.go       ← #2 in BM25, #4 in Vector = Good
3. auth_middleware.go ← Not in BM25 top, but #1 in Vector
4. token_service.go   ← #3 in BM25 only
```

---

## Query Classification

Different queries need different weights.

### Why?

- Searching `func ProcessPayment` → You want exact match → Heavy BM25
- Searching "how does payment work" → You want concepts → Heavy Vector
- Searching "ProcessPayment retry logic" → Mixed → Balanced

### How AmanMCP Classifies Queries

| Query Pattern | BM25 Weight | Vector Weight | Example |
|---------------|-------------|---------------|---------|
| Quoted phrase | 90% | 10% | `"exact match"` |
| Error code | 80% | 20% | `ERR_AUTH_001` |
| CamelCase identifier | 70% | 30% | `ValidateToken` |
| snake_case identifier | 70% | 30% | `process_payment` |
| Natural language question | 25% | 75% | "how does auth work" |
| Default | 35% | 65% | Most queries |

### Classification in Action

```
Query: "ERR_CONNECTION_REFUSED"
  → Looks like error code
  → BM25: 80%, Vector: 20%
  → Keyword search dominates

Query: "how to handle database connection errors"
  → Natural language question
  → BM25: 25%, Vector: 75%
  → Semantic search dominates

Query: "retry logic"
  → Mixed/default
  → BM25: 35%, Vector: 65%
  → Balanced approach
```

---

## The Complete Flow

Here's what happens when you search:

```
┌─────────────────────────────────────────────────────────────┐
│  1. Query: "authentication middleware"                      │
└─────────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  2. Classify Query                                          │
│     → Mixed terms, use default weights                      │
│     → BM25: 35%, Vector: 65%                                │
└─────────────────────────────────────────────────────────────┘
                         │
          ┌──────────────┴──────────────┐
          ▼                             ▼
┌─────────────────────┐     ┌─────────────────────┐
│  3a. BM25 Search    │     │  3b. Vector Search  │
│  (runs in parallel) │     │  (runs in parallel) │
│                     │     │                     │
│  Results:           │     │  Results:           │
│  1. auth.go         │     │  1. middleware.go   │
│  2. middleware.go   │     │  2. auth.go         │
│  3. token.go        │     │  3. session.go      │
└─────────────────────┘     └─────────────────────┘
          │                             │
          └──────────────┬──────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  4. RRF Fusion                                              │
│     Combine rankings with weights                           │
│     auth.go: #1 BM25 + #2 Vector = Top result               │
│     middleware.go: #2 BM25 + #1 Vector = Top result         │
└─────────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  5. Final Results                                           │
│     1. auth.go (0.92)                                       │
│     2. middleware.go (0.91)                                 │
│     3. session.go (0.78)                                    │
│     ...                                                     │
└─────────────────────────────────────────────────────────────┘
```

---

## Performance

Both searches run **in parallel**, so the total time is the slowest one, not both added:

| Component | Typical Time |
|-----------|--------------|
| Query classification | < 1ms |
| BM25 search | 2-10ms |
| Vector search | 5-15ms |
| RRF fusion | < 1ms |
| **Total** | **~15ms** |

---

## Practical Tips

### Get Better Results

| If Results Are... | Try |
|-------------------|-----|
| Missing exact matches | Quote your query: `"exact phrase"` |
| Too literal | Use natural language: "how does X work" |
| Wrong domain | Add context: "payment authentication" vs just "auth" |

### Debugging Poor Results

1. **Check if file is indexed:** `amanmcp status`
2. **Try different terms:** Code might use different vocabulary
3. **Use both styles:** "ValidateToken authentication flow"

---

## Next Steps

| Want to... | Read |
|------------|------|
| See the formulas and algorithms | [Advanced](advanced.md) |
| Understand vector search deeply | [Vector Search](../vector-search/) |
| Try searching | [Your First Search](../../tutorials/your-first-search.md) |

---

*Hybrid search combines the best of both worlds. You don't need to choose.*
