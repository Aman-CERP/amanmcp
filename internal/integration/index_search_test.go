package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/embed"
	"github.com/Aman-CERP/amanmcp/internal/search"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

// Integration Tests - These test the full flow from indexing to search
// to verify components work together correctly.

// testEmbedder creates a static embedder for testing (fast, no model download)
func testEmbedder(t *testing.T) embed.Embedder {
	t.Helper()
	return embed.NewStaticEmbedder768()
}

// testMetadataStore creates an in-memory metadata store for testing
func testMetadataStore(t *testing.T) store.MetadataStore {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	ms, err := store.NewSQLiteStore(dbPath)
	require.NoError(t, err)

	t.Cleanup(func() { _ = ms.Close() })
	return ms
}

// testVectorStore creates a vector store for testing
func testVectorStore(t *testing.T) store.VectorStore {
	t.Helper()
	cfg := store.DefaultVectorStoreConfig(768) // Match static embedder dimensions
	vs, err := store.NewHNSWStore(cfg)
	require.NoError(t, err)

	t.Cleanup(func() { _ = vs.Close() })
	return vs
}

// testBM25Index creates a BM25 index for testing
func testBM25Index(t *testing.T) store.BM25Index {
	t.Helper()
	tmpDir := t.TempDir()
	indexBasePath := filepath.Join(tmpDir, "test")

	idx, err := store.NewBM25IndexWithBackend(indexBasePath, store.DefaultBM25Config(), "")
	require.NoError(t, err)

	t.Cleanup(func() { _ = idx.Close() })
	return idx
}

// TestIntegration_IndexAndSearch_FindsResults tests the complete flow:
// create files -> index -> search -> get results
func TestIntegration_IndexAndSearch_FindsResults(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Given: a project with some source files
	projectDir := t.TempDir()
	createTestProject(t, projectDir)

	// And: initialized stores
	embedder := testEmbedder(t)
	metadata := testMetadataStore(t)
	vector := testVectorStore(t)
	bm25 := testBM25Index(t)

	// Create search engine
	engine := search.New(
		bm25,
		vector,
		embedder,
		metadata,
		search.DefaultConfig(),
	)
	defer func() { _ = engine.Close() }()

	// Index the test files
	ctx := context.Background()
	files, chunks := createTestFilesAndChunks(t)

	// Save project first (required for files foreign key)
	err := metadata.SaveProject(ctx, testProject())
	require.NoError(t, err)

	// Save files (required for chunks foreign key)
	err = metadata.SaveFiles(ctx, files)
	require.NoError(t, err)

	// Then save chunks
	err = metadata.SaveChunks(ctx, chunks)
	require.NoError(t, err)

	// Index chunks in search engine
	err = engine.Index(ctx, chunks)
	require.NoError(t, err)

	// When: searching for known content
	results, err := engine.Search(ctx, "HTTP handler function", search.SearchOptions{
		Limit: 10,
	})

	// Then: results should be found
	require.NoError(t, err)
	assert.NotEmpty(t, results, "Search should find results")

	// Verify at least one result matches expected file
	foundHandler := false
	for _, r := range results {
		if r.Chunk != nil && r.Chunk.FilePath == "main.go" {
			foundHandler = true
			break
		}
	}
	assert.True(t, foundHandler, "Should find main.go with handler function")
}

// TestIntegration_SearchAfterDelete_ExcludesDeleted tests that deleted
// content is no longer returned in search results.
func TestIntegration_SearchAfterDelete_ExcludesDeleted(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Given: indexed content
	projectDir := t.TempDir()
	createTestProject(t, projectDir)

	embedder := testEmbedder(t)
	metadata := testMetadataStore(t)
	vector := testVectorStore(t)
	bm25 := testBM25Index(t)

	engine := search.New(bm25, vector, embedder, metadata, search.DefaultConfig())
	defer func() { _ = engine.Close() }()

	ctx := context.Background()
	files, chunks := createTestFilesAndChunks(t)
	err := metadata.SaveProject(ctx, testProject())
	require.NoError(t, err)
	err = metadata.SaveFiles(ctx, files)
	require.NoError(t, err)
	err = metadata.SaveChunks(ctx, chunks)
	require.NoError(t, err)
	err = engine.Index(ctx, chunks)
	require.NoError(t, err)

	// When: deleting a chunk and searching
	chunkToDelete := chunks[0].ID
	err = engine.Delete(ctx, []string{chunkToDelete})
	require.NoError(t, err)

	results, err := engine.Search(ctx, "HTTP handler", search.SearchOptions{Limit: 10})
	require.NoError(t, err)

	// Then: deleted chunk should not appear in results
	for _, r := range results {
		if r.Chunk != nil {
			assert.NotEqual(t, chunkToDelete, r.Chunk.ID, "Deleted chunk should not appear in results")
		}
	}
}

// TestIntegration_EmptyIndex_ReturnsNoResults tests that an empty index
// returns empty results without error.
func TestIntegration_EmptyIndex_ReturnsNoResults(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Given: an empty search engine
	embedder := testEmbedder(t)
	metadata := testMetadataStore(t)
	vector := testVectorStore(t)
	bm25 := testBM25Index(t)

	engine := search.New(bm25, vector, embedder, metadata, search.DefaultConfig())
	defer func() { _ = engine.Close() }()

	// When: searching empty index
	ctx := context.Background()
	results, err := engine.Search(ctx, "any query", search.SearchOptions{Limit: 10})

	// Then: no error, empty results
	require.NoError(t, err)
	assert.Empty(t, results)
}

// TestIntegration_SearchWithFilters_FiltersResults tests that search
// filters (language, type) work correctly.
func TestIntegration_SearchWithFilters_FiltersResults(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Given: indexed content with different languages
	projectDir := t.TempDir()
	createMultiLangProject(t, projectDir)

	embedder := testEmbedder(t)
	metadata := testMetadataStore(t)
	vector := testVectorStore(t)
	bm25 := testBM25Index(t)

	engine := search.New(bm25, vector, embedder, metadata, search.DefaultConfig())
	defer func() { _ = engine.Close() }()

	ctx := context.Background()
	files, chunks := createMultiLangFilesAndChunks(t)
	err := metadata.SaveProject(ctx, testProject())
	require.NoError(t, err)
	err = metadata.SaveFiles(ctx, files)
	require.NoError(t, err)
	err = metadata.SaveChunks(ctx, chunks)
	require.NoError(t, err)
	err = engine.Index(ctx, chunks)
	require.NoError(t, err)

	// When: searching with language filter
	results, err := engine.Search(ctx, "function", search.SearchOptions{
		Limit:    10,
		Language: "go",
	})
	require.NoError(t, err)

	// Then: only Go files should be in results
	for _, r := range results {
		if r.Chunk != nil && r.Chunk.FilePath != "" {
			ext := filepath.Ext(r.Chunk.FilePath)
			assert.Equal(t, ".go", ext, "Filtered results should only contain Go files")
		}
	}
}

// TestIntegration_ConcurrentSearches_NoRace tests that concurrent searches
// don't cause race conditions.
func TestIntegration_ConcurrentSearches_NoRace(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Given: indexed content
	projectDir := t.TempDir()
	createTestProject(t, projectDir)

	embedder := testEmbedder(t)
	metadata := testMetadataStore(t)
	vector := testVectorStore(t)
	bm25 := testBM25Index(t)

	engine := search.New(bm25, vector, embedder, metadata, search.DefaultConfig())
	defer func() { _ = engine.Close() }()

	ctx := context.Background()
	files, chunks := createTestFilesAndChunks(t)
	err := metadata.SaveProject(ctx, testProject())
	require.NoError(t, err)
	err = metadata.SaveFiles(ctx, files)
	require.NoError(t, err)
	err = metadata.SaveChunks(ctx, chunks)
	require.NoError(t, err)
	err = engine.Index(ctx, chunks)
	require.NoError(t, err)

	// When: running concurrent searches
	done := make(chan bool, 20)
	for i := 0; i < 20; i++ {
		go func(query string) {
			_, err := engine.Search(ctx, query, search.SearchOptions{Limit: 5})
			assert.NoError(t, err)
			done <- true
		}("test query " + string(rune('a'+i%26)))
	}

	// Then: all searches complete without error
	timeout := time.After(10 * time.Second)
	for i := 0; i < 20; i++ {
		select {
		case <-done:
		case <-timeout:
			t.Fatal("Concurrent searches timed out")
		}
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

// createTestProject creates a simple test project structure
func createTestProject(t *testing.T, dir string) {
	t.Helper()

	files := map[string]string{
		"main.go": `package main

import "net/http"

// handleRequest is the main HTTP handler function
func handleRequest(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("Hello, World!"))
}

func main() {
    http.HandleFunc("/", handleRequest)
    http.ListenAndServe(":8080", nil)
}
`,
		"util.go": `package main

// formatMessage formats a message with a prefix
func formatMessage(msg string) string {
    return "[APP] " + msg
}

// validateInput checks if input is valid
func validateInput(input string) bool {
    return len(input) > 0
}
`,
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		err := os.WriteFile(path, []byte(content), 0644)
		require.NoError(t, err)
	}
}

// createTestFilesAndChunks creates test files and chunks with proper relationships
func createTestFilesAndChunks(t *testing.T) ([]*store.File, []*store.Chunk) {
	t.Helper()
	now := time.Now()

	files := []*store.File{
		{
			ID:          "file-1",
			ProjectID:   "test-project", // No project FK enforcement in test
			Path:        "main.go",
			Size:        500,
			ModTime:     now,
			ContentHash: "hash1",
			Language:    "go",
			ContentType: "code",
			IndexedAt:   now,
		},
		{
			ID:          "file-2",
			ProjectID:   "test-project",
			Path:        "util.go",
			Size:        200,
			ModTime:     now,
			ContentHash: "hash2",
			Language:    "go",
			ContentType: "code",
			IndexedAt:   now,
		},
	}

	chunks := []*store.Chunk{
		{
			ID:          "chunk-1",
			FileID:      "file-1",
			FilePath:    "main.go",
			Content:     "package main\n\nimport \"net/http\"\n\n// handleRequest is the main HTTP handler function\nfunc handleRequest(w http.ResponseWriter, r *http.Request) {\n    w.Write([]byte(\"Hello, World!\"))\n}",
			StartLine:   1,
			EndLine:     8,
			Language:    "go",
			ContentType: store.ContentTypeCode,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "chunk-2",
			FileID:      "file-1",
			FilePath:    "main.go",
			Content:     "func main() {\n    http.HandleFunc(\"/\", handleRequest)\n    http.ListenAndServe(\":8080\", nil)\n}",
			StartLine:   10,
			EndLine:     13,
			Language:    "go",
			ContentType: store.ContentTypeCode,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "chunk-3",
			FileID:      "file-2",
			FilePath:    "util.go",
			Content:     "package main\n\n// formatMessage formats a message with a prefix\nfunc formatMessage(msg string) string {\n    return \"[APP] \" + msg\n}",
			StartLine:   1,
			EndLine:     6,
			Language:    "go",
			ContentType: store.ContentTypeCode,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}

	return files, chunks
}

// testProject creates a test project for foreign key constraints
func testProject() *store.Project {
	return &store.Project{
		ID:          "test-project",
		Name:        "test",
		RootPath:    "/tmp/test",
		ProjectType: "go",
	}
}

// createMultiLangProject creates a project with multiple languages
func createMultiLangProject(t *testing.T, dir string) {
	t.Helper()

	files := map[string]string{
		"main.go": `package main

func main() {
    println("Hello from Go")
}
`,
		"index.js": `// JavaScript function
function greet(name) {
    console.log("Hello, " + name);
}
`,
		"script.py": `# Python function
def greet(name):
    print(f"Hello, {name}")
`,
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		err := os.WriteFile(path, []byte(content), 0644)
		require.NoError(t, err)
	}
}

// createMultiLangFilesAndChunks creates files and chunks for multi-language project
func createMultiLangFilesAndChunks(t *testing.T) ([]*store.File, []*store.Chunk) {
	t.Helper()
	now := time.Now()

	files := []*store.File{
		{
			ID:          "file-go",
			ProjectID:   "test-project",
			Path:        "main.go",
			Size:        100,
			ModTime:     now,
			ContentHash: "hash-go",
			Language:    "go",
			ContentType: "code",
			IndexedAt:   now,
		},
		{
			ID:          "file-js",
			ProjectID:   "test-project",
			Path:        "index.js",
			Size:        100,
			ModTime:     now,
			ContentHash: "hash-js",
			Language:    "javascript",
			ContentType: "code",
			IndexedAt:   now,
		},
		{
			ID:          "file-py",
			ProjectID:   "test-project",
			Path:        "script.py",
			Size:        100,
			ModTime:     now,
			ContentHash: "hash-py",
			Language:    "python",
			ContentType: "code",
			IndexedAt:   now,
		},
	}

	chunks := []*store.Chunk{
		{
			ID:          "go-chunk",
			FileID:      "file-go",
			FilePath:    "main.go",
			Content:     "package main\n\nfunc main() {\n    println(\"Hello from Go\")\n}",
			StartLine:   1,
			EndLine:     5,
			Language:    "go",
			ContentType: store.ContentTypeCode,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "js-chunk",
			FileID:      "file-js",
			FilePath:    "index.js",
			Content:     "// JavaScript function\nfunction greet(name) {\n    console.log(\"Hello, \" + name);\n}",
			StartLine:   1,
			EndLine:     4,
			Language:    "javascript",
			ContentType: store.ContentTypeCode,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "py-chunk",
			FileID:      "file-py",
			FilePath:    "script.py",
			Content:     "# Python function\ndef greet(name):\n    print(f\"Hello, {name}\")",
			StartLine:   1,
			EndLine:     3,
			Language:    "python",
			ContentType: store.ContentTypeCode,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}

	return files, chunks
}

// =============================================================================
// Config Integration Tests
// =============================================================================

// TestIntegration_ConfigLoad_AppliesDefaults tests that config loading
// works end-to-end with defaults.
func TestIntegration_ConfigLoad_AppliesDefaults(t *testing.T) {
	// Given: a directory without config file
	tmpDir := t.TempDir()

	// When: loading config
	cfg, err := config.Load(tmpDir)

	// Then: defaults are applied (empty provider = auto-detect: MLX -> Ollama -> Static)
	require.NoError(t, err)
	assert.Equal(t, 0.65, cfg.Search.BM25Weight)  // RCA-015: BM25 favored
	assert.Equal(t, 0.35, cfg.Search.SemanticWeight)
	assert.Equal(t, "", cfg.Embeddings.Provider) // Empty = auto-detect
}

// TestIntegration_ConfigLoad_WithFile_OverridesDefaults tests that
// config file values override defaults for YAML-accessible fields.
// Note: Search weights are internal-only (yaml:"-") - use env vars instead.
func TestIntegration_ConfigLoad_WithFile_OverridesDefaults(t *testing.T) {
	// Given: a directory with config file
	tmpDir := t.TempDir()
	configContent := `
version: 1
search:
  chunk_size: 2000
embeddings:
  provider: static
`
	err := os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yaml"), []byte(configContent), 0644)
	require.NoError(t, err)

	// When: loading config
	cfg, err := config.Load(tmpDir)

	// Then: file values override defaults for YAML-accessible fields
	require.NoError(t, err)
	assert.Equal(t, 2000, cfg.Search.ChunkSize)
	assert.Equal(t, "static", cfg.Embeddings.Provider)
	// Weights use defaults (not overridable via YAML - RCA-015)
	assert.Equal(t, 0.65, cfg.Search.BM25Weight)
	assert.Equal(t, 0.35, cfg.Search.SemanticWeight)
}
