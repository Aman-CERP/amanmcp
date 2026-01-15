// Package search provides hybrid search functionality combining BM25 and semantic search.
package search

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// SearchFunc is a function type for executing a single search query.
// This abstraction allows MultiQuerySearcher to be tested without a full Engine.
type SearchFunc func(ctx context.Context, query string, opts SearchOptions) ([]*FusedResult, error)

// MultiQuerySearcher orchestrates multi-query search for improved generic query handling.
//
// FEAT-QI3: This addresses generic query failures by:
// 1. Decomposing generic queries into multiple specific sub-queries
// 2. Running sub-queries in parallel
// 3. Fusing results using multi-RRF with consensus boosting
//
// Example:
//
//	"Search function" decomposes to:
//	- "func Search"     (Go pattern)
//	- "Search method"   (identifier)
//	- "engine.go Search" (file hint)
//
// Documents appearing in multiple sub-query results get boosted,
// surfacing consensus matches above single-source matches.
type MultiQuerySearcher struct {
	decomposer QueryDecomposer
	search     SearchFunc
	fusion     *MultiRRFFusion

	// Configuration
	maxSubQueries int // Maximum sub-queries to run (default: 4)
	parallelism   int // Max parallel searches (default: 4)
}

// MultiQueryOption configures the MultiQuerySearcher.
type MultiQueryOption func(*MultiQuerySearcher)

// WithMaxSubQueries sets the maximum number of sub-queries to run.
func WithMaxSubQueries(n int) MultiQueryOption {
	return func(m *MultiQuerySearcher) {
		if n > 0 {
			m.maxSubQueries = n
		}
	}
}

// WithParallelism sets the maximum number of parallel searches.
func WithParallelism(n int) MultiQueryOption {
	return func(m *MultiQuerySearcher) {
		if n > 0 {
			m.parallelism = n
		}
	}
}

// NewMultiQuerySearcher creates a new multi-query search orchestrator.
func NewMultiQuerySearcher(decomposer QueryDecomposer, search SearchFunc, opts ...MultiQueryOption) *MultiQuerySearcher {
	m := &MultiQuerySearcher{
		decomposer:    decomposer,
		search:        search,
		fusion:        NewMultiRRFFusion(),
		maxSubQueries: 8, // Increased from 4 to include domain-specific patterns
		parallelism:   4,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Search executes a multi-query search if the query benefits from decomposition,
// otherwise falls through to single-query search.
//
// The algorithm:
// 1. Check if query should be decomposed (ShouldDecompose)
// 2. If not, execute single search (pass-through)
// 3. If yes, decompose into sub-queries
// 4. Run sub-queries in parallel
// 5. Fuse results using multi-RRF with consensus boost
// 6. Apply limit and return
func (m *MultiQuerySearcher) Search(ctx context.Context, query string, opts SearchOptions) ([]*MultiFusedResult, error) {
	start := time.Now()

	// Normalize query
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	// Check if decomposition benefits this query
	if !m.decomposer.ShouldDecompose(query) {
		// Pass-through to single search
		results, err := m.search(ctx, query, opts)
		if err != nil {
			return nil, err
		}
		// Convert to MultiFusedResult
		return m.convertToMultiFused(results), nil
	}

	// Decompose query into sub-queries
	subQueries := m.decomposer.Decompose(query)

	// Cap sub-queries
	if len(subQueries) > m.maxSubQueries {
		subQueries = subQueries[:m.maxSubQueries]
	}

	slog.Debug("multi_query_decomposition",
		slog.String("original", query),
		slog.Int("sub_queries", len(subQueries)))

	// Run sub-queries in parallel
	subResults, err := m.parallelSubSearch(ctx, subQueries, opts)
	if err != nil {
		return nil, err
	}

	// Fuse results
	fused := m.fusion.FuseMultiQuery(subResults)

	// Apply limit
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}
	if len(fused) > limit {
		fused = fused[:limit]
	}

	slog.Debug("multi_query_search_complete",
		slog.String("query", query),
		slog.Int("sub_queries", len(subQueries)),
		slog.Int("results", len(fused)),
		slog.Duration("duration", time.Since(start)))

	return fused, nil
}

// parallelSubSearch executes sub-queries in parallel and collects results.
func (m *MultiQuerySearcher) parallelSubSearch(ctx context.Context, subQueries []SubQuery, opts SearchOptions) ([]SubQueryResult, error) {
	// Create result slice with same length as sub-queries
	results := make([]SubQueryResult, len(subQueries))

	// Use errgroup for parallel execution with cancellation
	g, gctx := errgroup.WithContext(ctx)

	// Limit parallelism
	sem := make(chan struct{}, m.parallelism)

	// Track errors
	var mu sync.Mutex
	var firstErr error

	for i, sq := range subQueries {
		i, sq := i, sq // Capture loop variables

		g.Go(func() error {
			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-gctx.Done():
				return gctx.Err()
			}

			// Apply hint as filter if present (only if no explicit filter was set)
			subOpts := opts
			if sq.Hint != "" && (subOpts.Filter == "" || subOpts.Filter == "all") {
				subOpts.Filter = sq.Hint
			}

			// BUG FIX: Use higher internal limit for sub-queries to ensure good fusion.
			// Without this, small limits (e.g., 10) cause sub-queries to miss results
			// that would appear with larger limits, leading to inconsistent rankings.
			// Minimum of 50 ensures enough results for multi-query consensus boosting
			// and allows ranking algorithms (like test file penalty) to work properly.
			minSubQueryLimit := 50
			if subOpts.Limit < minSubQueryLimit {
				subOpts.Limit = minSubQueryLimit
			}

			// Execute search
			searchResults, err := m.search(gctx, sq.Query, subOpts)

			// Store results
			mu.Lock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				// Continue with empty results for this sub-query
				results[i] = SubQueryResult{
					SubQuery: sq,
					Results:  []*FusedResult{},
				}
			} else {
				results[i] = SubQueryResult{
					SubQuery: sq,
					Results:  searchResults,
				}
			}
			mu.Unlock()

			return nil // Don't fail the group on individual search errors
		})
	}

	// Wait for all searches to complete
	if err := g.Wait(); err != nil {
		// Context was cancelled
		return nil, err
	}

	// Log if there were errors but we have partial results
	if firstErr != nil {
		slog.Warn("some sub-queries failed, continuing with partial results",
			slog.String("error", firstErr.Error()))
	}

	return results, nil
}

// convertToMultiFused converts FusedResult slice to MultiFusedResult slice.
// Used for pass-through queries that don't need decomposition.
func (m *MultiQuerySearcher) convertToMultiFused(results []*FusedResult) []*MultiFusedResult {
	if len(results) == 0 {
		return []*MultiFusedResult{}
	}

	multi := make([]*MultiFusedResult, len(results))
	for i, r := range results {
		multi[i] = &MultiFusedResult{
			FusedResult:  *r,
			SubQueryHits: 1, // Single query, so 1 hit
		}
	}
	return multi
}
