# Tutorial: Understanding Results

Learn how to interpret AmanMCP search results, why certain results rank higher, and how to improve your searches.

**Time:** 15 minutes
**Prerequisites:** Completed [Your First Search](your-first-search.md)

---

## Goal

By the end of this tutorial, you will:
- Understand what relevance scores mean
- Know how hybrid search affects ranking
- Diagnose why expected results might be missing
- Tune searches for better results

---

## Step 1: Anatomy of a Result

Let's examine a search result in detail:

```bash
amanmcp search "database connection"
```

**Sample output:**
```
Found 5 results for "database connection"

1. internal/store/postgres.go:23-58 (score: 0.91, bm25: 0.42, vec: 0.58)
   func NewPostgresStore(connStr string) (*Store, error) {
       db, err := sql.Open("postgres", connStr)
       if err != nil {
           return nil, fmt.Errorf("failed to open database connection: %w", err)
       }
       ...

2. internal/store/pool.go:12-45 (score: 0.84, bm25: 0.35, vec: 0.49)
   type ConnectionPool struct {
       maxConns    int
       activeConns int
       ...
```

### Breaking Down the Score

| Component | Meaning |
|-----------|---------|
| `score: 0.91` | **Final combined score** (0.0 to 1.0) |
| `bm25: 0.42` | Keyword match contribution |
| `vec: 0.58` | Semantic match contribution |

The final score is calculated using **RRF (Reciprocal Rank Fusion)** with:
- BM25 weight: 35%
- Vector weight: 65%

---

## Step 2: Why Results Rank the Way They Do

### High BM25 + High Vector = Top Result

```
Query: "database connection"

Result 1: postgres.go (score: 0.91)
- Contains exact words "database connection" → High BM25
- Semantically about database connectivity → High Vector
- Both agree → Top rank
```

### High Vector, Lower BM25

```
Result 2: pool.go (score: 0.84)
- Doesn't contain exact "database connection" → Lower BM25
- Is semantically about connection pooling → High Vector
- Still ranks well because of semantic relevance
```

### High BM25, Lower Vector

Sometimes a result has exact keywords but isn't conceptually relevant:

```
// In a comment somewhere
"See database connection docs for more info"

- Contains exact phrase → High BM25
- Just a reference, not actual implementation → Lower Vector
- Ranks lower overall
```

---

## Step 3: Experiment with Search Modes

Try the same query with different modes to see how each search type behaves.

### Hybrid (default)

```bash
amanmcp search "user authentication"
```

Gets you the best of both worlds.

### What each component finds

Imagine these results:

| Rank | Code | BM25 Would Find | Vector Would Find |
|------|------|-----------------|-------------------|
| 1 | `AuthenticateUser()` | Yes (exact match) | Yes (same concept) |
| 2 | `VerifyCredentials()` | No (different words) | Yes (same concept) |
| 3 | `// user authentication` | Yes (exact match) | Maybe (just a comment) |
| 4 | `ValidateSession()` | No | Yes (related concept) |

**Hybrid finds all of these** and ranks them by combined relevance.

---

## Step 4: Diagnosing Missing Results

### Problem: Expected result not appearing

**Step 1: Verify the file is indexed**

```bash
amanmcp status
```

Check the file count. If your file is excluded:

```bash
cat .amanmcp/config.yaml
# Check exclude patterns
```

**Step 2: Check if the chunk exists**

Large functions might be split across chunks. Try searching for a unique identifier:

```bash
amanmcp search_code "func ExactFunctionName"
```

**Step 3: Try alternative terms**

Your code might use different vocabulary:

```bash
# Instead of "user"
amanmcp search "customer"
amanmcp search "account"
amanmcp search "principal"
```

---

## Step 5: Understanding Chunk Boundaries

AmanMCP chunks code at semantic boundaries using tree-sitter:

```
┌─────────────────────────────────────────────────────────────┐
│  Original File                │  How It's Chunked           │
├───────────────────────────────┼─────────────────────────────┤
│  package main                 │  Chunk 1:                   │
│                               │  func main() { ... }        │
│  func main() {                │                             │
│      // ...                   │  Chunk 2:                   │
│  }                            │  func helper() { ... }      │
│                               │                             │
│  func helper() {              │  Chunk 3:                   │
│      // ...                   │  type Config struct { ... } │
│  }                            │                             │
│                               │                             │
│  type Config struct {         │                             │
│      // ...                   │                             │
│  }                            │                             │
└───────────────────────────────┴─────────────────────────────┘
```

**Why this matters:**
- Each chunk is searched independently
- A query about `main()` won't include `helper()` in results (unless both match)
- Very long functions might be split

---

## Step 6: Score Interpretation Guide

### What different scores mean

| Score Range | Interpretation | Action |
|-------------|----------------|--------|
| **0.9 - 1.0** | Excellent match | Likely exactly what you need |
| **0.7 - 0.9** | Good match | Worth reviewing |
| **0.5 - 0.7** | Moderate match | Might be relevant context |
| **0.3 - 0.5** | Weak match | Probably tangential |
| **Below 0.3** | Poor match | Likely not useful |

### When scores seem wrong

**High score but wrong result?**
- Check if it's a comment or string containing your keywords
- The code might be semantically similar but not what you want

**Low score but correct result?**
- Your query terms might differ from the code's vocabulary
- Try synonyms or more specific terms

---

## Step 7: Improving Your Searches

### Technique 1: Add context

```bash
# Instead of:
amanmcp search "validate"

# Try:
amanmcp search "validate user input form"
```

More context helps semantic search understand your intent.

### Technique 2: Use domain language

Use terms that appear in your codebase:

```bash
# If your code uses "Customer" not "User":
amanmcp search "customer authentication"
```

### Technique 3: Search for patterns

```bash
# Find error handling patterns
amanmcp search "if err != nil return"

# Find interface implementations
amanmcp search "implements Reader interface"
```

### Technique 4: Combine specific + general

```bash
# Specific identifier + concept
amanmcp search "PaymentService process stripe"
```

---

## Step 8: Working with Results

### Best practices when reviewing results

1. **Start with the highest-scoring results** - They're most likely relevant
2. **Check the file path** - Is it the right area of the codebase?
3. **Read the preview** - Does it look like what you need?
4. **Follow up with Read** - Get the full context if needed

### Using results with AI

When you get results, you can ask follow-up questions:

```
"I found PaymentService in internal/billing/payment.go.
Can you explain how the retry logic works?"
```

The AI can then read that specific file for detailed answers.

---

## Verification

You've completed this tutorial if you can:

- [ ] Explain what BM25 and Vector scores mean
- [ ] Diagnose why an expected result might be missing
- [ ] Use different query techniques for better results
- [ ] Interpret score ranges correctly

---

## Practice Exercises

### Exercise 1: Compare search types
Search for the same term and observe the score breakdown:
```bash
amanmcp search "config"
```

Note which results have high BM25 vs high Vector scores.

### Exercise 2: Find missing results
Think of a function you know exists. Search for it:
1. By exact name
2. By what it does
3. By related concepts

Compare the results.

### Exercise 3: Improve a vague search
Start with a vague query:
```bash
amanmcp search "data"
```

Refine it until you get useful results.

---

## Summary

| Concept | Key Point |
|---------|-----------|
| **Final Score** | Combined BM25 + Vector relevance |
| **BM25** | Keyword match (exact terms) |
| **Vector** | Semantic match (meaning) |
| **Missing Results** | Check indexing, try synonyms |
| **Better Searches** | Add context, use domain terms |

---

## Next Steps

You now understand AmanMCP search results. Explore further:

- [Search Fundamentals](../learning/search-fundamentals.md) - Deep dive into how search works
- [Configuration](../reference/configuration.md) - Tune search weights
- [Guides](../guides/) - MLX setup, auto-reindexing
