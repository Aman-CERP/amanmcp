# AmanMCP Architecture Design

**Version:** 1.0.0-draft | **Go:** 1.25.5+ | **MCP Spec:** 2025-11-25

---

## Quick Overview

**New here?** This section gives you the essentials in 2 minutes.

### How AmanMCP Works

1. **Auto-Discovery** — Detects project type (Go, Node, Python) and source directories automatically
2. **AST-Based Chunking** — Uses tree-sitter to split code at function/class boundaries (not arbitrary lines)
3. **Hybrid Search** — Combines BM25 keyword search + semantic vector search for best results
4. **Incremental Updates** — Only re-indexes changed files

### Key Components

| Component | Technology | Purpose |
|-----------|------------|---------|
| MCP Server | Official Go SDK | Claude Code integration |
| Code Parsing | tree-sitter | AST-aware chunking |
| Keyword Search | Bleve BM25 | Exact term matching |
| Vector Search | coder/hnsw | Semantic similarity |
| Embeddings | Ollama (or Static768 fallback) | Text → vectors |
| Metadata | SQLite | File and chunk info |

### Multi-Project Support

Each VS Code/Cursor instance spawns its own isolated AmanMCP server. No manual switching needed.

```
VS Code #1 (api/)     VS Code #2 (web/)
       │                     │
       ▼                     ▼
  AmanMCP #1            AmanMCP #2
  (indexes api/)        (indexes web/)
```

**For detailed design, see the sections below.**

---

## 1. Architectural Overview

### 1.1 Design Philosophy

AmanMCP follows the **"It Just Works"** philosophy, inspired by Apple's design principles:

1. **Simplicity First** - Zero configuration required
2. **Sensible Defaults** - Convention over configuration
3. **Progressive Disclosure** - Advanced options available but hidden
4. **Local-First** - Privacy by design, no cloud dependencies
5. **Single Binary** - No runtime dependencies for users

### 1.2 High-Level Architecture

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                              CLIENT LAYER                                     │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────────────┐  │
│  │   Claude Code   │  │     Cursor      │  │   Other MCP Clients         │  │
│  └────────┬────────┘  └────────┬────────┘  └──────────────┬──────────────┘  │
│           │                    │                          │                  │
│           └────────────────────┴──────────────────────────┘                  │
│                                │                                              │
│                         MCP Protocol (stdio/SSE)                             │
└────────────────────────────────┼─────────────────────────────────────────────┘
                                 │
┌────────────────────────────────┼─────────────────────────────────────────────┐
│                                ▼                                              │
│  ┌─────────────────────────────────────────────────────────────────────────┐ │
│  │                        MCP SERVER LAYER                                  │ │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌─────────────┐  │ │
│  │  │   Protocol   │  │    Tool      │  │   Resource   │  │  Notifier   │  │ │
│  │  │   Handler    │  │   Router     │  │   Provider   │  │   (Events)  │  │ │
│  │  └──────────────┘  └──────────────┘  └──────────────┘  └─────────────┘  │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
│                                │                                              │
│  ┌─────────────────────────────┼───────────────────────────────────────────┐ │
│  │                        CORE ENGINE                                       │ │
│  │  ┌──────────────────────────▼──────────────────────────────────────────┐│ │
│  │  │                    SEARCH ENGINE                                     ││ │
│  │  │  ┌───────────────┐  ┌───────────────┐  ┌───────────────────────┐   ││ │
│  │  │  │  Query Parser │→ │ Query Router  │→ │   Result Aggregator   │   ││ │
│  │  │  └───────────────┘  └───────┬───────┘  └───────────▲───────────┘   ││ │
│  │  │                             │                       │               ││ │
│  │  │              ┌──────────────┴──────────────┐       │               ││ │
│  │  │              ▼                              ▼       │               ││ │
│  │  │  ┌───────────────────┐        ┌───────────────────┐│               ││ │
│  │  │  │   BM25 Searcher   │        │  Vector Searcher  ││               ││ │
│  │  │  │   (Keyword)       │        │  (Semantic)       │┘               ││ │
│  │  │  └─────────┬─────────┘        └─────────┬─────────┘                ││ │
│  │  │            │                            │                           ││ │
│  │  │            └────────────┬───────────────┘                           ││ │
│  │  │                         ▼                                           ││ │
│  │  │            ┌─────────────────────────┐                              ││ │
│  │  │            │    Score Fusion (RRF)   │                              ││ │
│  │  │            └─────────────────────────┘                              ││ │
│  │  └─────────────────────────────────────────────────────────────────────┘│ │
│  │                                                                          │ │
│  │  ┌─────────────────────────────────────────────────────────────────────┐│ │
│  │  │                       INDEXER                                        ││ │
│  │  │  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌──────────────┐  ││ │
│  │  │  │  Scanner   │→ │  Chunker   │→ │  Embedder  │→ │   Persister  │  ││ │
│  │  │  │(Discovery) │  │(tree-sitter)  │(Ollama)    │  │  (Storage)   │  ││ │
│  │  │  └────────────┘  └────────────┘  └────────────┘  └──────────────┘  ││ │
│  │  └─────────────────────────────────────────────────────────────────────┘│ │
│  │                                                                          │ │
│  │  ┌─────────────────────────────────────────────────────────────────────┐│ │
│  │  │                       WATCHER                                        ││ │
│  │  │  ┌────────────┐  ┌────────────┐  ┌────────────┐                     ││ │
│  │  │  │  fsnotify  │→ │  Debouncer │→ │ Diff Queue │→ Indexer            ││ │
│  │  │  └────────────┘  └────────────┘  └────────────┘                     ││ │
│  │  └─────────────────────────────────────────────────────────────────────┘│ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
│                                │                                              │
│  ┌─────────────────────────────┼───────────────────────────────────────────┐ │
│  │                       STORAGE LAYER                                      │ │
│  │  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────────┐  │ │
│  │  │  coder/hnsw     │  │   BM25 Index    │  │    Metadata Store       │  │ │
│  │  │  (HNSW Vectors) │  │   (Inverted)    │  │    (SQLite)             │  │ │
│  │  └─────────────────┘  └─────────────────┘  └─────────────────────────┘  │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
│                                │                                              │
│                         .amanmcp/ directory                                   │
└──────────────────────────────────────────────────────────────────────────────┘
```

![High-Level Architecture](./diagrams/architecture.svg)

---

## 2. Component Design

### 2.1 Project Structure

```
amanmcp/
├── cmd/
│   └── amanmcp/
│       └── main.go              # CLI entry point
├── internal/
│   ├── config/
│   │   ├── config.go            # Configuration management
│   │   ├── defaults.go          # Default values
│   │   └── detection.go         # Project type detection
│   ├── mcp/
│   │   ├── server.go            # MCP server implementation
│   │   ├── tools.go             # Tool definitions
│   │   ├── resources.go         # Resource providers
│   │   └── transport.go         # stdio/SSE transports
│   ├── search/
│   │   ├── engine.go            # Hybrid search coordinator
│   │   ├── bm25.go              # BM25 implementation
│   │   ├── vector.go            # Vector search wrapper
│   │   ├── fusion.go            # Score fusion (RRF)
│   │   └── classifier.go        # Query classification
│   ├── index/
│   │   ├── indexer.go           # Main indexer
│   │   ├── scanner.go           # File discovery
│   │   ├── chunker.go           # Chunking coordinator
│   │   └── watcher.go           # File system watcher
│   ├── chunk/
│   │   ├── code.go              # AST-based code chunker
│   │   ├── markdown.go          # Markdown chunker
│   │   ├── text.go              # Plain text chunker
│   │   └── treesitter.go        # tree-sitter integration
│   ├── embed/
│   │   ├── types.go             # Embedder interface
│   │   ├── ollama.go            # OllamaEmbedder (recommended, uses Ollama API)
│   │   ├── static768.go         # Static768 (768-dim fallback, default)
│   │   └── static.go            # Static256 (legacy fallback)
│   ├── store/
│   │   ├── store.go             # Storage coordinator
│   │   ├── hnsw.go              # coder/hnsw wrapper
│   │   ├── bm25.go              # BM25 index storage
│   │   └── metadata.go          # SQLite metadata
│   └── models/
│       ├── chunk.go             # Chunk model
│       ├── project.go           # Project model
│       └── result.go            # Search result model
├── pkg/
│   └── version/
│       └── version.go           # Version info
├── testdata/
│   ├── projects/                # Test projects
│   ├── fixtures/                # Test fixtures
│   └── golden/                  # Golden files
├── scripts/
│   ├── build.sh                 # Build script
│   └── release.sh               # Release script
├── .goreleaser.yaml             # GoReleaser config
├── Makefile                     # Development commands
├── go.mod
├── go.sum
├── LICENSE                      # MIT License
└── README.md                    # User documentation
```

### 2.2 Component Interactions

![Search Request Flow](./diagrams/sequence-search.svg)

### 2.3 Indexing Pipeline

![Indexing Pipeline](./diagrams/indexing-pipeline.svg)

---

## 3. Key Algorithms

### 3.1 Query Classification

```
┌─────────────────────────────────────────────────────────────────┐
│                    QUERY CLASSIFICATION                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Query: "useEffect cleanup function"                            │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  Step 1: Pattern Detection                                  │ │
│  │  ├─ Has camelCase: YES (useEffect)                         │ │
│  │  ├─ Has error code pattern: NO                              │ │
│  │  ├─ Is natural language: MIXED                              │ │
│  │  └─ Has special chars: NO                                   │ │
│  └────────────────────────────────────────────────────────────┘ │
│                          ▼                                       │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  Step 2: Weight Assignment                                  │ │
│  │  ├─ Technical term detected: boost BM25                    │ │
│  │  ├─ Natural language present: keep semantic                 │ │
│  │  └─ Result: BM25=0.5, Semantic=0.5                         │ │
│  └────────────────────────────────────────────────────────────┘ │
│                          ▼                                       │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  Step 3: Output Classification                              │ │
│  │  {                                                          │ │
│  │    type: "mixed",                                           │ │
│  │    bm25_weight: 0.5,                                        │ │
│  │    semantic_weight: 0.5,                                    │ │
│  │    boost_terms: ["useEffect"]                               │ │
│  │  }                                                          │ │
│  └────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

![Query Classification & Search Flow](./diagrams/search-flow.svg)

**Classification Rules:**

| Query Pattern | BM25 Weight | Semantic Weight | Reason |
|---------------|-------------|-----------------|--------|
| Error codes (ERR_*, E0001) | 0.8 | 0.2 | Exact match critical |
| camelCase/snake_case only | 0.7 | 0.3 | Technical identifier |
| Natural language | 0.25 | 0.75 | Conceptual understanding |
| Mixed (technical + NL) | 0.5 | 0.5 | Balanced approach |
| Quoted "exact phrase" | 0.9 | 0.1 | User wants exact |

### 3.2 Reciprocal Rank Fusion (RRF)

```
┌─────────────────────────────────────────────────────────────────┐
│                  RECIPROCAL RANK FUSION                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  BM25 Results:           Vector Results:                        │
│  1. chunk_A (rank 1)     1. chunk_C (rank 1)                   │
│  2. chunk_B (rank 2)     2. chunk_A (rank 2)                   │
│  3. chunk_C (rank 3)     3. chunk_D (rank 3)                   │
│  4. chunk_D (rank 4)     4. chunk_B (rank 4)                   │
│                                                                  │
│  RRF Formula: score(d) = Σ weight_i / (k + rank_i)              │
│  Where k = 60 (constant to prevent extreme values)              │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  chunk_A:                                                   │ │
│  │    BM25: 0.35 × (1/(60+1)) = 0.00574                       │ │
│  │    Vec:  0.65 × (1/(60+2)) = 0.01048                       │ │
│  │    Total: 0.01622                                           │ │
│  │                                                             │ │
│  │  chunk_C:                                                   │ │
│  │    BM25: 0.35 × (1/(60+3)) = 0.00556                       │ │
│  │    Vec:  0.65 × (1/(60+1)) = 0.01066                       │ │
│  │    Total: 0.01622                                           │ │
│  │                                                             │ │
│  │  chunk_B:                                                   │ │
│  │    BM25: 0.35 × (1/(60+2)) = 0.00565                       │ │
│  │    Vec:  0.65 × (1/(60+4)) = 0.01016                       │ │
│  │    Total: 0.01581                                           │ │
│  │                                                             │ │
│  │  chunk_D:                                                   │ │
│  │    BM25: 0.35 × (1/(60+4)) = 0.00547                       │ │
│  │    Vec:  0.65 × (1/(60+3)) = 0.01032                       │ │
│  │    Total: 0.01579                                           │ │
│  └────────────────────────────────────────────────────────────┘ │
│                                                                  │
│  Final Ranking: chunk_A ≈ chunk_C > chunk_B > chunk_D           │
│  (chunk_A chosen as tie-breaker: appears in both lists)        │
└─────────────────────────────────────────────────────────────────┘
```

*See [search-flow.svg](./diagrams/search-flow.svg) above for the visual RRF flow.*

### 3.3 AST-Based Chunking (cAST Algorithm)

```
┌─────────────────────────────────────────────────────────────────┐
│                    cAST CHUNKING ALGORITHM                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Input: Go source file                                          │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  package main                                               │ │
│  │                                                             │ │
│  │  import "fmt"                                               │ │
│  │                                                             │ │
│  │  type User struct {                                         │ │
│  │      Name string                                            │ │
│  │      Age  int                                               │ │
│  │  }                                                          │ │
│  │                                                             │ │
│  │  func (u *User) Greet() string {                           │ │
│  │      return fmt.Sprintf("Hello, %s", u.Name)               │ │
│  │  }                                                          │ │
│  │                                                             │ │
│  │  func main() {                                              │ │
│  │      user := User{Name: "Alice", Age: 30}                  │ │
│  │      fmt.Println(user.Greet())                             │ │
│  │  }                                                          │ │
│  └────────────────────────────────────────────────────────────┘ │
│                          ▼                                       │
│  Step 1: Parse into AST                                         │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  source_file                                                │ │
│  │  ├── package_clause                                         │ │
│  │  ├── import_declaration                                     │ │
│  │  ├── type_declaration (User struct)                        │ │
│  │  ├── method_declaration (User.Greet)                       │ │
│  │  └── function_declaration (main)                           │ │
│  └────────────────────────────────────────────────────────────┘ │
│                          ▼                                       │
│  Step 2: Extract top-level nodes with context                   │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  Chunk 1: User struct                                       │ │
│  │  ├── Include: package, imports                              │ │
│  │  ├── Content: type definition                               │ │
│  │  └── Symbols: [{name: "User", type: "struct"}]             │ │
│  │                                                             │ │
│  │  Chunk 2: User.Greet method                                 │ │
│  │  ├── Include: package, imports, User type reference        │ │
│  │  ├── Content: method definition                             │ │
│  │  └── Symbols: [{name: "Greet", type: "method"}]            │ │
│  │                                                             │ │
│  │  Chunk 3: main function                                     │ │
│  │  ├── Include: package, imports                              │ │
│  │  ├── Content: function definition                           │ │
│  │  └── Symbols: [{name: "main", type: "function"}]           │ │
│  └────────────────────────────────────────────────────────────┘ │
│                          ▼                                       │
│  Step 3: Size check and recursive splitting if needed           │
│  (All chunks < 1500 tokens, no splitting required)              │
│                          ▼                                       │
│  Step 4: Merge small adjacent chunks if beneficial              │
│  (Optional: merge if combined < 500 tokens)                     │
└─────────────────────────────────────────────────────────────────┘
```

---

## 4. Storage Design

### 4.1 Storage Directory Structure

```
.amanmcp/
├── vectors.hnsw             # coder/hnsw HNSW index (GOB encoded)
├── vectors.hnsw.meta        # Vector ID mappings (GOB encoded)
├── bm25.bleve/              # Bleve BM25 index directory
├── metadata.db              # SQLite metadata (chunks, files, symbols)
└── config.yaml              # Project-specific config (if any)
```

### 4.2 Metadata Schema (SQLite)

```sql
-- Projects table
CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    root_path TEXT NOT NULL UNIQUE,
    project_type TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Files table (for incremental indexing)
CREATE TABLE files (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    path TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    size INTEGER,
    mod_time TIMESTAMP,
    indexed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (project_id) REFERENCES projects(id),
    UNIQUE(project_id, path)
);

-- Chunks table (metadata only, content in vector store)
CREATE TABLE chunks (
    id TEXT PRIMARY KEY,
    file_id TEXT NOT NULL,
    content_type TEXT NOT NULL,
    language TEXT,
    start_line INTEGER,
    end_line INTEGER,
    symbols TEXT,  -- JSON array
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (file_id) REFERENCES files(id)
);

-- Indexes for fast lookups
CREATE INDEX idx_files_project ON files(project_id);
CREATE INDEX idx_files_path ON files(path);
CREATE INDEX idx_chunks_file ON chunks(file_id);
CREATE INDEX idx_chunks_type ON chunks(content_type);
```

### 4.3 Vector Store Design (coder/hnsw)

AmanMCP uses [coder/hnsw](https://github.com/coder/hnsw) — a pure Go HNSW implementation
designed for simplicity and portability. Replaced USearch (CGO) in v0.1.38.

**Why coder/hnsw?**

- **Pure Go** — no CGO, portable binary distribution
- **HNSW algorithm** — scales logarithmically O(log n), not linearly
- **Simple persistence** — GOB encoding for fast save/load
- **Production-tested** — used by Coder for AI features
- **No external deps** — simplifies build and deployment

**Performance Benchmarks** (HNSW with typical embeddings):

| Documents | Query Time | Notes |
|-----------|------------|-------|
| 10,000 | < 1ms | ⚡ Instant |
| 100,000 | ~2-5ms | ⚡ Excellent |
| **300,000** | **~5-10ms** | ✅ **Target scale** |
| 1,000,000 | ~10-20ms | ✅ Scales logarithmically |
| 10,000,000+ | ~20-50ms | ✅ Memory-mapped for large indexes |

```go
// Collection structure
type VectorCollection struct {
    Name       string                 // "chunks"
    Metadata   map[string]interface{} // Collection metadata
    Embeddings []EmbeddingRecord      // Vector records
}

type EmbeddingRecord struct {
    ID        string            // Chunk ID
    Embedding []float32         // 768-dim vector
    Content   string            // Original text (for retrieval)
    Metadata  map[string]string // file_path, language, etc.
}

// Storage format: GOB-encoded for fast serialization
// Persistence: Write on shutdown, load on startup
// Memory: Keep full index in memory (typical: 50-200MB for 100K files)
```

#### 4.3.1 coder/hnsw Integration

```go
import (
    "github.com/coder/hnsw"
)

// Initialize HNSW graph for vector storage
func NewVectorIndex(dimensions int) (*hnsw.Graph[uint64], error) {
    graph := hnsw.NewGraph[uint64]()
    return graph, nil
}

// Add vector to the graph
func (s *HNSWStore) Add(id string, vector []float32) error {
    key := s.getOrCreateKey(id)
    node := hnsw.MakeNode(key, vector)
    s.graph.Add(node)
    return nil
}

// Search for similar vectors
func (s *HNSWStore) Search(vector []float32, k int) ([]VectorResult, error) {
    neighbors := s.graph.Search(vector, k)
    // Convert to VectorResult...
    return results, nil
}

// Persistence via GOB encoding
func (s *HNSWStore) Save(path string) error {
    return s.graph.Export(path)
}

func (s *HNSWStore) Load(path string) error {
    return s.graph.Import(path)
}
```

#### 4.3.2 Vector Store Selection (v0.1.38)

**Why we switched from USearch to coder/hnsw:**

USearch required CGO with dynamic library linking, which caused distribution problems
(BUG-018: CLI binary hung when installed to `~/.local/bin/`). coder/hnsw is pure Go,
eliminating all CGO-related distribution issues.

| Solution | Status | Notes |
|----------|--------|-------|
| **[coder/hnsw](https://github.com/coder/hnsw)** | **Current (v0.1.38+)** | Pure Go, no CGO, portable |
| USearch | Removed (v0.1.38) | CGO issues, see ADR-022 |
| chromem-go | Not used | Linear scan, doesn't scale |

**AmanMCP Default:** coder/hnsw is the vector store since v0.1.38. With 300K+ documents as the target scale,
HNSW's logarithmic scaling ensures sub-10ms queries even at scale.

---

## 5. Performance Considerations

### 5.1 Memory Management

AmanMCP is designed for **typical developer hardware** (16-32GB RAM), not high-end workstations.

#### 5.1.1 Realistic Hardware Profiles

| Developer Setup | Total RAM | Available for AmanMCP | Recommended Settings |
|-----------------|-----------|----------------------|----------------------|
| MacBook Air M2 | 16 GB | ~4-6 GB | `GOGC=100 GOMEMLIMIT=4GiB` |
| **MacBook Pro M4** | **24 GB** | **~8-10 GB** | **`GOGC=100 GOMEMLIMIT=8GiB`** |
| MacBook Pro M4 Max | 32 GB | ~12-16 GB | `GOGC=50 GOMEMLIMIT=12GiB` |
| Linux Workstation | 64 GB+ | ~32+ GB | `GOGC=off GOMEMLIMIT=32GiB` |

**Why these limits?** Typical developer machines run:

- macOS + System: ~4-5 GB
- IDE (VS Code/Cursor): ~2-3 GB
- Browser: ~2-4 GB
- Ollama service: external process (memory varies by model)
- Other apps: ~2-4 GB

#### 5.1.2 Memory Budget (per 100K documents)

| Component | F32 (full) | F16 (default) | I8 (compact) |
|-----------|------------|---------------|--------------|
| Vector embeddings | ~300 MB | **~150 MB** | ~75 MB |
| BM25 inverted index | ~50 MB | ~50 MB | ~50 MB |
| Metadata (SQLite) | ~10 MB | ~10 MB | ~10 MB |
| Tree-sitter parsers | ~20 MB | ~20 MB | ~20 MB |
| Runtime overhead | ~50 MB | ~50 MB | ~50 MB |
| **Total** | **~430 MB** | **~280 MB** | **~205 MB** |

**Default:** F16 quantization — half the memory of F32, negligible quality loss.

#### 5.1.3 Memory-Mapped Index (Critical for Scale)

For 300K+ documents, memory-mapped mode is **essential**:

```go
// Memory-mapped loading — OS manages paging, not Go heap
func LoadIndexView(path string) (*usearch.Index, error) {
    index, err := usearch.NewIndex(usearch.DefaultConfig(768))
    if err != nil {
        return nil, err
    }
    // View() memory-maps the file instead of loading into RAM
    return index, index.View(path)
}
```

| Mode | 300K docs | 24GB System | Notes |
|------|-----------|-------------|-------|
| Load into RAM | ~450 MB heap | ⚠️ Tight | Faster queries |
| **Memory-mapped** | **~50 MB heap** | ✅ **Recommended** | OS pages on demand |

#### 5.1.4 Go Runtime Tuning

```bash
# For 24GB M4 Pro (recommended)
GOGC=100 GOMEMLIMIT=8GiB ./amanmcp serve

# For 16GB systems (conservative)
GOGC=100 GOMEMLIMIT=4GiB ./amanmcp serve

# For 32GB+ systems (aggressive)
GOGC=50 GOMEMLIMIT=12GiB ./amanmcp serve
```

Or programmatically with auto-detection:

```go
import (
    "runtime/debug"
    "github.com/pbnjay/memory"
)

func ConfigureMemory() {
    totalMem := memory.TotalMemory()

    // Reserve 60% for other apps, use 40% for AmanMCP
    limit := int64(float64(totalMem) * 0.4)

    // Cap at 16GB even on large systems
    if limit > 16*1024*1024*1024 {
        limit = 16 * 1024 * 1024 * 1024
    }

    debug.SetMemoryLimit(limit)
    debug.SetGCPercent(100) // Or -1 for GOGC=off on large systems
}
```

### 5.2 Latency Optimization

```
┌─────────────────────────────────────────────────────────────────┐
│                    LATENCY BREAKDOWN                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Query: "authentication middleware"                             │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Component             Target    Strategy                │   │
│  ├──────────────────────────────────────────────────────────┤   │
│  │  Query parsing         < 1ms     Simple string ops       │   │
│  │  Query classification  < 2ms     Pattern matching        │   │
│  │  Query embedding       ~30-50ms  Ollama (cached)         │   │
│  │  BM25 search           < 5ms     Inverted index lookup   │   │
│  │  Vector search         < 10ms    HNSW similarity         │   │
│  │  Score fusion          < 1ms     Simple arithmetic       │   │
│  │  Result formatting     < 1ms     JSON serialization      │   │
│  ├──────────────────────────────────────────────────────────┤   │
│  │  Total (cold)          ~70ms                             │   │
│  │  Total (warm cache)    ~20ms     Embedding cached        │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  Optimization: LRU cache for frequent queries                    │
└─────────────────────────────────────────────────────────────────┘
```

### 5.3 Indexing Throughput

```
┌─────────────────────────────────────────────────────────────────┐
│                    INDEXING PIPELINE                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Bottleneck: Embedding generation (Ollama API inference)        │
│                                                                  │
│  Strategy: Batch processing + parallelism                       │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  Pipeline Stage        Files/sec   Strategy                │ │
│  ├────────────────────────────────────────────────────────────┤ │
│  │  File discovery        10,000+     Concurrent walk         │ │
│  │  File reading          5,000+      Buffered I/O            │ │
│  │  AST parsing           2,000+      tree-sitter (fast)      │ │
│  │  Chunking              3,000+      Simple splitting        │ │
│  │  Embedding (Ollama)    200-500     Batch + concurrent      │ │
│  │  Storage               5,000+      Batch writes            │ │
│  └────────────────────────────────────────────────────────────┘ │
│                                                                  │
│  Effective throughput: ~100-200 files/sec (embedding-bound)     │
│                                                                  │
│  For 10K files: ~50-100 seconds initial index                   │
│  For 100K files: ~10-20 minutes initial index                   │
│                                                                  │
│  Mitigation:                                                     │
│  1. Show progress bar with ETA                                  │
│  2. Incremental: only re-index changed files                    │
│  3. Background indexing: serve queries while indexing           │
│  4. Priority: index frequently-accessed dirs first               │
└─────────────────────────────────────────────────────────────────┘
```

---

## 6. Error Handling & Resilience

### 6.1 Failure Modes

| Failure | Detection | Recovery |
|---------|-----------|----------|
| Model download fails | Network error | Use static embeddings |
| Corrupted index | Checksum mismatch | Rebuild from scratch |
| File read error | OS error | Skip file, log warning |
| Parse error | tree-sitter error | Fall back to line chunking |
| OOM during indexing | Memory threshold | Pause, flush, resume |
| Network timeout | HTTP timeout | Retry with backoff |

### 6.2 Graceful Degradation

```
┌─────────────────────────────────────────────────────────────────┐
│                    FALLBACK CHAIN                                │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Embedding Generation:                                          │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  1. OllamaEmbedder (Ollama)    ── fail ──►              │   │
│  │  2. Static768 embeddings       ── works ──► Continue     │   │
│  │     (768 dims, dimension-compatible, no re-indexing)     │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  Code Parsing:                                                   │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  1. tree-sitter AST parsing    ── fail ──►              │   │
│  │  2. Regex-based extraction     ── fail ──►              │   │
│  │  3. Line-based chunking        ── works ──► Continue     │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  Search:                                                         │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  1. Hybrid (BM25 + Vector)     ── vec fail ──►          │   │
│  │  2. BM25 only                  ── works ──► Continue     │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  Principle: Always return something useful                       │
└─────────────────────────────────────────────────────────────────┘
```

---

## 7. Security Model

### 7.1 Threat Model

```
┌─────────────────────────────────────────────────────────────────┐
│                    SECURITY BOUNDARIES                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │  TRUSTED ZONE (User's Machine)                          │    │
│  │                                                         │    │
│  │  ┌───────────────┐    ┌───────────────┐                │    │
│  │  │  Claude Code  │◄──►│   AmanMCP     │                │    │
│  │  │  (Client)     │    │   (Server)    │                │    │
│  │  └───────────────┘    └───────┬───────┘                │    │
│  │                               │                         │    │
│  │                        ┌──────▼────────┐                │    │
│  │                        │  File System  │                │    │
│  │                        │  (project)    │                │    │
│  │                        └───────────────┘                │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │  UNTRUSTED ZONE (External Network)                      │    │
│  │                                                         │    │
│  │  ✗ No outbound connections                              │    │
│  │  ✗ No telemetry                                         │    │
│  │  ✗ No cloud sync                                        │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### 7.2 Sensitive File Handling

```go
// Default patterns to exclude from indexing
var SensitivePatterns = []string{
    // Environment and secrets
    ".env*",
    "*.pem",
    "*.key",
    "*.p12",
    "*.pfx",
    "*credentials*",
    "*secrets*",
    "*password*",

    // Cloud provider configs
    ".aws/*",
    ".gcp/*",
    ".azure/*",

    // SSH and auth
    ".ssh/*",
    ".netrc",
    ".npmrc",
    ".pypirc",

    // Database files
    "*.sqlite",
    "*.db",
    "*.sql",

    // IDE and local configs
    ".idea/*",
    ".vscode/*",
    "*.local.*",
}
```

---

## 8. Extensibility

### 8.1 Plugin Architecture (Future)

```
┌─────────────────────────────────────────────────────────────────┐
│                    PLUGIN SYSTEM (v2.0)                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Plugin Types:                                            │   │
│  │                                                           │   │
│  │  1. Chunkers (add language support)                      │   │
│  │     interface Chunker {                                   │   │
│  │       Name() string                                       │   │
│  │       Extensions() []string                               │   │
│  │       Chunk(path, content) ([]Chunk, error)              │   │
│  │     }                                                     │   │
│  │                                                           │   │
│  │  2. Embedders (add embedding providers)                   │   │
│  │     interface Embedder {                                  │   │
│  │       Name() string                                       │   │
│  │       Dimensions() int                                    │   │
│  │       Embed(text) ([]float32, error)                     │   │
│  │     }                                                     │   │
│  │                                                           │   │
│  │  3. Searchers (add search strategies)                    │   │
│  │     interface Searcher {                                  │   │
│  │       Name() string                                       │   │
│  │       Search(query, opts) ([]Result, error)              │   │
│  │     }                                                     │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  Plugin Location: ~/.amanmcp/plugins/                            │
│  Discovery: Automatic at startup                                 │
│  Format: Shared libraries (.so/.dylib) or gRPC services         │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### 8.2 Language Support Matrix

| Language | Phase 1 | Phase 2 | Method |
|----------|---------|---------|--------|
| Go | ✅ | - | tree-sitter |
| TypeScript | ✅ | - | tree-sitter |
| JavaScript | ✅ | - | tree-sitter |
| Python | ✅ | - | tree-sitter |
| Markdown | ✅ | - | Custom parser |
| HTML | - | ✅ | tree-sitter |
| CSS | - | ✅ | tree-sitter |
| React/JSX | ✅ | - | tree-sitter (tsx) |
| React Native | - | ✅ | tree-sitter (tsx) |
| Next.js | - | ✅ | tree-sitter + special handling |
| Vue | - | ✅ | tree-sitter |
| SQL | - | ✅ | tree-sitter |
| JSON | - | ✅ | Built-in |
| YAML | - | ✅ | Built-in |

---

## 9. Testing Strategy

### 9.1 Test Pyramid

```
                    ┌───────────┐
                    │    E2E    │  MCP protocol compliance
                    │   Tests   │  Full server tests
                    └─────┬─────┘
                          │
              ┌───────────┴───────────┐
              │   Integration Tests   │  Component interactions
              │   (Search, Indexing)  │  Database operations
              └───────────┬───────────┘
                          │
        ┌─────────────────┴─────────────────┐
        │          Unit Tests               │  Individual functions
        │   (Chunkers, BM25, Fusion)        │  Edge cases
        └───────────────────────────────────┘
```

### 9.2 Benchmark Suite

```go
// Performance benchmarks
func BenchmarkSearch_SmallIndex(b *testing.B)    // 1K files
func BenchmarkSearch_MediumIndex(b *testing.B)   // 10K files
func BenchmarkSearch_LargeIndex(b *testing.B)    // 100K files

func BenchmarkIndexing_GoProject(b *testing.B)   // Go codebase
func BenchmarkIndexing_NodeProject(b *testing.B) // Node.js project

func BenchmarkChunking_LargeFile(b *testing.B)   // 10K line file
func BenchmarkEmbedding_Batch(b *testing.B)      // 100 texts
```

---

## 10. Deployment Considerations

### 10.1 Build Matrix

| OS | Arch | CGO | Status |
|----|------|-----|--------|
| macOS | amd64 | Yes | Primary |
| macOS | arm64 | Yes | Primary |
| Linux | amd64 | Yes | Primary |
| Linux | arm64 | Yes | Secondary |
| Windows | amd64 | Yes | Community |

### 10.2 Release Checklist

1. **Pre-release**
   - [ ] All tests pass
   - [ ] Benchmarks within targets
   - [ ] CHANGELOG updated
   - [ ] Version bumped

2. **Build**
   - [ ] GoReleaser build
   - [ ] Cross-platform binaries
   - [ ] Homebrew formula

3. **Publish**
   - [ ] GitHub Release
   - [ ] Homebrew tap update
   - [ ] Documentation update

4. **Verification**
   - [ ] macOS smoke test
   - [ ] Linux smoke test
   - [ ] MCP integration test

---

## Appendix A: Technology Validation

For comprehensive validation of all technology choices against 2025 industry best practices, see:

**[Technology Validation Report (2026)](./technology-validation-2026.md)**

This document validates each component choice with grounded research from 20+ industry sources, including:
- Embedding backend comparison (Ollama vs vLLM vs TEI)
- Vector store evaluation (Pure Go HNSW vs CGO alternatives)
- Hybrid search strategy validation (RRF vs linear combination)
- Code parsing approach (tree-sitter AST vs alternatives)

---

## Appendix B: Decision Log

| Decision | Options Considered | Choice | Rationale |
|----------|-------------------|--------|-----------|
| Vector DB | Qdrant, Milvus, chromem-go, USearch, coder/hnsw | [coder/hnsw](https://github.com/coder/hnsw) (v0.1.38+) | Pure Go, no CGO, portable binary (ADR-022) |
| Previous Vector DB | USearch | Removed in v0.1.38 | CGO caused distribution issues (BUG-018) |
| Embeddings | OpenAI, Ollama, Hugot, gollama.cpp | Ollama API (OllamaEmbedder) + Static768 fallback | HTTP API, Metal GPU via Ollama, dimension-compatible fallback |
| Embedding Model | Qwen3-embedding, nomic-embed-text | [Qwen3-embedding](https://huggingface.co/Qwen/Qwen3-Embedding-8B) (recommended) | #1 MTEB, 32K context, via Ollama |
| Code parsing | Regex, custom, tree-sitter | [tree-sitter](https://github.com/tree-sitter/go-tree-sitter) | Battle-tested, 40+ languages, official Go bindings |
| MCP SDK | Custom, mcp-go, official | [Official Go SDK](https://github.com/modelcontextprotocol/go-sdk) | Maintained by Google & Anthropic, stable since July 2025 |
| Search fusion | Linear, RRF, learned | RRF | Simple, effective, no training |
| Storage | JSON, SQLite, custom | SQLite + GOB | Best of both: queries + speed |
| CLI framework | flag, cobra, urfave | cobra | Widely used, good UX |

---

*Document maintained by AmanMCP Team. Last updated: 2026-01-03*
