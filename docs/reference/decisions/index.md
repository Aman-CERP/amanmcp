# Architecture Decision Records

**Version:** 1.2.0
**Last Updated:** 2026-01-11

This is the authoritative index of all Architecture Decision Records (ADRs) for AmanMCP.

---

## ADR Status

| Status | Meaning |
|--------|---------|
| **Proposed** | Under discussion |
| **Accepted** | Decision made, not yet implemented |
| **Implemented** | Decision made and code reflects it |
| **Deprecated** | No longer applies |
| **Superseded** | Replaced by another ADR |

---

## ADR Registry

### Core Architecture

| ADR | Title | Status | Date |
|-----|-------|--------|------|
| [ADR-001](./ADR-001-vector-database-usearch.md) | USearch for Vector Storage | Superseded | 2025-12-28 |
| [ADR-002](./ADR-002-embedding-model-nomic.md) | Nomic Embed Text for Embeddings | Superseded | 2025-12-28 |
| [ADR-003](./ADR-003-tree-sitter-chunking.md) | Tree-sitter for Code Chunking (Official Bindings) | Implemented | 2025-12-28 |
| [ADR-004](./ADR-004-hybrid-search-rrf.md) | Hybrid Search with RRF Fusion | Implemented | 2025-12-28 |
| [ADR-005](./ADR-005-hugot-embedder.md) | Hugot as Default Embedding Provider | Superseded | 2025-12-30 |
| [ADR-022](./ADR-022-cgo-minimal-standalone-architecture.md) | CGO-Minimal Standalone Architecture | Accepted | 2026-01-03 |
| [ADR-012](./ADR-012-bm25-implementation.md) | BM25 Implementation (Bleve v2) | Accepted | 2025-12-28 |
| [ADR-016](./ADR-016-ollama-removal-embeddinggemma-default.md) | Ollama Removal + EmbeddingGemma Default | Amended | 2025-12-31 |
| [ADR-017](./ADR-017-process-isolation.md) | Process Isolation for Multi-Project Support | Accepted | 2025-12-31 |
| [ADR-023](./ADR-023-ollama-http-api-embedder.md) | Ollama HTTP API Embedder Re-introduction | Implemented | 2026-01-04 |

### Infrastructure & Tooling

| ADR | Title | Status | Date |
|-----|-------|--------|------|
| [ADR-010](./ADR-010-mcp-protocol-version.md) | MCP Protocol 2025-11-25 | Implemented | 2025-12-28 |
| [ADR-011](./ADR-011-version-pinning.md) | Version Pinning Strategy | Implemented | 2025-12-28 |
| [ADR-013](./ADR-013-cgo-environment-setup.md) | CGO Environment Setup Strategy | Implemented | 2025-12-30 |
| [ADR-014](./ADR-014-mcp-capability-signaling.md) | MCP Capability Signaling for Embedder State | Implemented | 2025-12-30 |

### Process & Documentation

| ADR | Title | Status | Date |
|-----|-------|--------|------|
| [ADR-020](./ADR-020-documentation-architecture.md) | Documentation Architecture (SSOT) | Implemented | 2025-12-28 |
| [ADR-021](./ADR-021-tdd-workflow.md) | TDD as Mandatory Practice | Implemented | 2025-12-28 |

### Performance & Optimization

| ADR | Title | Status | Date |
|-----|-------|--------|------|
| [ADR-032](./ADR-032-query-pattern-telemetry.md) | Query Pattern Telemetry | Accepted | 2026-01-06 |
| [ADR-033](./ADR-033-contextual-retrieval.md) | Contextual Retrieval for Search Quality | Implemented | 2026-01-08 |
| [ADR-034](./ADR-034-query-expansion-bm25.md) | Query Expansion for BM25 Search | Implemented | 2026-01-08 |
| [ADR-035](./ADR-035-mlx-default-embedder.md) | MLX as Default Embedder on Apple Silicon | Superseded | 2026-01-08 |
| [ADR-036](./ADR-036-multi-backend-embedding-testing.md) | Multi-Backend Embedding Model Testing Framework | Accepted | 2026-01-11 |
| [ADR-037](./ADR-037-ollama-default-embedder.md) | Ollama as Default Embedder on All Platforms | Implemented | 2026-01-14 |

---

## How to Create an ADR

1. Copy `000-template.md` to `ADR-XXX-short-title.md`
2. Fill in all sections
3. Add to this index
4. Get review/approval
5. Update status as implementation progresses

---

## ADR Numbering

| Range | Category |
|-------|----------|
| 001-009 | Core Architecture |
| 010-019 | Infrastructure & Tooling |
| 020-029 | Process & Documentation |
| 030-039 | Performance & Optimization |
| 040-049 | Security |

---

## Related Documents

- [Tech Debt Registry](../tech-debt/index.md) - Deferred decisions
- [Feature Specs](../specs/features/index.md) - Implementation details

---

*Decisions are permanent records. Think carefully before accepting.*
