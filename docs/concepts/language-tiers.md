# Language Support Tiers

AmanMCP indexes many file types, but not every programming language has the
same level of code understanding. Search results expose this distinction as
`results[].language_support_tier` so AI clients and users can tell parser-backed
code support from fallback indexing.

The tier is per result, not aggregate search metadata. A single query can return
Go, Rust, Markdown, and PDF results together, and each result can have a
different tier.

## Tier Table

| Tier | Value | What it means | Current examples |
|------|-------|---------------|------------------|
| Tier 1 | `tier_1_parser_backed` | Parser-backed AST chunking and symbol extraction. Chunks preserve functions, methods, classes, interfaces, or similar semantic boundaries. | Go, TypeScript, TSX, JavaScript, JSX, Python |
| Tier 2 | `tier_2_line_fallback` | The language is detected, but AmanMCP does not have parser-backed AST support for it yet. Content is indexed with line fallback, so matching works but symbol boundaries are less precise. | Rust, Java, Ruby, C, C++, C#, Swift, PHP, Shell, HTML, CSS, SQL, and other detected fallback code languages |
| Tier 3 | `tier_3_plain_text` | Plain text or document-format indexing with no programming-language semantics. | Text files, Markdown sections, PDFs, unknown extensions |

## Result Contract

Every structured MCP search result includes:

```json
{
  "file_path": "src/main.go",
  "language": "go",
  "language_support_tier": "tier_1_parser_backed"
}
```

The field is intentionally not part of `search_quality`. `search_quality`
describes the query execution mode and index health for the whole response;
language support belongs on each result because mixed-language result sets are
normal.

## What Each Tier Provides

Tier 1 parser-backed results can expose symbol-aware chunks, symbol type,
signature, and AST-informed boundaries when the underlying parser supports
them. These are the highest-confidence code results.

Tier 2 line-fallback results still benefit from language detection, filtering,
BM25 matching, embeddings, and normal source metadata. They should not be
described as AST-aware or parser-backed until a real parser integration lands.

Tier 3 plain-text results are useful for prose, generated text, PDFs, and
unknown file types. PDF and Markdown support are document-format capabilities,
not programming-language parser support.

## Contributor Checklist

When adding or changing language support:

1. Update `internal/language` definitions.
2. Add parser-backed chunking tests for any new Tier 1 language.
3. Keep line-fallback languages in Tier 2 until AST chunking and symbol
   extraction are implemented and tested.
4. Update this page and any user-facing tables.
5. Add or update tests that assert `results[].language_support_tier`.
