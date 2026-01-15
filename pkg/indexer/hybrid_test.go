package indexer

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

// MockIndexer implements Indexer for testing HybridIndexer.
type MockIndexer struct {
	IndexFn  func(ctx context.Context, chunks []*store.Chunk) error
	DeleteFn func(ctx context.Context, ids []string) error
	ClearFn  func(ctx context.Context) error
	StatsFn  func() IndexStats
	CloseFn  func() error

	// Call tracking
	indexCalled  atomic.Int32
	deleteCalled atomic.Int32
	clearCalled  atomic.Int32
	statsCalled  atomic.Int32
	closeCalled  atomic.Int32
}

func (m *MockIndexer) Index(ctx context.Context, chunks []*store.Chunk) error {
	m.indexCalled.Add(1)
	if m.IndexFn != nil {
		return m.IndexFn(ctx, chunks)
	}
	return nil
}

func (m *MockIndexer) Delete(ctx context.Context, ids []string) error {
	m.deleteCalled.Add(1)
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, ids)
	}
	return nil
}

func (m *MockIndexer) Clear(ctx context.Context) error {
	m.clearCalled.Add(1)
	if m.ClearFn != nil {
		return m.ClearFn(ctx)
	}
	return nil
}

func (m *MockIndexer) Stats() IndexStats {
	m.statsCalled.Add(1)
	if m.StatsFn != nil {
		return m.StatsFn()
	}
	return IndexStats{}
}

func (m *MockIndexer) Close() error {
	m.closeCalled.Add(1)
	if m.CloseFn != nil {
		return m.CloseFn()
	}
	return nil
}

// =============================================================================
// Constructor Tests
// =============================================================================

func TestNewHybridIndexer_BothIndexers_Success(t *testing.T) {
	// Given: Both BM25 and Vector indexers
	bm25 := &MockIndexer{}
	vector := &MockIndexer{}

	// When: Creating hybrid indexer with both
	h, err := NewHybridIndexer(
		WithBM25(bm25),
		WithVector(vector),
	)

	// Then: Success
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if h == nil {
		t.Fatal("expected non-nil indexer")
	}
}

func TestNewHybridIndexer_BM25Only_Success(t *testing.T) {
	// Given: Only BM25 indexer
	bm25 := &MockIndexer{}

	// When: Creating hybrid with BM25 only
	h, err := NewHybridIndexer(WithBM25(bm25))

	// Then: Success (vector-less mode)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if h == nil {
		t.Fatal("expected non-nil indexer")
	}
}

func TestNewHybridIndexer_VectorOnly_Success(t *testing.T) {
	// Given: Only Vector indexer
	vector := &MockIndexer{}

	// When: Creating hybrid with Vector only
	h, err := NewHybridIndexer(WithVector(vector))

	// Then: Success (BM25-less mode)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if h == nil {
		t.Fatal("expected non-nil indexer")
	}
}

func TestNewHybridIndexer_NoIndexers_ReturnsError(t *testing.T) {
	// Given: No indexers

	// When: Creating hybrid with no options
	h, err := NewHybridIndexer()

	// Then: Error - at least one required
	if err == nil {
		t.Fatal("expected error for no indexers")
	}
	if h != nil {
		t.Fatal("expected nil indexer on error")
	}
	if !errors.Is(err, ErrNoIndexers) {
		t.Errorf("expected ErrNoIndexers, got %v", err)
	}
}

// =============================================================================
// Index Tests
// =============================================================================

func TestHybridIndexer_Index_BothCalled(t *testing.T) {
	// Given: Hybrid with both indexers
	bm25 := &MockIndexer{}
	vector := &MockIndexer{}
	h, _ := NewHybridIndexer(WithBM25(bm25), WithVector(vector))

	chunks := []*store.Chunk{
		{ID: "1", Content: "test content"},
	}

	// When: Indexing
	err := h.Index(context.Background(), chunks)

	// Then: Both indexers called
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if bm25.indexCalled.Load() != 1 {
		t.Errorf("expected bm25 Index called once, got %d", bm25.indexCalled.Load())
	}
	if vector.indexCalled.Load() != 1 {
		t.Errorf("expected vector Index called once, got %d", vector.indexCalled.Load())
	}
}

func TestHybridIndexer_Index_BM25Only(t *testing.T) {
	// Given: Hybrid with BM25 only
	bm25 := &MockIndexer{}
	h, _ := NewHybridIndexer(WithBM25(bm25))

	chunks := []*store.Chunk{{ID: "1", Content: "test"}}

	// When: Indexing
	err := h.Index(context.Background(), chunks)

	// Then: Only BM25 called
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if bm25.indexCalled.Load() != 1 {
		t.Errorf("expected bm25 Index called once, got %d", bm25.indexCalled.Load())
	}
}

func TestHybridIndexer_Index_VectorOnly(t *testing.T) {
	// Given: Hybrid with Vector only
	vector := &MockIndexer{}
	h, _ := NewHybridIndexer(WithVector(vector))

	chunks := []*store.Chunk{{ID: "1", Content: "test"}}

	// When: Indexing
	err := h.Index(context.Background(), chunks)

	// Then: Only Vector called
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if vector.indexCalled.Load() != 1 {
		t.Errorf("expected vector Index called once, got %d", vector.indexCalled.Load())
	}
}

func TestHybridIndexer_Index_EmptySlice_NoOp(t *testing.T) {
	// Given: Hybrid with both indexers
	bm25 := &MockIndexer{}
	vector := &MockIndexer{}
	h, _ := NewHybridIndexer(WithBM25(bm25), WithVector(vector))

	// When: Indexing empty slice
	err := h.Index(context.Background(), []*store.Chunk{})

	// Then: No-op, no errors
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// Indexers may or may not be called for empty input - implementation detail
}

func TestHybridIndexer_Index_BM25Error_FailsFast(t *testing.T) {
	// Given: BM25 that fails
	bm25Err := errors.New("bm25 failed")
	bm25 := &MockIndexer{
		IndexFn: func(ctx context.Context, chunks []*store.Chunk) error {
			return bm25Err
		},
	}
	vector := &MockIndexer{}
	h, _ := NewHybridIndexer(WithBM25(bm25), WithVector(vector))

	chunks := []*store.Chunk{{ID: "1", Content: "test"}}

	// When: Indexing
	err := h.Index(context.Background(), chunks)

	// Then: Error propagated, vector NOT called (fail fast)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, bm25Err) {
		t.Errorf("expected bm25 error, got %v", err)
	}
	if vector.indexCalled.Load() != 0 {
		t.Error("expected vector Index NOT called on BM25 failure")
	}
}

func TestHybridIndexer_Index_VectorError_Propagates(t *testing.T) {
	// Given: Vector that fails
	vectorErr := errors.New("vector failed")
	bm25 := &MockIndexer{}
	vector := &MockIndexer{
		IndexFn: func(ctx context.Context, chunks []*store.Chunk) error {
			return vectorErr
		},
	}
	h, _ := NewHybridIndexer(WithBM25(bm25), WithVector(vector))

	chunks := []*store.Chunk{{ID: "1", Content: "test"}}

	// When: Indexing
	err := h.Index(context.Background(), chunks)

	// Then: Error propagated
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, vectorErr) {
		t.Errorf("expected vector error, got %v", err)
	}
	// BM25 should have been called (sequential)
	if bm25.indexCalled.Load() != 1 {
		t.Error("expected bm25 Index called before vector failure")
	}
}

func TestHybridIndexer_Index_ContextCancelled(t *testing.T) {
	// Given: Cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	bm25 := &MockIndexer{
		IndexFn: func(ctx context.Context, chunks []*store.Chunk) error {
			return ctx.Err()
		},
	}
	h, _ := NewHybridIndexer(WithBM25(bm25))

	// When: Indexing with cancelled context
	err := h.Index(ctx, []*store.Chunk{{ID: "1"}})

	// Then: Context error
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// =============================================================================
// Delete Tests
// =============================================================================

func TestHybridIndexer_Delete_BothCalled(t *testing.T) {
	// Given: Hybrid with both indexers
	bm25 := &MockIndexer{}
	vector := &MockIndexer{}
	h, _ := NewHybridIndexer(WithBM25(bm25), WithVector(vector))

	ids := []string{"1", "2"}

	// When: Deleting
	err := h.Delete(context.Background(), ids)

	// Then: Both called
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if bm25.deleteCalled.Load() != 1 {
		t.Error("expected bm25 Delete called")
	}
	if vector.deleteCalled.Load() != 1 {
		t.Error("expected vector Delete called")
	}
}

func TestHybridIndexer_Delete_EmptySlice_NoOp(t *testing.T) {
	// Given: Hybrid with both indexers
	bm25 := &MockIndexer{}
	vector := &MockIndexer{}
	h, _ := NewHybridIndexer(WithBM25(bm25), WithVector(vector))

	// When: Deleting empty slice
	err := h.Delete(context.Background(), []string{})

	// Then: No-op
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestHybridIndexer_Delete_BM25Fails_VectorStillCalled(t *testing.T) {
	// Given: BM25 that fails (best-effort pattern)
	bm25 := &MockIndexer{
		DeleteFn: func(ctx context.Context, ids []string) error {
			return errors.New("bm25 delete failed")
		},
	}
	vector := &MockIndexer{}
	h, _ := NewHybridIndexer(WithBM25(bm25), WithVector(vector))

	// When: Deleting
	err := h.Delete(context.Background(), []string{"1"})

	// Then: Error returned but vector still called (best-effort)
	if err == nil {
		t.Fatal("expected error")
	}
	if vector.deleteCalled.Load() != 1 {
		t.Error("expected vector Delete still called despite BM25 failure")
	}
}

func TestHybridIndexer_Delete_VectorFails_ErrorReturned(t *testing.T) {
	// Given: Vector that fails
	bm25 := &MockIndexer{}
	vector := &MockIndexer{
		DeleteFn: func(ctx context.Context, ids []string) error {
			return errors.New("vector delete failed")
		},
	}
	h, _ := NewHybridIndexer(WithBM25(bm25), WithVector(vector))

	// When: Deleting
	err := h.Delete(context.Background(), []string{"1"})

	// Then: Error returned
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHybridIndexer_Delete_BothFail_JoinedError(t *testing.T) {
	// Given: Both fail
	bm25Err := errors.New("bm25 failed")
	vectorErr := errors.New("vector failed")
	bm25 := &MockIndexer{
		DeleteFn: func(ctx context.Context, ids []string) error {
			return bm25Err
		},
	}
	vector := &MockIndexer{
		DeleteFn: func(ctx context.Context, ids []string) error {
			return vectorErr
		},
	}
	h, _ := NewHybridIndexer(WithBM25(bm25), WithVector(vector))

	// When: Deleting
	err := h.Delete(context.Background(), []string{"1"})

	// Then: Both errors joined
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, bm25Err) {
		t.Error("expected error to contain bm25 error")
	}
	if !errors.Is(err, vectorErr) {
		t.Error("expected error to contain vector error")
	}
}

// =============================================================================
// Clear Tests
// =============================================================================

func TestHybridIndexer_Clear_BothCalled(t *testing.T) {
	// Given: Hybrid with both indexers
	bm25 := &MockIndexer{}
	vector := &MockIndexer{}
	h, _ := NewHybridIndexer(WithBM25(bm25), WithVector(vector))

	// When: Clearing
	err := h.Clear(context.Background())

	// Then: Both called
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if bm25.clearCalled.Load() != 1 {
		t.Error("expected bm25 Clear called")
	}
	if vector.clearCalled.Load() != 1 {
		t.Error("expected vector Clear called")
	}
}

func TestHybridIndexer_Clear_BM25Only(t *testing.T) {
	// Given: Hybrid with BM25 only
	bm25 := &MockIndexer{}
	h, _ := NewHybridIndexer(WithBM25(bm25))

	// When: Clearing
	err := h.Clear(context.Background())

	// Then: Only BM25 called
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if bm25.clearCalled.Load() != 1 {
		t.Error("expected bm25 Clear called")
	}
}

func TestHybridIndexer_Clear_ErrorPropagates(t *testing.T) {
	// Given: BM25 that fails on clear
	clearErr := errors.New("clear failed")
	bm25 := &MockIndexer{
		ClearFn: func(ctx context.Context) error {
			return clearErr
		},
	}
	h, _ := NewHybridIndexer(WithBM25(bm25))

	// When: Clearing
	err := h.Clear(context.Background())

	// Then: Error propagated
	if !errors.Is(err, clearErr) {
		t.Errorf("expected clear error, got %v", err)
	}
}

// =============================================================================
// Stats Tests
// =============================================================================

func TestHybridIndexer_Stats_BothIndexers(t *testing.T) {
	// Given: Both indexers with stats
	bm25 := &MockIndexer{
		StatsFn: func() IndexStats {
			return IndexStats{
				DocumentCount: 100,
				TermCount:     500,
				AvgDocLength:  25.5,
			}
		},
	}
	vector := &MockIndexer{
		StatsFn: func() IndexStats {
			return IndexStats{
				DocumentCount: 100,
				TermCount:     0,
				AvgDocLength:  0,
			}
		},
	}
	h, _ := NewHybridIndexer(WithBM25(bm25), WithVector(vector))

	// When: Getting stats
	stats := h.Stats()

	// Then: Combined stats
	if stats.DocumentCount != 100 {
		t.Errorf("expected DocumentCount 100, got %d", stats.DocumentCount)
	}
	if stats.TermCount != 500 {
		t.Errorf("expected TermCount 500, got %d", stats.TermCount)
	}
	if stats.AvgDocLength != 25.5 {
		t.Errorf("expected AvgDocLength 25.5, got %f", stats.AvgDocLength)
	}
}

func TestHybridIndexer_Stats_BM25Only(t *testing.T) {
	// Given: BM25 only
	bm25 := &MockIndexer{
		StatsFn: func() IndexStats {
			return IndexStats{
				DocumentCount: 50,
				TermCount:     200,
				AvgDocLength:  10.0,
			}
		},
	}
	h, _ := NewHybridIndexer(WithBM25(bm25))

	// When: Getting stats
	stats := h.Stats()

	// Then: BM25 stats returned
	if stats.DocumentCount != 50 {
		t.Errorf("expected DocumentCount 50, got %d", stats.DocumentCount)
	}
	if stats.TermCount != 200 {
		t.Errorf("expected TermCount 200, got %d", stats.TermCount)
	}
}

func TestHybridIndexer_Stats_VectorOnly(t *testing.T) {
	// Given: Vector only
	vector := &MockIndexer{
		StatsFn: func() IndexStats {
			return IndexStats{
				DocumentCount: 75,
				TermCount:     0,
				AvgDocLength:  0,
			}
		},
	}
	h, _ := NewHybridIndexer(WithVector(vector))

	// When: Getting stats
	stats := h.Stats()

	// Then: Vector stats returned
	if stats.DocumentCount != 75 {
		t.Errorf("expected DocumentCount 75, got %d", stats.DocumentCount)
	}
	if stats.TermCount != 0 {
		t.Errorf("expected TermCount 0, got %d", stats.TermCount)
	}
}

// =============================================================================
// Close Tests
// =============================================================================

func TestHybridIndexer_Close_BothCalled(t *testing.T) {
	// Given: Hybrid with both indexers
	bm25 := &MockIndexer{}
	vector := &MockIndexer{}
	h, _ := NewHybridIndexer(WithBM25(bm25), WithVector(vector))

	// When: Closing
	err := h.Close()

	// Then: Both closed
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if bm25.closeCalled.Load() != 1 {
		t.Error("expected bm25 Close called")
	}
	if vector.closeCalled.Load() != 1 {
		t.Error("expected vector Close called")
	}
}

func TestHybridIndexer_Close_Idempotent(t *testing.T) {
	// Given: Hybrid indexer
	bm25 := &MockIndexer{}
	h, _ := NewHybridIndexer(WithBM25(bm25))

	// When: Closing multiple times
	_ = h.Close()
	err := h.Close()

	// Then: Safe (idempotent)
	if err != nil {
		t.Fatalf("expected no error on second close, got %v", err)
	}
	// Should only close underlying indexer once
	if bm25.closeCalled.Load() != 1 {
		t.Errorf("expected bm25 Close called once, got %d", bm25.closeCalled.Load())
	}
}

func TestHybridIndexer_Close_AccumulatesErrors(t *testing.T) {
	// Given: Both fail to close
	bm25Err := errors.New("bm25 close failed")
	vectorErr := errors.New("vector close failed")
	bm25 := &MockIndexer{
		CloseFn: func() error { return bm25Err },
	}
	vector := &MockIndexer{
		CloseFn: func() error { return vectorErr },
	}
	h, _ := NewHybridIndexer(WithBM25(bm25), WithVector(vector))

	// When: Closing
	err := h.Close()

	// Then: Both errors accumulated
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, bm25Err) {
		t.Error("expected error to contain bm25 error")
	}
	if !errors.Is(err, vectorErr) {
		t.Error("expected error to contain vector error")
	}
}

// =============================================================================
// Concurrency Tests
// =============================================================================

func TestHybridIndexer_ConcurrentIndex_ThreadSafe(t *testing.T) {
	// Given: Hybrid indexer
	bm25 := &MockIndexer{}
	vector := &MockIndexer{}
	h, _ := NewHybridIndexer(WithBM25(bm25), WithVector(vector))

	// When: Concurrent indexing
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			chunks := []*store.Chunk{{ID: string(rune('0' + id)), Content: "test"}}
			_ = h.Index(context.Background(), chunks)
		}(i)
	}

	// Then: All complete without race
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestHybridIndexer_ConcurrentOperations_ThreadSafe(t *testing.T) {
	// Given: Hybrid indexer
	bm25 := &MockIndexer{}
	vector := &MockIndexer{}
	h, _ := NewHybridIndexer(WithBM25(bm25), WithVector(vector))

	// When: Concurrent mixed operations
	done := make(chan struct{})

	// Index operations
	go func() {
		defer func() { done <- struct{}{} }()
		for i := 0; i < 5; i++ {
			_ = h.Index(context.Background(), []*store.Chunk{{ID: "idx"}})
		}
	}()

	// Delete operations
	go func() {
		defer func() { done <- struct{}{} }()
		for i := 0; i < 5; i++ {
			_ = h.Delete(context.Background(), []string{"del"})
		}
	}()

	// Stats operations
	go func() {
		defer func() { done <- struct{}{} }()
		for i := 0; i < 5; i++ {
			_ = h.Stats()
		}
	}()

	// Then: All complete without race
	for i := 0; i < 3; i++ {
		<-done
	}
}

// =============================================================================
// Interface Compliance
// =============================================================================

func TestHybridIndexer_ImplementsIndexer(t *testing.T) {
	// Given: Hybrid indexer
	bm25 := &MockIndexer{}
	h, _ := NewHybridIndexer(WithBM25(bm25))

	// Then: Implements Indexer interface
	var _ Indexer = h
}

// Ensure compile-time interface compliance
var _ Indexer = (*HybridIndexer)(nil)
