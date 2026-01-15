# AmanMCP

**Local RAG MCP Server for Developers**

> Zero configuration. Privacy-first. It just works.

[![Version](https://img.shields.io/badge/version-0.10.2-green)](https://github.com/Aman-CERP/amanmcp/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.25.5+-00ADD8?logo=go)](https://golang.org/)
[![MCP](https://img.shields.io/badge/MCP-2025--11--25-blue)](https://modelcontextprotocol.io/)

---

> **USE AT YOUR OWN RISK**
>
> AmanMCP is experimental software in active development. By using this software, you acknowledge this is **alpha/beta quality** software, accept **full responsibility** for any issues, and understand the developers are **not liable** for data loss, system issues, or other problems. You are encouraged to **review the source code** before use and **backup your data** before running on important projects. This software is provided "AS IS" without warranty of any kind.

---

## What is AmanMCP?

AmanMCP is a **local-first** RAG (Retrieval-Augmented Generation) server that integrates with AI coding assistants like [Claude Code](https://claude.ai/code), [Cursor](https://cursor.sh/), and other MCP-compatible tools.

**It gives your AI assistant context about your codebase** — helping it understand your code, find relevant functions, and retrieve documentation instantly.

```text
┌─────────────────┐       ┌─────────────────┐       ┌─────────────────┐
│  Your Codebase  │       │    AmanMCP      │       │  AI Assistant   │
│  ────────────── │ index │  ────────────── │ query │  ────────────── │
│  cmd/           │──────►│  BM25 Index     │◄──────│  Claude Code    │
│  internal/      │       │  Vector Store   │──────►│  Cursor         │
│  docs/          │       │  100% Local     │results│  etc.           │
└─────────────────┘       └─────────────────┘       └─────────────────┘
```

---

## Features

| Feature | Description |
|---------|-------------|
| **Zero Config** | Auto-detects project structure, no setup required |
| **Hybrid Search** | Combines keyword (BM25) + semantic search for best results |
| **Code-Aware** | Uses tree-sitter for AST-based chunking (preserves functions/classes) |
| **Privacy-First** | 100% local, no cloud, no telemetry, your code stays on your machine |
| **Fast** | < 100ms search latency, incremental indexing |
| **Multi-Language** | Go, TypeScript, JavaScript, Python, HTML, CSS |

---

## Recent Improvements (v0.3.x)

| Category | Improvement |
|----------|-------------|
| **Ollama Default** | Ollama is now default on all platforms (lower RAM usage) |
| **Backend Switching** | `--backend` flag for easy switching (ollama, mlx, static) |
| **Index Info** | `amanmcp index info` shows index configuration and stats |
| **Observability** | Stage timing, throughput, backend info in indexing output |
| **MLX Install** | `make install-mlx` one-command setup with model download |

See [Changelog](docs/changelog/CHANGELOG.md) for details.

---

## Technology Stack

| Component | Technology |
|-----------|------------|
| **Language** | Go 1.25.5+ |
| **Protocol** | MCP 2025-11-25 ([Official Go SDK](https://github.com/modelcontextprotocol/go-sdk)) |
| **Code Parsing** | [tree-sitter](https://tree-sitter.github.io/) |
| **Keyword Search** | [Bleve](https://blevesearch.com/) BM25 |
| **Vector Search** | [coder/hnsw](https://github.com/coder/hnsw) (pure Go) |
| **Embeddings (Default)** | [Ollama](https://ollama.com/) on all platforms (lower RAM) |
| **Embeddings (Opt-in)** | MLX on Apple Silicon (~1.7x faster, higher RAM) |

---

## Quick Start

> **Detailed instructions:** See the [Getting Started Guide](docs/guides/first-time-user-guide.md)

### 3 Commands to Get Started

```bash
# 1. Install AmanMCP
brew tap Aman-CERP/tap && brew install amanmcp

# 2. Initialize your project (auto-starts Ollama, pulls model if needed)
cd /path/to/your/project
amanmcp init

# 3. Restart Claude Code to activate
```

**Done!** Ask Claude: "Search my codebase for authentication"

### What `amanmcp init` Does Automatically

1. **Checks Ollama** - Detects if installed and running
2. **Starts Ollama** - Auto-starts if installed but not running
3. **Pulls Model** - Downloads embedding model if needed (~400MB)
4. **Indexes Project** - Scans and indexes your codebase
5. **Configures MCP** - Sets up Claude Code integration

### Alternative Install Methods

```bash
# Install script (non-Homebrew)
curl -sSL https://raw.githubusercontent.com/Aman-CERP/amanmcp/main/scripts/install.sh | sh

# Offline mode (no Ollama, BM25-only search)
amanmcp init --offline
```

### Optional: MLX (Apple Silicon, ~1.7x Faster)

```bash
# One-command install + start
make install-mlx && make start-mlx

# Use MLX explicitly
AMANMCP_EMBEDDER=mlx amanmcp index .
```

---

## Commands

### Getting Started

| Command | Description |
|---------|-------------|
| `amanmcp` | Smart default: auto-index + start server |
| `amanmcp init` | Initialize project (MCP config + indexing) |
| `amanmcp init --force` | Reinitialize, overwrite existing config |
| `amanmcp init --config-only` | Fix config without reindexing |
| `amanmcp init --offline` | Use static embeddings (no Ollama) |
| `amanmcp setup` | Check/configure embedding backend |
| `amanmcp setup --check` | Check status only, don't start/pull |
| `amanmcp setup --auto` | Non-interactive mode (for scripts) |
| `amanmcp setup --offline` | Configure for offline mode |

### Indexing

| Command | Description |
|---------|-------------|
| `amanmcp index` | Index current directory |
| `amanmcp index [path]` | Index specific directory |
| `amanmcp index --backend=ollama` | Force Ollama backend (cross-platform) |
| `amanmcp index --force` | Clear existing index and rebuild |
| `amanmcp index --resume` | Resume interrupted indexing |
| `amanmcp index --no-tui` | Plain text output (no TUI) |
| `amanmcp index info` | Show index configuration and stats |
| `amanmcp index info --json` | Index info as JSON |
| `amanmcp compact` | Optimize vector index |

### Search

| Command | Description |
|---------|-------------|
| `amanmcp search "query"` | Hybrid search across codebase |
| `amanmcp search -t code "query"` | Search code files only |
| `amanmcp search -t docs "query"` | Search documentation only |
| `amanmcp search -l go "query"` | Filter by language |
| `amanmcp search -n 20 "query"` | Limit results (default: 10) |
| `amanmcp search -f json "query"` | JSON output format |

### Session Management

| Command | Description |
|---------|-------------|
| `amanmcp sessions` | List all sessions |
| `amanmcp sessions delete NAME` | Delete a session |
| `amanmcp sessions prune` | Remove sessions older than 30 days |
| `amanmcp resume NAME` | Resume a saved session |
| `amanmcp switch NAME` | Switch to different session |

### Server & Daemon

| Command | Description |
|---------|-------------|
| `amanmcp serve` | Start MCP server (stdio) |
| `amanmcp serve --transport sse --port 8765` | SSE transport on port |
| `amanmcp daemon start` | Start background daemon |
| `amanmcp daemon stop` | Stop daemon |
| `amanmcp daemon status` | Check daemon status |

### Diagnostics

| Command | Description |
|---------|-------------|
| `amanmcp doctor` | Check system requirements |
| `amanmcp status` | Show index health |
| `amanmcp stats` | Show statistics |
| `amanmcp stats queries --days 7` | Query analytics |
| `amanmcp version` | Show version |
| `amanmcp version --json` | Version as JSON |

### Debugging

| Command | Description |
|---------|-------------|
| `amanmcp --debug <cmd>` | Enable file logging |
| `amanmcp-logs` | Show last 50 log lines |
| `amanmcp-logs -f` | Follow logs real-time |
| `amanmcp-logs --level error` | Filter by level |
| `amanmcp-logs --source mlx` | View MLX server logs |
| `amanmcp-logs --source all` | View all logs merged by timestamp |

Run `amanmcp --help` for complete command reference.

---

## MCP Tools (What Claude Can Do)

When connected via MCP, Claude Code has access to:

| Tool | Purpose |
|------|---------|
| `search` | Hybrid BM25 + semantic search across codebase |
| `search_code` | Find functions, classes, types by name or concept |
| `search_docs` | Search documentation and markdown files |
| `index_status` | Check index health, embedder, and dimensions |

**Example prompts:**
- "Search my codebase for authentication"
- "Find the function that handles database connections"
- "What does the config system do?"
- "Check the index status"

---

## Configuration (Optional)

AmanMCP works without configuration. Settings can be customized at two levels.

> **Full Reference:** [Configuration Reference](docs/guides/configuration-reference.md) - Complete list of all 35+ configuration options.

### User Configuration (Machine-Specific)

For machine-specific settings like Ollama host and thermal management:

```bash
# Create user config from template
amanmcp config init

# Upgrade existing config with new defaults (preserves your settings)
amanmcp config init --force

# View config location
amanmcp config path
# ~/.config/amanmcp/config.yaml
```

> **Note:** When upgrading AmanMCP, your config is automatically upgraded with new defaults. A backup is created at `config.yaml.bak.<timestamp>`.

Edit `~/.config/amanmcp/config.yaml`:

```yaml
version: 1
embeddings:
  provider: ollama
  model: qwen3-embedding:0.6b
  ollama_host: http://localhost:11434

  # Thermal management (Apple Silicon)
  # inter_batch_delay: 200ms
  # timeout_progression: 1.5
```

### Project Configuration (Per-Project)

For project-specific settings, create `.amanmcp.yaml` in your project root:

```yaml
version: 1
paths:
  include: [src/, docs/]
  exclude: [legacy/**]
search:
  bm25_weight: 0.35
  semantic_weight: 0.65
```

### Configuration Precedence

Settings are applied in order (later overrides earlier):

1. **Defaults** (hardcoded in binary)
2. **User config** (`~/.config/amanmcp/config.yaml`)
3. **Project config** (`.amanmcp.yaml`)
4. **Environment variables** (`AMANMCP_*`)

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| **Backend Selection** | | |
| `AMANMCP_EMBEDDER` | `auto` | Provider: `mlx`, `ollama`, or `static` |
| **Ollama Settings** | | |
| `AMANMCP_OLLAMA_HOST` | `http://localhost:11434` | Ollama API endpoint |
| `AMANMCP_OLLAMA_MODEL` | `qwen3-embedding:0.6b` | Ollama embedding model |
| `AMANMCP_OLLAMA_TIMEOUT` | `5m` | Request timeout (e.g., `2m`, `120s`) |
| **MLX Settings** | | |
| `AMANMCP_MLX_ENDPOINT` | `http://localhost:9659` | MLX server endpoint |
| `AMANMCP_MLX_MODEL` | `large` | MLX model: `small`, `medium`, `large` |
| **Search Tuning** | | |
| `AMANMCP_BM25_WEIGHT` | `0.35` | BM25 keyword search weight (0.0-1.0) |
| `AMANMCP_SEMANTIC_WEIGHT` | `0.65` | Semantic search weight (0.0-1.0) |
| **Caching** | | |
| `AMANMCP_EMBED_CACHE` | `true` | Enable query embedding cache |
| **Logging** | | |
| `AMANMCP_LOG_LEVEL` | `info` | Log level: debug/info/warn/error |

For thermal management settings (Apple Silicon), see [Thermal Management Guide](docs/guides/thermal-management.md).

---

## Troubleshooting

| Issue | Solution |
|-------|----------|
| `zsh: killed` | Run `xattr -cr ~/.local/bin/amanmcp` |
| `command not found` | Add `export PATH="$HOME/.local/bin:$PATH"` to `~/.zshrc` |
| Slow indexing | Use `amanmcp init --offline` or close other apps |
| Ollama issues | Run `ollama serve` in separate terminal |

For detailed help: [Getting Started Guide](docs/guides/first-time-user-guide.md) or run `amanmcp doctor`

---

## MLX Server (Apple Silicon - Optional)

The bundled MLX embedding server provides **~1.7x faster** embedding throughput than Ollama on Apple Silicon.

### Quick Setup

```bash
make install-mlx   # One-command: venv + deps + model download
make start-mlx     # Start server on port 9659
```

### Quick Reference

| Command | Description |
|---------|-------------|
| `make install-mlx` | One-command setup (venv + deps + model) |
| `make start-mlx` | Start MLX server |
| `amanmcp index --backend=mlx .` | Index with MLX |
| `curl http://localhost:9659/health` | Check server health |
| `amanmcp-logs --source mlx` | View MLX logs |

### Features

- **Opt-in:** Use `--backend=mlx` or `AMANMCP_EMBEDDER=mlx` to enable
- **Models:** small (0.6B), medium (4B), large (8B) - defaults to small
- **Storage:** Models cached in `~/.amanmcp/models/mlx/`
- **Logging:** JSON logs to `~/.amanmcp/logs/mlx-server.log`

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | 9659 | Server port |
| `MODEL_NAME` | `large` | Model: `small`, `medium`, `large` |
| `LOG_LEVEL` | INFO | Logging level |

For complete documentation: [MLX Server README](mlx-server/README.md)

---

## Backend Management

AmanMCP supports multiple embedding backends. Use the Makefile targets for easy management.

### Quick Reference

| Command | Description |
|---------|-------------|
| `make install-ollama` | Install Ollama + pull default model |
| `make start-ollama` | Start Ollama server |
| `make stop-ollama` | Stop Ollama server |
| `make install-mlx` | Install MLX server (Apple Silicon) |
| `make start-mlx` | Start MLX server |
| `make switch-backend-mlx` | Switch to MLX backend |
| `make switch-backend-ollama` | Switch to Ollama backend |
| `make verify-install` | Verify installation is working |
| `./scripts/switch-backend.sh status` | Show backend status |

### Backend Comparison

| Backend | Platform | Speed | Memory | Best For |
|---------|----------|-------|--------|----------|
| **Ollama** | Cross-platform | Good | ~3-6GB | Default, easy setup |
| **MLX** | Apple Silicon | ~1.7x faster | ~3GB | M1/M2/M3/M4 Macs |
| **Static** | Any | Instant | <100MB | Offline, no server |

### Switching Backends

```bash
# Switch to MLX (Apple Silicon)
make switch-backend-mlx
amanmcp index --force .  # Reindex with new backend

# Switch to Ollama
make switch-backend-ollama
amanmcp index --force .  # Reindex with new backend
```

**Note:** When switching backends with different dimensions, you must reindex (`--force`).

---

## Development

### Build & Install

| Command | Description |
|---------|-------------|
| `make build` | Compile binary to bin/ |
| `make build-all` | Build all binaries |
| `make install-local` | Install to ~/.local/bin (recommended) |
| `make install` | Install to /usr/local/bin |
| `make install-mlx` | Set up MLX server (Apple Silicon) |
| `make start-mlx` | Start MLX embedding server |
| `make uninstall-local` | Remove from ~/.local/bin |
| `make clean` | Remove build artifacts |

### Testing

| Command | Description |
|---------|-------------|
| `make test` | Run unit tests |
| `make test-race` | Tests with race detector |
| `make test-cover` | Generate coverage report |
| `make test-cover-html` | HTML coverage report |

### Code Quality

| Command | Description |
|---------|-------------|
| `make lint` | Run golangci-lint |
| `make lint-fix` | Auto-fix lint issues |
| `make lint-fast` | Lint changed files only |

### CI Parity

| Command | Description |
|---------|-------------|
| `make ci-check` | Full CI validation (run before commits) |
| `make ci-check-quick` | Fast validation during development |

### Verification

| Command | Description |
|---------|-------------|
| `make verify-all` | Run all verification checks |
| `make check-versions` | Check version consistency |
| `make verify-docs` | Check documentation drift |

### Benchmarks

| Command | Description |
|---------|-------------|
| `make bench` | Run all benchmarks |
| `make bench-search` | Search engine benchmarks |
| `make bench-compare` | Compare against baseline |
| `./scripts/benchmark-backends.sh` | Compare MLX vs Ollama backends |

Run `make help` for complete target list.

---

## Development Status

**Current Version:** 0.2.4 | **Overall Progress:** 100% (39 features validated)

| Phase | Status |
|-------|--------|
| Phase 1A: Foundation (F01-F05) | Complete |
| Phase 1B: Core Search (F06-F12) | Complete |
| Phase 2: MCP Integration (F13-F18) | Complete |
| Phase 3: Polish & Release (F19-F29) | Complete |
| Sprint 4: Backend Decision | 70% (7/10 items) |

See [Feature Catalog](docs/reference/specs/features/index.md) for details.

---

## Privacy & Security

- **100% Local** - No internet required after installation
- **No Telemetry** - We don't collect any data
- **No Cloud** - Your code never leaves your machine
- **.gitignore Respected** - Sensitive files are excluded

---

## Documentation

| Document | Description |
|----------|-------------|
| [Getting Started](docs/guides/first-time-user-guide.md) | Step-by-step installation and setup |
| [Architecture](docs/reference/architecture/architecture.md) | How AmanMCP works internally |
| [Feature Catalog](docs/reference/specs/features/index.md) | Complete feature reference |
| [Hybrid Search](docs/guides/hybrid-search.md) | BM25 + semantic search explained |
| [Contributing](CONTRIBUTING.md) | Development guidelines |

---

## Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md).

**Priority areas:** Additional language support, Windows support, performance improvements.

**For maintainers:** See [Homebrew Setup Guide](docs/guides/homebrew-setup-guide.md).

---

## License

MIT License - see [LICENSE](LICENSE) for details.

---

## Acknowledgments

- [Model Context Protocol](https://modelcontextprotocol.io/) by Anthropic
- [Official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
- [coder/hnsw](https://github.com/coder/hnsw) for pure Go HNSW
- [Ollama](https://ollama.com/) for local embedding model serving
- [tree-sitter](https://tree-sitter.github.io/) for code parsing
- [Qwen](https://huggingface.co/Alibaba-NLP) for Qwen3-embedding model

---

**Made with love by the AmanERP Team**

*"It just works."*
