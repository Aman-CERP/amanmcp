# ADR-033: Contextual Retrieval for Search Quality

**Status:** Implemented
**Date:** 2026-01-08
**Deciders:** AmanMCP Team
**References:** [Anthropic Contextual Retrieval Research](https://www.anthropic.com/news/contextual-retrieval)

---

## Context

### Problem Statement

AmanMCP's dogfooding baseline shows 75% Tier 1 pass rate (9/12 queries). Three queries consistently fail:

1. "Search function" - Expected: `internal/search/engine`
2. "Index function" - Expected: `internal/index`
3. "OllamaEmbedder" - Expected: `internal/embed/ollama`

**Root Cause (RCA-010):** Vocabulary mismatch between natural language queries and indexed code. Chunks contain raw code without semantic context explaining what the code does or how it fits into the broader system.

### Research Foundation

Anthropic's Contextual Retrieval research demonstrates that prepending LLM-generated context to chunks before embedding can reduce retrieval failure rates by 49% for top-20 results and 67% when combined with hybrid search.

The key insight: embeddings capture semantic meaning better when chunks include explicit context about their purpose and role within the document.

### Current Chunk Content

```
// Example: chunk from internal/search/engine.go
func (e *Engine) Search(ctx context.Context, query string) ([]Result, error) {
    ...
}
```

A user searching for "Search function" expects this result, but the embedding doesn't capture that this is THE primary search function in the codebase.

### Proposed Enhanced Content

```
This file contains the core search engine for hybrid BM25+vector search.
The Search function is the main entry point that orchestrates parallel
BM25 and vector searches with RRF fusion.

func (e *Engine) Search(ctx context.Context, query string) ([]Result, error) {
    ...
}
```

---

## Decision

**We will implement Contextual Retrieval** by generating semantic context for each chunk at index time and prepending it to the chunk content before embedding.

### Architecture

```
Existing:  Scan → Chunk → Embed → Index
Enhanced:  Scan → Chunk → [Context Generation] → Embed → Index
```

### Context Generation Strategy

1. **Primary:** LLM-based context generation using Ollama (qwen3:0.6b)
2. **Fallback:** Pattern-based context using file path, symbols, and doc comments

### Key Design Choices

| Decision | Rationale |
|----------|-----------|
| Index-time generation | One-time cost, no query latency impact |
| Prepend (not replace) | Preserves original content for BM25 matching |
| Small/fast LLM | 0.6B model balances quality vs. speed |
| Pattern fallback | Works without Ollama, zero external dependencies |
| Per-file context | Enables prompt caching optimization |

---

## Rationale

### Why Index-Time Context?

| Approach | Pros | Cons |
|----------|------|------|
| **Index-time (chosen)** | No query latency, cached embeddings | Requires reindex |
| Query-time expansion | No reindex needed | Adds latency, compute cost |
| Hybrid (both) | Maximum flexibility | Complexity |

**Decision:** Index-time only. Quality matters more than avoiding reindexes.

### Why Pattern Fallback?

Pattern-based context ensures zero-config "It Just Works" experience:
- Uses file path: "From file: internal/search/engine.go"
- Uses symbol names: "Defines: function Search"
- Uses doc comments when available

This provides basic context even without Ollama, maintaining our core philosophy.

### Why qwen3:0.6b?

| Model | Size | Speed | Quality | Choice |
|-------|------|-------|---------|--------|
| qwen3:0.6b | 600M | ~50ms/chunk | Good | **Primary** |
| qwen3:1.5b | 1.5B | ~100ms/chunk | Better | Alternative |
| llama3.2:1b | 1B | ~80ms/chunk | Good | Alternative |

The 0.6B model provides the best speed/quality tradeoff for context generation.

---

## Consequences

### Positive

1. **Semantic bridging** - Context explains code purpose, bridging vocabulary gap
2. **Improved retrieval** - Expected 49-67% error reduction (per Anthropic research)
3. **Better embeddings** - Richer semantic signal for vector search
4. **Graceful degradation** - Pattern fallback ensures zero-config works

### Negative

1. **Increased index time** - ~50ms/chunk with LLM (mitigated by batching)
2. **Ollama dependency** - LLM mode requires Ollama running (mitigated by fallback)
3. **Storage increase** - ~100-200 bytes/chunk for context (minimal impact)

### Mitigations

1. **Batching** - Process chunks by file for prompt caching
2. **Pattern fallback** - Always works without external dependencies
3. **Configurable** - Can disable contextual enrichment if needed

---

## Implementation

### Files Created

| File | Purpose |
|------|---------|
| `internal/index/contextual.go` | ContextGenerator interface, HybridContextGenerator |
| `internal/index/contextual_llm.go` | LLMContextGenerator (Ollama) |
| `internal/index/contextual_pattern.go` | PatternContextGenerator (fallback) |
| `internal/index/contextual_test.go` | Unit tests |

### Files Modified

| File | Change |
|------|--------|
| `cmd/amanmcp/cmd/index.go` | Add contextual stage after chunking |
| `internal/config/config.go` | Add ContextualConfig |
| `internal/ui/ui.go` | Add StageContextual constant |

### ContextGenerator Interface

```go
type ContextGenerator interface {
    GenerateContext(ctx context.Context, chunk *store.Chunk, docContext string) (string, error)
    GenerateBatch(ctx context.Context, chunks []*store.Chunk, docContext string) ([]string, error)
    Available(ctx context.Context) bool
    ModelName() string
    Close() error
}
```

### Configuration

```yaml
contextual:
  enabled: true
  model: "qwen3:0.6b"
  timeout: "5s"
  batch_size: 8
  fallback_only: false  # Use pattern-only mode
```

### LLM Prompt Template

```
You are analyzing code. Generate a 1-2 sentence context for this code chunk.

File: {file_path}

Document context:
{doc_context}

Code chunk:
{chunk_content}

Instructions:
- Describe what this code does and its purpose
- Be specific about function names and types
- Keep it under 100 tokens
- Output ONLY the context, no preamble

Context:
```

---

## Validation Criteria

This ADR is successfully implemented when:

- [x] ContextGenerator interface defined
- [x] LLMContextGenerator implemented with Ollama
- [x] PatternContextGenerator fallback implemented
- [x] HybridContextGenerator combines both
- [x] Integrated into indexing pipeline
- [x] Configuration added to config.go
- [x] StageContextual added to UI
- [ ] Dogfood queries pass (pending reindex)
- [x] `make ci-check` passes

---

## References

- [Anthropic Contextual Retrieval](https://www.anthropic.com/news/contextual-retrieval)
- [RCA-010 - Vocabulary Mismatch Analysis](../dogfooding/rca-010.md)
- [F33 - Contextual Retrieval Feature Spec](../specs/features/F33-contextual-retrieval.md)
- [ADR-023 - Ollama HTTP API](./ADR-023-ollama-http-api-embedder.md)

---

## Changelog

| Date | Change |
|------|--------|
| 2026-01-08 | Initial implementation |
