# Learning Guides

**Purpose:** Educational content for learning technologies used in AmanMCP.

Use these guides to understand the "why" and "how" behind our technology choices.

---

## Guide Catalog

### AI-Native Project Management

| Guide | Topic | When to Read |
|-------|-------|--------------|
| [AI-Native PM Guide](./ai-native-pm-guide.md) | Complete AI-human collaboration system | Setting up PM for AI projects |
| [AI-Native PM Quick Start](./ai-native-pm-quick-start.md) | 30-minute setup guide | Getting started quickly |

### User Guides

| Guide | Topic | When to Read |
|-------|-------|--------------|
| [First-Time User Guide](./first-time-user-guide.md) | Installation, setup, basic usage | New users getting started |
| [Homebrew Setup Guide](./homebrew-setup-guide.md) | Tap setup, releases, maintenance | Maintainers and contributors |
| [Thermal Management](./thermal-management.md) | GPU cooling, sustained indexing | Users with thermal throttling issues |

### Core Technologies

| Guide | Topic | When to Read |
|-------|-------|--------------|
| [Go Patterns](./go-patterns.md) | Go idioms, error handling, testing | Before any Go implementation |
| [Tree-sitter Basics](./tree-sitter-guide.md) | AST parsing, language grammars | Before F06 (tree-sitter integration) |
| [Vector Search Concepts](./vector-search-concepts.md) | Embeddings, similarity, HNSW | Before F09-F12 (embedding/vector) |
| [Hybrid Search](./hybrid-search.md) | BM25, semantic, RRF fusion | Before F13-F15 (search engine) |
| [MCP Protocol](./mcp-protocol.md) | Model Context Protocol basics | Before F16-F18 (MCP integration) |

### Testing & Quality

| Guide | Topic | When to Read |
|-------|-------|--------------|
| [Validation Testing](./validation-testing.md) | Data-driven search quality tests | Adding validation queries |

### Operational

| Guide | Topic | When to Read |
|-------|-------|--------------|
| [CI Parity Check](../.claude/guides/ci-parity-check.md) | Local CI validation | Before any push |
| [Deferred Work](../.claude/guides/deferred-work-decision-tree.md) | When to defer vs. do now | When facing choices |

---

## How to Use Guides

### During Implementation

When implementing a feature, read relevant guides first:

```
F06 (tree-sitter) → Read: tree-sitter-guide.md
F11 (BM25)        → Read: hybrid-search.md
F12 (vectors)     → Read: vector-search-concepts.md
```

### For Learning

Ask Claude: "teach me about X" or "explain X"

Claude will use the learning-content skill and relevant guides to provide structured explanations.

---

## Guide Structure

Each guide follows this structure:

1. **Overview** - What is this and why does it matter?
2. **Core Concepts** - Key ideas to understand
3. **How It Works** - Technical explanation
4. **In AmanMCP** - How we use it specifically
5. **Examples** - Code/diagrams
6. **Common Mistakes** - What to avoid
7. **Further Reading** - Links for deeper learning

---

## Contributing Guides

When you learn something worth sharing:

1. Create guide in `docs/guides/`
2. Follow the structure above
3. Add to this index
4. Link from relevant feature specs

---

*Learning is part of building. These guides capture knowledge as we go.*
