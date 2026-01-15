# ADR-017: Keep Process Isolation for Multi-Project Support

**Date:** 2025-12-31
**Status:** Accepted
**Deciders:** AmanMCP Team
**Related:** F26, F27, F28, F-007

---

## Context

Users working on multiple projects (particularly monorepos with git submodules) requested multi-project support. The initial requirements document (F-007) specified "single server instance handles multiple projects."

We needed to decide between:

1. **Multi-project server**: Single AmanMCP instance indexing multiple projects
2. **Process isolation**: Separate AmanMCP instance per project (current architecture)

---

## Decision

**Keep single-project-per-server architecture.** Do NOT build a multi-project server.

Instead, add features to reduce the friction of working with multiple projects:

- **F26: Git Submodule Support** - Index submodule content within a project
- **F27: Session Management** - Named sessions for fast project switching
- **F28: Scope Filtering** - Path prefix filtering for monorepo search

---

## Rationale

### 1. Process Isolation Provides Security Boundaries

OS-level process isolation ensures:
- No memory sharing between projects
- No file descriptor leakage
- Clean resource management
- Crash isolation (one project can't crash another)

### 2. Context Pollution Degrades RAG Quality

Research on RAG systems consistently shows that mixing contexts reduces search quality:

> "Per-repository sharding is recommended for code search. Mixed repositories lead to irrelevant cross-contamination in results." — [Qodo AI: RAG for Large Scale Code Repos](https://www.qodo.ai/blog/rag-for-large-scale-code-repos/)

### 3. Industry Best Practices

Major code search and RAG systems use namespace isolation:

**Pinecone Multi-Tenancy:**
> "Use separate namespaces or indexes per tenant. Never mix tenant data in a shared vector space." — [Pinecone Multi-Tenancy Guide](https://www.pinecone.io/learn/series/vector-databases-in-production-for-busy-engineers/vector-database-multi-tenancy/)

**Sourcegraph Zoekt:**
> "Each repository is indexed separately and searched independently." — [Sourcegraph Architecture](https://github.com/sourcegraph/zoekt)

### 4. Simpler Architecture

Multi-project server would require:
- Namespace prefixing for all chunk IDs
- Per-project query routing
- Cross-project cache management
- Complex index versioning
- Project-aware embeddings

Single-project architecture avoids this complexity entirely.

### 5. User Workflow Already Supports It

Claude Code and other AI assistants can already run multiple MCP servers concurrently. The "one server per project" model aligns with how these tools work.

---

## Consequences

### Positive

- Clean separation of concerns
- No context pollution between projects
- Simpler codebase, lower maintenance
- Easier debugging (one project per process)
- Follows industry best practices

### Negative

- Users must manage multiple server instances
- Memory overhead for multiple Go processes (~50-100MB each)
- Switching projects requires stopping/starting servers

### Mitigations

The negative consequences are addressed by:

- **F27 (Session Management)**: Reduces switching friction with named sessions
- **F26 (Submodule Support)**: Brings related repos into single index
- **F28 (Scope Filtering)**: Allows focusing on specific paths within large monorepos

---

## Alternatives Considered

### Multi-Project Server with Namespace Isolation

**Rejected because:**
- Adds complexity without clear benefit over process isolation
- Still requires careful isolation to avoid context pollution
- Not how users actually work (one active project at a time)

### Shared Index with Project Filtering

**Rejected because:**
- Mixing embeddings from different codebases degrades search quality
- Cross-project results are rarely helpful
- Security concerns (secrets from project A visible in project B)

---

## References

- [Pinecone Multi-Tenancy Guide](https://www.pinecone.io/learn/series/vector-databases-in-production-for-busy-engineers/vector-database-multi-tenancy/)
- [RAG for Large Scale Code Repos](https://www.qodo.ai/blog/rag-for-large-scale-code-repos/)
- [Sourcegraph Zoekt Architecture](https://github.com/sourcegraph/zoekt)
- [RAG Best Practices 2025](https://arxiv.org/abs/2501.07391)

---

## Changelog

| Date | Change |
|------|--------|
| 2025-12-31 | Initial decision |
