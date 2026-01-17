# MCP (Model Context Protocol) Overview

What MCP is, why it matters, and how it enables AI-tool integration.

**Reading time:** 3 minutes
**Audience:** Everyone
**Prerequisites:** None

---

## Quick Summary

- MCP is a **protocol** that lets AI assistants use external tools
- It's like USB for AI - a universal interface
- AmanMCP is an MCP **server** that provides code search
- Claude Code, Cursor, etc. are MCP **clients** that use the search

---

## The Problem MCP Solves

### Before MCP: Integration Chaos

Every AI app needed custom integrations with every tool:

```
Claude ──────── Custom API ──────── Codebase
       ╲                           ╱
        ╲─── Another API ─────────╱── GitHub
         ╲                       ╱
          ╲── Yet Another API ──╱──── Docs

Cursor ────── Different API ────── Codebase
       ╲                          ╱
        ╲─── Its Own API ────────╱── GitHub

Result: N clients × M tools = N×M integrations
```

If you have 5 AI tools and 10 data sources, you need 50 different integrations.

### After MCP: Universal Language

All tools speak the same protocol:

```
┌─────────────────────────────────────────────┐
│              MCP CLIENTS                     │
│  Claude Code  │  Cursor  │  Any Future AI   │
└───────┬───────┴────┬─────┴────────┬─────────┘
        │            │              │
        ▼            ▼              ▼
     ┌──────────────────────────────────┐
     │         MCP PROTOCOL             │
     │    (Universal Interface)         │
     └──────────────────────────────────┘
        │            │              │
        ▼            ▼              ▼
┌───────┴───────┬────┴─────┬────────┴─────────┐
│              MCP SERVERS                     │
│   AmanMCP    │  GitHub  │  Any Other Tool   │
│  (code search)│ (repos)  │                   │
└─────────────────────────────────────────────┘

Result: N + M integrations (linear!)
```

Now 5 AI tools and 10 data sources need only 15 integrations.

---

## The USB Analogy

Think of MCP like USB:

| USB | MCP |
|-----|-----|
| Computers (any brand) | AI assistants (Claude, Cursor, etc.) |
| USB devices (any type) | MCP servers (search, databases, APIs) |
| USB cable/protocol | MCP protocol (JSON-RPC) |
| Plug in and it works | Configure once and it works |

Before USB, every device needed its own port type. Before MCP, every AI tool needed custom integrations.

---

## How AmanMCP Fits In

AmanMCP is an **MCP server** that exposes code search:

```
┌───────────────────────────────────────────────┐
│  You (asking Claude)                          │
│  "Where is authentication handled?"           │
└───────────────────┬───────────────────────────┘
                    │
                    ▼
┌───────────────────────────────────────────────┐
│  Claude Code (MCP Client)                     │
│  "I should search the codebase"               │
│  → calls amanmcp search tool                  │
└───────────────────┬───────────────────────────┘
                    │ MCP Protocol
                    ▼
┌───────────────────────────────────────────────┐
│  AmanMCP (MCP Server)                         │
│  → runs hybrid search (BM25 + vector)         │
│  → returns: auth.go, session.go, middleware.go│
└───────────────────┬───────────────────────────┘
                    │
                    ▼
┌───────────────────────────────────────────────┐
│  Claude's Answer                              │
│  "Authentication is in auth.go at line 42..." │
└───────────────────────────────────────────────┘
```

---

## The Three Things MCP Exposes

MCP servers can provide three types of capabilities:

| Capability | What It Is | AmanMCP Example |
|------------|------------|-----------------|
| **Tools** | Actions the AI can take | `search`, `lookup`, `similar` |
| **Resources** | Data the AI can read | Indexed files as file:// URIs |
| **Prompts** | Reusable templates | (not currently used) |

### Tools

Functions the AI can call:

```
Tool: "search"
Input: { query: "authentication", limit: 10 }
Output: Top 10 code chunks matching "authentication"
```

### Resources

Data the AI can access:

```
Resource: file:///src/auth/handler.go
Type: Go source file
Content: The actual file contents
```

---

## Benefits of MCP

| Benefit | Why It Matters |
|---------|----------------|
| **Privacy** | AmanMCP runs locally - code never leaves your machine |
| **Speed** | No network latency to external APIs |
| **Universal** | Works with any MCP-compatible AI assistant |
| **Extensible** | Add more MCP servers for more capabilities |

---

## MCP vs Alternatives

| Approach | Pros | Cons |
|----------|------|------|
| **MCP (AmanMCP)** | Universal, local, fast | Requires setup |
| **Cloud RAG** | Easy setup | Code leaves machine, latency |
| **LLM context stuffing** | No tool needed | Token limits, expensive |
| **Manual copy-paste** | Always works | Slow, tedious |

---

## Next Steps

| Want to... | Read |
|------------|------|
| Configure AmanMCP for Claude/Cursor | [Using MCP](using-mcp.md) |
| Understand the protocol details | [Building MCP Servers](building-mcp.md) |
| Try code search | [Your First Search](../../tutorials/your-first-search.md) |

---

*MCP is the bridge between AI and tools. AmanMCP makes your codebase searchable across that bridge.*
