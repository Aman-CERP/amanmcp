package searcher

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

// MockSearcher implements Searcher for testing FusionSearcher.
type MockSearcher struct {
	SearchFn func(ctx context.Context, query string, limit int) ([]Result, error)

	searchCalled atomic.Int32
}

func (m *MockSearcher) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	m.searchCalled.Add(1)
	if m.SearchFn != nil {
		return m.SearchFn(ctx, query, limit)
	}
	return nil, nil
}

// =============================================================================
// Constructor Tests
// =============================================================================

func TestNewFusionSearcher_WithBothSearchers_Success(t *testing.T) {
	// Given: Both BM25 and Vector searchers
	bm25 := &MockSearcher{}
	vector := &MockSearcher{}

	// When: Creating fusion searcher
	s, err := NewFusionSearcher(
		WithBM25Searcher(bm25),
		WithVectorSearcher(vector),
	)

	// Then: Success
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil searcher")
	}
}

func TestNewFusionSearcher_BM25Only_Success(t *testing.T) {
	// Given: Only BM25 searcher (BM25-only mode)
	bm25 := &MockSearcher{}

	// When: Creating fusion searcher
	s, err := NewFusionSearcher(WithBM25Searcher(bm25))

	// Then: Success (graceful degradation)
	if err != nil {
		t.Fatalf("expected no error for BM25-only mode, got %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil searcher")
	}
}

func TestNewFusionSearcher_VectorOnly_Success(t *testing.T) {
	// Given: Only Vector searcher
	vector := &MockSearcher{}

	// When: Creating fusion searcher
	s, err := NewFusionSearcher(WithVectorSearcher(vector))

	// Then: Success (graceful degradation)
	if err != nil {
		t.Fatalf("expected no error for vector-only mode, got %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil searcher")
	}
}

func TestNewFusionSearcher_NoSearchers_ReturnsError(t *testing.T) {
	// Given: No searchers

	// When: Creating fusion searcher
	s, err := NewFusionSearcher()

	// Then: Error
	if err == nil {
		t.Fatal("expected error for no searchers")
	}
	if s != nil {
		t.Fatal("expected nil searcher on error")
	}
	if !errors.Is(err, ErrNoSearchers) {
		t.Errorf("expected ErrNoSearchers, got %v", err)
	}
}

func TestNewFusionSearcher_WithCustomConfig_Success(t *testing.T) {
	// Given: Custom fusion config
	bm25 := &MockSearcher{}
	config := FusionConfig{
		BM25Weight:     0.5,
		SemanticWeight: 0.5,
		RRFConstant:    100,
	}

	// When: Creating fusion searcher with custom config
	s, err := NewFusionSearcher(
		WithBM25Searcher(bm25),
		WithFusionConfig(config),
	)

	// Then: Success
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil searcher")
	}
}

// =============================================================================
// Search Tests - Hybrid Mode
// =============================================================================

func TestFusionSearcher_Search_FusesBothResults(t *testing.T) {
	// Given: Both searchers return results
	bm25 := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return []Result{
				{ID: "chunk1", Score: 0.9, MatchedTerms: []string{"search"}},
				{ID: "chunk2", Score: 0.7, MatchedTerms: []string{"function"}},
			}, nil
		},
	}
	vector := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return []Result{
				{ID: "chunk2", Score: 0.95}, // Same as BM25's chunk2
				{ID: "chunk3", Score: 0.8},
			}, nil
		},
	}
	s, _ := NewFusionSearcher(WithBM25Searcher(bm25), WithVectorSearcher(vector))

	// When: Searching
	results, err := s.Search(context.Background(), "search function", 10)

	// Then: Results fused correctly
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}

	// chunk2 appears in both, should have highest fused score
	foundChunk2 := false
	for _, r := range results {
		if r.ID == "chunk2" {
			foundChunk2 = true
			break
		}
	}
	if !foundChunk2 {
		t.Error("expected chunk2 (in both result sets) to be in fused results")
	}
}

func TestFusionSearcher_Search_CallsBothSearchers(t *testing.T) {
	// Given: Both searchers
	bm25 := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return []Result{}, nil
		},
	}
	vector := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return []Result{}, nil
		},
	}
	s, _ := NewFusionSearcher(WithBM25Searcher(bm25), WithVectorSearcher(vector))

	// When: Searching
	_, _ = s.Search(context.Background(), "test", 10)

	// Then: Both searchers called
	if bm25.searchCalled.Load() != 1 {
		t.Errorf("expected BM25 searcher called once, got %d", bm25.searchCalled.Load())
	}
	if vector.searchCalled.Load() != 1 {
		t.Errorf("expected Vector searcher called once, got %d", vector.searchCalled.Load())
	}
}

func TestFusionSearcher_Search_RespectsLimit(t *testing.T) {
	// Given: Searchers return many results
	bm25 := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			results := make([]Result, 20)
			for i := range results {
				results[i] = Result{ID: "bm25-" + string(rune('a'+i)), Score: float64(20-i) / 20}
			}
			return results, nil
		},
	}
	vector := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			results := make([]Result, 20)
			for i := range results {
				results[i] = Result{ID: "vec-" + string(rune('a'+i)), Score: float64(20-i) / 20}
			}
			return results, nil
		},
	}
	s, _ := NewFusionSearcher(WithBM25Searcher(bm25), WithVectorSearcher(vector))

	// When: Searching with limit
	results, err := s.Search(context.Background(), "test", 5)

	// Then: Results limited
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) > 5 {
		t.Errorf("expected at most 5 results, got %d", len(results))
	}
}

func TestFusionSearcher_Search_PreservesMatchedTerms(t *testing.T) {
	// Given: BM25 returns matched terms
	bm25 := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return []Result{
				{ID: "chunk1", Score: 0.9, MatchedTerms: []string{"search", "function"}},
			}, nil
		},
	}
	vector := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return []Result{}, nil
		},
	}
	s, _ := NewFusionSearcher(WithBM25Searcher(bm25), WithVectorSearcher(vector))

	// When: Searching
	results, err := s.Search(context.Background(), "search function", 10)

	// Then: Matched terms preserved
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if len(results[0].MatchedTerms) == 0 {
		t.Error("expected matched terms to be preserved from BM25 results")
	}
}

// =============================================================================
// Search Tests - Single Searcher Mode
// =============================================================================

func TestFusionSearcher_Search_BM25Only_ReturnsResults(t *testing.T) {
	// Given: Only BM25 searcher
	bm25 := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return []Result{
				{ID: "chunk1", Score: 0.9, MatchedTerms: []string{"test"}},
			}, nil
		},
	}
	s, _ := NewFusionSearcher(WithBM25Searcher(bm25))

	// When: Searching
	results, err := s.Search(context.Background(), "test", 10)

	// Then: BM25-only results returned
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "chunk1" {
		t.Errorf("expected chunk1, got %s", results[0].ID)
	}
}

func TestFusionSearcher_Search_VectorOnly_ReturnsResults(t *testing.T) {
	// Given: Only Vector searcher
	vector := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return []Result{
				{ID: "chunk1", Score: 0.95},
			}, nil
		},
	}
	s, _ := NewFusionSearcher(WithVectorSearcher(vector))

	// When: Searching
	results, err := s.Search(context.Background(), "test", 10)

	// Then: Vector-only results returned
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "chunk1" {
		t.Errorf("expected chunk1, got %s", results[0].ID)
	}
}

// =============================================================================
// Search Tests - Error Handling
// =============================================================================

func TestFusionSearcher_Search_BM25Error_ReturnsVectorResults(t *testing.T) {
	// Given: BM25 fails, Vector succeeds
	bm25Err := errors.New("bm25 error")
	bm25 := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return nil, bm25Err
		},
	}
	vector := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return []Result{{ID: "vec1", Score: 0.9}}, nil
		},
	}
	s, _ := NewFusionSearcher(WithBM25Searcher(bm25), WithVectorSearcher(vector))

	// When: Searching
	results, err := s.Search(context.Background(), "test", 10)

	// Then: Graceful degradation - vector results returned
	if err != nil {
		t.Fatalf("expected graceful degradation, got error %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result from vector, got %d", len(results))
	}
}

func TestFusionSearcher_Search_VectorError_ReturnsBM25Results(t *testing.T) {
	// Given: Vector fails, BM25 succeeds
	vectorErr := errors.New("vector error")
	bm25 := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return []Result{{ID: "bm25-1", Score: 0.9, MatchedTerms: []string{"test"}}}, nil
		},
	}
	vector := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return nil, vectorErr
		},
	}
	s, _ := NewFusionSearcher(WithBM25Searcher(bm25), WithVectorSearcher(vector))

	// When: Searching
	results, err := s.Search(context.Background(), "test", 10)

	// Then: Graceful degradation - BM25 results returned
	if err != nil {
		t.Fatalf("expected graceful degradation, got error %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result from BM25, got %d", len(results))
	}
}

func TestFusionSearcher_Search_BothError_ReturnsError(t *testing.T) {
	// Given: Both searchers fail
	bm25Err := errors.New("bm25 error")
	vectorErr := errors.New("vector error")
	bm25 := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return nil, bm25Err
		},
	}
	vector := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return nil, vectorErr
		},
	}
	s, _ := NewFusionSearcher(WithBM25Searcher(bm25), WithVectorSearcher(vector))

	// When: Searching
	results, err := s.Search(context.Background(), "test", 10)

	// Then: Error returned
	if err == nil {
		t.Fatal("expected error when both searchers fail")
	}
	if results != nil {
		t.Error("expected nil results on error")
	}
}

func TestFusionSearcher_Search_ContextCancelled(t *testing.T) {
	// Given: Cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	bm25 := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return nil, ctx.Err()
		},
	}
	vector := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return nil, ctx.Err()
		},
	}
	s, _ := NewFusionSearcher(WithBM25Searcher(bm25), WithVectorSearcher(vector))

	// When: Searching with cancelled context
	_, err := s.Search(ctx, "test", 10)

	// Then: Error returned (both searchers failed with context error)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	// Note: FusionSearcher wraps both errors, so we check if error message contains info about failures
	// The underlying context.Canceled errors are wrapped in the combined error message
}

// =============================================================================
// Search Tests - Empty Results
// =============================================================================

func TestFusionSearcher_Search_BothEmpty_ReturnsEmpty(t *testing.T) {
	// Given: Both searchers return empty
	bm25 := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return []Result{}, nil
		},
	}
	vector := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return []Result{}, nil
		},
	}
	s, _ := NewFusionSearcher(WithBM25Searcher(bm25), WithVectorSearcher(vector))

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
// RRF Fusion Tests
// =============================================================================

func TestFusionSearcher_Search_RRFScoring(t *testing.T) {
	// Given: Results with known rankings
	// Using default config: k=60, BM25=0.35, Semantic=0.65
	bm25 := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return []Result{
				{ID: "A", Score: 0.9}, // rank 1 in BM25
				{ID: "B", Score: 0.8}, // rank 2 in BM25
			}, nil
		},
	}
	vector := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return []Result{
				{ID: "B", Score: 0.95}, // rank 1 in Vector
				{ID: "C", Score: 0.85}, // rank 2 in Vector
			}, nil
		},
	}
	s, _ := NewFusionSearcher(WithBM25Searcher(bm25), WithVectorSearcher(vector))

	// When: Searching
	results, err := s.Search(context.Background(), "test", 10)

	// Then: Results sorted by RRF score
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// B appears in both lists, should have highest score
	// RRF(B) = 0.35/(60+2) + 0.65/(60+1) ≈ 0.0056 + 0.0107 ≈ 0.0163
	// RRF(A) = 0.35/(60+1) ≈ 0.0057
	// RRF(C) = 0.65/(60+2) ≈ 0.0105
	// Order should be: B > C > A (or B > A > C depending on exact calculation)
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	if results[0].ID != "B" {
		t.Errorf("expected B (in both lists) to be ranked first, got %s", results[0].ID)
	}
}

func TestFusionSearcher_Search_CustomWeights(t *testing.T) {
	// Given: Equal weights (50/50)
	bm25 := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return []Result{
				{ID: "A", Score: 0.9},
			}, nil
		},
	}
	vector := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return []Result{
				{ID: "B", Score: 0.95},
			}, nil
		},
	}
	config := FusionConfig{
		BM25Weight:     0.5,
		SemanticWeight: 0.5,
		RRFConstant:    60,
	}
	s, _ := NewFusionSearcher(
		WithBM25Searcher(bm25),
		WithVectorSearcher(vector),
		WithFusionConfig(config),
	)

	// When: Searching
	results, err := s.Search(context.Background(), "test", 10)

	// Then: Both results present with equal RRF scores
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// With equal weights and same rank, scores should be equal
	// Both are rank 1 in their respective lists
	// RRF(A) = 0.5/(60+1) ≈ 0.0082
	// RRF(B) = 0.5/(60+1) ≈ 0.0082
	// Order is stable (implementation-dependent)
}

// =============================================================================
// Concurrency Tests
// =============================================================================

func TestFusionSearcher_ConcurrentSearch_ThreadSafe(t *testing.T) {
	// Given: Fusion searcher
	bm25 := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return []Result{{ID: "1", Score: 0.5}}, nil
		},
	}
	vector := &MockSearcher{
		SearchFn: func(ctx context.Context, query string, limit int) ([]Result, error) {
			return []Result{{ID: "2", Score: 0.5}}, nil
		},
	}
	s, _ := NewFusionSearcher(WithBM25Searcher(bm25), WithVectorSearcher(vector))

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

func TestFusionSearcher_ImplementsSearcher(t *testing.T) {
	bm25 := &MockSearcher{}
	s, _ := NewFusionSearcher(WithBM25Searcher(bm25))

	var _ Searcher = s
}

var _ Searcher = (*FusionSearcher)(nil)
