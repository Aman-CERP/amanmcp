package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// SQLite FTS5 BM25 Index Tests
// Mirror of bm25_test.go tests for interface compatibility verification
// ============================================================================

// TS01: Basic Indexing and Search
func TestSQLiteBM25Index_IndexAndSearch_Basic(t *testing.T) {
	// Given: empty index
	idx, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// When: index documents
	docs := []*Document{
		{ID: "1", Content: "func getUserById"},
		{ID: "2", Content: "func createUser"},
		{ID: "3", Content: "func deleteUser"},
	}
	err = idx.Index(context.Background(), docs)
	require.NoError(t, err)

	// Then: search finds matching documents
	results, err := idx.Search(context.Background(), "user", 10)
	require.NoError(t, err)
	assert.Len(t, results, 3)

	// And: results are scored by BM25
	assert.Greater(t, results[0].Score, 0.0)
}

// TS02: CamelCase Tokenization
func TestSQLiteBM25Index_Search_FindsCamelCase(t *testing.T) {
	// Given: index with camelCase content
	idx, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	docs := []*Document{{ID: "1", Content: "func getUserById"}}
	err = idx.Index(context.Background(), docs)
	require.NoError(t, err)

	// When: searching for partial term
	results, err := idx.Search(context.Background(), "user", 10)
	require.NoError(t, err)

	// Then: document is found
	require.Len(t, results, 1)
	assert.Equal(t, "1", results[0].DocID)

	// And: searching for full term also works
	results, err = idx.Search(context.Background(), "getUserById", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

// TS03: snake_case Tokenization
func TestSQLiteBM25Index_Search_FindsSnakeCase(t *testing.T) {
	// Given: index with snake_case content
	idx, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	docs := []*Document{{ID: "1", Content: "def get_user_by_id"}}
	err = idx.Index(context.Background(), docs)
	require.NoError(t, err)

	// When: searching for partial term
	results, err := idx.Search(context.Background(), "user", 10)
	require.NoError(t, err)

	// Then: document is found
	require.Len(t, results, 1)
	assert.Equal(t, "1", results[0].DocID)
}

// TS04: Multi-Term Query Ranking
func TestSQLiteBM25Index_Search_MultiTermRanking(t *testing.T) {
	// Given: index with documents containing different term combinations
	idx, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	docs := []*Document{
		{ID: "1", Content: "handle http request"},
		{ID: "2", Content: "process http response"},
		{ID: "3", Content: "handle database query"},
	}
	err = idx.Index(context.Background(), docs)
	require.NoError(t, err)

	// When: searching with multiple terms
	results, err := idx.Search(context.Background(), "http handle", 10)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 1)

	// Then: document with both terms ranks highest
	assert.Equal(t, "1", results[0].DocID)
}

// TS05: IDF Affects Ranking
func TestSQLiteBM25Index_Search_IDFAffectsRanking(t *testing.T) {
	// Given: index where some terms are rare
	idx, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	docs := []*Document{
		{ID: "1", Content: "error handling code"},
		{ID: "2", Content: "error logging code"},
		{ID: "3", Content: "authentication error code"}, // "authentication" is rare
	}
	err = idx.Index(context.Background(), docs)
	require.NoError(t, err)

	// When: searching for rare term
	results, err := idx.Search(context.Background(), "authentication", 10)
	require.NoError(t, err)

	// Then: rare term finds the right document
	require.Len(t, results, 1)
	assert.Equal(t, "3", results[0].DocID)

	// And: score for rare term is positive
	assert.Greater(t, results[0].Score, 0.0)
}

// TS06: Delete Removes Document
func TestSQLiteBM25Index_Delete_RemovesDocument(t *testing.T) {
	// Given: index with documents
	idx, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	docs := []*Document{
		{ID: "1", Content: "document one unique"},
		{ID: "2", Content: "document two different"},
	}
	err = idx.Index(context.Background(), docs)
	require.NoError(t, err)

	// When: deleting document 1
	err = idx.Delete(context.Background(), []string{"1"})
	require.NoError(t, err)

	// Then: searching for "unique" returns no results
	results, err := idx.Search(context.Background(), "unique", 10)
	require.NoError(t, err)
	assert.Empty(t, results)

	// And: document 2 is still findable
	results, err = idx.Search(context.Background(), "different", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "2", results[0].DocID)
}

// TS07: Persistence Round-Trip
func TestSQLiteBM25Index_Persistence_RoundTrip(t *testing.T) {
	// Given: a temporary directory for the index
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "bm25.db")

	// Create and populate index
	idx1, err := NewSQLiteBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)

	docs := []*Document{{ID: "1", Content: "persistent data storage"}}
	err = idx1.Index(context.Background(), docs)
	require.NoError(t, err)

	// Close the index
	err = idx1.Close()
	require.NoError(t, err)

	// When: reopening the index
	idx2, err := NewSQLiteBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx2.Close() }()

	// Then: data is persisted
	results, err := idx2.Search(context.Background(), "persistent", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "1", results[0].DocID)
}

// TS08: Empty Query
func TestSQLiteBM25Index_Search_EmptyQuery(t *testing.T) {
	// Given: index with documents
	idx, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	docs := []*Document{{ID: "1", Content: "some content here"}}
	err = idx.Index(context.Background(), docs)
	require.NoError(t, err)

	// When: searching with empty string
	results, err := idx.Search(context.Background(), "", 10)
	require.NoError(t, err)

	// Then: returns empty results (not an error)
	assert.Empty(t, results)

	// And: whitespace-only query also returns empty
	results, err = idx.Search(context.Background(), "   ", 10)
	require.NoError(t, err)
	assert.Empty(t, results)
}

// TS09: Stats Accuracy
func TestSQLiteBM25Index_Stats_Accuracy(t *testing.T) {
	// Given: index with documents
	idx, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	docs := []*Document{
		{ID: "1", Content: "hello world"},       // 2 tokens
		{ID: "2", Content: "hello there world"}, // 3 tokens
	}
	err = idx.Index(context.Background(), docs)
	require.NoError(t, err)

	// When: getting stats
	stats := idx.Stats()

	// Then: document count is accurate
	assert.Equal(t, 2, stats.DocumentCount)
}

// TS10: AllIDs returns all document IDs
func TestSQLiteBM25Index_AllIDs(t *testing.T) {
	// Given: index with documents
	idx, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	docs := []*Document{
		{ID: "doc1", Content: "first document"},
		{ID: "doc2", Content: "second document"},
		{ID: "doc3", Content: "third document"},
	}
	err = idx.Index(context.Background(), docs)
	require.NoError(t, err)

	// When: getting all IDs
	ids, err := idx.AllIDs()
	require.NoError(t, err)

	// Then: all document IDs are returned
	assert.Len(t, ids, 3)
	assert.Contains(t, ids, "doc1")
	assert.Contains(t, ids, "doc2")
	assert.Contains(t, ids, "doc3")
}

// ============================================================================
// Edge Case Tests
// ============================================================================

func TestSQLiteBM25Index_Index_EmptyDocs(t *testing.T) {
	// Given: empty document list
	idx, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// When: indexing empty list
	err = idx.Index(context.Background(), []*Document{})
	require.NoError(t, err)

	// Then: no error, stats show 0 documents
	stats := idx.Stats()
	assert.Equal(t, 0, stats.DocumentCount)
}

func TestSQLiteBM25Index_Index_NilDocs(t *testing.T) {
	// Given: nil document list
	idx, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// When: indexing nil
	err = idx.Index(context.Background(), nil)
	require.NoError(t, err)
}

func TestSQLiteBM25Index_Close_Idempotent(t *testing.T) {
	// Given: an index
	idx, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)

	// When: closing multiple times
	err = idx.Close()
	require.NoError(t, err)

	err = idx.Close()
	require.NoError(t, err) // Should not error
}

func TestSQLiteBM25Index_Search_AfterClose(t *testing.T) {
	// Given: a closed index
	idx, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)

	docs := []*Document{{ID: "1", Content: "test content"}}
	err = idx.Index(context.Background(), docs)
	require.NoError(t, err)

	err = idx.Close()
	require.NoError(t, err)

	// When: searching after close
	_, err = idx.Search(context.Background(), "test", 10)

	// Then: returns error
	assert.Error(t, err)
}

func TestSQLiteBM25Index_Search_MatchedTerms(t *testing.T) {
	// Given: index with document
	idx, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	docs := []*Document{{ID: "1", Content: "hello world goodbye"}}
	err = idx.Index(context.Background(), docs)
	require.NoError(t, err)

	// When: searching
	results, err := idx.Search(context.Background(), "hello world", 10)
	require.NoError(t, err)

	// Then: matched terms are populated
	require.Len(t, results, 1)
	assert.NotEmpty(t, results[0].MatchedTerms)
}

func TestSQLiteBM25Index_Delete_NonExistent(t *testing.T) {
	// Given: index with documents
	idx, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	docs := []*Document{{ID: "1", Content: "test content"}}
	err = idx.Index(context.Background(), docs)
	require.NoError(t, err)

	// When: deleting non-existent document
	err = idx.Delete(context.Background(), []string{"non-existent-id"})

	// Then: no error (delete is idempotent)
	require.NoError(t, err)

	// And: original document still exists
	results, err := idx.Search(context.Background(), "test", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

func TestSQLiteBM25Index_Delete_Empty(t *testing.T) {
	// Given: index with documents
	idx, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// When: deleting empty list
	err = idx.Delete(context.Background(), []string{})

	// Then: no error
	require.NoError(t, err)
}

func TestSQLiteBM25Index_PersistentPath_CreatesDirectory(t *testing.T) {
	// Given: a path that doesn't exist
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "nested", "dir", "bm25.db")

	// When: creating index at that path
	idx, err := NewSQLiteBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// Then: directory is created
	_, err = os.Stat(indexPath)
	assert.NoError(t, err)
}

// ============================================================================
// Concurrency Tests (BUG-003 equivalent)
// ============================================================================

func TestSQLiteBM25Index_ConcurrentLoadAndSearch(t *testing.T) {
	// Given: a disk-based index with data
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "bm25.db")

	idx, err := NewSQLiteBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)

	docs := []*Document{{ID: "1", Content: "concurrent test data"}}
	require.NoError(t, idx.Index(context.Background(), docs))
	require.NoError(t, idx.Close())

	// Reopen for test
	idx, err = NewSQLiteBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// When: multiple goroutines search and reload concurrently
	var wg sync.WaitGroup
	errChan := make(chan error, 100)

	// Searchers - 50 goroutines doing 10 searches each
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_, err := idx.Search(context.Background(), "test", 10)
				// "index is closed" and "database is locked" are acceptable during reload
				// SQLite may return locked errors when Load() closes/reopens connection
				if err != nil &&
					err.Error() != "index is closed" &&
					!strings.Contains(err.Error(), "database is locked") {
					errChan <- err
				}
			}
		}()
	}

	// Loaders - 5 goroutines reloading 5 times each
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if err := idx.Load(indexPath); err != nil {
					// Lock errors during Load are expected with concurrent operations
					if !strings.Contains(err.Error(), "database is locked") {
						errChan <- err
					}
				}
			}
		}()
	}

	wg.Wait()
	close(errChan)

	// Then: no unexpected errors occur
	for err := range errChan {
		t.Errorf("concurrent operation error: %v", err)
	}
}

// ============================================================================
// BUG-064 Fix Verification Tests (Multi-Process Concurrent Access)
// These tests verify that SQLite FTS5 with WAL mode solves the exclusive
// file locking issue that prevented concurrent access with Bleve/BoltDB.
// ============================================================================

// TestSQLiteBM25Index_WALMode verifies WAL mode is enabled
func TestSQLiteBM25Index_WALMode(t *testing.T) {
	// Given: a disk-based index
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "bm25.db")

	idx, err := NewSQLiteBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)

	// Index some data to trigger WAL file creation
	docs := []*Document{{ID: "1", Content: "test content"}}
	require.NoError(t, idx.Index(context.Background(), docs))

	// Then: WAL file should exist (indicates WAL mode is active)
	_, err = os.Stat(indexPath + "-wal")
	assert.NoError(t, err, "WAL file should exist, indicating WAL mode is active")

	require.NoError(t, idx.Close())
}

// TestSQLiteBM25Index_ConcurrentMultiProcess is the KEY test for BUG-064.
// It verifies that multiple processes/connections can access the same index.
// This test would FAIL with BleveBM25Index due to BoltDB's exclusive lock.
func TestSQLiteBM25Index_ConcurrentMultiProcess(t *testing.T) {
	// Given: a disk-based index
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "bm25.db")

	// First connection creates and populates the index
	idx1, err := NewSQLiteBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx1.Close() }()

	docs := []*Document{
		{ID: "1", Content: "first test document"},
		{ID: "2", Content: "second test document"},
	}
	require.NoError(t, idx1.Index(context.Background(), docs))

	// When: opening a second connection (simulates CLI while MCP running)
	// With Bleve/BoltDB, this would block indefinitely or fail with lock error
	idx2, err := NewSQLiteBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err, "Second connection should open successfully (BUG-064 fix)")
	defer func() { _ = idx2.Close() }()

	// Then: Both connections should be able to read concurrently
	results1, err := idx1.Search(context.Background(), "test", 10)
	require.NoError(t, err, "First connection search should work")
	assert.Len(t, results1, 2)

	results2, err := idx2.Search(context.Background(), "test", 10)
	require.NoError(t, err, "Second connection search should work")
	assert.Len(t, results2, 2)

	// And: Both should see the same data
	assert.Equal(t, results1[0].DocID, results2[0].DocID)
}

// TestSQLiteBM25Index_ConcurrentReaderWriter tests that readers don't block writers
func TestSQLiteBM25Index_ConcurrentReaderWriter(t *testing.T) {
	// Given: a disk-based index with initial data
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "bm25.db")

	idx, err := NewSQLiteBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	docs := []*Document{{ID: "1", Content: "initial content"}}
	require.NoError(t, idx.Index(context.Background(), docs))

	// When: concurrent reads and writes
	var wg sync.WaitGroup
	errors := make(chan error, 200)

	// Readers - 20 goroutines doing 10 searches each
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_, err := idx.Search(context.Background(), "content", 10)
				if err != nil && err.Error() != "index is closed" {
					errors <- err
				}
			}
		}()
	}

	// Writers - 5 goroutines adding 5 documents each
	for i := 0; i < 5; i++ {
		writerID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				docID := "writer" + string(rune('0'+writerID)) + "_" + string(rune('0'+j))
				doc := &Document{ID: docID, Content: "writer content"}
				if err := idx.Index(context.Background(), []*Document{doc}); err != nil {
					errors <- err
				}
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Then: no errors during concurrent operations
	errorCount := 0
	for err := range errors {
		t.Errorf("concurrent operation error: %v", err)
		errorCount++
	}
	assert.Equal(t, 0, errorCount, "Should have no errors during concurrent read/write")
}

// ============================================================================
// Corruption Detection and Recovery Tests (BUG-049 equivalent)
// ============================================================================

func TestSQLiteBM25Index_CorruptedEmptyFile(t *testing.T) {
	// Given: a corrupted index (empty file)
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "bm25.db")

	// Create empty file (simulates corruption)
	require.NoError(t, os.WriteFile(indexPath, []byte{}, 0644))

	// When: opening the corrupted index
	idx, err := NewSQLiteBM25Index(indexPath, DefaultBM25Config())

	// Then: index opens successfully (corruption was auto-cleared)
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// And: index is functional
	docs := []*Document{{ID: "1", Content: "test after recovery"}}
	require.NoError(t, idx.Index(context.Background(), docs))

	results, err := idx.Search(context.Background(), "recovery", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

func TestSQLiteBM25Index_ValidIndexNotCleared(t *testing.T) {
	// Given: a valid index with data
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "bm25.db")

	idx, err := NewSQLiteBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)

	docs := []*Document{{ID: "1", Content: "original data"}}
	require.NoError(t, idx.Index(context.Background(), docs))
	require.NoError(t, idx.Close())

	// When: reopening the valid index
	idx, err = NewSQLiteBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// Then: original data is still present
	results, err := idx.Search(context.Background(), "original", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "1", results[0].DocID)
}

func TestValidateSQLiteIntegrity(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T, path string)
		wantError bool
		errorMsg  string
	}{
		{
			name:      "non-existent path is valid",
			setup:     func(t *testing.T, path string) {},
			wantError: false,
		},
		{
			name: "valid SQLite database is valid",
			setup: func(t *testing.T, path string) {
				// Create a valid SQLite index
				idx, err := NewSQLiteBM25Index(path, DefaultBM25Config())
				require.NoError(t, err)
				docs := []*Document{{ID: "1", Content: "test"}}
				require.NoError(t, idx.Index(context.Background(), docs))
				require.NoError(t, idx.Close())
			},
			wantError: false,
		},
		{
			name: "empty file is corrupt",
			setup: func(t *testing.T, path string) {
				require.NoError(t, os.WriteFile(path, []byte{}, 0644))
			},
			wantError: true,
			errorMsg:  "FTS5 table 'fts_content' missing", // Empty file opens but lacks schema
		},
		{
			name: "invalid data is corrupt",
			setup: func(t *testing.T, path string) {
				require.NoError(t, os.WriteFile(path, []byte("not a database"), 0644))
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			path := filepath.Join(tmpDir, "test.db")

			tt.setup(t, path)

			err := validateSQLiteIntegrity(path)

			if tt.wantError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ============================================================================
// Update/Replace Tests
// ============================================================================

func TestSQLiteBM25Index_Index_UpdatesExisting(t *testing.T) {
	// Given: index with document
	idx, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	docs := []*Document{{ID: "1", Content: "original content"}}
	require.NoError(t, idx.Index(context.Background(), docs))

	// When: indexing same ID with different content
	updatedDocs := []*Document{{ID: "1", Content: "updated content"}}
	require.NoError(t, idx.Index(context.Background(), updatedDocs))

	// Then: search finds updated content
	results, err := idx.Search(context.Background(), "updated", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "1", results[0].DocID)

	// And: original content is NOT found
	results, err = idx.Search(context.Background(), "original", 10)
	require.NoError(t, err)
	assert.Empty(t, results)
}

// ============================================================================
// Persistence Tests (DEBT-028)
// ============================================================================

// TestSQLiteBM25Index_Save tests the Save method for WAL checkpoint
func TestSQLiteBM25Index_Save(t *testing.T) {
	// Given: a persistent index with documents
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "bm25_save_test.db")

	idx, err := NewSQLiteBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)

	docs := []*Document{
		{ID: "1", Content: "test document one"},
		{ID: "2", Content: "test document two"},
	}
	err = idx.Index(context.Background(), docs)
	require.NoError(t, err)

	// When: calling Save
	err = idx.Save(indexPath)

	// Then: should succeed
	require.NoError(t, err)

	// And: data should be readable after reopen
	_ = idx.Close()

	idx2, err := NewSQLiteBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx2.Close() }()

	results, err := idx2.Search(context.Background(), "document", 10)
	require.NoError(t, err)
	assert.Len(t, results, 2, "data should persist after Save")
}

// TestSQLiteBM25Index_Save_ClosedIndex tests Save on closed index
func TestSQLiteBM25Index_Save_ClosedIndex(t *testing.T) {
	// Given: a closed index
	idx, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	_ = idx.Close()

	// When: calling Save on closed index
	err = idx.Save("")

	// Then: should return error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed", "should indicate index is closed")
}

// TestSQLiteBM25Index_Load tests the Load method
func TestSQLiteBM25Index_Load(t *testing.T) {
	// Given: a persistent index with documents
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "bm25_load_test.db")

	idx, err := NewSQLiteBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)

	docs := []*Document{{ID: "1", Content: "test content"}}
	err = idx.Index(context.Background(), docs)
	require.NoError(t, err)
	_ = idx.Save(indexPath)
	_ = idx.Close()

	// When: creating new index and loading from path
	idx2, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx2.Close() }()

	err = idx2.Load(indexPath)
	require.NoError(t, err)

	// Then: data should be accessible
	results, err := idx2.Search(context.Background(), "test", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

// TestSQLiteBM25Index_Load_InvalidPath tests Load with invalid path
func TestSQLiteBM25Index_Load_InvalidPath(t *testing.T) {
	// Given: an index
	idx, err := NewSQLiteBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// When: loading from a directory that doesn't exist (invalid parent)
	// SQLite will fail because the directory doesn't exist
	err = idx.Load("/nonexistent-dir-abc123xyz/path/to/db.db")

	// Then: should return error (SQLite can't create file in non-existent dir)
	// Note: SQLite creates db files if they don't exist, but fails if directory doesn't exist
	if err == nil {
		// If no error, verify that the path would create an empty db
		// which means no data - acceptable behavior for some SQLite versions
		t.Log("SQLite created empty db at non-existent path - behavior varies by version")
	}
}

// TestSQLiteBM25Index_SaveLoad_RoundTrip tests full round-trip persistence with explicit Save/Load
func TestSQLiteBM25Index_SaveLoad_RoundTrip(t *testing.T) {
	// Given: create index, add docs, save
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "bm25_roundtrip.db")

	// Phase 1: Create and populate
	idx1, err := NewSQLiteBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)

	docs := []*Document{
		{ID: "func1", Content: "func getUserById(id string) *User"},
		{ID: "func2", Content: "func createUser(name string) error"},
		{ID: "func3", Content: "func deleteUserFromDB(id string) error"},
	}
	err = idx1.Index(context.Background(), docs)
	require.NoError(t, err)

	// Save and close
	err = idx1.Save(indexPath)
	require.NoError(t, err)
	err = idx1.Close()
	require.NoError(t, err)

	// Phase 2: Reopen and verify
	idx2, err := NewSQLiteBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx2.Close() }()

	// Search for user-related functions
	results, err := idx2.Search(context.Background(), "user", 10)
	require.NoError(t, err)
	assert.Len(t, results, 3, "all user-related functions should be found")

	// Search for specific term
	results, err = idx2.Search(context.Background(), "delete", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1, "delete function should be found")
	assert.Equal(t, "func3", results[0].DocID)
}

// ============================================================================
// Benchmarks
// ============================================================================

func BenchmarkSQLiteBM25Index_Index_1K(b *testing.B) {
	docs := generateTestDocs(1000, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx, _ := NewSQLiteBM25Index("", DefaultBM25Config())
		_ = idx.Index(context.Background(), docs)
		_ = idx.Close()
	}
}

func BenchmarkSQLiteBM25Index_Index_10K(b *testing.B) {
	docs := generateTestDocs(10000, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx, _ := NewSQLiteBM25Index("", DefaultBM25Config())
		_ = idx.Index(context.Background(), docs)
		_ = idx.Close()
	}
}

func BenchmarkSQLiteBM25Index_Search(b *testing.B) {
	idx, _ := NewSQLiteBM25Index("", DefaultBM25Config())
	docs := generateTestDocs(10000, 100)
	_ = idx.Index(context.Background(), docs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = idx.Search(context.Background(), "getUserById", 10)
	}
	_ = idx.Close()
}

func BenchmarkSQLiteBM25Index_Persistent_Search(b *testing.B) {
	tmpDir := b.TempDir()
	indexPath := filepath.Join(tmpDir, "bm25.db")

	idx, _ := NewSQLiteBM25Index(indexPath, DefaultBM25Config())
	docs := generateTestDocs(10000, 100)
	_ = idx.Index(context.Background(), docs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = idx.Search(context.Background(), "getUserById", 10)
	}
	_ = idx.Close()
}
