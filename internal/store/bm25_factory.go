package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// BM25Backend represents the BM25 index backend type.
type BM25Backend string

const (
	// BM25BackendSQLite uses SQLite FTS5 for BM25 search (default).
	// Enables concurrent multi-process access via WAL mode.
	BM25BackendSQLite BM25Backend = "sqlite"

	// BM25BackendBleve uses Bleve v2 for BM25 search (legacy).
	// Has exclusive file locking via BoltDB - single process only.
	BM25BackendBleve BM25Backend = "bleve"
)

// NewBM25IndexWithBackend creates a BM25Index using the specified backend.
// The path should be the base path without extension - the extension will be
// added based on the backend type (.db for SQLite, .bleve for Bleve).
//
// backend options:
//   - "sqlite" (default): SQLite FTS5 with WAL mode for concurrent access
//   - "bleve": Bleve v2 with BoltDB (legacy, single-process only)
//
// If path is empty, creates an in-memory index for testing.
func NewBM25IndexWithBackend(basePath string, config BM25Config, backend string) (BM25Index, error) {
	switch backend {
	case string(BM25BackendSQLite), "":
		// Default to SQLite (concurrent access, pure Go)
		var path string
		if basePath != "" {
			path = basePath + ".db"
		}
		return NewSQLiteBM25Index(path, config)

	case string(BM25BackendBleve):
		// Legacy Bleve backend (single process due to BoltDB lock)
		var path string
		if basePath != "" {
			path = basePath + ".bleve"
		}
		return NewBleveBM25Index(path, config)

	default:
		return nil, fmt.Errorf("unknown BM25 backend: %s (valid options: sqlite, bleve)", backend)
	}
}

// DetectBM25Backend detects which backend an existing index uses based on file existence.
// Returns the detected backend or an empty string if no index exists.
// This is useful for backwards compatibility when opening existing indexes.
func DetectBM25Backend(basePath string) BM25Backend {
	// Check for SQLite first (preferred)
	sqlitePath := basePath + ".db"
	if fileExists(sqlitePath) {
		return BM25BackendSQLite
	}

	// Check for Bleve (legacy)
	blevePath := basePath + ".bleve"
	if dirExists(blevePath) {
		return BM25BackendBleve
	}

	// No existing index
	return ""
}

// GetBM25IndexPath returns the full path to the BM25 index file/directory
// based on the backend type.
func GetBM25IndexPath(dataDir string, backend string) string {
	basePath := filepath.Join(dataDir, "bm25")
	switch backend {
	case string(BM25BackendBleve):
		return basePath + ".bleve"
	default:
		return basePath + ".db"
	}
}

// fileExists checks if a file exists at the given path.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// dirExists checks if a directory exists at the given path.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
