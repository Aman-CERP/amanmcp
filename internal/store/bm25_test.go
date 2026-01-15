package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TS01: Basic Indexing and Search
func TestBleveBM25Index_IndexAndSearch_Basic(t *testing.T) {
	// Given: empty index
	idx, err := NewBleveBM25Index("", DefaultBM25Config())
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
func TestBleveBM25Index_Search_FindsCamelCase(t *testing.T) {
	// Given: index with camelCase content
	idx, err := NewBleveBM25Index("", DefaultBM25Config())
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
func TestBleveBM25Index_Search_FindsSnakeCase(t *testing.T) {
	// Given: index with snake_case content
	idx, err := NewBleveBM25Index("", DefaultBM25Config())
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
func TestBleveBM25Index_Search_MultiTermRanking(t *testing.T) {
	// Given: index with documents containing different term combinations
	idx, err := NewBleveBM25Index("", DefaultBM25Config())
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
func TestBleveBM25Index_Search_IDFAffectsRanking(t *testing.T) {
	// Given: index where some terms are rare
	idx, err := NewBleveBM25Index("", DefaultBM25Config())
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
func TestBleveBM25Index_Delete_RemovesDocument(t *testing.T) {
	// Given: index with documents
	idx, err := NewBleveBM25Index("", DefaultBM25Config())
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
func TestBleveBM25Index_Persistence_RoundTrip(t *testing.T) {
	// Given: a temporary directory for the index
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "bm25.bleve")

	// Create and populate index
	idx1, err := NewBleveBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)

	docs := []*Document{{ID: "1", Content: "persistent data storage"}}
	err = idx1.Index(context.Background(), docs)
	require.NoError(t, err)

	// Close the index
	err = idx1.Close()
	require.NoError(t, err)

	// When: reopening the index
	idx2, err := NewBleveBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx2.Close() }()

	// Then: data is persisted
	results, err := idx2.Search(context.Background(), "persistent", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "1", results[0].DocID)
}

// TS08: Empty Query
func TestBleveBM25Index_Search_EmptyQuery(t *testing.T) {
	// Given: index with documents
	idx, err := NewBleveBM25Index("", DefaultBM25Config())
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
func TestBleveBM25Index_Stats_Accuracy(t *testing.T) {
	// Given: index with documents
	idx, err := NewBleveBM25Index("", DefaultBM25Config())
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

// Additional tests for edge cases

func TestBleveBM25Index_Index_EmptyDocs(t *testing.T) {
	// Given: empty document list
	idx, err := NewBleveBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// When: indexing empty list
	err = idx.Index(context.Background(), []*Document{})
	require.NoError(t, err)

	// Then: no error, stats show 0 documents
	stats := idx.Stats()
	assert.Equal(t, 0, stats.DocumentCount)
}

func TestBleveBM25Index_Index_NilDocs(t *testing.T) {
	// Given: nil document list
	idx, err := NewBleveBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// When: indexing nil
	err = idx.Index(context.Background(), nil)
	require.NoError(t, err)
}

func TestBleveBM25Index_Close_Idempotent(t *testing.T) {
	// Given: an index
	idx, err := NewBleveBM25Index("", DefaultBM25Config())
	require.NoError(t, err)

	// When: closing multiple times
	err = idx.Close()
	require.NoError(t, err)

	err = idx.Close()
	require.NoError(t, err) // Should not error
}

func TestBleveBM25Index_Search_AfterClose(t *testing.T) {
	// Given: a closed index
	idx, err := NewBleveBM25Index("", DefaultBM25Config())
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

func TestBleveBM25Index_Search_MatchedTerms(t *testing.T) {
	// Given: index with document
	idx, err := NewBleveBM25Index("", DefaultBM25Config())
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

func TestBleveBM25Index_Delete_NonExistent(t *testing.T) {
	// Given: index with documents
	idx, err := NewBleveBM25Index("", DefaultBM25Config())
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

func TestBleveBM25Index_PersistentPath_CreatesDirectory(t *testing.T) {
	// Given: a path that doesn't exist
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "nested", "dir", "bm25.bleve")

	// When: creating index at that path
	idx, err := NewBleveBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// Then: directory is created
	_, err = os.Stat(indexPath)
	assert.NoError(t, err)
}

// BUG-003: Race Condition Test
// Tests that Load() is safe during concurrent searches.
// The implementation acquires the lock before closing the old index,
// preventing race conditions between Load and Search operations.
func TestBleveBM25Index_ConcurrentLoadAndSearch(t *testing.T) {
	// Given: a disk-based index with data
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "bm25.bleve")

	idx, err := NewBleveBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)

	docs := []*Document{{ID: "1", Content: "concurrent test data"}}
	require.NoError(t, idx.Index(context.Background(), docs))
	require.NoError(t, idx.Close())

	// Reopen for test
	idx, err = NewBleveBM25Index(indexPath, DefaultBM25Config())
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
				// "index is closed" is acceptable during reload
				if err != nil && err.Error() != "index is closed" {
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
					errChan <- err
				}
			}
		}()
	}

	wg.Wait()
	close(errChan)

	// Then: no race-related errors occur
	for err := range errChan {
		t.Errorf("concurrent operation error: %v", err)
	}
}

// Benchmarks

func BenchmarkBleveBM25Index_Index_1K(b *testing.B) {
	docs := generateTestDocs(1000, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx, _ := NewBleveBM25Index("", DefaultBM25Config())
		_ = idx.Index(context.Background(), docs)
		_ = idx.Close()
	}
}

func BenchmarkBleveBM25Index_Index_10K(b *testing.B) {
	docs := generateTestDocs(10000, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx, _ := NewBleveBM25Index("", DefaultBM25Config())
		_ = idx.Index(context.Background(), docs)
		_ = idx.Close()
	}
}

func BenchmarkBleveBM25Index_Search(b *testing.B) {
	idx, _ := NewBleveBM25Index("", DefaultBM25Config())
	docs := generateTestDocs(10000, 100)
	_ = idx.Index(context.Background(), docs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = idx.Search(context.Background(), "getUserById", 10)
	}
	_ = idx.Close()
}

// BUG-049: Index Corruption Detection and Recovery Tests

// TestBleveBM25Index_CorruptedEmptyMetaJSON tests that empty index_meta.json is detected.
func TestBleveBM25Index_CorruptedEmptyMetaJSON(t *testing.T) {
	// Given: a corrupted index with empty index_meta.json
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "bm25.bleve")

	// Create index directory with empty meta file (simulates corruption)
	require.NoError(t, os.MkdirAll(indexPath, 0755))
	metaPath := filepath.Join(indexPath, "index_meta.json")
	require.NoError(t, os.WriteFile(metaPath, []byte{}, 0644))

	// When: opening the corrupted index
	idx, err := NewBleveBM25Index(indexPath, DefaultBM25Config())

	// Then: index opens successfully (corruption was auto-cleared)
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// And: index is functional (can add and search documents)
	docs := []*Document{{ID: "1", Content: "test after recovery"}}
	require.NoError(t, idx.Index(context.Background(), docs))

	results, err := idx.Search(context.Background(), "recovery", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

// TestBleveBM25Index_CorruptedInvalidJSON tests that invalid JSON in index_meta.json is detected.
func TestBleveBM25Index_CorruptedInvalidJSON(t *testing.T) {
	// Given: a corrupted index with invalid JSON
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "bm25.bleve")

	// Create index directory with invalid JSON meta file
	require.NoError(t, os.MkdirAll(indexPath, 0755))
	metaPath := filepath.Join(indexPath, "index_meta.json")
	require.NoError(t, os.WriteFile(metaPath, []byte(`{"truncated`), 0644))

	// When: opening the corrupted index
	idx, err := NewBleveBM25Index(indexPath, DefaultBM25Config())

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

// TestBleveBM25Index_MissingMetaJSON tests that missing index_meta.json in existing dir is detected.
func TestBleveBM25Index_MissingMetaJSON(t *testing.T) {
	// Given: an incomplete index directory (no index_meta.json)
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "bm25.bleve")

	// Create just the directory without any files
	require.NoError(t, os.MkdirAll(indexPath, 0755))
	// Create a store subdir to simulate partial corruption
	require.NoError(t, os.MkdirAll(filepath.Join(indexPath, "store"), 0755))

	// When: opening the corrupted index
	idx, err := NewBleveBM25Index(indexPath, DefaultBM25Config())

	// Then: index opens successfully (corruption was auto-cleared and fresh index created)
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// And: index is functional
	docs := []*Document{{ID: "1", Content: "test after recovery"}}
	require.NoError(t, idx.Index(context.Background(), docs))

	results, err := idx.Search(context.Background(), "recovery", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

// TestBleveBM25Index_ValidIndexNotCleared tests that valid index is not cleared.
func TestBleveBM25Index_ValidIndexNotCleared(t *testing.T) {
	// Given: a valid index with data
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "bm25.bleve")

	idx, err := NewBleveBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)

	docs := []*Document{{ID: "1", Content: "original data"}}
	require.NoError(t, idx.Index(context.Background(), docs))
	require.NoError(t, idx.Close())

	// When: reopening the valid index
	idx, err = NewBleveBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// Then: original data is still present
	results, err := idx.Search(context.Background(), "original", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "1", results[0].DocID)
}

// TestValidateIndexIntegrity tests the validateIndexIntegrity function directly.
func TestValidateIndexIntegrity(t *testing.T) {
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
			name: "valid index is valid",
			setup: func(t *testing.T, path string) {
				require.NoError(t, os.MkdirAll(path, 0755))
				meta := `{"storage":"scorch","index_type":"upside_down"}`
				require.NoError(t, os.WriteFile(filepath.Join(path, "index_meta.json"), []byte(meta), 0644))
			},
			wantError: false,
		},
		{
			name: "empty meta is corrupt",
			setup: func(t *testing.T, path string) {
				require.NoError(t, os.MkdirAll(path, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(path, "index_meta.json"), []byte{}, 0644))
			},
			wantError: true,
			errorMsg:  "empty",
		},
		{
			name: "invalid JSON is corrupt",
			setup: func(t *testing.T, path string) {
				require.NoError(t, os.MkdirAll(path, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(path, "index_meta.json"), []byte(`{invalid`), 0644))
			},
			wantError: true,
			errorMsg:  "corrupt",
		},
		{
			name: "missing meta in existing dir is corrupt",
			setup: func(t *testing.T, path string) {
				require.NoError(t, os.MkdirAll(path, 0755))
			},
			wantError: true,
			errorMsg:  "missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			path := filepath.Join(tmpDir, "test.bleve")

			tt.setup(t, path)

			err := validateIndexIntegrity(path)

			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestIsCorruptionError tests the isCorruptionError function.
func TestIsCorruptionError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "unexpected end of JSON",
			err:      fmt.Errorf("error parsing mapping JSON: unexpected end of JSON input"),
			expected: true,
		},
		{
			name:     "failed to load segment",
			err:      fmt.Errorf("unable to load snapshot, failed to load segment: error"),
			expected: true,
		},
		{
			name:     "error opening bolt",
			err:      fmt.Errorf("error opening bolt segment: file not found"),
			expected: true,
		},
		{
			name:     "no such file or directory",
			err:      fmt.Errorf("open /path/file.zap: no such file or directory"),
			expected: true,
		},
		{
			name:     "normal error",
			err:      fmt.Errorf("connection refused"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCorruptionError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test helpers

func generateTestDocs(count, tokensPerDoc int) []*Document {
	docs := make([]*Document, count)
	words := []string{"user", "auth", "handler", "request", "response", "error", "data", "config", "service", "client"}

	for i := 0; i < count; i++ {
		var content string
		for j := 0; j < tokensPerDoc; j++ {
			content += words[j%len(words)] + " "
		}
		docs[i] = &Document{
			ID:      string(rune('a' + (i % 26))) + string(rune('0' + (i / 26))),
			Content: content,
		}
	}
	return docs
}

// =============================================================================
// AllIDs Tests (DEBT-028: Coverage improvement)
// =============================================================================

func TestBleveBM25Index_AllIDs_Empty(t *testing.T) {
	idx, err := NewBleveBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	ids, err := idx.AllIDs()
	require.NoError(t, err)
	assert.Empty(t, ids)
}

func TestBleveBM25Index_AllIDs_WithDocuments(t *testing.T) {
	idx, err := NewBleveBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// Index some documents
	docs := []*Document{
		{ID: "doc1", Content: "first document"},
		{ID: "doc2", Content: "second document"},
		{ID: "doc3", Content: "third document"},
	}
	require.NoError(t, idx.Index(context.Background(), docs))

	// Get all IDs
	ids, err := idx.AllIDs()
	require.NoError(t, err)
	assert.Len(t, ids, 3)

	// Verify all expected IDs are present
	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	assert.True(t, idSet["doc1"])
	assert.True(t, idSet["doc2"])
	assert.True(t, idSet["doc3"])
}

func TestBleveBM25Index_AllIDs_AfterDelete(t *testing.T) {
	idx, err := NewBleveBM25Index("", DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// Index documents
	docs := []*Document{
		{ID: "doc1", Content: "first document"},
		{ID: "doc2", Content: "second document"},
	}
	require.NoError(t, idx.Index(context.Background(), docs))

	// Delete one
	require.NoError(t, idx.Delete(context.Background(), []string{"doc1"}))

	// Verify only one remains
	ids, err := idx.AllIDs()
	require.NoError(t, err)
	assert.Len(t, ids, 1)
	assert.Equal(t, "doc2", ids[0])
}

func TestBleveBM25Index_AllIDs_ClosedIndex(t *testing.T) {
	idx, err := NewBleveBM25Index("", DefaultBM25Config())
	require.NoError(t, err)

	// Close the index
	require.NoError(t, idx.Close())

	// AllIDs should return error on closed index
	_, err = idx.AllIDs()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

// =============================================================================
// Save Tests (DEBT-028: Coverage improvement)
// =============================================================================

func TestBleveBM25Index_Save(t *testing.T) {
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "bm25.bleve")

	// Create index on disk
	idx, err := NewBleveBM25Index(indexPath, DefaultBM25Config())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// Index some documents
	docs := []*Document{{ID: "doc1", Content: "test content"}}
	require.NoError(t, idx.Index(context.Background(), docs))

	// Save should succeed (no-op for Bleve as it persists automatically)
	err = idx.Save(indexPath)
	require.NoError(t, err)

	// Verify the index directory exists
	_, err = os.Stat(indexPath)
	require.NoError(t, err)
}
