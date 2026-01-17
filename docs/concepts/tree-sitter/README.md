# Tree-sitter

Tree-sitter enables AmanMCP to understand code structure, not just text patterns.

---

## Choose Your Path

| I want to... | Read | Time |
|--------------|------|------|
| Understand why tree-sitter (not regex) | [Overview](overview.md) | 4 min |
| Learn how parsing and ASTs work | [How It Works](how-it-works.md) | 10 min |
| See Go implementation and queries | [Advanced](advanced.md) | 15 min |

---

## Quick Summary

- Tree-sitter **parses code** into structured syntax trees
- Unlike regex, it handles **nested structures** correctly
- AmanMCP extracts **complete functions and types** as search chunks
- Supports **40+ languages** with language-specific grammars

**Why it matters:** Search returns whole functions, not line fragments.

---

## Related Documentation

- [Indexing Pipeline](../indexing-pipeline.md) - Full flow from file to index
- [Hybrid Search](../hybrid-search/) - How chunks are searched
- [Two-Stage Retrieval](../two-stage-retrieval.md) - Chunking strategy
