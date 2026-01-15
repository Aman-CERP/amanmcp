# Claude Code Native Search vs amanmcp MCP Tools: A Benchmark

> When should you use Grep/Glob vs semantic search? This benchmark answers that question with data.

```mermaid
%%{init: {'theme': 'base', 'themeVariables': { 'primaryColor': '#e1f5fe', 'secondaryColor': '#fff3e0'}}}%%
flowchart LR
    subgraph Query["User Query"]
        Q["'Find retry logic'"]
    end

    subgraph Grep["Claude Code Grep"]
        G1["Pattern Match"] --> G2["Line Numbers"]
        G2 --> G3["Text Fragments"]
    end

    subgraph MCP["amanmcp MCP"]
        M1["Semantic Understanding"] --> M2["Concept Matching"]
        M2 --> M3["Full Implementations"]
    end

    Q --> Grep
    Q --> MCP

    G3 --> R1["WHERE words appear"]
    M3 --> R2["WHAT implements it"]

    style G3 fill:#ffcdd2
    style M3 fill:#c8e6c9
    style R1 fill:#ffcdd2
    style R2 fill:#c8e6c9
```

## Executive Summary

| Metric | Claude Code (Grep/Glob) | amanmcp MCP Tools |
|--------|------------------------|-------------------|
| **Speed** | ~8ms/search | ~34ms/search |
| **Query Type** | Keyword/pattern matching | Semantic/concept understanding |
| **Best For** | Finding WHERE words appear | Finding WHAT implements concepts |
| **Result Format** | Line matches | Full function implementations |

**Verdict**: Use both. MCP tools for exploration and understanding; Grep for exact matches and confirmation.

```mermaid
%%{init: {'theme': 'base'}}%%
quadrantChart
    title Search Tool Comparison
    x-axis Low Speed --> High Speed
    y-axis Surface Results --> Deep Understanding
    quadrant-1 Ideal for Exploration
    quadrant-2 Not Practical
    quadrant-3 Limited Use
    quadrant-4 Ideal for Precision
    "amanmcp MCP": [0.35, 0.85]
    "Claude Grep": [0.85, 0.25]
    "Hybrid Approach": [0.60, 0.70]
```

---

## The Benchmark

We ran identical queries through both systems on the amanmcp codebase (~3,800 chunks, ~150 Go files).

### Test Environment

- **Codebase**: amanmcp (Go, ~50K LOC)
- **Index**: qwen3-embedding:0.6b (1024 dimensions)
- **Hardware**: Apple Silicon M-series
- **Claude Code Version**: Latest
- **amanmcp Version**: 0.8.2

```mermaid
%%{init: {'theme': 'base', 'themeVariables': { 'primaryColor': '#e8f5e9'}}}%%
block-beta
    columns 3

    block:Codebase:3
        A["amanmcp Codebase"]
    end

    B["~150 Go Files"] C["~50K LOC"] D["~3,800 Chunks"]

    block:Index:3
        E["qwen3-embedding:0.6b<br/>1024 dimensions"]
    end

    style A fill:#e3f2fd
    style E fill:#fff3e0
```

---

## How Each Search Works

Before diving into the queries, let's understand the fundamental difference in how each search operates:

```mermaid
%%{init: {'theme': 'base'}}%%
flowchart TB
    subgraph Grep["Claude Code Grep/Glob"]
        direction TB
        G1["Query: 'retry backoff'"] --> G2["Regex Engine"]
        G2 --> G3["Scan All Files"]
        G3 --> G4["Pattern Match:<br/>retry.*backoff"]
        G4 --> G5["Return Line Numbers<br/>+ Surrounding Text"]
    end

    subgraph MCP["amanmcp MCP Tools"]
        direction TB
        M1["Query: 'retry logic'"] --> M2["Neural Embedder<br/>(qwen3-0.6b)"]
        M2 --> M3["Query Vector<br/>[0.12, -0.45, ...]"]
        M3 --> M4["Cosine Similarity<br/>+ BM25 Hybrid"]
        M4 --> M5["Return Full Functions<br/>+ Context"]
    end

    style G5 fill:#ffcdd2
    style M5 fill:#c8e6c9
```

---

## Query 1: "Retry Logic with Exponential Backoff"

### Claude Code Grep

```bash
grep -rn "retry.*exponential|backoff" --include="*.go"
```

**Results** (5 matches, ~8ms):
```
cmd/amanmcp/cmd/init.go:139: "error handling retry backoff"
internal/embed/ollama.go:388: // Calculate retry factor (exponential increase per retry)
internal/embed/ollama.go:468: // Exponential backoff: 100ms * 2^attempt
internal/embed/ollama.go:469: backoff := time.Duration(100<<attempt) * time.Millisecond
internal/embed/ollama.go:473: case <-time.After(backoff):
```

**Analysis**: Found lines containing the keywords. Useful for seeing where these terms appear, but doesn't show the actual retry implementation.

### amanmcp MCP search_code

```json
{"query": "retry logic with exponential backoff", "limit": 5}
```

**Results** (5 matches, ~34ms):
```go
// File: internal/errors/retry.go
// function 'Retry'; documented as: Retry executes a function with exponential back...

func Retry(ctx context.Context, cfg RetryConfig, fn func() error) error {
    delay := cfg.InitialDelay
    var lastErr error

    for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        if err := fn(); err != nil {
            lastErr = err
            if attempt >= cfg.MaxRetries {
                break
            }

            waitDelay := delay
            if cfg.Jitter {
                jitterFactor := 0.5 + rand.Float64()*0.5
                waitDelay = time.Duration(float64(delay) * jitterFactor)
            }

            select {
            case <-ctx.Done():
                return ctx.Err()
            case <-time.After(waitDelay):
            }

            delay = time.Duration(float64(delay) * cfg.Multiplier)
            if delay > cfg.MaxDelay {
                delay = cfg.MaxDelay
            }
            continue
        }
        return nil
    }
    return fmt.Errorf("failed after %d retries: %w", cfg.MaxRetries, lastErr)
}
```

**Analysis**: Found the actual `Retry()` function implementation with full context, jitter support, and exponential backoff logic. This is the answer you'd want when asking "how does retry work?"

### Winner: amanmcp MCP

The semantic search understood "retry logic with exponential backoff" as a concept and found the implementation, not just mentions of the words.

```mermaid
%%{init: {'theme': 'base'}}%%
flowchart LR
    subgraph Input
        Q["'retry exponential backoff'"]
    end

    subgraph Grep["Grep Results"]
        G1["Line 388: // Calculate retry factor"]
        G2["Line 468: // Exponential backoff"]
        G3["Line 469: backoff := time.Duration..."]
    end

    subgraph MCP["MCP Results"]
        M1["func Retry(...) {<br/>  delay := cfg.InitialDelay<br/>  for attempt := 0...<br/>    // Full 40-line implementation<br/>}"]
    end

    Q --> Grep
    Q --> MCP

    Grep --> Out1["Comments & Fragments"]
    MCP --> Out2["Complete Function"]

    style Out1 fill:#ffcdd2
    style Out2 fill:#c8e6c9
```

---

## Query 2: "Prevent Memory Leaks / Clean Up Resources"

### Claude Code Grep

```bash
grep -rn "memory leak|goroutine leak|resource cleanup"
```

**Results** (5 matches):
```
CONTRIBUTING.md:143: // IMPORTANT: Always close tree-sitter objects to prevent memory leaks
cmd/amanmcp/cmd/setup_test.go:38: assert.LessOrEqual(t, leaked, 2, "goroutine leak detected...")
cmd/amanmcp/cmd/doctor.go:71: // Set up context with signal handling (uses signal.NotifyContext to prevent goroutine leaks)
cmd/amanmcp/cmd/init_test.go:53: assert.LessOrEqual(t, leaked, 2, "goroutine leak detected...")
cmd/amanmcp/cmd/doctor_test.go:38: assert.LessOrEqual(t, leaked, 2, "goroutine leak detected...")
```

**Analysis**: Found documentation and test assertions mentioning leaks. Doesn't show actual cleanup code.

### amanmcp MCP search

```json
{"query": "prevent memory leaks clean up resources properly", "limit": 5}
```

**Results**:
```go
// File: internal/telemetry/query_metrics.go
// method 'Close'; documented as: Close flushes and releases resources.

func (m *QueryMetrics) Close() error {
    m.mu.Lock()
    if m.closed {
        m.mu.Unlock()
        return nil
    }
    m.closed = true
    m.mu.Unlock()

    // Stop auto-flush
    if m.flushTicker != nil {
        m.flushTicker.Stop()
        close(m.stopCh)
    }

    // Final flush
    if err := m.Flush(); err != nil {
        return err
    }
    return nil
}
```

Also found:
- `TestServer_Close_ReleasesResources` - test verifying cleanup
- `DeleteChunks`, `DeleteFile` methods - actual resource deletion

**Analysis**: Found `Close()` methods that actually DO resource cleanup, not just comments about it.

### Winner: amanmcp MCP

Semantic search understood "clean up resources" as a concept and found the code that implements it.

```mermaid
%%{init: {'theme': 'base'}}%%
flowchart TB
    subgraph Query
        Q["'prevent memory leaks<br/>clean up resources'"]
    end

    subgraph GrepPath["Grep: Keyword Match"]
        G1["Searches for exact words"]
        G2["'memory leak' OR 'resource cleanup'"]
        G3["Finds: comments, test assertions,<br/>documentation mentions"]
    end

    subgraph MCPPath["MCP: Concept Match"]
        M1["Understands: cleanup = Close()"]
        M2["Understands: resources = tickers,<br/>channels, connections"]
        M3["Finds: actual Close() methods<br/>that DO the cleanup"]
    end

    Q --> GrepPath
    Q --> MCPPath

    GrepPath --> R1["'// prevent memory leaks'<br/>(comment)"]
    MCPPath --> R2["func Close() error {<br/>  m.flushTicker.Stop()<br/>  close(m.stopCh)<br/>}"]

    style R1 fill:#ffcdd2
    style R2 fill:#c8e6c9
```

---

## Query 3: "RRF Fusion Combine BM25 Vector Scores"

### Claude Code Grep

```bash
grep -rn "func.*Fuse|RRF.*func"
```

**Results** (16 matches):
```
internal/search/fusion.go:60:func (f *RRFFusion) Fuse(
internal/search/fusion.go:119:func (f *RRFFusion) getOrCreate(...)
internal/search/fusion.go:138:func (f *RRFFusion) toSortedSlice(...)
internal/search/fusion.go:159:func (f *RRFFusion) compare(...)
internal/search/fusion.go:181:func (f *RRFFusion) normalize(...)
internal/search/multi_fusion.go:80:func (f *MultiRRFFusion) FuseMultiQuery(...)
```

**Analysis**: Found the exact function signatures. Great for finding the entry point.

### amanmcp MCP search_code

```json
{"query": "RRF reciprocal rank fusion combine BM25 vector scores", "limit": 5}
```

**Results**:
```go
// File: internal/search/fusion_test.go
// function 'TestRRFFusion_Basic'

func TestRRFFusion_Basic(t *testing.T) {
    // Given: BM25 results [A, B, C] and Vector results [C, A, D]
    bm25 := createBM25Results([]string{"A", "B", "C"}, []float64{2.5, 2.0, 1.5})
    vec := createVecResults([]string{"C", "A", "D"}, []float32{0.95, 0.90, 0.85})
    weights := DefaultWeights() // BM25: 0.35, Semantic: 0.65
    fusion := NewRRFFusion()

    // When: fusing results
    results := fusion.Fuse(bm25, vec, weights)

    // Then: results are ranked by RRF scores
    // A: BM25 rank 1, Vec rank 2 -> score calculation
    // ...
}
```

**Analysis**: Found test cases that demonstrate HOW the fusion works with concrete examples.

### Winner: Tie

Grep found the implementation signatures; MCP found usage examples. Both are useful for different purposes.

```mermaid
%%{init: {'theme': 'base'}}%%
flowchart LR
    subgraph Q3["Query: 'RRF fusion combine scores'"]
        direction TB
    end

    subgraph GrepWin["Grep Strength"]
        G1["func (f *RRFFusion) Fuse("]
        G2["func (f *RRFFusion) normalize("]
        G3["Exact function signatures"]
    end

    subgraph MCPWin["MCP Strength"]
        M1["TestRRFFusion_Basic showing:<br/>- Input: BM25=[A,B,C], Vec=[C,A,D]<br/>- How fusion combines them<br/>- Expected output behavior"]
    end

    Q3 --> GrepWin
    Q3 --> MCPWin

    GrepWin --> Use1["Use to: Jump to implementation"]
    MCPWin --> Use2["Use to: Understand behavior"]

    style Use1 fill:#e3f2fd
    style Use2 fill:#fff3e0
```

---

## Speed Benchmark

```bash
# 10 iterations each

# Grep
time (for i in {1..10}; do grep -rn "embedder" --include="*.go" . | head -1 > /dev/null; done)
# Result: 0.08s total = ~8ms/search

# amanmcp CLI
time (for i in {1..10}; do amanmcp search "embedder" --limit 1 > /dev/null; done)
# Result: 0.338s total = ~34ms/search
```

| Tool | 10 Searches | Per Search | Relative |
|------|-------------|------------|----------|
| Grep | 80ms | ~8ms | 1x (baseline) |
| amanmcp | 338ms | ~34ms | 4.2x slower |

**Analysis**: Grep is 4x faster, but both are well under the 100ms target for interactive use. The semantic quality gain justifies the latency increase.

```mermaid
%%{init: {'theme': 'base'}}%%
xychart-beta
    title "Search Latency Comparison (lower is better)"
    x-axis ["Grep (10 runs)", "amanmcp (10 runs)"]
    y-axis "Time (ms)" 0 --> 400
    bar [80, 338]
```

```mermaid
%%{init: {'theme': 'base'}}%%
pie showData
    title "Time Budget per Search"
    "Grep (~8ms)" : 8
    "amanmcp (~34ms)" : 34
    "Remaining to 100ms target" : 58
```

Both tools operate well within the 100ms interactive threshold. The 4x speed difference is imperceptible to users but the quality difference is significant.

---

## When to Use Each

```mermaid
%%{init: {'theme': 'base', 'themeVariables': { 'primaryColor': '#e8f5e9'}}}%%
flowchart TD
    Start["What are you searching for?"]

    Start --> Q1{"Do you know the<br/>exact text/pattern?"}

    Q1 -->|"Yes"| Grep["Use Grep/Glob"]
    Q1 -->|"No"| Q2{"Are you trying to<br/>understand a concept?"}

    Q2 -->|"Yes"| MCP["Use amanmcp MCP"]
    Q2 -->|"No"| Q3{"Looking for<br/>file by name?"}

    Q3 -->|"Yes"| Glob["Use Glob"]
    Q3 -->|"No"| MCP

    Grep --> GrepEx["Examples:<br/>• func NewClient\(<br/>• TODO:<br/>• import \"package\""]

    MCP --> MCPEx["Examples:<br/>• How does auth work?<br/>• Find retry logic<br/>• Error handling patterns"]

    Glob --> GlobEx["Examples:<br/>• **/*_test.go<br/>• src/**/*.ts<br/>• *.config.json"]

    style Grep fill:#e3f2fd
    style MCP fill:#c8e6c9
    style Glob fill:#fff3e0
```

### Use amanmcp MCP Tools When:

1. **Exploring unfamiliar code** - "How does authentication work?"
2. **Finding implementations by concept** - "Find retry logic"
3. **Understanding architecture** - "What handles database connections?"
4. **Learning a codebase** - Returns full context, not fragments

```mermaid
%%{init: {'theme': 'base'}}%%
flowchart LR
    subgraph Semantic["Semantic Understanding"]
        Q1["'cleanup resources'"] --> C1["Close()"]
        Q2["'retry with backoff'"] --> C2["Retry()"]
        Q3["'handle errors'"] --> C3["ErrorHandler"]
        Q4["'store data'"] --> C4["Repository"]
    end

    style C1 fill:#c8e6c9
    style C2 fill:#c8e6c9
    style C3 fill:#c8e6c9
    style C4 fill:#c8e6c9
```

MCP tools understand that concepts map to implementations, even when the words don't match exactly.

### Use Claude Code Grep/Glob When:

1. **Finding exact text** - `func NewClient\(`
2. **Counting occurrences** - How many times is X called?
3. **Confirming presence** - Does this file contain X?
4. **Fast iteration** - Quick confirmation during refactoring

```mermaid
%%{init: {'theme': 'base'}}%%
flowchart LR
    subgraph Pattern["Pattern Matching"]
        P1["'func NewClient'"] --> R1["func NewClient("]
        P2["'TODO:'"] --> R2["// TODO: fix this"]
        P3["'*.test.go'"] --> R3["auth_test.go<br/>client_test.go<br/>..."]
    end

    style R1 fill:#e3f2fd
    style R2 fill:#e3f2fd
    style R3 fill:#e3f2fd
```

Grep excels when you know exactly what text you're looking for - no interpretation needed.

---

## Key Insight: Different Questions, Different Tools

```mermaid
%%{init: {'theme': 'base'}}%%
flowchart TB
    subgraph GrepWorld["GREP: WHERE words appear"]
        direction LR
        GQ["Q: 'retry backoff'"]
        GA["A: Line 468:<br/>'// Exponential backoff: 100ms * 2^attempt'"]
        GQ --> GA
    end

    subgraph MCPWorld["MCP: WHAT implements the concept"]
        direction LR
        MQ["Q: 'retry logic with<br/>exponential backoff'"]
        MA["A: func Retry(...) {<br/>  delay := cfg.InitialDelay<br/>  for attempt := 0...<br/>    waitDelay *= cfg.Multiplier<br/>  ...<br/>}"]
        MQ --> MA
    end

    style GA fill:#ffcdd2
    style MA fill:#c8e6c9
```

The fundamental difference visualized:

```mermaid
%%{init: {'theme': 'base'}}%%
mindmap
  root((Search Tools))
    Grep/Glob
      Text Matching
        Exact patterns
        Regular expressions
        File globs
      Returns
        Line numbers
        Text fragments
        File paths
      Best for
        Known targets
        Counting
        Refactoring
    amanmcp MCP
      Semantic Understanding
        Concept matching
        Neural embeddings
        Hybrid BM25+Vector
      Returns
        Full functions
        Context
        Related code
      Best for
        Exploration
        Learning
        Architecture
```

---

## Recommendations for Claude Code Users

### 1. Default to MCP for Exploration

When you're trying to understand code, use MCP tools first:

```
User: "How does the search engine combine results?"

# Good approach:
mcp__amanmcp__search_code("RRF fusion combine results")
→ Returns full Fuse() implementation with context

# Less effective:
Grep("Fuse")
→ Returns line numbers where "Fuse" appears
```

### 2. Use Grep for Precision

When you know exactly what you're looking for:

```
User: "Find all calls to NewClient"

# Good approach:
Grep("NewClient\\(")
→ Fast, exhaustive list of all call sites

# Less effective:
mcp__amanmcp__search("NewClient function calls")
→ May return related but not exact matches
```

### 3. Combine Both for Comprehensive Understanding

```mermaid
%%{init: {'theme': 'base'}}%%
sequenceDiagram
    participant U as User
    participant MCP as amanmcp MCP
    participant G as Grep

    U->>MCP: "How does retry work?"
    MCP-->>U: func Retry() in internal/errors/retry.go<br/>(full implementation)

    Note over U: Now I understand the concept

    U->>G: Find all calls to errors.Retry
    G-->>U: 5 call sites found:<br/>• ollama.go:142<br/>• client.go:89<br/>• daemon.go:201<br/>• ...

    Note over U: Now I know WHERE it's used

    rect rgb(200, 230, 201)
        Note over U,G: Complete Understanding:<br/>WHAT it does + WHERE it's used
    end
```

```
# Step 1: Find the concept with MCP
mcp__amanmcp__search_code("retry with backoff")
→ Found: internal/errors/retry.go

# Step 2: Find all usages with Grep
Grep("errors.Retry\\(")
→ Found: 5 call sites across codebase

# Result: Understand both WHAT and WHERE
```

---

## Conclusion

The benchmark clearly shows that Claude Code's native Grep/Glob and amanmcp's MCP tools serve different purposes:

| Aspect | Grep/Glob | MCP Tools |
|--------|-----------|-----------|
| **Speed** | Faster (4x) | Acceptable (~34ms) |
| **Understanding** | Surface level | Deep semantic |
| **Results** | Line fragments | Full implementations |
| **Best for** | Known targets | Exploration |

```mermaid
%%{init: {'theme': 'base'}}%%
flowchart LR
    subgraph Before["Without MCP Tools"]
        B1["User asks question"] --> B2["Grep for keywords"]
        B2 --> B3["Read multiple files"]
        B3 --> B4["Piece together understanding"]
        B4 --> B5["Maybe find the answer"]
    end

    subgraph After["With MCP Tools"]
        A1["User asks question"] --> A2["MCP semantic search"]
        A2 --> A3["Get full implementation"]
        A3 --> A4["Understand immediately"]
    end

    style B5 fill:#ffcdd2
    style A4 fill:#c8e6c9
```

**The optimal strategy**: Use MCP tools for understanding and exploration, Grep for precision and confirmation. Having both available makes Claude Code significantly more effective at navigating codebases.

```mermaid
%%{init: {'theme': 'base'}}%%
graph TD
    subgraph Toolbox["Claude Code Search Toolbox"]
        MCP["amanmcp MCP<br/>🧠 Semantic Search"]
        Grep["Grep<br/>🔍 Pattern Match"]
        Glob["Glob<br/>📁 File Patterns"]
    end

    Task["Search Task"]
    Task --> Decision{"What type?"}

    Decision -->|"Understand concept"| MCP
    Decision -->|"Find exact text"| Grep
    Decision -->|"Find files"| Glob

    MCP --> Result["Comprehensive<br/>Codebase Navigation"]
    Grep --> Result
    Glob --> Result

    style Result fill:#c8e6c9
```

---

*Benchmark conducted: 2026-01-15 | amanmcp v0.8.2 | qwen3-embedding:0.6b*
