# Contributing to AmanMCP

Thank you for your interest in contributing to AmanMCP! This document provides guidelines and information for contributors.

---

## Code of Conduct

Be respectful, inclusive, and constructive. We're building something useful together.

---

## How to Contribute

### Reporting Bugs

1. **Check existing issues** - Your bug might already be reported
2. **Create a minimal reproduction** - Helps us debug faster
3. **Include environment details** - OS, Go version, Ollama version
4. **Describe expected vs actual behavior**

### Suggesting Features

1. **Check the roadmap** - It might already be planned
2. **Explain the use case** - Why is this feature valuable?
3. **Consider scope** - Does it fit our "It Just Works" philosophy?

### Submitting Code

1. **Fork the repository**
2. **Create a feature branch** - `git checkout -b feature/my-feature`
3. **Write tests** - We aim for 80%+ coverage
4. **Follow Go conventions** - Run `gofmt` and `golangci-lint`
5. **Submit a PR** - Reference any related issues

---

## Development Setup

### Prerequisites

- Go 1.25.5+
- Make
- golangci-lint

### Getting Started

```bash
# Clone your fork
git clone https://github.com/YOUR_USERNAME/amanmcp
cd amanmcp

# Install dependencies
go mod download

# Run tests
make test

# Build
make build

# Run linter
make lint
```

### Project Structure

```
amanmcp/
├── cmd/amanmcp/        # CLI entry point
├── internal/           # Private packages
│   ├── config/         # Configuration
│   ├── mcp/            # MCP protocol
│   ├── search/         # Search engine
│   ├── index/          # Indexer
│   ├── chunk/          # Chunkers
│   ├── embed/          # Embedders
│   └── store/          # Storage
├── pkg/                # Public packages
├── testdata/           # Test fixtures
└── docs/               # Documentation
```

---

## Contribution Areas

### High Priority

| Area | Description | Skills Needed |
|------|-------------|---------------|
| **Language Support** | Add tree-sitter grammars | tree-sitter, Go |
| **Windows Support** | Cross-compile, test | Windows, CGO |
| **Performance** | Optimize hot paths | Go, profiling |

### Medium Priority

| Area | Description | Skills Needed |
|------|-------------|---------------|
| **Documentation** | Improve guides, examples | Writing |
| **Testing** | Increase coverage | Go testing |
| **CI/CD** | Improve release process | GitHub Actions |

### Nice to Have

| Area | Description | Skills Needed |
|------|-------------|---------------|
| **IDE Integration** | VS Code extension | TypeScript |
| **Benchmarking** | Performance dashboards | Go, metrics |

---

## Adding Language Support

To add support for a new programming language using the
[official tree-sitter Go bindings](https://github.com/tree-sitter/go-tree-sitter):

### 1. Add tree-sitter Grammar

```go
// internal/chunk/languages.go

import (
    sitter "github.com/tree-sitter/go-tree-sitter"
    tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
)

func init() {
    RegisterLanguage(Language{
        Name:       "rust",
        Extensions: []string{".rs"},
        Grammar:    tree_sitter_rust.Language(),
        NodeTypes:  RustNodeTypes,
    })
}

var RustNodeTypes = NodeTypeConfig{
    Function:  []string{"function_item", "impl_item"},
    Class:     []string{"struct_item", "enum_item"},
    Interface: []string{"trait_item"},
}

// IMPORTANT: Always close tree-sitter objects to prevent memory leaks
// parser := sitter.NewParser()
// defer parser.Close()
// tree := parser.Parse(content, nil)
// defer tree.Close()
```

### 2. Add Tests

```go
// internal/chunk/languages_test.go

func TestRustChunking(t *testing.T) {
    content := []byte(`
fn main() {
    println!("Hello");
}
`)
    chunks, err := ChunkFile("main.rs", content)
    require.NoError(t, err)
    require.Len(t, chunks, 1)
    assert.Equal(t, "function", chunks[0].Symbols[0].Type)
}
```

### 3. Add Test Fixtures

```
testdata/
└── projects/
    └── rust-project/
        ├── main.rs
        └── lib.rs
```

### 4. Update Documentation

Add the language to `README.md` supported languages table.

---

## Code Style

### Go Conventions

- Run `gofmt -s` before committing
- Run `golangci-lint run` to check for issues
- Follow [Effective Go](https://golang.org/doc/effective_go)

### Naming

- Use descriptive names
- Prefer `HandleAuth` over `HA` or `handle_auth`
- Keep package names short and lowercase

### Error Handling

```go
// Good: Wrap errors with context
if err != nil {
    return fmt.Errorf("failed to parse file %s: %w", path, err)
}

// Bad: Lose context
if err != nil {
    return err
}
```

### Testing

```go
// Use table-driven tests
func TestSearch(t *testing.T) {
    tests := []struct {
        name    string
        query   string
        want    int
    }{
        {"exact match", "handleAuth", 1},
        {"partial match", "handle", 5},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := search(tt.query)
            assert.Equal(t, tt.want, len(got))
        })
    }
}
```

---

## Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add Rust language support
fix: handle empty files gracefully
docs: update installation instructions
test: add benchmarks for search
refactor: extract common chunking logic
chore: update dependencies
```

---

## Pull Request Process

1. **Update tests** - All changes need tests
2. **Update docs** - If behavior changes
3. **Run CI locally** - `make ci-check`
4. **Write clear PR description**
5. **Link related issues**

### PR Checklist

- [ ] Tests pass (`make test`)
- [ ] Linter passes (`make lint`)
- [ ] Documentation updated
- [ ] Commit messages follow convention
- [ ] PR description explains changes

---

## Philosophy Alignment

Before contributing, ensure your changes align with our philosophy:

| Principle | Your Change Should... |
|-----------|-----------------------|
| **Zero Config** | Not require new configuration |
| **Privacy-First** | Not add network calls |
| **Performance** | Not regress benchmarks |
| **Simplicity** | Not add unnecessary complexity |
| **Single Binary** | Not add runtime dependencies |

If your change conflicts with these principles, open a discussion first.

---

## Getting Help

- **GitHub Issues** - Bug reports, feature requests
- **Discussions** - Questions, ideas, RFC
- **Code Review** - Maintainers will review PRs

---

## Recognition

Contributors will be:

- Listed in CONTRIBUTORS.md
- Mentioned in release notes
- Credited in relevant documentation

---

## License

By contributing, you agree that your contributions will be licensed under the MIT License.

---

**Thank you for contributing to AmanMCP!**
