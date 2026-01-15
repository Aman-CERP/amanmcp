package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// DEBT-028: BM25 Factory Tests
// =============================================================================

func TestNewBM25IndexWithBackend_SQLite(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "bm25")

	// When: creating with SQLite backend
	index, err := NewBM25IndexWithBackend(basePath, BM25Config{}, "sqlite")
	require.NoError(t, err)
	require.NotNil(t, index)
	defer index.Close()

	// Then: SQLite index is created
	_, err = os.Stat(basePath + ".db")
	assert.NoError(t, err, "SQLite file should exist")
}

func TestNewBM25IndexWithBackend_EmptyBackend(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "bm25")

	// When: creating with empty backend (default)
	index, err := NewBM25IndexWithBackend(basePath, BM25Config{}, "")
	require.NoError(t, err)
	require.NotNil(t, index)
	defer index.Close()

	// Then: SQLite index is created (default)
	_, err = os.Stat(basePath + ".db")
	assert.NoError(t, err, "SQLite file should exist (default backend)")
}

func TestNewBM25IndexWithBackend_Bleve(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "bm25")

	// When: creating with Bleve backend
	index, err := NewBM25IndexWithBackend(basePath, BM25Config{}, "bleve")
	require.NoError(t, err)
	require.NotNil(t, index)
	defer index.Close()

	// Then: Bleve directory is created
	info, err := os.Stat(basePath + ".bleve")
	assert.NoError(t, err, "Bleve directory should exist")
	assert.True(t, info.IsDir(), "Bleve should be a directory")
}

func TestNewBM25IndexWithBackend_InMemory(t *testing.T) {
	// When: creating with empty base path (in-memory)
	index, err := NewBM25IndexWithBackend("", BM25Config{}, "sqlite")
	require.NoError(t, err)
	require.NotNil(t, index)
	defer index.Close()

	// Then: index works for operations
	ctx := t.Context()
	docs := []*Document{{ID: "doc1", Content: "test content"}}
	err = index.Index(ctx, docs)
	assert.NoError(t, err)
}

func TestNewBM25IndexWithBackend_InvalidBackend(t *testing.T) {
	// When: creating with invalid backend
	index, err := NewBM25IndexWithBackend("", BM25Config{}, "invalid")

	// Then: error is returned
	assert.Error(t, err)
	assert.Nil(t, index)
	assert.Contains(t, err.Error(), "unknown BM25 backend")
	assert.Contains(t, err.Error(), "valid options: sqlite, bleve")
}

func TestDetectBM25Backend_SQLite(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "bm25")

	// Given: a SQLite index file exists
	sqlitePath := basePath + ".db"
	f, err := os.Create(sqlitePath)
	require.NoError(t, err)
	f.Close()

	// When: detecting backend
	backend := DetectBM25Backend(basePath)

	// Then: SQLite is detected
	assert.Equal(t, BM25BackendSQLite, backend)
}

func TestDetectBM25Backend_Bleve(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "bm25")

	// Given: a Bleve directory exists
	blevePath := basePath + ".bleve"
	require.NoError(t, os.MkdirAll(blevePath, 0755))

	// When: detecting backend
	backend := DetectBM25Backend(basePath)

	// Then: Bleve is detected
	assert.Equal(t, BM25BackendBleve, backend)
}

func TestDetectBM25Backend_PrefersSQLite(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "bm25")

	// Given: both SQLite and Bleve exist
	sqlitePath := basePath + ".db"
	f, err := os.Create(sqlitePath)
	require.NoError(t, err)
	f.Close()

	blevePath := basePath + ".bleve"
	require.NoError(t, os.MkdirAll(blevePath, 0755))

	// When: detecting backend
	backend := DetectBM25Backend(basePath)

	// Then: SQLite is preferred
	assert.Equal(t, BM25BackendSQLite, backend)
}

func TestDetectBM25Backend_NoIndex(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "bm25")

	// Given: no index exists
	// When: detecting backend
	backend := DetectBM25Backend(basePath)

	// Then: empty string is returned
	assert.Equal(t, BM25Backend(""), backend)
}

func TestGetBM25IndexPath_SQLite(t *testing.T) {
	// When: getting path for SQLite
	path := GetBM25IndexPath("/data/dir", "sqlite")

	// Then: .db extension is used
	assert.Equal(t, "/data/dir/bm25.db", path)
}

func TestGetBM25IndexPath_Bleve(t *testing.T) {
	// When: getting path for Bleve
	path := GetBM25IndexPath("/data/dir", "bleve")

	// Then: .bleve extension is used
	assert.Equal(t, "/data/dir/bm25.bleve", path)
}

func TestGetBM25IndexPath_Default(t *testing.T) {
	// When: getting path for empty/unknown backend
	path := GetBM25IndexPath("/data/dir", "")

	// Then: .db extension is used (default)
	assert.Equal(t, "/data/dir/bm25.db", path)
}

func TestGetBM25IndexPath_UnknownBackend(t *testing.T) {
	// When: getting path for unknown backend
	path := GetBM25IndexPath("/data/dir", "unknown")

	// Then: .db extension is used (default)
	assert.Equal(t, "/data/dir/bm25.db", path)
}

func TestFileExists(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("file exists", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "testfile")
		f, err := os.Create(filePath)
		require.NoError(t, err)
		f.Close()

		assert.True(t, fileExists(filePath))
	})

	t.Run("file does not exist", func(t *testing.T) {
		assert.False(t, fileExists(filepath.Join(tmpDir, "nonexistent")))
	})

	t.Run("directory is not a file", func(t *testing.T) {
		dirPath := filepath.Join(tmpDir, "subdir")
		require.NoError(t, os.MkdirAll(dirPath, 0755))
		assert.False(t, fileExists(dirPath))
	})
}

func TestDirExists(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("directory exists", func(t *testing.T) {
		dirPath := filepath.Join(tmpDir, "subdir")
		require.NoError(t, os.MkdirAll(dirPath, 0755))
		assert.True(t, dirExists(dirPath))
	})

	t.Run("directory does not exist", func(t *testing.T) {
		assert.False(t, dirExists(filepath.Join(tmpDir, "nonexistent")))
	})

	t.Run("file is not a directory", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "testfile")
		f, err := os.Create(filePath)
		require.NoError(t, err)
		f.Close()
		assert.False(t, dirExists(filePath))
	})
}
