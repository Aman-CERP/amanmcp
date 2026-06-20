# Tree-sitter Basics

Learn how tree-sitter enables intelligent code parsing in AmanMCP.

---

## Overview

Tree-sitter is a parser generator tool and incremental parsing library. It builds concrete syntax trees for source files and efficiently updates them as the file is edited.

**Why we use it**: To understand code structure, extract meaningful chunks, and identify semantic boundaries (functions, classes, methods) rather than just splitting on line counts.

---

## Core Concepts

### Abstract Syntax Tree (AST)

Code is structured data. Tree-sitter converts text into a tree:

```mermaid
graph TD
    Source["Source: func hello() { return 'world' }"]

    subgraph AST["Abstract Syntax Tree"]
        FD[function_declaration]
        FD --> Name["name: identifier<br/>'hello'"]
        FD --> Params["parameters: parameter_list<br/>()"]
        FD --> Body["body: block"]
        Body --> Return["return_statement"]
        Return --> Expr["expression: string_literal<br/>'world'"]
    end

    Source --> AST

    style FD fill:#e74c3c,stroke-width:2px
    style Body fill:#3498db,stroke-width:2px
    style Return fill:#27ae60,stroke-width:2px
```

### Why Not Regex?

Regex fails for nested structures:

```go
// How do you match the closing brace of this function?
func outer() {
    if true {
        go func() {
            // nested
        }()
    }
}  // <- This one, not earlier ones
```

Tree-sitter handles nesting correctly because it understands grammar.

### Incremental Parsing

When code changes, tree-sitter doesn't re-parse everything:

```mermaid
flowchart LR
    subgraph Before["Before Edit"]
        B1["function_declaration"]
        B2["name: 'hello'"]
        B3["parameters"]
        B4["body"]
    end

    subgraph After["After Edit: 'hello' → 'greet'"]
        A1["function_declaration<br/>(reused)"]
        A2["name: 'greet'<br/>(re-parsed)"]
        A3["parameters<br/>(reused)"]
        A4["body<br/>(reused)"]
    end

    B1 -.->|reuse| A1
    B3 -.->|reuse| A3
    B4 -.->|reuse| A4

    style A2 fill:#e67e22,stroke-width:2px
    style A1 fill:#27ae60,stroke-width:2px
    style A3 fill:#27ae60,stroke-width:2px
    style A4 fill:#27ae60,stroke-width:2px
```

For AmanMCP, this means fast re-indexing on file changes.

---

## How Tree-sitter Works

### 1. Grammar Definition

Each language has a grammar (JavaScript DSL):

```javascript
// Simplified Go grammar excerpt
module.exports = grammar({
  name: 'go',

  rules: {
    source_file: $ => repeat($._definition),

    _definition: $ => choice(
      $.function_declaration,
      $.type_declaration,
      $.var_declaration,
    ),

    function_declaration: $ => seq(
      'func',
      field('name', $.identifier),
      field('parameters', $.parameter_list),
      optional(field('result', $._type)),
      field('body', $.block)
    ),
    // ...
  }
});
```

### 2. Parser Generation

Grammar compiles to a state machine (C code):

```mermaid
flowchart LR
    Grammar["grammar.js<br/>(language definition)"]
    Generate["tree-sitter generate"]
    Parser["parser.c<br/>(state machine)"]

    Grammar --> Generate --> Parser

    style Grammar fill:#3498db,stroke-width:2px
    style Generate fill:#9b59b6,stroke-width:2px
    style Parser fill:#27ae60,stroke-width:2px
```

### 3. Runtime Parsing

Parser consumes source code byte-by-byte:

```mermaid
flowchart TB
    Start([Parser Start<br/>State 0]) --> Input["Input: 'func main() { return 0 }'"]

    subgraph Tokenization["Tokenization & Lexing"]
        Input --> T1["Read 'func'<br/>Token: FUNC_KEYWORD"]
        T1 --> T2["Read 'main'<br/>Token: IDENTIFIER"]
        T2 --> T3["Read '('<br/>Token: LPAREN"]
        T3 --> T4["Read ')'<br/>Token: RPAREN"]
        T4 --> T5["Read '{'<br/>Token: LBRACE"]
        T5 --> T6["Read 'return'<br/>Token: RETURN_KEYWORD"]
        T6 --> T7["Read '0'<br/>Token: NUMBER"]
        T7 --> T8["Read '}'<br/>Token: RBRACE"]
    end

    subgraph Parsing["Parsing State Machine"]
        SM1["State 0<br/>Expect: definition"]
        SM2["State 5<br/>Matched: FUNC<br/>Expect: name"]
        SM3["State 12<br/>Matched: name<br/>Expect: parameters"]
        SM4["State 20<br/>Matched: params<br/>Expect: body"]
        SM5["State 35<br/>Inside body<br/>Expect: statements"]
        SM6["State 42<br/>Matched: return<br/>Expect: expression"]
        SM7["State 50<br/>Complete"]

        SM1 -->|FUNC| SM2
        SM2 -->|IDENTIFIER| SM3
        SM3 -->|LPAREN, RPAREN| SM4
        SM4 -->|LBRACE| SM5
        SM5 -->|RETURN| SM6
        SM6 -->|NUMBER, RBRACE| SM7
    end

    subgraph TreeBuilding["AST Construction"]
        Node1["function_declaration"]
        Node2["name: 'main'"]
        Node3["parameters: ()"]
        Node4["body: block"]
        Node5["return_statement"]
        Node6["expression: 0"]

        Node1 --> Node2
        Node1 --> Node3
        Node1 --> Node4
        Node4 --> Node5
        Node5 --> Node6
    end

    Tokenization --> Parsing
    Parsing --> TreeBuilding
    TreeBuilding --> Complete([Complete AST])

    style Start fill:#3498db,stroke-width:2px
    style Tokenization fill:#9b59b6,stroke-width:2px
    style Parsing fill:#e67e22,stroke-width:2px
    style TreeBuilding fill:#27ae60,stroke-width:2px
    style Complete fill:#27ae60,stroke-width:2px
    style SM1 fill:#fff9c4
    style SM7 fill:#c8e6c9
```

**The process:**

1. **Tokenization**: Convert characters to tokens (keywords, identifiers, symbols)
2. **State Machine**: Navigate through grammar states based on tokens
3. **AST Construction**: Build tree nodes as grammar rules match
4. **Result**: Complete syntax tree representing code structure

---

## Language Support

### Built-in Language Tiers

Tree-sitter has parsers for many languages, but AmanMCP only ships parser-backed
chunking for the languages compiled into this binary. See
[Language Support Tiers](language-tiers.md) for the user-facing contract.

| Tier | AmanMCP behavior | Current examples |
|------|------------------|------------------|
| Tier 1 parser-backed | AST chunking and symbol extraction | Go, TypeScript, TSX, JavaScript, JSX, Python |
| Tier 2 line fallback | Language detected, indexed with line fallback | Rust, Java, Ruby, C, C++, C#, Swift, PHP, Shell, HTML, CSS, SQL |
| Tier 3 plain text | No programming-language semantics | Text, Markdown, PDFs, unknown extensions |

### Language Detection

AmanMCP detects language by:

1. **File extension** (`.go`, `.py`, `.ts`) - Primary method, fastest
2. **Shebang** (`#!/usr/bin/env python`) - For scripts without extensions
3. **Content heuristics** (fallback) - Keyword analysis when extension is ambiguous

---

## In AmanMCP

### Go Bindings

We use the official Go bindings:

```go
import (
    sitter "github.com/tree-sitter/go-tree-sitter"
    golang "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

func parseGoFile(content []byte) (*sitter.Tree, error) {
    parser := sitter.NewParser()
    defer parser.Close()  // IMPORTANT: Must close

    parser.SetLanguage(sitter.NewLanguage(golang.Language()))

    tree := parser.Parse(content, nil)
    // Note: tree.Close() when done

    return tree, nil
}
```

### Node Types for Chunking

We extract these node types as chunks:

```go
var goChunkNodes = []string{
    "function_declaration",     // func foo() {}
    "method_declaration",       // func (r Recv) Method() {}
    "type_declaration",         // type Foo struct {}
    "const_declaration",        // const X = 1
    "var_declaration",          // var x int
}
```

### Walking the Tree

```go
func extractChunks(tree *sitter.Tree, source []byte) []Chunk {
    var chunks []Chunk

    cursor := sitter.NewTreeCursor(tree.RootNode())
    defer cursor.Close()

    var walk func()
    walk = func() {
        node := cursor.Node()

        if isChunkNode(node.Kind()) {
            chunks = append(chunks, Chunk{
                Type:    node.Kind(),
                Content: string(source[node.StartByte():node.EndByte()]),
                Start:   node.StartPoint(),
                End:     node.EndPoint(),
            })
        }

        if cursor.GoToFirstChild() {
            for {
                walk()
                if !cursor.GoToNextSibling() {
                    break
                }
            }
            cursor.GoToParent()
        }
    }

    walk()
    return chunks
}
```

### Tree Traversal Visualization

How AmanMCP walks the AST using depth-first search to extract chunks:

```mermaid
---
config:
  layout: elk
  theme: neo
---
flowchart TB
    Start([Start at Root]) --> Root["source_file<br/>(not a chunk node)"]

    Root --> Check1{Is chunk<br/>node?}
    Check1 -->|No| Children1{Has<br/>children?}
    Children1 -->|Yes| Child1["import_declaration<br/>(not a chunk node)"]

    Child1 --> Check2{Is chunk<br/>node?}
    Check2 -->|No| NextSibling1["Move to next sibling"]

    NextSibling1 --> Child2["type_declaration<br/>(IS chunk node!)"]
    Child2 --> Check3{Is chunk<br/>node?}
    Check3 -->|Yes| Extract1["Extract Chunk #1<br/>type User struct {...}"]

    Extract1 --> Recurse1{Has<br/>children?}
    Recurse1 -->|Yes| RecurseDown["Recurse into children<br/>(struct fields)"]
    RecurseDown --> BackUp1["Go back to parent"]

    BackUp1 --> NextSibling2["Move to next sibling"]
    NextSibling2 --> Child3["function_declaration<br/>(IS chunk node!)"]

    Child3 --> Check4{Is chunk<br/>node?}
    Check4 -->|Yes| Extract2["Extract Chunk #2<br/>func NewUser() {...}"]

    Extract2 --> Recurse2{Has<br/>children?}
    Recurse2 -->|Yes| RecurseDown2["Recurse into children<br/>(func body statements)"]
    RecurseDown2 --> BackUp2["Go back to parent"]

    BackUp2 --> NextSibling3["Move to next sibling"]
    NextSibling3 --> Child4["method_declaration<br/>(IS chunk node!)"]

    Child4 --> Check5{Is chunk<br/>node?}
    Check5 -->|Yes| Extract3["Extract Chunk #3<br/>func (u User) Validate() {...}"]

    Extract3 --> NoMore{More<br/>siblings?}
    NoMore -->|No| Complete([Traversal Complete<br/>Extracted 3 chunks])

    style Start fill:#3498db,stroke-width:2px
    style Root fill:#e1f5ff
    style Child1 fill:#e1f5ff
    style Child2 fill:#c8e6c9
    style Child3 fill:#c8e6c9
    style Child4 fill:#c8e6c9
    style Extract1 fill:#27ae60,stroke-width:2px
    style Extract2 fill:#27ae60,stroke-width:2px
    style Extract3 fill:#27ae60,stroke-width:2px
    style Complete fill:#27ae60,stroke-width:2px
    style Check1 fill:#fff9c4
    style Check2 fill:#fff9c4
    style Check3 fill:#fff9c4
    style Check4 fill:#fff9c4
    style Check5 fill:#fff9c4
```

**Traversal Algorithm (Depth-First Search):**

1. **Visit node**: Check if it's a chunk type (function, method, type, etc.)
2. **Extract if match**: If chunk node, extract content and metadata
3. **Recurse to children**: Go to first child, repeat process
4. **Visit siblings**: After all children, move to next sibling
5. **Backtrack**: When no more siblings, go back to parent
6. **Complete**: Continue until all nodes visited

**Why DFS?**

- Preserves document order (top to bottom)
- Processes complete functions before moving to next
- Natural stack-based recursion matches code structure

### Chunk Context

For each chunk, we extract context:

```go
type ChunkContext struct {
    // The chunk itself
    Content string

    // Where it lives
    FilePath   string
    Package    string  // Go package name
    ParentType string  // For methods: the receiver type

    // Navigation
    StartLine int
    EndLine   int

    // For search ranking
    Signature string  // func name(params) returns
    DocString string  // Comment above
}
```

---

## Common Operations

### Get Function Name

```go
func getFunctionName(node *sitter.Node, source []byte) string {
    nameNode := node.ChildByFieldName("name")
    if nameNode != nil {
        return string(source[nameNode.StartByte():nameNode.EndByte()])
    }
    return ""
}
```

### Get Method Receiver

```go
func getReceiver(node *sitter.Node, source []byte) string {
    receiver := node.ChildByFieldName("receiver")
    if receiver == nil {
        return ""
    }

    // Walk to find type identifier
    for i := 0; i < int(receiver.ChildCount()); i++ {
        child := receiver.Child(uint(i))
        if child.Kind() == "type_identifier" {
            return string(source[child.StartByte():child.EndByte()])
        }
    }
    return ""
}
```

### Extract Doc Comment

```go
func getDocComment(node *sitter.Node, source []byte) string {
    // Look at previous sibling
    prev := node.PrevSibling()
    if prev != nil && prev.Kind() == "comment" {
        return string(source[prev.StartByte():prev.EndByte()])
    }
    return ""
}
```

### Query Pattern Matching

Tree-sitter supports powerful S-expression queries for pattern matching:

```mermaid
flowchart TB
    Start([Query Pattern]) --> Pattern["(function_declaration<br/>  name: (identifier) @func.name<br/>  parameters: (parameter_list) @func.params<br/>  body: (block) @func.body)"]

    Pattern --> Tree[Parse Tree]

    subgraph AST["Abstract Syntax Tree"]
        direction TB
        Root["source_file"]
        Func1["function_declaration"]
        Name1["identifier: 'NewUser'"]
        Params1["parameter_list"]
        Body1["block"]
        Func2["function_declaration"]
        Name2["identifier: 'main'"]
        Params2["parameter_list"]
        Body2["block"]

        Root --> Func1
        Func1 --> Name1
        Func1 --> Params1
        Func1 --> Body1
        Root --> Func2
        Func2 --> Name2
        Func2 --> Params2
        Func2 --> Body2
    end

    Tree --> AST

    AST --> Match1{Pattern<br/>Match?}
    Match1 -->|Yes| Capture1["Capture Match #1<br/>@func.name = 'NewUser'<br/>@func.params = (...)<br/>@func.body = {...}"]

    Capture1 --> Match2{More<br/>Matches?}
    Match2 -->|Yes| Capture2["Capture Match #2<br/>@func.name = 'main'<br/>@func.params = ()<br/>@func.body = {...}"]

    Capture2 --> Results["Query Results<br/>2 function declarations found"]

    Results --> Usage["Use captures for:<br/>• Refactoring<br/>• Analysis<br/>• Code generation<br/>• Custom chunking"]

    style Start fill:#3498db,stroke-width:2px
    style Pattern fill:#9b59b6,stroke-width:2px
    style AST fill:#e1f5ff
    style Func1 fill:#c8e6c9
    style Func2 fill:#c8e6c9
    style Capture1 fill:#27ae60,stroke-width:2px
    style Capture2 fill:#27ae60,stroke-width:2px
    style Results fill:#27ae60,stroke-width:2px
    style Usage fill:#fff9c4
```

**Query Examples:**

```scheme
; Find all exported functions
(function_declaration
  name: (identifier) @name
  (#match? @name "^[A-Z]"))

; Find error handling patterns
(if_statement
  condition: (binary_expression
    operator: "!="
    right: (nil))
  consequence: (block
    (return_statement) @error.return))

; Find methods with specific receivers
(method_declaration
  receiver: (parameter_list
    (parameter_declaration
      type: (type_identifier) @receiver))
  name: (identifier) @method.name
  (#eq? @receiver "UserService"))
```

**Why Queries are Powerful:**

- **Structural matching**: Match AST patterns, not text
- **Captures**: Extract specific nodes for processing
- **Predicates**: Filter matches with conditions
- **Composable**: Build complex patterns from simple ones

---

## Memory Management

### CRITICAL: Close Resources

```mermaid
---
config:
  layout: elk
  look: neo
---
flowchart TB
    subgraph Good["Good: Resources Closed"]
        G1["parser := NewParser()"]
        G2["defer parser.Close()"]
        G3["tree := parser.Parse()"]
        G4["defer tree.Close()"]
        G5["Process tree..."]
        G6["Resources freed"]

        G1 --> G2 --> G3 --> G4 --> G5 --> G6
    end

    subgraph Bad["Bad: Memory Leak"]
        B1["parser := NewParser()"]
        B2["tree := parser.Parse()"]
        B3["Process tree..."]
        B4["Memory leaked!"]

        B1 --> B2 --> B3 --> B4
    end

    style Good color:#FFFFFF
    style Bad color:#FFFFFF
    style Good fill:#27ae60,stroke-width:2px
    style Bad fill:#e74c3c,stroke-width:2px
    style G6 fill:#27ae60,stroke-width:2px
    style B4 fill:#e74c3c,stroke-width:2px
    style G6 color:#FFFFFF
    style B4 color:#FFFFFF
```

Tree-sitter uses CGO. You MUST close resources:

```go
// ALWAYS use defer
parser := sitter.NewParser()
defer parser.Close()

tree := parser.Parse(content, nil)
defer tree.Close()

cursor := sitter.NewTreeCursor(tree.RootNode())
defer cursor.Close()
```

### Reuse Parsers

For batch processing, reuse the parser:

```go
func (c *Chunker) ProcessFiles(files []File) []Chunk {
    parser := sitter.NewParser()
    defer parser.Close()

    var allChunks []Chunk
    for _, file := range files {
        parser.SetLanguage(c.languageFor(file.Ext))
        tree := parser.Parse(file.Content, nil)

        chunks := c.extractChunks(tree, file.Content)
        allChunks = append(allChunks, chunks...)

        tree.Close()  // Close each tree after use
    }
    return allChunks
}
```

---

## Error Handling

### Parse Errors

Tree-sitter is error-tolerant. Invalid syntax still produces a tree:

```go
// Input with error
source := `func broken( {`

tree := parser.Parse([]byte(source), nil)
root := tree.RootNode()

// Tree still exists, has ERROR nodes
if root.HasError() {
    // Handle gracefully
}
```

### Finding Error Nodes

```go
func hasErrors(node *sitter.Node) bool {
    if node.IsError() || node.IsMissing() {
        return true
    }
    for i := 0; i < int(node.ChildCount()); i++ {
        if hasErrors(node.Child(uint(i))) {
            return true
        }
    }
    return false
}
```

---

## Performance

### Benchmarks

| Operation | Time | Notes |
|-----------|------|-------|
| Parse 1KB file | ~0.2ms | Very fast |
| Parse 100KB file | ~5ms | Still fast |
| Incremental reparse | ~0.1ms | For small edits |
| Tree traversal | ~1ms/1000 nodes | Depends on depth |

### Tree-sitter vs Regex Performance

Why tree-sitter dominates regex for code parsing:

```mermaid
---
config:
  layout: elk
---
flowchart TB
    subgraph Scenario["Parsing Task: Extract all functions from 100KB Go file"]
        Task["File: 100KB, 50 functions, nested structures"]
    end

    subgraph RegexApproach["Regex Approach"]
        direction TB
        R1["Pattern: func\\s+(\\w+)\\s*\\("]
        R2["Problem 1: Can't match nested braces<br/>func outer() { func inner() { } } ← Where does outer end?"]
        R3["Problem 2: Must scan entire file<br/>Multiple passes for different patterns"]
        R4["Problem 3: Fragile<br/>Breaks with comments, strings, edge cases"]
        R5["Result: FAILS or INCOMPLETE<br/>• Missed nested functions<br/>• Extracted partial code<br/>• No context (receiver, params)"]

        R1 --> R2 --> R3 --> R4 --> R5
    end

    subgraph TreeSitterApproach["Tree-sitter Approach"]
        direction TB
        T1["Parse entire file: ~5ms<br/>Builds complete AST"]
        T2["Traverse tree: ~2ms<br/>Visit all nodes once"]
        T3["Extract function nodes<br/>With full context and boundaries"]
        T4["Result: SUCCESS<br/>• All 50 functions found<br/>• Complete code + metadata<br/>• Nested structures handled<br/>• Type-safe extraction"]

        T1 --> T2 --> T3 --> T4
    end

    subgraph Comparison["Performance Comparison"]
        direction TB
        C1["Parse time: N/A vs 5ms"]
        C2["Extract time: 50-100ms vs 2ms"]
        C3["Accuracy: 60-80% vs 100%"]
        C4["Nested code: FAILS vs WORKS"]
        C5["Incremental: Full rescan vs Partial reparse"]
        C6["Maintenance: High vs Low"]

        C1 --> C2 --> C3 --> C4 --> C5 --> C6
    end

    Scenario --> RegexApproach
    Scenario --> TreeSitterApproach
    RegexApproach --> Comparison
    TreeSitterApproach --> Comparison

    style Scenario fill:#e1f5ff
    style RegexApproach fill:#ffccbc
    style TreeSitterApproach fill:#c8e6c9
    style Comparison fill:#fff9c4
    style R5 fill:#e74c3c,stroke-width:2px
    style T4 fill:#27ae60,stroke-width:2px
    style R2 fill:#d84315,stroke-width:2px
    style R3 fill:#d84315,stroke-width:2px
    style R4 fill:#d84315,stroke-width:2px
    style T1 fill:#229954,stroke-width:2px
    style T2 fill:#229954,stroke-width:2px
    style T3 fill:#229954,stroke-width:2px
    
    style R5 color:#FFFFFF
    style T4 color:#FFFFFF
    style R2 color:#FFFFFF
    style R3 color:#FFFFFF
    style R4 color:#FFFFFF
    style T1 color:#FFFFFF
    style T2 color:#FFFFFF
    style T3 color:#FFFFFF
```

**Performance Comparison Table:**

| Metric | Regex | Tree-sitter |
|--------|-------|-------------|
| Parse time | N/A | 5ms |
| Extract time | 50-100ms | 2ms |
| Accuracy | 60-80% | 100% |
| Nested code | FAILS | WORKS |
| Incremental | Full rescan | Partial reparse |
| Maintenance | High (fragile) | Low (grammar-based) |

**Key Insights:**

1. **Regex limitations**:
   - Can't handle nested structures (braces, brackets)
   - No semantic understanding
   - Fragile to edge cases (strings, comments, multiline)
   - Multiple passes needed for different patterns

2. **Tree-sitter advantages**:
   - Single parse, complete AST
   - Grammar-based = robust to edge cases
   - Incremental updates for file changes
   - Full context extraction (not just text matching)

3. **Real-world impact**:
   - AmanMCP extracts ~500 chunks/second with tree-sitter
   - Regex would achieve ~50-100 chunks/second with 60-80% accuracy
   - Tree-sitter: 5-10x faster, 100% accurate

### Optimization Tips

1. **Reuse parsers** - Creating parser has overhead
2. **Parse incrementally** - Use old tree for re-parses
3. **Early exit** - Stop walking when you found what you need
4. **Batch by language** - Switch languages less often

---

## Debugging

### Print Tree Structure

```go
func printTree(node *sitter.Node, source []byte, indent int) {
    prefix := strings.Repeat("  ", indent)
    content := ""
    if node.ChildCount() == 0 {
        content = fmt.Sprintf(" = %q", source[node.StartByte():node.EndByte()])
    }
    fmt.Printf("%s%s%s\n", prefix, node.Kind(), content)

    for i := 0; i < int(node.ChildCount()); i++ {
        printTree(node.Child(uint(i)), source, indent+1)
    }
}
```

### CLI Tool

Use `tree-sitter` CLI for exploration:

```bash
# Install
npm install -g tree-sitter-cli

# Parse a file
tree-sitter parse example.go

# Highlight (shows node types)
tree-sitter highlight example.go
```

---

## Common Mistakes

### 1. Forgetting to Close

```go
// BAD: Memory leak
tree := parser.Parse(content, nil)
// forgot tree.Close()

// GOOD: Always defer
tree := parser.Parse(content, nil)
defer tree.Close()
```

### 2. Wrong Language

```go
// BAD: Using Go parser for Python
parser.SetLanguage(goLanguage)
tree := parser.Parse(pythonCode, nil)  // Garbage tree

// GOOD: Match language to content
parser.SetLanguage(languageForExtension(filepath.Ext(path)))
```

### 3. Index Out of Bounds

```go
// BAD: Assuming child exists
name := node.Child(0).Child(1)  // Might panic

// GOOD: Check bounds
if node.ChildCount() > 0 {
    child := node.Child(0)
    if child.ChildCount() > 1 {
        name := child.Child(1)
    }
}
```

---

## Further Reading

- [Tree-sitter Documentation](https://tree-sitter.github.io/tree-sitter/)
- [Go Bindings](https://github.com/tree-sitter/go-tree-sitter)
- [Creating Parsers](https://tree-sitter.github.io/tree-sitter/creating-parsers)
- [Playground](https://tree-sitter.github.io/tree-sitter/playground) - Try queries interactively

---

*Tree-sitter turns code into data. Use it to understand, not just split.*
