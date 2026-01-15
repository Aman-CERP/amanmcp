# ADR-013: CGO Environment Setup Strategy

**Status:** Implemented
**Date:** 2025-12-30
**Supersedes:** None
**Superseded by:** None

---

## Context

AmanMCP uses two CGO-dependent libraries:

1. **USearch** - High-performance vector storage (HNSW algorithm)
2. **tree-sitter** - Code parsing for semantic chunking

Both require C libraries and headers. USearch specifically needs `usearch.h` and `libusearch_c` from the `lib/` directory. Without proper CGO environment variables (`CGO_CFLAGS`, `CGO_LDFLAGS`), direct `go test` or `go build` commands fail:

```
fatal error: 'usearch.h' file not found
```

The project philosophy emphasizes "It Just Works" with zero configuration. We needed a solution that:
- Requires no additional tool installation
- Works across macOS and Linux
- Supports multiple developer workflows
- Maintains simplicity

---

## Decision

We will provide **multiple complementary approaches** for CGO environment setup, prioritizing zero-install solutions:

1. **Makefile (primary)** - All `make` commands export CGO variables automatically
2. **`./dev` wrapper script** - Zero-install wrapper for direct go commands
3. **`.envrc` file** - Sourceable manually OR auto-loads with direnv (optional tool)

Developers choose their preferred workflow; all paths lead to working builds.

---

## Rationale

### Alternatives Considered

| Option | Pros | Cons |
|--------|------|------|
| Makefile only | Simple, already exists | Can't use raw `go` commands |
| direnv required | Automatic, elegant | Requires tool installation |
| Shell profile modification | Truly automatic | Invasive, breaks "It Just Works" |
| Go workspace/module | Native Go solution | CGO flags can't be set this way |
| **Chosen: Multi-approach** | Flexibility, zero-install | Slight documentation overhead |

### Why Multi-Approach?

Different developers have different preferences:
- **Make users**: `make test` just works (most common)
- **Go purists**: `./dev go test ./...` feels natural
- **Power users**: `eval "$(./dev env)"` sets up shell permanently
- **direnv users**: Automatic environment loading (optional enhancement)

All approaches use the same underlying configuration, ensuring consistency.

---

## Consequences

### Positive

- Zero additional tools required for basic development
- Developers can use their preferred workflow
- Clear error messages when `lib/` directory is missing
- Works on macOS (DYLD_LIBRARY_PATH) and Linux (LD_LIBRARY_PATH)

### Negative

- Multiple ways to do the same thing (documentation needed)
- New contributors may be confused initially
- `./dev` adds a file to project root

### Neutral

- `.envrc` file committed to repo (standard practice for direnv projects)
- Makefile remains the "official" recommended approach

---

## Implementation Notes

### File Structure

```
amanmcp/
├── dev                 # Wrapper script (executable)
├── .envrc              # direnv config / sourceable
├── lib/
│   ├── usearch.h       # USearch C header
│   └── libusearch_c.*  # USearch C library
└── Makefile            # Exports CGO vars for all targets
```

### Environment Variables Set

```bash
export CGO_ENABLED=1
export CGO_CFLAGS="-I${PWD}/lib"
export CGO_LDFLAGS="-L${PWD}/lib -lusearch_c -Wl,-rpath,${PWD}/lib"
export DYLD_LIBRARY_PATH="${PWD}/lib:${DYLD_LIBRARY_PATH}"  # macOS
export LD_LIBRARY_PATH="${PWD}/lib:${LD_LIBRARY_PATH}"      # Linux
```

### Usage Examples

```bash
# Recommended (always works)
make test
make build

# Wrapper approach (no install needed)
./dev go test ./...
./dev go build ./cmd/amanmcp

# Set up current shell
eval "$(./dev env)"
go test ./...  # Now works directly

# With direnv (optional)
direnv allow   # One-time
go test ./...  # Auto-works in this directory
```

---

## Related

- [ADR-001](./ADR-001-vector-database-usearch.md) - USearch for Vector Storage (requires CGO)
- [ADR-003](./ADR-003-tree-sitter-chunking.md) - Tree-sitter for Code Chunking (requires CGO)

---

## Changelog

| Date | Change |
|------|--------|
| 2025-12-30 | Initial implementation |
