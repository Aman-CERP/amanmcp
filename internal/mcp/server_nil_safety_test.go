package mcp

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/search"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

// Nil Safety Tests - These test that the MCP server handles nil values
// and error conditions gracefully without panicking.

// =============================================================================
// Nil Embedder Tests
// =============================================================================

// TestServer_NilEmbedder_CreatesSuccessfully tests that server works without
// embedder (embedder is optional).
func TestServer_NilEmbedder_CreatesSuccessfully(t *testing.T) {
	// Given: nil embedder
	engine := &MockSearchEngine{}
	metadata := &MockMetadataStore{}
	cfg := config.NewConfig()

	// When: creating server with nil embedder
	srv, err := NewServer(engine, metadata, nil, cfg, "")

	// Then: server is created successfully
	require.NoError(t, err)
	require.NotNil(t, srv)
}

// TestServer_NilEmbedder_SearchStillWorks tests that search works even
// without an embedder.
func TestServer_NilEmbedder_SearchStillWorks(t *testing.T) {
	// Given: server with nil embedder
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			return []*search.SearchResult{
				{
					Chunk: &store.Chunk{
						ID:       "test-1",
						Content:  "Test content",
						FilePath: "test.go",
					},
					Score: 0.9,
				},
			}, nil
		},
	}
	metadata := &MockMetadataStore{}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, nil, cfg, "")
	require.NoError(t, err)

	// When: calling search tool
	result, err := srv.CallTool(context.Background(), "search", map[string]any{
		"query": "test query",
	})

	// Then: search succeeds
	require.NoError(t, err)
	assert.NotEmpty(t, result)
}

// =============================================================================
// Search Engine Error Handling Tests
// =============================================================================

// TestServer_SearchEngineError_ReturnsErrorNotPanic tests that search engine
// errors are properly propagated as errors, not panics.
func TestServer_SearchEngineError_ReturnsErrorNotPanic(t *testing.T) {
	// Given: search engine that returns an error
	searchErr := errors.New("search engine failure")
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			return nil, searchErr
		},
	}
	metadata := &MockMetadataStore{}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)

	// When: calling search tool (should not panic)
	_, err = srv.CallTool(context.Background(), "search", map[string]any{
		"query": "test query",
	})

	// Then: error is returned (not panic)
	require.Error(t, err, "Search engine error should be returned as error")
}

// TestServer_SearchEngineNilResults_ReturnsEmptyGracefully tests that nil
// results from search engine are handled gracefully.
func TestServer_SearchEngineNilResults_ReturnsEmptyGracefully(t *testing.T) {
	// Given: search engine that returns nil results
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			return nil, nil // Nil results, no error
		},
	}
	metadata := &MockMetadataStore{}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)

	// When: calling search tool
	result, err := srv.CallTool(context.Background(), "search", map[string]any{
		"query": "test query",
	})

	// Then: empty results are returned gracefully (not panic)
	require.NoError(t, err)
	assert.Contains(t, result, "No results found")
}

// TestServer_SearchResultsWithNilChunks_FilteredOut tests that results
// with nil chunks are filtered out gracefully.
func TestServer_SearchResultsWithNilChunks_FilteredOut(t *testing.T) {
	// Given: search engine that returns results with some nil chunks
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			return []*search.SearchResult{
				{Chunk: nil, Score: 0.9},    // Nil chunk
				{Chunk: &store.Chunk{ID: "valid", Content: "Valid content", FilePath: "test.go"}, Score: 0.8},
				nil,                          // Nil result
				{Chunk: nil, Score: 0.7},    // Another nil chunk
			}, nil
		},
	}
	metadata := &MockMetadataStore{}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)

	// When: calling search tool
	result, err := srv.CallTool(context.Background(), "search", map[string]any{
		"query": "test query",
	})

	// Then: only valid result is included (not panic)
	require.NoError(t, err)
	resultStr := result.(string)
	assert.Contains(t, resultStr, "Valid content")
}

// =============================================================================
// Concurrent Access Tests
// =============================================================================

// TestServer_ConcurrentSearch_NoRace tests that concurrent search operations
// don't cause race conditions or panics.
func TestServer_ConcurrentSearch_NoRace(t *testing.T) {
	// Given: a server
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			return []*search.SearchResult{
				{Chunk: &store.Chunk{ID: "test", Content: "Test"}, Score: 0.9},
			}, nil
		},
	}
	metadata := &MockMetadataStore{}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)

	// When: many concurrent searches
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := srv.CallTool(context.Background(), "search", map[string]any{
				"query": "concurrent test",
			})
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Then: all searches complete without error
	for err := range errors {
		t.Errorf("Concurrent search failed: %v", err)
	}
}

// TestServer_ConcurrentToolCalls_NoRace tests that concurrent tool calls
// of different types don't cause race conditions.
func TestServer_ConcurrentToolCalls_NoRace(t *testing.T) {
	// Given: a server with stats
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			return []*search.SearchResult{}, nil
		},
		StatsFn: func() *search.EngineStats {
			return &search.EngineStats{
				VectorCount: 100,
			}
		},
	}
	metadata := &MockMetadataStore{}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)

	// When: concurrent calls to different tools
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Search calls
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := srv.CallTool(context.Background(), "search", map[string]any{
				"query": "test",
			})
			if err != nil {
				errors <- err
			}
		}()
	}

	// Index status calls
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := srv.CallTool(context.Background(), "index_status", nil)
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Then: all calls complete without error
	for err := range errors {
		t.Errorf("Concurrent tool call failed: %v", err)
	}
}

// =============================================================================
// Context Cancellation Tests
// =============================================================================

// TestServer_CancelledContext_ReturnsError tests that cancelled contexts
// are handled gracefully.
func TestServer_CancelledContext_ReturnsError(t *testing.T) {
	// Given: a server
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			// Check if context is cancelled
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return []*search.SearchResult{}, nil
		},
	}
	metadata := &MockMetadataStore{}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)

	// When: calling with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = srv.CallTool(ctx, "search", map[string]any{
		"query": "test",
	})

	// Then: context cancellation error is returned (not panic)
	require.Error(t, err)
}

// =============================================================================
// Stats Nil Safety Tests
// =============================================================================

// TestServer_NilStats_HandledGracefully tests that nil stats from engine
// are handled gracefully in index_status.
func TestServer_NilStats_HandledGracefully(t *testing.T) {
	// Given: search engine that returns nil stats
	engine := &MockSearchEngine{
		StatsFn: func() *search.EngineStats {
			return nil
		},
	}
	metadata := &MockMetadataStore{}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)

	// When: calling index_status
	result, err := srv.CallTool(context.Background(), "index_status", nil)

	// Then: graceful response (not panic)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// =============================================================================
// Invalid Arguments Tests
// =============================================================================

// TestServer_NilArguments_HandledGracefully tests that nil arguments map
// is handled gracefully.
func TestServer_NilArguments_HandledGracefully(t *testing.T) {
	// Given: a server
	engine := &MockSearchEngine{}
	metadata := &MockMetadataStore{}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)

	// When: calling search with nil arguments
	_, err = srv.CallTool(context.Background(), "search", nil)

	// Then: error returned (not panic) - query is required
	require.Error(t, err, "Nil arguments should return error for search")
}

// TestServer_EmptyQuery_ReturnsError tests that empty query returns
// an error instead of panicking.
func TestServer_EmptyQuery_ReturnsError(t *testing.T) {
	// Given: a server
	engine := &MockSearchEngine{}
	metadata := &MockMetadataStore{}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)

	// When: calling search with empty query
	_, err = srv.CallTool(context.Background(), "search", map[string]any{
		"query": "",
	})

	// Then: error returned (not panic)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query")
}

// TestServer_WhitespaceQuery_Rejected tests that whitespace-only query
// is rejected with a validation error (DEBT-019 resolved).
func TestServer_WhitespaceQuery_Rejected(t *testing.T) {
	// Given: a server
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			return []*search.SearchResult{}, nil
		},
	}
	metadata := &MockMetadataStore{}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)

	// When: calling search with whitespace query
	result, err := srv.CallTool(context.Background(), "search", map[string]any{
		"query": "   ",
	})

	// Then: validation error is returned (DEBT-019 resolved)
	require.Error(t, err, "Whitespace query should be rejected")
	require.Empty(t, result, "Result should be empty when validation fails")
	assert.Contains(t, err.Error(), "query cannot be empty or whitespace only")
}

// TestServer_WrongArgumentType_ReturnsError tests that wrong argument types
// return errors instead of panicking.
func TestServer_WrongArgumentType_ReturnsError(t *testing.T) {
	// Given: a server
	engine := &MockSearchEngine{}
	metadata := &MockMetadataStore{}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)

	// When: calling search with wrong type for query
	_, err = srv.CallTool(context.Background(), "search", map[string]any{
		"query": 123, // Should be string, not int
	})

	// Then: error returned (not panic)
	require.Error(t, err)
}

// TestServer_NegativeLimit_HandledGracefully tests that negative limit
// is handled gracefully.
func TestServer_NegativeLimit_HandledGracefully(t *testing.T) {
	// Given: a server
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			// Limit should be normalized
			return []*search.SearchResult{}, nil
		},
	}
	metadata := &MockMetadataStore{}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)

	// When: calling search with negative limit
	_, err = srv.CallTool(context.Background(), "search", map[string]any{
		"query": "test",
		"limit": -10,
	})

	// Then: handled gracefully (not panic)
	require.NoError(t, err)
}
