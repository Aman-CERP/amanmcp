package indexer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

// MockBM25Store implements store.BM25Index for testing.
// Uses function pointers for behavior injection.
type MockBM25Store struct {
	IndexFn   func(ctx context.Context, docs []*store.Document) error
	SearchFn  func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error)
	DeleteFn  func(ctx context.Context, docIDs []string) error
	AllIDsFn  func() ([]string, error)
	StatsFn   func() *store.IndexStats
	SaveFn    func(path string) error
	LoadFn    func(path string) error
	CloseFn   func() error

	// Call tracking
	indexCalled  atomic.Int32
	deleteCalled atomic.Int32
	closeCalled  atomic.Int32
}

func (m *MockBM25Store) Index(ctx context.Context, docs []*store.Document) error {
	m.indexCalled.Add(1)
	if m.IndexFn != nil {
		return m.IndexFn(ctx, docs)
	}
	return nil
}

func (m *MockBM25Store) Search(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
	if m.SearchFn != nil {
		return m.SearchFn(ctx, query, limit)
	}
	return nil, nil
}

func (m *MockBM25Store) Delete(ctx context.Context, docIDs []string) error {
	m.deleteCalled.Add(1)
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, docIDs)
	}
	return nil
}

func (m *MockBM25Store) AllIDs() ([]string, error) {
	if m.AllIDsFn != nil {
		return m.AllIDsFn()
	}
	return nil, nil
}

func (m *MockBM25Store) Stats() *store.IndexStats {
	if m.StatsFn != nil {
		return m.StatsFn()
	}
	return &store.IndexStats{
		DocumentCount: 0,
		TermCount:     0,
		AvgDocLength:  0,
	}
}

func (m *MockBM25Store) Save(path string) error {
	if m.SaveFn != nil {
		return m.SaveFn(path)
	}
	return nil
}

func (m *MockBM25Store) Load(path string) error {
	if m.LoadFn != nil {
		return m.LoadFn(path)
	}
	return nil
}

func (m *MockBM25Store) Close() error {
	m.closeCalled.Add(1)
	if m.CloseFn != nil {
		return m.CloseFn()
	}
	return nil
}

// Ensure MockBM25Store implements store.BM25Index
var _ store.BM25Index = (*MockBM25Store)(nil)

// =============================================================================
// Constructor Tests
// =============================================================================

func TestNewBM25Indexer_WithStore_Success(t *testing.T) {
	// Given: a mock BM25 store
	mockStore := &MockBM25Store{}

	// When: creating a new BM25Indexer with the store
	indexer, err := NewBM25Indexer(WithStore(mockStore))

	// Then: indexer is created without error
	require.NoError(t, err)
	require.NotNil(t, indexer)
	defer func() { _ = indexer.Close() }()
}

func TestNewBM25Indexer_NilStore_ReturnsError(t *testing.T) {
	// Given: no store provided

	// When: creating a new BM25Indexer without a store
	indexer, err := NewBM25Indexer()

	// Then: an error is returned
	require.Error(t, err)
	assert.Nil(t, indexer)
	assert.Contains(t, err.Error(), "store")
}

// =============================================================================
// Index Tests
// =============================================================================

func TestBM25Indexer_Index_Basic(t *testing.T) {
	// Given: an indexer with a mock store that tracks calls
	var capturedDocs []*store.Document
	mockStore := &MockBM25Store{
		IndexFn: func(ctx context.Context, docs []*store.Document) error {
			capturedDocs = docs
			return nil
		},
	}
	indexer, err := NewBM25Indexer(WithStore(mockStore))
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: indexing chunks
	chunks := []*store.Chunk{
		{ID: "chunk1", Content: "func main() {}"},
		{ID: "chunk2", Content: "type User struct {}"},
	}
	err = indexer.Index(context.Background(), chunks)

	// Then: store receives converted documents
	require.NoError(t, err)
	require.Len(t, capturedDocs, 2)
	assert.Equal(t, "chunk1", capturedDocs[0].ID)
	assert.Equal(t, "func main() {}", capturedDocs[0].Content)
	assert.Equal(t, "chunk2", capturedDocs[1].ID)
	assert.Equal(t, int32(1), mockStore.indexCalled.Load())
}

func TestBM25Indexer_Index_EmptySlice_NoOp(t *testing.T) {
	// Given: an indexer
	mockStore := &MockBM25Store{}
	indexer, err := NewBM25Indexer(WithStore(mockStore))
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: indexing empty slice
	err = indexer.Index(context.Background(), []*store.Chunk{})

	// Then: no error, store not called
	require.NoError(t, err)
	assert.Equal(t, int32(0), mockStore.indexCalled.Load())
}

func TestBM25Indexer_Index_NilSlice_NoOp(t *testing.T) {
	// Given: an indexer
	mockStore := &MockBM25Store{}
	indexer, err := NewBM25Indexer(WithStore(mockStore))
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: indexing nil slice
	err = indexer.Index(context.Background(), nil)

	// Then: no error, store not called
	require.NoError(t, err)
	assert.Equal(t, int32(0), mockStore.indexCalled.Load())
}

func TestBM25Indexer_Index_StoreError_Propagates(t *testing.T) {
	// Given: an indexer with a store that returns an error
	expectedErr := errors.New("store error")
	mockStore := &MockBM25Store{
		IndexFn: func(ctx context.Context, docs []*store.Document) error {
			return expectedErr
		},
	}
	indexer, err := NewBM25Indexer(WithStore(mockStore))
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: indexing chunks
	chunks := []*store.Chunk{{ID: "chunk1", Content: "test"}}
	err = indexer.Index(context.Background(), chunks)

	// Then: error is propagated
	require.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
}

func TestBM25Indexer_Index_ContextCancelled(t *testing.T) {
	// Given: an indexer and a cancelled context
	mockStore := &MockBM25Store{
		IndexFn: func(ctx context.Context, docs []*store.Document) error {
			return ctx.Err()
		},
	}
	indexer, err := NewBM25Indexer(WithStore(mockStore))
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// When: indexing with cancelled context
	chunks := []*store.Chunk{{ID: "chunk1", Content: "test"}}
	err = indexer.Index(ctx, chunks)

	// Then: context error is returned
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// =============================================================================
// Delete Tests
// =============================================================================

func TestBM25Indexer_Delete_Basic(t *testing.T) {
	// Given: an indexer with a mock store
	var capturedIDs []string
	mockStore := &MockBM25Store{
		DeleteFn: func(ctx context.Context, docIDs []string) error {
			capturedIDs = docIDs
			return nil
		},
	}
	indexer, err := NewBM25Indexer(WithStore(mockStore))
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: deleting chunks
	ids := []string{"chunk1", "chunk2"}
	err = indexer.Delete(context.Background(), ids)

	// Then: store receives the IDs
	require.NoError(t, err)
	assert.Equal(t, ids, capturedIDs)
	assert.Equal(t, int32(1), mockStore.deleteCalled.Load())
}

func TestBM25Indexer_Delete_EmptySlice_NoOp(t *testing.T) {
	// Given: an indexer
	mockStore := &MockBM25Store{}
	indexer, err := NewBM25Indexer(WithStore(mockStore))
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: deleting empty slice
	err = indexer.Delete(context.Background(), []string{})

	// Then: no error, store not called
	require.NoError(t, err)
	assert.Equal(t, int32(0), mockStore.deleteCalled.Load())
}

func TestBM25Indexer_Delete_StoreError_Propagates(t *testing.T) {
	// Given: an indexer with a store that returns an error
	expectedErr := errors.New("delete error")
	mockStore := &MockBM25Store{
		DeleteFn: func(ctx context.Context, docIDs []string) error {
			return expectedErr
		},
	}
	indexer, err := NewBM25Indexer(WithStore(mockStore))
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: deleting chunks
	err = indexer.Delete(context.Background(), []string{"chunk1"})

	// Then: error is propagated
	require.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
}

// =============================================================================
// Clear Tests
// =============================================================================

func TestBM25Indexer_Clear_CallsDeleteAllIDs(t *testing.T) {
	// Given: an indexer with indexed documents
	var deletedIDs []string
	mockStore := &MockBM25Store{
		AllIDsFn: func() ([]string, error) {
			return []string{"id1", "id2", "id3"}, nil
		},
		DeleteFn: func(ctx context.Context, docIDs []string) error {
			deletedIDs = docIDs
			return nil
		},
	}
	indexer, err := NewBM25Indexer(WithStore(mockStore))
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: clearing the index
	err = indexer.Clear(context.Background())

	// Then: all IDs are deleted
	require.NoError(t, err)
	assert.Equal(t, []string{"id1", "id2", "id3"}, deletedIDs)
}

func TestBM25Indexer_Clear_EmptyIndex_NoOp(t *testing.T) {
	// Given: an indexer with no documents
	mockStore := &MockBM25Store{
		AllIDsFn: func() ([]string, error) {
			return []string{}, nil
		},
	}
	indexer, err := NewBM25Indexer(WithStore(mockStore))
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: clearing the index
	err = indexer.Clear(context.Background())

	// Then: no error, delete not called
	require.NoError(t, err)
	assert.Equal(t, int32(0), mockStore.deleteCalled.Load())
}

// =============================================================================
// Stats Tests
// =============================================================================

func TestBM25Indexer_Stats_ReturnsStoreStats(t *testing.T) {
	// Given: an indexer with a store that has stats
	mockStore := &MockBM25Store{
		StatsFn: func() *store.IndexStats {
			return &store.IndexStats{
				DocumentCount: 100,
				TermCount:     500,
				AvgDocLength:  25.5,
			}
		},
	}
	indexer, err := NewBM25Indexer(WithStore(mockStore))
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: getting stats
	stats := indexer.Stats()

	// Then: stats match store stats
	assert.Equal(t, 100, stats.DocumentCount)
	assert.Equal(t, 500, stats.TermCount)
	assert.Equal(t, 25.5, stats.AvgDocLength)
}

// =============================================================================
// Close Tests
// =============================================================================

func TestBM25Indexer_Close_CallsStoreClose(t *testing.T) {
	// Given: an indexer
	mockStore := &MockBM25Store{}
	indexer, err := NewBM25Indexer(WithStore(mockStore))
	require.NoError(t, err)

	// When: closing the indexer
	err = indexer.Close()

	// Then: store close is called
	require.NoError(t, err)
	assert.Equal(t, int32(1), mockStore.closeCalled.Load())
}

func TestBM25Indexer_Close_Idempotent(t *testing.T) {
	// Given: an indexer
	mockStore := &MockBM25Store{}
	indexer, err := NewBM25Indexer(WithStore(mockStore))
	require.NoError(t, err)

	// When: closing multiple times
	err1 := indexer.Close()
	err2 := indexer.Close()
	err3 := indexer.Close()

	// Then: no errors, store only closed once
	require.NoError(t, err1)
	require.NoError(t, err2)
	require.NoError(t, err3)
	assert.Equal(t, int32(1), mockStore.closeCalled.Load())
}

func TestBM25Indexer_Close_PropagatesError(t *testing.T) {
	// Given: an indexer with a store that errors on close
	expectedErr := errors.New("close error")
	mockStore := &MockBM25Store{
		CloseFn: func() error {
			return expectedErr
		},
	}
	indexer, err := NewBM25Indexer(WithStore(mockStore))
	require.NoError(t, err)

	// When: closing the indexer
	err = indexer.Close()

	// Then: error is propagated
	require.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
}

// =============================================================================
// Concurrency Tests
// =============================================================================

func TestBM25Indexer_ConcurrentIndex_ThreadSafe(t *testing.T) {
	// Given: an indexer
	mockStore := &MockBM25Store{}
	indexer, err := NewBM25Indexer(WithStore(mockStore))
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: multiple goroutines index concurrently
	var wg sync.WaitGroup
	errChan := make(chan error, 100)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			chunks := []*store.Chunk{
				{ID: "chunk", Content: "content"},
			}
			if err := indexer.Index(context.Background(), chunks); err != nil {
				errChan <- err
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Then: no race-related errors
	for err := range errChan {
		t.Errorf("concurrent index error: %v", err)
	}
	assert.Equal(t, int32(50), mockStore.indexCalled.Load())
}

func TestBM25Indexer_ConcurrentIndexAndDelete_ThreadSafe(t *testing.T) {
	// Given: an indexer
	mockStore := &MockBM25Store{}
	indexer, err := NewBM25Indexer(WithStore(mockStore))
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: concurrent index and delete operations
	var wg sync.WaitGroup
	errChan := make(chan error, 200)

	// 50 indexers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			chunks := []*store.Chunk{{ID: "chunk", Content: "content"}}
			if err := indexer.Index(context.Background(), chunks); err != nil {
				errChan <- err
			}
		}()
	}

	// 50 deleters
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := indexer.Delete(context.Background(), []string{"chunk"}); err != nil {
				errChan <- err
			}
		}()
	}

	wg.Wait()
	close(errChan)

	// Then: no race-related errors
	for err := range errChan {
		t.Errorf("concurrent operation error: %v", err)
	}
}

// =============================================================================
// Interface Compliance Test
// =============================================================================

func TestBM25Indexer_ImplementsIndexer(t *testing.T) {
	// Given: a BM25Indexer
	mockStore := &MockBM25Store{}
	indexer, err := NewBM25Indexer(WithStore(mockStore))
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// Then: it implements the Indexer interface
	var _ Indexer = indexer
}

// =============================================================================
// Helper Functions
// =============================================================================

func makeChunks(count int) []*store.Chunk {
	chunks := make([]*store.Chunk, count)
	for i := 0; i < count; i++ {
		chunks[i] = &store.Chunk{
			ID:      "chunk" + string(rune('0'+i%10)),
			Content: "content for chunk",
		}
	}
	return chunks
}
