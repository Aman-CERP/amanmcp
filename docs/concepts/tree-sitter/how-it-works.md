# Tree-sitter: How It Works

Understanding parsing, syntax trees, and how AmanMCP extracts code chunks.

**Reading time:** 10 minutes
**Audience:** Users who want to understand the parsing process
**Prerequisites:** [Overview](overview.md)

---

## Quick Summary

- Parsing converts code text into a **structured tree**
- Each tree **node** represents a code element (function, if-statement, etc.)
- Tree-sitter uses **grammars** to know each language's rules
- **Incremental parsing** makes re-indexing fast when files change

---

## The Parsing Process

When tree-sitter parses code, it goes through three stages:

```mermaid
flowchart LR
    subgraph Stage1["1. Tokenization"]
        Input["func main() { }"]
        Tokens["FUNC, IDENT, LPAREN,<br/>RPAREN, LBRACE, RBRACE"]
    end

    subgraph Stage2["2. Parsing"]
        Grammar["Grammar Rules"]
        StateMachine["State Machine"]
    end

    subgraph Stage3["3. Tree Building"]
        AST["Syntax Tree"]
    end

    Input --> Tokens
    Tokens --> StateMachine
    Grammar --> StateMachine
    StateMachine --> AST

    style Stage1 fill:#e3f2fd,stroke:#1565c0
    style Stage2 fill:#fff8e1,stroke:#ff8f00
    style Stage3 fill:#c8e6c9,stroke:#2e7d32
```

### Stage 1: Tokenization

First, the raw text is split into **tokens** (meaningful pieces):

```
Source: func main() { return 0 }

Tokens:
┌──────┬─────────────┬──────────────────┐
│ Text │ Token Type  │ Position         │
├──────┼─────────────┼──────────────────┤
│ func │ KEYWORD     │ bytes 0-4        │
│ main │ IDENTIFIER  │ bytes 5-9        │
│ (    │ LPAREN      │ byte 9           │
│ )    │ RPAREN      │ byte 10          │
│ {    │ LBRACE      │ byte 12          │
│return│ KEYWORD     │ bytes 14-20      │
│ 0    │ NUMBER      │ byte 21          │
│ }    │ RBRACE      │ byte 23          │
└──────┴─────────────┴──────────────────┘
```

### Stage 2: Parsing (State Machine)

The parser reads tokens and navigates through states based on grammar rules:

```mermaid
stateDiagram-v2
    [*] --> ExpectDefinition: Start
    ExpectDefinition --> SawFunc: FUNC keyword
    SawFunc --> SawName: IDENTIFIER
    SawName --> SawParams: LPAREN...RPAREN
    SawParams --> InBody: LBRACE
    InBody --> InBody: statements
    InBody --> Complete: RBRACE
    Complete --> [*]: Done

    note right of SawFunc: Expecting function name next
    note right of InBody: Collecting function body
```

Each state knows what tokens are valid next. Invalid tokens trigger error recovery.

### Stage 3: Tree Building

As the parser matches grammar rules, it builds tree nodes:

```mermaid
graph TD
    subgraph Building["Tree Construction (Bottom-Up)"]
        direction TB

        Step1["Step 1: Match 'func' keyword"]
        Step2["Step 2: Match identifier 'main'"]
        Step3["Step 3: Match parameter_list '()'"]
        Step4["Step 4: Match block '{ return 0 }'"]
        Step5["Step 5: Combine into function_declaration"]
    end

    subgraph Result["Final Tree"]
        FD["function_declaration"]
        FD --> Name["name: main"]
        FD --> Params["parameters: ()"]
        FD --> Body["body: block"]
        Body --> Ret["return_statement"]
        Ret --> Zero["0"]
    end

    Step1 --> Step2 --> Step3 --> Step4 --> Step5
    Step5 -.-> FD

    style Building fill:#e1f5ff
    style Result fill:#c8e6c9
```

---

## Understanding the Syntax Tree

### Node Anatomy

Every node in the tree has:

```mermaid
graph LR
    subgraph Node["Tree Node"]
        Kind["Kind: function_declaration"]
        Start["Start: byte 0, line 1, col 0"]
        End["End: byte 45, line 5, col 1"]
        Children["Children: [name, params, body]"]
        Parent["Parent: source_file"]
    end

    style Node fill:#fff9c4,stroke:#f57f17
```

| Property | What It Tells You |
|----------|-------------------|
| **Kind** | What type of code element (function, if, variable) |
| **Start/End** | Exact byte positions in source file |
| **Children** | Sub-elements contained within |
| **Parent** | What contains this element |

### Named vs Anonymous Children

Nodes can have **named** children (important) and **anonymous** children (syntax):

```go
func greet(name string) string {
    return "Hello, " + name
}
```

```mermaid
graph TD
    FD["function_declaration"]

    FD --> KW["'func'<br/><i>anonymous</i>"]
    FD --> Name["name: identifier<br/><b>'greet'</b>"]
    FD --> Params["parameters: parameter_list"]
    FD --> Result["result: type_identifier<br/><b>'string'</b>"]
    FD --> Body["body: block"]

    Params --> P1["parameter_declaration<br/>name string"]

    style KW fill:#f5f5f5,stroke:#9e9e9e
    style Name fill:#c8e6c9,stroke:#2e7d32
    style Params fill:#c8e6c9,stroke:#2e7d32
    style Result fill:#c8e6c9,stroke:#2e7d32
    style Body fill:#c8e6c9,stroke:#2e7d32
```

**Named children** (green) carry semantic meaning.
**Anonymous children** (gray) are just syntax tokens like `func`, `{`, `}`.

---

## Tree Traversal

### Depth-First Walk

To extract chunks, AmanMCP walks the tree depth-first:

```mermaid
flowchart TB
    subgraph Walk["Depth-First Traversal"]
        direction TB
        Start([Start]) --> N1["1. Visit source_file"]
        N1 --> N2["2. Visit import_declaration"]
        N2 --> N3["3. Back up, visit type_declaration ✓"]
        N3 --> N4["4. Visit struct fields..."]
        N4 --> N5["5. Back up, visit function_declaration ✓"]
        N5 --> N6["6. Visit function body..."]
        N6 --> N7["7. Back up, visit method_declaration ✓"]
        N7 --> Done([Done])
    end

    subgraph Extracted["Chunks Extracted"]
        C1["Chunk 1: type User struct {...}"]
        C2["Chunk 2: func NewUser() {...}"]
        C3["Chunk 3: func (u User) Validate() {...}"]
    end

    N3 -.-> C1
    N5 -.-> C2
    N7 -.-> C3

    style Walk fill:#e1f5ff
    style Extracted fill:#c8e6c9
    style N3 fill:#81c784
    style N5 fill:#81c784
    style N7 fill:#81c784
```

The algorithm:
1. Visit current node
2. If it's a chunk type (function, type, etc.), extract it
3. Recurse into children
4. Move to next sibling
5. When no siblings, go back to parent

### What Gets Extracted

AmanMCP extracts these node types:

| Language | Node Types |
|----------|------------|
| **Go** | `function_declaration`, `method_declaration`, `type_declaration` |
| **Python** | `function_definition`, `class_definition` |
| **TypeScript** | `function_declaration`, `class_declaration`, `interface_declaration` |
| **Rust** | `function_item`, `impl_item`, `struct_item`, `enum_item` |

---

## Language Detection

Before parsing, AmanMCP must choose the right grammar:

```mermaid
flowchart TB
    File["File: unknown.xyz"] --> Ext{Check Extension}

    Ext -->|.go| Go["Use Go grammar"]
    Ext -->|.py| Python["Use Python grammar"]
    Ext -->|.ts/.tsx| TS["Use TypeScript grammar"]
    Ext -->|Unknown| Shebang{Check Shebang}

    Shebang -->|#!/usr/bin/env python| Python
    Shebang -->|#!/bin/bash| Bash["Use Bash grammar"]
    Shebang -->|None| Heuristics{Content Heuristics}

    Heuristics -->|func keyword| Go
    Heuristics -->|def keyword| Python
    Heuristics -->|No match| Plain["Treat as plain text"]

    style File fill:#e1f5ff
    style Go fill:#00add8,color:#fff
    style Python fill:#3776ab,color:#fff
    style TS fill:#3178c6,color:#fff
    style Bash fill:#4eaa25,color:#fff
    style Plain fill:#9e9e9e,color:#fff
```

### Detection Priority

1. **File extension** (fastest, most reliable)
2. **Shebang line** (`#!/usr/bin/env python`)
3. **Content analysis** (keyword detection)
4. **Fallback** to plain text

---

## Incremental Parsing

When a file changes, tree-sitter doesn't re-parse everything:

```mermaid
flowchart LR
    subgraph Before["Before Edit"]
        B1["function_declaration<br/>'hello'"]
        B2["function_declaration<br/>'world'"]
    end

    subgraph Edit["Edit: 'hello' → 'greet'"]
        Change["Change at bytes 5-10"]
    end

    subgraph After["After Edit"]
        A1["function_declaration<br/>'greet' <i>(re-parsed)</i>"]
        A2["function_declaration<br/>'world' <i>(reused!)</i>"]
    end

    Before --> Edit --> After

    B2 -.->|"Same bytes, reuse"| A2

    style A1 fill:#fff9c4,stroke:#f57f17
    style A2 fill:#c8e6c9,stroke:#2e7d32
    style B2 fill:#c8e6c9,stroke:#2e7d32
```

**How it works:**
1. Tell parser what bytes changed
2. Parser reuses unchanged subtrees
3. Only re-parses affected regions

**Impact for AmanMCP:**
- File edit → only changed functions re-indexed
- Milliseconds instead of seconds for large files

---

## Error Recovery

Tree-sitter is **error-tolerant**. Syntax errors don't crash parsing:

```go
// Broken code
func incomplete(
    // missing closing paren and body
```

```mermaid
graph TD
    Root["source_file"]
    Root --> FD["function_declaration"]
    FD --> Name["name: 'incomplete'"]
    FD --> Params["parameter_list (ERROR)"]
    FD --> Missing["MISSING ')'"]
    FD --> MissingBody["MISSING body"]

    style Params fill:#ffcdd2,stroke:#c62828
    style Missing fill:#ffcdd2,stroke:#c62828
    style MissingBody fill:#ffcdd2,stroke:#c62828
```

The tree still exists with `ERROR` and `MISSING` nodes. AmanMCP can:
- Detect the error with `node.HasError()`
- Still extract partial information
- Skip malformed chunks gracefully

---

## Extracting Context

For each chunk, AmanMCP extracts rich context:

```mermaid
graph LR
    subgraph Code["Source Code"]
        Func["// ValidateEmail checks format<br/>func (u *User) ValidateEmail() error {<br/>    ...<br/>}"]
    end

    subgraph Context["Extracted Context"]
        C1["Content: full function text"]
        C2["FilePath: internal/user/validate.go"]
        C3["StartLine: 42"]
        C4["EndLine: 58"]
        C5["Signature: func (u *User) ValidateEmail() error"]
        C6["Receiver: User"]
        C7["DocComment: // ValidateEmail checks format"]
    end

    Func --> C1
    Func --> C2
    Func --> C3
    Func --> C4
    Func --> C5
    Func --> C6
    Func --> C7

    style Code fill:#e1f5ff
    style Context fill:#c8e6c9
```

This context helps with:
- **Search ranking** - function names are weighted higher
- **Result display** - show file path and line numbers
- **Navigation** - jump directly to the code

---

## Supported Languages

### Tier 1: Excellent Support

```mermaid
graph LR
    subgraph Tier1["Full AST extraction, all constructs"]
        Go["Go"]
        TS["TypeScript"]
        Python["Python"]
        Rust["Rust"]
    end

    style Go fill:#00add8,color:#fff
    style TS fill:#3178c6,color:#fff
    style Python fill:#3776ab,color:#fff
    style Rust fill:#dea584,color:#000
```

### Tier 2: Good Support

| Language | Notes |
|----------|-------|
| Java | Classes, methods, interfaces |
| C/C++ | Functions, structs, templates |
| Ruby | Classes, methods, modules |
| JavaScript | Functions, classes, arrow functions |

### Tier 3: Basic Support

| Language | Notes |
|----------|-------|
| Markdown | Headers, code blocks |
| JSON/YAML | Structure extraction |
| HTML | Tags and attributes |

---

## The Chunk Lifecycle

```mermaid
sequenceDiagram
    participant File as Source File
    participant TS as Tree-sitter
    participant Chunker as AmanMCP Chunker
    participant Index as Search Index

    File->>TS: Raw bytes
    TS->>TS: Tokenize
    TS->>TS: Parse to tree
    TS->>Chunker: Syntax tree

    loop For each interesting node
        Chunker->>Chunker: Check if chunk type
        Chunker->>Chunker: Extract content + context
        Chunker->>Index: Store chunk
    end

    Note over Index: Chunk now searchable
```

---

## Key Takeaways

| Concept | What It Means |
|---------|---------------|
| **Tokenization** | Split text into meaningful pieces |
| **Grammar** | Rules defining language syntax |
| **Syntax Tree** | Hierarchical code representation |
| **Depth-First Walk** | Visit all nodes systematically |
| **Incremental Parse** | Only re-parse what changed |
| **Error Recovery** | Handle broken code gracefully |

---

## Next Steps

| Want to... | Read |
|------------|------|
| See Go implementation details | [Advanced](advanced.md) |
| Understand the full indexing flow | [Indexing Pipeline](../indexing-pipeline.md) |
| Learn about code search | [Hybrid Search](../hybrid-search/) |

---

*Tree-sitter transforms code from text into data structures you can navigate, query, and extract.*
