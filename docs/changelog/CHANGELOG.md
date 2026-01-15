# Changelog

All notable changes to AmanMCP are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

See [unreleased.md](./unreleased.md) for work in progress.

---

## [0.10.1] - 2026-01-15

### PM System Updates

Sprint 8 closure and PM state synchronization.

### Changed

- Sprint 8 closed (6/7 items completed, 86%)
- FEAT-MEM3 deferred (MLX batch processing - Ollama is default backend)
- Velocity tracking updated for Sprint 8

See [v0.10.1.md](./0.10/v0.10.1.md) for full details.

---

## [0.10.0] - 2026-01-15

### Lazy Background Compaction

Automatic, invisible memory optimization for HNSW vector index.

### Added

- **FEAT-AI3**: Lazy background compaction for HNSW vector index
  - Automatically detects orphan vectors from lazy deletion
  - Triggers compaction during idle periods (no searches for 30s)
  - Threshold-based: only runs when >20% orphans and >100 orphan count
  - Fully interruptible: cancels immediately if search request arrives
  - Zero-config: works out of the box with sensible defaults
  - Configurable via `.amanmcp.yaml` or env vars for power users

See [v0.10.0.md](./0.10/v0.10.0.md) for full details.

---

## [0.9.0] - 2026-01-15

### Zero-Config UX

True 3-command setup for amanmcp. Ollama lifecycle is now fully automated.

### Added

- **DEBT-026**: Zero-config Ollama lifecycle management in `amanmcp init`
- **DEBT-026**: New `internal/lifecycle` package for Ollama lifecycle management
- **DEBT-026**: Enhanced `amanmcp setup` command with `--check`, `--auto`, `--offline` flags
- **FEAT-UNIX3**: Add `--explain` flag to `amanmcp search` for transparency

### Fixed

- **BUG-073**: Fix silent fallback to static embeddings when Ollama unavailable

### Documentation

- **DEBT-026**: Simplify README Quick Start to 3 commands

See [v0.9.0.md](./0.9/v0.9.0.md) for full details.

---

## [0.8.2] - 2026-01-14

### Data-Driven Configuration (FEAT-UNIX2)

RRF weights and fusion constant are now configurable via `.amanmcp.yaml` and environment variables.

### Added

- **FEAT-UNIX2**: RRF weights (`bm25_weight`, `semantic_weight`) configurable via `.amanmcp.yaml`
- **FEAT-UNIX2**: RRF constant (`rrf_constant`) configurable via `.amanmcp.yaml`
- **FEAT-UNIX2**: Environment variable `AMANMCP_RRF_CONSTANT` for overriding RRF k parameter

### Fixed

- Validation tests now skip gracefully when no index exists (fixes CI failures)

See [v0.8.2.md](./0.8/v0.8.2.md) for full details.

---

## [0.7.2] - 2026-01-14

### Documentation Improvements

Comprehensive documentation updates for `.amanmcp.yaml` auto-generation and search pollution prevention.

### Documentation

- Add detailed comments to `generateAmanmcpYAML()` explaining auto-generation workflow
- Update `configs/project-config.example.yaml` with sensible defaults for AI assistants and PM systems
- Add "Preventing Search Pollution" section to configuration reference

See [v0.7.2.md](./0.7/v0.7.2.md) for full details.

---

## [0.7.0] - 2026-01-14

### Search Quality Improvements

Path-based scoring and test file deprioritization for better code search results.

### Added

- **Test file deprioritization**: 0.5x score penalty (FEAT-QI4)
- **Path-based scoring**: internal/ boosted 1.3x, cmd/ penalized 0.6x (BUG-066)

### Fixed

- **BUG-066**: Multi-query consensus favored wrappers over implementations
- **BUG-067**: Search results varied based on limit parameter
- **BUG-065**: CI check validation test exclusion

See [v0.7.0.md](./0.7/v0.7.0.md) for full details.

---

## [0.6.0] - 2026-01-14

### SQLite FTS5 BM25 Backend (REARCH-002)

Major architectural change: Replace Bleve/BoltDB with SQLite FTS5 for BM25 keyword search. This enables concurrent multi-process access, fixing BUG-064 where CLI search was blocked when the MCP server was running.

### Added

- **SQLite FTS5 BM25 backend** with WAL mode (multiple readers + single writer)
- **BM25 backend factory** for backend selection (sqlite/bleve)
- **`--local` flag** for search command to bypass daemon

### Changed

- **Default BM25 backend is now SQLite** for better concurrent access
- **Config option `bm25_backend`** to explicitly select backend

### Fixed

- **BUG-064**: CLI blocked when MCP server running (BoltDB exclusive lock issue)

See [v0.6.0.md](./0.6/v0.6.0.md) for full details.

---

## [0.5.0] - 2026-01-14

### Tiered Validation for Fast CI

Performance release implementing tiered validation (DEBT-025) that makes commit validation 90% faster.

See [v0.5.0.md](./0.5/v0.5.0.md) for full details.

---

## [0.4.0] - 2026-01-13

### Multi-Query Fusion + BM25 Index Auto-Recovery

Feature release adding multi-query fusion for improved generic query handling and automatic BM25 index corruption recovery.

### Added

- **FEAT-QI3**: Multi-Query Fusion - Pattern-based decomposition transforms queries like "How does RRF fusion work" into multiple specific sub-queries. Improves Tier 1 validation from 75% to 83%.

### Fixed

- **BUG-049**: BM25 index corruption after binary rebuild. Adds integrity validation on open with auto-recovery. Corrupted indices are detected and cleared automatically.

See [v0.4.0.md](./0.4/v0.4.0.md) for full details.

---

## [0.3.3] - 2026-01-13

### MLX Default on Apple Silicon

MLX is now the default embedding backend on Apple Silicon (~1.7x faster). Ollama remains default on other platforms.

### Changed

- **MLX Default**: Auto-detect uses MLX on Apple Silicon, Ollama elsewhere
- Updated CLI help, Quick Start guide, and Makefile for new defaults

See [v0.3.3.md](./0.3/v0.3.3.md) for full details.

---

## [0.3.2] - 2026-01-13

### Query Telemetry + Backlog Prioritization

Patch release adding query repetition telemetry, closing FEAT-AI5 after analysis, and reprioritizing search quality items.

### Added

- Query repetition telemetry (SPIKE-004) for data-driven decisions

### Changed

- Close FEAT-AI5 (Batched Incremental Updates) - core batching already exists
- Reprioritize search quality items: FEAT-QI3→P1, SPIKE-TH1→P2, FEAT-RR2→P4

See [v0.3.2.md](./0.3/v0.3.2.md) for full details.

---

## [0.3.1] - 2026-01-13

### BUG-060 Fix + Aman Skill Simplification

Bug fix release resolving MLX config validation issue and simplifying `/aman` skill commands.

### Fixed

- **BUG-060**: Config validation missing 'mlx' caused silent fallback to defaults (31% file variance)

### Changed

- `/aman groom --resequence` now includes apply prompt (merged `--apply-resequence`)
- `sync` and `archive` are now automatic (removed manual commands)

### Removed

- `/aman sync` command (now automatic)
- `/aman archive` command (now automatic)

See [v0.3.1.md](./0.3/v0.3.1.md) for full details.

---

### [v0.3.0](0.3/v0.3.0.md) - 2026-01-12

---

## [0.2.6] - 2026-01-12

### BUG-041 Fix: Explicit Embedder Selection

Bug fix release preventing silent fallback to Static768 when embedder is explicitly set.

### Fixed

- **BUG-041**: Prevent silent fallback to Static768 when `AMANMCP_EMBEDDER` is explicitly set
  - Returns clear error when explicit embedder unavailable
  - Increases cold start timeout from 5s to 180s for dimension detection

See [v0.2.6.md](./0.2/v0.2.6.md) for full details.

---

## [0.2.5] - 2026-01-12

### Sprint 4 Completion + BM25-Only Mode

Completes Sprint 4 (100%). Adds `--bm25-only` flag for keyword-only search and enhanced dimension mismatch UX.

### Added

- `--bm25-only` flag for CLI search command (FEAT-DIM1)
- `BM25Only` option in SearchOptions and daemon protocol (FEAT-DIM1)
- Enhanced dimension mismatch warning with recovery options (FEAT-DIM1)
- Detailed reranker telemetry (HTTP timing, phase breakdown) (DEBT-024)

### Documentation

- `docs/guides/two-stage-retrieval.md` - Bi-encoder vs cross-encoder architecture

See [v0.2.5.md](./0.2/v0.2.5.md) for full details.

---

## [0.2.4] - 2026-01-12

### Backend Decision + Observability

Ollama becomes default backend based on data-driven benchmark. Adds comprehensive indexing observability and MLX one-command install.

### Added

- `amanmcp index info` command for index configuration visibility (FEAT-DIM2)
- Indexing observability with stage timing, throughput, backend info (FEAT-OBS1)
- `make install-mlx` one-command MLX setup with model download (FEAT-MLX2)
- `scripts/benchmark-backends.sh` for backend comparison

### Changed

- **Ollama Default Backend**: Ollama is now default (MLX via `--backend=mlx`)
- **Backend Flag**: `--backend` flag for easy switching (ollama, mlx, static)

### Fixed

- BUG-042: CLI index now stores dimension/model metadata

See [v0.2.4.md](./0.2/v0.2.4.md) for full details.

---

## [0.2.3] - 2026-01-11

### Cross-Encoder Reranking

Adds cross-encoder reranking via Qwen3-Reranker for improved search relevance.

### Added

- Cross-encoder reranking with Qwen3-Reranker-0.6B (FEAT-RR1)
- `/rerank` endpoint in MLX server
- `Reranker` interface and `MLXReranker` client

### Fixed

- Daemon not writing logs to file (BUG-040)
- Index command hanging on embedder initialization (BUG-040)
- Stale serve.pid files not cleaned up (BUG-040)

See [v0.2.3.md](./0.2/v0.2.3.md) for full details.

---

## [0.2.2] - 2026-01-10

### Unified /aman Command

Consolidates 7 PM skills into unified `/aman` command with auto-increment sprint start, sprint-end ceremony, and auto-versioning release.

### Added

- Unified `/aman` command (replaces 7 separate PM skills)
- Auto-increment for `/aman sprint-start`
- `/aman sprint-end` command for closing sprints with ceremony
- Auto-versioning for `/aman release` with `--patch`, `--minor`, `--major` flags

### Changed

- Restructure aman-pm backlog directories for consistency
- Consolidate all PM skills into unified /aman namespace

### Fixed

- Flaky TestParser_Performance_Parse1000LOC test

See [v0.2.2.md](./0.2/v0.2.2.md) for full details.

---

## [0.2.1] - 2026-01-10

### MLX Memory Optimization + CR-1 Tuning

Reduces idle memory from 15-35GB to <100MB and improves code search quality.

### Added

- MLX lazy loading with TTL auto-unload (FEAT-MEM1)
- Qwen3 query instruction format for better retrieval (RCA-015)
- Contextual retrieval `code_chunks` config option

### Changed

- MLX default model: 8B → 0.6B (94% quality, 5x less memory)
- Default weights: BM25=0.65, Semantic=0.35 (favor keyword matching)

### Fixed

- Search weight env var overrides for debugging

See [v0.2.1.md](./0.2/v0.2.1.md) for full details.

---

## [0.2.0] - 2026-01-10

### AI-Native PM System Complete

Major milestone: Full AI-native project management system with memory awareness.

### Added

- AI-Native PM system v2.0 with memory awareness and sprint system
- 7 PM skills: /aman-pm, /session-start, /session-end, /backlog-groom, /sprint-report, /release, /rearchitect
- Knowledge persistence system (learnings.md, decisions.md, velocity.json)
- Session handoff system with accountability documents
- Development plan sequencing system
- 128 backlog items migrated with full metadata

### Changed

- Reorganized `.aman-pm/` directory structure
- Skill registry updated to v3.3.0

See [v0.2.0.md](./0.2/v0.2.0.md) for full details.

---

## [0.1.69] - 2026-01-09

### AI-Native PM Session Handoff System

Session handoff system for accountability, release skill for version management, and changelog reorganization.

### Added

- AI-Native PM session handoff system with accountability documents (Phase 12)
- Comprehensive AI-Native PM Guide and quick reference
- `/release` skill for version management with changelog automation
- ConsistencyChecker and Index Runner for code quality (DEBT-021, DEBT-022)

### Changed

- Changelog directory structure reorganized by minor version (`docs/changelog/0.1/`)
- Session skills enhanced for handoff generation (v3.0.0)
- Backlog items populated with full context (Phase 11 data integrity fix)

### Fixed

- Config init bypassing MLX auto-detection
- MLX thermal throttling timeouts

See [v0.1.69.md](./0.1/v0.1.69.md) for full details.

---

## [0.1.52] - 2026-01-06

### GPU Ctrl+C Fix & ETA Smoothing

Fix GPU not stopping after Ctrl+C in index command, plus ETA display smoothing for better UX.

### Changed

- Reduce HTTP IdleConnTimeout from 90s to 10s for faster cleanup

### Fixed

- GPU not stopping after Ctrl+C in `index` command (BUG-045)
- ETA fluctuation during indexing (add exponential smoothing)

See [v0.1.52.md](./0.1/v0.1.52.md) for full details.

---

## [0.1.51] - 2026-01-06

### Checkpoint/Resume for Indexing

Resume interrupted indexing operations with per-batch checkpointing. Critical bug fixes for process hangs and embedding timeouts.

### Added

- Checkpoint infrastructure for resumable indexing
- `--resume` flag for `init` and `index` commands
- Per-batch (32 chunks) checkpointing during embedding

### Fixed

- Context deadline exceeded timeout regression (QW-4)
- Process hangs after indexing error (BUG-044)

See [v0.1.51 details](./0.1/v0.1.51.md)

---

## [0.1.50] - 2026-01-06

### TUI Progress Display & Embedding Fixes

Major UX improvements for indexing: TUI progress display with spinner, progress bar, and ETA.

### Added

- TUI progress display with stage pipeline visualization
- `CachedEmbedder.Inner()` method for progress callback access
- `--no-tui` flag to force plain text output

### Fixed

- Embedding progress not displayed (CachedEmbedder wrapper issue)
- Context deadline exceeded on large codebases (increased cold timeout to 120s)

See [v0.1.50 details](./0.1/v0.1.50.md)

---

## [0.1.40] - 2026-01-04

### Bug Fixing Sprint: Index Integrity & File Watching

Fixed 7 bugs from code audit of index update/removal and gitignore handling.

### Fixed

- **BUG-022**: Scanner gitignore cache invalidation on .gitignore changes
- **BUG-023**: Best-effort delete pattern (metadata source of truth)
- **BUG-025**: RWMutex for gitignore reload race prevention
- **BUG-026**: Log warning for orphan file delete failures
- **BUG-027**: Config file change detection with reconciliation
- **BUG-029**: Log warnings for nested gitignore errors
- **BUG-031**: RowsAffected check in DeleteChunks

### Added

- OllamaEmbedder for Ollama API-based neural embeddings
- YzmaEmbedder using hybridgroup/yzma v1.4.0
- Graceful embedder fallback chain: Ollama → Yzma → Static
- `OpConfigChange` operation type for config file watching

See [v0.1.40 details](./0.1/v0.1.40.md)

---

## [0.1.39] - 2026-01-03

### USearch Removal & CI Optimization

- **Complete USearch removal** from build infrastructure
  - Removed `lib/` directory (libusearch_c.dylib, usearch.h)
  - Cleaned CI workflows, goreleaser config, install scripts
- **CI Optimization**: Reduced from 4 jobs to 3 (removed redundant Build job)
- **BUG-020**: Fixed invalid default embedder config (hugot→llama)

### Fixed

- Default embedder config now correctly uses `llama/nomic-embed-text-v1.5/768`
- golangci-lint version aligned between Makefile and CI (v2.7.2)

See [v0.1.39 details](./0.1/v0.1.39.md)

---

## [0.1.38] - 2026-01-03

### BUG-018 Fix: Pure Go Embeddings

- **BUG-018**: CLI search no longer hangs from `~/.local/bin/`
  - Replaced USearch (CGO) with coder/hnsw (pure Go)
  - Replaced Hugot (CGO) with gollama.cpp (purego)

### Added

- **LlamaEmbedder**: Neural embeddings with Metal GPU acceleration
  - Uses nomic-embed-text-v1.5 (768 dimensions)
  - Auto-downloads model on first use
- **Daemon Infrastructure**: Background search service
  - Unix socket IPC, JSON-RPC 2.0 protocol
  - Multi-project support with LRU eviction

See [v0.1.38 details](./0.1/v0.1.38.md)

---

## [0.1.37] - 2026-01-02

### Maintenance Release: Polish & Cleanup

- **DEBT-004**: Signal Handler Leaks - replaced with `signal.NotifyContext()`
- **DEBT-011**: Cache Size Hardcoded - added configurable `CacheSizeMB`
- **DEBT-009**: Error Wrapping Inconsistent - wrapped 26 bare `return err` across 7 files

### Fixed

- All error returns now include context for better debugging
- Signal handlers properly cleaned up on context cancellation
- SQLite cache size configurable (default 64MB)

**Maintenance Complete**: All 18 items (4 bugs + 14 tech debts) resolved.

See [v0.1.37 details](./0.1/v0.1.37.md)

---

## [0.1.36] - 2026-01-02

### Maintenance Release: API Consistency

- **DEBT-013**: Negative Cursor Accepted - added cursor validation
- **DEBT-005**: MarkdownChunker Missing Close - added Close() method
- **DEBT-014**: File Close Error Suppressed - now logs warning
- **DEBT-012**: Nil vs Empty Slice Returns - standardized 10 functions

### Fixed

- All slice-returning functions now return `[]T{}` instead of `nil`
- Cursor offset validation rejects negative values

See [v0.1.36 details](./0.1/v0.1.36.md)

---

## [0.1.35] - 2026-01-02

### Maintenance Release: Embedding & Locks

- **DEBT-008**: Double Lock in Close() - refactored to single lock acquisition
- **DEBT-010**: Batch Size Unbounded - added 1-256 validation
- **DEBT-007**: Lock Lifecycle Unclear - added state tracking and idempotent unlock
- **DEBT-006**: Windows flock Incompatibility - replaced with cross-platform gofrs/flock

### Fixed

- File locking now works on Windows, macOS, and Linux
- Batch size clamped to valid range with warning

See [v0.1.35 details](./0.1/v0.1.35.md)

---

## [0.1.34] - 2026-01-02

### Maintenance Release: Concurrency & Timeouts

- **BUG-003**: Race condition in BM25 Load - already correct, added race test
- **DEBT-002**: Watcher goroutine cleanup - already correct, uses errgroup
- **BUG-004**: Model download timeout - added `downloadModelWithTimeout()` wrapper

### Fixed

- Model downloads now have configurable timeout (default: 10 minutes)
- All bugs resolved (5/5)

See [v0.1.34 details](./0.1/v0.1.34.md)

---

## [0.1.33] - 2026-01-02

### Maintenance Release: Resource Safety

- **DEBT-003**: Scanner channel abandonment - added 6 edge-case tests
- **DEBT-001**: Gitignore cache unbounded - added 2 tests verifying LRU behavior
- **BUG-002**: File size validation - added 100MB limit before ReadFile
- **BUG-005**: Symlink handling - added Lstat detection, skip symlinks

See [v0.1.33 details](./0.1/v0.1.33.md)

---

## [0.1.32] - 2026-01-01

### Validated

- F24: Release Packaging
  - GoReleaser configuration for darwin/arm64 and darwin/amd64
  - GitHub Actions release workflow
  - Version command with `--json` and `--short` flags
  - Homebrew formula auto-generation
  - Release script with validation

### Milestone

- **Phase 3 Polish & Release: 100% complete** (14/14 features)
- **Overall: 97% complete** (31/32 features)
- Phase 3 features complete (v1.0 prerequisites met, release pending full validation)

See [v0.1.32 details](./0.1/v0.1.32.md)

---

## [0.1.31] - 2026-01-01

### Validated

- F29: Zero-Friction Startup
  - Auto-download USearch library (~180KB) on first run
  - Background indexing with thread-safe progress tracking
  - MCP progress reporting via index_status tool
  - Unified entry point with re-exec pattern for CGO
  - 44 tests total (async: 23, libloader: 21)

### Milestone

- **Phase 3 Polish & Release: 94% complete** (13/14 features)
- **Overall: 94% complete** (30/32 features)

See [v0.1.31 details](./0.1/v0.1.31.md)

---

## [0.1.30] - 2025-01-01

### Validated

- F27: Session Management
  - `--session=NAME` creates/loads named sessions
  - `resume`, `switch`, `sessions` commands
  - Atomic persistence, configurable storage
  - 42 tests, 80% coverage

### Milestone

- **Phase 3 Polish & Release: 94% complete** (12/13 features)
- **Overall: 94% complete** (29/31 features)

See [v0.1.30 details](./0.1/v0.1.30.md)

---

## [0.1.29] - 2025-12-31

### Validated

- F26: Git Submodule Support
  - Opt-in via `submodules.enabled: true` in config
  - `.gitmodules` parsing with path, URL, branch extraction
  - Recursive nested submodule discovery
  - Include/exclude pattern filtering
  - Uninitialized submodule detection with graceful skip
  - Circular reference protection
  - Performance: 0.156ms for 10 submodules (target: < 100ms)

### Milestone

- **Phase 3 Polish & Release: 85% complete** (11/13 features)
- **Overall: 90% complete** (28/31 features)

See [v0.1.29 details](./0.1/v0.1.29.md)

---

## [0.1.28] - 2025-12-31

### Validated

- F28: Monorepo Scope Filtering
  - `--scope` CLI flag for path prefix filtering (repeatable)
  - MCP search tools accept `scope` array parameter
  - Multiple scopes use OR logic
  - Case-sensitive path boundary matching
  - <100ns single scope, <500ns multi-scope overhead

### Milestone

- **Phase 3 Polish & Release: 77% complete** (10/13 features)
- **Overall: 87% complete** (27/31 features)

See [v0.1.28 details](./0.1/v0.1.28.md)

---

## [0.1.27] - 2025-12-31

### Validated

- F22.5: Production Hardening
  - Project detection from go.mod, package.json, pyproject.toml
  - Structured logging with request IDs for debugging
  - Proper error handling (Scanner.New() returns error)
  - Graceful degradation (UserHomeDir fallback, coordinator warnings)
  - ModelDownloadTimeout configuration (10 min default)

### Milestone

- **Phase 3 Polish & Release: 69% complete** (9/13 features)
- **Overall: 84% complete** (26/31 features)

See [v0.1.27 details](./0.1/v0.1.27.md)

---

## [0.1.26] - 2025-12-31

### Validated

- F25b: Adaptive Embedder (Auto-Recovery)
  - Immediate startup with Static768 fallback when Hugot unavailable
  - Background recovery with exponential backoff (1s → 512s, max 10 retries)
  - Dimension-safe hot-swap (blocks if dimensions mismatch)
  - Thread-safe with RWLock pattern
  - BUG-001 fix: search.go uses embedder.Dimensions()

### Milestone

- **Phase 3 Polish & Release: 78% complete** (7/9 features - F19, F20, F21, F22, F25a, F25b, F25c)
- **Overall: 89% complete** (24/27 features)

See [v0.1.26 details](./0.1/v0.1.26.md)

---

## [0.1.25] - 2025-12-31

### Validated

- F22: Error Handling & Resilience
  - AmanError structured type with codes, categories, severity
  - Retry strategy with exponential backoff and jitter
  - Circuit breaker (opens after 5 failures, half-open recovery)
  - User-friendly formatting with suggestions
  - MCP error code mapping
  - Three P1 tech debt fixes (DEBT-001, DEBT-002, DEBT-003)

### Milestone

- **Phase 3 Polish & Release: 67% complete** (6/9 features - F19, F20, F21, F22, F25a, F25c)
- **Overall: 85% complete** (23/27 features)

See [v0.1.25 details](./0.1/v0.1.25.md)

---

## [0.1.24] - 2025-12-31

### Validated

- F25c: Embedder Resilience & Dimension-Compatible Fallback
  - StaticEmbedder768: 768-dim deterministic embedder matching Hugot dimensions
  - Factory fallback: Hugot → Static768 (no re-indexing needed)
  - Retry logic with exponential backoff for model downloads
  - Embedder preflight checks in doctor command

### Milestone

- **Phase 3 Polish & Release: 56% complete** (5/9 features - F19, F20, F21, F25a, F25c)
- **Overall: 81% complete** (22/27 features)

See [v0.1.24 details](./0.1/v0.1.24.md)

---

## [0.1.23] - 2025-12-31

### Changed

- **BREAKING**: Default embedding model changed from MiniLM (384 dims) to EmbeddingGemma (768 dims)
- **BREAKING**: Removed Ollama embedding provider (`--embedder=ollama` no longer works)
- Simplified embedder architecture: Hugot → Static (removed Ollama fallback)
- Deleted ~1,150 lines of Ollama-related code

### Added

- ADR-016: Ollama Removal + EmbeddingGemma Default decision record
- EmbeddingGemma as default model (4x larger context window: 2048 vs 512 tokens)

### Documentation

- Updated README.md to remove Ollama setup section
- Updated CLAUDE.md tech stack references
- Added migration guide for Ollama users

See [v0.1.23 details](./0.1/v0.1.23.md)

---

## [0.1.22] - 2025-12-31

### Validated

- F21: Progress & Status UI - rich terminal UI and index health monitoring
  - Status command (`amanmcp status`) with JSON output support
  - Charmbracelet stack (bubbletea, bubbles, lipgloss) for TUI
  - PlainRenderer for CI/pipes, TUIRenderer ready for future integration
  - 67.1% UI package coverage

### Milestone

- **Phase 3 Polish & Release: 50% complete** (4/8 features - F19, F20, F21, F25a)
- **Overall: 85% complete** (22/26 features)

---

## [0.1.21] - 2025-12-31

### Validated

- F25a: Hugot Embedder - zero-config default embedding provider
  - All 7 test cases passed (TC1-TC7)
  - Factory fallback chain: Hugot → Ollama → Static
  - "It Just Works" - no external dependencies required

### Milestone

- **Phase 3 Polish & Release: 38% complete** (3/8 features - F19, F20, F25a)
- **Overall: 81% complete** (21/26 features)

---

## [0.1.20] - 2025-12-31

### Added

- F25a: Hugot Embedder - zero-config embedding provider
  - HugotEmbedder using knights-analytics/hugot library
  - Embedder factory with fallback chain: Hugot → Ollama → Static
  - Default model: all-MiniLM-L6-v2 (384 dims, 512 context)
  - EmbeddingGemma support via `--model=embeddinggemma` (768 dims, 2048 context)
  - Auto-download from HuggingFace on first use
  - ADR-005 updated with implementation notes

### Changed

- Default embedding provider changed from "ollama" to "hugot"
- Default embedding model changed to "minilm" (384 dimensions)
- CLI commands (index, search, serve) now use embedder factory pattern

### Milestone

- **Phase 3 Polish & Release: 29% complete** (3/7 features - F19, F20, F25a)
- **Overall: 84% complete** (21/25 features)

---

## [0.1.19] - 2025-12-30

### Validated

- F19: File Watcher - real-time file watching with incremental index updates
  - fsnotify-based primary watcher with polling fallback
  - Event debouncing (200ms window)
  - Incremental index updates (create/modify/delete)
  - Doctor command (`amanmcp doctor`) for system diagnostics
  - First-run preflight checks (disk, memory, file descriptors)

### Fixed

- BUG-001: Dimension mismatch in offline mode (serve.go)

### Milestone

- **Phase 3 Polish & Release: 29% complete** (2/7 features - F19, F20 validated)
- **Overall: 80% complete** (20/25 features)

---

## [0.1.18] - 2025-12-30

### Added

- F20: CLI Commands - "It Just Works" CLI experience
  - Smart default: `amanmcp` with no args = full setup + MCP server
  - Search command with hybrid search, filtering, and JSON output
  - Index command for standalone indexing
  - Output module for consistent CLI formatting

### Fixed

- `--offline` flag now properly passed to indexing logic
- Index tests now use static embeddings config

### Milestone

- **Phase 3 Polish & Release: 17% complete** (1/6 features - F20 validated)
- **Overall: 79% complete** (19/24 features)

---

## [0.1.17] - 2025-12-30

### Added

- F18: MCP Resources - expose indexed files to AI clients
  - Resource registration via MCP SDK's `AddResource()` method
  - Resource handlers: `RegisterResources()`, `handleReadResource()`
  - MIME type detection: 30+ extensions + special filenames
  - Security: Path traversal prevention, index-only access, 1MB limit
  - Cursor-based pagination in `ListFiles()` metadata method
  - 8 resource tests + 49 MIME type tests (77.9% coverage)

### Milestone

- **Phase 2 MCP Integration: 100% complete** (6/6 features - F13-F18 validated)
- **Overall: 75% complete** (18/24 features)

---

## [0.1.16] - 2025-12-30

### Added

- F17: MCP Tools - Four search and status tools for AI clients
  - `search` - General hybrid search returning markdown
  - `search_code` - Code-specific search with language/symbol filtering
  - `search_docs` - Documentation search preserving hierarchy
  - `index_status` - Index statistics as JSON
  - Input validation and limit clamping (1-50)
  - 46 tests with 80.2% coverage

- ADR-013: CGO Environment Setup Strategy
  - `./dev` wrapper script (zero-install CGO environment)
  - `.envrc` for direnv users (auto-load) or manual sourcing
  - Updated README with Development Setup section

### Milestone

- **Phase 2 MCP Integration: 83% complete** (5/6 features - F13-F17 validated)
- **Overall: 71% complete** (17/24 features)

---

## [0.1.15] - 2025-12-30

### Added

- F16: MCP Server Core - MCP protocol server implementation
  - Server struct with MCP SDK integration (modelcontextprotocol/go-sdk)
  - NewServer() constructor with dependency injection
  - Tool registration infrastructure (search tool placeholder for F17)
  - Resource listing and reading from MetadataStore
  - Custom error codes (-32001 to -32005) with MapError()
  - CLI: `amanmcp serve` command with --transport and --port flags
  - Graceful shutdown handling via signals
  - 31 tests with 66.0% coverage

### Milestone

- **Phase 2 MCP Integration: 67% complete** (4/6 features - F13-F16 validated)
- **Overall: 67% complete** (16/24 features)

---

## [0.1.14] - 2025-12-29

### Added

- F15: Query Classifier - adaptive search weight selection
  - HybridClassifier: LLM-first with pattern fallback
  - LLMClassifier: Ollama `/api/generate`, llama3.2:1b model
  - PatternClassifier: Regex-based fallback (error codes, quotes, paths, identifiers, NL)
  - LRU Cache: hashicorp/golang-lru/v2, 1000 entries
  - Weight mapping: LEXICAL (0.85/0.15), SEMANTIC (0.20/0.80), MIXED (0.35/0.65)
  - 18 tests with 74.7% coverage

### Milestone

- **Phase 2 MCP Integration: 50% complete** (3/6 features - F13-F15 validated)
- **Overall: 63% complete** (15/24 features)

---

## [0.1.13] - 2025-12-29

### Added

- F14: RRF Score Fusion - Reciprocal Rank Fusion algorithm
  - Formula: score(d) = Σ weight_i / (k + rank_i) with k=60
  - Deterministic tie-breaking (RRFScore → InBothLists → BM25Score → ChunkID)
  - Missing rank handling for single-list documents
  - Score normalization 0-1, preserving original scores
  - 12 tests, benchmarks 60-110x faster than targets

### Milestone

- **Phase 2 MCP Integration: 33% complete** (2/6 features - F13-F14 validated)
- **Overall: 58% complete** (14/24 features)

---

## [0.1.12] - 2025-12-29

### Added

- F13: Hybrid Search Engine - combines BM25 and vector search
  - Parallel execution with errgroup
  - Graceful degradation (BM25-only, vector-only, or hybrid)
  - Result fusion with weighted scoring (BM25: 0.35, Semantic: 0.65)
  - Filter support (content type, language, symbol type)
  - 30 tests with 88.0% coverage

### Milestone

- **Phase 2 MCP Integration: Started** (1/6 features - F13 validated)
- **Overall: 54% complete** (13/24 features)

---

## [0.1.11] - 2025-12-29

### Added

- F12: Vector Store (USearch) - HNSW approximate nearest neighbor search
  - USearchStore with F16 quantization (50% memory reduction)
  - Thread-safe with RWMutex (concurrent reads, exclusive writes)
  - Atomic persistence (temp file + rename pattern)
  - 16 tests with 82.8% coverage

### Milestone

- **Phase 1B Core Search: 100% complete** (7/7 features - F06-F12 validated)
- **Overall: 50% complete** (12/24 features)

---

## [0.1.10] - 2025-12-28

### Added

- F11: BM25 Index - keyword search for hybrid retrieval
  - BleveBM25Index using Bleve v2.5.7
  - Code-aware tokenizer (camelCase, snake_case splitting)
  - Stop word filtering (programming keywords)
  - 16 BM25 tests + 8 tokenizer tests with 85.2% coverage

### Milestone

- **Phase 1B Core Search: 86% complete** (6/7 features - F06-F11 validated)

---

## [0.1.9] - 2025-12-28

### Added

- F10: Static Embedding Fallback - offline embedding capability
  - 256-dimensional vectors with FNV-64 hash mapping
  - Code-aware tokenization (camelCase, snake_case)
  - Stop word filtering, 3-character n-grams
  - Thread-safe, zero external dependencies
  - 28 tests with 83.6% coverage

### Milestone

- **Phase 1B Core Search: 71% complete** (5/7 features - F06-F10 validated)

---

## [0.1.8] - 2025-12-28

### Added

- F09: Embedding Provider (Ollama) - local embeddings for semantic search
  - OllamaEmbedder with 768-dimensional vectors
  - Vector normalization for cosine similarity
  - Batch embedding with parallel processing
  - Model fallback (nomic-embed-text-v2-moe -> nomic-embed-text)
  - Setup command (`amanmcp setup`) with interactive/non-interactive modes
  - 22 embed tests (80.2% coverage), 14 setup tests (68.0% coverage)

### Milestone

- **Phase 1B Core Search: 57% complete** (4/7 features - F06-F09 validated)

---

## [0.1.7] - 2025-12-28

### Added

- F08: Markdown Chunker - documentation-aware chunking
  - Header-based section splitting (H1-H6)
  - Header path tracking (breadcrumb navigation)
  - Atomic block preservation (code, tables, lists, MDX)
  - YAML frontmatter extraction
  - 19 tests with 88.2% coverage

### Milestone

- **Phase 1B Core Search: 43% complete** (3/7 features - F06, F07, F08 validated)

---

## [0.1.6] - 2025-12-28

### Added

- F07: Code Chunker - AST-aware chunking with semantic boundaries
  - Context preservation (imports, package declarations)
  - Large function/class splitting with overlap
  - Fallback to line-based chunking for unsupported languages
  - 16 new tests with 89.1% coverage

### Milestone

- **Phase 1B Core Search: 29% complete** (2/7 features - F06, F07 validated)

---

## [0.1.5] - 2025-12-28

### Added

- F06: Tree-sitter Integration - first feature of Phase 1B
  - Parser wrapper with CGO resource management
  - Language registry (Go, TypeScript, TSX, JavaScript, JSX, Python)
  - Symbol extractor for functions, methods, classes, interfaces, types
  - 17 tests with 81.2% coverage

### Milestone

- **Phase 1B Core Search: Started** (1/7 features - F06 validated)

---

## [0.1.4] - 2025-12-28

### Added

- F05: Metadata Store (SQLite) - completes Phase 1A
  - SQLite database with WAL mode for concurrent reads
  - Data models: Chunk, Symbol, File, Project
  - MetadataStore interface, cascading deletes, batch operations
  - 13 tests with 85.3% coverage

### Milestone

- **Phase 1A Foundation: 100% complete** (F01-F05 validated)

---

## [0.1.3] - 2025-12-28

### Added

- F04: Gitignore Parser with comprehensive pattern matching
  - Wildcard, double-star, rooted, negation patterns
  - Directory-only patterns, nested gitignore support
  - Thread-safe implementation with sync.RWMutex
  - 20 tests with 86.8% coverage

### Fixed

- Bug #1: Path patterns (`src/temp/`) now match correctly
- Bug #2: Anchored patterns (`/temp/`) now supported
- Bug #3: `**/pattern` in gitignore files now handled

---

## [0.1.2] - 2025-12-28

### Added

- F03: File Scanner with comprehensive file discovery
  - Recursive directory traversal with early exclusion
  - Default exclusions (node_modules, .git, vendor, __pycache__, dist, build)
  - Sensitive file detection (.env, *.pem, *credentials*, etc.)
  - Binary file detection, gitignore support, symlink handling
  - Language detection (38 languages), content type classification
  - Streaming API with context cancellation
  - 25 tests with 89.7% coverage

### Discovered

- 3 gitignore bugs documented for F04 (path patterns, anchored patterns, **/pattern)

---

## [0.1.1] - 2025-12-28

### Added

- F02: Configuration System with zero-config defaults
  - `Config` struct with sensible defaults for all settings
  - YAML config file loading (`.amanmcp.yaml` / `.amanmcp.yml`)
  - Project type detection (Go, Node.js, Python)
  - Project root auto-detection (git root, config file location)
  - Source and documentation directory discovery
  - Environment variable overrides (`AMANMCP_*`)
  - 26 tests with 74.8% coverage

---

## [0.1.0] - 2025-12-28

### Added

- Initial project structure and documentation
- AI-native development workflow
- Feature catalog with 24 features across 4 phases
- Claude Code skills and agents
- CI parity checking scripts
- Learning guides for core technologies
- ADR structure and initial decisions
- Validation guide framework
- RCA/post-mortem process

### Documentation

- CLAUDE.md with full workflow integration
- Strategy and philosophy documents
- Glossary of project terms
- Tech debt registry

---

## Version History

| Version | Date | Phase | Highlights |
|---------|------|-------|------------|
| [0.10.0](./0.10/v0.10.0.md) | 2026-01-15 | PM | Lazy Background Compaction (FEAT-AI3) |
| [0.9.0](./0.9/v0.9.0.md) | 2026-01-15 | PM | Zero-Config UX (DEBT-026) |
| [0.8.2](./0.8/v0.8.2.md) | 2026-01-14 | PM | Data-Driven Configuration (FEAT-UNIX2) |
| [0.3.2](./0.3/v0.3.2.md) | 2026-01-13 | PM | Query Telemetry + Backlog Prioritization |
| [0.3.1](./0.3/v0.3.1.md) | 2026-01-13 | PM | BUG-060 Fix + Aman Skill Simplification |
| [0.3.0](./0.3/v0.3.0.md) | 2026-01-12 | PM | Release |
| [0.2.6](./0.2/v0.2.6.md) | 2026-01-12 | PM | BUG-041 Explicit Embedder Fix |
| [0.2.5](./0.2/v0.2.5.md) | 2026-01-12 | PM | Sprint 4 Completion + BM25-Only |
| [0.2.4](./0.2/v0.2.4.md) | 2026-01-12 | PM | Backend Decision + Observability |
| [0.2.3](./0.2/v0.2.3.md) | 2026-01-11 | PM | Cross-Encoder Reranking |
| [0.2.2](./0.2/v0.2.2.md) | 2026-01-10 | PM | Unified /aman Command |
| [0.2.1](./0.2/v0.2.1.md) | 2026-01-10 | PM | MLX Memory Optimization |
| [0.2.0](./0.2/v0.2.0.md) | 2026-01-10 | PM | AI-Native PM System Complete |
| [0.1.69](./0.1/v0.1.69.md) | 2026-01-09 | PM | Session Handoff System, Release Skill |
| [0.1.40](./0.1/v0.1.40.md) | 2026-01-04 | Maint | Bug Fixing Sprint - 7 bugs fixed |
| [0.1.35](./0.1/v0.1.35.md) | 2026-01-02 | Maint | Embedding & Locks - v0.1.35 Complete |
| [0.1.34](./0.1/v0.1.34.md) | 2026-01-02 | Maint | Concurrency & Timeouts |
| [0.1.33](./0.1/v0.1.33.md) | 2026-01-02 | Maint | Resource Safety |
| [0.1.32](./0.1/v0.1.32.md) | 2026-01-01 | 3 | F24 Release Packaging - Phase 3 Complete! |
| [0.1.31](./0.1/v0.1.31.md) | 2026-01-01 | 3 | F29 Zero-Friction Startup Validated |
| [0.1.30](./0.1/v0.1.30.md) | 2025-01-01 | 3 | F27 Session Management Validated |
| [0.1.29](./0.1/v0.1.29.md) | 2025-12-31 | 3 | F26 Submodule Support Validated |
| [0.1.28](./0.1/v0.1.28.md) | 2025-12-31 | 3 | F28 Scope Filtering Validated |
| [0.1.27](./0.1/v0.1.27.md) | 2025-12-31 | 3 | F22.5 Production Hardening Validated |
| [0.1.26](./0.1/v0.1.26.md) | 2025-12-31 | 3 | F25b Adaptive Embedder Validated |
| [0.1.25](./0.1/v0.1.25.md) | 2025-12-31 | 3 | F22 Error Handling Validated |
| [0.1.24](./0.1/v0.1.24.md) | 2025-12-31 | 3 | F25c Embedder Resilience Validated |
| [0.1.23](./0.1/v0.1.23.md) | 2025-12-31 | 3 | EmbeddingGemma Default + Ollama Removal |
| [0.1.22](./0.1/v0.1.22.md) | 2025-12-31 | 3 | F21 Progress & Status UI Validated |
| [0.1.21](./0.1/v0.1.21.md) | 2025-12-31 | 3 | F25a Hugot Embedder Validated |
| [0.1.20](./0.1/v0.1.20.md) | 2025-12-31 | 3 | F25a Hugot Embedder - Zero-Config Default |
| [0.1.19](./0.1/v0.1.19.md) | 2025-12-30 | 3 | F19 File Watcher Validated |
| [0.1.18](./0.1/v0.1.18.md) | 2025-12-30 | 3 | F20 CLI Commands - Phase 3 Started |
| [0.1.17](./0.1/v0.1.17.md) | 2025-12-30 | 2 | F18 MCP Resources - Phase 2 Complete |
| [0.1.16](./0.1/v0.1.16.md) | 2025-12-30 | 2 | F17 MCP Tools + ADR-013 CGO Setup |
| [0.1.15](./0.1/v0.1.15.md) | 2025-12-30 | 2 | F16 MCP Server Core |
| [0.1.14](./0.1/v0.1.14.md) | 2025-12-29 | 2 | F15 Query Classifier |
| [0.1.13](./0.1/v0.1.13.md) | 2025-12-29 | 2 | F14 RRF Score Fusion |
| [0.1.12](./0.1/v0.1.12.md) | 2025-12-29 | 2 | F13 Hybrid Search Engine - Phase 2 Started |
| [0.1.11](./0.1/v0.1.11.md) | 2025-12-29 | 1B | F12 Vector Store - Phase 1B Complete |
| [0.1.10](./0.1/v0.1.10.md) | 2025-12-28 | 1B | F11 BM25 Index |
| [0.1.9](./0.1/v0.1.9.md) | 2025-12-28 | 1B | F10 Static Embedder |
| [0.1.8](./0.1/v0.1.8.md) | 2025-12-28 | 1B | F09 Ollama Embedder |
| [0.1.7](./0.1/v0.1.7.md) | 2025-12-28 | 1B | F08 Markdown Chunker |
| [0.1.6](./0.1/v0.1.6.md) | 2025-12-28 | 1B | F07 Code Chunker |
| [0.1.5](./0.1/v0.1.5.md) | 2025-12-28 | 1B | F06 Tree-sitter Integration |
| [0.1.4](./0.1/v0.1.4.md) | 2025-12-28 | 1A | F05 Metadata Store - Phase 1A Complete |
| [0.1.3](./0.1/v0.1.3.md) | 2025-12-28 | 1A | F04 Gitignore Parser |
| [0.1.2](./0.1/v0.1.2.md) | 2025-12-28 | 1A | F03 File Scanner |
| [0.1.1](./0.1/v0.1.1.md) | 2025-12-28 | 1A | F02 Configuration System |
| [0.1.0](./0.1/v0.1.0.md) | 2025-12-28 | Pre-1A | Project bootstrap, documentation |

---

## Versioning Strategy

### Version Format

```
MAJOR.MINOR.PATCH

MAJOR: Breaking changes, major milestones
MINOR: New features, phase completions
PATCH: Bug fixes, documentation updates
```

### Phase to Version Mapping

| Phase | Version Range |
|-------|---------------|
| Pre-implementation | 0.1.x |
| Phase 1A: Foundation | 0.2.x - 0.3.x |
| Phase 1B: Core Search | 0.4.x - 0.6.x |
| Phase 2: MCP Integration | 0.7.x - 0.9.x |
| Phase 3: Polish | 0.10.x - 0.12.x |
| Release Candidate | 1.0.0-rc.x |
| Production | 1.0.0+ |

---

## How to Update

1. Add changes to `unreleased.md` as you work
2. At checkpoint/release:
   - Create version file: `v0.X/0.X.Y.md`
   - Update this file with new version section
   - Update `VERSION` file
   - Update `docs/context.md`
   - Reset `unreleased.md`

---

## Links

- [Unreleased Changes](./unreleased.md)
- [Version 0.10.0](./0.10/v0.10.0.md)
- [Version 0.9.0](./0.9/v0.9.0.md)
- [Version 0.8.2](./0.8/v0.8.2.md)
- [Version 0.3.2](./0.3/v0.3.2.md)
- [Version 0.3.1](./0.3/v0.3.1.md)
- [Version 0.3.0](./0.3/v0.3.0.md)
- [Version 0.2.6](./0.2/v0.2.6.md)
- [Version 0.2.5](./0.2/v0.2.5.md)
- [Version 0.2.4](./0.2/v0.2.4.md)
- [Version 0.2.3](./0.2/v0.2.3.md)
- [Version 0.2.2](./0.2/v0.2.2.md)
- [Version 0.2.1](./0.2/v0.2.1.md)
- [Version 0.2.0](./0.2/v0.2.0.md)
- [Version 0.1.69](./0.1/v0.1.69.md)
- [Version 0.1.50](./0.1/v0.1.50.md)
- [Version 0.1.40](./0.1/v0.1.40.md)
- [Version 0.1.35](./0.1/v0.1.35.md)
- [Version 0.1.34](./0.1/v0.1.34.md)
- [Version 0.1.33](./0.1/v0.1.33.md)
- [Version 0.1.32](./0.1/v0.1.32.md)
- [Version 0.1.31](./0.1/v0.1.31.md)
- [Version 0.1.30](./0.1/v0.1.30.md)
- [Version 0.1.29](./0.1/v0.1.29.md)
- [Version 0.1.28](./0.1/v0.1.28.md)
- [Version 0.1.27](./0.1/v0.1.27.md)
- [Version 0.1.26](./0.1/v0.1.26.md)
- [Version 0.1.25](./0.1/v0.1.25.md)
- [Version 0.1.24](./0.1/v0.1.24.md)
- [Version 0.1.23](./0.1/v0.1.23.md)
- [Version 0.1.22](./0.1/v0.1.22.md)
- [Version 0.1.21](./0.1/v0.1.21.md)
- [Version 0.1.20](./0.1/v0.1.20.md)
- [Version 0.1.19](./0.1/v0.1.19.md)
- [Version 0.1.18](./0.1/v0.1.18.md)
- [Version 0.1.17](./0.1/v0.1.17.md)
- [Version 0.1.16](./0.1/v0.1.16.md)
- [Version 0.1.15](./0.1/v0.1.15.md)
- [Version 0.1.14](./0.1/v0.1.14.md)
- [Version 0.1.13](./0.1/v0.1.13.md)
- [Version 0.1.12](./0.1/v0.1.12.md)
- [Version 0.1.11](./0.1/v0.1.11.md)
- [Version 0.1.10](./0.1/v0.1.10.md)
- [Version 0.1.9](./0.1/v0.1.9.md)
- [Version 0.1.8](./0.1/v0.1.8.md)
- [Version 0.1.7](./0.1/v0.1.7.md)
- [Version 0.1.6](./0.1/v0.1.6.md)
- [Version 0.1.5](./0.1/v0.1.5.md)
- [Version 0.1.4](./0.1/v0.1.4.md)
- [Version 0.1.3](./0.1/v0.1.3.md)
- [Version 0.1.2](./0.1/v0.1.2.md)
- [Version 0.1.1](./0.1/v0.1.1.md)
- [Version 0.1.0](./0.1/v0.1.0.md)

[Unreleased]: ./unreleased.md
[0.10.0]: ./0.10/v0.10.0.md
[0.9.0]: ./0.9/v0.9.0.md
[0.8.2]: ./0.8/v0.8.2.md
[0.3.2]: ./0.3/v0.3.2.md
[0.3.1]: ./0.3/v0.3.1.md
[0.3.0]: ./0.3/v0.3.0.md
[0.2.6]: ./0.2/v0.2.6.md
[0.2.5]: ./0.2/v0.2.5.md
[0.2.4]: ./0.2/v0.2.4.md
[0.2.3]: ./0.2/v0.2.3.md
[0.2.2]: ./0.2/v0.2.2.md
[0.2.1]: ./0.2/v0.2.1.md
[0.2.0]: ./0.2/v0.2.0.md
[0.1.69]: ./0.1/v0.1.69.md
[0.1.50]: ./0.1/v0.1.50.md
[0.1.40]: ./0.1/v0.1.40.md
[0.1.35]: ./0.1/v0.1.35.md
[0.1.34]: ./0.1/v0.1.34.md
[0.1.33]: ./0.1/v0.1.33.md
[0.1.32]: ./0.1/v0.1.32.md
[0.1.31]: ./0.1/v0.1.31.md
[0.1.30]: ./0.1/v0.1.30.md
[0.1.29]: ./0.1/v0.1.29.md
[0.1.28]: ./0.1/v0.1.28.md
[0.1.27]: ./0.1/v0.1.27.md
[0.1.26]: ./0.1/v0.1.26.md
[0.1.25]: ./0.1/v0.1.25.md
[0.1.24]: ./0.1/v0.1.24.md
[0.1.23]: ./0.1/v0.1.23.md
[0.1.22]: ./0.1/v0.1.22.md
[0.1.21]: ./0.1/v0.1.21.md
[0.1.20]: ./0.1/v0.1.20.md
[0.1.19]: ./0.1/v0.1.19.md
[0.1.18]: ./0.1/v0.1.18.md
[0.1.17]: ./0.1/v0.1.17.md
[0.1.16]: ./0.1/v0.1.16.md
[0.1.15]: ./0.1/v0.1.15.md
[0.1.14]: ./0.1/v0.1.14.md
[0.1.13]: ./0.1/v0.1.13.md
[0.1.12]: ./0.1/v0.1.12.md
[0.1.11]: ./0.1/v0.1.11.md
[0.1.10]: ./0.1/v0.1.10.md
[0.1.9]: ./0.1/v0.1.9.md
[0.1.8]: ./0.1/v0.1.8.md
[0.1.7]: ./0.1/v0.1.7.md
[0.1.6]: ./0.1/v0.1.6.md
[0.1.5]: ./0.1/v0.1.5.md
[0.1.4]: ./0.1/v0.1.4.md
[0.1.3]: ./0.1/v0.1.3.md
[0.1.2]: ./0.1/v0.1.2.md
[0.1.1]: ./0.1/v0.1.1.md
[0.1.0]: ./0.1/v0.1.0.md
