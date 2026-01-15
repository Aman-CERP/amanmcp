package mcp

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/embed"
	"github.com/Aman-CERP/amanmcp/internal/search"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

// MockSearchEngine implements search.SearchEngine for testing.
type MockSearchEngine struct {
	SearchFn func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error)
	IndexFn  func(ctx context.Context, chunks []*store.Chunk) error
	DeleteFn func(ctx context.Context, chunkIDs []string) error
	StatsFn  func() *search.EngineStats
	CloseFn  func() error
}

func (m *MockSearchEngine) Search(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
	if m.SearchFn != nil {
		return m.SearchFn(ctx, query, opts)
	}
	return []*search.SearchResult{}, nil
}

func (m *MockSearchEngine) Index(ctx context.Context, chunks []*store.Chunk) error {
	if m.IndexFn != nil {
		return m.IndexFn(ctx, chunks)
	}
	return nil
}

func (m *MockSearchEngine) Delete(ctx context.Context, chunkIDs []string) error {
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, chunkIDs)
	}
	return nil
}

func (m *MockSearchEngine) Stats() *search.EngineStats {
	if m.StatsFn != nil {
		return m.StatsFn()
	}
	return &search.EngineStats{}
}

func (m *MockSearchEngine) Close() error {
	if m.CloseFn != nil {
		return m.CloseFn()
	}
	return nil
}

// Ensure MockSearchEngine implements search.SearchEngine
var _ search.SearchEngine = (*MockSearchEngine)(nil)

// MockMetadataStore implements store.MetadataStore for testing.
type MockMetadataStore struct {
	Files           []*store.File
	Chunks          []*store.Chunk
	GetFileByPathFn func(ctx context.Context, projectID, path string) (*store.File, error)
}

func (m *MockMetadataStore) SaveProject(_ context.Context, _ *store.Project) error { return nil }
func (m *MockMetadataStore) GetProject(_ context.Context, _ string) (*store.Project, error) {
	return nil, nil
}
func (m *MockMetadataStore) UpdateProjectStats(_ context.Context, _ string, _, _ int) error {
	return nil
}
func (m *MockMetadataStore) RefreshProjectStats(_ context.Context, _ string) error {
	return nil
}
func (m *MockMetadataStore) SaveFiles(_ context.Context, _ []*store.File) error { return nil }
func (m *MockMetadataStore) GetFileByPath(ctx context.Context, projectID, path string) (*store.File, error) {
	if m.GetFileByPathFn != nil {
		return m.GetFileByPathFn(ctx, projectID, path)
	}
	return nil, nil
}
func (m *MockMetadataStore) GetChangedFiles(_ context.Context, _ string, _ time.Time) ([]*store.File, error) {
	return m.Files, nil
}
func (m *MockMetadataStore) ListFiles(_ context.Context, _ string, _ string, limit int) ([]*store.File, string, error) {
	if limit <= 0 || limit > len(m.Files) {
		return m.Files, "", nil
	}
	return m.Files[:limit], "", nil
}
func (m *MockMetadataStore) DeleteFilesByProject(_ context.Context, _ string) error { return nil }
func (m *MockMetadataStore) SaveChunks(_ context.Context, _ []*store.Chunk) error   { return nil }
func (m *MockMetadataStore) GetChunk(_ context.Context, id string) (*store.Chunk, error) {
	for _, c := range m.Chunks {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, nil
}
func (m *MockMetadataStore) GetChunks(_ context.Context, ids []string) ([]*store.Chunk, error) {
	result := make([]*store.Chunk, 0, len(ids))
	for _, id := range ids {
		for _, c := range m.Chunks {
			if c.ID == id {
				result = append(result, c)
				break
			}
		}
	}
	return result, nil
}
func (m *MockMetadataStore) GetChunksByFile(_ context.Context, _ string) ([]*store.Chunk, error) {
	return m.Chunks, nil
}
func (m *MockMetadataStore) DeleteChunks(_ context.Context, _ []string) error     { return nil }
func (m *MockMetadataStore) DeleteChunksByFile(_ context.Context, _ string) error { return nil }
func (m *MockMetadataStore) SearchSymbols(_ context.Context, _ string, _ int) ([]*store.Symbol, error) {
	return nil, nil
}
func (m *MockMetadataStore) GetFilePathsByProject(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (m *MockMetadataStore) GetFilesForReconciliation(_ context.Context, _ string) (map[string]*store.File, error) {
	return nil, nil
}
func (m *MockMetadataStore) ListFilePathsUnder(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}
func (m *MockMetadataStore) DeleteFile(_ context.Context, _ string) error { return nil }
func (m *MockMetadataStore) GetState(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (m *MockMetadataStore) SetState(_ context.Context, _, _ string) error { return nil }

// Embedding methods (for HNSW compaction - BUG-024 fix)
func (m *MockMetadataStore) SaveChunkEmbeddings(_ context.Context, _ []string, _ [][]float32, _ string) error {
	return nil
}
func (m *MockMetadataStore) GetAllEmbeddings(_ context.Context) (map[string][]float32, error) {
	return nil, nil
}
func (m *MockMetadataStore) GetEmbeddingStats(_ context.Context) (int, int, error) {
	return 0, 0, nil
}

// Checkpoint methods (DEBT-022: Index Runner)
func (m *MockMetadataStore) SaveIndexCheckpoint(_ context.Context, _ string, _, _ int, _ string) error {
	return nil
}
func (m *MockMetadataStore) LoadIndexCheckpoint(_ context.Context) (*store.IndexCheckpoint, error) {
	return nil, nil
}
func (m *MockMetadataStore) ClearIndexCheckpoint(_ context.Context) error {
	return nil
}

func (m *MockMetadataStore) Close() error { return nil }

// Ensure MockMetadataStore implements store.MetadataStore
var _ store.MetadataStore = (*MockMetadataStore)(nil)

// MockEmbedder implements embed.Embedder for testing.
type MockEmbedder struct {
	DimensionsFn func() int
	ModelNameFn  func() string
	AvailableFn  func(ctx context.Context) bool
}

func (m *MockEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, m.Dimensions()), nil
}

func (m *MockEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = make([]float32, m.Dimensions())
	}
	return result, nil
}

func (m *MockEmbedder) Dimensions() int {
	if m.DimensionsFn != nil {
		return m.DimensionsFn()
	}
	return embed.DefaultDimensions // Default to Hugot dimensions
}

func (m *MockEmbedder) ModelName() string {
	if m.ModelNameFn != nil {
		return m.ModelNameFn()
	}
	return "embeddinggemma-300m"
}

func (m *MockEmbedder) Available(ctx context.Context) bool {
	if m.AvailableFn != nil {
		return m.AvailableFn(ctx)
	}
	return true
}

func (m *MockEmbedder) Close() error          { return nil }
func (m *MockEmbedder) SetBatchIndex(_ int)   {}
func (m *MockEmbedder) SetFinalBatch(_ bool)  {}

// Ensure MockEmbedder implements embed.Embedder
var _ embed.Embedder = (*MockEmbedder)(nil)

// newTestServer creates a server with mock dependencies for testing.
func newTestServer(t *testing.T) *Server {
	t.Helper()

	engine := &MockSearchEngine{}
	metadata := &MockMetadataStore{}
	embedder := &MockEmbedder{}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, embedder, cfg, "")
	require.NoError(t, err)
	require.NotNil(t, srv)

	return srv
}

// =============================================================================
// TS01: Server Initialization
// =============================================================================

func TestServer_New_Success(t *testing.T) {
	// Given: valid dependencies
	engine := &MockSearchEngine{}
	metadata := &MockMetadataStore{}
	cfg := config.NewConfig()

	// When: creating server
	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")

	// Then: no error, server is valid
	require.NoError(t, err)
	require.NotNil(t, srv)
	assert.NotNil(t, srv.MCPServer())
}

func TestServer_New_NilEngine_ReturnsError(t *testing.T) {
	// Given: nil search engine
	metadata := &MockMetadataStore{}
	cfg := config.NewConfig()

	// When: creating server
	srv, err := NewServer(nil, metadata, &MockEmbedder{}, cfg, "")

	// Then: error returned
	require.Error(t, err)
	assert.Nil(t, srv)
	assert.Contains(t, err.Error(), "search engine")
}

func TestServer_New_NilMetadata_ReturnsError(t *testing.T) {
	// Given: nil metadata store
	engine := &MockSearchEngine{}
	cfg := config.NewConfig()

	// When: creating server
	srv, err := NewServer(engine, nil, &MockEmbedder{}, cfg, "")

	// Then: error returned
	require.Error(t, err)
	assert.Nil(t, srv)
	assert.Contains(t, err.Error(), "metadata")
}

func TestServer_New_NilConfig_UsesDefaults(t *testing.T) {
	// Given: nil config
	engine := &MockSearchEngine{}
	metadata := &MockMetadataStore{}

	// When: creating server with nil config
	srv, err := NewServer(engine, metadata, &MockEmbedder{}, nil, "")

	// Then: server created with defaults
	require.NoError(t, err)
	require.NotNil(t, srv)
}

// =============================================================================
// TS02: Initialize Handshake
// =============================================================================

func TestServer_Info_ReturnsCorrectValues(t *testing.T) {
	// Given: a server
	srv := newTestServer(t)

	// When: getting server info
	name, ver := srv.Info()

	// Then: returns correct name and version
	assert.Equal(t, "AmanMCP", name)
	assert.NotEmpty(t, ver)
}

func TestServer_Capabilities_HasToolsAndResources(t *testing.T) {
	// Given: a server
	srv := newTestServer(t)

	// When: checking capabilities
	hasTools, hasResources := srv.Capabilities()

	// Then: both are enabled
	assert.True(t, hasTools, "tools capability should be enabled")
	assert.True(t, hasResources, "resources capability should be enabled")
}

// =============================================================================
// TS03: Tools List
// =============================================================================

func TestServer_ListTools_ReturnsRegisteredTools(t *testing.T) {
	// Given: server with registered placeholder tools
	srv := newTestServer(t)

	// When: listing tools
	tools := srv.ListTools()

	// Then: at least one tool returned (placeholder search tool)
	assert.NotEmpty(t, tools)
	for _, tool := range tools {
		assert.NotEmpty(t, tool.Name)
		assert.NotEmpty(t, tool.Description)
	}
}

func TestServer_ListTools_SearchToolExists(t *testing.T) {
	// Given: server
	srv := newTestServer(t)

	// When: listing tools
	tools := srv.ListTools()

	// Then: search tool exists
	var found bool
	for _, tool := range tools {
		if tool.Name == "search" {
			found = true
			break
		}
	}
	assert.True(t, found, "search tool should be registered")
}

// =============================================================================
// TS04: Tool Call Routing
// =============================================================================

func TestServer_CallTool_SearchRouting(t *testing.T) {
	// Given: server with mock search returning results
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			return []*search.SearchResult{
				{
					Chunk: &store.Chunk{
						ID:       "chunk1",
						FilePath: "src/main.go",
						Content:  "func main() {}",
					},
					Score: 0.95,
				},
			}, nil
		},
	}
	metadata := &MockMetadataStore{}
	cfg := config.NewConfig()
	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)

	// When: calling search tool
	result, err := srv.CallTool(context.Background(), "search", map[string]any{
		"query": "main function",
	})

	// Then: returns results
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// TS05: Unknown Tool
// =============================================================================

func TestServer_CallTool_UnknownTool_ReturnsError(t *testing.T) {
	// Given: server
	srv := newTestServer(t)

	// When: calling non-existent tool
	_, err := srv.CallTool(context.Background(), "nonexistent_tool", nil)

	// Then: error with method not found
	require.Error(t, err)
	var mcpErr *MCPError
	if assert.ErrorAs(t, err, &mcpErr) {
		assert.Equal(t, ErrCodeMethodNotFound, mcpErr.Code)
	}
}

// =============================================================================
// TS06: Invalid Parameters
// =============================================================================

func TestServer_CallTool_InvalidParams_MissingQuery(t *testing.T) {
	// Given: server
	srv := newTestServer(t)

	// When: calling search without query parameter
	_, err := srv.CallTool(context.Background(), "search", map[string]any{})

	// Then: error with invalid params
	require.Error(t, err)
	var mcpErr *MCPError
	if assert.ErrorAs(t, err, &mcpErr) {
		assert.Equal(t, ErrCodeInvalidParams, mcpErr.Code)
	}
}

func TestServer_CallTool_InvalidParams_EmptyQuery(t *testing.T) {
	// Given: server
	srv := newTestServer(t)

	// When: calling search with empty query
	_, err := srv.CallTool(context.Background(), "search", map[string]any{
		"query": "",
	})

	// Then: error with invalid params
	require.Error(t, err)
	var mcpErr *MCPError
	if assert.ErrorAs(t, err, &mcpErr) {
		assert.Equal(t, ErrCodeInvalidParams, mcpErr.Code)
	}
}

// =============================================================================
// TS07: Resources List
// =============================================================================

func TestServer_ListResources_ReturnsIndexedFiles(t *testing.T) {
	// Given: server with mock files
	engine := &MockSearchEngine{}
	metadata := &MockMetadataStore{
		Files: []*store.File{
			{Path: "src/main.go", Language: "go"},
			{Path: "README.md", Language: "markdown"},
		},
	}
	cfg := config.NewConfig()
	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)

	// When: listing resources
	resources, cursor, err := srv.ListResources(context.Background(), "")

	// Then: files returned as resources
	require.NoError(t, err)
	assert.Empty(t, cursor) // No pagination for now
	assert.Len(t, resources, 2)

	// Verify resource structure
	for _, res := range resources {
		assert.NotEmpty(t, res.URI)
		assert.NotEmpty(t, res.Name)
	}
}

func TestServer_ListResources_Empty(t *testing.T) {
	// Given: server with no files
	srv := newTestServer(t)

	// When: listing resources
	resources, _, err := srv.ListResources(context.Background(), "")

	// Then: empty list returned
	require.NoError(t, err)
	assert.Empty(t, resources)
}

// =============================================================================
// TS08: Resource Read
// =============================================================================

func TestServer_ReadResource_ReturnsContent(t *testing.T) {
	// Given: server with indexed chunk
	engine := &MockSearchEngine{}
	metadata := &MockMetadataStore{
		Chunks: []*store.Chunk{
			{
				ID:       "chunk1",
				FilePath: "src/main.go",
				Content:  "package main\n\nfunc main() {}",
				Language: "go",
			},
		},
	}
	cfg := config.NewConfig()
	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)

	// When: reading resource
	result, err := srv.ReadResource(context.Background(), "chunk://chunk1")

	// Then: content returned
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.Content, "func main()")
}

func TestServer_ReadResource_NotFound(t *testing.T) {
	// Given: server
	srv := newTestServer(t)

	// When: reading non-existent resource
	_, err := srv.ReadResource(context.Background(), "chunk://nonexistent")

	// Then: error returned
	require.Error(t, err)
}

// =============================================================================
// TS09: Graceful Shutdown
// =============================================================================

func TestServer_Close_ReleasesResources(t *testing.T) {
	// Given: server
	srv := newTestServer(t)

	// When: closing server
	err := srv.Close()

	// Then: no error
	assert.NoError(t, err)
}

// =============================================================================
// TS10: Concurrent Requests
// =============================================================================

func TestServer_ConcurrentRequests_RaceSafe(t *testing.T) {
	// Given: server with mock search
	callCount := 0
	var mu sync.Mutex

	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			mu.Lock()
			callCount++
			mu.Unlock()
			time.Sleep(10 * time.Millisecond) // Simulate work
			return []*search.SearchResult{}, nil
		},
	}
	metadata := &MockMetadataStore{}
	cfg := config.NewConfig()
	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)

	// When: 10 concurrent requests
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := srv.CallTool(context.Background(), "search", map[string]any{
				"query": "test query",
			})
			assert.NoError(t, err)
		}(i)
	}

	// Then: all complete without race
	wg.Wait()
	assert.Equal(t, 10, callCount)
}
