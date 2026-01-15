package searcher

import (
	"context"
	"fmt"
	"sync"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

// BM25Searcher performs lexical search using BM25 algorithm.
//
// It wraps a store.BM25Index to provide the Searcher interface.
// Thread-safe for concurrent use.
type BM25Searcher struct {
	store store.BM25Index
	mu    sync.RWMutex
}

// BM25Option configures BM25Searcher.
type BM25Option func(*BM25Searcher)

// WithBM25Store sets the BM25 store backend.
func WithBM25Store(s store.BM25Index) BM25Option {
	return func(searcher *BM25Searcher) {
		searcher.store = s
	}
}

// NewBM25Searcher creates a new BM25 searcher.
//
// Requires WithBM25Store option. Returns ErrNilBM25Store if store is nil.
func NewBM25Searcher(opts ...BM25Option) (*BM25Searcher, error) {
	s := &BM25Searcher{}

	for _, opt := range opts {
		opt(s)
	}

	if s.store == nil {
		return nil, ErrNilBM25Store
	}

	return s, nil
}

// Search executes a BM25 search and returns ranked results.
//
// The query is passed directly to the BM25 index.
// Returns an empty slice if no results match.
func (s *BM25Searcher) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bm25Results, err := s.store.Search(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("BM25 search failed: %w", err)
	}

	// Convert store results to searcher results
	results := make([]Result, len(bm25Results))
	for i, r := range bm25Results {
		results[i] = Result{
			ID:           r.DocID,
			Score:        r.Score,
			MatchedTerms: r.MatchedTerms,
		}
	}

	return results, nil
}
