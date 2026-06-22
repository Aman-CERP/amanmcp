# internal/ — Scoped Discipline

**Territory:** AmanMCP Go packages (search, MCP, index, store, embed, chunk, config).

## Hard Rules

1. **TDD mandatory** — failing test before implementation; `make test-race` on touched packages.
2. **CGO / tree-sitter** — `defer parser.Close()`, `defer tree.Close()` on every parse path.
3. **Error wrapping** — `fmt.Errorf("failed to <op>: %w", err)`; no silent fallbacks.
4. **Config SSOT** — tunable weights and exclusions live in `.amanmcp.yaml`, not literals.
5. **MCP-first search discipline** — use amanmcp MCP tools for implementations; Grep for locations only.

## Key Packages

| Package | Role |
|---------|------|
| `internal/search` | Hybrid BM25 + vector + RRF |
| `internal/mcp` | MCP protocol, tools, resources |
| `internal/index` | Scanner, chunker, watcher |
| `internal/store` | HNSW vectors, BM25, SQLite |
| `internal/embed` | Ollama / MLX / static embedders |

## Gates Before Done

```bash
go test -race ./internal/<pkg>/...
make ci-check-quick    # iteration
make ci-check          # before PR
```