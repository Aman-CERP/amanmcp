# ADR-003: Tree-sitter for Code Chunking

**Status:** Implemented (Binding choice updated 2025-12-28)
**Date:** 2025-12-28
**Supersedes:** None
**Superseded by:** None

---

## Context

AmanMCP requires AST-based code parsing to create semantic chunks for code search. Unlike text, code has well-defined syntactic structure that should be preserved during chunking.

Options considered:

1. **Regex-based parsing** - Pattern matching for functions/classes
2. **Language-specific parsers** - go/parser, typescript-eslint, etc.
3. **Tree-sitter** - Universal parser generator with Go bindings

Additionally, for tree-sitter, two Go binding options exist:

1. **smacker/go-tree-sitter** - Community maintained, auto-GC finalizers
2. **tree-sitter/go-tree-sitter** - Official bindings (December 2024+)

---

## Decision

We will use **tree-sitter** with the **official tree-sitter/go-tree-sitter bindings** for AST-based code parsing and chunking.

```go
import (
    sitter "github.com/tree-sitter/go-tree-sitter"
    golang "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

func parse(source []byte) (*sitter.Tree, error) {
    parser := sitter.NewParser()
    defer parser.Close()  // MANDATORY

    parser.SetLanguage(golang.Language())
    tree := parser.Parse(source, nil)
    // Caller must call tree.Close()
    return tree, nil
}
```

---

## Rationale

### Why Tree-sitter?

| Option | Pros | Cons |
|--------|------|------|
| Regex | Simple, no dependencies | Fragile, misses edge cases, no AST structure |
| Language-specific | Accurate for that language | Different API per language, heavy dependencies |
| **Tree-sitter** | Universal API, 40+ languages, incremental parsing, battle-tested | CGO requirement, learning curve |

Tree-sitter advantages:

- **Universal API**: Same code parses Go, Python, TypeScript
- **Error tolerance**: Produces partial AST on syntax errors
- **Performance**: Incremental parsing, ~5ms for 1000 LOC
- **Adoption**: Used by GitHub, Neovim, Helix, Zed

### Why Official Bindings?

| Option | Pros | Cons |
|--------|------|------|
| smacker/go-tree-sitter | Auto-GC via finalizers, bundled grammars | Community maintained, finalizer issues with CGO |
| **tree-sitter/go-tree-sitter** | Official, actively maintained, modular grammars | Requires explicit Close() calls |

Official bindings chosen because:

1. **Long-term maintenance**: Official project, not community volunteer
2. **Correct memory management**: Explicit Close() avoids CGO finalizer bugs
3. **Modular grammars**: Only import languages you need
4. **Active development**: Regular updates and bug fixes

---

## Consequences

### Positive

- Consistent parsing across 40+ programming languages
- Graceful handling of syntax errors
- Fast parsing (~5ms for 1000 LOC)
- Well-documented node types per language
- Future-proof (new languages added regularly)

### Negative

- **CGO requirement**: Requires C compiler (build-essential/Xcode)
- **Memory management**: Must call Close() on Parser, Tree, TreeCursor
- **Binary size**: Increases binary size per language grammar
- **Build time**: CGO compilation slower than pure Go

### Neutral

- Learning curve for AST traversal patterns
- Need to map node types per language

---

## Implementation Notes

### Memory Management (Critical)

The official bindings require explicit cleanup:

```go
parser := sitter.NewParser()
defer parser.Close()  // MANDATORY - prevents memory leak

tree := parser.Parse(source, nil)
defer tree.Close()    // MANDATORY

cursor := sitter.NewTreeCursor(tree.RootNode())
defer cursor.Close()  // MANDATORY
```

**Warning:** Failing to call Close() will cause memory leaks. The official bindings do NOT use Go finalizers due to CGO bugs.

### Grammar Imports

Each language requires a separate grammar package:

```go
import (
    sitter "github.com/tree-sitter/go-tree-sitter"
    golang "github.com/tree-sitter/tree-sitter-go/bindings/go"
    typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
    javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
    python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)
```

### CGO Build Requirements

**macOS:**

```bash
xcode-select --install
```

**Linux:**

```bash
apt-get install build-essential
```

**GitHub Actions:**

```yaml
- name: Setup CGO
  run: sudo apt-get install -y build-essential
```

---

## Related

- [Feature F06](../specs/features/F06-treesitter.md) - Tree-sitter Integration
- [Feature F07](../specs/features/F07-code-chunker.md) - Code Chunker
- [Tree-sitter Official Go Bindings](https://github.com/tree-sitter/go-tree-sitter)
- [Tree-sitter Documentation](https://tree-sitter.github.io/)
- [docs/guides/tree-sitter-guide.md](../guides/tree-sitter-guide.md)

---

## Changelog

| Date | Change |
|------|--------|
| 2025-12-28 | Initial decision - tree-sitter for code parsing |
| 2025-12-28 | Binding choice updated to official tree-sitter/go-tree-sitter |
