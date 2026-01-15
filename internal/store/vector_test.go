package store

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TS01: Add and Search
func TestHNSWStore_AddAndSearch(t *testing.T) {
	// Given: empty vector store with 4 dimensions
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// And: vectors a=[1,0,0,0], b=[0,1,0,0], c=[0.9,0.1,0,0]
	ids := []string{"a", "b", "c"}
	vectors := [][]float32{
		{1, 0, 0, 0},
		{0, 1, 0, 0},
		{0.9, 0.1, 0, 0},
	}

	// When: I add all vectors
	err = store.Add(context.Background(), ids, vectors)
	require.NoError(t, err)

	// And: I search for query [1,0,0,0] with k=2
	results, err := store.Search(context.Background(), []float32{1, 0, 0, 0}, 2)
	require.NoError(t, err)

	// Then: results are ["a", "c"] in that order (a is exact match, c is similar)
	require.Len(t, results, 2)
	assert.Equal(t, "a", results[0].ID)
	assert.Equal(t, "c", results[1].ID)

	// And: "a" has high score (near exact match)
	assert.Greater(t, results[0].Score, float32(0.99))
}

// TS02: Delete Vector
func TestHNSWStore_Delete(t *testing.T) {
	// Given: a store with vectors "a" and "b"
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ids := []string{"a", "b"}
	vectors := [][]float32{
		{1, 0, 0, 0},
		{0, 1, 0, 0},
	}
	err = store.Add(context.Background(), ids, vectors)
	require.NoError(t, err)

	// When: I delete "a"
	err = store.Delete(context.Background(), []string{"a"})
	require.NoError(t, err)

	// Then: Contains("a") returns false
	assert.False(t, store.Contains("a"))

	// And: Count() returns 1
	assert.Equal(t, 1, store.Count())

	// And: Contains("b") returns true
	assert.True(t, store.Contains("b"))
}

// TS03: Update Vector (add with same ID replaces)
func TestHNSWStore_Update(t *testing.T) {
	// Given: a store with vector "a" = [1,0,0,0]
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	err = store.Add(context.Background(), []string{"a"}, [][]float32{{1, 0, 0, 0}})
	require.NoError(t, err)

	// When: I add "a" = [0,1,0,0]
	err = store.Add(context.Background(), []string{"a"}, [][]float32{{0, 1, 0, 0}})
	require.NoError(t, err)

	// Then: Count() is still 1 (not 2)
	assert.Equal(t, 1, store.Count())

	// And: searching for [0,1,0,0] finds "a" with high score
	results, err := store.Search(context.Background(), []float32{0, 1, 0, 0}, 1)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "a", results[0].ID)
	assert.Greater(t, results[0].Score, float32(0.99))
}

// TS04: Persistence Round-Trip
func TestHNSWStore_Persistence(t *testing.T) {
	// Given: a temporary directory
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "vectors.hnsw")

	// And: a store with vectors "a" and "b"
	cfg := DefaultVectorStoreConfig(4)
	store1, err := NewHNSWStore(cfg)
	require.NoError(t, err)

	ids := []string{"a", "b"}
	vectors := [][]float32{
		{1, 0, 0, 0},
		{0, 1, 0, 0},
	}
	err = store1.Add(context.Background(), ids, vectors)
	require.NoError(t, err)

	// When: I save to disk and close
	err = store1.Save(indexPath)
	require.NoError(t, err)
	err = store1.Close()
	require.NoError(t, err)

	// And: load into a new store
	store2, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store2.Close() }()

	err = store2.Load(indexPath)
	require.NoError(t, err)

	// Then: Count() is 2
	assert.Equal(t, 2, store2.Count())

	// And: Contains("a") is true
	assert.True(t, store2.Contains("a"))

	// And: search results match
	results, err := store2.Search(context.Background(), []float32{1, 0, 0, 0}, 2)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "a", results[0].ID)
}

// TS05: F16 Quantization Quality
func TestHNSWStore_F16Quantization(t *testing.T) {
	// Given: a store with F16 quantization and 768 dimensions
	cfg := VectorStoreConfig{
		Dimensions:     768,
		Quantization:   "f16",
		Metric:         "cos",
		M:              32,
		EfConstruction: 128,
		EfSearch:       64,
	}
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Generate a test vector and normalize it
	vector := make([]float32, 768)
	for i := range vector {
		vector[i] = float32(i) / 768.0
	}
	normalizeVector(vector)

	// When: I add a vector and search for it
	err = store.Add(context.Background(), []string{"test"}, [][]float32{vector})
	require.NoError(t, err)

	results, err := store.Search(context.Background(), vector, 1)
	require.NoError(t, err)

	// Then: the vector is found with score > 0.99 (minimal precision loss)
	require.Len(t, results, 1)
	assert.Equal(t, "test", results[0].ID)
	assert.Greater(t, results[0].Score, float32(0.99))
}

// TS06: Batch Search (multiple queries)
func TestHNSWStore_BatchSearch(t *testing.T) {
	// Given: a store with vectors "a", "b", "c"
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ids := []string{"a", "b", "c"}
	vectors := [][]float32{
		{1, 0, 0, 0},
		{0, 1, 0, 0},
		{0, 0, 1, 0},
	}
	err = store.Add(context.Background(), ids, vectors)
	require.NoError(t, err)

	// When: I search for vectors matching "a" and "b"
	results1, err := store.Search(context.Background(), []float32{1, 0, 0, 0}, 1)
	require.NoError(t, err)
	results2, err := store.Search(context.Background(), []float32{0, 1, 0, 0}, 1)
	require.NoError(t, err)

	// Then: results match expected IDs
	assert.Equal(t, "a", results1[0].ID)
	assert.Equal(t, "b", results2[0].ID)
}

// TS07: Empty Store Search
func TestHNSWStore_EmptySearch(t *testing.T) {
	// Given: an empty store
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// When: I search for any query
	results, err := store.Search(context.Background(), []float32{1, 0, 0, 0}, 10)

	// Then: empty results are returned (no error)
	require.NoError(t, err)
	assert.Empty(t, results)
}

// TS08: Dimension Mismatch on Add
func TestHNSWStore_DimensionMismatch(t *testing.T) {
	// Given: a store configured for 768 dimensions
	cfg := DefaultVectorStoreConfig(768)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// When: I try to add a 256-dimension vector
	err = store.Add(context.Background(), []string{"test"}, [][]float32{make([]float32, 256)})

	// Then: an error is returned describing the mismatch
	require.Error(t, err)
	var dimErr ErrDimensionMismatch
	assert.ErrorAs(t, err, &dimErr)
	assert.Equal(t, 768, dimErr.Expected)
	assert.Equal(t, 256, dimErr.Got)
}

// Additional edge case tests

func TestHNSWStore_AddEmpty(t *testing.T) {
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Adding empty list should be no-op
	err = store.Add(context.Background(), []string{}, [][]float32{})
	require.NoError(t, err)
	assert.Equal(t, 0, store.Count())
}

func TestHNSWStore_DeleteNonExistent(t *testing.T) {
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Deleting non-existent should not error
	err = store.Delete(context.Background(), []string{"nonexistent"})
	require.NoError(t, err)
}

func TestHNSWStore_CloseIdempotent(t *testing.T) {
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)

	// Multiple closes should not error
	err = store.Close()
	require.NoError(t, err)
	err = store.Close()
	require.NoError(t, err)
}

func TestHNSWStore_SearchAfterClose(t *testing.T) {
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)

	err = store.Close()
	require.NoError(t, err)

	// Search after close should error
	_, err = store.Search(context.Background(), []float32{1, 0, 0, 0}, 10)
	require.Error(t, err)
}

func TestHNSWStore_AddAfterClose(t *testing.T) {
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)

	err = store.Close()
	require.NoError(t, err)

	// Add after close should error
	err = store.Add(context.Background(), []string{"a"}, [][]float32{{1, 0, 0, 0}})
	require.Error(t, err)
}

func TestHNSWStore_SearchDimensionMismatch(t *testing.T) {
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Add a vector first
	err = store.Add(context.Background(), []string{"a"}, [][]float32{{1, 0, 0, 0}})
	require.NoError(t, err)

	// Search with wrong dimensions
	_, err = store.Search(context.Background(), []float32{1, 0}, 10)
	require.Error(t, err)
	var dimErr ErrDimensionMismatch
	assert.ErrorAs(t, err, &dimErr)
}

func TestHNSWStore_ContainsAfterDelete(t *testing.T) {
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Add and then delete
	err = store.Add(context.Background(), []string{"a"}, [][]float32{{1, 0, 0, 0}})
	require.NoError(t, err)
	assert.True(t, store.Contains("a"))

	err = store.Delete(context.Background(), []string{"a"})
	require.NoError(t, err)
	assert.False(t, store.Contains("a"))
}

func TestHNSWStore_MismatchedIDsAndVectors(t *testing.T) {
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Different number of IDs and vectors should error
	err = store.Add(context.Background(), []string{"a", "b"}, [][]float32{{1, 0, 0, 0}})
	require.Error(t, err)
}

// FEAT-AI3: Stats tests for background compaction

func TestHNSWStore_Stats_Empty(t *testing.T) {
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	stats := store.Stats()
	assert.Equal(t, 0, stats.ValidIDs)
	assert.Equal(t, 0, stats.GraphNodes)
	assert.Equal(t, 0, stats.Orphans)
}

func TestHNSWStore_Stats_AfterAdd(t *testing.T) {
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Add 3 vectors
	ids := []string{"a", "b", "c"}
	vectors := [][]float32{
		{1, 0, 0, 0},
		{0, 1, 0, 0},
		{0, 0, 1, 0},
	}
	err = store.Add(context.Background(), ids, vectors)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 3, stats.ValidIDs)
	assert.Equal(t, 3, stats.GraphNodes)
	assert.Equal(t, 0, stats.Orphans)
}

func TestHNSWStore_Stats_AfterDelete(t *testing.T) {
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Add 3 vectors
	ids := []string{"a", "b", "c"}
	vectors := [][]float32{
		{1, 0, 0, 0},
		{0, 1, 0, 0},
		{0, 0, 1, 0},
	}
	err = store.Add(context.Background(), ids, vectors)
	require.NoError(t, err)

	// Delete 1 vector (lazy deletion creates orphan)
	err = store.Delete(context.Background(), []string{"a"})
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 2, stats.ValidIDs)    // Only b and c are valid
	assert.Equal(t, 3, stats.GraphNodes)  // All 3 nodes still in graph
	assert.Equal(t, 1, stats.Orphans)     // "a" is now an orphan
}

func TestHNSWStore_Stats_AfterUpdate(t *testing.T) {
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Add vector "a"
	err = store.Add(context.Background(), []string{"a"}, [][]float32{{1, 0, 0, 0}})
	require.NoError(t, err)

	// Update vector "a" (lazy deletion of old, add new)
	err = store.Add(context.Background(), []string{"a"}, [][]float32{{0, 1, 0, 0}})
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 1, stats.ValidIDs)    // Only 1 valid ID
	assert.Equal(t, 2, stats.GraphNodes)  // 2 nodes in graph (old orphaned, new active)
	assert.Equal(t, 1, stats.Orphans)     // Old "a" is orphan
}

func TestHNSWStore_Stats_AfterClose(t *testing.T) {
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)

	err = store.Close()
	require.NoError(t, err)

	// Stats on closed store should return zeros
	stats := store.Stats()
	assert.Equal(t, 0, stats.ValidIDs)
	assert.Equal(t, 0, stats.GraphNodes)
	assert.Equal(t, 0, stats.Orphans)
}

// Helper function for tests - normalizes vector to unit length
func normalizeVector(v []float32) {
	var sumSquares float64
	for _, val := range v {
		sumSquares += float64(val) * float64(val)
	}

	if sumSquares == 0 {
		return
	}

	magnitude := float32(math.Sqrt(sumSquares))
	for i := range v {
		v[i] /= magnitude
	}
}

// Benchmarks

func BenchmarkHNSWStore_Add1K(b *testing.B) {
	cfg := VectorStoreConfig{
		Dimensions:     768,
		Quantization:   "f16",
		Metric:         "cos",
		M:              32,
		EfConstruction: 128,
		EfSearch:       64,
	}

	vectors := generateBenchVectors(1000, 768)
	ids := generateBenchIDs(1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store, _ := NewHNSWStore(cfg)
		_ = store.Add(context.Background(), ids, vectors)
		_ = store.Close()
	}
}

func BenchmarkHNSWStore_Search10K(b *testing.B) {
	cfg := VectorStoreConfig{
		Dimensions:     768,
		Quantization:   "f16",
		Metric:         "cos",
		M:              32,
		EfConstruction: 128,
		EfSearch:       64,
	}

	store, _ := NewHNSWStore(cfg)
	vectors := generateBenchVectors(10000, 768)
	ids := generateBenchIDs(10000)
	_ = store.Add(context.Background(), ids, vectors)

	query := vectors[0]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.Search(context.Background(), query, 10)
	}
	_ = store.Close()
}

func generateBenchVectors(count, dim int) [][]float32 {
	vectors := make([][]float32, count)
	for i := 0; i < count; i++ {
		v := make([]float32, dim)
		for j := 0; j < dim; j++ {
			v[j] = float32(i+j) / float32(dim)
		}
		// Normalize for cosine similarity
		normalizeVector(v)
		vectors[i] = v
	}
	return vectors
}

func generateBenchIDs(count int) []string {
	ids := make([]string, count)
	for i := 0; i < count; i++ {
		ids[i] = fmt.Sprintf("id_%d", i)
	}
	return ids
}

// =============================================================================
// Concurrent Operation Tests (run with -race flag)
// =============================================================================

func TestHNSWStore_ConcurrentAddAndSearch(t *testing.T) {
	// Given: a vector store with some initial data
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Add initial data
	initialIDs := []string{"a", "b"}
	initialVectors := [][]float32{
		{1, 0, 0, 0},
		{0, 1, 0, 0},
	}
	err = store.Add(context.Background(), initialIDs, initialVectors)
	require.NoError(t, err)

	// When: concurrent Add and Search operations
	const goroutines = 10
	const opsPerGoroutine = 50
	done := make(chan bool, goroutines*2)

	// Start search goroutines
	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < opsPerGoroutine; j++ {
				_, _ = store.Search(context.Background(), []float32{1, 0, 0, 0}, 2)
			}
			done <- true
		}()
	}

	// Start add goroutines
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			for j := 0; j < opsPerGoroutine; j++ {
				id := fmt.Sprintf("concurrent_%d_%d", i, j)
				vec := []float32{float32(i), float32(j), 0, 0}
				normalizeVector(vec)
				_ = store.Add(context.Background(), []string{id}, [][]float32{vec})
			}
			done <- true
		}()
	}

	// Then: all operations complete without race conditions
	for i := 0; i < goroutines*2; i++ {
		<-done
	}

	// And: store is in valid state
	assert.True(t, store.Count() > 2, "should have more than initial 2 vectors")
}

func TestHNSWStore_ConcurrentDeleteAndSearch(t *testing.T) {
	// Given: a vector store with data
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Add test data
	ids := make([]string, 100)
	vectors := make([][]float32, 100)
	for i := 0; i < 100; i++ {
		ids[i] = fmt.Sprintf("vec_%d", i)
		vectors[i] = []float32{float32(i), float32(i + 1), float32(i + 2), float32(i + 3)}
		normalizeVector(vectors[i])
	}
	err = store.Add(context.Background(), ids, vectors)
	require.NoError(t, err)

	// When: concurrent Delete and Search operations
	const goroutines = 5
	done := make(chan bool, goroutines*2)

	// Start search goroutines
	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				_, _ = store.Search(context.Background(), []float32{1, 2, 3, 4}, 10)
			}
			done <- true
		}()
	}

	// Start delete goroutines (delete different subsets)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			start := i * 10
			end := start + 10
			for j := start; j < end; j++ {
				id := fmt.Sprintf("vec_%d", j)
				_ = store.Delete(context.Background(), []string{id})
			}
			done <- true
		}()
	}

	// Then: all operations complete without race conditions
	for i := 0; i < goroutines*2; i++ {
		<-done
	}

	// And: store state is consistent (less than 100 vectors)
	assert.True(t, store.Count() < 100, "some vectors should be deleted")
}

func TestHNSWStore_LazyDeletionOrphanCount(t *testing.T) {
	// Given: a vector store
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// When: I add a vector and then update it multiple times (lazy deletion)
	err = store.Add(context.Background(), []string{"a"}, [][]float32{{1, 0, 0, 0}})
	require.NoError(t, err)

	// Update same ID 5 times (creates 5 orphans)
	// Each update adds a slightly different vector
	for i := 0; i < 5; i++ {
		vec := []float32{0.9, 0.1 * float32(i+1), 0, 0}
		err = store.Add(context.Background(), []string{"a"}, [][]float32{vec})
		require.NoError(t, err)
	}

	// Then: Count() should be 1 (logical count)
	assert.Equal(t, 1, store.Count(), "logical count should be 1")

	// And: Stats() should report orphans
	stats := store.Stats()
	assert.True(t, stats.Orphans >= 5, "should have orphans from lazy deletion: got %d", stats.Orphans)

	// And: search should still work correctly (search for vector similar to last update)
	results, err := store.Search(context.Background(), []float32{0.9, 0.5, 0, 0}, 1)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "a", results[0].ID)
}

func TestHNSWStore_PersistenceWithOrphans(t *testing.T) {
	// Given: a temporary directory
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "vectors_orphans.hnsw")

	// And: a store with orphans from updates
	cfg := DefaultVectorStoreConfig(4)
	store1, err := NewHNSWStore(cfg)
	require.NoError(t, err)

	// Create some orphans via updates
	err = store1.Add(context.Background(), []string{"a"}, [][]float32{{1, 0, 0, 0}})
	require.NoError(t, err)
	err = store1.Add(context.Background(), []string{"a"}, [][]float32{{0, 1, 0, 0}}) // update creates orphan
	require.NoError(t, err)
	err = store1.Add(context.Background(), []string{"b"}, [][]float32{{0, 0, 1, 0}})
	require.NoError(t, err)

	// When: I save and reload
	err = store1.Save(indexPath)
	require.NoError(t, err)
	err = store1.Close()
	require.NoError(t, err)

	// And: load into a new store
	store2, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store2.Close() }()

	err = store2.Load(indexPath)
	require.NoError(t, err)

	// Then: logical count is preserved
	assert.Equal(t, 2, store2.Count(), "should have 2 logical vectors")

	// And: search returns correct results
	results, err := store2.Search(context.Background(), []float32{0, 1, 0, 0}, 1)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "a", results[0].ID) // "a" was updated to [0,1,0,0]
}

// =============================================================================
// normalizeVectorInPlace Tests
// =============================================================================

func TestNormalizeVectorInPlace_NormalVector(t *testing.T) {
	// Given: a non-normalized vector
	v := []float32{3, 4, 0, 0}

	// When: normalizing
	normalizeVectorInPlace(v)

	// Then: length should be 1.0
	length := float32(0)
	for _, val := range v {
		length += val * val
	}
	length = float32(math.Sqrt(float64(length)))
	assert.InDelta(t, 1.0, float64(length), 0.0001, "normalized vector should have length 1.0")

	// And: direction is preserved (3:4 ratio)
	assert.InDelta(t, 0.6, float64(v[0]), 0.0001) // 3/5
	assert.InDelta(t, 0.8, float64(v[1]), 0.0001) // 4/5
}

func TestNormalizeVectorInPlace_ZeroVector(t *testing.T) {
	// Given: a zero vector
	v := []float32{0, 0, 0, 0}

	// When: normalizing
	normalizeVectorInPlace(v)

	// Then: vector remains zero (no NaN)
	for _, val := range v {
		assert.False(t, math.IsNaN(float64(val)), "zero vector should not produce NaN")
		assert.Equal(t, float32(0), val, "zero vector elements should remain 0")
	}
}

func TestNormalizeVectorInPlace_AlreadyNormalized(t *testing.T) {
	// Given: an already normalized vector
	v := []float32{1, 0, 0, 0}

	// When: normalizing
	normalizeVectorInPlace(v)

	// Then: vector is unchanged
	assert.InDelta(t, 1.0, float64(v[0]), 0.0001)
	assert.InDelta(t, 0.0, float64(v[1]), 0.0001)
}

func TestNormalizeVectorInPlace_VerySmallVector(t *testing.T) {
	// Given: a very small vector (tests numerical stability)
	v := []float32{1e-10, 1e-10, 1e-10, 1e-10}

	// When: normalizing
	normalizeVectorInPlace(v)

	// Then: no NaN/Inf produced
	for _, val := range v {
		assert.False(t, math.IsNaN(float64(val)), "small vector should not produce NaN")
		assert.False(t, math.IsInf(float64(val), 0), "small vector should not produce Inf")
	}
}

// =============================================================================
// AllIDs Tests (DEBT-028: Coverage improvement)
// =============================================================================

func TestHNSWStore_AllIDs_Empty(t *testing.T) {
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ids := store.AllIDs()
	assert.Empty(t, ids)
}

func TestHNSWStore_AllIDs_WithVectors(t *testing.T) {
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Add vectors
	ids := []string{"v1", "v2", "v3"}
	vectors := [][]float32{
		{1, 0, 0, 0},
		{0, 1, 0, 0},
		{0, 0, 1, 0},
	}
	require.NoError(t, store.Add(context.Background(), ids, vectors))

	// Get all IDs
	allIDs := store.AllIDs()
	assert.Len(t, allIDs, 3)

	// Verify all expected IDs are present
	idSet := make(map[string]bool)
	for _, id := range allIDs {
		idSet[id] = true
	}
	assert.True(t, idSet["v1"])
	assert.True(t, idSet["v2"])
	assert.True(t, idSet["v3"])
}

func TestHNSWStore_AllIDs_AfterDelete(t *testing.T) {
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Add vectors
	ids := []string{"v1", "v2"}
	vectors := [][]float32{
		{1, 0, 0, 0},
		{0, 1, 0, 0},
	}
	require.NoError(t, store.Add(context.Background(), ids, vectors))

	// Delete one
	require.NoError(t, store.Delete(context.Background(), []string{"v1"}))

	// Verify only one remains
	allIDs := store.AllIDs()
	assert.Len(t, allIDs, 1)
	assert.Equal(t, "v2", allIDs[0])
}

func TestHNSWStore_AllIDs_ClosedStore(t *testing.T) {
	cfg := DefaultVectorStoreConfig(4)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)

	// Close the store
	require.NoError(t, store.Close())

	// AllIDs should return nil on closed store
	ids := store.AllIDs()
	assert.Nil(t, ids)
}

// =============================================================================
// ReadHNSWStoreDimensions Tests (DEBT-028: Coverage improvement)
// =============================================================================

func TestReadHNSWStoreDimensions_NonexistentFile(t *testing.T) {
	// Should return 0 for non-existent file (fresh start)
	dim, err := ReadHNSWStoreDimensions("/nonexistent/path/vectors.hnsw")
	require.NoError(t, err)
	assert.Equal(t, 0, dim)
}

func TestReadHNSWStoreDimensions_AfterSave(t *testing.T) {
	tmpDir := t.TempDir()
	vectorPath := filepath.Join(tmpDir, "vectors.hnsw")

	// Create and save a store with known dimensions
	cfg := DefaultVectorStoreConfig(768) // 768 dimensions
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)

	// Add at least one vector so Save writes the metadata
	ids := []string{"test-id"}
	vectors := [][]float32{make([]float32, 768)}
	for i := range vectors[0] {
		vectors[0][i] = float32(i) / 768.0
	}
	require.NoError(t, store.Add(context.Background(), ids, vectors))

	// Save to disk
	require.NoError(t, store.Save(vectorPath))
	require.NoError(t, store.Close())

	// Read dimensions from saved metadata
	dim, err := ReadHNSWStoreDimensions(vectorPath)
	require.NoError(t, err)
	assert.Equal(t, 768, dim)
}

func TestReadHNSWStoreDimensions_DifferentDimensions(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name       string
		dimensions int
	}{
		{"small dimensions", 64},
		{"medium dimensions", 384},
		{"large dimensions", 1024},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vectorPath := filepath.Join(tmpDir, tc.name+".hnsw")

			cfg := DefaultVectorStoreConfig(tc.dimensions)
			store, err := NewHNSWStore(cfg)
			require.NoError(t, err)

			// Add a vector
			ids := []string{"test"}
			vectors := [][]float32{make([]float32, tc.dimensions)}
			require.NoError(t, store.Add(context.Background(), ids, vectors))

			// Save and close
			require.NoError(t, store.Save(vectorPath))
			require.NoError(t, store.Close())

			// Verify dimensions
			dim, err := ReadHNSWStoreDimensions(vectorPath)
			require.NoError(t, err)
			assert.Equal(t, tc.dimensions, dim)
		})
	}
}

// =============================================================================
// distanceToScore Tests (DEBT-028: Coverage improvement)
// =============================================================================

func TestDistanceToScore_Cosine(t *testing.T) {
	tests := []struct {
		distance float32
		expected float32
	}{
		{0.0, 1.0},   // Identical vectors
		{1.0, 0.5},   // Orthogonal
		{2.0, 0.0},   // Opposite vectors
	}

	for _, tc := range tests {
		result := distanceToScore(tc.distance, "cos")
		assert.InDelta(t, tc.expected, result, 0.001, "cosine distance %f", tc.distance)
	}
}

func TestDistanceToScore_L2(t *testing.T) {
	tests := []struct {
		distance float32
		expected float32
	}{
		{0.0, 1.0},                  // Identical
		{1.0, 0.5},                  // distance 1
		{3.0, 0.25},                 // distance 3
	}

	for _, tc := range tests {
		result := distanceToScore(tc.distance, "l2")
		assert.InDelta(t, tc.expected, result, 0.001, "L2 distance %f", tc.distance)
	}
}

func TestDistanceToScore_DefaultMetric(t *testing.T) {
	// Unknown metric defaults to cosine distance formula
	result := distanceToScore(0.5, "unknown")
	expected := float32(1.0 - 0.5/2.0) // = 0.75
	assert.InDelta(t, expected, result, 0.001)
}

// =============================================================================
// DEBT-028: HNSW Save/Load Error Path Tests
// =============================================================================

func TestHNSWStore_Save_ClosedStore(t *testing.T) {
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "closed.hnsw")

	cfg := DefaultVectorStoreConfig(64)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)

	// Add a vector
	err = store.Add(context.Background(), []string{"v1"}, [][]float32{make([]float32, 64)})
	require.NoError(t, err)

	// Close the store
	require.NoError(t, store.Close())

	// When: saving after close
	err = store.Save(indexPath)

	// Then: error is returned
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestHNSWStore_Save_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	// Path with nested non-existent directories
	indexPath := filepath.Join(tmpDir, "nested", "deep", "index.hnsw")

	cfg := DefaultVectorStoreConfig(64)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer store.Close()

	// Add a vector
	err = store.Add(context.Background(), []string{"v1"}, [][]float32{make([]float32, 64)})
	require.NoError(t, err)

	// When: saving to nested path
	err = store.Save(indexPath)

	// Then: succeeds (directories created)
	require.NoError(t, err)

	// Verify files exist
	_, err = os.Stat(indexPath)
	assert.NoError(t, err)
	_, err = os.Stat(indexPath + ".meta")
	assert.NoError(t, err)
}

func TestHNSWStore_Load_ClosedStore(t *testing.T) {
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "test.hnsw")

	// Create and save a store
	cfg := DefaultVectorStoreConfig(64)
	store1, err := NewHNSWStore(cfg)
	require.NoError(t, err)

	err = store1.Add(context.Background(), []string{"v1"}, [][]float32{make([]float32, 64)})
	require.NoError(t, err)
	require.NoError(t, store1.Save(indexPath))
	require.NoError(t, store1.Close())

	// Create new store, close it, then try to load
	store2, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	require.NoError(t, store2.Close())

	// When: loading after close
	err = store2.Load(indexPath)

	// Then: error is returned
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestHNSWStore_Load_NonexistentFile(t *testing.T) {
	cfg := DefaultVectorStoreConfig(64)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer store.Close()

	// When: loading non-existent file
	err = store.Load("/nonexistent/path/index.hnsw")

	// Then: error is returned
	assert.Error(t, err)
}

func TestHNSWStore_Load_CorruptedMeta(t *testing.T) {
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "test.hnsw")

	// Create and save a valid store
	cfg := DefaultVectorStoreConfig(64)
	store1, err := NewHNSWStore(cfg)
	require.NoError(t, err)

	err = store1.Add(context.Background(), []string{"v1"}, [][]float32{make([]float32, 64)})
	require.NoError(t, err)
	require.NoError(t, store1.Save(indexPath))
	require.NoError(t, store1.Close())

	// Corrupt the meta file
	err = os.WriteFile(indexPath+".meta", []byte("invalid gob data"), 0644)
	require.NoError(t, err)

	// Create new store and try to load
	store2, err := NewHNSWStore(cfg)
	require.NoError(t, err)
	defer store2.Close()

	// When: loading with corrupted meta
	err = store2.Load(indexPath)

	// Then: error is returned
	assert.Error(t, err)
}

func TestHNSWStore_Contains_ClosedStore(t *testing.T) {
	cfg := DefaultVectorStoreConfig(64)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)

	// Add a vector
	err = store.Add(context.Background(), []string{"v1"}, [][]float32{make([]float32, 64)})
	require.NoError(t, err)

	// Close the store
	require.NoError(t, store.Close())

	// When: checking contains after close
	contains := store.Contains("v1")

	// Then: returns false (closed store)
	assert.False(t, contains)
}

func TestHNSWStore_Count_ClosedStore(t *testing.T) {
	cfg := DefaultVectorStoreConfig(64)
	store, err := NewHNSWStore(cfg)
	require.NoError(t, err)

	// Add a vector
	err = store.Add(context.Background(), []string{"v1"}, [][]float32{make([]float32, 64)})
	require.NoError(t, err)

	// Close the store
	require.NoError(t, store.Close())

	// When: getting count after close
	count := store.Count()

	// Then: returns 0 (closed store)
	assert.Equal(t, 0, count)
}
