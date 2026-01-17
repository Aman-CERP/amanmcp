# Caching & Performance

Why AmanMCP is fast: what's cached, what's computed, and how it all fits together.

**Reading time:** 7 minutes
**Audience:** Users curious about performance characteristics
**Prerequisites:** [Indexing Pipeline](indexing-pipeline.md)

---

## Quick Summary

- **Embeddings cached** at index time (expensive computation done once)
- **Indexes in memory** for fast search (HNSW, BM25)
- **Query embedding** is the only per-search cost
- **Incremental updates** avoid re-processing unchanged code

---

## Performance Targets

| Metric | Target | Typical |
|--------|--------|---------|
| Search latency | < 100ms | 15-50ms |
| Memory usage | < 300MB | 100-200MB |
| Startup time | < 2s | 0.5-1s |
| Index update | < 1s per file | 50-200ms |

---

## Where Time Goes

### Search Breakdown

```mermaid
pie title "Search Time Distribution (Typical Query)"
    "Query Embedding" : 15
    "Vector Search" : 10
    "BM25 Search" : 5
    "RRF Fusion" : 2
    "Result Formatting" : 3
```

| Component | Time | Notes |
|-----------|------|-------|
| Query embedding | ~15ms | The biggest cost |
| Vector search (HNSW) | ~10ms | Very fast (logarithmic) |
| BM25 search | ~5ms | SQLite FTS5 |
| RRF fusion | ~2ms | In-memory ranking |
| Result formatting | ~3ms | Build response |
| **Total** | **~35ms** | Well under 100ms target |

### Why Query Embedding Dominates

```mermaid
flowchart LR
    subgraph Cached["Cached at Index Time"]
        DocEmbed["Document embeddings<br/>Done once per chunk"]
        HNSW["HNSW graph<br/>Pre-built"]
        BM25["BM25 tokens<br/>Pre-indexed"]
    end

    subgraph PerQuery["Per Query"]
        QueryEmbed["Query embedding<br/>Must compute every time"]
    end

    subgraph Fast["Very Fast"]
        Search["Search indexes<br/>Milliseconds"]
    end

    QueryEmbed --> Search
    Cached --> Search

    style Cached fill:#c8e6c9,stroke:#2e7d32
    style PerQuery fill:#fff9c4,stroke:#f57f17
    style Fast fill:#e1f5ff,stroke:#1565c0
```

---

## What's Cached

### 1. Document Embeddings

**When:** At index time
**What:** 768-float vector for each chunk
**Why:** Embedding is expensive (~20ms per chunk)

```mermaid
flowchart TB
    subgraph IndexTime["Index Time (Once)"]
        Chunk["Code Chunk"]
        Embed["Generate Embedding<br/>~20ms"]
        Store["Store in HNSW"]
    end

    subgraph SearchTime["Search Time (Every Query)"]
        Load["Load from disk<br/>~0ms (already in memory)"]
        Compare["Compare vectors<br/>~0.01ms per comparison"]
    end

    Chunk --> Embed --> Store
    Store -.->|"Pre-computed"| Load --> Compare

    style IndexTime fill:#fff9c4
    style SearchTime fill:#c8e6c9
```

**Cost without caching:** 20,000 chunks × 20ms = 400 seconds per search!
**Cost with caching:** 0ms (vectors already computed)

### 2. HNSW Index

**When:** At index time
**What:** Navigable graph connecting similar vectors
**Why:** Makes search O(log N) instead of O(N)

```mermaid
flowchart LR
    subgraph Naive["Without HNSW (Brute Force)"]
        N1["Compare to chunk 1"]
        N2["Compare to chunk 2"]
        N3["Compare to chunk 3"]
        Ndots["..."]
        NN["Compare to chunk N"]

        N1 --> N2 --> N3 --> Ndots --> NN
    end

    subgraph HNSW["With HNSW (Graph Search)"]
        H1["Start at entry point"]
        H2["Jump to better neighbor"]
        H3["Jump to better neighbor"]
        H4["Found best!"]

        H1 --> H2 --> H3 --> H4
    end

    style Naive fill:#ffcdd2
    style HNSW fill:#c8e6c9
```

| Chunks | Brute Force | HNSW |
|--------|-------------|------|
| 1,000 | 1,000 comparisons | ~10 comparisons |
| 10,000 | 10,000 comparisons | ~15 comparisons |
| 100,000 | 100,000 comparisons | ~20 comparisons |

### 3. BM25 Index (SQLite FTS5)

**When:** At index time
**What:** Inverted index of tokens → documents
**Why:** Instant keyword lookup

```mermaid
flowchart TB
    subgraph Index["BM25 Inverted Index"]
        Token1["'authenticate' → [doc1, doc5, doc23]"]
        Token2["'validate' → [doc2, doc5, doc89]"]
        Token3["'token' → [doc5, doc23, doc45]"]
    end

    subgraph Query["Query: 'validate token'"]
        Lookup1["Find 'validate' docs"]
        Lookup2["Find 'token' docs"]
        Intersect["Intersect + rank"]
    end

    Index --> Lookup1
    Index --> Lookup2
    Lookup1 --> Intersect
    Lookup2 --> Intersect
    Intersect --> Results["[doc5, doc45, ...]"]

    style Index fill:#fff9c4
    style Query fill:#e1f5ff
    style Results fill:#c8e6c9
```

---

## Memory Architecture

### What Lives Where

```mermaid
flowchart TB
    subgraph Memory["In Memory (Fast Access)"]
        HNSW["HNSW Graph<br/>~3KB per chunk"]
        BM25Cache["BM25 Hot Data<br/>~100 bytes per chunk"]
        MetaCache["Metadata Cache<br/>~200 bytes per chunk"]
    end

    subgraph Disk["On Disk (Persistent)"]
        HNSWFile["vectors.hnsw"]
        SQLite["bm25.db / metadata.db"]
    end

    subgraph OnDemand["Loaded On Demand"]
        ChunkContent["Chunk full text<br/>Only for results"]
    end

    Memory <-->|"Memory-mapped"| Disk
    Disk --> OnDemand

    style Memory fill:#c8e6c9,stroke:#2e7d32
    style Disk fill:#e1f5ff,stroke:#1565c0
    style OnDemand fill:#fff9c4,stroke:#f57f17
```

### Memory Calculation

For a 100,000 chunk index:

| Component | Per Chunk | Total |
|-----------|-----------|-------|
| HNSW vectors (768 × 4 bytes) | 3 KB | 300 MB |
| HNSW graph structure | 200 bytes | 20 MB |
| Metadata cache | 200 bytes | 20 MB |
| **Total** | ~3.4 KB | **~340 MB** |

Most codebases have 10K-50K chunks, so typical usage is 100-200 MB.

---

## Incremental Updates

### The Problem

Re-indexing everything when one file changes would be slow:
- 10,000 files × 5ms parse = 50 seconds
- 50,000 chunks × 20ms embed = 1000 seconds

### The Solution

Only process what changed:

```mermaid
flowchart TB
    Change["File Changed:<br/>utils.go"]

    Change --> Hash["Compare file hash"]

    Hash -->|Same| Skip["Skip: no changes"]
    Hash -->|Different| Process["Process changes"]

    Process --> OldChunks["Find old chunks from this file"]
    Process --> NewChunks["Extract new chunks"]

    OldChunks --> Remove["Remove from indexes"]
    NewChunks --> Add["Add to indexes"]

    subgraph Result["Result"]
        Update["Index updated<br/>~200ms instead of 1000s"]
    end

    Remove --> Update
    Add --> Update

    style Change fill:#fff9c4
    style Skip fill:#c8e6c9
    style Result fill:#c8e6c9
```

### What Gets Reused

| Scenario | Reused | Re-computed |
|----------|--------|-------------|
| File unchanged | Everything | Nothing |
| One function changed | Other files, other functions in same file | Just that function |
| New file added | All existing | Just new file |
| File deleted | All remaining | Nothing |

---

## Startup Optimization

### Cold Start

First launch loads indexes from disk:

```mermaid
sequenceDiagram
    participant User
    participant AmanMCP
    participant Disk

    User->>AmanMCP: Start
    AmanMCP->>Disk: Load HNSW index
    Note over AmanMCP,Disk: ~200ms for 50K chunks
    AmanMCP->>Disk: Load BM25 index
    Note over AmanMCP,Disk: ~100ms (SQLite)
    AmanMCP->>Disk: Load metadata
    Note over AmanMCP,Disk: ~50ms

    AmanMCP->>User: Ready! (~350ms total)
```

### Warm Queries

After startup, searches are memory-only:

```mermaid
sequenceDiagram
    participant User
    participant AmanMCP
    participant Memory

    User->>AmanMCP: Search "authentication"

    AmanMCP->>Memory: Embed query (~15ms)
    AmanMCP->>Memory: HNSW search (~10ms)
    AmanMCP->>Memory: BM25 search (~5ms)
    AmanMCP->>Memory: Fuse results (~2ms)

    AmanMCP->>User: Results (~32ms total)
```

---

## Embedding Provider Performance

### Provider Comparison

```mermaid
xychart-beta
    title "Embedding Speed by Provider"
    x-axis ["Ollama (CPU)", "Ollama (GPU)", "MLX (Apple Silicon)", "Static"]
    y-axis "Chunks per Second" 0 --> 500
    bar [50, 150, 200, 10000]
```

| Provider | Speed | Quality | Best For |
|----------|-------|---------|----------|
| Ollama (CPU) | 50/sec | High | Cross-platform default |
| Ollama (GPU) | 150/sec | High | NVIDIA systems |
| MLX | 200/sec | High | Apple Silicon |
| Static | 10,000/sec | Medium | Offline, instant |

### When Speed Matters

```mermaid
flowchart LR
    subgraph Indexing["Initial Indexing"]
        Slow["Speed matters a lot<br/>50K chunks = 4-17 minutes"]
    end

    subgraph Search["Search Queries"]
        Fast["Speed matters less<br/>Only 1 embedding per query"]
    end

    style Indexing fill:#fff9c4
    style Search fill:#c8e6c9
```

---

## Performance Tuning

### Quick Wins

| Setting | Default | For Speed | For Quality |
|---------|---------|-----------|-------------|
| Embedding batch size | 32 | 64 | 16 |
| HNSW ef_search | 50 | 30 | 100 |
| Result limit | 10 | 5 | 20 |
| BM25 results | 50 | 30 | 100 |

### Configuration

```yaml
# .amanmcp.yaml
performance:
  # Embedding batching
  embedding_batch_size: 32

  # HNSW search parameters
  ef_search: 50  # Lower = faster, higher = better recall

  # Candidate pool sizes
  bm25_candidates: 50
  vector_candidates: 50

  # Memory limits
  max_memory_mb: 300
```

### When to Tune

| Symptom | Cause | Fix |
|---------|-------|-----|
| Search > 100ms | Large index, ef too high | Lower ef_search |
| Missing results | ef too low | Raise ef_search |
| High memory | Large index | Increase max_memory_mb or reduce candidates |
| Slow indexing | CPU embeddings | Switch to MLX (Mac) or Ollama GPU |

---

## Monitoring Performance

### Built-in Metrics

```bash
# Check index stats
amanmcp status

# Output:
# Index size: 45 MB (disk), 180 MB (memory)
# Chunks: 12,345
# Average search time: 28ms
# Cache hit rate: 94%
```

### Verbose Timing

```bash
amanmcp search "query" --timing

# Output:
# Query embedding: 14ms
# BM25 search: 4ms
# Vector search: 8ms
# RRF fusion: 2ms
# Formatting: 3ms
# Total: 31ms
```

---

## Performance Summary

```mermaid
flowchart TB
    subgraph Expensive["Expensive Operations"]
        E1["Embedding generation<br/>~20ms per chunk"]
        E2["Initial index build<br/>Minutes for large codebases"]
    end

    subgraph Cheap["Cheap Operations"]
        C1["HNSW search<br/>~10ms for millions"]
        C2["BM25 search<br/>~5ms"]
        C3["Result fusion<br/>~2ms"]
    end

    subgraph Strategy["AmanMCP Strategy"]
        S1["Do expensive work once (at index time)"]
        S2["Cache everything possible"]
        S3["Keep cheap work for query time"]
    end

    Expensive --> S1
    Cheap --> S3
    S1 --> S2

    style Expensive fill:#ffcdd2
    style Cheap fill:#c8e6c9
    style Strategy fill:#e1f5ff
```

---

## Next Steps

| Want to... | Read |
|------------|------|
| Understand the indexing process | [Indexing Pipeline](indexing-pipeline.md) |
| Configure for your system | [Configuration Guide](../reference/configuration.md) |
| Optimize search quality | [Understanding Results](../tutorials/understanding-results.md) |

---

*Fast search is achieved by doing expensive work once and caching aggressively. AmanMCP indexes once, searches instantly.*
