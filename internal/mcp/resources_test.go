package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

// TS03: Read Indexed File
func TestServer_HandleReadResource_ReturnsContent(t *testing.T) {
	// Given: a temp directory with a file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "src", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(testFile), 0755))
	require.NoError(t, os.WriteFile(testFile, []byte("package main\n\nfunc main() {}"), 0644))

	// And: a server with the file indexed
	engine := &MockSearchEngine{}
	metadata := &MockMetadataStore{
		Files: []*store.File{
			{ID: "file-1", ProjectID: "proj-1", Path: "src/main.go", Size: 30, Language: "go"},
		},
	}
	metadata.GetFileByPathFn = func(_ context.Context, _, path string) (*store.File, error) {
		if path == "src/main.go" {
			return metadata.Files[0], nil
		}
		return nil, nil
	}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)
	srv.projectID = "proj-1"
	srv.rootPath = tmpDir

	// When: reading the resource
	result, err := srv.handleReadResource(context.Background(), "src/main.go")

	// Then: content is returned with MIME type
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Contents, 1)
	assert.Contains(t, result.Contents[0].Text, "package main")
	assert.Equal(t, "text/x-go", result.Contents[0].MIMEType)
}

// TS05: Read Non-Existent File
func TestServer_HandleReadResource_FileNotFound(t *testing.T) {
	// Given: a server with an indexed file that no longer exists on disk
	tmpDir := t.TempDir()
	engine := &MockSearchEngine{}
	metadata := &MockMetadataStore{
		Files: []*store.File{
			{ID: "file-1", ProjectID: "proj-1", Path: "deleted.go", Size: 100, Language: "go"},
		},
	}
	metadata.GetFileByPathFn = func(_ context.Context, _, path string) (*store.File, error) {
		if path == "deleted.go" {
			return metadata.Files[0], nil
		}
		return nil, nil
	}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)
	srv.projectID = "proj-1"
	srv.rootPath = tmpDir

	// When: reading the resource
	_, err = srv.handleReadResource(context.Background(), "deleted.go")

	// Then: error is returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TS04: Read Non-Indexed File
func TestServer_HandleReadResource_NotIndexed(t *testing.T) {
	// Given: a server
	tmpDir := t.TempDir()
	engine := &MockSearchEngine{}
	metadata := &MockMetadataStore{}
	metadata.GetFileByPathFn = func(_ context.Context, _, _ string) (*store.File, error) {
		return nil, nil // File not in index
	}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)
	srv.projectID = "proj-1"
	srv.rootPath = tmpDir

	// When: reading a non-indexed file
	_, err = srv.handleReadResource(context.Background(), "not-indexed.go")

	// Then: error is returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not indexed")
}

// TS06: Path Traversal Prevention
func TestServer_HandleReadResource_PathTraversal(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "parent traversal", path: "../../../etc/passwd"},
		{name: "absolute path", path: "/etc/passwd"},
		{name: "hidden traversal", path: "src/../../../etc/passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			engine := &MockSearchEngine{}
			metadata := &MockMetadataStore{}
			cfg := config.NewConfig()

			srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
			require.NoError(t, err)
			srv.projectID = "proj-1"
			srv.rootPath = tmpDir

			// When: attempting path traversal
			_, err = srv.handleReadResource(context.Background(), tt.path)

			// Then: error is returned
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid path")
		})
	}
}

// TS07: Large File Rejection
func TestServer_HandleReadResource_LargeFileRejection(t *testing.T) {
	// Given: a large file (>1MB)
	tmpDir := t.TempDir()
	largeFile := filepath.Join(tmpDir, "large.txt")
	largeContent := make([]byte, 1024*1024+1) // 1MB + 1 byte
	for i := range largeContent {
		largeContent[i] = 'x'
	}
	require.NoError(t, os.WriteFile(largeFile, largeContent, 0644))

	engine := &MockSearchEngine{}
	metadata := &MockMetadataStore{
		Files: []*store.File{
			{ID: "file-large", ProjectID: "proj-1", Path: "large.txt", Size: int64(len(largeContent))},
		},
	}
	metadata.GetFileByPathFn = func(_ context.Context, _, path string) (*store.File, error) {
		if path == "large.txt" {
			return metadata.Files[0], nil
		}
		return nil, nil
	}
	cfg := config.NewConfig()

	srv, err := NewServer(engine, metadata, &MockEmbedder{}, cfg, "")
	require.NoError(t, err)
	srv.projectID = "proj-1"
	srv.rootPath = tmpDir

	// When: reading the large file
	_, err = srv.handleReadResource(context.Background(), "large.txt")

	// Then: error about size limit is returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

// Test isValidPath
func TestIsValidPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{name: "simple path", path: "main.go", expected: true},
		{name: "nested path", path: "src/internal/mcp/server.go", expected: true},
		{name: "parent traversal", path: "../etc/passwd", expected: false},
		{name: "hidden parent", path: "src/../../../etc/passwd", expected: false},
		{name: "absolute path", path: "/etc/passwd", expected: false},
		{name: "windows absolute", path: "C:\\Windows\\System32", expected: false},
		{name: "double dot in name", path: "file..go", expected: true}, // This is valid
		{name: "empty path", path: "", expected: false},
	}

	srv := &Server{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := srv.isValidPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test humanSize
func TestHumanSize(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1572864, "1.5 MB"},
		{1073741824, "1.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := humanSize(tt.bytes)
			assert.Equal(t, tt.expected, result)
		})
	}
}
