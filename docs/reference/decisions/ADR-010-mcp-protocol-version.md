# ADR-010: MCP Protocol Version 2025-11-25

**Status:** Implemented
**Date:** 2025-12-28
**Supersedes:** None
**Superseded by:** None

---

## Context

AmanMCP implements the Model Context Protocol (MCP) to expose search functionality to AI coding assistants. MCP is a rapidly evolving protocol, and we need to:

1. Choose a stable protocol version
2. Select an SDK/implementation approach
3. Ensure compatibility with major MCP clients (Claude Code, Cursor, etc.)

The protocol version affects which features are available and how clients interact with our server.

---

## Decision

We will implement **MCP Specification version 2025-11-25** using the **Official Go SDK** from Google and Anthropic.

SDK: `github.com/modelcontextprotocol/go-sdk`

---

## Rationale

### Alternatives Considered

| Option | Pros | Cons |
|--------|------|------|
| Earlier MCP versions | More stable, widely tested | Missing features (async, auth) |
| **Chosen: 2025-11-25** | Latest stable, async tasks, CIMD auth, anniversary release | Newer, less battle-tested |
| Custom implementation | Full control | High maintenance, compatibility risk |
| Third-party SDK | May have extra features | Uncertain maintenance, not official |

### Why 2025-11-25 with Official SDK

1. **Anniversary Release**: Major milestone release with significant additions
2. **Async Tasks**: Supports long-running operations (important for indexing)
3. **CIMD Authorization**: Proper auth model for production use
4. **Official SDK**: Maintained by Anthropic/Google, long-term support
5. **Go Native**: First-class Go support, not a wrapper

---

## Consequences

### Positive

- Access to latest protocol features
- Official SDK ensures compatibility with clients
- Go-native implementation (no FFI or wrappers)
- Active maintenance from protocol authors

### Negative

- Newer version may have undiscovered issues
- May need updates as protocol evolves
- SDK is relatively new (less community examples)

### Neutral

- Ties us to official SDK patterns
- Version pinned to specific release

---

## Implementation Notes

```go
// go.mod
require github.com/modelcontextprotocol/go-sdk v0.x.x

// internal/mcp/server.go
import "github.com/modelcontextprotocol/go-sdk/mcp"

type Server struct {
    mcpServer *mcp.Server
    engine    *search.Engine
}

func NewServer(engine *search.Engine) *Server {
    return &Server{
        mcpServer: mcp.NewServer("amanmcp", version.Version),
        engine:    engine,
    }
}
```

Features implemented:
- Tools: `search`, `index`, `status`
- Resources: `file://` for indexed content
- Prompts: Context-aware search prompts

---

## Related

- [F16](../specs/features/F16-mcp-server.md) - MCP server implementation
- [F17](../specs/features/F17-mcp-tools.md) - MCP tools (search, index)
- [F18](../specs/features/F18-mcp-resources.md) - MCP resources
- [MCP Protocol Guide](../guides/mcp-protocol.md) - Protocol concepts
- [MCP Specification](https://modelcontextprotocol.io/specification/2025-11-25) - Official spec
- [MCP Anniversary Blog](http://blog.modelcontextprotocol.io/posts/2025-11-25-first-mcp-anniversary/) - Release notes

---

## Changelog

| Date | Change |
|------|--------|
| 2025-12-28 | Initial implementation with official Go SDK |
