package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSessionName_Valid(t *testing.T) {
	tests := []struct {
		name        string
		sessionName string
	}{
		{"simple lowercase", "myproject"},
		{"with hyphen", "my-project"},
		{"with underscore", "my_project"},
		{"mixed case", "MyProject"},
		{"with numbers", "project123"},
		{"complex valid", "Work-API_v2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSessionName(tt.sessionName)
			assert.NoError(t, err)
		})
	}
}

func TestValidateSessionName_Invalid(t *testing.T) {
	tests := []struct {
		name        string
		sessionName string
		wantErr     string
	}{
		{"empty", "", "session name cannot be empty"},
		{"with slash", "my/project", "session name can only contain"},
		{"with backslash", "my\\project", "session name can only contain"},
		{"with dots", "my..project", "session name can only contain"},
		{"with space", "my project", "session name can only contain"},
		{"too long", string(make([]byte, 65)), "session name too long"},
		{"special chars", "my@project!", "session name can only contain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSessionName(tt.sessionName)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSaveSession_CreatesDirectory(t *testing.T) {
	// Given: a session with a non-existent directory
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "new-session")
	sess := NewSession("new-session", "/home/user/project", sessionDir)

	// When: saving the session
	err := SaveSession(sess)

	// Then: directory is created and session.json exists
	require.NoError(t, err)
	assert.DirExists(t, sessionDir)
	assert.FileExists(t, filepath.Join(sessionDir, "session.json"))
}

func TestSaveSession_WritesValidJSON(t *testing.T) {
	// Given: a session with index stats
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "test-session")
	sess := NewSession("test-session", "/home/user/project", sessionDir)
	sess.IndexStats.FileCount = 100
	sess.IndexStats.ChunkCount = 500

	// When: saving the session
	err := SaveSession(sess)
	require.NoError(t, err)

	// Then: can load it back
	loaded, err := LoadSession(sessionDir)
	require.NoError(t, err)
	assert.Equal(t, sess.Name, loaded.Name)
	assert.Equal(t, sess.ProjectPath, loaded.ProjectPath)
	assert.Equal(t, sess.IndexStats.FileCount, loaded.IndexStats.FileCount)
	assert.Equal(t, sess.IndexStats.ChunkCount, loaded.IndexStats.ChunkCount)
}

func TestLoadSession_ValidJSON(t *testing.T) {
	// Given: a saved session
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "load-test")
	sess := NewSession("load-test", "/path/to/project", sessionDir)
	sess.IndexStats.FileCount = 42
	require.NoError(t, SaveSession(sess))

	// When: loading the session
	loaded, err := LoadSession(sessionDir)

	// Then: session is loaded correctly
	require.NoError(t, err)
	assert.Equal(t, "load-test", loaded.Name)
	assert.Equal(t, "/path/to/project", loaded.ProjectPath)
	assert.Equal(t, 42, loaded.IndexStats.FileCount)
	assert.Equal(t, sessionDir, loaded.SessionDir)
}

func TestLoadSession_MissingFile(t *testing.T) {
	// Given: a non-existent session directory
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "nonexistent")

	// When: loading the session
	_, err := LoadSession(sessionDir)

	// Then: returns error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session.json not found")
}

func TestLoadSession_InvalidJSON(t *testing.T) {
	// Given: a session.json with invalid JSON
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "invalid-json")
	require.NoError(t, os.MkdirAll(sessionDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "session.json"), []byte("not json"), 0644))

	// When: loading the session
	_, err := LoadSession(sessionDir)

	// Then: returns error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse session.json")
}

func TestCalculateDirSize_RecursiveCount(t *testing.T) {
	// Given: a directory with files
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), make([]byte, 1000), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "subdir", "file2.txt"), make([]byte, 500), 0644))

	// When: calculating directory size
	size, err := CalculateDirSize(tmpDir)

	// Then: includes all files
	require.NoError(t, err)
	assert.GreaterOrEqual(t, size, int64(1500))
}

func TestCalculateDirSize_EmptyDir(t *testing.T) {
	// Given: an empty directory
	tmpDir := t.TempDir()

	// When: calculating directory size
	size, err := CalculateDirSize(tmpDir)

	// Then: size is 0
	require.NoError(t, err)
	assert.Equal(t, int64(0), size)
}

func TestCalculateDirSize_NonexistentDir(t *testing.T) {
	// When: calculating size of nonexistent dir
	size, err := CalculateDirSize("/nonexistent/path")

	// Then: returns 0 with no error (graceful handling)
	require.NoError(t, err)
	assert.Equal(t, int64(0), size)
}

func TestCopyIndexFiles_CopiesAllFiles_SQLite(t *testing.T) {
	// Given: source directory with index files (SQLite FTS5 backend)
	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "dest")

	// Create mock index files with SQLite BM25
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "metadata.db"), []byte("sqlite data"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "vectors.hnsw"), []byte("vector data"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "bm25.db"), []byte("fts5 data"), 0644))

	// When: copying index files
	err := CopyIndexFiles(srcDir, dstDir)

	// Then: all files are copied
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dstDir, "metadata.db"))
	assert.FileExists(t, filepath.Join(dstDir, "vectors.hnsw"))
	assert.FileExists(t, filepath.Join(dstDir, "bm25.db"))
}

func TestCopyIndexFiles_CopiesAllFiles_Bleve(t *testing.T) {
	// Given: source directory with index files (Bleve backend - legacy)
	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "dest")

	// Create mock index files with Bleve BM25
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "metadata.db"), []byte("sqlite data"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "vectors.hnsw"), []byte("vector data"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "bm25.bleve"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "bm25.bleve", "index.dat"), []byte("bm25 data"), 0644))

	// When: copying index files
	err := CopyIndexFiles(srcDir, dstDir)

	// Then: all files are copied
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dstDir, "metadata.db"))
	assert.FileExists(t, filepath.Join(dstDir, "vectors.hnsw"))
	assert.FileExists(t, filepath.Join(dstDir, "bm25.bleve", "index.dat"))
}

func TestCopyIndexFiles_SourceNotExist(t *testing.T) {
	// Given: nonexistent source directory
	dstDir := t.TempDir()

	// When: copying from nonexistent source
	err := CopyIndexFiles("/nonexistent/source", dstDir)

	// Then: returns error
	require.Error(t, err)
}

func TestCopyIndexFiles_PartialCopy(t *testing.T) {
	// Given: source with only some index files
	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "partial")

	// Only create metadata.db
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "metadata.db"), []byte("data"), 0644))

	// When: copying index files
	err := CopyIndexFiles(srcDir, dstDir)

	// Then: copies what exists, no error
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dstDir, "metadata.db"))
}

func TestSessionMetadata_TimestampPreservation(t *testing.T) {
	// Given: a session with specific timestamps
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "timestamp-test")
	sess := NewSession("timestamp-test", "/path", sessionDir)

	// Set specific times
	createdAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	lastUsed := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	lastIndexed := time.Date(2025, 1, 1, 11, 0, 0, 0, time.UTC)
	sess.CreatedAt = createdAt
	sess.LastUsed = lastUsed
	sess.IndexStats.LastIndexed = lastIndexed

	// When: saving and loading
	require.NoError(t, SaveSession(sess))
	loaded, err := LoadSession(sessionDir)

	// Then: timestamps are preserved
	require.NoError(t, err)
	assert.True(t, loaded.CreatedAt.Equal(createdAt), "CreatedAt should be preserved")
	assert.True(t, loaded.LastUsed.Equal(lastUsed), "LastUsed should be preserved")
	assert.True(t, loaded.IndexStats.LastIndexed.Equal(lastIndexed), "LastIndexed should be preserved")
}
