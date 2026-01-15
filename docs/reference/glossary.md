# Glossary

**Version:** 1.0.0
**Last Updated:** 2025-12-28

Definitions of terms used throughout AmanMCP documentation and code.

---

## Search & Retrieval

| Term | Definition |
|------|------------|
| **BM25** | Best Match 25. A ranking function for keyword search based on term frequency and inverse document frequency. |
| **Chunk** | A semantic unit of code (function, class, method) extracted for indexing. Smaller than a file, larger than a line. |
| **Cosine Similarity** | Measure of similarity between two vectors based on the angle between them. Range: -1 to 1. |
| **Embedding** | A dense vector representation of text that captures semantic meaning. Typically 384-1536 dimensions. |
| **HNSW** | Hierarchical Navigable Small World. Graph-based algorithm for approximate nearest neighbor search. O(log n) complexity. |
| **Hybrid Search** | Combining multiple search methods (e.g., BM25 + vector) for better results. |
| **IDF** | Inverse Document Frequency. Weights rare terms higher than common ones. |
| **RAG** | Retrieval-Augmented Generation. Pattern where AI retrieves relevant context before generating responses. |
| **RRF** | Reciprocal Rank Fusion. Algorithm to combine ranked results from multiple sources. Formula: 1/(k + rank). |
| **Semantic Search** | Finding documents by meaning rather than exact keyword match. Uses embeddings. |
| **TF** | Term Frequency. How often a term appears in a document. |
| **Vector Database** | Database optimized for storing and querying high-dimensional vectors. |

---

## Code Parsing

| Term | Definition |
|------|------------|
| **AST** | Abstract Syntax Tree. Tree representation of code structure. |
| **CGO** | Go's foreign function interface for calling C code. Required for tree-sitter. |
| **Grammar** | Rules defining the syntax of a programming language. Used by parsers. |
| **Node** | Element in an AST representing a syntactic construct (function, class, statement). |
| **Parser** | Tool that converts source code text into an AST. |
| **Tree-sitter** | Incremental parsing library used for code understanding. Supports 100+ languages. |

---

## MCP (Model Context Protocol)

| Term | Definition |
|------|------------|
| **Client** | Application that connects to MCP servers (e.g., Claude Code, Cursor). |
| **JSON-RPC** | JSON Remote Procedure Call. Protocol used for MCP communication. |
| **MCP** | Model Context Protocol. Open protocol for AI-tool integration. |
| **Prompt** | Reusable template exposed by MCP server. |
| **Resource** | Data that an MCP server makes available (files, database entries). |
| **Server** | Application that exposes tools/resources via MCP (AmanMCP). |
| **stdio** | Standard input/output. Transport mechanism for MCP communication. |
| **Tool** | Function that an AI can call via MCP (e.g., search, lookup). |

---

## Machine Learning

| Term | Definition |
|------|------------|
| **Dimension** | Number of values in an embedding vector. Higher = more nuance, more memory. |
| **F16** | Half-precision floating point. 16 bits per value. Half memory of F32. |
| **F32** | Single-precision floating point. 32 bits per value. Standard precision. |
| **I8** | 8-bit integer quantization. Quarter memory of F32, some quality loss. |
| **Inference** | Running a trained model to get predictions/embeddings. |
| **Model** | Trained neural network that performs a specific task. |
| **Quantization** | Reducing precision of numbers to save memory (F32 → F16 → I8). |
| **Transformer** | Neural network architecture used in modern embedding models. |

---

## Development Process

| Term | Definition |
|------|------------|
| **AC** | Acceptance Criteria. Testable conditions that define "done" for a feature. |
| **ADR** | Architecture Decision Record. Document explaining why a decision was made. |
| **CI** | Continuous Integration. Automated testing on code changes. |
| **Feature** | Discrete unit of functionality with ID (F01, F02, etc.). |
| **Phase** | Group of related features implemented together. |
| **RCA** | Root Cause Analysis. Post-mortem document for incidents. |
| **RFC** | Request for Comments. Proposal for significant changes. |
| **SSOT** | Single Source of Truth. One authoritative location for each piece of information. |
| **TDD** | Test-Driven Development. Write tests before implementation. |

---

## Project-Specific

| Term | Definition |
|------|------------|
| **AmanMCP** | This project. Local-first RAG MCP server for developers. |
| **chromem-go** | Pure Go in-memory vector database. Fallback for small codebases. |
| **EmbeddingGemma** | Code-optimized embedding model. 768 dimensions, 2048 token context. Default model. |
| **Hugot** | Go library for running ONNX models locally. Used for embedding generation. |
| **USearch** | High-performance vector search library. Primary vector store. |

---

## Metrics & Performance

| Term | Definition |
|------|------------|
| **Latency** | Time from request to response. Target: <100ms P95. |
| **P95** | 95th percentile. 95% of requests complete faster than this. |
| **Recall** | Percentage of relevant results actually retrieved. |
| **Throughput** | Requests processed per second. |

---

## Adding Terms

When adding new terms:

1. Place in appropriate category
2. Keep definitions concise (1-2 sentences)
3. Link to related terms if helpful
4. Update alphabetically within category

---

*Shared vocabulary enables clear communication.*
