# Vector Search: Advanced

Deep dive into HNSW algorithms, quantization, and implementation details.

**Reading time:** 15 minutes
**Audience:** Developers who want to understand or extend the system
**Prerequisites:** [How It Works](how-it-works.md)

---

## Quick Summary

- HNSW uses skip-list-inspired hierarchical graphs for O(log N) search
- Parameter tuning trades memory/build time for search accuracy
- Quantization reduces memory by 4-8x with minimal accuracy loss
- AmanMCP uses `coder/hnsw` - pure Go, no CGO required

---

## HNSW Algorithm Deep Dive

### The Skip List Inspiration

HNSW (Hierarchical Navigable Small World) is inspired by skip lists:

```
Skip List (1D):
Level 2:  1 ─────────────────────────── 9
Level 1:  1 ─────── 4 ─────── 7 ─────── 9
Level 0:  1 ─ 2 ─ 3 ─ 4 ─ 5 ─ 6 ─ 7 ─ 8 ─ 9

To find 6:
1. Start at Level 2, node 1
2. Jump to 9? No, too far
3. Drop to Level 1
4. Jump to 4, then 7? No, 7 > 6
5. Drop to Level 0
6. Walk: 4 → 5 → 6 ✓
```

HNSW extends this to multi-dimensional space where "neighbors" are defined by vector similarity, not numeric order.

### Graph Structure

```
Layer 2 (sparse):
     A ←───────────────────────→ D
     │                           │
     └───────────────────────────┘
     Few nodes, long-range connections

Layer 1 (medium):
     A ←────→ B ←────→ C ←────→ D
     │        │        │        │
     └────────┴────────┴────────┘
     More nodes, medium connections

Layer 0 (dense):
     A─B─E─F─G─H─I─J─K─L─M─N─C─D
     └─┬─┴───┴─┬─┴───┴─┬─┴───┴─┘
       └───────┴───────┴───────┘
     All nodes, local + some long-range connections
```

### Layer Assignment

When inserting a new vector, its maximum layer is chosen probabilistically:

```
P(layer = l) = (1/M)^l

With M = 16 (default):
- 93.75% of nodes: Layer 0 only
- 5.86% of nodes: Layer 0 + Layer 1
- 0.37% of nodes: Layer 0 + Layer 1 + Layer 2
- ...
```

This creates a natural hierarchy where few nodes appear in upper layers (for fast traversal) and all nodes appear in Layer 0 (for accurate results).

### The Search Algorithm

```
function HNSW_Search(query, K, ef):
    # Start from entry point at top layer
    current = entry_point

    # Greedy search through upper layers (layer L down to 1)
    for layer in range(L, 0, -1):
        current = greedy_search(query, current, layer)

    # Detailed search at layer 0
    candidates = search_layer_0(query, current, ef)

    # Return K nearest from candidates
    return top_K(candidates, K)

function greedy_search(query, start, layer):
    current = start
    while True:
        # Check all neighbors at this layer
        neighbors = get_neighbors(current, layer)
        closest = argmin(distance(query, n) for n in neighbors)

        if distance(query, closest) >= distance(query, current):
            break  # No improvement, stop
        current = closest

    return current

function search_layer_0(query, entry, ef):
    candidates = min_heap([entry])  # To explore
    results = max_heap([entry])     # Best found so far
    visited = set([entry])

    while candidates not empty:
        current = candidates.pop_min()
        furthest_result = results.peek_max()

        # Pruning: stop if current is further than our worst result
        if distance(query, current) > distance(query, furthest_result):
            break

        for neighbor in get_neighbors(current, layer=0):
            if neighbor not in visited:
                visited.add(neighbor)

                if distance(query, neighbor) < distance(query, furthest_result) or len(results) < ef:
                    candidates.push(neighbor)
                    results.push(neighbor)

                    if len(results) > ef:
                        results.pop_max()  # Keep only ef best

    return results
```

### The Build Algorithm

```
function HNSW_Insert(new_vector):
    # Determine layer for this vector
    max_layer = floor(-ln(random()) * mL)  # mL = 1/ln(M)

    # If new vector goes higher than current max, update entry point
    if max_layer > current_max_layer:
        entry_point = new_vector
        current_max_layer = max_layer

    # Find entry point by descending from top
    current = entry_point
    for layer in range(current_max_layer, max_layer, -1):
        current = greedy_search(new_vector, current, layer)

    # Insert into each layer from max_layer down to 0
    for layer in range(max_layer, -1, -1):
        neighbors = search_layer(new_vector, current, layer, efConstruction)
        selected = select_neighbors(new_vector, neighbors, M)

        # Add bidirectional connections
        for neighbor in selected:
            add_edge(new_vector, neighbor, layer)
            add_edge(neighbor, new_vector, layer)

            # Prune if neighbor has too many connections
            if count_neighbors(neighbor, layer) > M_max:
                prune_connections(neighbor, layer, M_max)
```

---

## Key Parameters

### M (Max Connections per Node)

Controls how many neighbors each node connects to.

| M Value | Memory | Build Time | Search Quality | Use Case |
|---------|--------|------------|----------------|----------|
| 8 | Low | Fast | Lower | Small datasets |
| **16** | **Medium** | **Medium** | **Good** | **Default, most cases** |
| 32 | High | Slow | Higher | When accuracy critical |
| 64 | Very High | Very Slow | Highest | Maximum quality |

**Rule of thumb:** M = 16 works well for 100K-1M vectors.

### efConstruction (Build-time Beam Width)

How many candidates to consider when building connections.

| efConstruction | Build Time | Index Quality |
|----------------|------------|---------------|
| 50 | Fast | Lower |
| **100** | **Medium** | **Good** |
| 200 | Slow | Higher |
| 400 | Very Slow | Highest |

**Rule:** Higher efConstruction = better graph quality = better search results.

### efSearch (Query-time Beam Width)

How many candidates to consider when searching.

| efSearch | Search Time | Recall |
|----------|-------------|--------|
| 10 | 1ms | ~85% |
| **50** | **5ms** | **~95%** |
| 100 | 10ms | ~98% |
| 200 | 20ms | ~99% |

**Rule:** efSearch >= K (number of results you want).

### Parameter Relationships

```
┌─────────────────────────────────────────────────────┐
│                   BUILD TIME                         │
│                       ↑                              │
│                       │                              │
│    High M, High efConstruction = Slow but Better    │
│                       │                              │
│    Low M, Low efConstruction = Fast but Worse       │
│                       │                              │
└───────────────────────┼─────────────────────────────┘
                        │
                        ↓
┌─────────────────────────────────────────────────────┐
│                   SEARCH TIME                        │
│                       ↑                              │
│                       │                              │
│    High efSearch = Slow but More Accurate           │
│                       │                              │
│    Low efSearch = Fast but Less Accurate            │
│                       │                              │
└─────────────────────────────────────────────────────┘
```

---

## Distance Metrics

### Cosine Similarity vs Distance

```
Cosine Similarity = (A · B) / (||A|| × ||B||)

Range: -1 to 1
  1 = identical direction
  0 = perpendicular
 -1 = opposite direction

For normalized vectors: Cosine Distance = 1 - Cosine Similarity
```

### Why Normalize Vectors?

Pre-normalizing vectors converts cosine similarity to dot product:

```
If ||A|| = ||B|| = 1:
    Cosine Similarity = A · B  (just dot product!)

Benefits:
- Dot product is faster (no division, no sqrt)
- Same ranking as cosine similarity
- AmanMCP normalizes all vectors at index time
```

### Implementation

```go
// Cosine similarity for normalized vectors (fast)
func cosineSimilarityNormalized(a, b []float32) float32 {
    var sum float32
    for i := range a {
        sum += a[i] * b[i]
    }
    return sum  // Already cosine sim because vectors are normalized
}

// Full cosine similarity (slower, for non-normalized)
func cosineSimilarity(a, b []float32) float32 {
    var dot, normA, normB float32
    for i := range a {
        dot += a[i] * b[i]
        normA += a[i] * a[i]
        normB += b[i] * b[i]
    }
    return dot / (sqrt(normA) * sqrt(normB))
}
```

---

## Quantization

Quantization reduces memory by storing vectors in fewer bits.

### Scalar Quantization

Convert float32 to int8 (4x memory reduction):

```
Original: [0.234, -0.567, 0.891, ...]  (32 bits each)
Quantized: [30, -73, 114, ...]          (8 bits each)

Formula:
quantized = round((value - min) / (max - min) * 255) - 128
```

**Trade-off:**
- 4x less memory
- ~1-3% recall loss
- Slightly faster distance calculations

### Product Quantization (PQ)

Split vector into subvectors, quantize each to a codebook ID:

```
768-dim vector → 96 subvectors of 8 dims each
Each subvector → 1 byte codebook ID

Memory: 768 × 4 bytes = 3072 bytes → 96 bytes (32x reduction!)
```

**Trade-off:**
- Massive memory savings
- More recall loss (~5-10%)
- Complex implementation
- Good for billion-scale datasets

### AmanMCP's Approach

AmanMCP uses full float32 vectors because:
1. Codebase size is typically < 1M vectors
2. Memory fits comfortably (768 dims × 4 bytes × 100K = 307 MB)
3. Maximum accuracy for code search

Quantization would be added if targeting very large monorepos.

---

## Go Implementation Details

### The HNSW Library

AmanMCP uses `github.com/coder/hnsw`:

```go
import "github.com/coder/hnsw"

// Create index with cosine similarity
index := hnsw.NewGraph[uint64]()
index.M = 16
index.EfConstruction = 100

// Insert vectors
for id, vector := range vectors {
    index.Add(hnsw.MakeNode(id, vector))
}

// Search
results := index.Search(queryVector, k, efSearch)
```

### Why Pure Go?

- No CGO = simpler builds, better cross-compilation
- No external dependencies (unlike FAISS, Annoy)
- Performance is competitive (within 2x of C++ implementations)
- Easy to embed in Go applications

### Memory Layout

```go
type Node struct {
    ID       uint64
    Vector   []float32  // The embedding
    Layers   [][]uint64 // Neighbor IDs at each layer
}

// Memory per node (768 dims, M=16, avg 1.07 layers):
// - ID: 8 bytes
// - Vector: 768 × 4 = 3072 bytes
// - Layers: ~16 × 8 × 1.07 = ~137 bytes
// Total: ~3.2 KB per node

// 100K nodes ≈ 320 MB
```

### Concurrent Operations

```go
// AmanMCP's thread-safe wrapper
type VectorStore struct {
    index *hnsw.Graph[uint64]
    mu    sync.RWMutex
}

func (v *VectorStore) Search(query []float32, k int) []Result {
    v.mu.RLock()
    defer v.mu.RUnlock()
    return v.index.Search(query, k, efSearch)
}

func (v *VectorStore) Add(id uint64, vector []float32) {
    v.mu.Lock()
    defer v.mu.Unlock()
    v.index.Add(hnsw.MakeNode(id, vector))
}
```

---

## Performance Characteristics

### Time Complexity

| Operation | Complexity | Typical Time (100K vectors) |
|-----------|------------|----------------------------|
| Insert | O(log N × M × efConstruction) | 5-10ms |
| Search | O(log N × M × efSearch) | 5-15ms |
| Delete | O(M × log N) | 1-5ms |

### Space Complexity

```
Memory = N × (D × 4 + M × 8 × avg_layers)

Where:
- N = number of vectors
- D = dimensions (768)
- M = max connections (16)
- avg_layers ≈ 1.07 for M=16

Example (100K vectors, 768 dims, M=16):
= 100,000 × (768 × 4 + 16 × 8 × 1.07)
= 100,000 × (3072 + 137)
≈ 320 MB
```

### Recall vs Speed Trade-off

```
efSearch:  10    50    100   200   500
Recall:    85%   95%   98%   99%   99.5%
Time:      1ms   5ms   10ms  20ms  50ms

The curve flattens - diminishing returns above ef=100
```

---

## Tuning for Code Search

### AmanMCP's Default Settings

```yaml
# Optimized for codebases 10K-500K chunks
vector:
  dimensions: 768
  m: 16
  ef_construction: 100
  ef_search: 50
```

### When to Adjust

| Scenario | Adjustment |
|----------|------------|
| Small codebase (< 10K chunks) | Lower M=12, efConstruction=50 |
| Large monorepo (> 500K chunks) | Higher M=24, efConstruction=200 |
| Need faster queries | Lower efSearch=30 |
| Need higher accuracy | Higher efSearch=100+ |

### Monitoring Quality

```go
// Calculate recall@k against brute force
func measureRecall(index *VectorStore, testQueries [][]float32, k int) float64 {
    var totalRecall float64

    for _, query := range testQueries {
        hnswResults := index.Search(query, k)
        bruteResults := bruteForceSearch(query, k)

        overlap := countOverlap(hnswResults, bruteResults)
        totalRecall += float64(overlap) / float64(k)
    }

    return totalRecall / float64(len(testQueries))
}
```

---

## Common Issues and Solutions

### Issue: Low Recall

**Symptoms:** Relevant results not appearing in top-K

**Solutions:**
1. Increase efSearch (easiest)
2. Increase efConstruction and rebuild
3. Increase M (most memory-expensive)

### Issue: Slow Searches

**Symptoms:** Queries taking > 50ms

**Solutions:**
1. Decrease efSearch
2. Pre-filter before vector search
3. Use quantization (if memory-bound)

### Issue: High Memory Usage

**Symptoms:** Index consuming > 1GB for 100K vectors

**Solutions:**
1. Verify dimensions (should be 768, not 1536)
2. Implement scalar quantization
3. Use disk-based index for very large datasets

---

## Further Reading

### Papers

- [Efficient and Robust Approximate Nearest Neighbor Search Using HNSW Graphs](https://arxiv.org/abs/1603.09320) - Original HNSW paper
- [Billion-Scale Similarity Search with GPUs](https://arxiv.org/abs/1702.08734) - FAISS paper

### Implementations

- [coder/hnsw](https://github.com/coder/hnsw) - Pure Go (used by AmanMCP)
- [nmslib/hnswlib](https://github.com/nmslib/hnswlib) - C++ reference implementation
- [FAISS](https://github.com/facebookresearch/faiss) - Facebook's similarity search library

---

## Next Steps

| Want to... | Read |
|------------|------|
| Understand hybrid search | [Hybrid Search](../hybrid-search/) |
| See practical usage | [Your First Search](../../tutorials/your-first-search.md) |
| Learn about embeddings | [Search Fundamentals](../../learning/search-fundamentals.md) |

---

*HNSW makes searching millions of vectors feel instant. The math is elegant, the implementation is practical.*
