package searcher

import (
	"context"
	"fmt"
	"sync"

	"github.com/Aman-CERP/amanmcp/internal/embed"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

// Qwen3QueryInstruction is the instruction prefix for query embeddings.
// This produces asymmetric embeddings optimized for code search.
const Qwen3QueryInstruction = "Instruct: Given a code search query, retrieve relevant code snippets that answer the query\nQuery:"

// formatQueryForEmbedding wraps a query with the Qwen3 instruction prefix.
func formatQueryForEmbedding(query string) string {
	return Qwen3QueryInstruction + " " + query
}

// VectorSearcher performs semantic search using embeddings.
//
// It wraps an embed.Embedder and store.VectorStore to provide the Searcher interface.
// Queries are embedded with the Qwen3 instruction prefix for asymmetric embedding.
// Thread-safe for concurrent use.
type VectorSearcher struct {
	embedder embed.Embedder
	store    store.VectorStore
	mu       sync.RWMutex
}

// VectorOption configures VectorSearcher.
type VectorOption func(*VectorSearcher)

// WithSearchEmbedder sets the embedder for query embedding.
func WithSearchEmbedder(e embed.Embedder) VectorOption {
	return func(s *VectorSearcher) {
		s.embedder = e
	}
}

// WithSearchVectorStore sets the vector store backend.
func WithSearchVectorStore(vs store.VectorStore) VectorOption {
	return func(s *VectorSearcher) {
		s.store = vs
	}
}

// NewVectorSearcher creates a new vector searcher.
//
// Requires both WithSearchEmbedder and WithSearchVectorStore options.
// Returns ErrNilEmbedder or ErrNilVectorStore if dependencies are missing.
func NewVectorSearcher(opts ...VectorOption) (*VectorSearcher, error) {
	s := &VectorSearcher{}

	for _, opt := range opts {
		opt(s)
	}

	if s.embedder == nil {
		return nil, ErrNilEmbedder
	}
	if s.store == nil {
		return nil, ErrNilVectorStore
	}

	return s, nil
}

// Search executes a semantic search and returns ranked results.
//
// The query is:
// 1. Formatted with Qwen3 instruction prefix
// 2. Embedded using the configured embedder
// 3. Searched against the vector store
//
// Returns an empty slice if no results match.
func (s *VectorSearcher) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Format query with instruction prefix for asymmetric embedding
	formattedQuery := formatQueryForEmbedding(query)

	// Embed the query
	embedding, err := s.embedder.Embed(ctx, formattedQuery)
	if err != nil {
		return nil, fmt.Errorf("embedding query failed: %w", err)
	}

	// Search vector store
	vectorResults, err := s.store.Search(ctx, embedding, limit)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	// Convert store results to searcher results
	results := make([]Result, len(vectorResults))
	for i, r := range vectorResults {
		results[i] = Result{
			ID:           r.ID,
			Score:        float64(r.Score),
			MatchedTerms: nil, // Vector search doesn't have matched terms
		}
	}

	return results, nil
}
