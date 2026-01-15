package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/search"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

// ============================================================================
// TS01: Search Tool Basic - Returns Markdown
// ============================================================================

func TestSearchTool_Basic_ReturnsMarkdown(t *testing.T) {
	// Given: server with mock search returning results
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			return []*search.SearchResult{
				{
					Chunk: &store.Chunk{
						FilePath:  "internal/auth/handler.go",
						StartLine: 42,
						EndLine:   78,
						Content:   "func AuthMiddleware() {}",
						Language:  "go",
					},
					Score: 0.95,
				},
			}, nil
		},
	}
	srv := newTestServerWithEngine(t, engine)

	// When: calling search tool
	result, err := srv.CallTool(context.Background(), "search", map[string]any{
		"query": "authentication",
	})

	// Then: markdown format returned (not struct)
	require.NoError(t, err)
	text, ok := result.(string)
	require.True(t, ok, "expected string result, got %T", result)
	assert.Contains(t, text, "## Search Results")
	assert.Contains(t, text, "internal/auth/handler.go:42-78")
	assert.Contains(t, text, "score: 0.95")
	assert.Contains(t, text, "```go")
}

// ============================================================================
// TS02: Search with Filter
// ============================================================================

func TestSearchTool_WithCodeFilter_FiltersResults(t *testing.T) {
	// Given: server with mock search
	var capturedOpts search.SearchOptions
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			capturedOpts = opts
			return []*search.SearchResult{}, nil
		},
	}
	srv := newTestServerWithEngine(t, engine)

	// When: calling search with filter=code
	_, err := srv.CallTool(context.Background(), "search", map[string]any{
		"query":  "test",
		"filter": "code",
	})

	// Then: filter passed to engine
	require.NoError(t, err)
	assert.Equal(t, "code", capturedOpts.Filter)
}

// ============================================================================
// TS03: Search Code with Language
// ============================================================================

func TestSearchCodeTool_WithLanguage_FiltersResults(t *testing.T) {
	var capturedOpts search.SearchOptions
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			capturedOpts = opts
			return []*search.SearchResult{}, nil
		},
	}
	srv := newTestServerWithEngine(t, engine)

	// When: calling search_code with language=go
	_, err := srv.CallTool(context.Background(), "search_code", map[string]any{
		"query":    "handler",
		"language": "go",
	})

	// Then: language filter applied, code filter implicit
	require.NoError(t, err)
	assert.Equal(t, "code", capturedOpts.Filter)
	assert.Equal(t, "go", capturedOpts.Language)
}

// ============================================================================
// TS04: Search Code with Symbol Type
// ============================================================================

func TestSearchCodeTool_WithSymbolType_FiltersResults(t *testing.T) {
	var capturedOpts search.SearchOptions
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			capturedOpts = opts
			return []*search.SearchResult{}, nil
		},
	}
	srv := newTestServerWithEngine(t, engine)

	// When: calling search_code with symbol_type=function
	_, err := srv.CallTool(context.Background(), "search_code", map[string]any{
		"query":       "auth",
		"symbol_type": "function",
	})

	// Then: symbol type filter applied
	require.NoError(t, err)
	assert.Equal(t, "function", capturedOpts.SymbolType)
}

func TestSearchCodeTool_SymbolTypeAny_DoesNotFilter(t *testing.T) {
	var capturedOpts search.SearchOptions
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			capturedOpts = opts
			return []*search.SearchResult{}, nil
		},
	}
	srv := newTestServerWithEngine(t, engine)

	// When: calling search_code with symbol_type=any
	_, err := srv.CallTool(context.Background(), "search_code", map[string]any{
		"query":       "auth",
		"symbol_type": "any",
	})

	// Then: symbol type is empty (no filter)
	require.NoError(t, err)
	assert.Equal(t, "", capturedOpts.SymbolType)
}

// ============================================================================
// TS05: Search Docs Preserves Section Hierarchy
// ============================================================================

func TestSearchDocsTool_PreservesSectionHierarchy(t *testing.T) {
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			return []*search.SearchResult{
				{
					Chunk: &store.Chunk{
						FilePath: "docs/installation.md",
						Content:  "## Installation\n\nRun `go install`...",
						Language: "markdown",
					},
					Score: 0.88,
				},
			}, nil
		},
	}
	srv := newTestServerWithEngine(t, engine)

	// When: calling search_docs
	result, err := srv.CallTool(context.Background(), "search_docs", map[string]any{
		"query": "installation",
	})

	// Then: markdown content preserved, docs filter applied
	require.NoError(t, err)
	text, ok := result.(string)
	require.True(t, ok)
	assert.Contains(t, text, "## Installation")
	assert.Contains(t, text, "docs/installation.md")
}

func TestSearchDocsTool_AppliesDocsFilter(t *testing.T) {
	var capturedOpts search.SearchOptions
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			capturedOpts = opts
			return []*search.SearchResult{}, nil
		},
	}
	srv := newTestServerWithEngine(t, engine)

	// When: calling search_docs
	_, err := srv.CallTool(context.Background(), "search_docs", map[string]any{
		"query": "installation",
	})

	// Then: docs filter applied automatically
	require.NoError(t, err)
	assert.Equal(t, "docs", capturedOpts.Filter)
}

// ============================================================================
// TS06: Index Status Returns JSON
// ============================================================================

func TestIndexStatusTool_ReturnsJSON(t *testing.T) {
	engine := &MockSearchEngine{
		StatsFn: func() *search.EngineStats {
			return &search.EngineStats{
				BM25Stats:   &store.IndexStats{DocumentCount: 100},
				VectorCount: 250,
			}
		},
	}
	srv := newTestServerWithEngine(t, engine)

	// When: calling index_status
	result, err := srv.CallTool(context.Background(), "index_status", map[string]any{})

	// Then: returns IndexStatusOutput struct
	require.NoError(t, err)
	output, ok := result.(*IndexStatusOutput)
	require.True(t, ok, "expected *IndexStatusOutput, got %T", result)
	assert.Equal(t, 100, output.Stats.FileCount)
	assert.Equal(t, 250, output.Stats.ChunkCount)
	assert.NotEmpty(t, output.Project.Name)
}

// ============================================================================
// TS06B: Capability Signaling - Hugot Embedder
// ============================================================================

func TestIndexStatusTool_HugotEmbedder_HighSemanticQuality(t *testing.T) {
	// Given: server with Hugot embedder (768 dimensions)
	engine := &MockSearchEngine{}
	metadata := &MockMetadataStore{}
	embedder := &MockEmbedder{
		DimensionsFn: func() int { return 768 },
		ModelNameFn:  func() string { return "embeddinggemma-300m" },
		AvailableFn:  func(_ context.Context) bool { return true },
	}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, embedder, cfg, "")
	require.NoError(t, err)

	// When: calling index_status
	result, err := srv.CallTool(context.Background(), "index_status", map[string]any{})

	// Then: returns high semantic quality indicators
	require.NoError(t, err)
	output, ok := result.(*IndexStatusOutput)
	require.True(t, ok)

	// Verify capability signaling fields
	assert.Equal(t, "hugot", output.Embeddings.ActualProvider)
	assert.Equal(t, "embeddinggemma-300m", output.Embeddings.ActualModel)
	assert.Equal(t, 768, output.Embeddings.Dimensions)
	assert.False(t, output.Embeddings.IsFallbackActive)
	assert.Equal(t, "high", output.Embeddings.SemanticQuality)
	assert.Equal(t, "ready", output.Embeddings.Status)
}

// ============================================================================
// TS06C: Capability Signaling - Static Fallback
// ============================================================================

func TestIndexStatusTool_StaticEmbedder_LowSemanticQuality(t *testing.T) {
	// Given: server with static embedder (256 dimensions)
	engine := &MockSearchEngine{}
	metadata := &MockMetadataStore{}
	embedder := &MockEmbedder{
		DimensionsFn: func() int { return 256 },
		ModelNameFn:  func() string { return "static" },
		AvailableFn:  func(_ context.Context) bool { return true },
	}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, embedder, cfg, "")
	require.NoError(t, err)

	// When: calling index_status
	result, err := srv.CallTool(context.Background(), "index_status", map[string]any{})

	// Then: returns low semantic quality indicators
	require.NoError(t, err)
	output, ok := result.(*IndexStatusOutput)
	require.True(t, ok)

	// Verify capability signaling fields
	assert.Equal(t, "static", output.Embeddings.ActualProvider)
	assert.Equal(t, "static", output.Embeddings.ActualModel)
	assert.Equal(t, 256, output.Embeddings.Dimensions)
	assert.True(t, output.Embeddings.IsFallbackActive)
	assert.Equal(t, "low", output.Embeddings.SemanticQuality)
	assert.Equal(t, "ready", output.Embeddings.Status)
}

// ============================================================================
// TS06D: Capability Signaling - No Embedder
// ============================================================================

func TestIndexStatusTool_NilEmbedder_Unavailable(t *testing.T) {
	// Given: server without embedder
	engine := &MockSearchEngine{}
	metadata := &MockMetadataStore{}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, nil, cfg, "")
	require.NoError(t, err)

	// When: calling index_status
	result, err := srv.CallTool(context.Background(), "index_status", map[string]any{})

	// Then: returns unavailable status
	require.NoError(t, err)
	output, ok := result.(*IndexStatusOutput)
	require.True(t, ok)

	// Verify capability signaling fields
	assert.Equal(t, "none", output.Embeddings.ActualProvider)
	assert.Equal(t, "none", output.Embeddings.ActualModel)
	assert.Equal(t, 0, output.Embeddings.Dimensions)
	assert.True(t, output.Embeddings.IsFallbackActive)
	assert.Equal(t, "none", output.Embeddings.SemanticQuality)
	assert.Equal(t, "unavailable", output.Embeddings.Status)
}

// ============================================================================
// TS07: Empty Results Handling
// ============================================================================

func TestSearchTool_EmptyResults_GracefulMessage(t *testing.T) {
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			return []*search.SearchResult{}, nil
		},
	}
	srv := newTestServerWithEngine(t, engine)

	// When: search returns no results
	result, err := srv.CallTool(context.Background(), "search", map[string]any{
		"query": "xyznonexistent123",
	})

	// Then: friendly message, no error
	require.NoError(t, err)
	text, ok := result.(string)
	require.True(t, ok)
	assert.Contains(t, text, "No results found")
	assert.Contains(t, text, "xyznonexistent123")
}

// ============================================================================
// TS08: Missing Required Parameter
// ============================================================================

func TestSearchTool_MissingQuery_ReturnsError(t *testing.T) {
	srv := newTestServer(t)

	// When: calling search without query
	_, err := srv.CallTool(context.Background(), "search", map[string]any{})

	// Then: invalid params error
	require.Error(t, err)
	var mcpErr *MCPError
	require.ErrorAs(t, err, &mcpErr)
	assert.Equal(t, ErrCodeInvalidParams, mcpErr.Code)
}

func TestSearchCodeTool_MissingQuery_ReturnsError(t *testing.T) {
	srv := newTestServer(t)

	// When: calling search_code without query
	_, err := srv.CallTool(context.Background(), "search_code", map[string]any{
		"language": "go",
	})

	// Then: invalid params error
	require.Error(t, err)
	var mcpErr *MCPError
	require.ErrorAs(t, err, &mcpErr)
	assert.Equal(t, ErrCodeInvalidParams, mcpErr.Code)
}

func TestSearchDocsTool_MissingQuery_ReturnsError(t *testing.T) {
	srv := newTestServer(t)

	// When: calling search_docs without query
	_, err := srv.CallTool(context.Background(), "search_docs", map[string]any{})

	// Then: invalid params error
	require.Error(t, err)
	var mcpErr *MCPError
	require.ErrorAs(t, err, &mcpErr)
	assert.Equal(t, ErrCodeInvalidParams, mcpErr.Code)
}

// ============================================================================
// TS09: Limit Parameter Clamping
// ============================================================================

func TestSearchTool_LimitClamping(t *testing.T) {
	tests := []struct {
		name     string
		limit    float64
		expected int
	}{
		{"above max", 100, 50},
		{"zero uses default", 0, 10},
		{"negative uses default", -5, 10},
		{"valid", 25, 25},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var capturedOpts search.SearchOptions
			engine := &MockSearchEngine{
				SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
					capturedOpts = opts
					return []*search.SearchResult{}, nil
				},
			}
			srv := newTestServerWithEngine(t, engine)

			_, _ = srv.CallTool(context.Background(), "search", map[string]any{
				"query": "test",
				"limit": tc.limit,
			})

			assert.Equal(t, tc.expected, capturedOpts.Limit)
		})
	}
}

// ============================================================================
// TS10: Large Result Formatting
// ============================================================================

func TestSearchTool_LargeResults_FormatsAll(t *testing.T) {
	// Generate 50 results
	results := make([]*search.SearchResult, 50)
	for i := 0; i < 50; i++ {
		results[i] = &search.SearchResult{
			Chunk: &store.Chunk{
				FilePath:  "file.go",
				StartLine: i * 10,
				EndLine:   i*10 + 10,
				Content:   "func Test() {}",
				Language:  "go",
			},
			Score: float64(50-i) / 50.0,
		}
	}

	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			return results, nil
		},
	}
	srv := newTestServerWithEngine(t, engine)

	// When: formatting 50 results
	result, err := srv.CallTool(context.Background(), "search", map[string]any{
		"query": "test",
		"limit": float64(50),
	})

	// Then: all 50 included
	require.NoError(t, err)
	text, ok := result.(string)
	require.True(t, ok)
	assert.Contains(t, text, "Found 50 results")
	assert.Equal(t, 50, strings.Count(text, "### "))
}

// ============================================================================
// ListTools Tests
// ============================================================================

func TestListTools_ReturnsAllFourTools(t *testing.T) {
	srv := newTestServer(t)

	tools := srv.ListTools()

	// Should have 4 tools
	assert.Len(t, tools, 4)

	// Find tool names
	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name] = true
	}

	assert.True(t, names["search"], "missing search tool")
	assert.True(t, names["search_code"], "missing search_code tool")
	assert.True(t, names["search_docs"], "missing search_docs tool")
	assert.True(t, names["index_status"], "missing index_status tool")
}

// ============================================================================
// Helper Functions
// ============================================================================

// newTestServerWithEngine creates a server with a custom mock engine.
// Note: newTestServer is defined in server_test.go
func newTestServerWithEngine(t *testing.T, engine *MockSearchEngine) *Server {
	t.Helper()
	metadata := &MockMetadataStore{}
	embedder := &MockEmbedder{}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, embedder, cfg, "")
	require.NoError(t, err)
	return srv
}
