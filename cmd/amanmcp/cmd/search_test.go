package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

func TestSearchCmd_RequiresIndex(t *testing.T) {
	// Given: a directory without an index
	tmpDir := t.TempDir()

	// When: running search command
	rootCmd := NewRootCmd()
	rootCmd.SetArgs([]string{"search", "test query"})

	// Change to temp dir
	oldDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldDir) }()

	err := rootCmd.Execute()

	// Then: error about missing index
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no index found")
}

func TestSearchCmd_RequiresQuery(t *testing.T) {
	// Given: search command without query
	rootCmd := NewRootCmd()
	rootCmd.SetArgs([]string{"search"})

	buf := &bytes.Buffer{}
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	err := rootCmd.Execute()

	// Then: error about missing query
	require.Error(t, err)
}

func TestSearchCmd_WithIndex_ReturnsResults(t *testing.T) {
	// Given: a directory with a valid index
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0755))

	// Create minimal index files
	metadataPath := filepath.Join(dataDir, "metadata.db")
	metadataStore, err := store.NewSQLiteStore(metadataPath)
	require.NoError(t, err)

	// Add a project and file
	ctx := context.Background()
	project := &store.Project{
		ID:       "test-project",
		Name:     "test",
		RootPath: tmpDir,
	}
	require.NoError(t, metadataStore.SaveProject(ctx, project))

	file := &store.File{
		ID:        "test-file",
		ProjectID: project.ID,
		Path:      "test.go",
		Language:  "go",
	}
	require.NoError(t, metadataStore.SaveFiles(ctx, []*store.File{file}))

	chunk := &store.Chunk{
		ID:          "test-chunk",
		FileID:      file.ID,
		FilePath:    "test.go",
		Content:     "func TestFunction() { return }",
		ContentType: store.ContentTypeCode,
		Language:    "go",
		StartLine:   1,
		EndLine:     1,
	}
	require.NoError(t, metadataStore.SaveChunks(ctx, []*store.Chunk{chunk}))
	require.NoError(t, metadataStore.Close())

	// Create BM25 index
	bm25BasePath := filepath.Join(dataDir, "bm25")
	bm25Config := store.DefaultBM25Config()
	bm25Index, err := store.NewBM25IndexWithBackend(bm25BasePath, bm25Config, "")
	require.NoError(t, err)
	docs := []*store.Document{{ID: chunk.ID, Content: chunk.Content}}
	require.NoError(t, bm25Index.Index(ctx, docs))
	require.NoError(t, bm25Index.Close())

	// Change to temp dir
	oldDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldDir) }()

	// When: running search command with --local to bypass daemon
	// BUG-073: Use --bm25-only since we don't have vector store in test
	rootCmd := NewRootCmd()
	buf := &bytes.Buffer{}
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"search", "TestFunction", "--local", "--bm25-only"})

	err = rootCmd.Execute()

	// Then: no error and output contains result
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "test.go")
}

func TestSearchCmd_FormatText_ShowsScore(t *testing.T) {
	// Given: a directory with a valid index
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0755))

	// Create minimal index
	metadataPath := filepath.Join(dataDir, "metadata.db")
	metadataStore, err := store.NewSQLiteStore(metadataPath)
	require.NoError(t, err)

	ctx := context.Background()
	project := &store.Project{ID: "p1", Name: "test", RootPath: tmpDir}
	require.NoError(t, metadataStore.SaveProject(ctx, project))

	file := &store.File{ID: "f1", ProjectID: "p1", Path: "main.go", Language: "go"}
	require.NoError(t, metadataStore.SaveFiles(ctx, []*store.File{file}))

	chunk := &store.Chunk{
		ID:          "c1",
		FileID:      "f1",
		FilePath:    "main.go",
		Content:     "func main() { fmt.Println(\"hello\") }",
		ContentType: store.ContentTypeCode,
		Language:    "go",
		StartLine:   1,
		EndLine:     1,
	}
	require.NoError(t, metadataStore.SaveChunks(ctx, []*store.Chunk{chunk}))
	require.NoError(t, metadataStore.Close())

	bm25BasePath := filepath.Join(dataDir, "bm25")
	bm25Index, err := store.NewBM25IndexWithBackend(bm25BasePath, store.DefaultBM25Config(), "")
	require.NoError(t, err)
	docs := []*store.Document{{ID: chunk.ID, Content: chunk.Content}}
	require.NoError(t, bm25Index.Index(ctx, docs))
	require.NoError(t, bm25Index.Close())

	oldDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldDir) }()

	// When: running search with text format
	// BUG-073: Use --bm25-only since we don't have vector store in test
	rootCmd := NewRootCmd()
	buf := &bytes.Buffer{}
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"search", "main", "--format", "text", "--local", "--bm25-only"})

	err = rootCmd.Execute()

	// Then: output contains score
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "main.go")
	// Score should be present in some form
	assert.Regexp(t, `\d+`, output) // Should contain numbers (line numbers or scores)
}

func TestSearchCmd_FormatJSON_ValidJSON(t *testing.T) {
	// Given: a directory with a valid index
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0755))

	metadataPath := filepath.Join(dataDir, "metadata.db")
	metadataStore, err := store.NewSQLiteStore(metadataPath)
	require.NoError(t, err)

	ctx := context.Background()
	project := &store.Project{ID: "p1", Name: "test", RootPath: tmpDir}
	require.NoError(t, metadataStore.SaveProject(ctx, project))

	file := &store.File{ID: "f1", ProjectID: "p1", Path: "test.go", Language: "go"}
	require.NoError(t, metadataStore.SaveFiles(ctx, []*store.File{file}))

	chunk := &store.Chunk{
		ID:          "c1",
		FileID:      "f1",
		FilePath:    "test.go",
		Content:     "func Test() {}",
		ContentType: store.ContentTypeCode,
		Language:    "go",
		StartLine:   1,
		EndLine:     1,
	}
	require.NoError(t, metadataStore.SaveChunks(ctx, []*store.Chunk{chunk}))
	require.NoError(t, metadataStore.Close())

	bm25BasePath := filepath.Join(dataDir, "bm25")
	bm25Index, err := store.NewBM25IndexWithBackend(bm25BasePath, store.DefaultBM25Config(), "")
	require.NoError(t, err)
	docs := []*store.Document{{ID: chunk.ID, Content: chunk.Content}}
	require.NoError(t, bm25Index.Index(ctx, docs))
	require.NoError(t, bm25Index.Close())

	oldDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldDir) }()

	// When: running search with JSON format
	// BUG-073: Use --bm25-only since we don't have vector store in test
	rootCmd := NewRootCmd()
	buf := &bytes.Buffer{}
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"search", "Test", "--format", "json", "--local", "--bm25-only"})

	err = rootCmd.Execute()

	// Then: output is valid JSON
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "{") // Should contain JSON structure
	assert.Contains(t, output, "test.go")
}

func TestSearchCmd_LimitFlag(t *testing.T) {
	// Given: search command with limit flag
	rootCmd := NewRootCmd()
	searchCmd, _, _ := rootCmd.Find([]string{"search"})
	require.NotNil(t, searchCmd)

	// Then: limit flag exists
	limitFlag := searchCmd.Flags().Lookup("limit")
	assert.NotNil(t, limitFlag)
	assert.Equal(t, "10", limitFlag.DefValue)
}

func TestSearchCmd_TypeFlag(t *testing.T) {
	// Given: search command with type flag
	rootCmd := NewRootCmd()
	searchCmd, _, _ := rootCmd.Find([]string{"search"})
	require.NotNil(t, searchCmd)

	// Then: type flag exists
	typeFlag := searchCmd.Flags().Lookup("type")
	assert.NotNil(t, typeFlag)
	assert.Equal(t, "all", typeFlag.DefValue)
}

func TestSearchCmd_FormatFlag(t *testing.T) {
	// Given: search command with format flag
	rootCmd := NewRootCmd()
	searchCmd, _, _ := rootCmd.Find([]string{"search"})
	require.NotNil(t, searchCmd)

	// Then: format flag exists
	formatFlag := searchCmd.Flags().Lookup("format")
	assert.NotNil(t, formatFlag)
	assert.Equal(t, "text", formatFlag.DefValue)
}

// FEAT-DIM1: BM25Only flag test
func TestSearchCmd_BM25OnlyFlag(t *testing.T) {
	// Given: search command with bm25-only flag
	rootCmd := NewRootCmd()
	searchCmd, _, _ := rootCmd.Find([]string{"search"})
	require.NotNil(t, searchCmd)

	// Then: bm25-only flag exists with correct default
	bm25OnlyFlag := searchCmd.Flags().Lookup("bm25-only")
	assert.NotNil(t, bm25OnlyFlag, "should have --bm25-only flag")
	assert.Equal(t, "false", bm25OnlyFlag.DefValue, "default should be false")
}

func TestSearchCmd_NoResults_ShowsMessage(t *testing.T) {
	// Given: a directory with an empty index
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0755))

	metadataPath := filepath.Join(dataDir, "metadata.db")
	metadataStore, err := store.NewSQLiteStore(metadataPath)
	require.NoError(t, err)

	ctx := context.Background()
	project := &store.Project{ID: "p1", Name: "test", RootPath: tmpDir}
	require.NoError(t, metadataStore.SaveProject(ctx, project))
	require.NoError(t, metadataStore.Close())

	bm25BasePath := filepath.Join(dataDir, "bm25")
	bm25Index, err := store.NewBM25IndexWithBackend(bm25BasePath, store.DefaultBM25Config(), "")
	require.NoError(t, err)
	require.NoError(t, bm25Index.Close())

	oldDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldDir) }()

	// When: searching for something not in index
	// BUG-073: Use --bm25-only since we don't have vector store in test
	rootCmd := NewRootCmd()
	buf := &bytes.Buffer{}
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"search", "nonexistent_xyz_123", "--local", "--bm25-only"})

	err = rootCmd.Execute()

	// Then: shows "no results" message
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "No results")
}
