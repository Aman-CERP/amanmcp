package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/chunk"
	"github.com/Aman-CERP/amanmcp/internal/embed"
	"github.com/Aman-CERP/amanmcp/internal/scanner"
	"github.com/Aman-CERP/amanmcp/internal/search"
	"github.com/Aman-CERP/amanmcp/internal/store"
	"github.com/Aman-CERP/amanmcp/internal/watcher"
)

func setupTestCoordinator(t *testing.T) (*Coordinator, string, func()) {
	t.Helper()

	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	// Create metadata store
	metadataPath := filepath.Join(dataDir, "metadata.db")
	metadata, err := store.NewSQLiteStore(metadataPath)
	require.NoError(t, err)

	// Create BM25 index
	bm25BasePath := filepath.Join(dataDir, "bm25")
	bm25, err := store.NewBM25IndexWithBackend(bm25BasePath, store.DefaultBM25Config(), "")
	require.NoError(t, err)

	// Create vector store (with static embedder dimensions)
	vectorCfg := store.DefaultVectorStoreConfig(256) // Static embedder uses 256 dims
	vector, err := store.NewHNSWStore(vectorCfg)
	require.NoError(t, err)

	// Create static embedder (for fast testing without model download)
	embedder := embed.NewStaticEmbedder()

	// Create search engine
	engineCfg := search.DefaultConfig()
	engine := search.New(bm25, vector, embedder, metadata, engineCfg)

	// Create code chunker
	codeChunker := chunk.NewCodeChunker()

	// Create markdown chunker
	mdChunker := chunk.NewMarkdownChunker()

	// Create project record (required for foreign key constraints)
	project := &store.Project{
		ID:       "test-project",
		Name:     "Test Project",
		RootPath: tempDir,
	}
	require.NoError(t, metadata.SaveProject(context.Background(), project))

	// Create coordinator
	coord := NewCoordinator(CoordinatorConfig{
		ProjectID:   "test-project",
		RootPath:    tempDir,
		DataDir:     dataDir,
		Engine:      engine,
		Metadata:    metadata,
		CodeChunker: codeChunker,
		MDChunker:   mdChunker,
	})

	cleanup := func() {
		_ = engine.Close()
		_ = metadata.Close()
		_ = bm25.Close()
		_ = vector.Close()
		codeChunker.Close()
	}

	return coord, tempDir, cleanup
}

func TestCoordinator_HandleEvents_Create(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinator(t)
	defer cleanup()

	ctx := context.Background()

	// Create a test file
	testFile := filepath.Join(tempDir, "main.go")
	content := `package main

func hello() {
	println("Hello, World!")
}
`
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0o644))

	// Handle create event
	events := []watcher.FileEvent{
		{
			Path:      "main.go",
			Operation: watcher.OpCreate,
			IsDir:     false,
			Timestamp: time.Now(),
		},
	}

	err := coord.HandleEvents(ctx, events)
	require.NoError(t, err)

	// Verify file was indexed - check search returns results
	results, err := coord.config.Engine.Search(ctx, "hello", search.SearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.NotEmpty(t, results, "expected search results for indexed file")
}

func TestCoordinator_HandleEvents_Modify(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinator(t)
	defer cleanup()

	ctx := context.Background()

	// Create and index initial file
	testFile := filepath.Join(tempDir, "main.go")
	content := `package main

func oldFunction() {
	println("Old")
}
`
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0o644))

	// Index initial version
	createEvents := []watcher.FileEvent{
		{Path: "main.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, createEvents))

	// Verify old content is searchable
	results, _ := coord.config.Engine.Search(ctx, "oldFunction", search.SearchOptions{Limit: 10})
	assert.NotEmpty(t, results, "expected old content to be searchable")

	// Modify the file
	newContent := `package main

func newFunction() {
	println("New")
}
`
	require.NoError(t, os.WriteFile(testFile, []byte(newContent), 0o644))

	// Handle modify event
	modifyEvents := []watcher.FileEvent{
		{Path: "main.go", Operation: watcher.OpModify, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, modifyEvents))

	// Verify new content is searchable
	results, _ = coord.config.Engine.Search(ctx, "newFunction", search.SearchOptions{Limit: 10})
	assert.NotEmpty(t, results, "expected new content to be searchable")
}

func TestCoordinator_HandleEvents_Delete(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinator(t)
	defer cleanup()

	ctx := context.Background()

	// Create and index a file
	testFile := filepath.Join(tempDir, "todelete.go")
	content := `package main

func deleteMe() {
	println("Delete me")
}
`
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0o644))

	// Index the file
	createEvents := []watcher.FileEvent{
		{Path: "todelete.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, createEvents))

	// Verify it's indexed
	results, _ := coord.config.Engine.Search(ctx, "deleteMe", search.SearchOptions{Limit: 10})
	require.NotEmpty(t, results, "expected file to be indexed before delete")

	// Delete the file
	require.NoError(t, os.Remove(testFile))

	// Handle delete event
	deleteEvents := []watcher.FileEvent{
		{Path: "todelete.go", Operation: watcher.OpDelete, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, deleteEvents))

	// Verify it's no longer searchable
	results, _ = coord.config.Engine.Search(ctx, "deleteMe", search.SearchOptions{Limit: 10})
	assert.Empty(t, results, "expected file to be removed from index")
}

func TestCoordinator_HandleEvents_SkipsBinaryFiles(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinator(t)
	defer cleanup()

	ctx := context.Background()

	// Create a binary file (contains null bytes)
	binaryFile := filepath.Join(tempDir, "binary.bin")
	binaryContent := []byte{0x00, 0x01, 0x02, 0x03, 0x00}
	require.NoError(t, os.WriteFile(binaryFile, binaryContent, 0o644))

	// Handle create event
	events := []watcher.FileEvent{
		{Path: "binary.bin", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}

	// Should not error, just skip
	err := coord.HandleEvents(ctx, events)
	assert.NoError(t, err)
}

func TestCoordinator_HandleEvents_SkipsDirectories(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinator(t)
	defer cleanup()

	ctx := context.Background()

	// Create a directory
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0o755))

	// Handle directory create event
	events := []watcher.FileEvent{
		{Path: "subdir", Operation: watcher.OpCreate, IsDir: true, Timestamp: time.Now()},
	}

	// Should not error, just skip directories
	err := coord.HandleEvents(ctx, events)
	assert.NoError(t, err)
}

func TestCoordinator_HandleEvents_MarkdownFile(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinator(t)
	defer cleanup()

	ctx := context.Background()

	// Create a markdown file
	mdFile := filepath.Join(tempDir, "README.md")
	content := `# Project Title

## Overview

This is a test markdown file with some content.

## Usage

Run the program with these steps.
`
	require.NoError(t, os.WriteFile(mdFile, []byte(content), 0o644))

	// Handle create event
	events := []watcher.FileEvent{
		{Path: "README.md", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}

	err := coord.HandleEvents(ctx, events)
	require.NoError(t, err)

	// Verify markdown was indexed
	results, err := coord.config.Engine.Search(ctx, "markdown file", search.SearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.NotEmpty(t, results, "expected markdown file to be indexed")
}

func TestCoordinator_HandleEvents_MultipleFiles(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinator(t)
	defer cleanup()

	ctx := context.Background()

	// Create multiple files
	file1 := filepath.Join(tempDir, "file1.go")
	file2 := filepath.Join(tempDir, "file2.go")
	require.NoError(t, os.WriteFile(file1, []byte("package main\nfunc one() {}"), 0o644))
	require.NoError(t, os.WriteFile(file2, []byte("package main\nfunc two() {}"), 0o644))

	// Handle multiple create events
	events := []watcher.FileEvent{
		{Path: "file1.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
		{Path: "file2.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}

	err := coord.HandleEvents(ctx, events)
	require.NoError(t, err)

	// Verify both files are indexed
	results1, _ := coord.config.Engine.Search(ctx, "func one", search.SearchOptions{Limit: 10})
	results2, _ := coord.config.Engine.Search(ctx, "func two", search.SearchOptions{Limit: 10})
	assert.NotEmpty(t, results1, "expected file1 to be indexed")
	assert.NotEmpty(t, results2, "expected file2 to be indexed")
}

// setupTestCoordinatorWithScanner creates a coordinator with scanner for gitignore tests.
func setupTestCoordinatorWithScanner(t *testing.T) (*Coordinator, string, func()) {
	t.Helper()

	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	// Create metadata store
	metadataPath := filepath.Join(dataDir, "metadata.db")
	metadata, err := store.NewSQLiteStore(metadataPath)
	require.NoError(t, err)

	// Create BM25 index
	bm25BasePath := filepath.Join(dataDir, "bm25")
	bm25, err := store.NewBM25IndexWithBackend(bm25BasePath, store.DefaultBM25Config(), "")
	require.NoError(t, err)

	// Create vector store (with static embedder dimensions)
	vectorCfg := store.DefaultVectorStoreConfig(256)
	vector, err := store.NewHNSWStore(vectorCfg)
	require.NoError(t, err)

	// Create static embedder
	embedder := embed.NewStaticEmbedder()

	// Create search engine
	engineCfg := search.DefaultConfig()
	engine := search.New(bm25, vector, embedder, metadata, engineCfg)

	// Create chunkers
	codeChunker := chunk.NewCodeChunker()
	mdChunker := chunk.NewMarkdownChunker()

	// Create scanner for gitignore reconciliation
	fileScanner, err := scanner.New()
	require.NoError(t, err)

	// Create project record
	project := &store.Project{
		ID:       "test-project",
		Name:     "Test Project",
		RootPath: tempDir,
	}
	require.NoError(t, metadata.SaveProject(context.Background(), project))

	// Create coordinator with scanner
	coord := NewCoordinator(CoordinatorConfig{
		ProjectID:   "test-project",
		RootPath:    tempDir,
		DataDir:     dataDir,
		Engine:      engine,
		Metadata:    metadata,
		CodeChunker: codeChunker,
		MDChunker:   mdChunker,
		Scanner:     fileScanner,
	})

	cleanup := func() {
		_ = engine.Close()
		_ = metadata.Close()
		_ = bm25.Close()
		_ = vector.Close()
		codeChunker.Close()
	}

	return coord, tempDir, cleanup
}

func TestCoordinator_HandleEvents_GitignoreChange_RemovesIgnoredFiles(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// Create test files
	file1 := filepath.Join(tempDir, "keep.go")
	file2 := filepath.Join(tempDir, "ignored.go")
	file3 := filepath.Join(tempDir, "also_keep.go")

	require.NoError(t, os.WriteFile(file1, []byte("package main\nfunc keepMe() {}"), 0o644))
	require.NoError(t, os.WriteFile(file2, []byte("package main\nfunc ignoredFunc() {}"), 0o644))
	require.NoError(t, os.WriteFile(file3, []byte("package main\nfunc alsoKeep() {}"), 0o644))

	// Index all files
	createEvents := []watcher.FileEvent{
		{Path: "keep.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
		{Path: "ignored.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
		{Path: "also_keep.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, createEvents))

	// Verify all 3 files are indexed
	paths, err := coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, paths, 3, "expected 3 files indexed before gitignore")

	// Create .gitignore that ignores "ignored.go"
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	require.NoError(t, os.WriteFile(gitignorePath, []byte("ignored.go\n"), 0o644))

	// Trigger gitignore change event
	gitignoreEvents := []watcher.FileEvent{
		{Path: ".gitignore", Operation: watcher.OpGitignoreChange, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, gitignoreEvents))

	// Verify ignored.go was removed but others remain
	paths, err = coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, paths, 2, "expected 2 files after gitignore removed ignored.go")
	assert.Contains(t, paths, "keep.go")
	assert.Contains(t, paths, "also_keep.go")
	assert.NotContains(t, paths, "ignored.go", "ignored.go should be removed")
}

func TestCoordinator_HandleEvents_GitignoreChange_AddsUnignoredFiles(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// Create .gitignore first that ignores "newfile.go"
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	require.NoError(t, os.WriteFile(gitignorePath, []byte("newfile.go\n"), 0o644))

	// Create test files
	file1 := filepath.Join(tempDir, "existing.go")
	file2 := filepath.Join(tempDir, "newfile.go") // This is currently ignored

	require.NoError(t, os.WriteFile(file1, []byte("package main\nfunc existing() {}"), 0o644))
	require.NoError(t, os.WriteFile(file2, []byte("package main\nfunc newFunc() {}"), 0o644))

	// Index only the non-ignored file
	createEvents := []watcher.FileEvent{
		{Path: "existing.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, createEvents))

	// Verify only 1 file is indexed
	paths, err := coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, paths, 1, "expected 1 file indexed before gitignore change")

	// Remove "newfile.go" from .gitignore (making it unignored)
	require.NoError(t, os.WriteFile(gitignorePath, []byte("# empty gitignore\n"), 0o644))

	// Trigger gitignore change event
	gitignoreEvents := []watcher.FileEvent{
		{Path: ".gitignore", Operation: watcher.OpGitignoreChange, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, gitignoreEvents))

	// Verify newfile.go was added
	paths, err = coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, paths, 2, "expected 2 files after gitignore change added newfile.go")
	assert.Contains(t, paths, "existing.go")
	assert.Contains(t, paths, "newfile.go")
}

func TestCoordinator_HandleEvents_GitignoreChange_NoScanner(t *testing.T) {
	// Use the regular setup without scanner
	coord, _, cleanup := setupTestCoordinator(t)
	defer cleanup()

	ctx := context.Background()

	// Trigger gitignore change event - should not error, just log warning
	gitignoreEvents := []watcher.FileEvent{
		{Path: ".gitignore", Operation: watcher.OpGitignoreChange, IsDir: false, Timestamp: time.Now()},
	}

	err := coord.HandleEvents(ctx, gitignoreEvents)
	assert.NoError(t, err, "should not error when scanner is not configured")
}

// TestCoordinator_HandleEvents_ConfigChange_RespectsExcludePatterns tests that
// config change reconciliation uses ExcludePatterns from the config, ensuring
// files matching exclude patterns are not incorrectly removed from the index.
// This was a regression from BUG-027 where exclude patterns were not passed
// to the scanner during reconciliation.
func TestCoordinator_HandleEvents_ConfigChange_RespectsExcludePatterns(t *testing.T) {
	// Create a coordinator with exclude patterns
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	// Create metadata store
	metadataPath := filepath.Join(dataDir, "metadata.db")
	metadata, err := store.NewSQLiteStore(metadataPath)
	require.NoError(t, err)

	// Create BM25 index
	bm25BasePath := filepath.Join(dataDir, "bm25")
	bm25, err := store.NewBM25IndexWithBackend(bm25BasePath, store.DefaultBM25Config(), "")
	require.NoError(t, err)

	// Create vector store
	vectorCfg := store.DefaultVectorStoreConfig(256)
	vector, err := store.NewHNSWStore(vectorCfg)
	require.NoError(t, err)

	// Create static embedder
	embedder := embed.NewStaticEmbedder()

	// Create search engine
	engineCfg := search.DefaultConfig()
	engine := search.New(bm25, vector, embedder, metadata, engineCfg)

	// Create chunkers
	codeChunker := chunk.NewCodeChunker()
	mdChunker := chunk.NewMarkdownChunker()

	// Create scanner
	fileScanner, err := scanner.New()
	require.NoError(t, err)

	// Create project record
	project := &store.Project{
		ID:       "test-project",
		Name:     "Test Project",
		RootPath: tempDir,
	}
	require.NoError(t, metadata.SaveProject(context.Background(), project))

	// Create coordinator WITH exclude patterns for "excluded/**"
	coord := NewCoordinator(CoordinatorConfig{
		ProjectID:       "test-project",
		RootPath:        tempDir,
		DataDir:         dataDir,
		Engine:          engine,
		Metadata:        metadata,
		CodeChunker:     codeChunker,
		MDChunker:       mdChunker,
		Scanner:         fileScanner,
		ExcludePatterns: []string{"**/excluded/**"}, // Key: set exclude pattern
	})

	defer func() {
		_ = engine.Close()
		_ = metadata.Close()
		_ = bm25.Close()
		_ = vector.Close()
		codeChunker.Close()
	}()

	ctx := context.Background()

	// Create directory structure:
	// - keep.go (should remain indexed)
	// - excluded/test.go (matches exclude pattern, should NOT be indexed initially)
	// - also_keep.go (should remain indexed)
	keepFile := filepath.Join(tempDir, "keep.go")
	excludedDir := filepath.Join(tempDir, "excluded")
	excludedFile := filepath.Join(excludedDir, "test.go")
	alsoKeepFile := filepath.Join(tempDir, "also_keep.go")

	require.NoError(t, os.MkdirAll(excludedDir, 0o755))
	require.NoError(t, os.WriteFile(keepFile, []byte("package main\nfunc keep() {}"), 0o644))
	require.NoError(t, os.WriteFile(excludedFile, []byte("package excluded\nfunc excluded() {}"), 0o644))
	require.NoError(t, os.WriteFile(alsoKeepFile, []byte("package main\nfunc alsoKeep() {}"), 0o644))

	// Index the files we want to keep (simulating initial indexing which excluded excluded/test.go)
	createEvents := []watcher.FileEvent{
		{Path: "keep.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
		{Path: "also_keep.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, createEvents))

	// Verify 2 files are indexed
	paths, err := coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, paths, 2, "expected 2 files indexed initially")

	// Trigger config change event (simulates .amanmcp.yaml modification)
	configEvents := []watcher.FileEvent{
		{Path: ".amanmcp.yaml", Operation: watcher.OpConfigChange, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, configEvents))

	// Verify files are still indexed - exclude patterns should prevent
	// reconciliation from trying to remove files that weren't scanned
	paths, err = coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, paths, 2, "expected 2 files after config change - exclude patterns should be respected")
	assert.Contains(t, paths, "keep.go")
	assert.Contains(t, paths, "also_keep.go")
}

// =============================================================================
// BUG-002: File Size Validation
// =============================================================================

// setupTestCoordinatorWithMaxFileSize creates a coordinator with custom max file size for testing.
func setupTestCoordinatorWithMaxFileSize(t *testing.T, maxFileSize int64) (*Coordinator, string, func()) {
	t.Helper()

	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	// Create metadata store
	metadataPath := filepath.Join(dataDir, "metadata.db")
	metadata, err := store.NewSQLiteStore(metadataPath)
	require.NoError(t, err)

	// Create BM25 index
	bm25BasePath := filepath.Join(dataDir, "bm25")
	bm25, err := store.NewBM25IndexWithBackend(bm25BasePath, store.DefaultBM25Config(), "")
	require.NoError(t, err)

	// Create vector store
	vectorCfg := store.DefaultVectorStoreConfig(256)
	vector, err := store.NewHNSWStore(vectorCfg)
	require.NoError(t, err)

	// Create static embedder
	embedder := embed.NewStaticEmbedder()

	// Create search engine
	engineCfg := search.DefaultConfig()
	engine := search.New(bm25, vector, embedder, metadata, engineCfg)

	// Create chunkers
	codeChunker := chunk.NewCodeChunker()
	mdChunker := chunk.NewMarkdownChunker()

	// Create project record
	project := &store.Project{
		ID:       "test-project",
		Name:     "Test Project",
		RootPath: tempDir,
	}
	require.NoError(t, metadata.SaveProject(context.Background(), project))

	// Create coordinator with custom MaxFileSize
	coord := NewCoordinator(CoordinatorConfig{
		ProjectID:   "test-project",
		RootPath:    tempDir,
		DataDir:     dataDir,
		Engine:      engine,
		Metadata:    metadata,
		CodeChunker: codeChunker,
		MDChunker:   mdChunker,
		MaxFileSize: maxFileSize,
	})

	cleanup := func() {
		_ = engine.Close()
		_ = metadata.Close()
		_ = bm25.Close()
		_ = vector.Close()
		codeChunker.Close()
	}

	return coord, tempDir, cleanup
}

func TestCoordinator_HandleEvents_SkipsOversizedFiles(t *testing.T) {
	// Use a small limit (1KB) for testing
	const testMaxSize int64 = 1024
	coord, tempDir, cleanup := setupTestCoordinatorWithMaxFileSize(t, testMaxSize)
	defer cleanup()

	ctx := context.Background()

	// Create a file larger than the test limit (2KB of valid Go content)
	oversizedFile := filepath.Join(tempDir, "huge.go")
	content := "package main\n\nfunc huge() {\n"
	// Add enough content to exceed 1KB
	for i := 0; i < 50; i++ {
		content += "\t// This is a comment line to increase file size\n"
	}
	content += "}\n"
	require.NoError(t, os.WriteFile(oversizedFile, []byte(content), 0o644))

	// Verify file is larger than limit
	info, err := os.Stat(oversizedFile)
	require.NoError(t, err)
	require.Greater(t, info.Size(), testMaxSize, "file should be > 1KB")

	// Handle create event
	events := []watcher.FileEvent{
		{Path: "huge.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}

	// Should not error - oversized files are skipped gracefully
	err = coord.HandleEvents(ctx, events)
	assert.NoError(t, err)

	// Verify file was NOT indexed (oversized files should be skipped)
	results, err := coord.config.Engine.Search(ctx, "huge", search.SearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, results, "oversized file should NOT be indexed")
}

func TestCoordinator_HandleEvents_IndexesFileAtSizeLimit(t *testing.T) {
	// Use a limit that allows a small file (1KB)
	const testMaxSize int64 = 1024
	coord, tempDir, cleanup := setupTestCoordinatorWithMaxFileSize(t, testMaxSize)
	defer cleanup()

	ctx := context.Background()

	// Create a file smaller than the limit (less than 1KB)
	smallFile := filepath.Join(tempDir, "small.go")
	content := "package main\n\nfunc atLimit() {\n\tprintln(\"ok\")\n}\n"
	require.NoError(t, os.WriteFile(smallFile, []byte(content), 0o644))

	// Verify file is smaller than limit
	info, err := os.Stat(smallFile)
	require.NoError(t, err)
	require.LessOrEqual(t, info.Size(), testMaxSize, "file should be <= 1KB")

	// Handle create event
	events := []watcher.FileEvent{
		{Path: "small.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}

	// Should not error
	err = coord.HandleEvents(ctx, events)
	assert.NoError(t, err)

	// File under the limit SHOULD be indexed
	results, err := coord.config.Engine.Search(ctx, "atLimit", search.SearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.NotEmpty(t, results, "file under size limit SHOULD be indexed")
}

// =============================================================================
// BUG-005: Symlink Handling
// =============================================================================

func TestCoordinator_HandleEvents_SkipsSymlinks(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinator(t)
	defer cleanup()

	ctx := context.Background()

	// Create a real file
	realFile := filepath.Join(tempDir, "real.go")
	content := "package main\n\nfunc realFunc() {}\n"
	require.NoError(t, os.WriteFile(realFile, []byte(content), 0o644))

	// Create a symlink to the real file
	symlinkFile := filepath.Join(tempDir, "link.go")
	require.NoError(t, os.Symlink(realFile, symlinkFile))

	// Handle create event for the symlink
	events := []watcher.FileEvent{
		{Path: "link.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}

	// Should not error - symlinks are skipped gracefully
	err := coord.HandleEvents(ctx, events)
	assert.NoError(t, err)

	// Verify symlink was NOT indexed
	results, err := coord.config.Engine.Search(ctx, "realFunc", search.SearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, results, "symlink should NOT be indexed")
}

func TestCoordinator_HandleEvents_SkipsCircularSymlinks(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinator(t)
	defer cleanup()

	ctx := context.Background()

	// Create a circular symlink (points to itself via directory)
	loopLink := filepath.Join(tempDir, "loop")
	require.NoError(t, os.Symlink(".", loopLink))

	// Handle create event for the circular symlink
	events := []watcher.FileEvent{
		{Path: "loop", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}

	// Should not error and should not hang (infinite loop protection)
	err := coord.HandleEvents(ctx, events)
	assert.NoError(t, err, "circular symlink should not cause error or hang")
}

// =============================================================================
// BUG-036: Startup File Reconciliation
// =============================================================================

// TestCoordinator_ReconcileFilesOnStartup_DetectsNewFiles tests that new files
// created while server was stopped are indexed on startup.
func TestCoordinator_ReconcileFilesOnStartup_DetectsNewFiles(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Index initial file via event
	file1 := filepath.Join(tempDir, "existing.go")
	require.NoError(t, os.WriteFile(file1, []byte("package main\nfunc existing() {}"), 0o644))
	events := []watcher.FileEvent{{Path: "existing.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()}}
	require.NoError(t, coord.HandleEvents(ctx, events))

	// Verify only 1 file indexed
	paths, err := coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	require.Len(t, paths, 1, "should have 1 file before reconciliation")

	// 2. Create new file (simulating offline creation - no event)
	file2 := filepath.Join(tempDir, "newfile.go")
	require.NoError(t, os.WriteFile(file2, []byte("package main\nfunc newFunc() {}"), 0o644))

	// 3. Run startup file reconciliation
	require.NoError(t, coord.ReconcileFilesOnStartup(ctx))

	// 4. Verify new file is now indexed
	paths, err = coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, paths, 2, "should have 2 files after reconciliation")
	assert.Contains(t, paths, "newfile.go", "new file should be indexed")
}

// TestCoordinator_ReconcileFilesOnStartup_DetectsModifiedFiles tests that modified files
// are re-indexed on startup.
func TestCoordinator_ReconcileFilesOnStartup_DetectsModifiedFiles(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Index file with original content
	file1 := filepath.Join(tempDir, "modifiable.go")
	require.NoError(t, os.WriteFile(file1, []byte("package main\nfunc originalFunc() {}"), 0o644))
	events := []watcher.FileEvent{{Path: "modifiable.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()}}
	require.NoError(t, coord.HandleEvents(ctx, events))

	// Verify old content is searchable
	results, _ := coord.config.Engine.Search(ctx, "originalFunc", search.SearchOptions{Limit: 10})
	require.NotEmpty(t, results, "original content should be searchable")

	// Wait to ensure mtime changes
	time.Sleep(50 * time.Millisecond)

	// 2. Modify file (simulating offline modification - no event)
	require.NoError(t, os.WriteFile(file1, []byte("package main\nfunc modifiedFunc() {}"), 0o644))

	// 3. Run startup file reconciliation
	require.NoError(t, coord.ReconcileFilesOnStartup(ctx))

	// 4. Verify new content is searchable
	results, _ = coord.config.Engine.Search(ctx, "modifiedFunc", search.SearchOptions{Limit: 10})
	assert.NotEmpty(t, results, "modified content should be searchable after reconciliation")
}

// TestCoordinator_ReconcileFilesOnStartup_DetectsDeletedFiles tests that deleted files
// are removed from index on startup.
func TestCoordinator_ReconcileFilesOnStartup_DetectsDeletedFiles(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Index file
	file1 := filepath.Join(tempDir, "tobedeleted.go")
	require.NoError(t, os.WriteFile(file1, []byte("package main\nfunc deleteMe() {}"), 0o644))
	events := []watcher.FileEvent{{Path: "tobedeleted.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()}}
	require.NoError(t, coord.HandleEvents(ctx, events))

	// Verify indexed
	paths, _ := coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.Contains(t, paths, "tobedeleted.go", "file should be indexed before deletion")

	// 2. Delete file (simulating offline deletion - no event)
	require.NoError(t, os.Remove(file1))

	// 3. Run startup file reconciliation
	require.NoError(t, coord.ReconcileFilesOnStartup(ctx))

	// 4. Verify file is removed from index
	paths, _ = coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	assert.NotContains(t, paths, "tobedeleted.go", "deleted file should be removed from index")
}

// TestCoordinator_ReconcileFilesOnStartup_NoChanges tests that reconciliation
// is fast when no changes occurred.
func TestCoordinator_ReconcileFilesOnStartup_NoChanges(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Index file
	file1 := filepath.Join(tempDir, "stable.go")
	require.NoError(t, os.WriteFile(file1, []byte("package main\nfunc stable() {}"), 0o644))
	events := []watcher.FileEvent{{Path: "stable.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()}}
	require.NoError(t, coord.HandleEvents(ctx, events))

	// 2. Run reconciliation (no changes)
	start := time.Now()
	require.NoError(t, coord.ReconcileFilesOnStartup(ctx))
	duration := time.Since(start)

	// Should complete quickly (no re-indexing needed)
	assert.Less(t, duration, 500*time.Millisecond, "reconciliation with no changes should be fast")
}

// BUG-053: Gitignore Hash Exported and Used Correctly
// =============================================================================

// TestComputeGitignoreHash_Deterministic tests that the gitignore hash computation
// is deterministic and consistent across calls.
func TestComputeGitignoreHash_Deterministic(t *testing.T) {
	tempDir := t.TempDir()

	// Create .gitignore file
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	require.NoError(t, os.WriteFile(gitignorePath, []byte("*.log\n*.tmp\n"), 0o644))

	// Compute hash twice - should be identical
	hash1, err := ComputeGitignoreHash(tempDir)
	require.NoError(t, err)
	require.NotEmpty(t, hash1)

	hash2, err := ComputeGitignoreHash(tempDir)
	require.NoError(t, err)

	assert.Equal(t, hash1, hash2, "gitignore hash should be deterministic")
}

// TestComputeGitignoreHash_ChangesOnContent tests that the hash changes
// when gitignore content changes.
func TestComputeGitignoreHash_ChangesOnContent(t *testing.T) {
	tempDir := t.TempDir()

	// Create initial .gitignore
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	require.NoError(t, os.WriteFile(gitignorePath, []byte("*.log\n"), 0o644))

	hash1, err := ComputeGitignoreHash(tempDir)
	require.NoError(t, err)

	// Modify .gitignore
	require.NoError(t, os.WriteFile(gitignorePath, []byte("*.log\n*.tmp\n"), 0o644))

	hash2, err := ComputeGitignoreHash(tempDir)
	require.NoError(t, err)

	assert.NotEqual(t, hash1, hash2, "gitignore hash should change when content changes")
}

// TestComputeGitignoreHash_NoGitignore tests that empty hash is returned
// when no gitignore files exist.
func TestComputeGitignoreHash_NoGitignore(t *testing.T) {
	tempDir := t.TempDir()

	// No .gitignore file - should still succeed with empty hash
	hash, err := ComputeGitignoreHash(tempDir)
	require.NoError(t, err)
	// Hash of empty input should still be a valid SHA256 hash
	assert.NotEmpty(t, hash, "should return valid hash even with no gitignore files")
}

// TestReconcileOnStartup_SkipsWhenHashMatches tests that startup reconciliation
// is skipped when the cached gitignore hash matches current hash.
// This is the regression test for BUG-053.
func TestReconcileOnStartup_SkipsWhenHashMatches(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// Create .gitignore
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	require.NoError(t, os.WriteFile(gitignorePath, []byte("*.log\n"), 0o644))

	// Compute and save the gitignore hash (simulating what index.go should do)
	hash, err := ComputeGitignoreHash(tempDir)
	require.NoError(t, err)
	require.NoError(t, coord.config.Metadata.SetState(ctx, GitignoreHashKey, hash))

	// Create and index a file
	file1 := filepath.Join(tempDir, "test.go")
	require.NoError(t, os.WriteFile(file1, []byte("package main\nfunc test() {}"), 0o644))
	events := []watcher.FileEvent{{Path: "test.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()}}
	require.NoError(t, coord.HandleEvents(ctx, events))

	// Verify file is indexed
	paths, _ := coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.Len(t, paths, 1)

	// Run ReconcileOnStartup - should skip because hash matches
	err = coord.ReconcileOnStartup(ctx)
	require.NoError(t, err)

	// File should still be indexed (no reconciliation ran)
	paths, _ = coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	assert.Len(t, paths, 1, "file should remain indexed when gitignore hash matches")
}

// TestReconcileOnStartup_RunsWhenHashMissing tests that startup reconciliation
// runs when no cached hash exists (e.g., after fresh index without hash save).
// This documents the bug behavior that BUG-053 fixes.
func TestReconcileOnStartup_RunsWhenHashMissing(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// Create .gitignore
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	require.NoError(t, os.WriteFile(gitignorePath, []byte("*.log\n"), 0o644))

	// DO NOT save gitignore hash (simulating the bug where index.go didn't save it)

	// Create and index a file
	file1 := filepath.Join(tempDir, "test.go")
	require.NoError(t, os.WriteFile(file1, []byte("package main\nfunc test() {}"), 0o644))
	events := []watcher.FileEvent{{Path: "test.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()}}
	require.NoError(t, coord.HandleEvents(ctx, events))

	// Run ReconcileOnStartup - should run because no cached hash
	err := coord.ReconcileOnStartup(ctx)
	require.NoError(t, err)

	// After reconciliation, hash should be saved
	savedHash, err := coord.config.Metadata.GetState(ctx, GitignoreHashKey)
	require.NoError(t, err)
	assert.NotEmpty(t, savedHash, "hash should be saved after reconciliation")
}

// TestScanCurrentFiles_RespectsExcludePatterns tests that scanCurrentFiles
// uses ExcludePatterns from config, preventing false "new file" detections.
// This is the regression test for BUG-053 secondary issue.
func TestScanCurrentFiles_RespectsExcludePatterns(t *testing.T) {
	// Create a coordinator with exclude patterns
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	// Create metadata store
	metadataPath := filepath.Join(dataDir, "metadata.db")
	metadata, err := store.NewSQLiteStore(metadataPath)
	require.NoError(t, err)

	// Create BM25 index
	bm25BasePath := filepath.Join(dataDir, "bm25")
	bm25, err := store.NewBM25IndexWithBackend(bm25BasePath, store.DefaultBM25Config(), "")
	require.NoError(t, err)

	// Create vector store
	vectorCfg := store.DefaultVectorStoreConfig(256)
	vector, err := store.NewHNSWStore(vectorCfg)
	require.NoError(t, err)

	// Create static embedder
	embedder := embed.NewStaticEmbedder()

	// Create search engine
	engineCfg := search.DefaultConfig()
	engine := search.New(bm25, vector, embedder, metadata, engineCfg)

	// Create chunkers
	codeChunker := chunk.NewCodeChunker()
	mdChunker := chunk.NewMarkdownChunker()

	// Create scanner
	fileScanner, err := scanner.New()
	require.NoError(t, err)

	// Create project record
	project := &store.Project{
		ID:       "test-project",
		Name:     "Test Project",
		RootPath: tempDir,
	}
	require.NoError(t, metadata.SaveProject(context.Background(), project))

	// Create coordinator WITH exclude patterns for "excluded/**"
	coord := NewCoordinator(CoordinatorConfig{
		ProjectID:       "test-project",
		RootPath:        tempDir,
		DataDir:         dataDir,
		Engine:          engine,
		Metadata:        metadata,
		CodeChunker:     codeChunker,
		MDChunker:       mdChunker,
		Scanner:         fileScanner,
		ExcludePatterns: []string{"**/excluded/**"}, // Exclude pattern
	})

	defer func() {
		_ = engine.Close()
		_ = metadata.Close()
		_ = bm25.Close()
		_ = vector.Close()
		codeChunker.Close()
	}()

	ctx := context.Background()

	// Create directory structure:
	// - included.go (should be scanned)
	// - excluded/test.go (should NOT be scanned due to exclude pattern)
	includedFile := filepath.Join(tempDir, "included.go")
	excludedDir := filepath.Join(tempDir, "excluded")
	excludedFile := filepath.Join(excludedDir, "test.go")

	require.NoError(t, os.MkdirAll(excludedDir, 0o755))
	require.NoError(t, os.WriteFile(includedFile, []byte("package main\nfunc included() {}"), 0o644))
	require.NoError(t, os.WriteFile(excludedFile, []byte("package excluded\nfunc excluded() {}"), 0o644))

	// Index only the included file
	events := []watcher.FileEvent{
		{Path: "included.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, events))

	// Save gitignore hash to prevent gitignore reconciliation
	hash, _ := ComputeGitignoreHash(tempDir)
	require.NoError(t, metadata.SetState(ctx, GitignoreHashKey, hash))

	// Run file reconciliation - should NOT detect excluded/test.go as "new"
	err = coord.ReconcileFilesOnStartup(ctx)
	require.NoError(t, err)

	// Verify only the included file is indexed (excluded file not added)
	paths, _ := metadata.GetFilePathsByProject(ctx, "test-project")
	assert.Len(t, paths, 1, "only included.go should be indexed")
	assert.Contains(t, paths, "included.go")
	assert.NotContains(t, paths, "excluded/test.go", "excluded file should not be added by reconciliation")
}

// BUG-037: Shutdown Race Condition
// =============================================================================

// TestCoordinator_ApplyFileChanges_ContextCancelled tests that applyFileChanges
// exits gracefully when context is canceled during file processing.
// This prevents "database is closed" errors during server shutdown.
func TestCoordinator_ApplyFileChanges_ContextCancelled(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Index initial files via events (so they're in the database)
	for i := 0; i < 3; i++ {
		path := filepath.Join(tempDir, fmt.Sprintf("file%d.go", i))
		require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf("package main\nfunc f%d() {}", i)), 0o644))
		events := []watcher.FileEvent{{
			Path:      fmt.Sprintf("file%d.go", i),
			Operation: watcher.OpCreate,
			IsDir:     false,
			Timestamp: time.Now(),
		}}
		require.NoError(t, coord.HandleEvents(ctx, events))
	}

	// 2. Modify files to create changes that need processing
	for i := 0; i < 3; i++ {
		path := filepath.Join(tempDir, fmt.Sprintf("file%d.go", i))
		// Sleep to ensure mtime changes (filesystem precision is ~1s)
		time.Sleep(10 * time.Millisecond)
		require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf("package main\nfunc f%d() { /*modified*/ }", i)), 0o644))
	}

	// 3. Run reconciliation with a context that will be canceled shortly
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// 4. Run reconciliation - may complete or be canceled, both are valid
	// The key is: no panic, no "database is closed" errors, clean exit
	err := coord.ReconcileFilesOnStartup(ctx)

	// Either nil (completed) or context error (canceled gracefully) is acceptable
	if err != nil {
		// If error occurred, it should be a context-related error, not database error
		assert.Contains(t, err.Error(), "context", "error should be context-related, not database closed")
	}
}

// =============================================================================
// Gitignore Reconciliation Edge Case Tests
// =============================================================================

func TestCoordinator_HandleEvents_GitignoreChange_NestedGitignore(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// Create nested directory structure
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0o755))

	// Create files in subdir
	file1 := filepath.Join(subDir, "keep.go")
	file2 := filepath.Join(subDir, "ignore_me.go")
	require.NoError(t, os.WriteFile(file1, []byte("package subdir\nfunc keepMe() {}"), 0o644))
	require.NoError(t, os.WriteFile(file2, []byte("package subdir\nfunc ignoreMe() {}"), 0o644))

	// Index both files in subdir
	createEvents := []watcher.FileEvent{
		{Path: "subdir/keep.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
		{Path: "subdir/ignore_me.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, createEvents))

	// Verify both files indexed
	paths, err := coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, paths, 2)

	// Create nested .gitignore that ignores ignore_me.go
	nestedGitignore := filepath.Join(subDir, ".gitignore")
	require.NoError(t, os.WriteFile(nestedGitignore, []byte("ignore_me.go\n"), 0o644))

	// Trigger nested gitignore change event
	gitignoreEvents := []watcher.FileEvent{
		{Path: "subdir/.gitignore", Operation: watcher.OpGitignoreChange, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, gitignoreEvents))

	// Verify ignore_me.go was removed but keep.go remains
	paths, err = coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, paths, 1, "expected 1 file after nested gitignore")
	assert.Contains(t, paths, "subdir/keep.go")
	assert.NotContains(t, paths, "subdir/ignore_me.go")
}

func TestCoordinator_HandleEvents_GitignoreChange_PatternWithWildcard(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// Create files that can be matched with wildcard patterns
	file1 := filepath.Join(tempDir, "normal.go")
	file2 := filepath.Join(tempDir, "test_generated.go") // Generated file pattern
	require.NoError(t, os.WriteFile(file1, []byte("package main\nfunc normal() {}"), 0o644))
	require.NoError(t, os.WriteFile(file2, []byte("package main\nfunc generated() {}"), 0o644))

	// Index files
	createEvents := []watcher.FileEvent{
		{Path: "normal.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
		{Path: "test_generated.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, createEvents))

	// Verify both indexed
	paths, err := coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, paths, 2)

	// Create .gitignore with wildcard pattern
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	require.NoError(t, os.WriteFile(gitignorePath, []byte("*_generated.go\n"), 0o644))

	// Trigger gitignore change
	gitignoreEvents := []watcher.FileEvent{
		{Path: ".gitignore", Operation: watcher.OpGitignoreChange, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, gitignoreEvents))

	// Verify generated file was removed
	paths, err = coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, paths, 1, "expected 1 file after gitignore")
	assert.Contains(t, paths, "normal.go")
	assert.NotContains(t, paths, "test_generated.go")
}

func TestCoordinator_HandleEvents_GitignoreChange_CommentsOnly(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// Create test file
	file1 := filepath.Join(tempDir, "keep.go")
	require.NoError(t, os.WriteFile(file1, []byte("package main\nfunc keep() {}"), 0o644))

	// Create initial .gitignore with a pattern
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	require.NoError(t, os.WriteFile(gitignorePath, []byte("*.log\n"), 0o644))

	// Index file and cache gitignore content
	createEvents := []watcher.FileEvent{
		{Path: "keep.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, createEvents))

	// Trigger initial gitignore read to cache content
	gitignoreEvents := []watcher.FileEvent{
		{Path: ".gitignore", Operation: watcher.OpGitignoreChange, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, gitignoreEvents))

	// Modify .gitignore with only comment changes (no pattern changes)
	require.NoError(t, os.WriteFile(gitignorePath, []byte("# This is a comment\n*.log\n# Another comment\n"), 0o644))

	// Trigger gitignore change for comment-only modification
	require.NoError(t, coord.HandleEvents(ctx, gitignoreEvents))

	// Verify file still indexed (no reconciliation needed for comment-only changes)
	paths, err := coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, paths, 1)
	assert.Contains(t, paths, "keep.go")
}

func TestCoordinator_HandleEvents_GitignoreChange_EmptyToPatterns(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// Create test files (both .go files since scanner only indexes code files)
	file1 := filepath.Join(tempDir, "keep.go")
	file2 := filepath.Join(tempDir, "ignore_this.go")
	require.NoError(t, os.WriteFile(file1, []byte("package main\nfunc keep() {}"), 0o644))
	require.NoError(t, os.WriteFile(file2, []byte("package main\nfunc ignore() {}"), 0o644))

	// Create empty .gitignore
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	require.NoError(t, os.WriteFile(gitignorePath, []byte(""), 0o644))

	// Index all files
	createEvents := []watcher.FileEvent{
		{Path: "keep.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
		{Path: "ignore_this.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, createEvents))

	// Verify both indexed
	paths, err := coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, paths, 2)

	// Trigger initial gitignore read (empty)
	gitignoreEvents := []watcher.FileEvent{
		{Path: ".gitignore", Operation: watcher.OpGitignoreChange, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, gitignoreEvents))

	// Add pattern to .gitignore
	require.NoError(t, os.WriteFile(gitignorePath, []byte("ignore_this.go\n"), 0o644))

	// Trigger gitignore change with new pattern
	require.NoError(t, coord.HandleEvents(ctx, gitignoreEvents))

	// Verify ignore_this.go was removed
	paths, err = coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, paths, 1)
	assert.Contains(t, paths, "keep.go")
	assert.NotContains(t, paths, "ignore_this.go")
}

// =============================================================================
// determineReconciliationStrategy Tests (DEBT-028: Coverage improvement)
// =============================================================================

// TestDetermineReconciliationStrategy_NestedGitignore tests that nested .gitignore
// files trigger subtree reconciliation strategy.
func TestDetermineReconciliationStrategy_NestedGitignore(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// Create nested directory and .gitignore
	subDir := filepath.Join(tempDir, "src", "components")
	require.NoError(t, os.MkdirAll(subDir, 0o755))

	nestedGitignore := filepath.Join(subDir, ".gitignore")
	require.NoError(t, os.WriteFile(nestedGitignore, []byte("*.test.ts\n"), 0o644))

	// Determine strategy - should be subtree
	strategy := coord.determineReconciliationStrategy(ctx, nestedGitignore)

	assert.Equal(t, reconcileSubtree, strategy.Type, "nested gitignore should trigger subtree reconciliation")
	assert.Equal(t, "src/components", strategy.Scope, "scope should be the nested directory path")
}

// TestDetermineReconciliationStrategy_RootGitignoreOnlyAdded tests that root .gitignore
// with only added patterns triggers pattern diff (no full scan needed).
func TestDetermineReconciliationStrategy_RootGitignoreOnlyAdded(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// Create initial .gitignore
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	require.NoError(t, os.WriteFile(gitignorePath, []byte("*.log\n"), 0o644))

	// Cache the old content (simulating previous reconciliation)
	require.NoError(t, coord.config.Metadata.SetState(ctx, stateGitignoreContent, "*.log\n"))

	// Add new pattern to .gitignore
	require.NoError(t, os.WriteFile(gitignorePath, []byte("*.log\n*.tmp\n"), 0o644))

	// Determine strategy - should be pattern diff since only added
	strategy := coord.determineReconciliationStrategy(ctx, gitignorePath)

	assert.Equal(t, reconcilePatternDiff, strategy.Type, "only added patterns should use pattern diff")
	assert.Contains(t, strategy.AddedPatterns, "*.tmp", "added pattern should be detected")
	assert.Empty(t, strategy.RemovedPatterns, "no patterns should be marked as removed")
}

// TestDetermineReconciliationStrategy_RootGitignoreRemovedPatterns tests that root .gitignore
// with removed patterns triggers full reconciliation (to find newly-unignored files).
func TestDetermineReconciliationStrategy_RootGitignoreRemovedPatterns(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// Create initial .gitignore with multiple patterns
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	require.NoError(t, os.WriteFile(gitignorePath, []byte("*.log\n*.tmp\n"), 0o644))

	// Cache the old content
	require.NoError(t, coord.config.Metadata.SetState(ctx, stateGitignoreContent, "*.log\n*.tmp\n"))

	// Remove a pattern from .gitignore
	require.NoError(t, os.WriteFile(gitignorePath, []byte("*.log\n"), 0o644))

	// Determine strategy - should be full (removed patterns)
	strategy := coord.determineReconciliationStrategy(ctx, gitignorePath)

	assert.Equal(t, reconcileFull, strategy.Type, "removed patterns should trigger full reconciliation")
	assert.Contains(t, strategy.RemovedPatterns, "*.tmp", "removed pattern should be detected")
}

// TestDetermineReconciliationStrategy_NoCachedContent tests that missing cached content
// triggers full reconciliation (first time or cache cleared).
func TestDetermineReconciliationStrategy_NoCachedContent(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// Create .gitignore but don't cache any content
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	require.NoError(t, os.WriteFile(gitignorePath, []byte("*.log\n"), 0o644))

	// No cached content - determineReconciliationStrategy should return full
	strategy := coord.determineReconciliationStrategy(ctx, gitignorePath)

	assert.Equal(t, reconcileFull, strategy.Type, "no cached content should trigger full reconciliation")
}

// TestDetermineReconciliationStrategy_NoPatternChanges tests that comment-only changes
// trigger pattern diff with empty patterns (fast path, no work needed).
func TestDetermineReconciliationStrategy_NoPatternChanges(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// Create .gitignore
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	require.NoError(t, os.WriteFile(gitignorePath, []byte("*.log\n"), 0o644))

	// Cache the content
	require.NoError(t, coord.config.Metadata.SetState(ctx, stateGitignoreContent, "*.log\n"))

	// Update with only comment changes (same patterns)
	require.NoError(t, os.WriteFile(gitignorePath, []byte("# Comment\n*.log\n"), 0o644))

	// Determine strategy - should be pattern diff with empty added patterns
	strategy := coord.determineReconciliationStrategy(ctx, gitignorePath)

	assert.Equal(t, reconcilePatternDiff, strategy.Type, "no pattern changes should use pattern diff")
	assert.Empty(t, strategy.AddedPatterns, "no patterns should be added")
	assert.Empty(t, strategy.RemovedPatterns, "no patterns should be removed")
}

// TestDetermineReconciliationStrategy_GitignoreDeleted tests handling when
// the .gitignore file is deleted or unreadable.
func TestDetermineReconciliationStrategy_GitignoreDeleted(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// Cache previous content
	require.NoError(t, coord.config.Metadata.SetState(ctx, stateGitignoreContent, "*.log\n"))

	// Point to non-existent .gitignore
	gitignorePath := filepath.Join(tempDir, ".gitignore")

	// Determine strategy - should be full (file deleted)
	strategy := coord.determineReconciliationStrategy(ctx, gitignorePath)

	assert.Equal(t, reconcileFull, strategy.Type, "deleted gitignore should trigger full reconciliation")
}

// =============================================================================
// reconcileGitignorePatternDiff Tests (DEBT-028: Coverage improvement)
// =============================================================================

// TestReconcileGitignorePatternDiff_EmptyPatterns tests that empty patterns
// list returns early with no work done.
func TestReconcileGitignorePatternDiff_EmptyPatterns(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// Create and index a file
	file1 := filepath.Join(tempDir, "test.go")
	require.NoError(t, os.WriteFile(file1, []byte("package main\nfunc test() {}"), 0o644))
	events := []watcher.FileEvent{{Path: "test.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()}}
	require.NoError(t, coord.HandleEvents(ctx, events))

	// Verify file is indexed
	paths, err := coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	require.Len(t, paths, 1)

	// Call reconcile with empty patterns
	err = coord.reconcileGitignorePatternDiff(ctx, []string{})
	require.NoError(t, err)

	// File should still be indexed (no work done)
	paths, err = coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, paths, 1, "file should remain indexed with empty patterns")
}

// TestReconcileGitignorePatternDiff_RemovesMatchingFiles tests that files
// matching added patterns are removed from the index.
func TestReconcileGitignorePatternDiff_RemovesMatchingFiles(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// Create and index multiple files
	file1 := filepath.Join(tempDir, "keep.go")
	file2 := filepath.Join(tempDir, "remove_me.generated.go")
	file3 := filepath.Join(tempDir, "also_keep.go")

	require.NoError(t, os.WriteFile(file1, []byte("package main\nfunc keep() {}"), 0o644))
	require.NoError(t, os.WriteFile(file2, []byte("package main\nfunc generated() {}"), 0o644))
	require.NoError(t, os.WriteFile(file3, []byte("package main\nfunc alsoKeep() {}"), 0o644))

	events := []watcher.FileEvent{
		{Path: "keep.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
		{Path: "remove_me.generated.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
		{Path: "also_keep.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, events))

	// Verify all files indexed
	paths, err := coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	require.Len(t, paths, 3)

	// Apply pattern diff with pattern matching generated file
	err = coord.reconcileGitignorePatternDiff(ctx, []string{"*.generated.go"})
	require.NoError(t, err)

	// Verify generated file was removed, others remain
	paths, err = coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, paths, 2, "should have 2 files after pattern diff")
	assert.Contains(t, paths, "keep.go")
	assert.Contains(t, paths, "also_keep.go")
	assert.NotContains(t, paths, "remove_me.generated.go", "generated file should be removed")
}

// =============================================================================
// reconcileGitignoreSubtree Tests (DEBT-028: Coverage improvement)
// =============================================================================

// TestReconcileGitignoreSubtree_AddsNewFiles tests that subtree reconciliation
// adds files that were previously ignored but are now unignored.
func TestReconcileGitignoreSubtree_AddsNewFiles(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// Create subtree with files
	subDir := filepath.Join(tempDir, "lib")
	require.NoError(t, os.MkdirAll(subDir, 0o755))

	// Create .gitignore that initially ignores newfile.go
	gitignorePath := filepath.Join(subDir, ".gitignore")
	require.NoError(t, os.WriteFile(gitignorePath, []byte("newfile.go\n"), 0o644))

	// Create files
	file1 := filepath.Join(subDir, "existing.go")
	file2 := filepath.Join(subDir, "newfile.go") // Initially ignored

	require.NoError(t, os.WriteFile(file1, []byte("package lib\nfunc existing() {}"), 0o644))
	require.NoError(t, os.WriteFile(file2, []byte("package lib\nfunc newFunc() {}"), 0o644))

	// Index only the non-ignored file
	events := []watcher.FileEvent{
		{Path: "lib/existing.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, events))

	// Verify only 1 file indexed
	paths, err := coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	require.Len(t, paths, 1)

	// Remove ignore pattern (make newfile.go visible)
	require.NoError(t, os.WriteFile(gitignorePath, []byte("# empty\n"), 0o644))

	// Invalidate cache and run subtree reconciliation
	coord.config.Scanner.InvalidateGitignoreCache()
	err = coord.reconcileGitignoreSubtree(ctx, "lib")
	require.NoError(t, err)

	// Verify newfile.go is now indexed
	paths, err = coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, paths, 2, "should have 2 files after subtree reconciliation")
	assert.Contains(t, paths, "lib/existing.go")
	assert.Contains(t, paths, "lib/newfile.go", "new file should be added")
}

// TestReconcileGitignoreSubtree_RemovesIgnoredFiles tests that subtree reconciliation
// removes files that are now ignored.
func TestReconcileGitignoreSubtree_RemovesIgnoredFiles(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinatorWithScanner(t)
	defer cleanup()

	ctx := context.Background()

	// Create subtree
	subDir := filepath.Join(tempDir, "pkg")
	require.NoError(t, os.MkdirAll(subDir, 0o755))

	// Create files and index them
	file1 := filepath.Join(subDir, "keep.go")
	file2 := filepath.Join(subDir, "ignore.go")

	require.NoError(t, os.WriteFile(file1, []byte("package pkg\nfunc keep() {}"), 0o644))
	require.NoError(t, os.WriteFile(file2, []byte("package pkg\nfunc ignore() {}"), 0o644))

	events := []watcher.FileEvent{
		{Path: "pkg/keep.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
		{Path: "pkg/ignore.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}
	require.NoError(t, coord.HandleEvents(ctx, events))

	// Verify both indexed
	paths, err := coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	require.Len(t, paths, 2)

	// Create nested .gitignore that ignores ignore.go
	gitignorePath := filepath.Join(subDir, ".gitignore")
	require.NoError(t, os.WriteFile(gitignorePath, []byte("ignore.go\n"), 0o644))

	// Invalidate cache and run subtree reconciliation
	coord.config.Scanner.InvalidateGitignoreCache()
	err = coord.reconcileGitignoreSubtree(ctx, "pkg")
	require.NoError(t, err)

	// Verify ignore.go was removed
	paths, err = coord.config.Metadata.GetFilePathsByProject(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, paths, 1, "should have 1 file after subtree reconciliation")
	assert.Contains(t, paths, "pkg/keep.go")
	assert.NotContains(t, paths, "pkg/ignore.go", "ignored file should be removed")
}

// =============================================================================
// Additional Coordinator Edge Case Tests (DEBT-028)
// =============================================================================

// TestCoordinator_HandleEvents_InvalidPath tests handling of events with invalid paths.
func TestCoordinator_HandleEvents_InvalidPath(t *testing.T) {
	coord, _, cleanup := setupTestCoordinator(t)
	defer cleanup()

	ctx := context.Background()

	// Handle event with path that doesn't exist
	events := []watcher.FileEvent{
		{Path: "nonexistent/path/file.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}

	// Should not error - gracefully handles missing files
	err := coord.HandleEvents(ctx, events)
	assert.NoError(t, err, "should handle missing files gracefully")
}

// TestCoordinator_HandleEvents_EmptyFile tests handling of empty file events.
func TestCoordinator_HandleEvents_EmptyFile(t *testing.T) {
	coord, tempDir, cleanup := setupTestCoordinator(t)
	defer cleanup()

	ctx := context.Background()

	// Create an empty file
	emptyFile := filepath.Join(tempDir, "empty.go")
	require.NoError(t, os.WriteFile(emptyFile, []byte(""), 0o644))

	// Handle create event
	events := []watcher.FileEvent{
		{Path: "empty.go", Operation: watcher.OpCreate, IsDir: false, Timestamp: time.Now()},
	}

	// Should not error
	err := coord.HandleEvents(ctx, events)
	assert.NoError(t, err, "should handle empty files gracefully")
}
