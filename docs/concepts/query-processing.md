# Query Processing

How AmanMCP analyzes your search query and chooses the best search strategy.

**Reading time:** 6 minutes
**Audience:** Users who want to understand query behavior
**Prerequisites:** [Hybrid Search Overview](hybrid-search/overview.md)

---

## Quick Summary

- Queries are **classified** by type (exact identifier, natural language, etc.)
- Classification determines **BM25/Vector weights**
- Quoted phrases use **exact matching**
- Natural language queries favor **semantic search**

---

## The Query Processing Flow

```mermaid
flowchart TB
    subgraph Input["Your Query"]
        Query["'authentication middleware'"]
    end

    subgraph Analysis["Query Analysis"]
        Classify["Classify Query Type"]
        Weights["Select Weights"]
        Prepare["Prepare for Search"]
    end

    subgraph Search["Parallel Search"]
        BM25["BM25 Search<br/>(keywords)"]
        Vector["Vector Search<br/>(semantic)"]
    end

    subgraph Fusion["Result Fusion"]
        RRF["RRF Combine<br/>(with weights)"]
    end

    subgraph Output["Results"]
        Results["Ranked Results"]
    end

    Query --> Classify --> Weights --> Prepare
    Prepare --> BM25
    Prepare --> Vector
    BM25 --> RRF
    Vector --> RRF
    RRF --> Results

    style Input fill:#e3f2fd,stroke:#1565c0
    style Analysis fill:#fff8e1,stroke:#ff8f00
    style Search fill:#e8f5e9,stroke:#2e7d32
    style Fusion fill:#f3e5f5,stroke:#7b1fa2
    style Output fill:#c8e6c9,stroke:#2e7d32
```

---

## Query Classification

AmanMCP analyzes your query to determine what type of search will work best.

### Classification Rules

```mermaid
flowchart TB
    Query["Input Query"] --> Check1{Quoted?<br/>'\"exact phrase\"'}

    Check1 -->|Yes| Quoted["QUOTED PHRASE<br/>BM25: 90%, Vector: 10%"]

    Check1 -->|No| Check2{Error code?<br/>ERR_XXX, E1001}

    Check2 -->|Yes| ErrorCode["ERROR CODE<br/>BM25: 80%, Vector: 20%"]

    Check2 -->|No| Check3{CamelCase?<br/>ProcessPayment}

    Check3 -->|Yes| Identifier["IDENTIFIER<br/>BM25: 70%, Vector: 30%"]

    Check3 -->|No| Check4{snake_case?<br/>process_payment}

    Check4 -->|Yes| Identifier

    Check4 -->|No| Check5{Natural language?<br/>how does X work}

    Check5 -->|Yes| Natural["NATURAL LANGUAGE<br/>BM25: 25%, Vector: 75%"]

    Check5 -->|No| Default["DEFAULT<br/>BM25: 35%, Vector: 65%"]

    style Quoted fill:#ffcc80
    style ErrorCode fill:#ffcc80
    style Identifier fill:#fff9c4
    style Natural fill:#c8e6c9
    style Default fill:#e1f5ff
```

### Weight Table

| Query Pattern | BM25 Weight | Vector Weight | Example |
|---------------|-------------|---------------|---------|
| Quoted phrase | 90% | 10% | `"exact match"` |
| Error code | 80% | 20% | `ERR_AUTH_FAILED` |
| CamelCase | 70% | 30% | `ValidateToken` |
| snake_case | 70% | 30% | `validate_token` |
| Path-like | 70% | 30% | `internal/auth` |
| Natural language | 25% | 75% | "how does auth work" |
| **Default** | 35% | 65% | Most queries |

---

## Classification Patterns

### 1. Quoted Phrases

```
Query: "authentication middleware"

Detection: Surrounded by quotes
Strategy: Nearly pure BM25 (exact match)
Why: User explicitly wants exact phrase

Result: Only chunks containing exactly "authentication middleware"
```

```mermaid
graph LR
    Q["\"authentication middleware\""]
    Q --> D["Detect: Quoted"]
    D --> W["Weights: 90/10"]
    W --> R["Exact phrase matches only"]

    style Q fill:#ffcc80
    style R fill:#c8e6c9
```

### 2. Error Codes

```
Query: ERR_CONNECTION_REFUSED

Detection: Matches pattern [A-Z]+_[A-Z0-9_]+ or E[0-9]+
Strategy: Heavy BM25
Why: Error codes are literal strings

Result: Finds exact error code definitions/handlers
```

```mermaid
graph LR
    Q["ERR_CONNECTION_REFUSED"]
    Q --> D["Detect: Error Code"]
    D --> W["Weights: 80/20"]
    W --> R["Error handling code"]

    style Q fill:#ffcc80
    style R fill:#c8e6c9
```

### 3. Code Identifiers

```
Query: ProcessPaymentWithRetry

Detection: CamelCase or snake_case pattern
Strategy: Keyword-heavy with some semantic
Why: Likely searching for specific function/type

Result: Finds exact function + semantically related
```

```mermaid
graph LR
    Q["ProcessPaymentWithRetry"]
    Q --> D["Detect: CamelCase"]
    D --> W["Weights: 70/30"]
    W --> R["Exact function + related payment code"]

    style Q fill:#fff9c4
    style R fill:#c8e6c9
```

### 4. Natural Language

```
Query: how does the authentication flow work

Detection: Question words, multiple common words
Strategy: Heavy semantic search
Why: User is exploring concepts, not exact code

Result: Auth-related code even with different terminology
```

```mermaid
graph LR
    Q["how does authentication flow work"]
    Q --> D["Detect: Natural Language"]
    D --> W["Weights: 25/75"]
    W --> R["Conceptually related code"]

    style Q fill:#c8e6c9
    style R fill:#c8e6c9
```

---

## Detection Patterns in Detail

### CamelCase Detection

```go
// Matches CamelCase identifiers
pattern: [A-Z][a-z]+([A-Z][a-z0-9]+)+

Examples that match:
- ProcessPayment     ✓
- ValidateUserInput  ✓
- HTTPServer         ✓

Examples that don't match:
- process_payment    ✗ (snake_case, different pattern)
- CONSTANT          ✗ (all caps)
- lowercase         ✗ (no caps)
```

### Natural Language Detection

```go
// Heuristics for natural language
signals:
- Starts with question word: "how", "what", "where", "why"
- Contains common words: "the", "does", "is", "work"
- Multiple words without code patterns
- No quotes, no special characters

Examples that match:
- "how does caching work"           ✓
- "where is auth handled"           ✓
- "error handling patterns"         ✓

Examples that don't match:
- "ValidateToken"                   ✗ (identifier)
- "\"exact phrase\""                ✗ (quoted)
- "ERR_AUTH_001"                    ✗ (error code)
```

---

## Query Preparation

After classification, the query is prepared for both search systems:

```mermaid
flowchart TB
    Query["Query: 'validate user authentication'"]

    Query --> BM25Prep["BM25 Preparation"]
    Query --> VectorPrep["Vector Preparation"]

    subgraph BM25["BM25 Processing"]
        Tokenize["Tokenize words"]
        Stem["Optional stemming"]
        BM25Query["BM25 query"]
    end

    subgraph Vector["Vector Processing"]
        Embed["Generate embedding"]
        Normalize["Normalize vector"]
        VectorQuery["Vector query"]
    end

    BM25Prep --> Tokenize --> Stem --> BM25Query
    VectorPrep --> Embed --> Normalize --> VectorQuery

    style Query fill:#e1f5ff
    style BM25 fill:#fff9c4
    style Vector fill:#c8e6c9
```

### BM25 Preparation

1. **Tokenization** - Split into words
2. **Case normalization** - Lower case for matching
3. **Stop word handling** - Keep technical terms
4. **Query construction** - Build FTS5 query

### Vector Preparation

1. **Embedding** - Convert query to 768-dim vector
2. **Normalization** - Unit length for cosine similarity
3. **Query construction** - Set up HNSW search parameters

---

## Dynamic Weight Adjustment

### Context-Aware Weights

The classifier can adjust weights based on context:

```mermaid
flowchart LR
    subgraph Signals["Context Signals"]
        S1["Query length"]
        S2["Technical vocabulary"]
        S3["Code patterns"]
        S4["Question structure"]
    end

    subgraph Adjustment["Weight Adjustment"]
        Short["Short query → +BM25"]
        Long["Long query → +Vector"]
        Tech["Technical terms → +BM25"]
        Question["Questions → +Vector"]
    end

    S1 --> Short
    S1 --> Long
    S2 --> Tech
    S4 --> Question

    style Signals fill:#e1f5ff
    style Adjustment fill:#fff9c4
```

### Examples

| Query | Signals | Final Weights |
|-------|---------|---------------|
| `ValidateToken` | Short, CamelCase | BM25: 70%, Vector: 30% |
| `how does token validation work with JWT` | Long, question, technical | BM25: 30%, Vector: 70% |
| `"func ProcessPayment"` | Quoted, identifier | BM25: 90%, Vector: 10% |

---

## Tips for Better Queries

### When to Use What

| Goal | Query Style | Example |
|------|-------------|---------|
| Find exact function | Use the function name | `ProcessPayment` |
| Find by error code | Use exact code | `ERR_AUTH_FAILED` |
| Explore a concept | Use natural language | "how does auth work" |
| Find exact phrase | Quote it | `"validate token"` |
| Find by file path | Include path | `internal/auth handler` |

### Query Refinement

```mermaid
flowchart TB
    Start["Initial Query<br/>'auth'"]

    Start --> TooMany{Too many<br/>results?}

    TooMany -->|Yes| AddContext["Add context<br/>'auth middleware jwt'"]
    TooMany -->|No| Check{Right<br/>results?}

    AddContext --> Check

    Check -->|No, too broad| MoreSpecific["Be more specific<br/>'ValidateJWTToken'"]
    Check -->|No, wrong type| ChangeStyle["Change style<br/>'how does jwt validation work'"]
    Check -->|Yes| Done["Found it! ✓"]

    MoreSpecific --> Check
    ChangeStyle --> Check

    style Start fill:#e1f5ff
    style Done fill:#c8e6c9
```

### Common Patterns

```
# Finding implementations
"authentication"           → Semantic: finds auth-related code
"AuthMiddleware"          → Exact: finds specific struct/func

# Finding usage
"how is AuthMiddleware used" → Semantic: finds call sites

# Finding definitions
"func AuthMiddleware"     → Exact: finds definition
"type AuthMiddleware"     → Exact: finds type definition

# Finding related code
"authentication retry"    → Balanced: finds auth + retry logic
```

---

## Debugging Query Classification

### Check How Your Query Was Classified

```bash
# Verbose search shows classification
amanmcp search "your query" --verbose

# Output:
# Query: "your query"
# Classification: NATURAL_LANGUAGE
# Weights: BM25=35%, Vector=65%
# BM25 results: 45
# Vector results: 38
# Fused results: 10
```

### Override Classification

```bash
# Force keyword-heavy search
amanmcp search "auth" --bm25-weight=0.8

# Force semantic-heavy search
amanmcp search "ValidateToken" --vector-weight=0.8

# Pure BM25 (no semantic)
amanmcp search "ERR_AUTH_001" --bm25-only
```

---

## Next Steps

| Want to... | Read |
|------------|------|
| Understand BM25 vs Vector deeply | [Hybrid Search](hybrid-search/) |
| Learn about ranking and scores | [Understanding Results](../tutorials/understanding-results.md) |
| See how caching affects queries | [Caching & Performance](caching-performance.md) |

---

*Smart query processing means you don't have to think about how to search - just search.*
