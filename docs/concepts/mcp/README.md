# MCP (Model Context Protocol)

MCP is the protocol that connects AI assistants to AmanMCP's code search capabilities.

---

## Choose Your Path

| I want to... | Read | Time |
|--------------|------|------|
| Understand what MCP is | [Overview](overview.md) | 3 min |
| Configure AmanMCP for Claude/Cursor | [Using MCP](using-mcp.md) | 5 min |
| Learn the protocol and Go implementation | [Building MCP](building-mcp.md) | 12 min |

---

## Quick Summary

- **MCP** is a universal protocol for AI-tool integration
- **AmanMCP** is an MCP server that provides code search
- **Clients** (Claude Code, Cursor) connect and use our tools
- **Transport** is stdio (simple process communication)

**Tools we expose:** `search`, `lookup`, `similar`, `index-status`

---

## Related Documentation

- [Your First Search](../../tutorials/your-first-search.md) - Try searching
- [Debugging MCP Protocol](../../articles/debugging-mcp-protocol.md) - Troubleshooting
- [Search Fundamentals](../../learning/search-fundamentals.md) - How search works
