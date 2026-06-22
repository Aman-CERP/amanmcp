# cmd/ — Scoped Discipline

**Territory:** Cobra CLI entry points (`cmd/amanmcp/`).

## Hard Rules

1. **User-facing stability** — CLI flags and subcommands are contracts; deprecate before removing.
2. **Zero-config default** — `amanmcp` must work with no `.amanmcp.yaml` for basic search/index.
3. **Install paths** — `make install-local` targets `~/.local/bin`; verify with `make verify-install`.
4. **Version surface** — `pkg/version` is the single version source; never hardcode in cmd.

## Gates

```bash
make build
make verify-install    # after install-path changes
```