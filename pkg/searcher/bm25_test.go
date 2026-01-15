package searcher

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

// MockBM25Store implements store.BM25Index for testing.
type MockBM25Store struct {
	SearchFn func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error)

	searchCalled atomic.Int32
}

func (m *MockBM25Store) Search(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
	m.searchCalled.Add(1)
	if m.SearchFn != nil {
		return m.SearchFn(ctx, query, limit)
	}
	return nil, nil
}

// Implement remaining BM25Index interface methods (not used in tests)
func (m *MockBM25Store) Index(ctx context.Context, docs []*store.Document) error { return nil }
func (m *MockBM25Store) Delete(ctx context.Context, ids []string) error          { return nil }
func (m *MockBM25Store) AllIDs() ([]string, error)                               { return nil, nil }
func (m *MockBM25Store) Stats() *store.IndexStats                                { return nil }
func (m *MockBM25Store) Save(path string) error                                  { return nil }
func (m *MockBM25Store) Load(path string) error                                  { return nil }
func (m *MockBM25Store) Close() error                                            { return nil }

// =============================================================================
// Constructor Tests
// =============================================================================

func TestNewBM25Searcher_WithStore_Success(t *testing.T) {
	// Given: A valid BM25 store
	mockStore := &MockBM25Store{}

	// When: Creating searcher
	s, err := NewBM25Searcher(WithBM25Store(mockStore))

	// Then: Success
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil searcher")
	}
}

func TestNewBM25Searcher_NilStore_ReturnsError(t *testing.T) {
	// Given: No store

	// When: Creating searcher without store
	s, err := NewBM25Searcher()

	// Then: Error
	if err == nil {
		t.Fatal("expected error for nil store")
	}
	if s != nil {
		t.Fatal("expected nil searcher on error")
	}
	if !errors.Is(err, ErrNilBM25Store) {
		t.Errorf("expected ErrNilBM25Store, got %v", err)
	}
}

// =============================================================================
// Search Tests
// =============================================================================

func TestBM25Searcher_Search_Basic(t *testing.T) {
	// Given: Store returns results
	mockStore := &MockBM25Store{
		SearchFn: func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{
				{DocID: "chunk1", Score: 0.9, MatchedTerms: []string{"search"}},
				{DocID: "chunk2", Score: 0.7, MatchedTerms: []string{"search", "function"}},
			}, nil
		},
	}
	s, _ := NewBM25Searcher(WithBM25Store(mockStore))

	// When: Searching
	results, err := s.Search(context.Background(), "search function", 10)

	// Then: Results converted correctly
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "chunk1" {
		t.Errorf("expected first result ID 'chunk1', got '%s'", results[0].ID)
	}
	if results[0].Score != 0.9 {
		t.Errorf("expected score 0.9, got %f", results[0].Score)
	}
	if len(results[0].MatchedTerms) != 1 {
		t.Errorf("expected 1 matched term, got %d", len(results[0].MatchedTerms))
	}
}

func TestBM25Searcher_Search_EmptyQuery(t *testing.T) {
	// Given: Store returns empty for empty query
	mockStore := &MockBM25Store{
		SearchFn: func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{}, nil
		},
	}
	s, _ := NewBM25Searcher(WithBM25Store(mockStore))

	// When: Searching with empty query
	results, err := s.Search(context.Background(), "", 10)

	// Then: Empty results, no error
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if results == nil {
		t.Fatal("expected non-nil results slice")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestBM25Searcher_Search_StoreError(t *testing.T) {
	// Given: Store returns error
	storeErr := errors.New("store error")
	mockStore := &MockBM25Store{
		SearchFn: func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return nil, storeErr
		},
	}
	s, _ := NewBM25Searcher(WithBM25Store(mockStore))

	// When: Searching
	results, err := s.Search(context.Background(), "test", 10)

	// Then: Error propagated
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, storeErr) {
		t.Errorf("expected store error, got %v", err)
	}
	if results != nil {
		t.Error("expected nil results on error")
	}
}

func TestBM25Searcher_Search_ContextCancelled(t *testing.T) {
	// Given: Cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mockStore := &MockBM25Store{
		SearchFn: func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return nil, ctx.Err()
		},
	}
	s, _ := NewBM25Searcher(WithBM25Store(mockStore))

	// When: Searching with cancelled context
	_, err := s.Search(ctx, "test", 10)

	// Then: Context error
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestBM25Searcher_Search_ZeroLimit(t *testing.T) {
	// Given: Store that respects limit
	mockStore := &MockBM25Store{
		SearchFn: func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			if limit == 0 {
				return []*store.BM25Result{}, nil
			}
			return []*store.BM25Result{{DocID: "1"}}, nil
		},
	}
	s, _ := NewBM25Searcher(WithBM25Store(mockStore))

	// When: Searching with zero limit
	results, err := s.Search(context.Background(), "test", 0)

	// Then: Empty results
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for zero limit, got %d", len(results))
	}
}

// =============================================================================
// Concurrency Tests
// =============================================================================

func TestBM25Searcher_ConcurrentSearch_ThreadSafe(t *testing.T) {
	// Given: Searcher
	mockStore := &MockBM25Store{
		SearchFn: func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{{DocID: "1", Score: 0.5}}, nil
		},
	}
	s, _ := NewBM25Searcher(WithBM25Store(mockStore))

	// When: Concurrent searches
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			_, _ = s.Search(context.Background(), "test", 10)
		}()
	}

	// Then: All complete without race
	for i := 0; i < 10; i++ {
		<-done
	}
}

// =============================================================================
// Interface Compliance
// =============================================================================

func TestBM25Searcher_ImplementsSearcher(t *testing.T) {
	mockStore := &MockBM25Store{}
	s, _ := NewBM25Searcher(WithBM25Store(mockStore))

	var _ Searcher = s
}

var _ Searcher = (*BM25Searcher)(nil)
