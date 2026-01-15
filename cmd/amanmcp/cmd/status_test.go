package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/store"
	"github.com/Aman-CERP/amanmcp/internal/ui"
)

func TestStatusCmd_NoIndex(t *testing.T) {
	// Given: a directory with no index
	tmpDir := t.TempDir()

	// When: running status command
	cmd := newStatusCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// Change to temp directory
	oldDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldDir) }()
	_ = os.Chdir(tmpDir)

	err := cmd.Execute()

	// Then: returns error about missing index
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no index found")
}

func TestCollectStatus_WithProject(t *testing.T) {
	// Given: a directory with an index
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0755))

	// Create a minimal metadata store
	metadataPath := filepath.Join(dataDir, "metadata.db")
	metadata, err := store.NewSQLiteStore(metadataPath)
	require.NoError(t, err)

	// Save a project with the correct ID
	projectID := hashString(tmpDir)
	project := &store.Project{
		ID:         projectID,
		Name:       "test-project",
		RootPath:   tmpDir,
		FileCount:  10,
		ChunkCount: 50,
		IndexedAt:  time.Now(),
	}
	require.NoError(t, metadata.SaveProject(context.Background(), project))
	require.NoError(t, metadata.Close())

	// When: collecting status
	ctx := context.Background()
	info, err := collectStatus(ctx, tmpDir, dataDir)

	// Then: succeeds and contains correct data
	require.NoError(t, err)
	assert.Equal(t, 10, info.TotalFiles)
	assert.Equal(t, 50, info.TotalChunks)
	assert.NotZero(t, info.MetadataSize)
}

func TestCollectStatus_NoProject(t *testing.T) {
	// Given: a directory with metadata but no project
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0755))

	// Create a minimal metadata store (but don't add any project)
	metadataPath := filepath.Join(dataDir, "metadata.db")
	metadata, err := store.NewSQLiteStore(metadataPath)
	require.NoError(t, err)
	require.NoError(t, metadata.Close())

	// When: collecting status
	ctx := context.Background()
	info, err := collectStatus(ctx, tmpDir, dataDir)

	// Then: succeeds but shows zero counts
	require.NoError(t, err)
	assert.Equal(t, 0, info.TotalFiles)
	assert.Equal(t, 0, info.TotalChunks)
}

func TestStatusRenderer_Output(t *testing.T) {
	// Given: status info
	info := ui.StatusInfo{
		ProjectName:    "my-project",
		TotalFiles:     10,
		TotalChunks:    50,
		LastIndexed:    time.Now(),
		MetadataSize:   1024 * 1024,
		EmbedderType:   "hugot",
		EmbedderStatus: "ready",
		EmbedderModel:  "minilm",
	}

	// When: rendering
	buf := &bytes.Buffer{}
	renderer := ui.NewStatusRenderer(buf, true) // noColor
	err := renderer.Render(info)

	// Then: output contains expected values
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "my-project")
	assert.Contains(t, output, "10")  // File count
	assert.Contains(t, output, "50")  // Chunk count
	assert.Contains(t, output, "hugot")
	assert.Contains(t, output, "ready")
}

func TestStatusRenderer_JSON(t *testing.T) {
	// Given: status info
	info := ui.StatusInfo{
		ProjectName: "json-project",
		TotalFiles:  5,
		TotalChunks: 25,
	}

	// When: rendering as JSON
	buf := &bytes.Buffer{}
	renderer := ui.NewStatusRenderer(buf, false)
	err := renderer.RenderJSON(info)

	// Then: output is valid JSON
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, `"project_name"`)
	assert.Contains(t, output, `"json-project"`)
	assert.Contains(t, output, `"total_files"`)
}

func TestGetFileSize_NonExistent(t *testing.T) {
	// When: getting size of non-existent file
	size := getFileSize("/nonexistent/file.txt")

	// Then: returns 0
	assert.Equal(t, int64(0), size)
}

func TestGetFileSize_Exists(t *testing.T) {
	// Given: a file with known content
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.txt")
	content := []byte("hello world")
	require.NoError(t, os.WriteFile(filePath, content, 0644))

	// When: getting file size
	size := getFileSize(filePath)

	// Then: returns correct size
	assert.Equal(t, int64(len(content)), size)
}

func TestGetDirSize(t *testing.T) {
	// Given: a directory with files
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("aaaa"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("bb"), 0644))

	// When: getting directory size
	size := getDirSize(tmpDir)

	// Then: returns sum of file sizes
	assert.Equal(t, int64(6), size)
}

func TestGetDirSize_NonExistent(t *testing.T) {
	// When: getting size of non-existent directory
	size := getDirSize("/nonexistent/dir")

	// Then: returns 0
	assert.Equal(t, int64(0), size)
}
