package searcher

import (
	"context"
	"errors"
)

// ErrNilBM25Store is returned when attempting to create a BM25Searcher without a store.
var ErrNilBM25Store = errors.New("BM25 store is required")

// ErrNilEmbedder is returned when attempting to create a VectorSearcher without an embedder.
var ErrNilEmbedder = errors.New("embedder is required")

// ErrNilVectorStore is returned when attempting to create a VectorSearcher without a store.
var ErrNilVectorStore = errors.New("vector store is required")

// ErrNoSearchers is returned when attempting to create a FusionSearcher without any searchers.
var ErrNoSearchers = errors.New("at least one searcher is required")

// Searcher performs search operations and returns ranked results.
//
// Implementations must be thread-safe for concurrent use.
type Searcher interface {
	// Search executes a search query and returns ranked results.
	//
	// Parameters:
	//   - ctx: Context for cancellation and deadlines
	//   - query: The search query string
	//   - limit: Maximum number of results to return
	//
	// Returns an empty slice (not nil) if no results match.
	// Returns an error if the search fails.
	Search(ctx context.Context, query string, limit int) ([]Result, error)
}

// Result represents a single search result.
type Result struct {
	// ID is the unique identifier for the matched chunk.
	ID string

	// Score is the normalized relevance score (0-1).
	// Higher scores indicate more relevant results.
	Score float64

	// MatchedTerms contains the query terms that matched (BM25 only).
	// May be empty for vector search results.
	MatchedTerms []string
}

// FusionConfig configures the RRF fusion algorithm.
type FusionConfig struct {
	// BM25Weight is the weight for BM25 results in fusion.
	// Default: 0.35
	BM25Weight float64

	// SemanticWeight is the weight for vector/semantic results in fusion.
	// Default: 0.65
	SemanticWeight float64

	// RRFConstant is the smoothing constant for RRF.
	// Default: 60
	RRFConstant int
}

// DefaultFusionConfig returns the default fusion configuration.
//
// Weights are tuned for code search where semantic similarity
// is slightly more important than lexical matching.
func DefaultFusionConfig() FusionConfig {
	return FusionConfig{
		BM25Weight:     0.35,
		SemanticWeight: 0.65,
		RRFConstant:    60,
	}
}
