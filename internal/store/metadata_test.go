package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to create a test store with cleanup
func newTestStore(t *testing.T) (*SQLiteStore, string) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".amanmcp", "metadata.db")

	store, err := NewSQLiteStore(dbPath)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = store.Close()
	})

	return store, tmpDir
}

// TS01: Project CRUD
func TestSQLiteStore_ProjectCRUD(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Given: a new project
	project := &Project{
		ID:          "proj-123",
		Name:        "test-project",
		RootPath:    "/path/to/project",
		ProjectType: "go",
		ChunkCount:  0,
		FileCount:   0,
		IndexedAt:   time.Now(),
		Version:     "1.0.0",
	}

	// When: I save the project
	err := store.SaveProject(ctx, project)
	require.NoError(t, err)

	// Then: I can retrieve it by ID
	retrieved, err := store.GetProject(ctx, "proj-123")
	require.NoError(t, err)
	assert.Equal(t, project.ID, retrieved.ID)
	assert.Equal(t, project.Name, retrieved.Name)
	assert.Equal(t, project.RootPath, retrieved.RootPath)
	assert.Equal(t, project.ProjectType, retrieved.ProjectType)

	// And: updating stats updates the record
	err = store.UpdateProjectStats(ctx, "proj-123", 10, 100)
	require.NoError(t, err)

	updated, err := store.GetProject(ctx, "proj-123")
	require.NoError(t, err)
	assert.Equal(t, 10, updated.FileCount)
	assert.Equal(t, 100, updated.ChunkCount)
}

func TestSQLiteStore_GetProject_NotFound(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// When: getting a non-existent project
	project, err := store.GetProject(ctx, "non-existent")

	// Then: nil is returned without error
	assert.NoError(t, err)
	assert.Nil(t, project)
}

// TestSQLiteStore_RefreshProjectStats tests that RefreshProjectStats correctly
// counts files and chunks from the database and updates indexed_at.
func TestSQLiteStore_RefreshProjectStats(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Given: a project with files and chunks
	project := &Project{
		ID:       "proj-refresh",
		Name:     "refresh-test",
		RootPath: "/path/to/project",
	}
	require.NoError(t, store.SaveProject(ctx, project))

	// Add some files
	files := []*File{
		{ID: "file-1", ProjectID: "proj-refresh", Path: "file1.go", Language: "go"},
		{ID: "file-2", ProjectID: "proj-refresh", Path: "file2.go", Language: "go"},
		{ID: "file-3", ProjectID: "proj-refresh", Path: "file3.md", Language: "markdown"},
	}
	require.NoError(t, store.SaveFiles(ctx, files))

	// Add some chunks
	chunks := []*Chunk{
		{ID: "chunk-1", FileID: "file-1", Content: "content 1"},
		{ID: "chunk-2", FileID: "file-1", Content: "content 2"},
		{ID: "chunk-3", FileID: "file-2", Content: "content 3"},
		{ID: "chunk-4", FileID: "file-3", Content: "content 4"},
		{ID: "chunk-5", FileID: "file-3", Content: "content 5"},
	}
	require.NoError(t, store.SaveChunks(ctx, chunks))

	// When: refreshing project stats
	err := store.RefreshProjectStats(ctx, "proj-refresh")
	require.NoError(t, err)

	// Then: counts are correctly updated
	updated, err := store.GetProject(ctx, "proj-refresh")
	require.NoError(t, err)
	assert.Equal(t, 3, updated.FileCount, "should count 3 files")
	assert.Equal(t, 5, updated.ChunkCount, "should count 5 chunks")
	assert.False(t, updated.IndexedAt.IsZero(), "indexed_at should be set")
}

// TS02: File Tracking
func TestSQLiteStore_FileTracking(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Given: a project exists
	project := &Project{
		ID:       "proj-456",
		Name:     "file-test",
		RootPath: "/path/to/project",
	}
	require.NoError(t, store.SaveProject(ctx, project))

	// And: files are saved with different timestamps
	baseTime := time.Now().Add(-1 * time.Hour)
	files := []*File{
		{
			ID:          "file-1",
			ProjectID:   "proj-456",
			Path:        "src/main.go",
			Size:        1024,
			ModTime:     baseTime,
			ContentHash: "hash1",
			Language:    "go",
			ContentType: "code",
			IndexedAt:   baseTime,
		},
		{
			ID:          "file-2",
			ProjectID:   "proj-456",
			Path:        "src/util.go",
			Size:        512,
			ModTime:     baseTime.Add(30 * time.Minute),
			ContentHash: "hash2",
			Language:    "go",
			ContentType: "code",
			IndexedAt:   baseTime.Add(30 * time.Minute),
		},
		{
			ID:          "file-3",
			ProjectID:   "proj-456",
			Path:        "README.md",
			Size:        256,
			ModTime:     baseTime.Add(45 * time.Minute),
			ContentHash: "hash3",
			Language:    "markdown",
			ContentType: "markdown",
			IndexedAt:   baseTime.Add(45 * time.Minute),
		},
	}
	require.NoError(t, store.SaveFiles(ctx, files))

	// When: querying for files changed since 20 minutes after base
	since := baseTime.Add(20 * time.Minute)
	changed, err := store.GetChangedFiles(ctx, "proj-456", since)
	require.NoError(t, err)

	// Then: only files modified after that time are returned
	assert.Len(t, changed, 2)
	paths := make([]string, len(changed))
	for i, f := range changed {
		paths[i] = f.Path
	}
	assert.Contains(t, paths, "src/util.go")
	assert.Contains(t, paths, "README.md")
}

func TestSQLiteStore_GetFileByPath(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Given: a project with a file
	project := &Project{ID: "proj-path", Name: "path-test", RootPath: "/test"}
	require.NoError(t, store.SaveProject(ctx, project))

	file := &File{
		ID:          "file-path-1",
		ProjectID:   "proj-path",
		Path:        "internal/config/config.go",
		Size:        2048,
		ModTime:     time.Now(),
		ContentHash: "abc123",
		Language:    "go",
		ContentType: "code",
		IndexedAt:   time.Now(),
	}
	require.NoError(t, store.SaveFiles(ctx, []*File{file}))

	// When: I get file by path
	retrieved, err := store.GetFileByPath(ctx, "proj-path", "internal/config/config.go")

	// Then: the file is returned
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	assert.Equal(t, "file-path-1", retrieved.ID)
	assert.Equal(t, "internal/config/config.go", retrieved.Path)
}

// TS03: Batch Insert Performance
func TestSQLiteStore_BatchInsertPerformance(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Given: a project
	project := &Project{ID: "proj-perf", Name: "perf-test", RootPath: "/perf"}
	require.NoError(t, store.SaveProject(ctx, project))

	// And: a file
	file := &File{
		ID:          "file-perf",
		ProjectID:   "proj-perf",
		Path:        "main.go",
		Size:        10000,
		ModTime:     time.Now(),
		ContentHash: "perfhash",
		Language:    "go",
		ContentType: "code",
		IndexedAt:   time.Now(),
	}
	require.NoError(t, store.SaveFiles(ctx, []*File{file}))

	// And: 1000 chunks to insert
	chunks := make([]*Chunk, 1000)
	for i := 0; i < 1000; i++ {
		chunks[i] = &Chunk{
			ID:          "chunk-" + string(rune(i)),
			FileID:      "file-perf",
			FilePath:    "main.go",
			Content:     "func example() { return }",
			ContentType: ContentTypeCode,
			Language:    "go",
			StartLine:   i*10 + 1,
			EndLine:     i*10 + 10,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
	}

	// When: using SaveChunks batch operation
	start := time.Now()
	err := store.SaveChunks(ctx, chunks)
	elapsed := time.Since(start)

	// Then: insert completes without error
	require.NoError(t, err)

	// And: completes in < 100ms (spec target)
	assert.Less(t, elapsed.Milliseconds(), int64(100),
		"batch insert of 1000 chunks took %v, expected < 100ms", elapsed)
}

// TS04: Symbol Search
func TestSQLiteStore_SymbolSearch(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Given: a project and file
	project := &Project{ID: "proj-sym", Name: "symbol-test", RootPath: "/symbols"}
	require.NoError(t, store.SaveProject(ctx, project))

	file := &File{
		ID:          "file-sym",
		ProjectID:   "proj-sym",
		Path:        "handlers.go",
		Size:        5000,
		ModTime:     time.Now(),
		ContentHash: "symhash",
		Language:    "go",
		ContentType: "code",
		IndexedAt:   time.Now(),
	}
	require.NoError(t, store.SaveFiles(ctx, []*File{file}))

	// And: chunks with symbols
	chunks := []*Chunk{
		{
			ID:          "chunk-sym-1",
			FileID:      "file-sym",
			FilePath:    "handlers.go",
			Content:     "func HandleLogin() {}",
			ContentType: ContentTypeCode,
			Language:    "go",
			StartLine:   1,
			EndLine:     10,
			Symbols: []*Symbol{
				{Name: "HandleLogin", Type: SymbolTypeFunction, StartLine: 1, EndLine: 10, Signature: "func HandleLogin()"},
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		{
			ID:          "chunk-sym-2",
			FileID:      "file-sym",
			FilePath:    "handlers.go",
			Content:     "func HandleLogout() {}",
			ContentType: ContentTypeCode,
			Language:    "go",
			StartLine:   12,
			EndLine:     20,
			Symbols: []*Symbol{
				{Name: "HandleLogout", Type: SymbolTypeFunction, StartLine: 12, EndLine: 20, Signature: "func HandleLogout()"},
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		{
			ID:          "chunk-sym-3",
			FileID:      "file-sym",
			FilePath:    "handlers.go",
			Content:     "type UserService struct {}",
			ContentType: ContentTypeCode,
			Language:    "go",
			StartLine:   22,
			EndLine:     30,
			Symbols: []*Symbol{
				{Name: "UserService", Type: SymbolTypeType, StartLine: 22, EndLine: 30},
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	require.NoError(t, store.SaveChunks(ctx, chunks))

	// When: searching for symbols containing "Handle"
	results, err := store.SearchSymbols(ctx, "Handle", 10)

	// Then: matching symbols are returned
	require.NoError(t, err)
	assert.Len(t, results, 2)

	names := make([]string, len(results))
	for i, s := range results {
		names[i] = s.Name
	}
	assert.Contains(t, names, "HandleLogin")
	assert.Contains(t, names, "HandleLogout")
}

// TS05: Cascading Delete
func TestSQLiteStore_CascadingDelete(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Given: a project with files and chunks
	project := &Project{ID: "proj-del", Name: "delete-test", RootPath: "/delete"}
	require.NoError(t, store.SaveProject(ctx, project))

	files := []*File{
		{ID: "file-del-1", ProjectID: "proj-del", Path: "a.go", ModTime: time.Now(), IndexedAt: time.Now()},
		{ID: "file-del-2", ProjectID: "proj-del", Path: "b.go", ModTime: time.Now(), IndexedAt: time.Now()},
	}
	require.NoError(t, store.SaveFiles(ctx, files))

	chunks := []*Chunk{
		{ID: "chunk-del-1", FileID: "file-del-1", FilePath: "a.go", Content: "a", ContentType: ContentTypeCode, StartLine: 1, EndLine: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "chunk-del-2", FileID: "file-del-1", FilePath: "a.go", Content: "b", ContentType: ContentTypeCode, StartLine: 2, EndLine: 2, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "chunk-del-3", FileID: "file-del-2", FilePath: "b.go", Content: "c", ContentType: ContentTypeCode, StartLine: 1, EndLine: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}
	require.NoError(t, store.SaveChunks(ctx, chunks))

	// When: I delete files by project
	err := store.DeleteFilesByProject(ctx, "proj-del")
	require.NoError(t, err)

	// Then: files are deleted
	file1, err := store.GetFileByPath(ctx, "proj-del", "a.go")
	require.NoError(t, err)
	assert.Nil(t, file1)

	// And: associated chunks are deleted
	chunks1, err := store.GetChunksByFile(ctx, "file-del-1")
	require.NoError(t, err)
	assert.Empty(t, chunks1)
}

// TS06: Schema Auto-Creation
func TestSQLiteStore_SchemaAutoCreation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".amanmcp", "metadata.db")

	// Given: an empty database directory (db file doesn't exist)
	_, err := os.Stat(dbPath)
	assert.True(t, os.IsNotExist(err))

	// When: I open the store
	store, err := NewSQLiteStore(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Then: the database file is created
	_, err = os.Stat(dbPath)
	assert.NoError(t, err)

	// And: all tables are created automatically (we can use them)
	ctx := context.Background()
	project := &Project{ID: "auto-test", Name: "auto", RootPath: "/auto"}
	err = store.SaveProject(ctx, project)
	assert.NoError(t, err)

	retrieved, err := store.GetProject(ctx, "auto-test")
	assert.NoError(t, err)
	assert.NotNil(t, retrieved)
}

// TS07: Concurrent Reads
func TestSQLiteStore_ConcurrentReads(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Given: an indexed project with data
	project := &Project{ID: "proj-conc", Name: "concurrent-test", RootPath: "/concurrent"}
	require.NoError(t, store.SaveProject(ctx, project))

	files := make([]*File, 100)
	for i := 0; i < 100; i++ {
		files[i] = &File{
			ID:          "file-conc-" + string(rune(i)),
			ProjectID:   "proj-conc",
			Path:        "file" + string(rune(i)) + ".go",
			Size:        int64(i * 100),
			ModTime:     time.Now(),
			ContentHash: "hash",
			Language:    "go",
			ContentType: "code",
			IndexedAt:   time.Now(),
		}
	}
	require.NoError(t, store.SaveFiles(ctx, files))

	// When: multiple goroutines read concurrently
	var wg sync.WaitGroup
	errChan := make(chan error, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Read project
			_, err := store.GetProject(ctx, "proj-conc")
			if err != nil {
				errChan <- err
				return
			}
			// Read files
			_, err = store.GetChangedFiles(ctx, "proj-conc", time.Time{})
			if err != nil {
				errChan <- err
			}
		}()
	}

	wg.Wait()
	close(errChan)

	// Then: no errors occur (WAL mode enables concurrent reads)
	for err := range errChan {
		t.Errorf("concurrent read error: %v", err)
	}
}

// Additional tests for chunk operations
func TestSQLiteStore_ChunkOperations(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Setup
	project := &Project{ID: "proj-chunk", Name: "chunk-test", RootPath: "/chunk"}
	require.NoError(t, store.SaveProject(ctx, project))

	file := &File{
		ID:          "file-chunk",
		ProjectID:   "proj-chunk",
		Path:        "main.go",
		Size:        1000,
		ModTime:     time.Now(),
		ContentHash: "chunkhash",
		Language:    "go",
		ContentType: "code",
		IndexedAt:   time.Now(),
	}
	require.NoError(t, store.SaveFiles(ctx, []*File{file}))

	// Test saving and retrieving chunks
	chunk := &Chunk{
		ID:          "chunk-test-1",
		FileID:      "file-chunk",
		FilePath:    "main.go",
		Content:     "func main() { fmt.Println(\"Hello\") }",
		RawContent:  "func main() { fmt.Println(\"Hello\") }",
		Context:     "package main\n\nimport \"fmt\"",
		ContentType: ContentTypeCode,
		Language:    "go",
		StartLine:   5,
		EndLine:     7,
		Symbols: []*Symbol{
			{Name: "main", Type: SymbolTypeFunction, StartLine: 5, EndLine: 7, Signature: "func main()"},
		},
		Metadata:  map[string]string{"key": "value"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, store.SaveChunks(ctx, []*Chunk{chunk}))

	// GetChunk
	retrieved, err := store.GetChunk(ctx, "chunk-test-1")
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	assert.Equal(t, chunk.ID, retrieved.ID)
	assert.Equal(t, chunk.Content, retrieved.Content)
	assert.Equal(t, chunk.RawContent, retrieved.RawContent)
	assert.Equal(t, chunk.Context, retrieved.Context)
	assert.Equal(t, chunk.Language, retrieved.Language)
	assert.Equal(t, chunk.StartLine, retrieved.StartLine)
	assert.Equal(t, chunk.EndLine, retrieved.EndLine)

	// GetChunksByFile
	chunks, err := store.GetChunksByFile(ctx, "file-chunk")
	require.NoError(t, err)
	assert.Len(t, chunks, 1)
	assert.Equal(t, "chunk-test-1", chunks[0].ID)

	// DeleteChunksByFile
	err = store.DeleteChunksByFile(ctx, "file-chunk")
	require.NoError(t, err)

	chunks, err = store.GetChunksByFile(ctx, "file-chunk")
	require.NoError(t, err)
	assert.Empty(t, chunks)
}

func TestSQLiteStore_GetChunk_NotFound(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	chunk, err := store.GetChunk(ctx, "non-existent")
	assert.NoError(t, err)
	assert.Nil(t, chunk)
}

// Test file upsert behavior (update if exists)
func TestSQLiteStore_FileUpsert(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	project := &Project{ID: "proj-upsert", Name: "upsert-test", RootPath: "/upsert"}
	require.NoError(t, store.SaveProject(ctx, project))

	// Save file first time
	file := &File{
		ID:          "file-upsert-1",
		ProjectID:   "proj-upsert",
		Path:        "config.go",
		Size:        100,
		ModTime:     time.Now(),
		ContentHash: "hash-v1",
		Language:    "go",
		ContentType: "code",
		IndexedAt:   time.Now(),
	}
	require.NoError(t, store.SaveFiles(ctx, []*File{file}))

	// Save again with updated hash
	file.ContentHash = "hash-v2"
	file.Size = 200
	require.NoError(t, store.SaveFiles(ctx, []*File{file}))

	// Verify update
	retrieved, err := store.GetFileByPath(ctx, "proj-upsert", "config.go")
	require.NoError(t, err)
	assert.Equal(t, "hash-v2", retrieved.ContentHash)
	assert.Equal(t, int64(200), retrieved.Size)
}

// Test project upsert behavior
func TestSQLiteStore_ProjectUpsert(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Save project first time
	project := &Project{
		ID:          "proj-upsert-2",
		Name:        "upsert-test",
		RootPath:    "/upsert",
		ProjectType: "go",
	}
	require.NoError(t, store.SaveProject(ctx, project))

	// Save again with updated values
	project.Name = "updated-name"
	project.ProjectType = "python"
	require.NoError(t, store.SaveProject(ctx, project))

	// Verify update
	retrieved, err := store.GetProject(ctx, "proj-upsert-2")
	require.NoError(t, err)
	assert.Equal(t, "updated-name", retrieved.Name)
	assert.Equal(t, "python", retrieved.ProjectType)
}

// TS08: ListFiles - F18 MCP Resources
func TestSQLiteStore_ListFiles(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Given: a project with files
	project := &Project{ID: "proj-list", Name: "list-test", RootPath: "/list"}
	require.NoError(t, store.SaveProject(ctx, project))

	baseTime := time.Now()
	files := []*File{
		{ID: "file-list-1", ProjectID: "proj-list", Path: "src/main.go", Size: 1024, ModTime: baseTime, Language: "go", ContentType: "code", IndexedAt: baseTime},
		{ID: "file-list-2", ProjectID: "proj-list", Path: "src/util.go", Size: 512, ModTime: baseTime, Language: "go", ContentType: "code", IndexedAt: baseTime},
		{ID: "file-list-3", ProjectID: "proj-list", Path: "README.md", Size: 256, ModTime: baseTime, Language: "markdown", ContentType: "markdown", IndexedAt: baseTime},
	}
	require.NoError(t, store.SaveFiles(ctx, files))

	// When: listing files without cursor
	result, nextCursor, err := store.ListFiles(ctx, "proj-list", "", 100)

	// Then: all files are returned
	require.NoError(t, err)
	assert.Len(t, result, 3)
	assert.Empty(t, nextCursor) // No more pages

	// And: files have expected fields
	paths := make([]string, len(result))
	for i, f := range result {
		paths[i] = f.Path
	}
	assert.Contains(t, paths, "src/main.go")
	assert.Contains(t, paths, "src/util.go")
	assert.Contains(t, paths, "README.md")
}

func TestSQLiteStore_ListFiles_Pagination(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Given: a project with 5 files
	project := &Project{ID: "proj-page", Name: "page-test", RootPath: "/page"}
	require.NoError(t, store.SaveProject(ctx, project))

	baseTime := time.Now()
	files := make([]*File, 5)
	for i := 0; i < 5; i++ {
		files[i] = &File{
			ID:          fmt.Sprintf("file-page-%d", i),
			ProjectID:   "proj-page",
			Path:        fmt.Sprintf("file%d.go", i),
			Size:        int64(i * 100),
			ModTime:     baseTime,
			Language:    "go",
			ContentType: "code",
			IndexedAt:   baseTime,
		}
	}
	require.NoError(t, store.SaveFiles(ctx, files))

	// When: listing with limit 2
	page1, cursor1, err := store.ListFiles(ctx, "proj-page", "", 2)
	require.NoError(t, err)
	assert.Len(t, page1, 2)
	assert.NotEmpty(t, cursor1) // More pages available

	// And: requesting next page
	page2, cursor2, err := store.ListFiles(ctx, "proj-page", cursor1, 2)
	require.NoError(t, err)
	assert.Len(t, page2, 2)
	assert.NotEmpty(t, cursor2)

	// And: requesting final page
	page3, cursor3, err := store.ListFiles(ctx, "proj-page", cursor2, 2)
	require.NoError(t, err)
	assert.Len(t, page3, 1) // Only 1 file left
	assert.Empty(t, cursor3) // No more pages

	// Then: all files were returned across pages
	allPaths := make(map[string]bool)
	for _, f := range page1 {
		allPaths[f.Path] = true
	}
	for _, f := range page2 {
		allPaths[f.Path] = true
	}
	for _, f := range page3 {
		allPaths[f.Path] = true
	}
	assert.Len(t, allPaths, 5)
}

func TestSQLiteStore_ListFiles_Empty(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Given: a project with no files
	project := &Project{ID: "proj-empty", Name: "empty-test", RootPath: "/empty"}
	require.NoError(t, store.SaveProject(ctx, project))

	// When: listing files
	result, nextCursor, err := store.ListFiles(ctx, "proj-empty", "", 100)

	// Then: empty list is returned
	require.NoError(t, err)
	assert.Empty(t, result)
	assert.Empty(t, nextCursor)
}

func TestSQLiteStore_ListFiles_InvalidCursor(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Given: a project
	project := &Project{ID: "proj-invalid", Name: "invalid-test", RootPath: "/invalid"}
	require.NoError(t, store.SaveProject(ctx, project))

	// When: listing with invalid cursor
	_, _, err := store.ListFiles(ctx, "proj-invalid", "invalid-cursor", 100)

	// Then: error is returned
	assert.Error(t, err)
}

func TestSQLiteStore_ListFiles_NegativeCursor(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Given: a project with files
	project := &Project{ID: "proj-neg", Name: "negative-test", RootPath: "/negative"}
	require.NoError(t, store.SaveProject(ctx, project))

	baseTime := time.Now()
	files := []*File{
		{ID: "file-neg-1", ProjectID: "proj-neg", Path: "file1.go", Size: 100, ModTime: baseTime, Language: "go", ContentType: "code", IndexedAt: baseTime},
	}
	require.NoError(t, store.SaveFiles(ctx, files))

	// When: listing with a negative offset cursor (base64 encoded "offset:-5")
	// "offset:-5" base64 encoded is "b2Zmc2V0Oi01"
	negativeCursor := "b2Zmc2V0Oi01"
	_, _, err := store.ListFiles(ctx, "proj-neg", negativeCursor, 100)

	// Then: error is returned indicating negative offset is not allowed
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-negative")
}

// TS09: GetFilePathsByProject - for gitignore sync
func TestSQLiteStore_GetFilePathsByProject(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Given: a project with files
	project := &Project{ID: "proj-paths", Name: "paths-test", RootPath: "/paths"}
	require.NoError(t, store.SaveProject(ctx, project))

	baseTime := time.Now()
	files := []*File{
		{ID: "file-paths-1", ProjectID: "proj-paths", Path: "src/main.go", Size: 1024, ModTime: baseTime, Language: "go", ContentType: "code", IndexedAt: baseTime},
		{ID: "file-paths-2", ProjectID: "proj-paths", Path: "src/util.go", Size: 512, ModTime: baseTime, Language: "go", ContentType: "code", IndexedAt: baseTime},
		{ID: "file-paths-3", ProjectID: "proj-paths", Path: "README.md", Size: 256, ModTime: baseTime, Language: "markdown", ContentType: "markdown", IndexedAt: baseTime},
	}
	require.NoError(t, store.SaveFiles(ctx, files))

	// When: getting file paths
	paths, err := store.GetFilePathsByProject(ctx, "proj-paths")

	// Then: all paths are returned
	require.NoError(t, err)
	assert.Len(t, paths, 3)
	assert.Contains(t, paths, "src/main.go")
	assert.Contains(t, paths, "src/util.go")
	assert.Contains(t, paths, "README.md")
}

func TestSQLiteStore_GetFilePathsByProject_Empty(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Given: a project with no files
	project := &Project{ID: "proj-paths-empty", Name: "empty-test", RootPath: "/empty"}
	require.NoError(t, store.SaveProject(ctx, project))

	// When: getting file paths
	paths, err := store.GetFilePathsByProject(ctx, "proj-paths-empty")

	// Then: empty slice is returned
	require.NoError(t, err)
	assert.Empty(t, paths)
}

func TestSQLiteStore_GetFilePathsByProject_NonExistent(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// When: getting file paths for non-existent project
	paths, err := store.GetFilePathsByProject(ctx, "non-existent-project")

	// Then: empty slice is returned without error
	require.NoError(t, err)
	assert.Empty(t, paths)
}


// Test State Operations (key-value store)
func TestSQLiteStore_State_SetAndGet(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// When: setting a state value
	err := store.SetState(ctx, "test_key", "test_value")
	require.NoError(t, err)

	// Then: it can be retrieved
	value, err := store.GetState(ctx, "test_key")
	require.NoError(t, err)
	assert.Equal(t, "test_value", value)
}

func TestSQLiteStore_State_GetNonExistent(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// When: getting a non-existent key
	value, err := store.GetState(ctx, "non_existent_key")

	// Then: empty string returned without error
	require.NoError(t, err)
	assert.Equal(t, "", value)
}

func TestSQLiteStore_State_Upsert(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Given: a key with initial value
	err := store.SetState(ctx, "upsert_key", "initial_value")
	require.NoError(t, err)

	// When: setting the same key with new value
	err = store.SetState(ctx, "upsert_key", "updated_value")
	require.NoError(t, err)

	// Then: the value is updated
	value, err := store.GetState(ctx, "upsert_key")
	require.NoError(t, err)
	assert.Equal(t, "updated_value", value)
}

func TestSQLiteStore_State_EmptyValue(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// When: setting an empty value
	err := store.SetState(ctx, "empty_key", "")
	require.NoError(t, err)

	// Then: empty string is retrieved
	value, err := store.GetState(ctx, "empty_key")
	require.NoError(t, err)
	assert.Equal(t, "", value)
}

func TestSQLiteStore_State_MultipleKeys(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Given: multiple keys are set
	keys := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"gitignore_hash": "abc123",
	}
	for k, v := range keys {
		require.NoError(t, store.SetState(ctx, k, v))
	}

	// Then: each key returns its value
	for k, expected := range keys {
		value, err := store.GetState(ctx, k)
		require.NoError(t, err)
		assert.Equal(t, expected, value, "key %q should have value %q", k, expected)
	}
}

// DEBT-011: Configurable Cache Size
func TestSQLiteStore_DefaultCacheSize(t *testing.T) {
	// When: using default constructor
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".amanmcp", "metadata.db")

	store, err := NewSQLiteStore(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Then: store is created successfully (with default 64MB cache)
	ctx := context.Background()
	project := &Project{ID: "cache-test", Name: "cache-test", RootPath: "/cache"}
	err = store.SaveProject(ctx, project)
	assert.NoError(t, err)
}

func TestSQLiteStore_ConfigurableCacheSize(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".amanmcp", "metadata.db")

	// When: using configurable constructor with custom cache size
	cfg := StoreConfig{CacheSizeMB: 32} // 32MB instead of default 64MB
	store, err := NewSQLiteStoreWithConfig(dbPath, cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Then: store is created successfully
	ctx := context.Background()
	project := &Project{ID: "cache-test-2", Name: "cache-test-2", RootPath: "/cache2"}
	err = store.SaveProject(ctx, project)
	assert.NoError(t, err)
}

func TestSQLiteStore_DefaultStoreConfig(t *testing.T) {
	// When: getting default config
	cfg := DefaultStoreConfig()

	// Then: default cache size is 64MB
	assert.Equal(t, 64, cfg.CacheSizeMB)
}

func TestSQLiteStore_ZeroCacheSize_UsesDefault(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, ".amanmcp", "metadata.db")

	// When: using config with zero cache size
	cfg := StoreConfig{CacheSizeMB: 0}
	store, err := NewSQLiteStoreWithConfig(dbPath, cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Then: store is created (should use default)
	ctx := context.Background()
	project := &Project{ID: "cache-test-3", Name: "cache-test-3", RootPath: "/cache3"}
	err = store.SaveProject(ctx, project)
	assert.NoError(t, err)
}

// --- Embedding Storage Tests ---

func TestEmbeddingBytesConversion(t *testing.T) {
	// Given: a float32 embedding
	original := []float32{0.1, 0.2, 0.3, -0.5, 1.0, 0.0}

	// When: converted to bytes and back
	bytes := embeddingToBytes(original)
	result := bytesToEmbedding(bytes)

	// Then: values match
	require.Len(t, result, len(original))
	for i, v := range original {
		assert.InDelta(t, v, result[i], 0.0001, "mismatch at index %d", i)
	}
}

func TestEmbeddingBytesConversion_EmptyInput(t *testing.T) {
	// Given: empty inputs
	// When: converting empty slice
	bytes := embeddingToBytes([]float32{})
	assert.Empty(t, bytes)

	// When: converting nil bytes
	result := bytesToEmbedding(nil)
	assert.Nil(t, result)

	// When: converting empty bytes
	result = bytesToEmbedding([]byte{})
	assert.Nil(t, result)
}

func TestSaveChunkEmbeddings_Roundtrip(t *testing.T) {
	store, tmpDir := newTestStore(t)
	ctx := context.Background()

	// Given: a project and file
	project := &Project{ID: "emb-proj", Name: "embedding-test", RootPath: tmpDir}
	require.NoError(t, store.SaveProject(ctx, project))

	file := &File{ID: "emb-file", ProjectID: "emb-proj", Path: "test.go"}
	require.NoError(t, store.SaveFiles(ctx, []*File{file}))

	// And: some chunks
	chunks := []*Chunk{
		{ID: "chunk-1", FileID: "emb-file", FilePath: "test.go", Content: "func foo()", StartLine: 1, EndLine: 5},
		{ID: "chunk-2", FileID: "emb-file", FilePath: "test.go", Content: "func bar()", StartLine: 6, EndLine: 10},
	}
	require.NoError(t, store.SaveChunks(ctx, chunks))

	// When: saving embeddings
	embeddings := [][]float32{
		{0.1, 0.2, 0.3, 0.4},
		{0.5, 0.6, 0.7, 0.8},
	}
	chunkIDs := []string{"chunk-1", "chunk-2"}

	err := store.SaveChunkEmbeddings(ctx, chunkIDs, embeddings, "test-model")
	require.NoError(t, err)

	// Then: embeddings can be retrieved
	allEmbs, err := store.GetAllEmbeddings(ctx)
	require.NoError(t, err)
	assert.Len(t, allEmbs, 2)

	// Verify values
	for i, id := range chunkIDs {
		retrieved := allEmbs[id]
		require.NotNil(t, retrieved, "embedding for %s not found", id)
		for j, v := range embeddings[i] {
			assert.InDelta(t, v, retrieved[j], 0.0001)
		}
	}
}

func TestGetAllEmbeddings_SkipsNullEmbeddings(t *testing.T) {
	store, tmpDir := newTestStore(t)
	ctx := context.Background()

	// Given: a project, file, and chunks
	project := &Project{ID: "null-emb-proj", Name: "null-test", RootPath: tmpDir}
	require.NoError(t, store.SaveProject(ctx, project))

	file := &File{ID: "null-emb-file", ProjectID: "null-emb-proj", Path: "test.go"}
	require.NoError(t, store.SaveFiles(ctx, []*File{file}))

	chunks := []*Chunk{
		{ID: "has-emb", FileID: "null-emb-file", FilePath: "test.go", Content: "func foo()", StartLine: 1, EndLine: 5},
		{ID: "no-emb", FileID: "null-emb-file", FilePath: "test.go", Content: "func bar()", StartLine: 6, EndLine: 10},
	}
	require.NoError(t, store.SaveChunks(ctx, chunks))

	// When: saving embedding for only one chunk
	err := store.SaveChunkEmbeddings(ctx, []string{"has-emb"}, [][]float32{{0.1, 0.2}}, "test-model")
	require.NoError(t, err)

	// Then: only the chunk with embedding is returned
	allEmbs, err := store.GetAllEmbeddings(ctx)
	require.NoError(t, err)
	assert.Len(t, allEmbs, 1)
	assert.Contains(t, allEmbs, "has-emb")
	assert.NotContains(t, allEmbs, "no-emb")
}

func TestGetEmbeddingStats(t *testing.T) {
	store, tmpDir := newTestStore(t)
	ctx := context.Background()

	// Given: a project, file, and chunks
	project := &Project{ID: "stats-proj", Name: "stats-test", RootPath: tmpDir}
	require.NoError(t, store.SaveProject(ctx, project))

	file := &File{ID: "stats-file", ProjectID: "stats-proj", Path: "test.go"}
	require.NoError(t, store.SaveFiles(ctx, []*File{file}))

	chunks := []*Chunk{
		{ID: "s-chunk-1", FileID: "stats-file", FilePath: "test.go", Content: "func a()", StartLine: 1, EndLine: 5},
		{ID: "s-chunk-2", FileID: "stats-file", FilePath: "test.go", Content: "func b()", StartLine: 6, EndLine: 10},
		{ID: "s-chunk-3", FileID: "stats-file", FilePath: "test.go", Content: "func c()", StartLine: 11, EndLine: 15},
	}
	require.NoError(t, store.SaveChunks(ctx, chunks))

	// When: saving embeddings for 2 of 3 chunks
	err := store.SaveChunkEmbeddings(ctx, []string{"s-chunk-1", "s-chunk-2"}, [][]float32{{0.1}, {0.2}}, "test")
	require.NoError(t, err)

	// Then: stats reflect the correct counts
	withEmb, withoutEmb, err := store.GetEmbeddingStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, withEmb)
	assert.Equal(t, 1, withoutEmb)
}

// =============================================================================
// DEBT-028: Additional Coverage Tests
// =============================================================================

func TestSQLiteStore_DB(t *testing.T) {
	store, _ := newTestStore(t)

	// When: getting the underlying DB
	db := store.DB()

	// Then: it's not nil and works
	assert.NotNil(t, db)

	// Verify it works by pinging
	err := db.Ping()
	assert.NoError(t, err)
}

func TestSQLiteStore_ListFilePathsUnder(t *testing.T) {
	store, tmpDir := newTestStore(t)
	ctx := context.Background()

	// Given: a project with files in different directories
	project := &Project{ID: "proj-paths", Name: "paths-test", RootPath: tmpDir}
	require.NoError(t, store.SaveProject(ctx, project))

	files := []*File{
		{ID: "f1", ProjectID: "proj-paths", Path: "src/main.go"},
		{ID: "f2", ProjectID: "proj-paths", Path: "src/utils/helper.go"},
		{ID: "f3", ProjectID: "proj-paths", Path: "src/utils/math.go"},
		{ID: "f4", ProjectID: "proj-paths", Path: "test/main_test.go"},
		{ID: "f5", ProjectID: "proj-paths", Path: "README.md"},
	}
	require.NoError(t, store.SaveFiles(ctx, files))

	t.Run("list files under src/utils", func(t *testing.T) {
		paths, err := store.ListFilePathsUnder(ctx, "proj-paths", "src/utils")
		require.NoError(t, err)
		assert.Len(t, paths, 2)
		assert.Contains(t, paths, "src/utils/helper.go")
		assert.Contains(t, paths, "src/utils/math.go")
	})

	t.Run("list files under src", func(t *testing.T) {
		paths, err := store.ListFilePathsUnder(ctx, "proj-paths", "src")
		require.NoError(t, err)
		assert.Len(t, paths, 3)
		assert.Contains(t, paths, "src/main.go")
		assert.Contains(t, paths, "src/utils/helper.go")
		assert.Contains(t, paths, "src/utils/math.go")
	})

	t.Run("list files under test", func(t *testing.T) {
		paths, err := store.ListFilePathsUnder(ctx, "proj-paths", "test")
		require.NoError(t, err)
		assert.Len(t, paths, 1)
		assert.Contains(t, paths, "test/main_test.go")
	})

	t.Run("list files under nonexistent dir", func(t *testing.T) {
		paths, err := store.ListFilePathsUnder(ctx, "proj-paths", "nonexistent")
		require.NoError(t, err)
		assert.Empty(t, paths)
	})

	t.Run("empty prefix returns all files", func(t *testing.T) {
		paths, err := store.ListFilePathsUnder(ctx, "proj-paths", "")
		require.NoError(t, err)
		assert.Len(t, paths, 5)
	})

	t.Run("trailing slash is normalized", func(t *testing.T) {
		paths, err := store.ListFilePathsUnder(ctx, "proj-paths", "src/utils/")
		require.NoError(t, err)
		assert.Len(t, paths, 2)
	})
}

func TestSQLiteStore_GetFilesForReconciliation(t *testing.T) {
	store, tmpDir := newTestStore(t)
	ctx := context.Background()

	// Given: a project with files
	project := &Project{ID: "proj-recon", Name: "recon-test", RootPath: tmpDir}
	require.NoError(t, store.SaveProject(ctx, project))

	now := time.Now()
	files := []*File{
		{ID: "f1", ProjectID: "proj-recon", Path: "main.go", Size: 100, ModTime: now, Language: "go"},
		{ID: "f2", ProjectID: "proj-recon", Path: "util.go", Size: 200, ModTime: now.Add(-time.Hour), Language: "go"},
	}
	require.NoError(t, store.SaveFiles(ctx, files))

	// When: getting files for reconciliation
	fileMap, err := store.GetFilesForReconciliation(ctx, "proj-recon")

	// Then: all files are returned as a map keyed by path
	require.NoError(t, err)
	assert.Len(t, fileMap, 2)

	f1 := fileMap["main.go"]
	require.NotNil(t, f1)
	assert.Equal(t, "f1", f1.ID)
	assert.Equal(t, int64(100), f1.Size)

	f2 := fileMap["util.go"]
	require.NotNil(t, f2)
	assert.Equal(t, "f2", f2.ID)
	assert.Equal(t, int64(200), f2.Size)
}

func TestSQLiteStore_GetFilesForReconciliation_Empty(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Given: a project with no files
	project := &Project{ID: "proj-empty", Name: "empty-test", RootPath: "/tmp"}
	require.NoError(t, store.SaveProject(ctx, project))

	// When: getting files for reconciliation
	fileMap, err := store.GetFilesForReconciliation(ctx, "proj-empty")

	// Then: empty map is returned
	require.NoError(t, err)
	assert.Empty(t, fileMap)
}

func TestSQLiteStore_DeleteFile(t *testing.T) {
	store, tmpDir := newTestStore(t)
	ctx := context.Background()

	// Given: a project with a file and chunks
	project := &Project{ID: "proj-del", Name: "del-test", RootPath: tmpDir}
	require.NoError(t, store.SaveProject(ctx, project))

	file := &File{ID: "file-del", ProjectID: "proj-del", Path: "delete_me.go"}
	require.NoError(t, store.SaveFiles(ctx, []*File{file}))

	chunks := []*Chunk{
		{ID: "c1", FileID: "file-del", FilePath: "delete_me.go", Content: "func a()"},
		{ID: "c2", FileID: "file-del", FilePath: "delete_me.go", Content: "func b()"},
	}
	require.NoError(t, store.SaveChunks(ctx, chunks))

	// Verify file exists
	retrieved, err := store.GetFileByPath(ctx, "proj-del", "delete_me.go")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// When: deleting the file
	err = store.DeleteFile(ctx, "file-del")
	require.NoError(t, err)

	// Then: file is gone
	retrieved, err = store.GetFileByPath(ctx, "proj-del", "delete_me.go")
	require.NoError(t, err)
	assert.Nil(t, retrieved)

	// And: chunks are cascade deleted
	chunk, err := store.GetChunk(ctx, "c1")
	require.NoError(t, err)
	assert.Nil(t, chunk)
}

func TestSQLiteStore_DeleteFile_NonExistent(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// When: deleting a non-existent file
	err := store.DeleteFile(ctx, "nonexistent-file")

	// Then: no error (idempotent)
	assert.NoError(t, err)
}

func TestSQLiteStore_GetChunks(t *testing.T) {
	store, tmpDir := newTestStore(t)
	ctx := context.Background()

	// Given: a project, file, and chunks
	project := &Project{ID: "proj-chunks", Name: "chunks-test", RootPath: tmpDir}
	require.NoError(t, store.SaveProject(ctx, project))

	file := &File{ID: "file-chunks", ProjectID: "proj-chunks", Path: "main.go"}
	require.NoError(t, store.SaveFiles(ctx, []*File{file}))

	chunks := []*Chunk{
		{ID: "gc1", FileID: "file-chunks", FilePath: "main.go", Content: "func a()", StartLine: 1, EndLine: 5},
		{ID: "gc2", FileID: "file-chunks", FilePath: "main.go", Content: "func b()", StartLine: 6, EndLine: 10},
		{ID: "gc3", FileID: "file-chunks", FilePath: "main.go", Content: "func c()", StartLine: 11, EndLine: 15},
	}
	require.NoError(t, store.SaveChunks(ctx, chunks))

	t.Run("get multiple chunks", func(t *testing.T) {
		retrieved, err := store.GetChunks(ctx, []string{"gc1", "gc2", "gc3"})
		require.NoError(t, err)
		assert.Len(t, retrieved, 3)
	})

	t.Run("get subset of chunks", func(t *testing.T) {
		retrieved, err := store.GetChunks(ctx, []string{"gc1", "gc3"})
		require.NoError(t, err)
		assert.Len(t, retrieved, 2)
	})

	t.Run("get with some missing IDs", func(t *testing.T) {
		retrieved, err := store.GetChunks(ctx, []string{"gc1", "nonexistent", "gc2"})
		require.NoError(t, err)
		assert.Len(t, retrieved, 2) // Only existing chunks returned
	})

	t.Run("get empty list", func(t *testing.T) {
		retrieved, err := store.GetChunks(ctx, []string{})
		require.NoError(t, err)
		assert.Nil(t, retrieved)
	})

	t.Run("get all nonexistent", func(t *testing.T) {
		retrieved, err := store.GetChunks(ctx, []string{"none1", "none2"})
		require.NoError(t, err)
		assert.Empty(t, retrieved)
	})
}

func TestSQLiteStore_DeleteChunks(t *testing.T) {
	store, tmpDir := newTestStore(t)
	ctx := context.Background()

	// Given: a project, file, and chunks
	project := &Project{ID: "proj-delc", Name: "delc-test", RootPath: tmpDir}
	require.NoError(t, store.SaveProject(ctx, project))

	file := &File{ID: "file-delc", ProjectID: "proj-delc", Path: "main.go"}
	require.NoError(t, store.SaveFiles(ctx, []*File{file}))

	chunks := []*Chunk{
		{ID: "dc1", FileID: "file-delc", FilePath: "main.go", Content: "func a()"},
		{ID: "dc2", FileID: "file-delc", FilePath: "main.go", Content: "func b()"},
		{ID: "dc3", FileID: "file-delc", FilePath: "main.go", Content: "func c()"},
	}
	require.NoError(t, store.SaveChunks(ctx, chunks))

	t.Run("delete some chunks", func(t *testing.T) {
		err := store.DeleteChunks(ctx, []string{"dc1", "dc2"})
		require.NoError(t, err)

		// Verify deleted
		chunk, err := store.GetChunk(ctx, "dc1")
		require.NoError(t, err)
		assert.Nil(t, chunk)

		// Verify dc3 still exists
		chunk, err = store.GetChunk(ctx, "dc3")
		require.NoError(t, err)
		assert.NotNil(t, chunk)
	})

	t.Run("delete empty list", func(t *testing.T) {
		err := store.DeleteChunks(ctx, []string{})
		require.NoError(t, err)
	})

	t.Run("delete nonexistent chunks", func(t *testing.T) {
		err := store.DeleteChunks(ctx, []string{"none1", "none2"})
		require.NoError(t, err) // No error, just logs warning
	})
}

func TestSQLiteStore_IndexCheckpoint(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	t.Run("save and load checkpoint", func(t *testing.T) {
		// When: saving a checkpoint
		err := store.SaveIndexCheckpoint(ctx, "embedding", 100, 50, "nomic-embed-text")
		require.NoError(t, err)

		// Then: it can be loaded
		checkpoint, err := store.LoadIndexCheckpoint(ctx)
		require.NoError(t, err)
		require.NotNil(t, checkpoint)
		assert.Equal(t, "embedding", checkpoint.Stage)
		assert.Equal(t, 100, checkpoint.Total)
		assert.Equal(t, 50, checkpoint.EmbeddedCount)
		assert.Equal(t, "nomic-embed-text", checkpoint.EmbedderModel)
		assert.False(t, checkpoint.Timestamp.IsZero())
	})

	t.Run("update checkpoint", func(t *testing.T) {
		// When: updating checkpoint progress
		err := store.SaveIndexCheckpoint(ctx, "embedding", 100, 75, "nomic-embed-text")
		require.NoError(t, err)

		checkpoint, err := store.LoadIndexCheckpoint(ctx)
		require.NoError(t, err)
		assert.Equal(t, 75, checkpoint.EmbeddedCount)
	})

	t.Run("clear checkpoint", func(t *testing.T) {
		// When: clearing the checkpoint
		err := store.ClearIndexCheckpoint(ctx)
		require.NoError(t, err)

		// Then: no checkpoint exists
		checkpoint, err := store.LoadIndexCheckpoint(ctx)
		require.NoError(t, err)
		assert.Nil(t, checkpoint)
	})

	t.Run("no checkpoint returns nil", func(t *testing.T) {
		// Given: fresh store with no checkpoint
		store2, _ := newTestStore(t)

		// When: loading checkpoint
		checkpoint, err := store2.LoadIndexCheckpoint(ctx)

		// Then: nil is returned
		require.NoError(t, err)
		assert.Nil(t, checkpoint)
	})

	t.Run("complete stage returns nil", func(t *testing.T) {
		// When: saving a "complete" checkpoint
		err := store.SaveIndexCheckpoint(ctx, "complete", 100, 100, "nomic-embed-text")
		require.NoError(t, err)

		// Then: LoadIndexCheckpoint returns nil (complete = done)
		checkpoint, err := store.LoadIndexCheckpoint(ctx)
		require.NoError(t, err)
		assert.Nil(t, checkpoint)
	})
}
