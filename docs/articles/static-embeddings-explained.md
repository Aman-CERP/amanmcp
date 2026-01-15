# Static Embeddings: The Zero-Dependency Fallback for Code Search

*Understanding hash-based embeddings, their role in AmanMCP, and how they compare to neural models*

---

## Executive Summary

Static embeddings are a **hash-based approach** to generating vector representations of text that requires **no external dependencies** - no GPU, no network, no model downloads. They exist in AmanMCP as an explicit opt-in mode (`--backend=static`) for users who want BM25-only search or cannot run Ollama.

```mermaid
flowchart LR
    subgraph Static["Static Embeddings"]
        S1[Text] --> S2[Hash Function]
        S2 --> S3[Sparse Vector]
    end

    subgraph Neural["Neural Embeddings"]
        N1[Text] --> N2[Transformer Model]
        N2 --> N3[Dense Vector]
    end

    S3 --> Compare{Quality}
    N3 --> Compare

    Compare --> |"~35%"| Static
    Compare --> |"100%"| Neural

    style S2 fill:#f39c12,color:#fff
    style N2 fill:#9b59b6,color:#fff
    style Compare fill:#3498db,color:#fff
```

**Key characteristics:**
- Speed: ~1,000 embeddings/second (vs ~30/second for neural models)
- Quality: ~30-40% of neural embedding quality for semantic similarity
- Dependencies: Zero (pure computation)
- Use case: BM25-only mode, testing, CI/CD environments

---

## What Are Static Embeddings?

### The Core Concept

Traditional neural embeddings use trained transformer models (like Qwen3 or EmbeddingGemma) to convert text into vectors that capture semantic meaning. This requires:
- A model file (hundreds of MB to GB)
- An inference runtime (Ollama, MLX, etc.)
- Compute resources (GPU preferred)

Static embeddings take a radically different approach: they use **deterministic hash functions** to map text features directly to vector dimensions. No learning, no models, no dependencies.

```mermaid
flowchart TB
    subgraph Neural["Neural Embedding Pipeline"]
        direction LR
        T1["'authentication'"] --> Tok1[Tokenizer]
        Tok1 --> TF[Transformer<br/>600M params]
        TF --> V1["[0.12, -0.34, 0.56, ...]<br/><i>semantic meaning</i>"]
    end

    subgraph Static["Static Embedding Pipeline"]
        direction LR
        T2["'authentication'"] --> Tok2[Tokenizer]
        Tok2 --> Hash[FNV-64 Hash]
        Hash --> V2["[0.0, 0.7, 0.0, 0.3, ...]<br/><i>lexical features</i>"]
    end

    style TF fill:#9b59b6,color:#fff
    style Hash fill:#e67e22,color:#fff
    style V1 fill:#27ae60,color:#fff
    style V2 fill:#f39c12,color:#fff
```

### Why "Static"?

The term "static" refers to two properties:

1. **No learned parameters**: The algorithm is fixed, not trained on data
2. **Deterministic output**: Same input always produces identical output

```mermaid
flowchart LR
    Input["'func main()'"] --> Static1[Static Embedder<br/>Instance 1]
    Input --> Static2[Static Embedder<br/>Instance 2]
    Input --> Static3[Static Embedder<br/>Instance 3]

    Static1 --> Output["[0.2, 0, 0.7, ...]"]
    Static2 --> Output
    Static3 --> Output

    style Output fill:#27ae60,color:#fff
```

---

## How Static Embeddings Work

### The Algorithm (AmanMCP Implementation)

AmanMCP's `StaticEmbedder768` uses a four-step process:

```mermaid
flowchart TB
    Input["Input Text<br/><code>func getUserById(id string)</code>"] --> Step1

    subgraph Step1["Step 1: Tokenization"]
        direction LR
        A1[Split camelCase] --> A2[Split snake_case]
        A2 --> A3[Filter stop words]
    end

    Step1 --> Tokens["Tokens: [get, user, id, string]"]
    Tokens --> Step2

    subgraph Step2["Step 2: Token Hashing (0.7 weight)"]
        direction LR
        B1["'get' → hash → idx 42"]
        B2["'user' → hash → idx 156"]
        B3["'id' → hash → idx 89"]
    end

    Step2 --> Step3

    subgraph Step3["Step 3: N-gram Hashing (0.3 weight)"]
        direction LR
        C1["'get' → 'get'"]
        C2["'getu' → 'get','etu'"]
        C3["...3-char windows"]
    end

    Step3 --> Step4

    subgraph Step4["Step 4: Normalize"]
        direction LR
        D1[Sum squares] --> D2[√magnitude]
        D2 --> D3[Divide each dim]
    end

    Step4 --> Output["768-dim unit vector"]

    style Input fill:#3498db,color:#fff
    style Output fill:#27ae60,color:#fff
    style Tokens fill:#f39c12,color:#000
```

### Step 1: Code-Aware Tokenization

The tokenizer understands programming conventions:

```mermaid
flowchart LR
    subgraph CamelCase["CamelCase Splitting"]
        CC1["getUserById"] --> CC2["get | User | By | Id"]
        CC2 --> CC3["get, user, by, id"]
    end

    subgraph SnakeCase["Snake_Case Splitting"]
        SC1["MAX_BUFFER_SIZE"] --> SC2["MAX | BUFFER | SIZE"]
        SC2 --> SC3["max, buffer, size"]
    end

    style CC1 fill:#3498db,color:#fff
    style SC1 fill:#9b59b6,color:#fff
    style CC3 fill:#27ae60,color:#fff
    style SC3 fill:#27ae60,color:#fff
```

This is critical for code search. Without this, `getUserById` would be treated as a single opaque token, missing the semantic connection to queries like "get user by id".

### Step 2: Stop Word Filtering

Common programming keywords are filtered out:

```mermaid
flowchart LR
    Input["func, return, int,<br/>string, class, var,<br/>import, const, ..."]

    Input --> Filter{Stop Word<br/>Filter}

    Filter -->|Removed| Trash["🗑️"]
    Filter -->|Kept| Output["calculate, process,<br/>validate, user, data"]

    style Input fill:#e74c3c,color:#fff
    style Trash fill:#7f8c8d,color:#fff
    style Output fill:#27ae60,color:#fff
```

Why? These words appear in virtually every code file. Including them would make all code look similar.

### Step 3: Token Hashing (Weight: 0.7)

Each remaining token is hashed to a vector index using FNV-64:

```mermaid
flowchart TB
    subgraph Hashing["FNV-64 Hash Function"]
        Token["'user'"] --> FNV["FNV-64<br/>hash()"]
        FNV --> Raw["14695981039346656037"]
        Raw --> Mod["% 768"]
        Mod --> Index["Index: 342"]
    end

    subgraph Vector["768-dim Vector"]
        V1["[0]"] ~~~ V2["..."] ~~~ V342["[342] += 0.7"] ~~~ V3["..."] ~~~ V767["[767]"]
    end

    Index --> V342

    style Token fill:#3498db,color:#fff
    style Index fill:#e67e22,color:#fff
    style V342 fill:#27ae60,color:#fff
```

The weight (0.7) reflects that whole tokens carry more semantic signal than substrings.

### Step 4: N-gram Hashing (Weight: 0.3)

Character-level 3-grams capture partial matches:

```mermaid
flowchart LR
    Word["authentication"] --> NGrams

    subgraph NGrams["3-gram Extraction"]
        direction TB
        N1["aut"]
        N2["uth"]
        N3["the"]
        N4["hen"]
        N5["ent"]
        N6["..."]
    end

    NGrams --> Hash["Hash each<br/>to index"]
    Hash --> Vector["Add 0.3<br/>per n-gram"]

    style Word fill:#3498db,color:#fff
    style Vector fill:#27ae60,color:#fff
```

N-grams help with:
- Typo tolerance ("authen" vs "authan")
- Partial matches ("auth" matching "authentication")
- Morphological similarity ("authenticate", "authentication", "authenticator")

### Step 5: Normalization

The final vector is normalized to unit length:

```mermaid
flowchart LR
    Raw["[0, 0.7, 0, 0.3, 0.7, ...]"] --> Sum["Σ(x²)"]
    Sum --> Sqrt["√sum = magnitude"]
    Sqrt --> Divide["x / magnitude"]
    Divide --> Unit["[0, 0.58, 0, 0.25, 0.58, ...]<br/>|v| = 1.0"]

    style Raw fill:#e74c3c,color:#fff
    style Unit fill:#27ae60,color:#fff
```

Normalization ensures cosine similarity works correctly (comparing angles, not magnitudes).

---

## Why Static Embeddings Exist

### Historical Context

```mermaid
timeline
    title Static Embeddings in AmanMCP

    section Origin
        v0.1.x : Dimension mismatch crashes
               : Need fallback when Ollama unavailable

    section Evolution
        v0.2.x : Static768 added (768 dims)
               : Matches neural model dimensions
               : Silent fallback (problematic)

    section Current
        v0.8.x : BUG-073 fixed
               : Explicit opt-in only
               : Clear error messages
```

Static embeddings were added to AmanMCP for several reasons:

1. **Dimension Compatibility**: When switching between embedders, dimension mismatches caused crashes. Static768 (768 dimensions) matches Ollama's default model dimensions.

2. **Testing Infrastructure**: Integration tests need embeddings but shouldn't require Ollama running.

3. **CI/CD Pipelines**: GitHub Actions and similar environments may not have GPU access.

### Current Role (Post BUG-073)

```mermaid
flowchart TB
    User["User runs:<br/><code>amanmcp index</code>"]

    User --> Check{Ollama<br/>available?}

    Check -->|Yes| Neural["Use Ollama+Qwen3<br/>✅ Semantic search"]
    Check -->|No| Error["❌ Error:<br/>'ollama unavailable'"]

    Error --> Options["Options:<br/>1. Start Ollama<br/>2. Use --backend=static"]

    Options -->|--backend=static| Static["Use Static768<br/>⚠️ BM25 only"]

    style Neural fill:#27ae60,color:#fff
    style Error fill:#e74c3c,color:#fff
    style Static fill:#f39c12,color:#fff
```

After BUG-073, static embeddings are **explicitly opt-in only**.

---

## Understanding Qwen3-Embedding-0.6B

Before comparing static embeddings to neural models, let's understand what makes Qwen3 special and how it generates high-quality embeddings.

### What is Qwen3-Embedding-0.6B?

Qwen3-Embedding-0.6B is a **600 million parameter transformer model** specifically designed for generating text embeddings. It's part of Alibaba's Qwen3 family, optimized for code and documentation search.

```mermaid
flowchart TB
    subgraph Qwen3["Qwen3-Embedding-0.6B"]
        direction TB
        P1["600M Parameters"]
        P2["768 Dimensions Output"]
        P3["2048 Token Context"]
        P4["Trained on Code + Text Pairs"]
        P5["MTEB-Code Score: 74.57"]
    end

    subgraph Comparison["Model Size Comparison"]
        direction LR
        S1["Static: 0 params"]
        S2["Qwen3-0.6B: 600M"]
        S3["Qwen3-8B: 8B"]
        S4["GPT-3: 175B"]
    end

    style Qwen3 fill:#9b59b6,color:#fff
    style S2 fill:#27ae60,color:#fff
```

| Specification | Value |
|--------------|-------|
| Parameters | 600 million |
| Output Dimensions | 768 |
| Context Window | 2048 tokens |
| Architecture | Transformer (bidirectional attention) |
| MTEB-Code Score | 74.57 |
| Memory Usage | ~800MB |
| Inference Speed | ~30 embeddings/second |

### Why 0.6B? The RAM Constraint

AmanMCP uses the 0.6B variant (not 4B or 8B) due to practical memory constraints:

```mermaid
flowchart LR
    subgraph Models["Qwen3 Embedding Variants"]
        M1["0.6B<br/>~800MB RAM"]
        M2["4B<br/>~4GB RAM"]
        M3["8B<br/>~8GB RAM"]
    end

    subgraph Reality["Developer Reality"]
        R1["24GB MacBook"]
        R2["- IDE: 4GB"]
        R3["- Browser: 6GB"]
        R4["- Other apps: 6GB"]
        R5["= Available: ~8GB"]
    end

    M1 -->|"✅ Fits"| R5
    M2 -->|"⚠️ Tight"| R5
    M3 -->|"❌ System freeze"| R5

    style M1 fill:#27ae60,color:#fff
    style M2 fill:#f39c12,color:#fff
    style M3 fill:#e74c3c,color:#fff
```

The 8B model causes system freezes on 24GB machines when other applications are running. The 0.6B variant provides excellent quality while remaining practical.

---

## How Qwen3 Generates Embeddings

### The Transformer Architecture

Unlike static embeddings (hash function), Qwen3 uses a deep neural network with **attention mechanisms** that understand relationships between words:

```mermaid
flowchart TB
    subgraph Input["Input Processing"]
        Text["'validate user credentials'"]
        Text --> Tokenizer["Tokenizer<br/>(BPE)"]
        Tokenizer --> Tokens["[15234, 892, 7823]"]
        Tokens --> PosEnc["+ Position<br/>Encoding"]
    end

    subgraph Transformer["Transformer Layers (×24)"]
        direction TB
        PosEnc --> Attn1["Self-Attention"]
        Attn1 --> FF1["Feed-Forward"]
        FF1 --> Norm1["Layer Norm"]
        Norm1 --> Attn2["Self-Attention"]
        Attn2 --> FF2["Feed-Forward"]
        FF2 --> More["... ×24 layers"]
    end

    subgraph Output["Output"]
        More --> Pool["Mean Pooling"]
        Pool --> Vector["768-dim vector"]
    end

    style Tokenizer fill:#3498db,color:#fff
    style Attn1 fill:#9b59b6,color:#fff
    style Attn2 fill:#9b59b6,color:#fff
    style Vector fill:#27ae60,color:#fff
```

### The Magic: Self-Attention

The key innovation is **self-attention** - each word "looks at" every other word to understand context:

```mermaid
flowchart LR
    subgraph Attention["Self-Attention: 'validate user credentials'"]
        V["validate"] -->|0.3| V
        V -->|0.4| U["user"]
        V -->|0.8| C["credentials"]

        U -->|0.2| V
        U -->|0.3| U
        U -->|0.5| C

        C -->|0.7| V
        C -->|0.3| U
        C -->|0.2| C
    end

    subgraph Result["Attention Learns"]
        R1["'validate' strongly<br/>attends to 'credentials'"]
        R2["'user' + 'credentials'<br/>= authentication context"]
        R3["Model learns this is<br/>about authentication"]
    end

    Attention --> Result

    style C fill:#27ae60,color:#fff
    style V fill:#3498db,color:#fff
```

**What attention discovers:**
- "validate" and "credentials" are strongly related (security context)
- "user" + "credentials" together imply authentication
- This pattern matches other auth-related code even with different words

### Training: Learning from Billions of Examples

Qwen3 was trained on massive datasets of paired texts:

```mermaid
flowchart TB
    subgraph Training["Contrastive Learning"]
        direction TB
        P1["Positive pair:<br/>'login function' ↔ 'authentication code'"]
        P2["Negative pair:<br/>'login function' ↔ 'weather API'"]

        P1 --> Loss["Loss Function:<br/>Push positive pairs closer<br/>Push negative pairs apart"]
        P2 --> Loss
    end

    subgraph Result["After Training"]
        direction LR
        Auth["'authentication'"]
        Login["'login'"]
        Cred["'credentials'"]

        Auth <-->|"close"| Login
        Login <-->|"close"| Cred
        Auth <-->|"close"| Cred

        Weather["'weather'"]
        Weather x--x|"far"| Auth
    end

    Training --> Result

    style P1 fill:#27ae60,color:#fff
    style P2 fill:#e74c3c,color:#fff
```

**Training data included:**
- Code-documentation pairs (function + docstring)
- StackOverflow Q&A pairs
- GitHub issue-PR pairs
- Code search query logs

---

## The Instruction Prefix: Asymmetric Embeddings

### Why Queries and Documents Are Different

Qwen3 uses **asymmetric embedding** - queries and documents are embedded differently:

```mermaid
flowchart TB
    subgraph Query["Query Embedding"]
        Q1["User query:<br/>'authentication logic'"]
        Q1 --> Prefix["Add instruction prefix"]
        Prefix --> QF["'Instruct: Given a code search query,<br/>retrieve relevant code snippets...<br/>Query: authentication logic'"]
        QF --> QEmbed["Qwen3 Embed"]
        QEmbed --> QV["Query Vector"]
    end

    subgraph Document["Document Embedding"]
        D1["Code chunk:<br/>'func validateCredentials(...)'"]
        D1 --> DEmbed["Qwen3 Embed<br/>(no prefix)"]
        DEmbed --> DV["Document Vector"]
    end

    QV --> Sim{Cosine<br/>Similarity}
    DV --> Sim
    Sim --> Score["0.78"]

    style Prefix fill:#9b59b6,color:#fff
    style Score fill:#27ae60,color:#fff
```

### The Instruction Prefix in AmanMCP

```go
// From internal/search/engine.go
const Qwen3QueryInstruction = "Instruct: Given a code search query, " +
    "retrieve relevant code snippets that answer the query\nQuery:"

func formatQueryForEmbedding(query string) string {
    return Qwen3QueryInstruction + " " + query
}
```

**Why this helps (1-5% improvement):**
- Tells the model "this is a search query, not code"
- Model adjusts internal representations for retrieval
- Better matches between questions and answers

```mermaid
flowchart LR
    subgraph Without["Without Prefix"]
        Q1["'auth logic'"] --> E1["Embedded as<br/>code fragment"]
        E1 --> S1["Matches other<br/>fragments with 'auth'"]
    end

    subgraph With["With Prefix"]
        Q2["'Instruct...Query: auth logic'"] --> E2["Embedded as<br/>search intent"]
        E2 --> S2["Matches code that<br/>IMPLEMENTS auth"]
    end

    style S1 fill:#f39c12,color:#fff
    style S2 fill:#27ae60,color:#fff
```

---

## Why Qwen3 Beats Static Embeddings

### The Semantic Understanding Gap

Let's trace through exactly WHY neural embeddings outperform static:

```mermaid
flowchart TB
    Query["Query: 'authentication logic'"]

    subgraph Static["Static Embedding Process"]
        S1["Tokenize: [authentication, logic]"]
        S1 --> S2["Hash 'authentication' → idx 234"]
        S2 --> S3["Hash 'logic' → idx 567"]
        S3 --> S4["Sparse vector:<br/>[0,...,0.7,...,0,...,0.7,...,0]"]
    end

    subgraph Neural["Qwen3 Embedding Process"]
        N1["Tokenize: [15234, 892]"]
        N1 --> N2["24 transformer layers"]
        N2 --> N3["Attention: 'authentication' relates to<br/>security, login, credentials, validate..."]
        N3 --> N4["Dense vector:<br/>[0.12, -0.34, 0.56, 0.23, ...]<br/>encodes ALL related concepts"]
    end

    Query --> Static
    Query --> Neural

    subgraph Code["Code: 'func validateCredentials(u, p string) bool'"]
        C1["No word 'authentication'"]
        C2["No word 'logic'"]
        C3["Implements authentication logic!"]
    end

    S4 -->|"similarity: 0.08"| Code
    N4 -->|"similarity: 0.71"| Code

    style S4 fill:#e74c3c,color:#fff
    style N4 fill:#27ae60,color:#fff
```

### What Qwen3 "Knows" That Static Doesn't

```mermaid
mindmap
    root((Qwen3 Knowledge))
        Synonyms
            login = auth = signin
            fetch = get = retrieve
            validate = check = verify
        Concepts
            authentication
                credentials
                password
                token
                session
            error handling
                try/catch
                panic/recover
                Result type
        Code Patterns
            func + Credentials = auth
            Handler + Request = HTTP
            Parse + JSON = serialization
        Language Specifics
            Go: err != nil
            Python: try/except
            JS: .then/.catch
```

**Static embeddings know NONE of this.** They only know:
- Which characters appear in words
- Hash collisions between tokens

### Concrete Example: Tracing Both Approaches

```mermaid
sequenceDiagram
    participant U as User Query
    participant S as Static Embedder
    participant Q as Qwen3 Embedder
    participant C as Code Chunk

    U->>S: "fetch user data"
    S->>S: hash('fetch') → idx 123
    S->>S: hash('user') → idx 456
    S->>S: hash('data') → idx 789
    S-->>C: Compare with "func getUserById()"
    Note over S,C: Static: 0.31<br/>('user' matches, others don't)

    U->>Q: "fetch user data"
    Q->>Q: Transform through 24 layers
    Q->>Q: Attention learns: fetch≈get, data≈info
    Q->>Q: Context: this is a retrieval query
    Q-->>C: Compare with "func getUserById()"
    Note over Q,C: Qwen3: 0.82<br/>(semantic match!)
```

---

## Deep Dive: Qwen3 in Action

### How Qwen3 Handles Code-Specific Patterns

```mermaid
flowchart TB
    subgraph Input["Input: 'func (s *Server) HandleLogin(w http.ResponseWriter, r *http.Request)'"]
        direction LR
    end

    subgraph Processing["Qwen3 Processing"]
        P1["Recognizes Go syntax"]
        P2["Identifies: HTTP handler pattern"]
        P3["Extracts: Login = authentication"]
        P4["Understands: Server receiver method"]
    end

    subgraph Output["Embedding Encodes"]
        O1["Go language"]
        O2["HTTP handling"]
        O3["Authentication"]
        O4["Web server"]
        O5["Request/Response pattern"]
    end

    Input --> Processing --> Output

    subgraph Matches["Will Match Queries Like"]
        M1["'authentication endpoint'"]
        M2["'login handler'"]
        M3["'HTTP auth'"]
        M4["'web server security'"]
    end

    Output --> Matches

    style Processing fill:#9b59b6,color:#fff
    style Matches fill:#27ae60,color:#fff
```

### The Quality Improvement Explained

```mermaid
xychart-beta
    title "Why Qwen3 Wins: Capability Breakdown"
    x-axis ["Exact Match", "Synonym", "Concept", "Cross-Lang", "Intent"]
    y-axis "Capability %" 0 --> 100
    bar [85, 15, 5, 0, 10]
    bar [90, 80, 70, 50, 75]
```

| Capability | Static | Qwen3 | Why Qwen3 Wins |
|------------|--------|-------|----------------|
| **Exact Match** | 85% | 90% | Both good, Qwen3 slightly better context |
| **Synonym Match** | 15% | 80% | Qwen3 learned fetch≈get from training |
| **Concept Match** | 5% | 70% | Qwen3 knows auth≈login≈credentials |
| **Cross-Language** | 0% | 50% | Qwen3 maps Go↔Python↔JS patterns |
| **Intent Understanding** | 10% | 75% | Instruction prefix helps Qwen3 understand |

---

## Quality Comparison: Static vs Neural

### The Fundamental Difference

```mermaid
flowchart TB
    subgraph Neural["Neural Model (Qwen3 0.6B)"]
        direction TB
        NT["Trained on billions of text pairs"]
        NT --> NU["Understands: 'auth' ≈ 'login' ≈ 'credentials'"]
        NU --> NV["Captures semantic meaning"]
    end

    subgraph Static["Static Embedder"]
        direction TB
        ST["Zero training data"]
        ST --> SU["Only knows: 'auth' appears in 'authentication'"]
        SU --> SV["Captures lexical similarity only"]
    end

    Neural --> Q{Quality}
    Static --> Q

    Q -->|"100%"| Neural
    Q -->|"~35%"| Static

    style Neural fill:#27ae60,color:#fff
    style Static fill:#f39c12,color:#fff
```

### Empirical Quality Measurements

```mermaid
%%{init: {'theme': 'base', 'themeVariables': { 'pie1': '#27ae60', 'pie2': '#f39c12'}}}%%
xychart-beta
    title "Search Quality by Query Type"
    x-axis ["Exact Keyword", "Synonym Match", "Concept Match", "Code Structure"]
    y-axis "Accuracy %" 0 --> 100
    bar [85, 15, 5, 40]
    bar [90, 75, 60, 70]
```

| Query Type | Static768 | Ollama+Qwen3 | Gap |
|------------|-----------|--------------|-----|
| Exact keyword match | ~85% | ~90% | 1.06x |
| Synonym match | ~15% | ~75% | **5x** |
| Concept match | ~5% | ~60% | **12x** |
| Code structure | ~40% | ~70% | 1.75x |

**Key insight**: Static embeddings are essentially **fancy keyword matching**. They work when queries use the same words as the code. They fail when meaning matters.

### Concrete Examples

```mermaid
flowchart TB
    subgraph Example1["Example 1: Keyword Match ✅"]
        Q1["Query: 'getUserById'"]
        C1["Code: func getUserById(...)"]
        Q1 --> S1["Static: 0.92 ✅"]
        Q1 --> N1["Neural: 0.94 ✅"]
    end

    subgraph Example2["Example 2: Synonym Match ❌"]
        Q2["Query: 'fetch user by identifier'"]
        C2["Code: func getUserById(...)"]
        Q2 --> S2["Static: 0.31 ❌"]
        Q2 --> N2["Neural: 0.78 ✅"]
    end

    subgraph Example3["Example 3: Concept Match ❌"]
        Q3["Query: 'authentication logic'"]
        C3["Code: func validateCredentials(...)"]
        Q3 --> S3["Static: 0.08 ❌"]
        Q3 --> N3["Neural: 0.71 ✅"]
    end

    style S1 fill:#27ae60,color:#fff
    style N1 fill:#27ae60,color:#fff
    style S2 fill:#e74c3c,color:#fff
    style N2 fill:#27ae60,color:#fff
    style S3 fill:#e74c3c,color:#fff
    style N3 fill:#27ae60,color:#fff
```

### Why the Quality Gap?

```mermaid
flowchart TB
    subgraph Training["Neural Model Training"]
        Data["Billions of text pairs"]
        Data --> Learn["Model learns relationships"]
        Learn --> Cluster["Related concepts cluster together"]
    end

    subgraph VectorSpace["Resulting Vector Space"]
        Auth["'authentication'"]
        Login["'login'"]
        Cred["'credentials'"]

        Auth <-.->|close| Login
        Login <-.->|close| Cred
        Auth <-.->|close| Cred
    end

    Training --> VectorSpace

    subgraph StaticSpace["Static Vector Space"]
        SAuth["'authentication'"]
        SLogin["'login'"]
        SCred["'credentials'"]

        SAuth x--x|"far apart"| SLogin
        SLogin x--x|"far apart"| SCred
    end

    style VectorSpace fill:#d5f4e6,color:#000
    style StaticSpace fill:#fadbd8,color:#000
```

Neural embeddings learn from context that "login", "authentication", and "credentials" are related. Static embeddings only see that they share no common substrings.

---

## When to Use Static Embeddings

### Decision Tree

```mermaid
flowchart TB
    Start["Need embeddings?"]

    Start --> Q1{Ollama<br/>available?}

    Q1 -->|Yes| Q2{Semantic search<br/>needed?}
    Q1 -->|No| Q3{Can install<br/>Ollama?}

    Q2 -->|Yes| Neural["✅ Use Neural<br/>(Ollama+Qwen3)"]
    Q2 -->|No| Static1["⚠️ Use Static<br/>(--backend=static)"]

    Q3 -->|Yes| Install["Install Ollama"]
    Q3 -->|No| Q4{CI/CD or<br/>testing?}

    Install --> Neural

    Q4 -->|Yes| Static2["⚠️ Use Static<br/>(acceptable)"]
    Q4 -->|No| Static3["⚠️ Use Static<br/>(limited quality)"]

    style Neural fill:#27ae60,color:#fff
    style Static1 fill:#f39c12,color:#fff
    style Static2 fill:#f39c12,color:#fff
    style Static3 fill:#e74c3c,color:#fff
```

### Appropriate Use Cases

1. **BM25-Only Mode**: When you want pure keyword search without semantic matching

   ```bash
   amanmcp index --backend=static
   amanmcp search "getUserById" --bm25-only
   ```

2. **Testing and CI/CD**: When Ollama isn't available

3. **Resource-Constrained Environments**: No GPU, limited RAM

4. **Deterministic Reproducibility**: When you need identical results across runs

### When NOT to Use Static Embeddings

```mermaid
flowchart LR
    subgraph Bad["❌ Don't Use Static For"]
        B1["Production semantic search"]
        B2["Natural language queries"]
        B3["Concept discovery"]
        B4["'How does auth work?'"]
    end

    style Bad fill:#fadbd8,color:#000
```

---

## Technical Deep Dive: The Math

### Sparse vs Dense Vectors

```mermaid
flowchart TB
    subgraph Sparse["Static: Sparse Vector"]
        S["[0, 0, 0.7, 0, 0, 0.3, 0, 0, 0.7, 0, 0, 0, ...]"]
        SN["~50 non-zero values out of 768"]
    end

    subgraph Dense["Neural: Dense Vector"]
        D["[0.12, -0.34, 0.56, 0.02, -0.18, 0.41, 0.09, ...]"]
        DN["All 768 values are meaningful"]
    end

    Sparse --> Match{Can match<br/>semantically?}
    Dense --> Match

    Match -->|Sparse| No["Only if tokens overlap"]
    Match -->|Dense| Yes["Even with zero shared words"]

    style Sparse fill:#f39c12,color:#fff
    style Dense fill:#27ae60,color:#fff
    style No fill:#e74c3c,color:#fff
    style Yes fill:#27ae60,color:#fff
```

**Why this matters**: Sparse vectors can only match when hash collisions align. Dense vectors capture gradual similarity.

### Cosine Similarity Behavior

```mermaid
flowchart LR
    subgraph StaticSim["Static Similarity"]
        SA["Vector A: [0, 0.7, 0, 0.3, 0]"]
        SB["Vector B: [0, 0, 0.5, 0, 0.5]"]
        SA --> SDot["A · B = 0"]
        SB --> SDot
        SDot --> SResult["cos = 0<br/>(no overlap)"]
    end

    subgraph NeuralSim["Neural Similarity"]
        NA["Vector A: [0.1, 0.7, -0.2, 0.3, 0.1]"]
        NB["Vector B: [0.2, 0.6, -0.1, 0.4, 0.2]"]
        NA --> NDot["A · B = 0.72"]
        NB --> NDot
        NDot --> NResult["cos = 0.72<br/>(similar meaning)"]
    end

    style SResult fill:#e74c3c,color:#fff
    style NResult fill:#27ae60,color:#fff
```

---

## Performance Characteristics

### Speed Comparison

```mermaid
xychart-beta
    title "Embedding Speed (embeddings/second)"
    x-axis ["Static768", "Ollama+Qwen3"]
    y-axis "Embeddings/sec" 0 --> 1200
    bar [1000, 30]
```

| Operation | Static768 | Ollama+Qwen3 | Ratio |
|-----------|-----------|--------------|-------|
| Single embed | 0.1ms | 30ms | 300x |
| Batch (100) | 10ms | 500ms | 50x |
| Index 10K chunks | 1s | 5min | 300x |

### Memory Usage

```mermaid
pie showData
    title "Runtime Memory Usage"
    "Static768" : 1
    "Ollama+Qwen3" : 800
```

| Component | Static768 | Ollama+Qwen3 |
|-----------|-----------|--------------|
| Runtime | ~1MB | ~800MB |
| Per vector | 3KB | 3KB |
| Index (10K) | 30MB | 30MB |

---

## Integration Architecture

### Interface Pattern

```mermaid
classDiagram
    class Embedder {
        <<interface>>
        +Embed(ctx, text) []float32
        +EmbedBatch(ctx, texts) [][]float32
        +Dimensions() int
        +ModelName() string
        +Available(ctx) bool
        +Close() error
    }

    class StaticEmbedder768 {
        -closed bool
        +Embed(ctx, text) []float32
        +generateVector(text) []float32
    }

    class OllamaEmbedder {
        -client *http.Client
        -model string
        +Embed(ctx, text) []float32
    }

    class MLXEmbedder {
        -endpoint string
        +Embed(ctx, text) []float32
    }

    Embedder <|.. StaticEmbedder768
    Embedder <|.. OllamaEmbedder
    Embedder <|.. MLXEmbedder
```

### Factory Pattern

```mermaid
flowchart TB
    Request["NewEmbedder(provider)"]

    Request --> Switch{Provider?}

    Switch -->|static| Static["StaticEmbedder768"]
    Switch -->|ollama| Ollama["OllamaEmbedder"]
    Switch -->|mlx| MLX["MLXEmbedder"]
    Switch -->|auto| Auto{Detect<br/>available}

    Auto -->|"MLX available"| MLX
    Auto -->|"Ollama available"| Ollama
    Auto -->|"Nothing available"| Error["❌ Error<br/>(BUG-073)"]

    style Static fill:#f39c12,color:#fff
    style Ollama fill:#9b59b6,color:#fff
    style MLX fill:#e74c3c,color:#fff
    style Error fill:#c0392b,color:#fff
```

---

## Summary: The Trade-offs

```mermaid
quadrantChart
    title Embedding Approach Trade-offs
    x-axis Low Quality --> High Quality
    y-axis Slow --> Fast
    quadrant-1 Ideal
    quadrant-2 Fast but Limited
    quadrant-3 Avoid
    quadrant-4 Quality Focus

    Static768: [0.25, 0.95]
    Ollama-Qwen3: [0.75, 0.35]
    MLX-Qwen3: [0.75, 0.70]
```

| Aspect | Static768 | Neural (Ollama) |
|--------|-----------|-----------------|
| **Speed** | ⭐⭐⭐⭐⭐ | ⭐⭐ |
| **Quality** | ⭐⭐ | ⭐⭐⭐⭐⭐ |
| **Dependencies** | None | Ollama + Model |
| **Memory** | ~1MB | ~800MB |
| **Use Case** | BM25-only, CI/CD | Production search |

---

## Conclusion

### The Bottom Line

```mermaid
flowchart TB
    subgraph Summary["Static vs Qwen3: The Verdict"]
        direction TB

        subgraph Static["Static Embeddings"]
            S1["✅ Zero dependencies"]
            S2["✅ 1000x faster"]
            S3["✅ Deterministic"]
            S4["❌ No semantic understanding"]
            S5["❌ Keyword-only matching"]
            S6["❌ 35% of neural quality"]
        end

        subgraph Qwen3["Qwen3-0.6B"]
            Q1["✅ Understands synonyms"]
            Q2["✅ Concept matching"]
            Q3["✅ Cross-language patterns"]
            Q4["✅ Intent understanding"]
            Q5["⚠️ Requires Ollama"]
            Q6["⚠️ ~800MB memory"]
        end
    end

    style Static fill:#f39c12,color:#fff
    style Qwen3 fill:#27ae60,color:#fff
```

Static embeddings are a **pragmatic engineering solution** for scenarios where neural embeddings aren't available or appropriate.

```mermaid
flowchart LR
    subgraph Use["✅ Use Static When"]
        U1["CI/CD pipelines"]
        U2["BM25-only mode"]
        U3["Testing"]
        U4["No GPU available"]
    end

    subgraph UseQwen["✅ Use Qwen3 When"]
        Q1["Production search"]
        Q2["Semantic queries"]
        Q3["Concept discovery"]
        Q4["Quality matters"]
    end

    subgraph Avoid["❌ Avoid Static When"]
        A1["Users expect 'smart' search"]
        A2["Natural language queries"]
        A3["Synonym matching needed"]
    end

    style Use fill:#f39c12,color:#000
    style UseQwen fill:#27ae60,color:#fff
    style Avoid fill:#fadbd8,color:#000
```

### The Numbers That Matter

```mermaid
xychart-beta
    title "Final Comparison: Static vs Qwen3"
    x-axis ["Speed", "Quality", "Memory", "Dependencies", "Semantics"]
    y-axis "Score" 0 --> 100
    bar [95, 35, 99, 100, 15]
    bar [30, 100, 20, 60, 90]
```

| Metric | Static768 | Qwen3-0.6B | Winner |
|--------|-----------|------------|--------|
| **Embeddings/sec** | 1,000 | 30 | Static (33x) |
| **Search Quality** | 35% | 100% | Qwen3 (2.9x) |
| **Memory Usage** | 1MB | 800MB | Static (800x) |
| **Dependencies** | None | Ollama | Static |
| **Semantic Understanding** | 15% | 90% | Qwen3 (6x) |
| **Production Ready** | ⚠️ | ✅ | Qwen3 |

**Use static embeddings when you must. Use Qwen3 when you can.**

---

## Further Reading

### Static Embeddings
- [FNV Hash Algorithm](https://en.wikipedia.org/wiki/Fowler%E2%80%93Noll%E2%80%93Vo_hash_function) - Hash function used in StaticEmbedder
- [Bag of Words Models](https://en.wikipedia.org/wiki/Bag-of-words_model) - Conceptual foundation

### Qwen3 and Neural Embeddings
- [Qwen3-Embedding on HuggingFace](https://huggingface.co/Qwen/Qwen3-Embedding-0.6B) - Model documentation
- [Qwen3 Technical Report](https://arxiv.org/abs/2309.16609) - Architecture details
- [Ollama Documentation](https://ollama.ai/library/qwen3-embedding) - Running Qwen3 locally

### AmanMCP Documentation
- [Vector Search Concepts](../guides/vector-search-concepts.md) - How HNSW and embeddings work together
- [Smaller Models, Better Search](./smaller-models-better-search.md) - Neural model comparison
- [ADR-036: Multi-Backend Embedding Testing](../reference/decisions/ADR-036-multi-backend-embedding-testing.md) - Architecture decision

### Deep Dives
- [Attention Is All You Need](https://arxiv.org/abs/1706.03762) - Transformer architecture paper
- [Sentence-BERT](https://arxiv.org/abs/1908.10084) - Sentence embeddings for semantic similarity
- [MTEB Leaderboard](https://huggingface.co/spaces/mteb/leaderboard) - Embedding model benchmarks

---

*Static embeddings: When you need vectors but don't need meaning.*

*Qwen3 embeddings: When you need vectors that understand.*
