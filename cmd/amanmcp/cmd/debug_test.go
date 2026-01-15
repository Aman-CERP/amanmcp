package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

func TestDebugCmd_NoIndex(t *testing.T) {
	// Given: a directory with no index
	tmpDir := t.TempDir()

	// When: running debug command
	cmd := newDebugCmd()
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

func TestDebugCmd_WithIndex(t *testing.T) {
	// Given: a directory with an index
	tmpDir := t.TempDir()
	// Resolve symlinks for macOS temp directory consistency
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
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

	// When: running debug command
	cmd := newDebugCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	oldDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldDir) }()
	_ = os.Chdir(tmpDir)

	err = cmd.Execute()

	// Then: succeeds and output contains expected values
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "AmanMCP Debug Info")
	assert.Contains(t, output, "FILES & CHUNKS")
	assert.Contains(t, output, "10")  // File count
	assert.Contains(t, output, "50")  // Chunk count
	assert.Contains(t, output, "EMBEDDER")
	assert.Contains(t, output, "BM25 INDEX")
	assert.Contains(t, output, "VECTOR STORE")
	assert.Contains(t, output, "STORAGE")
}

func TestDebugCmd_JSON(t *testing.T) {
	// Given: a directory with an index
	tmpDir := t.TempDir()
	// Resolve symlinks for macOS temp directory consistency
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	dataDir := filepath.Join(tmpDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0755))

	// Create a minimal metadata store
	metadataPath := filepath.Join(dataDir, "metadata.db")
	metadata, err := store.NewSQLiteStore(metadataPath)
	require.NoError(t, err)

	projectID := hashString(tmpDir)
	project := &store.Project{
		ID:         projectID,
		Name:       "json-project",
		RootPath:   tmpDir,
		FileCount:  5,
		ChunkCount: 25,
		IndexedAt:  time.Now(),
	}
	require.NoError(t, metadata.SaveProject(context.Background(), project))
	require.NoError(t, metadata.Close())

	// When: running debug command with --json
	cmd := newDebugCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--json"})

	oldDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldDir) }()
	_ = os.Chdir(tmpDir)

	err = cmd.Execute()

	// Then: succeeds and output is valid JSON
	require.NoError(t, err)
	output := buf.String()

	var info DebugInfo
	err = json.Unmarshal([]byte(output), &info)
	require.NoError(t, err)
	assert.Equal(t, 5, info.FileCount)
	assert.Equal(t, 25, info.ChunkCount)
	assert.NotEmpty(t, info.IndexPath)
	assert.NotEmpty(t, info.ProjectRoot)
}

func TestCollectDebugInfo_WithProject(t *testing.T) {
	// Given: a directory with an index
	tmpDir := t.TempDir()
	// Resolve symlinks for macOS temp directory consistency
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	dataDir := filepath.Join(tmpDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0755))

	// Create a minimal metadata store
	metadataPath := filepath.Join(dataDir, "metadata.db")
	metadata, err := store.NewSQLiteStore(metadataPath)
	require.NoError(t, err)

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

	// When: collecting debug info
	ctx := context.Background()
	info, err := collectDebugInfo(ctx, tmpDir, dataDir)

	// Then: succeeds and contains correct data
	require.NoError(t, err)
	assert.Equal(t, dataDir, info.IndexPath)
	assert.Equal(t, tmpDir, info.ProjectRoot)
	assert.Equal(t, 10, info.FileCount)
	assert.Equal(t, 50, info.ChunkCount)
	assert.NotEmpty(t, info.EmbedderProvider)
	assert.NotEmpty(t, info.EmbedderModel)
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		name     string
		time     time.Time
		expected string
	}{
		{
			name:     "zero time",
			time:     time.Time{},
			expected: "unknown",
		},
		{
			name:     "just now",
			time:     time.Now(),
			expected: "just now",
		},
		{
			name:     "1 minute ago",
			time:     time.Now().Add(-time.Minute),
			expected: "1 minute ago",
		},
		{
			name:     "5 minutes ago",
			time:     time.Now().Add(-5 * time.Minute),
			expected: "5 minutes ago",
		},
		{
			name:     "1 hour ago",
			time:     time.Now().Add(-time.Hour),
			expected: "1 hour ago",
		},
		{
			name:     "3 hours ago",
			time:     time.Now().Add(-3 * time.Hour),
			expected: "3 hours ago",
		},
		{
			name:     "1 day ago",
			time:     time.Now().Add(-24 * time.Hour),
			expected: "1 day ago",
		},
		{
			name:     "5 days ago",
			time:     time.Now().Add(-5 * 24 * time.Hour),
			expected: "5 days ago",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatAge(tt.time)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{999, "999"},
		{1000, "1,000"},
		{12345, "12,345"},
		{1234567, "1,234,567"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatNumber(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatLanguages(t *testing.T) {
	tests := []struct {
		name     string
		langs    map[string]float64
		expected string
	}{
		{
			name:     "empty",
			langs:    map[string]float64{},
			expected: "none",
		},
		{
			name:     "single",
			langs:    map[string]float64{"go": 1.0},
			expected: "go (100%)",
		},
		{
			name:     "multiple sorted",
			langs:    map[string]float64{"go": 0.5, "ts": 0.3, "md": 0.2},
			expected: "go (50%), ts (30%), md (20%)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatLanguages(tt.langs)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeExtension(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"go", "go"},
		{"ts", "ts"},
		{"tsx", "ts"},
		{"js", "js"},
		{"jsx", "js"},
		{"mjs", "js"},
		{"yml", "yaml"},
		{"yaml", "yaml"},
		{"htm", "html"},
		{"html", "html"},
		{"md", "md"},
		{"py", "py"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeExtension(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
