// Package search provides hybrid search functionality combining BM25 and semantic search.
// Results are fused using Reciprocal Rank Fusion (RRF) for robust rank-based scoring.
package search

import (
	"context"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

// SearchEngine provides hybrid search combining BM25 and semantic search.
type SearchEngine interface {
	// Search executes a hybrid search query and returns ranked results.
	Search(ctx context.Context, query string, opts SearchOptions) ([]*SearchResult, error)

	// Index adds chunks to both BM25 and vector indices.
	Index(ctx context.Context, chunks []*store.Chunk) error

	// Delete removes chunks from both indices.
	Delete(ctx context.Context, chunkIDs []string) error

	// Stats returns engine statistics.
	Stats() *EngineStats

	// Close releases all resources.
	Close() error
}

// SearchOptions configures a search query.
type SearchOptions struct {
	// Limit is the maximum number of results to return (default: 10, max: 100).
	Limit int

	// Filter restricts results by content type: "all", "code", "docs".
	Filter string

	// Language filters results by programming language (e.g., "go", "typescript").
	Language string

	// SymbolType filters results by symbol type (e.g., "function", "class").
	SymbolType string

	// Weights overrides the default BM25/semantic weights.
	Weights *Weights

	// Scopes restricts results to files within these path prefixes.
	// Multiple scopes use OR logic (matches if file is within ANY scope).
	// Empty slice means no scope filtering.
	Scopes []string

	// BM25Only forces keyword-only search, skipping semantic/vector search entirely.
	// FEAT-DIM1: Useful when embedder is unavailable or for exact keyword matching.
	BM25Only bool

	// AdjacentChunks specifies how many chunks before/after to retrieve for context.
	// FEAT-QI5: Adjacent chunk retrieval for context continuity.
	// 0 = disabled (default), 1 = fetch 1 before + 1 after, 2 = fetch 2 each.
	AdjacentChunks int

	// Explain enables detailed search explanation mode.
	// FEAT-UNIX3: When true, returns ExplainData with search decision details.
	Explain bool
}

// Weights configures the relative importance of BM25 vs semantic search.
type Weights struct {
	// BM25 is the weight for keyword search (0-1, default: 0.35).
	BM25 float64

	// Semantic is the weight for vector search (0-1, default: 0.65).
	Semantic float64
}

// DefaultWeights returns the default search weights optimized for mixed queries.
func DefaultWeights() Weights {
	return Weights{
		BM25:     0.35,
		Semantic: 0.65,
	}
}

// SearchResult represents a single search result with scores and metadata.
type SearchResult struct {
	// Chunk contains the full chunk data from MetadataStore.
	Chunk *store.Chunk

	// Score is the combined normalized score (0-1).
	Score float64

	// BM25Score is the individual BM25 score (normalized).
	BM25Score float64

	// VecScore is the individual vector similarity score (0-1).
	VecScore float64

	// BM25Rank is the position in BM25 results (1-indexed, 0 if absent).
	// FEAT-UNIX3: Exposed for search explanation mode.
	BM25Rank int

	// VecRank is the position in vector results (1-indexed, 0 if absent).
	// FEAT-UNIX3: Exposed for search explanation mode.
	VecRank int

	// Highlights contains text ranges where query terms matched.
	Highlights []Range

	// InBothLists indicates the result appeared in both BM25 and vector results.
	InBothLists bool

	// MatchedTerms contains the BM25 query terms that matched this result.
	// UX-1: Exposed for context-rich result display.
	MatchedTerms []string

	// AdjacentContext contains chunks before/after this result for context.
	// FEAT-QI5: Adjacent chunk retrieval for context continuity.
	AdjacentContext AdjacentContext

	// Explain contains detailed search decision information when opts.Explain=true.
	// FEAT-UNIX3: Only populated on the first result to avoid duplication.
	Explain *ExplainData
}

// AdjacentContext contains surrounding chunks for context continuity.
// FEAT-QI5: This improves "How does X work" queries by providing
// implementation context that may span multiple chunks.
type AdjacentContext struct {
	// Before contains chunks appearing before this one in the same file.
	// Sorted by proximity (closest first).
	Before []*store.Chunk

	// After contains chunks appearing after this one in the same file.
	// Sorted by proximity (closest first).
	After []*store.Chunk
}

// Range represents a text range for highlighting.
type Range struct {
	// Start is the starting character offset (0-indexed).
	Start int

	// End is the ending character offset (exclusive).
	End int
}

// EngineStats provides statistics about the search engine.
type EngineStats struct {
	// BM25Stats contains BM25 index statistics.
	BM25Stats *store.IndexStats

	// VectorCount is the number of vectors in the store.
	VectorCount int
}

// EngineConfig configures the search engine.
type EngineConfig struct {
	// DefaultLimit is the default number of results (default: 10).
	DefaultLimit int

	// MaxLimit is the maximum allowed results (default: 100).
	MaxLimit int

	// DefaultWeights are the default BM25/semantic weights.
	DefaultWeights Weights

	// RRFConstant is the RRF fusion constant k (default: 60).
	RRFConstant int

	// SearchTimeout is the maximum search duration (default: 5s).
	SearchTimeout time.Duration
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() EngineConfig {
	return EngineConfig{
		DefaultLimit:   10,
		MaxLimit:       100,
		DefaultWeights: DefaultWeights(),
		RRFConstant:    60,
		SearchTimeout:  5 * time.Second,
	}
}

// QueryType represents the classification category for a search query.
type QueryType string

const (
	// QueryTypeLexical indicates the query needs exact/keyword matching.
	// Used for: error codes, identifiers, quoted phrases, file paths.
	QueryTypeLexical QueryType = "LEXICAL"

	// QueryTypeSemantic indicates the query is natural language seeking meaning.
	// Used for: questions, conceptual queries, explanations.
	QueryTypeSemantic QueryType = "SEMANTIC"

	// QueryTypeMixed indicates the query benefits from both approaches.
	// Used for: multi-word technical queries, default fallback.
	QueryTypeMixed QueryType = "MIXED"
)

// Classifier determines optimal search weights for a query.
// Implementations may use ML models, pattern matching, or hybrid approaches.
type Classifier interface {
	// Classify analyzes a query and returns its type and optimal weights.
	// On error, implementations should return (QueryTypeMixed, DefaultWeights(), err).
	Classify(ctx context.Context, query string) (QueryType, Weights, error)
}

// WeightsForQueryType returns the predefined weights for a query type.
func WeightsForQueryType(qt QueryType) Weights {
	switch qt {
	case QueryTypeLexical:
		return Weights{BM25: 0.85, Semantic: 0.15}
	case QueryTypeSemantic:
		return Weights{BM25: 0.20, Semantic: 0.80}
	default:
		return Weights{BM25: 0.35, Semantic: 0.65}
	}
}

// ExplainData contains detailed search decision information.
// FEAT-UNIX3: Implements Unix Rule of Transparency for debugging.
type ExplainData struct {
	// Query is the original search query.
	Query string

	// BM25ResultCount is the number of results from BM25 search.
	BM25ResultCount int

	// VectorResultCount is the number of results from vector search.
	VectorResultCount int

	// Weights are the BM25/semantic weights used for fusion.
	Weights Weights

	// RRFConstant is the RRF k value used for fusion.
	RRFConstant int

	// BM25Only indicates if vector search was skipped.
	BM25Only bool

	// DimensionMismatch indicates if vector search was disabled due to dimension mismatch.
	DimensionMismatch bool

	// MultiQueryDecomposed indicates if the query was decomposed into sub-queries.
	MultiQueryDecomposed bool

	// SubQueries contains the decomposed sub-queries (if MultiQueryDecomposed is true).
	SubQueries []string
}
