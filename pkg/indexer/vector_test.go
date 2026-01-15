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

// MockEmbedder implements embed.Embedder for testing.
type MockEmbedder struct {
	EmbedFn        func(ctx context.Context, text string) ([]float32, error)
	EmbedBatchFn   func(ctx context.Context, texts []string) ([][]float32, error)
	DimensionsFn   func() int
	ModelNameFn    func() string
	AvailableFn    func(ctx context.Context) bool
	CloseFn        func() error
	SetBatchIndexFn func(idx int)
	SetFinalBatchFn func(isFinal bool)

	embedBatchCalled atomic.Int32
}

func (m *MockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if m.EmbedFn != nil {
		return m.EmbedFn(ctx, text)
	}
	return make([]float32, 768), nil
}

func (m *MockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	m.embedBatchCalled.Add(1)
	if m.EmbedBatchFn != nil {
		return m.EmbedBatchFn(ctx, texts)
	}
	// Default: return 768-dim vectors
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = make([]float32, 768)
	}
	return result, nil
}

func (m *MockEmbedder) Dimensions() int {
	if m.DimensionsFn != nil {
		return m.DimensionsFn()
	}
	return 768
}

func (m *MockEmbedder) ModelName() string {
	if m.ModelNameFn != nil {
		return m.ModelNameFn()
	}
	return "mock-model"
}

func (m *MockEmbedder) Available(ctx context.Context) bool {
	if m.AvailableFn != nil {
		return m.AvailableFn(ctx)
	}
	return true
}

func (m *MockEmbedder) Close() error {
	if m.CloseFn != nil {
		return m.CloseFn()
	}
	return nil
}

func (m *MockEmbedder) SetBatchIndex(idx int) {
	if m.SetBatchIndexFn != nil {
		m.SetBatchIndexFn(idx)
	}
}

func (m *MockEmbedder) SetFinalBatch(isFinal bool) {
	if m.SetFinalBatchFn != nil {
		m.SetFinalBatchFn(isFinal)
	}
}

// MockVectorStore implements store.VectorStore for testing.
type MockVectorStore struct {
	AddFn      func(ctx context.Context, ids []string, vectors [][]float32) error
	SearchFn   func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error)
	DeleteFn   func(ctx context.Context, ids []string) error
	AllIDsFn   func() []string
	ContainsFn func(id string) bool
	CountFn    func() int
	SaveFn     func(path string) error
	LoadFn     func(path string) error
	CloseFn    func() error

	addCalled    atomic.Int32
	deleteCalled atomic.Int32
	closeCalled  atomic.Int32
}

func (m *MockVectorStore) Add(ctx context.Context, ids []string, vectors [][]float32) error {
	m.addCalled.Add(1)
	if m.AddFn != nil {
		return m.AddFn(ctx, ids, vectors)
	}
	return nil
}

func (m *MockVectorStore) Search(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
	if m.SearchFn != nil {
		return m.SearchFn(ctx, query, k)
	}
	return nil, nil
}

func (m *MockVectorStore) Delete(ctx context.Context, ids []string) error {
	m.deleteCalled.Add(1)
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, ids)
	}
	return nil
}

func (m *MockVectorStore) AllIDs() []string {
	if m.AllIDsFn != nil {
		return m.AllIDsFn()
	}
	return nil
}

func (m *MockVectorStore) Contains(id string) bool {
	if m.ContainsFn != nil {
		return m.ContainsFn(id)
	}
	return false
}

func (m *MockVectorStore) Count() int {
	if m.CountFn != nil {
		return m.CountFn()
	}
	return 0
}

func (m *MockVectorStore) Save(path string) error {
	if m.SaveFn != nil {
		return m.SaveFn(path)
	}
	return nil
}

func (m *MockVectorStore) Load(path string) error {
	if m.LoadFn != nil {
		return m.LoadFn(path)
	}
	return nil
}

func (m *MockVectorStore) Close() error {
	m.closeCalled.Add(1)
	if m.CloseFn != nil {
		return m.CloseFn()
	}
	return nil
}

// Ensure mocks implement interfaces
var _ store.VectorStore = (*MockVectorStore)(nil)

// =============================================================================
// Constructor Tests
// =============================================================================

func TestNewVectorIndexer_WithDependencies_Success(t *testing.T) {
	// Given: mock embedder and vector store
	mockEmbedder := &MockEmbedder{}
	mockStore := &MockVectorStore{}

	// When: creating a new VectorIndexer with dependencies
	indexer, err := NewVectorIndexer(
		WithEmbedder(mockEmbedder),
		WithVectorStore(mockStore),
	)

	// Then: indexer is created without error
	require.NoError(t, err)
	require.NotNil(t, indexer)
	defer func() { _ = indexer.Close() }()
}

func TestNewVectorIndexer_MissingEmbedder_ReturnsError(t *testing.T) {
	// Given: only vector store provided
	mockStore := &MockVectorStore{}

	// When: creating VectorIndexer without embedder
	indexer, err := NewVectorIndexer(WithVectorStore(mockStore))

	// Then: an error is returned
	require.Error(t, err)
	assert.Nil(t, indexer)
	assert.Contains(t, err.Error(), "embedder")
}

func TestNewVectorIndexer_MissingStore_ReturnsError(t *testing.T) {
	// Given: only embedder provided
	mockEmbedder := &MockEmbedder{}

	// When: creating VectorIndexer without store
	indexer, err := NewVectorIndexer(WithEmbedder(mockEmbedder))

	// Then: an error is returned
	require.Error(t, err)
	assert.Nil(t, indexer)
	assert.Contains(t, err.Error(), "store")
}

func TestNewVectorIndexer_NoDependencies_ReturnsError(t *testing.T) {
	// Given: no dependencies

	// When: creating VectorIndexer without any options
	indexer, err := NewVectorIndexer()

	// Then: an error is returned
	require.Error(t, err)
	assert.Nil(t, indexer)
}

// =============================================================================
// Index Tests
// =============================================================================

func TestVectorIndexer_Index_Basic(t *testing.T) {
	// Given: an indexer with mocks that track calls
	var capturedIDs []string
	var capturedVectors [][]float32

	mockEmbedder := &MockEmbedder{
		EmbedBatchFn: func(ctx context.Context, texts []string) ([][]float32, error) {
			result := make([][]float32, len(texts))
			for i := range texts {
				result[i] = make([]float32, 768)
				result[i][0] = float32(i) // Unique marker
			}
			return result, nil
		},
	}
	mockStore := &MockVectorStore{
		AddFn: func(ctx context.Context, ids []string, vectors [][]float32) error {
			capturedIDs = ids
			capturedVectors = vectors
			return nil
		},
	}

	indexer, err := NewVectorIndexer(
		WithEmbedder(mockEmbedder),
		WithVectorStore(mockStore),
	)
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: indexing chunks
	chunks := []*store.Chunk{
		{ID: "chunk1", Content: "func main() {}"},
		{ID: "chunk2", Content: "type User struct {}"},
	}
	err = indexer.Index(context.Background(), chunks)

	// Then: embedder was called and vectors stored
	require.NoError(t, err)
	assert.Equal(t, int32(1), mockEmbedder.embedBatchCalled.Load())
	assert.Equal(t, int32(1), mockStore.addCalled.Load())
	require.Len(t, capturedIDs, 2)
	assert.Equal(t, "chunk1", capturedIDs[0])
	assert.Equal(t, "chunk2", capturedIDs[1])
	require.Len(t, capturedVectors, 2)
}

func TestVectorIndexer_Index_EmptySlice_NoOp(t *testing.T) {
	// Given: an indexer
	mockEmbedder := &MockEmbedder{}
	mockStore := &MockVectorStore{}
	indexer, err := NewVectorIndexer(
		WithEmbedder(mockEmbedder),
		WithVectorStore(mockStore),
	)
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: indexing empty slice
	err = indexer.Index(context.Background(), []*store.Chunk{})

	// Then: no error, nothing called
	require.NoError(t, err)
	assert.Equal(t, int32(0), mockEmbedder.embedBatchCalled.Load())
	assert.Equal(t, int32(0), mockStore.addCalled.Load())
}

func TestVectorIndexer_Index_NilSlice_NoOp(t *testing.T) {
	// Given: an indexer
	mockEmbedder := &MockEmbedder{}
	mockStore := &MockVectorStore{}
	indexer, err := NewVectorIndexer(
		WithEmbedder(mockEmbedder),
		WithVectorStore(mockStore),
	)
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: indexing nil slice
	err = indexer.Index(context.Background(), nil)

	// Then: no error, nothing called
	require.NoError(t, err)
	assert.Equal(t, int32(0), mockEmbedder.embedBatchCalled.Load())
	assert.Equal(t, int32(0), mockStore.addCalled.Load())
}

func TestVectorIndexer_Index_EmbedderError_Propagates(t *testing.T) {
	// Given: an indexer with embedder that errors
	expectedErr := errors.New("embedder error")
	mockEmbedder := &MockEmbedder{
		EmbedBatchFn: func(ctx context.Context, texts []string) ([][]float32, error) {
			return nil, expectedErr
		},
	}
	mockStore := &MockVectorStore{}
	indexer, err := NewVectorIndexer(
		WithEmbedder(mockEmbedder),
		WithVectorStore(mockStore),
	)
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: indexing chunks
	chunks := []*store.Chunk{{ID: "chunk1", Content: "test"}}
	err = indexer.Index(context.Background(), chunks)

	// Then: error is propagated
	require.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
	assert.Equal(t, int32(0), mockStore.addCalled.Load()) // Store not called
}

func TestVectorIndexer_Index_StoreError_Propagates(t *testing.T) {
	// Given: an indexer with store that errors
	expectedErr := errors.New("store error")
	mockEmbedder := &MockEmbedder{}
	mockStore := &MockVectorStore{
		AddFn: func(ctx context.Context, ids []string, vectors [][]float32) error {
			return expectedErr
		},
	}
	indexer, err := NewVectorIndexer(
		WithEmbedder(mockEmbedder),
		WithVectorStore(mockStore),
	)
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: indexing chunks
	chunks := []*store.Chunk{{ID: "chunk1", Content: "test"}}
	err = indexer.Index(context.Background(), chunks)

	// Then: error is propagated
	require.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
}

func TestVectorIndexer_Index_ContextCancelled(t *testing.T) {
	// Given: an indexer and cancelled context
	mockEmbedder := &MockEmbedder{
		EmbedBatchFn: func(ctx context.Context, texts []string) ([][]float32, error) {
			return nil, ctx.Err()
		},
	}
	mockStore := &MockVectorStore{}
	indexer, err := NewVectorIndexer(
		WithEmbedder(mockEmbedder),
		WithVectorStore(mockStore),
	)
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

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

func TestVectorIndexer_Delete_Basic(t *testing.T) {
	// Given: an indexer with mock store
	var capturedIDs []string
	mockEmbedder := &MockEmbedder{}
	mockStore := &MockVectorStore{
		DeleteFn: func(ctx context.Context, ids []string) error {
			capturedIDs = ids
			return nil
		},
	}
	indexer, err := NewVectorIndexer(
		WithEmbedder(mockEmbedder),
		WithVectorStore(mockStore),
	)
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

func TestVectorIndexer_Delete_EmptySlice_NoOp(t *testing.T) {
	// Given: an indexer
	mockEmbedder := &MockEmbedder{}
	mockStore := &MockVectorStore{}
	indexer, err := NewVectorIndexer(
		WithEmbedder(mockEmbedder),
		WithVectorStore(mockStore),
	)
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: deleting empty slice
	err = indexer.Delete(context.Background(), []string{})

	// Then: no error, store not called
	require.NoError(t, err)
	assert.Equal(t, int32(0), mockStore.deleteCalled.Load())
}

func TestVectorIndexer_Delete_StoreError_Propagates(t *testing.T) {
	// Given: an indexer with store that errors
	expectedErr := errors.New("delete error")
	mockEmbedder := &MockEmbedder{}
	mockStore := &MockVectorStore{
		DeleteFn: func(ctx context.Context, ids []string) error {
			return expectedErr
		},
	}
	indexer, err := NewVectorIndexer(
		WithEmbedder(mockEmbedder),
		WithVectorStore(mockStore),
	)
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

func TestVectorIndexer_Clear_DeletesAllIDs(t *testing.T) {
	// Given: an indexer with vectors
	var deletedIDs []string
	mockEmbedder := &MockEmbedder{}
	mockStore := &MockVectorStore{
		AllIDsFn: func() []string {
			return []string{"id1", "id2", "id3"}
		},
		DeleteFn: func(ctx context.Context, ids []string) error {
			deletedIDs = ids
			return nil
		},
	}
	indexer, err := NewVectorIndexer(
		WithEmbedder(mockEmbedder),
		WithVectorStore(mockStore),
	)
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: clearing the index
	err = indexer.Clear(context.Background())

	// Then: all IDs are deleted
	require.NoError(t, err)
	assert.Equal(t, []string{"id1", "id2", "id3"}, deletedIDs)
}

func TestVectorIndexer_Clear_EmptyStore_NoOp(t *testing.T) {
	// Given: an indexer with no vectors
	mockEmbedder := &MockEmbedder{}
	mockStore := &MockVectorStore{
		AllIDsFn: func() []string {
			return []string{}
		},
	}
	indexer, err := NewVectorIndexer(
		WithEmbedder(mockEmbedder),
		WithVectorStore(mockStore),
	)
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

func TestVectorIndexer_Stats_ReturnsCount(t *testing.T) {
	// Given: an indexer with vectors
	mockEmbedder := &MockEmbedder{}
	mockStore := &MockVectorStore{
		CountFn: func() int {
			return 100
		},
	}
	indexer, err := NewVectorIndexer(
		WithEmbedder(mockEmbedder),
		WithVectorStore(mockStore),
	)
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: getting stats
	stats := indexer.Stats()

	// Then: DocumentCount reflects vector count
	assert.Equal(t, 100, stats.DocumentCount)
	assert.Equal(t, 0, stats.TermCount)      // N/A for vectors
	assert.Equal(t, 0.0, stats.AvgDocLength) // N/A for vectors
}

// =============================================================================
// Close Tests
// =============================================================================

func TestVectorIndexer_Close_CallsStoreClose(t *testing.T) {
	// Given: an indexer
	mockEmbedder := &MockEmbedder{}
	mockStore := &MockVectorStore{}
	indexer, err := NewVectorIndexer(
		WithEmbedder(mockEmbedder),
		WithVectorStore(mockStore),
	)
	require.NoError(t, err)

	// When: closing the indexer
	err = indexer.Close()

	// Then: store close is called
	require.NoError(t, err)
	assert.Equal(t, int32(1), mockStore.closeCalled.Load())
}

func TestVectorIndexer_Close_Idempotent(t *testing.T) {
	// Given: an indexer
	mockEmbedder := &MockEmbedder{}
	mockStore := &MockVectorStore{}
	indexer, err := NewVectorIndexer(
		WithEmbedder(mockEmbedder),
		WithVectorStore(mockStore),
	)
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

func TestVectorIndexer_Close_PropagatesError(t *testing.T) {
	// Given: an indexer with store that errors on close
	expectedErr := errors.New("close error")
	mockEmbedder := &MockEmbedder{}
	mockStore := &MockVectorStore{
		CloseFn: func() error {
			return expectedErr
		},
	}
	indexer, err := NewVectorIndexer(
		WithEmbedder(mockEmbedder),
		WithVectorStore(mockStore),
	)
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

func TestVectorIndexer_ConcurrentIndex_ThreadSafe(t *testing.T) {
	// Given: an indexer
	mockEmbedder := &MockEmbedder{}
	mockStore := &MockVectorStore{}
	indexer, err := NewVectorIndexer(
		WithEmbedder(mockEmbedder),
		WithVectorStore(mockStore),
	)
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// When: multiple goroutines index concurrently
	var wg sync.WaitGroup
	errChan := make(chan error, 100)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			chunks := []*store.Chunk{
				{ID: "chunk", Content: "content"},
			}
			if err := indexer.Index(context.Background(), chunks); err != nil {
				errChan <- err
			}
		}()
	}

	wg.Wait()
	close(errChan)

	// Then: no race-related errors
	for err := range errChan {
		t.Errorf("concurrent index error: %v", err)
	}
	assert.Equal(t, int32(50), mockEmbedder.embedBatchCalled.Load())
	assert.Equal(t, int32(50), mockStore.addCalled.Load())
}

func TestVectorIndexer_ConcurrentIndexAndDelete_ThreadSafe(t *testing.T) {
	// Given: an indexer
	mockEmbedder := &MockEmbedder{}
	mockStore := &MockVectorStore{}
	indexer, err := NewVectorIndexer(
		WithEmbedder(mockEmbedder),
		WithVectorStore(mockStore),
	)
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

func TestVectorIndexer_ImplementsIndexer(t *testing.T) {
	// Given: a VectorIndexer
	mockEmbedder := &MockEmbedder{}
	mockStore := &MockVectorStore{}
	indexer, err := NewVectorIndexer(
		WithEmbedder(mockEmbedder),
		WithVectorStore(mockStore),
	)
	require.NoError(t, err)
	defer func() { _ = indexer.Close() }()

	// Then: it implements the Indexer interface
	var _ Indexer = indexer
}
