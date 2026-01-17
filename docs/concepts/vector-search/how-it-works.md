# Vector Search: How It Works

A practical explanation of embeddings, similarity, and how AmanMCP searches by meaning.

**Reading time:** 10 minutes
**Audience:** Users who want to understand the search process
**Prerequisites:** [Overview](overview.md)

---

## Quick Summary

- Embeddings convert text to numbers that capture meaning
- Similar texts have similar numbers (close in "vector space")
- We measure closeness using cosine similarity
- HNSW makes searching millions of vectors fast

---

## How Text Becomes Numbers

### The Embedding Process

When AmanMCP indexes your code, each chunk goes through this:

```
┌─────────────────────────────────────────────────────────────┐
│  1. Code Chunk                                              │
│     "func ValidateToken(token string) (*Claims, error)"     │
└─────────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  2. Tokenize                                                │
│     Split into pieces: ["func", "Validate", "Token", ...]   │
└─────────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  3. Neural Network                                          │
│     Process through transformer model                       │
│     (like a mini version of GPT)                            │
└─────────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  4. Embedding                                               │
│     [0.12, -0.34, 0.56, 0.02, ..., 0.11]                    │
│     (768 numbers that capture the meaning)                  │
└─────────────────────────────────────────────────────────────┘
```

The same process happens to your query when you search.

### What the Numbers Mean

Each number in the embedding captures some aspect of meaning:

```
Position 1: How technical is this? (technical ↔ everyday)
Position 2: Is it about data? (data-related ↔ not data-related)
Position 3: Is it about security? (security ↔ other)
...
Position 768: Some other learned aspect
```

**Important:** These aren't hand-designed. The model learns them automatically by reading billions of text examples.

### Example: Similar Texts, Similar Numbers

```
"authentication"     → [0.82, 0.31, 0.91, -0.12, ...]
"login validation"   → [0.80, 0.33, 0.89, -0.10, ...]  ← Very similar!
"verify credentials" → [0.78, 0.35, 0.85, -0.14, ...]  ← Also similar!
"weather forecast"   → [0.11, 0.88, 0.02, 0.67, ...]   ← Very different

Notice: Auth-related texts have similar numbers
        Weather text has completely different numbers
```

---

## How Similarity Is Measured

### The Intuition

Imagine two arrows pointing from the origin:

```
                    A (your query)
                   ╱
                  ╱
                 ╱  θ (angle)
                ╱───────
               ╱        B (a document)
              ╱
             ╱

Small angle (θ) = Similar meaning
Large angle (θ) = Different meaning
```

### Cosine Similarity

We measure the cosine of the angle between vectors:

| Angle | Cosine | Meaning |
|-------|--------|---------|
| 0° | 1.0 | Identical meaning |
| 45° | 0.71 | Related |
| 90° | 0.0 | Unrelated |

**In practice for text:** Scores typically range from 0.3 to 0.95:
- 0.9+ = Very similar
- 0.7-0.9 = Related
- 0.5-0.7 = Somewhat related
- Below 0.5 = Probably not relevant

### Example Search

```
Query: "user login"
Query embedding: [0.75, 0.40, 0.82, ...]

Comparing to indexed chunks:

auth.go:         [0.73, 0.42, 0.80, ...] → Similarity: 0.94 ✓
session.go:      [0.68, 0.45, 0.75, ...] → Similarity: 0.87 ✓
database.go:     [0.15, 0.82, 0.10, ...] → Similarity: 0.31 ✗
payment.go:      [0.22, 0.55, 0.18, ...] → Similarity: 0.42 ✗

Results: auth.go, session.go (sorted by similarity)
```

---

## Why We Need Fast Search

### The Problem

With 100,000 code chunks:
- Each has 768-dimensional embedding
- To find the top 10 matches, compare query to all 100K
- That's 76.8 million multiplications per search
- Takes ~100ms (too slow!)

### The Solution: Don't Check Everything

Instead of comparing to every document, use a smart data structure that finds "probably the best" matches quickly.

**Trade-off:**
- Brute force: 100% accurate, slow
- Smart search (HNSW): 95-99% accurate, fast

For code search, "95% accurate" means you might miss the 47th best result. That's fine - you only look at the top 10 anyway.

---

## HNSW: Fast Approximate Search

HNSW (Hierarchical Navigable Small World) is how we search quickly.

### The Intuition: A Multi-Level Map

Imagine finding a coffee shop in a new city:

1. **Country level:** "It's in the downtown area"
2. **District level:** "It's in the financial district"
3. **Street level:** "It's on Main Street between 5th and 6th"

You don't check every building in the country - you zoom in through levels.

HNSW works the same way with vectors:

```
Layer 2 (few nodes, long jumps):
  A ─────────────────────── D

Layer 1 (more nodes, medium jumps):
  A ────── B ────── C ────── D

Layer 0 (all nodes, short jumps):
  A─B─E─F─G─H─I─J─K─L─M─N─C─D
```

### How Search Works

```
Query: Find closest to Q

1. Start at Layer 2, node A
   → Check neighbors, D is closer to Q
   → Move to D

2. Drop to Layer 1 at D
   → Check neighbors (B, C)
   → C is closest to Q
   → Move to C

3. Drop to Layer 0 at C
   → Check all neighbors exhaustively
   → Find that node K is closest

4. Return K (and its nearby neighbors)
```

### Performance Comparison

| Documents | Brute Force | HNSW |
|-----------|-------------|------|
| 10,000 | 10,000 comparisons | ~50 comparisons |
| 100,000 | 100,000 comparisons | ~60 comparisons |
| 1,000,000 | 1,000,000 comparisons | ~70 comparisons |

**HNSW scales logarithmically** - doubling your codebase adds barely any search time.

---

## The Complete Search Flow

### Index Time (Once)

```
┌─────────────────────────────────────────────────────────────┐
│  For each code file:                                        │
│    1. Parse with tree-sitter (split into functions/types)   │
│    2. For each chunk:                                       │
│       a. Generate embedding                                 │
│       b. Add to HNSW index                                  │
│       c. Store metadata (file path, line numbers)           │
└─────────────────────────────────────────────────────────────┘
```

### Query Time (Every Search)

```
┌─────────────────────────────────────────────────────────────┐
│  1. Query: "authentication flow"                            │
└─────────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  2. Generate query embedding                                │
│     [0.75, 0.40, 0.82, ...]                                 │
└─────────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  3. HNSW Search                                             │
│     Navigate graph to find nearest neighbors                │
│     (20 candidates retrieved)                               │
└─────────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  4. Re-rank by exact similarity                             │
│     Compute precise cosine similarity for candidates        │
└─────────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  5. Return top 10 with scores                               │
│     auth.go:23-45 (0.94)                                    │
│     middleware.go:12-38 (0.91)                              │
│     ...                                                     │
└─────────────────────────────────────────────────────────────┘
```

---

## Embedding Models

### What AmanMCP Uses

| Model | Dimensions | Quality | Speed |
|-------|------------|---------|-------|
| **nomic-embed-text** (default) | 768 | Very good | Fast |
| MLX embeddings | 768 | Very good | Faster on Apple Silicon |
| Static fallback | 256 | Basic | Instant |

### Why Local Matters

- **Privacy:** Your code never leaves your machine
- **Speed:** No network latency
- **Cost:** No API charges
- **Offline:** Works without internet

The embedding model runs via Ollama (or MLX on Apple Silicon), entirely on your computer.

---

## Practical Implications

### What Makes Good Embeddings

| Content Type | Embedding Quality | Why |
|--------------|-------------------|-----|
| Function with good name | Excellent | Name captures intent |
| Well-commented code | Excellent | Comments add context |
| Single-letter variables | Poor | No semantic meaning |
| Minified code | Poor | Structure lost |

### Tips for Better Results

1. **Use descriptive names:** `validateUserToken` embeds better than `vut`
2. **Comments help:** They add semantic signal
3. **Keep chunks coherent:** Complete functions embed better than fragments

---

## Next Steps

| Want to... | Read |
|------------|------|
| Learn HNSW algorithm details | [Advanced](advanced.md) |
| See how vector + keyword combine | [Hybrid Search](../hybrid-search/) |
| Understand scores | [Understanding Results](../../tutorials/understanding-results.md) |

---

*Vector search finds meaning, not just words. The embedding is the magic that makes it work.*
