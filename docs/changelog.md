# Changelog

Public-facing highlights of major changes. See `.aman-pm/changelog/` for detailed version history.

---

## [v0.14.0] - 2026-06-20

- **Update**: Minor improvements and fixes


## [v0.12.1] - 2026-05-03

- **Update**: Minor improvements and fixes


## [v0.12.0] - 2026-05-02

- **Pre-index secret scanning**: Add a compact detector package and runner gate for high-confidence secrets
- **MCP Go SDK**: Upgrade `github.com/modelcontextprotocol/go-sdk` from `v1.2.0` to `v1.4.1`


## [v0.10.1] - 2026-01-15

- **PM System**: Sprint 8 closure and state synchronization (6/7 items completed)

## [v0.10.0] - 2026-01-15

- **Feature**: Lazy background compaction for HNSW vector index
  - Automatically detects orphan vectors from lazy deletion
  - Triggers during idle periods (no searches for 30s)
  - Zero-config with sensible defaults

## [v0.9.0] - 2026-01-15

- **Feature**: Zero-config Ollama lifecycle management in `amanmcp init`
- **Feature**: Enhanced `amanmcp setup` with `--check`, `--auto`, `--offline` flags
- **Feature**: Add `--explain` flag to `amanmcp search` for transparency
- **Fix**: Silent fallback to static embeddings when Ollama unavailable

## [v0.8.2] - 2026-01-14

- **Feature**: RRF weights and fusion constant now configurable via `.amanmcp.yaml`
- **Feature**: Environment variable `AMANMCP_RRF_CONSTANT` for overriding RRF k parameter

## [v0.7.2] - 2026-01-14

- **Documentation**: Comprehensive `.amanmcp.yaml` auto-generation and search pollution prevention docs

## [v0.7.0] - 2026-01-14

- **Feature**: Test file deprioritization (0.5x score penalty)
- **Feature**: Path-based scoring (internal/ boosted 1.3x, cmd/ penalized 0.6x)
- **Fix**: Multi-query consensus favored wrappers over implementations
- **Fix**: Search results varied based on limit parameter

## [v0.6.0] - 2026-01-14

- **Feature**: SQLite FTS5 BM25 backend with concurrent multi-process access (default)
- **Feature**: `--local` flag for search command to bypass daemon
- **Fix**: CLI blocked when MCP server running (BoltDB exclusive lock issue)


---

**Full changelog:** See `.aman-pm/changelog/CHANGELOG.md` for complete version history with 110+ versions.
