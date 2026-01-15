# Vector Search Concepts

**Version:** 1.0.0
**Last Updated:** 2025-12-28

Learn how vector search enables semantic understanding in AmanMCP.

---

## Overview

Vector search finds documents by meaning, not just keywords. It converts text into high-dimensional vectors (embeddings) and finds similar vectors using mathematical distance.

**Why we use it**: A user searching for "authentication" should find code about "login", "auth", "credentials" even if those exact words aren't in the query.

---

## Core Concepts

### What is an Embedding?

An embedding is a list of numbers (vector) that captures the meaning of text:

```
"authentication" → [0.12, -0.34, 0.56, 0.02, ..., 0.11]  (768 numbers)
"login system"   → [0.11, -0.32, 0.58, 0.03, ..., 0.10]  (similar!)
"weather report" → [0.89, 0.12, -0.45, 0.78, ..., -0.22] (different)
```

Similar meanings → Similar vectors → Close in vector space

### Dimensionality

Modern embeddings use 384-1536 dimensions:

| Model | Dimensions | Quality | Speed |
|-------|------------|---------|-------|
| EmbeddingGemma-300M | 768 | Very Good (code) | Fast |
| all-MiniLM-L6-v2 | 384 | Good | Very Fast |
| Static (hash-based) | 256 | Basic | Instant |

More dimensions = more nuance, but more memory and slower search.

### Vector Space

Imagine a 3D space where each point is a document:

```
        ▲ auth-related
        │    ● login.go
        │  ● auth.go
        │      ● session.go
        │
        │
        │                    ● weather.go
        ├────────────────────────────────▶ time-related
       ╱                    ● calendar.go
      ╱
     ╱
    ▼ data-related
   ● database.go
```

Similar documents cluster together.

---

## How Embeddings Work

### Neural Network Magic

Text → Tokenize → Neural Network → Vector

```
Input: "handle user login"

Tokenization:
["handle", "user", "login"] → [3847, 1029, 8273]

Neural Network (Transformer):
- Attention mechanisms find relationships
- "login" relates to "user"
- Context builds meaning

Output: [0.12, -0.34, 0.56, ...]
```

### The Training Process

Embedding models learn from vast text:

```
Training data:
- "The cat sat on the mat"
- "A dog rested on the rug"
- ... billions of examples

Model learns:
- "cat" and "dog" are similar (both pets)
- "sat" and "rested" are similar (both verbs of position)
- "mat" and "rug" are similar (both floor coverings)
```

### Why 768 Dimensions?

Each dimension captures a different aspect:

- Dimension 1: formality (casual ↔ formal)
- Dimension 2: domain (technical ↔ everyday)
- Dimension 3: sentiment (negative ↔ positive)
- ... (768 such aspects, learned automatically)

---

## Similarity Measures

### Cosine Similarity

Measures the angle between vectors (most common):

```
           A (query)
          ╱│
         ╱ │
        ╱  │
       ╱ θ │
      ╱────┘
     B (document)

similarity = cos(θ)

θ = 0°   → cos = 1.0  (identical)
θ = 90°  → cos = 0.0  (unrelated)
θ = 180° → cos = -1.0 (opposite)
```

Formula:

```
cosine_similarity(A, B) = (A · B) / (|A| × |B|)
```

### Euclidean Distance

Measures straight-line distance:

```
distance = √(Σ(ai - bi)²)
```

Smaller distance = more similar.

### When to Use Which

| Metric | Best For | Notes |
|--------|----------|-------|
| Cosine | Text embeddings | Ignores magnitude |
| Euclidean | When magnitude matters | Sensitive to length |
| Dot Product | Normalized vectors | Fastest |

AmanMCP uses **cosine similarity** (standard for text).

---

## The Search Problem

### Brute Force is Slow

To find nearest neighbors naively:

```
For query Q, find top-10 similar from 100K documents:

1. Compute similarity(Q, doc1)   # 768 multiplications
2. Compute similarity(Q, doc2)   # 768 multiplications
3. ...
4. Compute similarity(Q, doc100000)
5. Sort all 100K scores
6. Return top 10

Total: 76.8 million multiplications + sort
Time: ~100ms per query (too slow)
```

### We Need Approximate Search

Trade accuracy for speed:

- Instead of checking all docs, check a smart subset
- Miss some good results, but much faster
- For 99% of queries, the "best" we find is good enough

---

## HNSW: How AmanMCP Searches

### Hierarchical Navigable Small Worlds

HNSW builds a multi-layer graph for O(log n) search:

```
Layer 2 (sparse):   A ──────────────────── D
                     \                    /
Layer 1 (medium):   A ─── B ────── C ─── D
                     \   / \      / \   /
Layer 0 (dense):    A ─ B ─ E ─ F ─ C ─ D
                        │   │   │   │
                        G   H   I   J
```

### Search Algorithm

```
Query: Find nearest to Q

1. Start at top layer (sparse)
   - Entry point: A
   - Check A's neighbors at this layer
   - Move to nearest (D is closer)

2. Drop to next layer
   - From D, check neighbors: C, D's L1 neighbors
   - C is closest, move there

3. Drop to bottom layer
   - From C, exhaustively check local neighborhood
   - Find I is actually closest

4. Return I
```

### Why It's Fast

| Documents | Brute Force | HNSW |
|-----------|-------------|------|
| 10K | 10,000 checks | ~50 checks |
| 100K | 100,000 checks | ~60 checks |
| 1M | 1,000,000 checks | ~70 checks |

Logarithmic scaling!

---

## coder/hnsw: Our Vector Database

> **Note:** AmanMCP originally used USearch (CGO). Replaced with coder/hnsw in v0.1.38 for simpler distribution (pure Go, no CGO dependency for vectors).

### Why coder/hnsw?

| Feature | coder/hnsw | Alternatives |
|---------|------------|--------------|
| Pure Go | Yes - no CGO | USearch requires CGO |
| Portability | True single binary | Library dependencies |
| Scale | 300K+ vectors | Similar |
| Memory | Efficient | Similar |

### Key Features

1. **Pure Go**: No CGO dependency, simple distribution
2. **HNSW Algorithm**: Same fast approximate nearest neighbor search
3. **Persistence**: Save/load from disk
4. **Incremental**: Add/remove vectors without full rebuild

### In AmanMCP

```go
import "github.com/coder/hnsw"

// Create index
g := hnsw.NewGraph[uint64]()

// Add vectors
g.Add(hnsw.MakeNode(key, embedding))

// Search
neighbors := g.Search(queryEmbedding, 10)
```

---

## Quantization

### The Memory Problem

```
768 dimensions × 4 bytes (float32) = 3,072 bytes per vector
100,000 documents = 307 MB just for vectors
```

### Solution: Use Less Precision

| Type | Bytes/dim | Memory (100K) | Quality |
|------|-----------|---------------|---------|
| F32 | 4 | 307 MB | 100% |
| F16 | 2 | 154 MB | ~99% |
| I8 | 1 | 77 MB | ~95% |

AmanMCP uses **F16** (half precision):

- Half the memory
- Negligible quality loss for code search

---

## Embeddings in AmanMCP

### Hugot Provider (Default)

AmanMCP uses Hugot with EmbeddingGemma for local embeddings:

```go
type HugotEmbedder struct {
    session  *hugot.Session
    pipeline *pipelines.FeatureExtractionPipeline
}

func (h *HugotEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
    result, err := h.pipeline.Run([]string{text})
    if err != nil {
        return nil, fmt.Errorf("embedding failed: %w", err)
    }
    return normalizeVector(result.Embeddings[0]), nil
}
```

### Model Choice: EmbeddingGemma

Why this model:

| Feature | EmbeddingGemma | OpenAI ada-002 |
|---------|----------------|----------------|
| Privacy | 100% local | API call |
| Cost | Free | $0.0001/1K tokens |
| Speed | ~50ms | ~200ms (+ network) |
| Quality | 68% MTEB code | Slightly better |
| Offline | Yes (after download) | No |
| Context | 2048 tokens | 8191 tokens |
| Dimensions | 768 | 1536 |

### Static Fallback

When Hugot model download fails, we use a simple fallback:

```go
func StaticEmbed(text string) []float32 {
    // Simple bag-of-words hashing
    // Not great, but functional
    vec := make([]float32, 768)
    words := tokenize(text)
    for i, word := range words {
        h := hash(word)
        idx := h % 768
        vec[idx] += float32(1.0 / float64(i+1))
    }
    normalize(vec)
    return vec
}
```

---

## Indexing Pipeline

### Document → Vector

```
┌─────────────┐
│ Source File │
└──────┬──────┘
       ▼
┌─────────────┐
│  Chunker    │  Tree-sitter splits into functions/types
└──────┬──────┘
       ▼
┌─────────────┐
│  Embedder   │  Convert each chunk to vector
└──────┬──────┘
       ▼
┌─────────────┐
│ Vector Index│  Store for fast retrieval
└─────────────┘
```

### Batching for Speed

```go
func (i *Indexer) IndexFiles(files []File) error {
    // Chunk all files
    var allChunks []Chunk
    for _, f := range files {
        allChunks = append(allChunks, i.chunker.Chunk(f)...)
    }

    // Batch embed (much faster than one-by-one)
    texts := make([]string, len(allChunks))
    for i, c := range allChunks {
        texts[i] = c.Content
    }
    embeddings := i.embedder.EmbedBatch(texts)

    // Add to index
    for i, emb := range embeddings {
        i.vectorIndex.Add(allChunks[i].ID, emb)
    }

    return nil
}
```

---

## Search Flow

### Query → Results

```
Query: "authentication middleware"
       │
       ▼
┌──────────────┐
│ Embed Query  │  → [0.12, -0.34, ...]
└──────┬───────┘
       ▼
┌──────────────┐
│ HNSW Search  │  → Find 20 nearest neighbors
└──────┬───────┘
       ▼
┌──────────────┐
│   Rerank     │  → Optional: reorder by exact similarity
└──────┬───────┘
       ▼
┌──────────────┐
│   Return     │  → Top 10 chunks with scores
└──────────────┘
```

### Code Example

```go
func (e *VectorEngine) Search(query string, limit int) ([]Result, error) {
    // 1. Embed query
    queryVec, err := e.embedder.Embed(query)
    if err != nil {
        return nil, fmt.Errorf("embed query: %w", err)
    }

    // 2. Search index
    keys, distances, err := e.index.Search(queryVec, limit*2)
    if err != nil {
        return nil, fmt.Errorf("search index: %w", err)
    }

    // 3. Convert to results
    results := make([]Result, len(keys))
    for i, key := range keys {
        chunk := e.getChunk(key)
        results[i] = Result{
            Chunk: chunk,
            Score: 1 - distances[i],  // Convert distance to similarity
        }
    }

    return results[:limit], nil
}
```

---

## Common Mistakes

### 1. Not Normalizing Vectors

```go
// BAD: Raw vectors
index.Add(key, embedding)

// GOOD: Normalize for cosine similarity
normalize(embedding)
index.Add(key, embedding)
```

### 2. Wrong Chunk Size

```
Too small (10 tokens):
  "func main" → Not enough context

Too large (5000 tokens):
  Entire file → Too diluted, hard to match

Just right (200-500 tokens):
  Complete function → Good semantic unit
```

### 3. Ignoring Empty Results

```go
// BAD: Assume results exist
results, _ := engine.Search(query, 10)
fmt.Println(results[0])  // Panic if empty

// GOOD: Check length
if len(results) == 0 {
    return nil, ErrNoResults
}
```

---

## Performance Tips

### 1. Pre-compute and Cache

```go
// Cache frequently used embeddings
type EmbedCache struct {
    cache map[string][]float32
    mu    sync.RWMutex
}

func (c *EmbedCache) Get(text string) ([]float32, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    v, ok := c.cache[text]
    return v, ok
}
```

### 2. Batch Embedding

```go
// BAD: One at a time (slow)
for _, text := range texts {
    emb := embedder.Embed(text)  // HTTP call each time
}

// GOOD: Batch (fast)
embeddings := embedder.EmbedBatch(texts)  // One HTTP call
```

### 3. Use Appropriate Precision

```go
// coder/hnsw uses float32 vectors
// For memory optimization, consider dimensionality reduction
// or using smaller embedding models

// AmanMCP uses float32 with 768-dimensional vectors
type Vector = []float32
```

---

## Further Reading

- [Sentence Transformers](https://www.sbert.net/) - Embedding models
- [HNSW Paper](https://arxiv.org/abs/1603.09320) - Original algorithm
- [coder/hnsw](https://github.com/coder/hnsw) - Pure Go HNSW implementation
- [What are Word Embeddings?](https://jalammar.github.io/illustrated-word2vec/)

---

*Vector search finds meaning, not just words. Use it to understand intent.*
