package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Aman-CERP/amanmcp/internal/embed"
	"github.com/Aman-CERP/amanmcp/internal/store"
	"github.com/Aman-CERP/amanmcp/internal/telemetry"
)

// Engine implements hybrid search combining BM25 and semantic search.
type Engine struct {
	bm25       store.BM25Index
	vector     store.VectorStore
	embedder   embed.Embedder
	metadata   store.MetadataStore
	config     EngineConfig
	fusion     *RRFFusion
	classifier Classifier                // Optional query classifier for dynamic weights
	metrics    *telemetry.QueryMetrics   // Optional query telemetry collector
	expander   *QueryExpander            // QI-1 Lite: Code-aware query expansion for BM25
	reranker   Reranker                  // FEAT-RR1: Optional cross-encoder reranker
	multiQuery *MultiQuerySearcher       // FEAT-QI3: Optional multi-query decomposition
	mu         sync.RWMutex
}

// Ensure Engine implements SearchEngine interface.
var _ SearchEngine = (*Engine)(nil)

// ErrNilDependency is returned when a required dependency is nil.
var ErrNilDependency = errors.New("nil dependency")

// ErrDimensionMismatch is returned when query embedding dimension doesn't match index dimension.
// QW-5: Clear error message when embedder changed (e.g., Ollama -> Static768 fallback).
var ErrDimensionMismatch = errors.New("embedding dimension mismatch")

// Qwen3QueryInstruction is the instruction prefix for Qwen3 embedding queries.
// Per Qwen3 documentation: queries require instruction prefix for optimal retrieval.
// Documents are embedded without instruction; queries need task-specific prefix.
// See: https://huggingface.co/Qwen/Qwen3-Embedding-0.6B
const Qwen3QueryInstruction = "Instruct: Given a code search query, retrieve relevant code snippets that answer the query\nQuery:"

// formatQueryForEmbedding formats a query with Qwen3 instruction prefix.
// This improves retrieval by 1-5% according to Qwen3 documentation.
func formatQueryForEmbedding(query string) string {
	return Qwen3QueryInstruction + query
}

// EngineOption configures the search engine.
type EngineOption func(*Engine)

// WithClassifier sets an optional query classifier for dynamic weight selection.
// When set and no explicit weights are provided in SearchOptions, the classifier
// determines optimal BM25/semantic weights based on query characteristics.
func WithClassifier(c Classifier) EngineOption {
	return func(e *Engine) {
		e.classifier = c
	}
}

// WithMetrics sets an optional query metrics collector for telemetry.
// When set, query patterns, latency, and zero-result queries are tracked.
func WithMetrics(m *telemetry.QueryMetrics) EngineOption {
	return func(e *Engine) {
		e.metrics = m
	}
}

// WithQueryExpander sets an optional query expander for BM25 search.
// QI-1 Lite: Expands queries with code-aware synonyms to bridge vocabulary gap.
// When set, BM25 search uses expanded query while vector search uses original.
func WithQueryExpander(exp *QueryExpander) EngineOption {
	return func(e *Engine) {
		e.expander = exp
	}
}

// WithReranker sets an optional cross-encoder reranker for result refinement.
// FEAT-RR1: Reranks fused results to improve relevance for generic queries.
// When set, results are reranked after RRF fusion but before enrichment.
func WithReranker(r Reranker) EngineOption {
	return func(e *Engine) {
		e.reranker = r
	}
}

// WithMultiQuerySearch enables multi-query decomposition for generic queries.
// FEAT-QI3: Decomposes generic queries like "Search function" into multiple
// specific sub-queries, runs them in parallel, and fuses results.
// Documents appearing in multiple sub-query results get boosted (consensus).
func WithMultiQuerySearch(decomposer QueryDecomposer) EngineOption {
	return func(e *Engine) {
		if decomposer == nil {
			return
		}
		// Create a search function that wraps the engine's internal search
		searchFunc := func(ctx context.Context, query string, opts SearchOptions) ([]*FusedResult, error) {
			return e.singleSearch(ctx, query, opts)
		}
		e.multiQuery = NewMultiQuerySearcher(decomposer, searchFunc)
	}
}

// NewEngine creates a new hybrid search engine with the given dependencies.
// Returns an error if any required dependency is nil.
// This is the preferred constructor - use this instead of New.
func NewEngine(
	bm25 store.BM25Index,
	vector store.VectorStore,
	embedder embed.Embedder,
	metadata store.MetadataStore,
	config EngineConfig,
	opts ...EngineOption,
) (*Engine, error) {
	if bm25 == nil {
		return nil, fmt.Errorf("%w: bm25 index is required", ErrNilDependency)
	}
	if vector == nil {
		return nil, fmt.Errorf("%w: vector store is required", ErrNilDependency)
	}
	if embedder == nil {
		return nil, fmt.Errorf("%w: embedder is required", ErrNilDependency)
	}
	if metadata == nil {
		return nil, fmt.Errorf("%w: metadata store is required", ErrNilDependency)
	}
	e := &Engine{
		bm25:     bm25,
		vector:   vector,
		embedder: embedder,
		metadata: metadata,
		config:   config,
		fusion:   NewRRFFusionWithK(config.RRFConstant),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e, nil
}

// New creates a new hybrid search engine with the given dependencies.
// Deprecated: Use NewEngine instead. This function panics on nil dependencies.
func New(
	bm25 store.BM25Index,
	vector store.VectorStore,
	embedder embed.Embedder,
	metadata store.MetadataStore,
	config EngineConfig,
	opts ...EngineOption,
) *Engine {
	e, err := NewEngine(bm25, vector, embedder, metadata, config, opts...)
	if err != nil {
		panic("search.New: " + err.Error())
	}
	return e
}

// Search executes a hybrid search combining BM25 and semantic search.
// It runs both searches in parallel and fuses results using Reciprocal Rank Fusion (RRF).
//
// FEAT-QI3: If multi-query search is enabled and the query benefits from
// decomposition, this method delegates to MultiQuerySearcher which runs
// multiple sub-queries in parallel and fuses results with consensus boosting.
func (e *Engine) Search(ctx context.Context, query string, opts SearchOptions) ([]*SearchResult, error) {
	start := time.Now()

	// Normalize query
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	// FEAT-QI3: Check if multi-query decomposition should be used
	if e.multiQuery != nil && e.multiQuery.decomposer.ShouldDecompose(query) {
		return e.multiQuerySearch(ctx, query, opts, start)
	}

	// Dynamic weight classification if no explicit weights provided
	if opts.Weights == nil && e.classifier != nil {
		_, weights, err := e.classifier.Classify(ctx, query)
		if err == nil {
			opts.Weights = &weights
		}
		// On error, fall through to applyDefaults which uses DefaultWeights
	}

	// Apply defaults
	opts = e.applyDefaults(opts)

	// FEAT-DIM1: Explicit BM25-only mode (user requested via --bm25-only flag)
	if opts.BM25Only {
		slog.Info("bm25_only mode enabled (user requested)")
		bm25Results, bm25Err := e.bm25.Search(ctx, query, opts.Limit*2)
		if bm25Err != nil {
			return nil, fmt.Errorf("BM25 search failed: %w", bm25Err)
		}
		// Fuse with no vector results (BM25-only mode)
		fused := e.fuseResults(bm25Results, nil, &Weights{BM25: 1.0, Semantic: 0.0})
		// FEAT-RR1: Apply reranking after fusion
		reranked := e.rerankResults(ctx, query, fused)
		enriched, err := e.enrichResults(ctx, reranked)
		if err != nil {
			return nil, err
		}
		// FEAT-QI5: Enrich with adjacent context if requested
		e.enrichResultsWithAdjacent(ctx, enriched, opts.AdjacentChunks, 5)
		// FEAT-QI4: Apply test file penalty to prioritize real implementations
		enriched = ApplyTestFilePenalty(enriched)
		// BUG-066: Apply path boost to prioritize internal/ over cmd/
		enriched = ApplyPathBoost(enriched)
		filtered := ApplyFilters(enriched, opts)
		if len(filtered) > opts.Limit {
			filtered = filtered[:opts.Limit]
		}
		// FEAT-UNIX3: Attach explain data for debugging
		e.attachExplainData(filtered, query, opts, len(bm25Results), 0, false, nil)
		e.recordMetrics(query, QueryTypeLexical, len(filtered), time.Since(start))
		return filtered, nil
	}

	// QW-5: Validate embedder dimensions match indexed dimensions
	if err := e.validateDimensions(ctx); err != nil {
		// FEAT-DIM1: Enhanced warning with recovery options
		slog.Warn("dimension mismatch detected, semantic search disabled",
			slog.String("error", err.Error()),
			slog.String("recovery_1", "amanmcp reindex --force"),
			slog.String("recovery_2", "amanmcp search --bm25-only"),
			slog.String("info", "amanmcp index info"))
		// Skip vector search entirely - return BM25 results only
		bm25Results, bm25Err := e.bm25.Search(ctx, query, opts.Limit*2)
		if bm25Err != nil {
			return nil, fmt.Errorf("BM25 search failed (semantic disabled due to dimension mismatch): %w", bm25Err)
		}
		// Fuse with no vector results (BM25-only mode)
		fused := e.fuseResults(bm25Results, nil, opts.Weights)
		// FEAT-RR1: Apply reranking after fusion
		reranked := e.rerankResults(ctx, query, fused)
		enriched, err := e.enrichResults(ctx, reranked)
		if err != nil {
			return nil, err
		}
		// FEAT-QI5: Enrich with adjacent context if requested
		e.enrichResultsWithAdjacent(ctx, enriched, opts.AdjacentChunks, 5)
		// FEAT-QI4: Apply test file penalty to prioritize real implementations
		enriched = ApplyTestFilePenalty(enriched)
		// BUG-066: Apply path boost to prioritize internal/ over cmd/
		enriched = ApplyPathBoost(enriched)
		filtered := ApplyFilters(enriched, opts)
		if len(filtered) > opts.Limit {
			filtered = filtered[:opts.Limit]
		}
		// FEAT-UNIX3: Attach explain data with dimension mismatch flag
		e.attachExplainData(filtered, query, opts, len(bm25Results), 0, true, nil)
		e.recordMetrics(query, QueryTypeLexical, len(filtered), time.Since(start))
		return filtered, nil
	}

	// Run searches in parallel
	bm25Results, vecResults, searchErr := e.parallelSearch(ctx, query, opts.Limit*2)

	// Handle graceful degradation
	if searchErr != nil {
		// Check if both failed
		if bm25Results == nil && vecResults == nil {
			return nil, searchErr
		}
		// Continue with partial results
	}

	// Fuse results
	fused := e.fuseResults(bm25Results, vecResults, opts.Weights)

	// FEAT-RR1: Apply cross-encoder reranking after fusion
	reranked := e.rerankResults(ctx, query, fused)

	// Enrich results with full chunk data
	enriched, err := e.enrichResults(ctx, reranked)
	if err != nil {
		return nil, err
	}

	// FEAT-QI5: Enrich with adjacent context if requested
	e.enrichResultsWithAdjacent(ctx, enriched, opts.AdjacentChunks, 5)

	// FEAT-QI4: Apply test file penalty to prioritize real implementations
	enriched = ApplyTestFilePenalty(enriched)
	// BUG-066: Apply path boost to prioritize internal/ over cmd/
	enriched = ApplyPathBoost(enriched)

	// Apply filters after enrichment (need chunk metadata)
	filtered := ApplyFilters(enriched, opts)

	// Apply limit
	if len(filtered) > opts.Limit {
		filtered = filtered[:opts.Limit]
	}

	// FEAT-UNIX3: Attach explain data for debugging
	e.attachExplainData(filtered, query, opts, len(bm25Results), len(vecResults), false, nil)

	// Record telemetry
	e.recordMetrics(query, e.classifyQueryType(ctx, query, opts), len(filtered), time.Since(start))

	return filtered, nil
}

// attachExplainData populates ExplainData on the first result when opts.Explain is true.
// FEAT-UNIX3: Implements Unix Rule of Transparency for search debugging.
func (e *Engine) attachExplainData(results []*SearchResult, query string, opts SearchOptions, bm25Count, vecCount int, dimMismatch bool, subQueries []string) {
	if !opts.Explain || len(results) == 0 {
		return
	}

	results[0].Explain = &ExplainData{
		Query:                query,
		BM25ResultCount:      bm25Count,
		VectorResultCount:    vecCount,
		Weights:              *opts.Weights,
		RRFConstant:          e.config.RRFConstant,
		BM25Only:             opts.BM25Only,
		DimensionMismatch:    dimMismatch,
		MultiQueryDecomposed: len(subQueries) > 0,
		SubQueries:           subQueries,
	}
}

// recordMetrics records query telemetry if metrics collector is configured.
func (e *Engine) recordMetrics(query string, queryType QueryType, resultCount int, latency time.Duration) {
	if e.metrics == nil {
		return
	}
	e.metrics.Record(telemetry.QueryEvent{
		Query:       query,
		QueryType:   telemetry.QueryType(queryType),
		ResultCount: resultCount,
		Latency:     latency,
		Timestamp:   time.Now(),
	})
}

// classifyQueryType determines the query type based on classifier or weights.
func (e *Engine) classifyQueryType(ctx context.Context, query string, opts SearchOptions) QueryType {
	// If weights are explicitly set, determine type from them
	if opts.Weights != nil {
		if opts.Weights.BM25 > 0.6 {
			return QueryTypeLexical
		}
		if opts.Weights.Semantic > 0.6 {
			return QueryTypeSemantic
		}
		return QueryTypeMixed
	}

	// If classifier is available, use it
	if e.classifier != nil {
		qt, _, err := e.classifier.Classify(ctx, query)
		if err == nil {
			return qt
		}
	}

	// Default to mixed
	return QueryTypeMixed
}

// Index adds chunks to both BM25 and vector indices.
func (e *Engine) Index(ctx context.Context, chunks []*store.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Prepare documents for BM25
	docs := make([]*store.Document, len(chunks))
	for i, c := range chunks {
		docs[i] = &store.Document{
			ID:      c.ID,
			Content: c.Content,
		}
	}

	// Generate embeddings
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}

	embeddings, err := e.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return fmt.Errorf("generate embeddings: %w", err)
	}

	// Index in BM25
	if err := e.bm25.Index(ctx, docs); err != nil {
		return fmt.Errorf("index in BM25: %w", err)
	}

	// Index in vector store
	ids := make([]string, len(chunks))
	for i, c := range chunks {
		ids[i] = c.ID
	}

	if err := e.vector.Add(ctx, ids, embeddings); err != nil {
		return fmt.Errorf("add vectors: %w", err)
	}

	// Save to metadata store
	if err := e.metadata.SaveChunks(ctx, chunks); err != nil {
		return fmt.Errorf("save chunks metadata: %w", err)
	}

	// Persist embeddings in SQLite for future compaction (BUG-024 fix)
	if err := e.metadata.SaveChunkEmbeddings(ctx, ids, embeddings, e.embedder.ModelName()); err != nil {
		// Log warning but don't fail - embeddings can be regenerated
		slog.Warn("failed to persist embeddings, compaction will require re-embedding",
			slog.String("error", err.Error()),
			slog.Int("count", len(ids)))
	}

	// QW-5: Store embedding dimension and model for mismatch detection
	if err := e.storeIndexEmbeddingInfo(ctx); err != nil {
		slog.Warn("failed to store index embedding info",
			slog.String("error", err.Error()))
	}

	return nil
}

// storeIndexEmbeddingInfo saves the current embedder's dimension and model to metadata.
// QW-5: This enables detection of dimension mismatch when embedder changes.
func (e *Engine) storeIndexEmbeddingInfo(ctx context.Context) error {
	dim := fmt.Sprintf("%d", e.embedder.Dimensions())
	model := e.embedder.ModelName()

	if err := e.metadata.SetState(ctx, store.StateKeyIndexDimension, dim); err != nil {
		return fmt.Errorf("failed to store index dimension: %w", err)
	}
	if err := e.metadata.SetState(ctx, store.StateKeyIndexModel, model); err != nil {
		return fmt.Errorf("failed to store index model: %w", err)
	}
	return nil
}

// validateDimensions checks if current embedder dimension matches indexed dimension.
// QW-5: Returns ErrDimensionMismatch if embedder changed (e.g., Ollama → Static768 fallback).
// Returns nil if no index dimension stored (first-time indexing) or dimensions match.
func (e *Engine) validateDimensions(ctx context.Context) error {
	storedDim, err := e.metadata.GetState(ctx, store.StateKeyIndexDimension)
	if err != nil || storedDim == "" {
		// No stored dimension - first time or legacy index, allow search
		return nil
	}

	var indexDim int
	if _, err := fmt.Sscanf(storedDim, "%d", &indexDim); err != nil {
		// Invalid stored dimension, allow search with warning
		slog.Warn("invalid stored index dimension", slog.String("value", storedDim))
		return nil
	}

	currentDim := e.embedder.Dimensions()
	if indexDim != currentDim {
		storedModel, _ := e.metadata.GetState(ctx, store.StateKeyIndexModel)
		currentModel := e.embedder.ModelName()
		return fmt.Errorf("%w: index has %d dimensions (%s), but current embedder has %d dimensions (%s). Run 'amanmcp reindex --force' to rebuild with current embedder",
			ErrDimensionMismatch, indexDim, storedModel, currentDim, currentModel)
	}

	return nil
}

// Delete removes chunks from all indices and metadata.
func (e *Engine) Delete(ctx context.Context, chunkIDs []string) error {
	if len(chunkIDs) == 0 {
		return nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// BUG-023 fix: Use best-effort delete pattern.
	// Metadata is the source of truth - orphans in BM25/Vector are
	// harmless (filtered during search by filterValidResults).

	var hasOrphans bool

	// Delete from BM25 (best effort - continue on error)
	if err := e.bm25.Delete(ctx, chunkIDs); err != nil {
		slog.Warn("BM25 delete failed, orphans will remain until compaction",
			slog.String("error", err.Error()),
			slog.Int("count", len(chunkIDs)))
		hasOrphans = true
	}

	// Delete from vector store (best effort - continue on error)
	if err := e.vector.Delete(ctx, chunkIDs); err != nil {
		slog.Warn("vector delete failed, orphans will remain until compaction",
			slog.String("error", err.Error()),
			slog.Int("count", len(chunkIDs)))
		hasOrphans = true
	}

	// Delete from metadata store (MUST succeed - source of truth)
	if err := e.metadata.DeleteChunks(ctx, chunkIDs); err != nil {
		return fmt.Errorf("delete chunks metadata: %w", err)
	}

	if hasOrphans {
		slog.Debug("delete completed with orphan remnants",
			slog.Int("chunks", len(chunkIDs)))
	}

	return nil
}

// Stats returns engine statistics.
func (e *Engine) Stats() *EngineStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return &EngineStats{
		BM25Stats:   e.bm25.Stats(),
		VectorCount: e.vector.Count(),
	}
}

// Close releases all resources.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var errs []error

	if err := e.bm25.Close(); err != nil {
		errs = append(errs, err)
	}

	if err := e.vector.Close(); err != nil {
		errs = append(errs, err)
	}

	if err := e.metadata.Close(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// applyDefaults fills in default values for search options.
func (e *Engine) applyDefaults(opts SearchOptions) SearchOptions {
	if opts.Limit <= 0 {
		opts.Limit = e.config.DefaultLimit
	}
	if opts.Limit > e.config.MaxLimit {
		opts.Limit = e.config.MaxLimit
	}

	if opts.Filter == "" {
		opts.Filter = "all"
	}

	if opts.Weights == nil {
		w := e.config.DefaultWeights
		opts.Weights = &w
	}

	return opts
}

// parallelSearch executes BM25 and vector searches concurrently.
// Returns partial results on single-search failure (graceful degradation).
//
// QI-1: BM25 uses expanded query (with code synonyms) while vector search
// uses original query. Embedding models handle semantic similarity natively,
// so expansion can hurt precision by adding noise. BM25 benefits from expansion
// because it matches exact keywords.
func (e *Engine) parallelSearch(ctx context.Context, query string, limit int) (
	bm25Results []*store.BM25Result,
	vecResults []*store.VectorResult,
	err error,
) {
	g, gctx := errgroup.WithContext(ctx)

	var bm25Err, vecErr error

	// QI-1: Expand query for BM25 search to bridge vocabulary gap
	// BM25 matches exact keywords, so synonyms help (e.g., "function" → "func method")
	// Vector search uses original query - embedding model handles semantic similarity
	bm25Query := query
	if e.expander != nil {
		bm25Query = e.expander.Expand(query)
		if bm25Query != query {
			slog.Debug("query expanded for BM25",
				slog.String("original", query),
				slog.String("expanded", bm25Query))
		}
	}

	// BM25 search (with expanded query)
	g.Go(func() error {
		var searchErr error
		bm25Results, searchErr = e.bm25.Search(gctx, bm25Query, limit)
		if searchErr != nil {
			bm25Err = searchErr
			// Don't return error - allow vector search to continue
		}
		return nil
	})

	// Vector search with Qwen3 query instruction format
	// Per Qwen3 docs: queries need instruction prefix, documents don't
	var queryEmbedding []float32 // Captured for telemetry (SPIKE-004)
	g.Go(func() error {
		formattedQuery := formatQueryForEmbedding(query)
		embedding, embedErr := e.embedder.Embed(gctx, formattedQuery)
		if embedErr != nil {
			vecErr = embedErr
			return nil // Don't fail the group
		}
		queryEmbedding = embedding // Capture for semantic similarity tracking

		var searchErr error
		vecResults, searchErr = e.vector.Search(gctx, embedding, limit)
		if searchErr != nil {
			vecErr = searchErr
		}
		return nil
	})

	// Wait for both to complete
	if waitErr := g.Wait(); waitErr != nil {
		// Context was cancelled
		return nil, nil, waitErr
	}

	// Record embedding for semantic similarity sampling (SPIKE-004)
	if e.metrics != nil && len(queryEmbedding) > 0 {
		e.metrics.RecordQueryEmbedding(queryEmbedding)
	}

	// Check if both failed
	if bm25Err != nil && vecErr != nil {
		return nil, nil, errors.Join(bm25Err, vecErr)
	}

	// Return any errors for logging, but continue with partial results
	if bm25Err != nil {
		err = bm25Err
	} else if vecErr != nil {
		err = vecErr
	}

	return bm25Results, vecResults, err
}

// fusedResult holds intermediate fusion state.
type fusedResult struct {
	chunkID      string
	rrfScore     float64 // Normalized RRF score (0-1)
	bm25Score    float64
	vecScore     float64
	bm25Rank     int
	vecRank      int
	inBothLists  bool
	matchedTerms []string
}

// fuseResults combines BM25 and vector results using Reciprocal Rank Fusion (RRF).
func (e *Engine) fuseResults(
	bm25Results []*store.BM25Result,
	vecResults []*store.VectorResult,
	weights *Weights,
) []*fusedResult {
	// Use RRF fusion
	rrfResults := e.fusion.Fuse(bm25Results, vecResults, *weights)

	// Convert to internal fusedResult type
	results := make([]*fusedResult, len(rrfResults))
	for i, r := range rrfResults {
		results[i] = &fusedResult{
			chunkID:      r.ChunkID,
			rrfScore:     r.RRFScore,
			bm25Score:    r.BM25Score,
			vecScore:     r.VecScore,
			bm25Rank:     r.BM25Rank,
			vecRank:      r.VecRank,
			inBothLists:  r.InBothLists,
			matchedTerms: r.MatchedTerms,
		}
	}

	return results
}

// enrichResults fetches full chunk data using batch retrieval for performance.
// Uses GetChunks to fetch all chunks in a single query instead of N individual queries.
func (e *Engine) enrichResults(ctx context.Context, fused []*fusedResult) ([]*SearchResult, error) {
	if len(fused) == 0 {
		return nil, nil
	}

	// Collect all chunk IDs for batch retrieval
	ids := make([]string, len(fused))
	fusedByID := make(map[string]*fusedResult, len(fused))
	for i, f := range fused {
		ids[i] = f.chunkID
		fusedByID[f.chunkID] = f
	}

	// Batch fetch all chunks in a single query
	chunks, err := e.metadata.GetChunks(ctx, ids)
	if err != nil {
		return nil, err
	}

	// Build results maintaining order from fused results
	results := make([]*SearchResult, 0, len(chunks))
	for _, chunk := range chunks {
		f, ok := fusedByID[chunk.ID]
		if !ok {
			continue // Should not happen, but defensive
		}

		result := &SearchResult{
			Chunk:        chunk,
			Score:        f.rrfScore, // Use pre-calculated RRF score (already normalized 0-1)
			BM25Score:    f.bm25Score,
			VecScore:     f.vecScore,
			BM25Rank:     f.bm25Rank, // FEAT-UNIX3: Expose for explain mode
			VecRank:      f.vecRank,  // FEAT-UNIX3: Expose for explain mode
			InBothLists:  f.inBothLists,
			Highlights:   e.calculateHighlights(chunk.Content, f.matchedTerms),
			MatchedTerms: f.matchedTerms, // UX-1: Expose matched terms for context display
		}

		results = append(results, result)
	}

	return results, nil
}

// enrichResultsWithAdjacent fetches adjacent chunks for context continuity.
// FEAT-QI5: For each top-N result, retrieves chunks before/after from the same file.
// This improves "How does X work" queries by providing surrounding context.
func (e *Engine) enrichResultsWithAdjacent(ctx context.Context, results []*SearchResult, adjacentCount int, topN int) {
	if adjacentCount <= 0 || len(results) == 0 {
		return
	}

	// Limit to topN results for performance (default: 5)
	enrichCount := len(results)
	if topN > 0 && enrichCount > topN {
		enrichCount = topN
	}

	// Group results by file to batch fetch chunks
	fileIDToResults := make(map[string][]*SearchResult)
	for i := 0; i < enrichCount; i++ {
		result := results[i]
		if result.Chunk == nil || result.Chunk.FileID == "" {
			continue
		}
		fileIDToResults[result.Chunk.FileID] = append(fileIDToResults[result.Chunk.FileID], result)
	}

	// For each file, fetch all chunks and find adjacent ones
	for fileID, fileResults := range fileIDToResults {
		// Fetch all chunks for this file
		allChunks, err := e.metadata.GetChunksByFile(ctx, fileID)
		if err != nil {
			// Graceful degradation: skip this file but continue with others
			slog.Debug("failed to fetch chunks for adjacent context",
				slog.String("file_id", fileID),
				slog.String("error", err.Error()))
			continue
		}

		// For each result in this file, find adjacent chunks
		for _, result := range fileResults {
			targetChunk := result.Chunk

			// Collect chunks before and after the target
			var before, after []*store.Chunk
			for _, c := range allChunks {
				if c.ID == targetChunk.ID {
					continue // Skip self
				}

				// Check if chunk is before (ends before target starts)
				if c.EndLine < targetChunk.StartLine {
					before = append(before, c)
				}
				// Check if chunk is after (starts after target ends)
				if c.StartLine > targetChunk.EndLine {
					after = append(after, c)
				}
			}

			// Sort by proximity (always sort for consistent ordering)
			// Before: sort by highest EndLine (closest to target first)
			sort.Slice(before, func(i, j int) bool {
				return before[i].EndLine > before[j].EndLine
			})
			// Limit to adjacentCount
			if len(before) > adjacentCount {
				before = before[:adjacentCount]
			}

			// After: sort by lowest StartLine (closest to target first)
			sort.Slice(after, func(i, j int) bool {
				return after[i].StartLine < after[j].StartLine
			})
			// Limit to adjacentCount
			if len(after) > adjacentCount {
				after = after[:adjacentCount]
			}

			// Assign to result
			result.AdjacentContext.Before = before
			result.AdjacentContext.After = after
		}
	}
}

// rerankResults applies cross-encoder reranking to improve result relevance.
// FEAT-RR1: Closes the 25% validation gap by reranking generic queries.
// Returns original results unchanged if reranker is nil or unavailable.
// DEBT-024: Instrumented with detailed timing for latency investigation.
func (e *Engine) rerankResults(ctx context.Context, query string, fused []*fusedResult) []*fusedResult {
	overallStart := time.Now()

	// Skip if no reranker configured
	if e.reranker == nil {
		return fused
	}

	// Skip if too few results to rerank
	if len(fused) < 2 {
		return fused
	}

	// DEBT-024: Measure availability check
	availStart := time.Now()
	if !e.reranker.Available(ctx) {
		slog.Debug("reranker unavailable, skipping reranking",
			slog.Duration("avail_check", time.Since(availStart)))
		return fused
	}
	availDuration := time.Since(availStart)

	// Build document list from fused results
	// We need to fetch chunk content for reranking
	chunkIDs := make([]string, len(fused))
	for i, f := range fused {
		chunkIDs[i] = f.chunkID
	}

	// DEBT-024: Measure chunk fetch time (key bottleneck candidate)
	fetchStart := time.Now()
	chunks, err := e.metadata.GetChunks(ctx, chunkIDs)
	fetchDuration := time.Since(fetchStart)
	if err != nil {
		slog.Warn("failed to fetch chunks for reranking, skipping",
			slog.String("error", err.Error()),
			slog.Duration("fetch_attempt", fetchDuration))
		return fused
	}

	// DEBT-024: Measure document building
	buildStart := time.Now()

	// Build ID to content map
	contentByID := make(map[string]string, len(chunks))
	var totalContentBytes int
	for _, chunk := range chunks {
		contentByID[chunk.ID] = chunk.Content
		totalContentBytes += len(chunk.Content)
	}

	// Prepare documents in fused order
	documents := make([]string, 0, len(fused))
	validFused := make([]*fusedResult, 0, len(fused))
	for _, f := range fused {
		content, ok := contentByID[f.chunkID]
		if ok && content != "" {
			documents = append(documents, content)
			validFused = append(validFused, f)
		}
	}
	buildDuration := time.Since(buildStart)

	if len(documents) == 0 {
		return fused
	}

	// DEBT-024: Measure reranker call
	rerankStart := time.Now()
	reranked, err := e.reranker.Rerank(ctx, query, documents, 0) // 0 = return all
	rerankDuration := time.Since(rerankStart)
	if err != nil {
		slog.Warn("reranking failed, using original order",
			slog.String("error", err.Error()),
			slog.Duration("rerank_attempt", rerankDuration))
		return fused
	}

	// DEBT-024: Measure reorder time
	reorderStart := time.Now()

	// Reorder fused results based on reranker scores
	// The reranker returns results sorted by score descending
	results := make([]*fusedResult, len(reranked))
	for i, rr := range reranked {
		if rr.Index < 0 || rr.Index >= len(validFused) {
			slog.Warn("invalid reranker index, skipping",
				slog.Int("index", rr.Index),
				slog.Int("valid_count", len(validFused)))
			continue
		}
		f := validFused[rr.Index]
		// Update RRF score with reranker score for final ranking
		// Keep original scores for debugging
		f.rrfScore = rr.Score
		results[i] = f
	}

	// Filter out nil entries (from invalid indices)
	finalResults := make([]*fusedResult, 0, len(results))
	for _, r := range results {
		if r != nil {
			finalResults = append(finalResults, r)
		}
	}
	reorderDuration := time.Since(reorderStart)

	totalDuration := time.Since(overallStart)

	// DEBT-024: Enhanced telemetry for latency investigation
	slog.Debug("rerank_results_timing",
		slog.String("query", truncateQuery(query, 50)),
		slog.Int("input_count", len(fused)),
		slog.Int("output_count", len(finalResults)),
		slog.Int("total_content_bytes", totalContentBytes),
		slog.Duration("avail_check", availDuration),
		slog.Duration("chunk_fetch", fetchDuration),
		slog.Duration("build_docs", buildDuration),
		slog.Duration("rerank_call", rerankDuration),
		slog.Duration("reorder", reorderDuration),
		slog.Duration("total", totalDuration))

	return finalResults
}

// calculateHighlights finds text ranges for matched terms.
// Optimized: pre-allocates capacity, limits matches per term.
func (e *Engine) calculateHighlights(content string, matchedTerms []string) []Range {
	// Early return for empty inputs - return empty slice, not nil (DEBT-012)
	if len(matchedTerms) == 0 || len(content) == 0 {
		return []Range{}
	}

	// Pre-allocate with estimated capacity (avg 3 matches per term)
	const maxMatchesPerTerm = 10
	highlights := make([]Range, 0, len(matchedTerms)*3)

	lowerContent := strings.ToLower(content)

	for _, term := range matchedTerms {
		if len(term) == 0 {
			continue
		}

		lowerTerm := strings.ToLower(term)
		start := 0
		matchCount := 0

		for matchCount < maxMatchesPerTerm {
			idx := strings.Index(lowerContent[start:], lowerTerm)
			if idx == -1 {
				break
			}

			absStart := start + idx
			highlights = append(highlights, Range{
				Start: absStart,
				End:   absStart + len(term),
			})

			start = absStart + len(term)
			matchCount++
		}
	}

	// Only sort if we have multiple highlights
	if len(highlights) > 1 {
		sort.Slice(highlights, func(i, j int) bool {
			return highlights[i].Start < highlights[j].Start
		})
	}

	return highlights
}

// multiQuerySearch handles FEAT-QI3 multi-query decomposition search.
// It decomposes the query, runs sub-queries in parallel, and fuses results.
func (e *Engine) multiQuerySearch(ctx context.Context, query string, opts SearchOptions, start time.Time) ([]*SearchResult, error) {
	// Apply defaults for consistent options across sub-queries
	opts = e.applyDefaults(opts)

	// FEAT-UNIX3: Get sub-queries for explain output
	var subQueryStrings []string
	if opts.Explain {
		subQueries := e.multiQuery.decomposer.Decompose(query)
		subQueryStrings = make([]string, len(subQueries))
		for i, sq := range subQueries {
			subQueryStrings[i] = sq.Query
		}
	}

	// Run multi-query search
	multiFused, err := e.multiQuery.Search(ctx, query, opts)
	if err != nil {
		return nil, err
	}

	// Convert MultiFusedResult to fusedResult for enrichment
	fused := make([]*fusedResult, len(multiFused))
	for i, mf := range multiFused {
		fused[i] = &fusedResult{
			chunkID:      mf.ChunkID,
			rrfScore:     mf.RRFScore,
			bm25Score:    mf.BM25Score,
			vecScore:     mf.VecScore,
			bm25Rank:     mf.BM25Rank,
			vecRank:      mf.VecRank,
			inBothLists:  mf.InBothLists,
			matchedTerms: mf.MatchedTerms,
		}
	}

	// Enrich results with full chunk data
	enriched, err := e.enrichResults(ctx, fused)
	if err != nil {
		return nil, err
	}

	// FEAT-QI5: Enrich with adjacent context if requested
	e.enrichResultsWithAdjacent(ctx, enriched, opts.AdjacentChunks, 5)

	// FEAT-QI4: Apply test file penalty to prioritize real implementations
	enriched = ApplyTestFilePenalty(enriched)
	// BUG-066: Apply path boost to prioritize internal/ over cmd/
	enriched = ApplyPathBoost(enriched)

	// Apply filters after enrichment (need chunk metadata)
	filtered := ApplyFilters(enriched, opts)

	// FEAT-UNIX3: Attach explain data for multi-query search
	// Note: BM25/vector counts are aggregated across sub-queries, so we use result count
	e.attachExplainData(filtered, query, opts, len(filtered), len(filtered), false, subQueryStrings)

	// Record telemetry
	e.recordMetrics(query, QueryTypeMixed, len(filtered), time.Since(start))

	slog.Debug("multi_query_search_complete",
		slog.String("query", query),
		slog.Int("results", len(filtered)),
		slog.Duration("duration", time.Since(start)))

	return filtered, nil
}

// singleSearch executes a single hybrid search without multi-query decomposition.
// This is used by MultiQuerySearcher for each sub-query.
// Returns FusedResult slice (pre-enrichment) for efficient multi-query fusion.
func (e *Engine) singleSearch(ctx context.Context, query string, opts SearchOptions) ([]*FusedResult, error) {
	// Normalize query
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	// Dynamic weight classification if no explicit weights provided
	if opts.Weights == nil && e.classifier != nil {
		_, weights, err := e.classifier.Classify(ctx, query)
		if err == nil {
			opts.Weights = &weights
		}
	}

	// Apply defaults
	opts = e.applyDefaults(opts)

	// Handle BM25-only mode
	if opts.BM25Only {
		bm25Results, err := e.bm25.Search(ctx, query, opts.Limit*2)
		if err != nil {
			return nil, fmt.Errorf("BM25 search failed: %w", err)
		}
		fused := e.fuseResults(bm25Results, nil, &Weights{BM25: 1.0, Semantic: 0.0})
		return e.convertToFusedResult(fused), nil
	}

	// Validate dimensions
	if err := e.validateDimensions(ctx); err != nil {
		// Fall back to BM25-only
		bm25Results, bm25Err := e.bm25.Search(ctx, query, opts.Limit*2)
		if bm25Err != nil {
			return nil, fmt.Errorf("BM25 search failed: %w", bm25Err)
		}
		fused := e.fuseResults(bm25Results, nil, opts.Weights)
		return e.convertToFusedResult(fused), nil
	}

	// Run parallel search
	bm25Results, vecResults, _ := e.parallelSearch(ctx, query, opts.Limit*2)

	// Fuse results
	fused := e.fuseResults(bm25Results, vecResults, opts.Weights)

	// Apply filtering if needed (for multi-query sub-query hints)
	if opts.Filter != "" && opts.Filter != "all" {
		// Enrich to get content type
		enriched, err := e.enrichResults(ctx, fused)
		if err != nil {
			return e.convertToFusedResult(fused), nil // Fall back to unfiltered
		}
		// Apply filter
		filtered := ApplyFilters(enriched, opts)
		// Convert back to FusedResult
		fusedFiltered := make([]*FusedResult, len(filtered))
		for i, r := range filtered {
			fusedFiltered[i] = &FusedResult{
				ChunkID:      r.Chunk.ID,
				RRFScore:     r.Score,
				BM25Score:    r.BM25Score,
				BM25Rank:     0, // Not tracked after enrichment
				VecScore:     r.VecScore,
				VecRank:      0, // Not tracked after enrichment
				InBothLists:  r.InBothLists,
				MatchedTerms: r.MatchedTerms,
			}
		}
		return fusedFiltered, nil
	}

	return e.convertToFusedResult(fused), nil
}

// convertToFusedResult converts internal fusedResult to public FusedResult.
func (e *Engine) convertToFusedResult(internal []*fusedResult) []*FusedResult {
	results := make([]*FusedResult, len(internal))
	for i, f := range internal {
		results[i] = &FusedResult{
			ChunkID:      f.chunkID,
			RRFScore:     f.rrfScore,
			BM25Score:    f.bm25Score,
			BM25Rank:     f.bm25Rank,
			VecScore:     f.vecScore,
			VecRank:      f.vecRank,
			InBothLists:  f.inBothLists,
			MatchedTerms: f.matchedTerms,
		}
	}
	return results
}
