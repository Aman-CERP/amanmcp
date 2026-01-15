package searcher

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

// MockEmbedderForSearch implements embed.Embedder for testing.
type MockEmbedderForSearch struct {
	EmbedFn      func(ctx context.Context, text string) ([]float32, error)
	DimensionsFn func() int
	ModelNameFn  func() string

	embedCalled atomic.Int32
}

func (m *MockEmbedderForSearch) Embed(ctx context.Context, text string) ([]float32, error) {
	m.embedCalled.Add(1)
	if m.EmbedFn != nil {
		return m.EmbedFn(ctx, text)
	}
	return []float32{0.1, 0.2, 0.3}, nil
}

func (m *MockEmbedderForSearch) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, nil
}

func (m *MockEmbedderForSearch) Dimensions() int {
	if m.DimensionsFn != nil {
		return m.DimensionsFn()
	}
	return 3
}

func (m *MockEmbedderForSearch) ModelName() string {
	if m.ModelNameFn != nil {
		return m.ModelNameFn()
	}
	return "mock-model"
}

func (m *MockEmbedderForSearch) Available(ctx context.Context) bool { return true }
func (m *MockEmbedderForSearch) Close() error                       { return nil }
func (m *MockEmbedderForSearch) SetBatchIndex(_ int)                {}
func (m *MockEmbedderForSearch) SetFinalBatch(_ bool)               {}

// MockVectorStoreForSearch implements store.VectorStore for testing.
type MockVectorStoreForSearch struct {
	SearchFn func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error)

	searchCalled atomic.Int32
}

func (m *MockVectorStoreForSearch) Search(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
	m.searchCalled.Add(1)
	if m.SearchFn != nil {
		return m.SearchFn(ctx, query, k)
	}
	return nil, nil
}

// Implement remaining VectorStore interface methods (not used in tests)
func (m *MockVectorStoreForSearch) Add(ctx context.Context, ids []string, vectors [][]float32) error {
	return nil
}
func (m *MockVectorStoreForSearch) Delete(ctx context.Context, ids []string) error { return nil }
func (m *MockVectorStoreForSearch) AllIDs() []string                               { return nil }
func (m *MockVectorStoreForSearch) Contains(id string) bool                        { return false }
func (m *MockVectorStoreForSearch) Count() int                                     { return 0 }
func (m *MockVectorStoreForSearch) Save(path string) error                         { return nil }
func (m *MockVectorStoreForSearch) Load(path string) error                         { return nil }
func (m *MockVectorStoreForSearch) Close() error                                   { return nil }

// =============================================================================
// Constructor Tests
// =============================================================================

func TestNewVectorSearcher_WithDependencies_Success(t *testing.T) {
	// Given: Valid embedder and store
	embedder := &MockEmbedderForSearch{}
	vectorStore := &MockVectorStoreForSearch{}

	// When: Creating searcher
	s, err := NewVectorSearcher(
		WithSearchEmbedder(embedder),
		WithSearchVectorStore(vectorStore),
	)

	// Then: Success
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil searcher")
	}
}

func TestNewVectorSearcher_MissingEmbedder_ReturnsError(t *testing.T) {
	// Given: Only store, no embedder
	vectorStore := &MockVectorStoreForSearch{}

	// When: Creating searcher
	s, err := NewVectorSearcher(WithSearchVectorStore(vectorStore))

	// Then: Error
	if err == nil {
		t.Fatal("expected error for missing embedder")
	}
	if s != nil {
		t.Fatal("expected nil searcher on error")
	}
	if !errors.Is(err, ErrNilEmbedder) {
		t.Errorf("expected ErrNilEmbedder, got %v", err)
	}
}

func TestNewVectorSearcher_MissingStore_ReturnsError(t *testing.T) {
	// Given: Only embedder, no store
	embedder := &MockEmbedderForSearch{}

	// When: Creating searcher
	s, err := NewVectorSearcher(WithSearchEmbedder(embedder))

	// Then: Error
	if err == nil {
		t.Fatal("expected error for missing store")
	}
	if s != nil {
		t.Fatal("expected nil searcher on error")
	}
	if !errors.Is(err, ErrNilVectorStore) {
		t.Errorf("expected ErrNilVectorStore, got %v", err)
	}
}

func TestNewVectorSearcher_NoDependencies_ReturnsError(t *testing.T) {
	// Given: No dependencies

	// When: Creating searcher
	s, err := NewVectorSearcher()

	// Then: Error
	if err == nil {
		t.Fatal("expected error for no dependencies")
	}
	if s != nil {
		t.Fatal("expected nil searcher on error")
	}
}

// =============================================================================
// Search Tests
// =============================================================================

func TestVectorSearcher_Search_Basic(t *testing.T) {
	// Given: Embedder and store return results
	embedder := &MockEmbedderForSearch{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
	vectorStore := &MockVectorStoreForSearch{
		SearchFn: func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return []*store.VectorResult{
				{ID: "chunk1", Score: 0.95},
				{ID: "chunk2", Score: 0.80},
			}, nil
		},
	}
	s, _ := NewVectorSearcher(WithSearchEmbedder(embedder), WithSearchVectorStore(vectorStore))

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
	// Float comparison with tolerance for float32->float64 conversion
	if results[0].Score < 0.949 || results[0].Score > 0.951 {
		t.Errorf("expected score ~0.95, got %f", results[0].Score)
	}
	// Vector results don't have matched terms
	if len(results[0].MatchedTerms) != 0 {
		t.Errorf("expected no matched terms for vector results, got %d", len(results[0].MatchedTerms))
	}
}

func TestVectorSearcher_Search_EmbedsQueryCorrectly(t *testing.T) {
	// Given: Embedder that captures the query
	var capturedQuery string
	embedder := &MockEmbedderForSearch{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			capturedQuery = text
			return []float32{0.1}, nil
		},
	}
	vectorStore := &MockVectorStoreForSearch{
		SearchFn: func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return []*store.VectorResult{}, nil
		},
	}
	s, _ := NewVectorSearcher(WithSearchEmbedder(embedder), WithSearchVectorStore(vectorStore))

	// When: Searching
	_, _ = s.Search(context.Background(), "test query", 10)

	// Then: Query was formatted with instruction prefix
	if capturedQuery == "" {
		t.Fatal("expected query to be captured")
	}
	// Should contain the Qwen3 instruction prefix
	if capturedQuery == "test query" {
		t.Error("expected query to be formatted with instruction prefix")
	}
}

func TestVectorSearcher_Search_EmbedderError(t *testing.T) {
	// Given: Embedder returns error
	embedErr := errors.New("embedder error")
	embedder := &MockEmbedderForSearch{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			return nil, embedErr
		},
	}
	vectorStore := &MockVectorStoreForSearch{}
	s, _ := NewVectorSearcher(WithSearchEmbedder(embedder), WithSearchVectorStore(vectorStore))

	// When: Searching
	results, err := s.Search(context.Background(), "test", 10)

	// Then: Error propagated
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, embedErr) {
		t.Errorf("expected embedder error, got %v", err)
	}
	if results != nil {
		t.Error("expected nil results on error")
	}
}

func TestVectorSearcher_Search_StoreError(t *testing.T) {
	// Given: Store returns error
	storeErr := errors.New("store error")
	embedder := &MockEmbedderForSearch{}
	vectorStore := &MockVectorStoreForSearch{
		SearchFn: func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return nil, storeErr
		},
	}
	s, _ := NewVectorSearcher(WithSearchEmbedder(embedder), WithSearchVectorStore(vectorStore))

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

func TestVectorSearcher_Search_ContextCancelled(t *testing.T) {
	// Given: Cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	embedder := &MockEmbedderForSearch{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			return nil, ctx.Err()
		},
	}
	vectorStore := &MockVectorStoreForSearch{}
	s, _ := NewVectorSearcher(WithSearchEmbedder(embedder), WithSearchVectorStore(vectorStore))

	// When: Searching with cancelled context
	_, err := s.Search(ctx, "test", 10)

	// Then: Context error
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestVectorSearcher_Search_EmptyResults(t *testing.T) {
	// Given: Store returns empty
	embedder := &MockEmbedderForSearch{}
	vectorStore := &MockVectorStoreForSearch{
		SearchFn: func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return []*store.VectorResult{}, nil
		},
	}
	s, _ := NewVectorSearcher(WithSearchEmbedder(embedder), WithSearchVectorStore(vectorStore))

	// When: Searching
	results, err := s.Search(context.Background(), "test", 10)

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

// =============================================================================
// Concurrency Tests
// =============================================================================

func TestVectorSearcher_ConcurrentSearch_ThreadSafe(t *testing.T) {
	// Given: Searcher
	embedder := &MockEmbedderForSearch{}
	vectorStore := &MockVectorStoreForSearch{
		SearchFn: func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return []*store.VectorResult{{ID: "1", Score: 0.5}}, nil
		},
	}
	s, _ := NewVectorSearcher(WithSearchEmbedder(embedder), WithSearchVectorStore(vectorStore))

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

func TestVectorSearcher_ImplementsSearcher(t *testing.T) {
	embedder := &MockEmbedderForSearch{}
	vectorStore := &MockVectorStoreForSearch{}
	s, _ := NewVectorSearcher(WithSearchEmbedder(embedder), WithSearchVectorStore(vectorStore))

	var _ Searcher = s
}

var _ Searcher = (*VectorSearcher)(nil)
