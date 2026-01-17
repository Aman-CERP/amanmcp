# Building MCP Servers

Protocol details, Go implementation, and patterns for MCP server development.

**Reading time:** 12 minutes
**Audience:** Developers extending or understanding AmanMCP internals
**Prerequisites:** [Overview](overview.md), Go basics

---

## Quick Summary

- MCP uses JSON-RPC 2.0 over stdio (or other transports)
- Servers expose tools, resources, and prompts
- AmanMCP uses the official Go SDK from Anthropic
- Tool handlers receive context and return results or errors

---

## Protocol Fundamentals

### JSON-RPC 2.0

MCP uses JSON-RPC for all communication:

**Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "search",
    "arguments": {
      "query": "authentication",
      "limit": 10
    }
  }
}
```

**Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "Found 5 results for 'authentication'..."
      }
    ]
  }
}
```

### Transport: stdio

AmanMCP uses stdio (standard input/output) for simplicity:

```
Client (Claude Code)          Server (AmanMCP)
        │                            │
        │──── stdin (JSON-RPC) ─────>│
        │                            │
        │<──── stdout (JSON-RPC) ────│
        │                            │
```

No network ports, no HTTP servers - just process communication.

---

## Message Types

### Lifecycle Messages

| Method | Direction | Purpose |
|--------|-----------|---------|
| `initialize` | Client → Server | Start session, exchange capabilities |
| `initialized` | Client → Server | Confirm initialization complete |
| `shutdown` | Client → Server | Request graceful shutdown |

### Tool Messages

| Method | Direction | Purpose |
|--------|-----------|---------|
| `tools/list` | Client → Server | Get available tools |
| `tools/call` | Client → Server | Execute a tool |

### Resource Messages

| Method | Direction | Purpose |
|--------|-----------|---------|
| `resources/list` | Client → Server | Get available resources |
| `resources/read` | Client → Server | Read a resource |
| `resources/subscribe` | Client → Server | Watch for changes |

---

## Complete Lifecycle

```
┌─────────────────────────────────────────────────────────┐
│                    INITIALIZATION                        │
├─────────────────────────────────────────────────────────┤
│  Client                              Server              │
│    │                                   │                 │
│    │── initialize (version, caps) ────>│                 │
│    │                                   │                 │
│    │<── initialize response ───────────│                 │
│    │    (server version, caps)         │                 │
│    │                                   │                 │
│    │── initialized ───────────────────>│                 │
│    │                                   │                 │
├─────────────────────────────────────────────────────────┤
│                      DISCOVERY                           │
├─────────────────────────────────────────────────────────┤
│    │── tools/list ────────────────────>│                 │
│    │                                   │                 │
│    │<── [search, lookup, similar, ...] │                 │
│    │                                   │                 │
│    │── resources/list ────────────────>│                 │
│    │                                   │                 │
│    │<── [file://src/..., ...]  ────────│                 │
│    │                                   │                 │
├─────────────────────────────────────────────────────────┤
│                    OPERATIONS                            │
├─────────────────────────────────────────────────────────┤
│    │── tools/call: search("auth") ────>│                 │
│    │                                   │──> Search       │
│    │                                   │    Engine       │
│    │<── results ───────────────────────│<──              │
│    │                                   │                 │
│    │── resources/read (file://...) ───>│                 │
│    │                                   │                 │
│    │<── file contents ─────────────────│                 │
│    │                                   │                 │
├─────────────────────────────────────────────────────────┤
│                     SHUTDOWN                             │
├─────────────────────────────────────────────────────────┤
│    │── shutdown ──────────────────────>│                 │
│    │                                   │                 │
│    │<── shutdown ack ──────────────────│                 │
│    │                                   │                 │
└─────────────────────────────────────────────────────────┘
```

---

## Go Implementation

### Using the Official SDK

AmanMCP uses the official MCP Go SDK:

```go
import (
    "github.com/modelcontextprotocol/go-sdk/mcp"
    "github.com/modelcontextprotocol/go-sdk/server"
)

func main() {
    // Create server with capabilities
    s := server.NewMCPServer(
        "AmanMCP",
        "0.4.0",
        server.WithToolCapabilities(true),
        server.WithResourceCapabilities(true, false),
    )

    // Register tools
    s.AddTool(searchTool, handleSearch)
    s.AddTool(lookupTool, handleLookup)

    // Start server (stdio transport)
    server.ServeStdio(s)
}
```

### Defining Tools

Tools have a name, description, and input schema:

```go
var searchTool = mcp.NewTool("search",
    mcp.WithDescription("Hybrid search over codebase (BM25 + semantic)"),
    mcp.WithString("query",
        mcp.Required(),
        mcp.Description("Search query"),
    ),
    mcp.WithNumber("limit",
        mcp.Description("Maximum results (default: 10)"),
    ),
)
```

### Tool Handlers

Handlers receive context and request, return result or error:

```go
func handleSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    // Extract and validate arguments
    query, ok := req.Params.Arguments["query"].(string)
    if !ok || query == "" {
        return nil, &mcp.Error{
            Code:    mcp.InvalidParams,
            Message: "query parameter is required",
        }
    }

    limit := 10
    if l, ok := req.Params.Arguments["limit"].(float64); ok {
        limit = int(l)
    }

    // Execute search
    results, err := searchEngine.Search(ctx, query, limit)
    if err != nil {
        return nil, fmt.Errorf("search failed: %w", err)
    }

    // Format and return
    return mcp.NewToolResultText(formatResults(results)), nil
}
```

---

## Error Handling

### Error Codes

MCP uses standard JSON-RPC error codes plus custom ones:

| Code | Name | When to Use |
|------|------|-------------|
| -32700 | Parse error | Invalid JSON |
| -32600 | Invalid request | Not a valid JSON-RPC request |
| -32601 | Method not found | Unknown method |
| -32602 | Invalid params | Missing/wrong parameters |
| -32603 | Internal error | Server-side error |
| -32000 | Server error | Generic server error |
| -32001 | Resource not found | Resource doesn't exist |

### Error Response Format

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32602,
    "message": "Invalid params",
    "data": {
      "parameter": "query",
      "issue": "required parameter missing"
    }
  }
}
```

### Error Handling Pattern

```go
func handleTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    // 1. Validate input early
    query, ok := req.Params.Arguments["query"].(string)
    if !ok {
        return nil, &mcp.Error{
            Code:    mcp.InvalidParams,
            Message: "query must be a string",
        }
    }

    // 2. Check context for cancellation
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }

    // 3. Execute operation
    result, err := doWork(ctx, query)
    if err != nil {
        // Log detailed error internally
        log.Printf("operation failed: %v", err)

        // Return generic message to client
        return nil, &mcp.Error{
            Code:    mcp.InternalError,
            Message: "operation failed",
        }
    }

    return mcp.NewToolResultText(result), nil
}
```

---

## AmanMCP's Tool Implementations

### Search Tool

```go
var amanMCPTools = []mcp.Tool{
    {
        Name:        "search",
        Description: "Hybrid search over codebase (BM25 + semantic)",
        InputSchema: mcp.Schema{
            Type: "object",
            Properties: map[string]mcp.Schema{
                "query": {Type: "string", Description: "Search query"},
                "limit": {Type: "number", Description: "Max results"},
            },
            Required: []string{"query"},
        },
    },
    {
        Name:        "lookup",
        Description: "Get specific code by file path and symbol",
        InputSchema: lookupSchema,
    },
    {
        Name:        "similar",
        Description: "Find code similar to a reference",
        InputSchema: similarSchema,
    },
    {
        Name:        "index-status",
        Description: "Check indexing status and statistics",
        InputSchema: nil, // No input needed
    },
}
```

### Resource Implementation

```go
func (s *Server) ListResources() []mcp.Resource {
    var resources []mcp.Resource

    // Expose indexed files as resources
    for _, file := range s.index.Files() {
        resources = append(resources, mcp.Resource{
            URI:         "file://" + file.Path,
            Name:        filepath.Base(file.Path),
            MimeType:    mimeTypeFor(file.Path),
            Description: fmt.Sprintf("Indexed file (%d chunks)", file.ChunkCount),
        })
    }

    return resources
}

func (s *Server) ReadResource(uri string) (string, error) {
    path := strings.TrimPrefix(uri, "file://")

    content, err := os.ReadFile(path)
    if err != nil {
        return "", &mcp.Error{
            Code:    -32001,
            Message: "resource not found",
        }
    }

    return string(content), nil
}
```

---

## Advanced Patterns

### Progress Reporting

For long operations, report progress:

```go
func (s *Server) handleReindex(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    files := s.scanner.AllFiles()
    total := len(files)

    for i, file := range files {
        // Check cancellation
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        default:
        }

        // Report progress
        s.mcp.SendProgress(mcp.Progress{
            Token:   req.ProgressToken,
            Current: float64(i),
            Total:   float64(total),
            Message: fmt.Sprintf("Indexing %s", file.Name),
        })

        if err := s.index(file); err != nil {
            log.Printf("index error for %s: %v", file.Name, err)
            continue // Skip problematic files
        }
    }

    return mcp.NewToolResultText(fmt.Sprintf("Indexed %d files", total)), nil
}
```

### Concurrent Tool Execution

Handle multiple concurrent requests safely:

```go
type Server struct {
    mcp    *server.MCPServer
    engine *SearchEngine
    mu     sync.RWMutex // Protects engine state
}

func (s *Server) handleSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    // Engine is safely accessed
    results, err := s.engine.Search(ctx, query, limit)
    // ...
}

func (s *Server) handleReindex(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    // Exclusive access for reindexing
    err := s.engine.Reindex(ctx)
    // ...
}
```

### Pagination

For large result sets:

```go
type SearchArgs struct {
    Query  string `json:"query"`
    Limit  int    `json:"limit"`
    Offset int    `json:"offset"`
}

func handleSearch(args SearchArgs) *mcp.CallToolResult {
    // Fetch one extra to detect more results
    results := engine.Search(args.Query, args.Limit+1, args.Offset)

    hasMore := len(results) > args.Limit
    if hasMore {
        results = results[:args.Limit]
    }

    response := formatResults(results)
    if hasMore {
        response += fmt.Sprintf("\n\n_More results available. Use offset=%d_", args.Offset+args.Limit)
    }

    return mcp.NewToolResultText(response)
}
```

---

## Testing MCP Servers

### Unit Testing

```go
func TestSearchTool(t *testing.T) {
    // Setup test server with mock engine
    engine := NewMockSearchEngine()
    engine.AddResult("auth.go", "func Auth() {}", 0.95)

    server := NewServer(engine)

    // Call tool
    result, err := server.handleSearch(context.Background(), mcp.CallToolRequest{
        Params: mcp.CallToolParams{
            Name: "search",
            Arguments: map[string]any{
                "query": "authentication",
                "limit": 5,
            },
        },
    })

    require.NoError(t, err)
    require.NotNil(t, result)

    text := result.Content[0].(mcp.TextContent).Text
    assert.Contains(t, text, "auth.go")
    assert.Contains(t, text, "0.95")
}
```

### Integration Testing with MCP Inspector

```bash
# Install inspector
npm install -g @modelcontextprotocol/inspector

# Run interactive test
mcp-inspector amanmcp serve
```

The inspector provides:
- Tool listing and testing UI
- JSON-RPC message viewer
- Error inspection

---

## Common Mistakes

### 1. Not Validating Input

```go
// BAD: Trusts all input
query := args["query"].(string)  // Panics if not string

// GOOD: Validate everything
query, ok := args["query"].(string)
if !ok || query == "" {
    return nil, &mcp.Error{Code: mcp.InvalidParams, Message: "query required"}
}
```

### 2. Blocking the Event Loop

```go
// BAD: Blocks all MCP traffic
time.Sleep(10 * time.Second)

// GOOD: Use goroutines, send notification when done
go func() {
    doLongWork()
    s.SendNotification(mcp.Notification{
        Method: "indexing/complete",
    })
}()
```

### 3. Ignoring Context Cancellation

```go
// BAD: Ignores cancellation
for _, file := range files {
    index(file)
}

// GOOD: Check context
for _, file := range files {
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
        index(file)
    }
}
```

### 4. Leaking Sensitive Information

```go
// BAD: Exposes internal paths
return nil, fmt.Errorf("failed to read /home/user/.ssh/id_rsa: %w", err)

// GOOD: Generic message, log details internally
log.Printf("file read failed: %v", err)
return nil, &mcp.Error{Code: mcp.InternalError, Message: "file read failed"}
```

---

## MCP Specification Reference

### Current Version

AmanMCP implements MCP spec version **2025-11-25**.

### Capabilities Declaration

```go
server.NewMCPServer(
    "AmanMCP",
    "0.4.0",
    server.WithToolCapabilities(true),
    server.WithResourceCapabilities(
        true,  // subscribe supported
        true,  // listChanged supported
    ),
    server.WithPromptCapabilities(true),
)
```

### Further Reading

- [MCP Specification](https://modelcontextprotocol.io/specification/2025-11-25) - Official spec
- [Go SDK Documentation](https://github.com/modelcontextprotocol/go-sdk) - SDK reference
- [MCP Servers Directory](https://github.com/modelcontextprotocol/servers) - Example servers

---

## Next Steps

| Want to... | Read |
|------------|------|
| Configure for Claude/Cursor | [Using MCP](using-mcp.md) |
| Understand search internals | [Hybrid Search](../hybrid-search/) |
| Debug MCP issues | [Debugging MCP Protocol](../../articles/debugging-mcp-protocol.md) |

---

*MCP is the interface. Your tools implement the value.*
