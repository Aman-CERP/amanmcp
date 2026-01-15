package search

import (
	"context"
	"sync"
	"testing"
)

// TestMultiQuerySearcher tests the multi-query search orchestrator.
func TestMultiQuerySearcher(t *testing.T) {
	t.Run("non-decomposable query passes through", func(t *testing.T) {
		// Create mock search function that tracks calls
		callCount := 0
		mockSearch := func(ctx context.Context, query string, opts SearchOptions) ([]*FusedResult, error) {
			callCount++
			return []*FusedResult{
				{ChunkID: "chunk1", RRFScore: 0.9},
			}, nil
		}

		decomposer := NewPatternDecomposer()
		searcher := NewMultiQuerySearcher(decomposer, mockSearch)

		ctx := context.Background()
		results, err := searcher.Search(ctx, "OllamaEmbedder", SearchOptions{Limit: 10})

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Should call search exactly once (pass-through)
		if callCount != 1 {
			t.Errorf("Expected 1 search call for non-decomposable query, got %d", callCount)
		}

		if len(results) != 1 {
			t.Errorf("Expected 1 result, got %d", len(results))
		}
	})

	t.Run("decomposable query runs multiple searches", func(t *testing.T) {
		var mu sync.Mutex
		callCount := 0
		queries := make([]string, 0)
		mockSearch := func(ctx context.Context, query string, opts SearchOptions) ([]*FusedResult, error) {
			mu.Lock()
			callCount++
			queries = append(queries, query)
			mu.Unlock()
			return []*FusedResult{
				{ChunkID: "chunk1", RRFScore: 0.8},
			}, nil
		}

		decomposer := NewPatternDecomposer()
		searcher := NewMultiQuerySearcher(decomposer, mockSearch)

		ctx := context.Background()
		results, err := searcher.Search(ctx, "Search function", SearchOptions{Limit: 10})

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Should call search multiple times (one per sub-query)
		if callCount < 3 {
			t.Errorf("Expected at least 3 search calls for 'Search function', got %d", callCount)
		}

		// Should have results
		if len(results) == 0 {
			t.Error("Expected results from multi-query search")
		}
	})

	t.Run("multi-query fusion boosts consensus", func(t *testing.T) {
		// Simulate scenario where engine.go appears in multiple sub-query results
		mockSearch := func(ctx context.Context, query string, opts SearchOptions) ([]*FusedResult, error) {
			// Different sub-queries return different results, but engine.go appears in all
			switch {
			case query == "func Search" || containsString(query, "func Search"):
				return []*FusedResult{
					{ChunkID: "engine.go:Search", RRFScore: 0.8},
					{ChunkID: "test_search.go", RRFScore: 0.7},
				}, nil
			case query == "Search method" || containsString(query, "method"):
				return []*FusedResult{
					{ChunkID: "engine.go:Search", RRFScore: 0.75},
					{ChunkID: "docs/search.md", RRFScore: 0.6},
				}, nil
			default:
				return []*FusedResult{
					{ChunkID: "engine.go:Search", RRFScore: 0.85},
				}, nil
			}
		}

		decomposer := NewPatternDecomposer()
		searcher := NewMultiQuerySearcher(decomposer, mockSearch)

		ctx := context.Background()
		results, err := searcher.Search(ctx, "Search function", SearchOptions{Limit: 10})

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// engine.go:Search should be first (appears in all sub-queries)
		if len(results) < 1 || results[0].ChunkID != "engine.go:Search" {
			var ids []string
			for _, r := range results {
				ids = append(ids, r.ChunkID)
			}
			t.Errorf("Expected engine.go:Search first (consensus), got %v", ids)
		}
	})

	t.Run("respects limit option", func(t *testing.T) {
		mockSearch := func(ctx context.Context, query string, opts SearchOptions) ([]*FusedResult, error) {
			return []*FusedResult{
				{ChunkID: "chunk1", RRFScore: 0.9},
				{ChunkID: "chunk2", RRFScore: 0.8},
				{ChunkID: "chunk3", RRFScore: 0.7},
				{ChunkID: "chunk4", RRFScore: 0.6},
				{ChunkID: "chunk5", RRFScore: 0.5},
			}, nil
		}

		decomposer := NewPatternDecomposer()
		searcher := NewMultiQuerySearcher(decomposer, mockSearch)

		ctx := context.Background()
		results, err := searcher.Search(ctx, "Search function", SearchOptions{Limit: 3})

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(results) > 3 {
			t.Errorf("Expected at most 3 results (limit), got %d", len(results))
		}
	})

	t.Run("handles empty results gracefully", func(t *testing.T) {
		mockSearch := func(ctx context.Context, query string, opts SearchOptions) ([]*FusedResult, error) {
			return []*FusedResult{}, nil
		}

		decomposer := NewPatternDecomposer()
		searcher := NewMultiQuerySearcher(decomposer, mockSearch)

		ctx := context.Background()
		results, err := searcher.Search(ctx, "Search function", SearchOptions{Limit: 10})

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if results == nil {
			t.Error("Expected empty slice, got nil")
		}
	})

	t.Run("empty query returns nil", func(t *testing.T) {
		mockSearch := func(ctx context.Context, query string, opts SearchOptions) ([]*FusedResult, error) {
			t.Error("Search should not be called for empty query")
			return nil, nil
		}

		decomposer := NewPatternDecomposer()
		searcher := NewMultiQuerySearcher(decomposer, mockSearch)

		ctx := context.Background()
		results, err := searcher.Search(ctx, "", SearchOptions{Limit: 10})

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if results != nil {
			t.Errorf("Expected nil for empty query, got %v", results)
		}
	})
}

// TestMultiQuerySearcherIntegration tests integration scenarios.
func TestMultiQuerySearcherIntegration(t *testing.T) {
	t.Run("Index function decomposition", func(t *testing.T) {
		var mu sync.Mutex
		searchedQueries := make([]string, 0)
		mockSearch := func(ctx context.Context, query string, opts SearchOptions) ([]*FusedResult, error) {
			mu.Lock()
			searchedQueries = append(searchedQueries, query)
			mu.Unlock()
			return []*FusedResult{
				{ChunkID: "index/coordinator.go", RRFScore: 0.8},
			}, nil
		}

		decomposer := NewPatternDecomposer()
		searcher := NewMultiQuerySearcher(decomposer, mockSearch)

		ctx := context.Background()
		_, err := searcher.Search(ctx, "Index function", SearchOptions{Limit: 10})

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Check that appropriate sub-queries were generated
		hasIndex := false
		hasFuncIndex := false
		for _, q := range searchedQueries {
			if q == "Index" || containsString(q, "Index") {
				hasIndex = true
			}
			if q == "func Index" || containsString(q, "func Index") {
				hasFuncIndex = true
			}
		}

		if !hasIndex {
			t.Errorf("Expected 'Index' in sub-queries, got %v", searchedQueries)
		}
		if !hasFuncIndex {
			t.Errorf("Expected 'func Index' in sub-queries, got %v", searchedQueries)
		}
	})

	t.Run("How does RRF fusion work decomposition", func(t *testing.T) {
		var mu sync.Mutex
		searchedQueries := make([]string, 0)
		mockSearch := func(ctx context.Context, query string, opts SearchOptions) ([]*FusedResult, error) {
			mu.Lock()
			searchedQueries = append(searchedQueries, query)
			mu.Unlock()
			return []*FusedResult{
				{ChunkID: "fusion.go:Fuse", RRFScore: 0.9},
			}, nil
		}

		decomposer := NewPatternDecomposer()
		searcher := NewMultiQuerySearcher(decomposer, mockSearch)

		ctx := context.Background()
		_, err := searcher.Search(ctx, "How does RRF fusion work", SearchOptions{Limit: 10})

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Check that RRF and fusion terms were searched
		hasRRF := false
		hasFusion := false
		for _, q := range searchedQueries {
			if q == "RRF" || containsString(q, "RRF") {
				hasRRF = true
			}
			if containsString(q, "fusion") {
				hasFusion = true
			}
		}

		if !hasRRF {
			t.Errorf("Expected 'RRF' in sub-queries, got %v", searchedQueries)
		}
		if !hasFusion {
			t.Errorf("Expected 'fusion' in sub-queries, got %v", searchedQueries)
		}
	})
}

// Helper function to check if a string contains a substring.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || findSubstringInTest(s, substr))
}

func findSubstringInTest(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// =============================================================================
// DEBT-028: MultiQuerySearcher Option Tests
// =============================================================================

func TestWithMaxSubQueries_SetsValue(t *testing.T) {
	mockSearch := func(ctx context.Context, query string, opts SearchOptions) ([]*FusedResult, error) {
		return []*FusedResult{}, nil
	}

	decomposer := NewPatternDecomposer()

	// When: creating with WithMaxSubQueries
	searcher := NewMultiQuerySearcher(decomposer, mockSearch, WithMaxSubQueries(2))

	// Then: maxSubQueries is set
	if searcher.maxSubQueries != 2 {
		t.Errorf("Expected maxSubQueries=2, got %d", searcher.maxSubQueries)
	}
}

func TestWithMaxSubQueries_IgnoresZeroOrNegative(t *testing.T) {
	mockSearch := func(ctx context.Context, query string, opts SearchOptions) ([]*FusedResult, error) {
		return []*FusedResult{}, nil
	}

	decomposer := NewPatternDecomposer()

	// When: creating with zero value
	searcher := NewMultiQuerySearcher(decomposer, mockSearch, WithMaxSubQueries(0))

	// Then: default value is kept
	if searcher.maxSubQueries != 8 { // Default is 8
		t.Errorf("Expected maxSubQueries=8 (default), got %d", searcher.maxSubQueries)
	}

	// When: creating with negative value
	searcher2 := NewMultiQuerySearcher(decomposer, mockSearch, WithMaxSubQueries(-5))

	// Then: default value is kept
	if searcher2.maxSubQueries != 8 {
		t.Errorf("Expected maxSubQueries=8 (default), got %d", searcher2.maxSubQueries)
	}
}

func TestWithParallelism_SetsValue(t *testing.T) {
	mockSearch := func(ctx context.Context, query string, opts SearchOptions) ([]*FusedResult, error) {
		return []*FusedResult{}, nil
	}

	decomposer := NewPatternDecomposer()

	// When: creating with WithParallelism
	searcher := NewMultiQuerySearcher(decomposer, mockSearch, WithParallelism(8))

	// Then: parallelism is set
	if searcher.parallelism != 8 {
		t.Errorf("Expected parallelism=8, got %d", searcher.parallelism)
	}
}

func TestWithParallelism_IgnoresZeroOrNegative(t *testing.T) {
	mockSearch := func(ctx context.Context, query string, opts SearchOptions) ([]*FusedResult, error) {
		return []*FusedResult{}, nil
	}

	decomposer := NewPatternDecomposer()

	// When: creating with zero value
	searcher := NewMultiQuerySearcher(decomposer, mockSearch, WithParallelism(0))

	// Then: default value is kept
	if searcher.parallelism != 4 { // Default is 4
		t.Errorf("Expected parallelism=4 (default), got %d", searcher.parallelism)
	}

	// When: creating with negative value
	searcher2 := NewMultiQuerySearcher(decomposer, mockSearch, WithParallelism(-1))

	// Then: default value is kept
	if searcher2.parallelism != 4 {
		t.Errorf("Expected parallelism=4 (default), got %d", searcher2.parallelism)
	}
}

func TestMultiQuerySearcher_MultipleOptions(t *testing.T) {
	mockSearch := func(ctx context.Context, query string, opts SearchOptions) ([]*FusedResult, error) {
		return []*FusedResult{}, nil
	}

	decomposer := NewPatternDecomposer()

	// When: creating with multiple options
	searcher := NewMultiQuerySearcher(decomposer, mockSearch,
		WithMaxSubQueries(3),
		WithParallelism(2),
	)

	// Then: all options are applied
	if searcher.maxSubQueries != 3 {
		t.Errorf("Expected maxSubQueries=3, got %d", searcher.maxSubQueries)
	}
	if searcher.parallelism != 2 {
		t.Errorf("Expected parallelism=2, got %d", searcher.parallelism)
	}
}
