package searcher

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"golang.org/x/sync/errgroup"
)

// FusionSearcher combines multiple searchers using Reciprocal Rank Fusion (RRF).
//
// Supports three modes:
//   - Hybrid: Both BM25 and Vector searchers (full fusion)
//   - BM25-only: Just BM25 searcher (lexical search)
//   - Vector-only: Just Vector searcher (semantic search)
//
// Thread-safe for concurrent use.
type FusionSearcher struct {
	bm25   Searcher
	vector Searcher
	config FusionConfig
	mu     sync.RWMutex
}

// FusionOption configures FusionSearcher.
type FusionOption func(*FusionSearcher)

// WithBM25Searcher sets the BM25 searcher for lexical search.
func WithBM25Searcher(s Searcher) FusionOption {
	return func(f *FusionSearcher) {
		f.bm25 = s
	}
}

// WithVectorSearcher sets the Vector searcher for semantic search.
func WithVectorSearcher(s Searcher) FusionOption {
	return func(f *FusionSearcher) {
		f.vector = s
	}
}

// WithFusionConfig sets the RRF fusion configuration.
func WithFusionConfig(config FusionConfig) FusionOption {
	return func(f *FusionSearcher) {
		f.config = config
	}
}

// NewFusionSearcher creates a new fusion searcher.
//
// At least one searcher (BM25 or Vector) must be provided.
// Returns ErrNoSearchers if no searchers are configured.
func NewFusionSearcher(opts ...FusionOption) (*FusionSearcher, error) {
	f := &FusionSearcher{
		config: DefaultFusionConfig(),
	}

	for _, opt := range opts {
		opt(f)
	}

	if f.bm25 == nil && f.vector == nil {
		return nil, ErrNoSearchers
	}

	return f, nil
}

// Search executes search on all configured searchers and fuses results.
//
// Behavior by mode:
//   - Hybrid: Parallel BM25 + Vector search, then RRF fusion
//   - BM25-only: Direct BM25 search
//   - Vector-only: Direct Vector search
//
// Graceful degradation: If one searcher fails, returns results from the other.
// Returns error only if all searchers fail.
func (f *FusionSearcher) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Single searcher modes
	if f.bm25 == nil {
		return f.vector.Search(ctx, query, limit)
	}
	if f.vector == nil {
		return f.bm25.Search(ctx, query, limit)
	}

	// Hybrid mode: parallel search with graceful degradation
	return f.hybridSearch(ctx, query, limit)
}

// hybridSearch runs both searchers in parallel and fuses results.
func (f *FusionSearcher) hybridSearch(ctx context.Context, query string, limit int) ([]Result, error) {
	var (
		bm25Results   []Result
		vectorResults []Result
		bm25Err       error
		vectorErr     error
	)

	// Fetch more results for fusion (2x limit)
	fetchLimit := limit * 2
	if fetchLimit < 20 {
		fetchLimit = 20 // Minimum for good fusion
	}

	// Run searches in parallel
	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		var err error
		bm25Results, err = f.bm25.Search(gctx, query, fetchLimit)
		bm25Err = err
		return nil // Don't fail the group, we handle errors below
	})

	g.Go(func() error {
		var err error
		vectorResults, err = f.vector.Search(gctx, query, fetchLimit)
		vectorErr = err
		return nil // Don't fail the group, we handle errors below
	})

	// Wait for both to complete
	_ = g.Wait()

	// Handle errors with graceful degradation
	if bm25Err != nil && vectorErr != nil {
		return nil, fmt.Errorf("all searchers failed: BM25: %v, Vector: %v", bm25Err, vectorErr)
	}

	// Single-source fallback
	if bm25Err != nil {
		return truncateResults(vectorResults, limit), nil
	}
	if vectorErr != nil {
		return truncateResults(bm25Results, limit), nil
	}

	// Fuse results using RRF
	fused := f.fuseResults(bm25Results, vectorResults)

	return truncateResults(fused, limit), nil
}

// fusedScore tracks score accumulation during RRF fusion.
type fusedScore struct {
	ID           string
	Score        float64
	MatchedTerms []string
	InBoth       bool
}

// fuseResults applies Reciprocal Rank Fusion to combine result lists.
//
// RRF formula: score(d) = Σ weight_i / (k + rank_i)
// Where k is the smoothing constant and rank is 1-indexed.
func (f *FusionSearcher) fuseResults(bm25Results, vectorResults []Result) []Result {
	scores := make(map[string]*fusedScore)

	// Process BM25 results
	for rank, r := range bm25Results {
		rrfScore := f.config.BM25Weight / float64(f.config.RRFConstant+rank+1)
		scores[r.ID] = &fusedScore{
			ID:           r.ID,
			Score:        rrfScore,
			MatchedTerms: r.MatchedTerms,
		}
	}

	// Process Vector results
	for rank, r := range vectorResults {
		rrfScore := f.config.SemanticWeight / float64(f.config.RRFConstant+rank+1)
		if existing, ok := scores[r.ID]; ok {
			existing.Score += rrfScore
			existing.InBoth = true
		} else {
			scores[r.ID] = &fusedScore{
				ID:    r.ID,
				Score: rrfScore,
			}
		}
	}

	// Convert to slice and sort by score
	results := make([]Result, 0, len(scores))
	for _, s := range scores {
		results = append(results, Result{
			ID:           s.ID,
			Score:        s.Score,
			MatchedTerms: s.MatchedTerms,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		// Stable sort by ID for deterministic ordering
		return results[i].ID < results[j].ID
	})

	return results
}

// truncateResults returns at most limit results.
func truncateResults(results []Result, limit int) []Result {
	if len(results) <= limit {
		return results
	}
	return results[:limit]
}
