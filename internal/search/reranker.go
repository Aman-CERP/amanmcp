package search

import (
	"context"
)

// RerankResult represents a single reranked result
type RerankResult struct {
	// Index is the original position in the input documents slice
	Index int
	// Score is the relevance score (0.0 to 1.0)
	Score float64
	// Document is the original document content
	Document string
}

// Reranker reranks search results using a cross-encoder model.
// Cross-encoders jointly encode query-document pairs for more accurate
// relevance scoring than bi-encoders, but at higher computational cost.
type Reranker interface {
	// Rerank scores and reorders documents by relevance to the query.
	// Returns results sorted by score descending.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//   - query: The search query
	//   - documents: Documents to rerank (max ~50-100 for reasonable latency)
	//   - topK: Optional limit on results (0 = return all)
	//
	// Returns:
	//   - Results sorted by score descending
	//   - Error if reranking fails
	Rerank(ctx context.Context, query string, documents []string, topK int) ([]RerankResult, error)

	// Available checks if the reranker service is available
	Available(ctx context.Context) bool

	// Close releases resources
	Close() error
}

// NoOpReranker is a reranker that returns results in original order.
// Used when reranking is disabled or unavailable.
type NoOpReranker struct{}

// Rerank returns documents in original order with decreasing scores.
func (n *NoOpReranker) Rerank(_ context.Context, _ string, documents []string, topK int) ([]RerankResult, error) {
	results := make([]RerankResult, len(documents))
	for i, doc := range documents {
		// Assign decreasing scores to maintain original order
		results[i] = RerankResult{
			Index:    i,
			Score:    1.0 - float64(i)*0.01, // 1.0, 0.99, 0.98, ...
			Document: doc,
		}
	}

	if topK > 0 && topK < len(results) {
		results = results[:topK]
	}

	return results, nil
}

// Available always returns true for NoOpReranker.
func (n *NoOpReranker) Available(_ context.Context) bool {
	return true
}

// Close is a no-op for NoOpReranker.
func (n *NoOpReranker) Close() error {
	return nil
}

// Verify interface implementation at compile time
var _ Reranker = (*NoOpReranker)(nil)
