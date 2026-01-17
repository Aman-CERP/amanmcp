# Using MCP with AmanMCP

How to configure AmanMCP as an MCP server for Claude Code, Cursor, and other AI assistants.

**Reading time:** 5 minutes
**Audience:** Users setting up AmanMCP
**Prerequisites:** [Overview](overview.md), [Installation](../../getting-started/introduction.md)

---

## Quick Summary

- Create a config file (`.mcp.json` or similar)
- Point it to the `amanmcp serve` command
- Set the working directory to your project
- Restart your AI assistant

---

## Claude Code Configuration

### Step 1: Create Config File

Create `.mcp.json` in your project root:

```json
{
  "mcpServers": {
    "amanmcp": {
      "command": "amanmcp",
      "args": ["serve"],
      "cwd": "/path/to/your/project"
    }
  }
}
```

**Important:** Replace `/path/to/your/project` with your actual project path.

### Step 2: Verify Installation

Make sure `amanmcp` is in your PATH:

```bash
which amanmcp
# Should output: /path/to/amanmcp

amanmcp --version
# Should output version number
```

If not found, run:

```bash
make install-local  # Installs to ~/.local/bin
```

### Step 3: Restart Claude Code

Close and reopen Claude Code. It will automatically discover and start AmanMCP.

### Verify Connection

Ask Claude: "What MCP tools are available?"

Claude should mention `amanmcp` with tools like `search`, `lookup`, `similar`.

---

## Cursor Configuration

### Step 1: Create Config File

Create `.cursor/mcp.json` in your project root:

```json
{
  "mcpServers": {
    "amanmcp": {
      "command": "amanmcp",
      "args": ["serve"],
      "cwd": "/path/to/your/project"
    }
  }
}
```

### Step 2: Restart Cursor

Use `Cmd+Shift+P` (Mac) or `Ctrl+Shift+P` (Windows/Linux), then "Developer: Reload Window".

---

## Configuration Options

### Full Configuration Example

```json
{
  "mcpServers": {
    "amanmcp": {
      "command": "amanmcp",
      "args": ["serve"],
      "cwd": "/path/to/your/project",
      "env": {
        "AMANMCP_LOG_LEVEL": "info",
        "AMANMCP_EMBEDDING_PROVIDER": "ollama"
      }
    }
  }
}
```

### Environment Variables

| Variable | Purpose | Default |
|----------|---------|---------|
| `AMANMCP_LOG_LEVEL` | Logging verbosity | `warn` |
| `AMANMCP_EMBEDDING_PROVIDER` | Embedding model | `ollama` |
| `AMANMCP_CACHE_DIR` | Where to store index | `.amanmcp/` |

### Command Arguments

| Argument | Purpose |
|----------|---------|
| `serve` | Start MCP server (required) |
| `--config path` | Use specific config file |
| `--verbose` | Enable verbose logging |

---

## The CWD Requirement

**Important:** The `cwd` (current working directory) parameter is required.

Why? Claude Code doesn't automatically set the working directory when spawning MCP servers. Without `cwd`, AmanMCP won't know which codebase to search.

```json
{
  "mcpServers": {
    "amanmcp": {
      "command": "amanmcp",
      "args": ["serve"],
      "cwd": "/Users/you/projects/myapp"  // ← Required!
    }
  }
}
```

---

## Multiple Projects

You can configure different AmanMCP instances for different projects:

### Per-Project Config

Each project gets its own `.mcp.json` with its own `cwd`.

### Global Config with Multiple Servers

```json
{
  "mcpServers": {
    "amanmcp-frontend": {
      "command": "amanmcp",
      "args": ["serve"],
      "cwd": "/path/to/frontend"
    },
    "amanmcp-backend": {
      "command": "amanmcp",
      "args": ["serve"],
      "cwd": "/path/to/backend"
    }
  }
}
```

---

## Available Tools

Once configured, these tools become available to your AI assistant:

| Tool | Purpose | Example Query |
|------|---------|---------------|
| `search` | Hybrid search (BM25 + semantic) | "authentication middleware" |
| `lookup` | Get specific code by path/symbol | "handler.go:AuthMiddleware" |
| `similar` | Find code similar to a reference | Similar to a given function |
| `index-status` | Check indexing status | "Is my code indexed?" |

---

## Troubleshooting

### "MCP server not found"

1. Check that `amanmcp` is in your PATH:

   ```bash
   which amanmcp
   ```

2. Try using the full path in config:

   ```json
   {
     "command": "/Users/you/.local/bin/amanmcp"
   }
   ```

### "No results returned"

1. Check if your project is indexed:

   ```bash
   cd /path/to/project
   amanmcp status
   ```

2. If not indexed, wait for auto-indexing or run manually:

   ```bash
   amanmcp index
   ```

### "Connection refused"

1. Check if another AmanMCP instance is running
2. Restart your AI assistant
3. Check logs:

   ```bash
   # Run manually to see errors
   amanmcp serve --verbose
   ```

### "Wrong project being searched"

Verify the `cwd` in your config matches the project you're working on.

---

## Checking Index Status

You can check what AmanMCP has indexed:

```bash
cd /path/to/project
amanmcp status
```

Output shows:
- Number of indexed files
- Number of code chunks
- Last index time
- Embedding model used

---

## Testing Your Setup

### Quick Test

Ask your AI assistant:

> "Search for 'main' in the codebase"

If configured correctly, you'll see search results from your actual code.

### Using MCP Inspector

For debugging, use the official MCP inspector:

```bash
npm install -g @modelcontextprotocol/inspector
mcp-inspector amanmcp serve
```

This opens a web UI where you can:
- See available tools
- Test tool calls manually
- View JSON-RPC messages

---

## Performance Tips

### Initial Indexing

First index can take a while for large codebases. Let it complete before searching.

### Auto-Reindexing

AmanMCP watches for file changes and reindexes automatically. You don't need to restart.

### Memory Usage

For very large codebases (> 500K lines), ensure you have at least 1GB free RAM.

---

## Next Steps

| Want to... | Read |
|------------|------|
| Understand the protocol | [Building MCP Servers](building-mcp.md) |
| Try searching | [Your First Search](../../tutorials/your-first-search.md) |
| Configure indexing | [Auto-Reindexing](../../guides/auto-reindexing.md) |

---

*Once configured, MCP just works. Ask questions, get answers from your code.*
