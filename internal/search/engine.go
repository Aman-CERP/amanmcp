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
	classifier Classifier              // Optional query classifier for dynamic weights
	metrics    *telemetry.QueryMetrics // Optional query telemetry collector
	expander   *QueryExpander          // QI-1 Lite: Code-aware query expansion for BM25
	reranker   Reranker                // FEAT-RR1: Optional cross-encoder reranker
	multiQuery *MultiQuerySearcher     // FEAT-QI3: Optional multi-query decomposition
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
	if err := config.RerankerPolicy.Validate(); err != nil {
		return nil, fmt.Errorf("invalid reranker policy: %w", err)
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
		queryType, weights, confidence, confidenceState, err := e.classifyForSearch(ctx, query)
		if err == nil {
			opts.Weights = &weights
			recordQueryClassification(opts, QueryClassification{
				Type:            queryType,
				Confidence:      confidence,
				ConfidenceState: confidenceState,
			})
		}
		// On error, fall through to applyDefaults which uses DefaultWeights
	}

	// Apply defaults
	opts = e.applyDefaults(opts)

	// FEAT-DIM1: Explicit BM25-only mode (user requested via --bm25-only flag)
	if opts.BM25Only {
		slog.Info("bm25_only mode enabled (user requested)")
		candidateLimit := candidateLimitForOptions(query, opts)
		bm25Results, bm25Err := e.bm25.Search(ctx, query, candidateLimit)
		if bm25Err != nil {
			return nil, fmt.Errorf("BM25 search failed: %w", bm25Err)
		}
		// Fuse with no vector results (BM25-only mode)
		fused := e.fuseResults(bm25Results, nil, &Weights{BM25: 1.0, Semantic: 0.0})
		// FEAT-RR1: Apply reranking after fusion
		reranked := e.rerankResults(ctx, query, fused, opts)
		enriched, err := e.enrichResults(ctx, reranked)
		if err != nil {
			return nil, err
		}
		enriched, err = e.addExactSymbolCandidates(ctx, enriched, query, opts)
		if err != nil {
			return nil, err
		}
		enriched, err = e.addADRReferenceCandidates(ctx, enriched, query, opts)
		if err != nil {
			return nil, err
		}
		enriched, err = e.addPDFContentCandidates(ctx, enriched, query, opts)
		if err != nil {
			return nil, err
		}
		// FEAT-QI5: Enrich with adjacent context if requested
		e.enrichResultsWithAdjacent(ctx, enriched, opts.AdjacentChunks, 5)
		// TASK-SYN42: Exact lexical lookups should rank definitions above references.
		enriched = ApplyExactMatchBoost(enriched, query)
		enriched = ApplyPDFContentBoost(enriched, query)
		// FEAT-QI4: Apply test file penalty to prioritize real implementations
		enriched = ApplyTestFilePenalty(enriched)
		// BUG-066: Apply path boost to prioritize internal/ over cmd/
		enriched = ApplyPathBoost(enriched)
		// F39: Apply authority/freshness boost after path boosts.
		enriched = ApplyAuthorityBoost(enriched)
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
		candidateLimit := candidateLimitForOptions(query, opts)
		bm25Results, bm25Err := e.bm25.Search(ctx, query, candidateLimit)
		if bm25Err != nil {
			return nil, fmt.Errorf("BM25 search failed (semantic disabled due to dimension mismatch): %w", bm25Err)
		}
		// Fuse with no vector results (BM25-only mode)
		fused := e.fuseResults(bm25Results, nil, opts.Weights)
		// FEAT-RR1: Apply reranking after fusion
		reranked := e.rerankResults(ctx, query, fused, opts)
		enriched, err := e.enrichResults(ctx, reranked)
		if err != nil {
			return nil, err
		}
		enriched, err = e.addExactSymbolCandidates(ctx, enriched, query, opts)
		if err != nil {
			return nil, err
		}
		enriched, err = e.addADRReferenceCandidates(ctx, enriched, query, opts)
		if err != nil {
			return nil, err
		}
		enriched, err = e.addPDFContentCandidates(ctx, enriched, query, opts)
		if err != nil {
			return nil, err
		}
		// FEAT-QI5: Enrich with adjacent context if requested
		e.enrichResultsWithAdjacent(ctx, enriched, opts.AdjacentChunks, 5)
		// TASK-SYN42: Exact lexical lookups should rank definitions above references.
		enriched = ApplyExactMatchBoost(enriched, query)
		enriched = ApplyPDFContentBoost(enriched, query)
		// FEAT-QI4: Apply test file penalty to prioritize real implementations
		enriched = ApplyTestFilePenalty(enriched)
		// BUG-066: Apply path boost to prioritize internal/ over cmd/
		enriched = ApplyPathBoost(enriched)
		// F39: Apply authority/freshness boost after path boosts.
		enriched = ApplyAuthorityBoost(enriched)
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
	candidateLimit := candidateLimitForOptions(query, opts)
	bm25Results, vecResults, searchErr := e.parallelSearch(ctx, query, candidateLimit)

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
	reranked := e.rerankResults(ctx, query, fused, opts)

	// Enrich results with full chunk data
	enriched, err := e.enrichResults(ctx, reranked)
	if err != nil {
		return nil, err
	}

	enriched, err = e.addExactSymbolCandidates(ctx, enriched, query, opts)
	if err != nil {
		return nil, err
	}
	enriched, err = e.addADRReferenceCandidates(ctx, enriched, query, opts)
	if err != nil {
		return nil, err
	}
	enriched, err = e.addPDFContentCandidates(ctx, enriched, query, opts)
	if err != nil {
		return nil, err
	}

	// FEAT-QI5: Enrich with adjacent context if requested
	e.enrichResultsWithAdjacent(ctx, enriched, opts.AdjacentChunks, 5)

	// TASK-SYN42: Exact lexical lookups should rank definitions above references.
	enriched = ApplyExactMatchBoost(enriched, query)
	enriched = ApplyPDFContentBoost(enriched, query)
	// FEAT-QI4: Apply test file penalty to prioritize real implementations
	enriched = ApplyTestFilePenalty(enriched)
	// BUG-066: Apply path boost to prioritize internal/ over cmd/
	enriched = ApplyPathBoost(enriched)
	// F39: Apply authority/freshness boost after path boosts.
	enriched = ApplyAuthorityBoost(enriched)

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

func candidateLimitForQuery(query string, resultLimit int) int {
	return candidateLimitForOptions(query, SearchOptions{Limit: resultLimit})
}

func candidateLimitForOptions(query string, opts SearchOptions) int {
	resultLimit := opts.Limit
	if resultLimit <= 0 {
		resultLimit = DefaultConfig().DefaultLimit
	}
	baseLimit := resultLimit * 2
	if !shouldBroadenCandidatePool(query, opts) {
		return baseLimit
	}
	if shouldUseBaseLimitForExpandedContentFilter(query, opts, resultLimit) {
		return baseLimit
	}

	exactLimit := resultLimit * 10
	if exactLimit < 50 {
		return 50
	}
	return exactLimit
}

func shouldUseBaseLimitForExpandedContentFilter(query string, opts SearchOptions, resultLimit int) bool {
	if resultLimit < 50 {
		return false
	}
	if !hasPostRetrievalContentFilter(opts) {
		return false
	}
	if shouldPreserveExactLexicalQuery(query) || adrRefPattern.MatchString(query) {
		return false
	}
	return true
}

func shouldBroadenCandidatePool(query string, opts SearchOptions) bool {
	switch opts.Mode {
	case SearchModeDecisions, SearchModeDecisionHistory:
		return true
	}

	if shouldPreserveExactLexicalQuery(query) {
		return true
	}

	// Content-type filters are applied after enrichment, so unfiltered retrieval
	// can spend the whole candidate window on another content class before the
	// requested class is filtered in.
	return hasPostRetrievalContentFilter(opts)
}

func hasPostRetrievalContentFilter(opts SearchOptions) bool {
	switch strings.ToLower(strings.TrimSpace(opts.Filter)) {
	case "code", "docs":
		return true
	default:
		return false
	}
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
			Content: store.BM25DocumentContent(c.FilePath, c.Content),
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
	if len(opts.ProfileRules.Profiles) == 0 {
		opts.ProfileRules = e.config.ProfileRules
	}
	if len(opts.ProfileRules.Profiles) == 0 {
		opts.ProfileRules = DefaultProfileRules()
	}

	return opts
}

type confidenceClassifier interface {
	ClassifyWithConfidence(ctx context.Context, query string) (QueryType, Weights, float64, error)
}

func (e *Engine) classifyForSearch(ctx context.Context, query string) (QueryType, Weights, *float64, string, error) {
	if classifier, ok := e.classifier.(confidenceClassifier); ok {
		queryType, weights, confidence, err := classifier.ClassifyWithConfidence(ctx, query)
		if err != nil {
			return queryType, weights, nil, QueryClassificationConfidenceUnavailable, err
		}
		return queryType, weights, &confidence, QueryClassificationConfidenceAvailable, nil
	}

	queryType, weights, err := e.classifier.Classify(ctx, query)
	if err != nil {
		return queryType, weights, nil, QueryClassificationConfidenceUnavailable, err
	}
	return queryType, weights, nil, QueryClassificationConfidenceNotReported, nil
}

func recordQueryClassification(opts SearchOptions, classification QueryClassification) {
	if opts.QueryClassification == nil {
		return
	}
	*opts.QueryClassification = classification
}

func recordRerankerStatus(opts SearchOptions, status RerankerStatus) {
	if opts.RerankerStatus == nil {
		return
	}
	*opts.RerankerStatus = status
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
			Chunk:          chunk,
			Score:          f.rrfScore, // Use pre-calculated RRF score (already normalized 0-1)
			BM25Score:      f.bm25Score,
			VecScore:       f.vecScore,
			BM25Rank:       f.bm25Rank, // FEAT-UNIX3: Expose for explain mode
			VecRank:        f.vecRank,  // FEAT-UNIX3: Expose for explain mode
			InBothLists:    f.inBothLists,
			Highlights:     e.calculateHighlights(chunk.Content, f.matchedTerms),
			MatchedTerms:   f.matchedTerms, // UX-1: Expose matched terms for context display
			SourceMetadata: SourceMetadataFromChunkWithRules(chunk, e.config.MetadataRules),
		}

		results = append(results, result)
	}

	return results, nil
}

// addExactSymbolCandidates supplements exact identifier searches with chunks
// from the symbol table. BM25 can rank dense references above a long definition
// chunk after code-aware tokenization splits identifiers, so the symbol table is
// the authoritative path for exact owner lookup.
func (e *Engine) addExactSymbolCandidates(ctx context.Context, results []*SearchResult, query string, opts SearchOptions) ([]*SearchResult, error) {
	needle, quoted := exactMatchNeedle(query)
	if needle == "" || quoted || strings.ContainsAny(needle, `/\`) {
		return results, nil
	}

	seen := make(map[string]struct{}, len(results))
	maxScore := 0.0
	for _, result := range results {
		if result == nil || result.Chunk == nil {
			continue
		}
		seen[result.Chunk.ID] = struct{}{}
		if result.Score > maxScore {
			maxScore = result.Score
		}
	}
	if maxScore <= 0 {
		maxScore = 1
	}

	limit := opts.Limit
	if limit < 10 {
		limit = 10
	}

	chunks, err := e.metadata.GetChunksBySymbol(ctx, needle, limit)
	if err != nil {
		return nil, fmt.Errorf("load exact symbol candidates: %w", err)
	}

	terms := store.TokenizeCode(needle)
	for _, chunk := range chunks {
		if chunk == nil {
			continue
		}
		if _, ok := seen[chunk.ID]; ok {
			continue
		}
		results = append(results, &SearchResult{
			Chunk: chunk,
			// Tie with the current top lexical result; ApplyExactMatchBoost
			// then boosts all exact matches uniformly in the shared pipeline.
			Score:          maxScore,
			Highlights:     e.calculateHighlights(chunk.Content, terms),
			MatchedTerms:   terms,
			SourceMetadata: SourceMetadataFromChunkWithRules(chunk, e.config.MetadataRules),
		})
		seen[chunk.ID] = struct{}{}
	}

	return results, nil
}

const (
	adrReferenceDocCandidateLimit = 20
	adrReferencePathLimit         = 12
	adrReferenceChunksPerPath     = 2
)

// addADRReferenceCandidates supplements ADR-to-code searches with implementation
// paths mentioned by indexed ADR documents. This bridges decision identifiers
// such as "ADR-004" to the files named in the decision record without hardcoding
// any ADR-specific mapping.
func (e *Engine) addADRReferenceCandidates(ctx context.Context, results []*SearchResult, query string, opts SearchOptions) ([]*SearchResult, error) {
	if !shouldAddADRReferenceCandidates(query, opts) {
		return results, nil
	}

	refs := uniqueADRRefs(query)
	if len(refs) == 0 {
		return results, nil
	}

	paths, err := e.implementationPathsForADRRefs(ctx, refs)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return results, nil
	}

	seen := make(map[string]struct{}, len(results))
	maxScore := 0.0
	for _, result := range results {
		if result == nil || result.Chunk == nil {
			continue
		}
		seen[result.Chunk.ID] = struct{}{}
		if result.Score > maxScore {
			maxScore = result.Score
		}
	}
	if maxScore <= 0 {
		maxScore = 1
	}

	terms := store.TokenizeCode(query)
	addedPaths := 0
	for _, filePath := range paths {
		if addedPaths >= adrReferencePathLimit {
			break
		}
		chunks, err := e.metadata.GetChunksByPath(ctx, filePath, adrReferenceChunksPerPath)
		if err != nil {
			return nil, fmt.Errorf("load ADR implementation path candidates for %s: %w", filePath, err)
		}
		if len(chunks) == 0 {
			continue
		}
		addedForPath := false
		for _, chunk := range chunks {
			if chunk == nil {
				continue
			}
			if _, ok := seen[chunk.ID]; ok {
				continue
			}
			results = append(results, &SearchResult{
				Chunk:          chunk,
				Score:          maxScore,
				Highlights:     e.calculateHighlights(chunk.Content, terms),
				MatchedTerms:   terms,
				SourceMetadata: SourceMetadataFromChunkWithRules(chunk, e.config.MetadataRules),
			})
			seen[chunk.ID] = struct{}{}
			addedForPath = true
		}
		if addedForPath {
			addedPaths++
		}
	}

	return results, nil
}

func shouldAddADRReferenceCandidates(query string, opts SearchOptions) bool {
	if !adrRefPattern.MatchString(query) {
		return false
	}
	filter := strings.ToLower(strings.TrimSpace(opts.Filter))
	return filter == "code"
}

func (e *Engine) implementationPathsForADRRefs(ctx context.Context, refs []string) ([]string, error) {
	seenFiles := make(map[string]struct{})
	seenPaths := make(map[string]struct{})
	var paths []string

	for _, ref := range refs {
		bm25Results, err := e.bm25.Search(ctx, ref, adrReferenceDocCandidateLimit)
		if err != nil {
			return nil, fmt.Errorf("search ADR reference %s: %w", ref, err)
		}
		chunkIDs := make([]string, 0, len(bm25Results))
		for _, result := range bm25Results {
			if result != nil && result.DocID != "" {
				chunkIDs = append(chunkIDs, result.DocID)
			}
		}
		chunks, err := e.metadata.GetChunks(ctx, chunkIDs)
		if err != nil {
			return nil, fmt.Errorf("load ADR reference chunks for %s: %w", ref, err)
		}
		for _, chunk := range chunks {
			if chunk == nil || chunk.FileID == "" {
				continue
			}
			meta := SourceMetadataFromChunkWithRules(chunk, e.config.MetadataRules)
			if meta.SourceClass != SourceClassADR {
				continue
			}
			if !strings.Contains(strings.ToUpper(chunk.FilePath+"\n"+chunk.Content), strings.ToUpper(ref)) {
				continue
			}
			if _, ok := seenFiles[chunk.FileID]; ok {
				continue
			}
			seenFiles[chunk.FileID] = struct{}{}

			adrChunks, err := e.metadata.GetChunksByFile(ctx, chunk.FileID)
			if err != nil {
				return nil, fmt.Errorf("load ADR document chunks for %s: %w", chunk.FilePath, err)
			}
			for _, adrChunk := range adrChunks {
				if adrChunk == nil {
					continue
				}
				for _, filePath := range extractIndexedPathReferences(adrChunk.Content) {
					if _, ok := seenPaths[filePath]; ok {
						continue
					}
					seenPaths[filePath] = struct{}{}
					paths = append(paths, filePath)
				}
			}
		}
	}

	return paths, nil
}

type chunksByContentTypeStore interface {
	GetChunksByContentType(ctx context.Context, contentType store.ContentType, limit int) ([]*store.Chunk, error)
}

const (
	pdfContentCandidateLimit = 48
	pdfContentCandidateTopN  = 8
)

func (e *Engine) addPDFContentCandidates(ctx context.Context, results []*SearchResult, query string, opts SearchOptions) ([]*SearchResult, error) {
	if !shouldAddPDFContentCandidates(query, opts) {
		return results, nil
	}
	loader, ok := e.metadata.(chunksByContentTypeStore)
	if !ok {
		return results, nil
	}

	chunks, err := loader.GetChunksByContentType(ctx, store.ContentTypePDF, pdfContentCandidateLimit)
	if err != nil {
		return nil, fmt.Errorf("load PDF content candidates: %w", err)
	}
	if len(chunks) == 0 {
		return results, nil
	}

	queryTerms := meaningfulPDFQueryTerms(query)
	if len(queryTerms) == 0 {
		return results, nil
	}

	seen := make(map[string]struct{}, len(results))
	maxScore := 0.0
	for _, result := range results {
		if result == nil || result.Chunk == nil {
			continue
		}
		seen[result.Chunk.ID] = struct{}{}
		if result.Score > maxScore {
			maxScore = result.Score
		}
	}
	if maxScore <= 0 {
		maxScore = 1
	}

	type scoredChunk struct {
		chunk *store.Chunk
		score int
	}
	scored := make([]scoredChunk, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk == nil {
			continue
		}
		if _, ok := seen[chunk.ID]; ok {
			continue
		}
		score := pdfQueryOverlapScore(queryTerms, chunk)
		if score == 0 {
			continue
		}
		scored = append(scored, scoredChunk{chunk: chunk, score: score})
	}
	if len(scored) == 0 {
		return results, nil
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].chunk.FilePath != scored[j].chunk.FilePath {
			return scored[i].chunk.FilePath < scored[j].chunk.FilePath
		}
		return scored[i].chunk.StartLine < scored[j].chunk.StartLine
	})
	if len(scored) > pdfContentCandidateTopN {
		scored = scored[:pdfContentCandidateTopN]
	}

	terms := store.TokenizeCode(query)
	for _, candidate := range scored {
		results = append(results, &SearchResult{
			Chunk:          candidate.chunk,
			Score:          maxScore * (1 + 0.05*float64(candidate.score)),
			Highlights:     e.calculateHighlights(candidate.chunk.Content, terms),
			MatchedTerms:   terms,
			SourceMetadata: SourceMetadataFromChunkWithRules(candidate.chunk, e.config.MetadataRules),
		})
		seen[candidate.chunk.ID] = struct{}{}
	}

	return results, nil
}

func shouldAddPDFContentCandidates(query string, opts SearchOptions) bool {
	if !shouldBoostPDFContent(query) {
		return false
	}
	filter := strings.ToLower(strings.TrimSpace(opts.Filter))
	return filter == "" || filter == "all" || filter == "docs"
}

func shouldBoostPDFContent(query string) bool {
	query = strings.TrimSpace(query)
	if !strings.Contains(strings.ToLower(query), "pdf") {
		return false
	}
	if !naturalLanguagePattern.MatchString(query) {
		return false
	}
	if strings.ContainsAny(query, `/\`) {
		return false
	}
	return !containsTechnicalLookupToken(query)
}

func containsTechnicalLookupToken(query string) bool {
	for _, token := range strings.Fields(query) {
		token = strings.Trim(token, `"'.,:;()[]{}<>`)
		if token == "" || strings.EqualFold(token, "pdf") {
			continue
		}
		if camelCasePattern.MatchString(token) ||
			pascalCasePattern.MatchString(token) ||
			exportedIdentifierPattern.MatchString(token) ||
			snakeCasePattern.MatchString(token) ||
			screamingSnakePattern.MatchString(token) {
			return true
		}
	}
	return false
}

func meaningfulPDFQueryTerms(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	seen := make(map[string]struct{}, len(fields))
	terms := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) < 3 || isStopWord(field) {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		terms = append(terms, field)
	}
	return terms
}

func pdfQueryOverlapScore(queryTerms []string, chunk *store.Chunk) int {
	text := strings.ToLower(chunk.FilePath + "\n" + chunk.Content)
	score := 0
	for _, term := range queryTerms {
		if strings.Contains(text, term) {
			score++
		}
	}
	return score
}

func uniqueADRRefs(query string) []string {
	matches := adrRefPattern.FindAllString(query, -1)
	seen := make(map[string]struct{}, len(matches))
	refs := make([]string, 0, len(matches))
	for _, match := range matches {
		ref := strings.ToUpper(match)
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
	}
	return refs
}

func extractIndexedPathReferences(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		switch r {
		case ' ', '\n', '\t', '\r', '`', '"', '\'', '(', ')', '[', ']', '{', '}', '<', '>', ',', ';':
			return true
		default:
			return false
		}
	})

	seen := make(map[string]struct{}, len(fields))
	paths := make([]string, 0, len(fields))
	for _, field := range fields {
		filePath := normalizeIndexedPathReference(field)
		if filePath == "" {
			continue
		}
		if _, ok := seen[filePath]; ok {
			continue
		}
		seen[filePath] = struct{}{}
		paths = append(paths, filePath)
	}
	return paths
}

func normalizeIndexedPathReference(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, ":.!?")
	value = strings.TrimPrefix(value, "./")
	if value == "" || strings.Contains(value, "://") || strings.HasPrefix(value, "../") {
		return ""
	}

	switch {
	case value == ".amanmcp.yaml", value == ".gitignore", value == "go.mod", value == "Makefile":
		return value
	case strings.HasPrefix(value, "cmd/"),
		strings.HasPrefix(value, "internal/"),
		strings.HasPrefix(value, "pkg/"),
		strings.HasPrefix(value, "configs/"),
		strings.HasPrefix(value, "docs/"),
		strings.HasPrefix(value, "scripts/"),
		strings.HasPrefix(value, "mlx-server/"),
		strings.HasPrefix(value, ".aman-pm/"):
		if strings.Contains(value, "/") && strings.Contains(value, ".") {
			return value
		}
	}

	return ""
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
func (e *Engine) rerankResults(ctx context.Context, query string, fused []*fusedResult, opts SearchOptions) []*fusedResult {
	overallStart := time.Now()
	policy := e.config.RerankerPolicy.normalized()
	status := RerankerStatus{
		Policy:         policy,
		CandidateCount: len(fused),
	}

	// Skip if no reranker configured.
	if e.reranker == nil {
		status.State = RerankerStateNotConfigured
		recordRerankerStatus(opts, status)
		return fused
	}

	decision := DecideRerankerPolicy(policy, query, opts.QueryClassification)
	if !decision.Apply {
		status.State = RerankerStateSkipped
		status.SkipReason = decision.SkipReason
		recordRerankerStatus(opts, status)
		return fused
	}

	// Skip if too few results to rerank
	if len(fused) < 2 {
		status.State = RerankerStateSkipped
		status.SkipReason = RerankerSkipTooFewResults
		recordRerankerStatus(opts, status)
		return fused
	}

	// DEBT-024: Measure availability check
	availStart := time.Now()
	if !e.reranker.Available(ctx) {
		slog.Debug("reranker unavailable, skipping reranking",
			slog.Duration("avail_check", time.Since(availStart)))
		status.State = RerankerStateUnavailable
		recordRerankerStatus(opts, status)
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
		status.State = RerankerStateSkipped
		status.SkipReason = RerankerSkipFetchFailed
		recordRerankerStatus(opts, status)
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
		status.State = RerankerStateSkipped
		status.SkipReason = RerankerSkipNoDocuments
		status.CandidateCount = len(documents)
		recordRerankerStatus(opts, status)
		return fused
	}
	status.CandidateCount = len(documents)

	// DEBT-024: Measure reranker call
	rerankStart := time.Now()
	reranked, err := e.reranker.Rerank(ctx, query, documents, 0) // 0 = return all
	rerankDuration := time.Since(rerankStart)
	if err != nil {
		slog.Warn("reranking failed, using original order",
			slog.String("error", err.Error()),
			slog.Duration("rerank_attempt", rerankDuration))
		status.State = RerankerStateFailed
		status.LatencyMS = rerankDuration.Milliseconds()
		recordRerankerStatus(opts, status)
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

	status.State = RerankerStateApplied
	status.RerankedCount = len(finalResults)
	status.LatencyMS = rerankDuration.Milliseconds()
	recordRerankerStatus(opts, status)
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

	subQueries := e.multiQuery.decomposer.Decompose(query)

	// FEAT-UNIX3: Get sub-queries for explain output
	var subQueryStrings []string
	if opts.Explain {
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
	enriched, err = e.addExactSymbolCandidates(ctx, enriched, query, opts)
	if err != nil {
		return nil, err
	}
	enriched, err = e.addADRReferenceCandidates(ctx, enriched, query, opts)
	if err != nil {
		return nil, err
	}
	enriched, err = e.addSubQueryPathCandidates(ctx, enriched, subQueries, opts)
	if err != nil {
		return nil, err
	}
	enriched, err = e.addPDFContentCandidates(ctx, enriched, query, opts)
	if err != nil {
		return nil, err
	}

	// FEAT-QI5: Enrich with adjacent context if requested
	e.enrichResultsWithAdjacent(ctx, enriched, opts.AdjacentChunks, 5)

	// TASK-SYN42: Exact lexical lookups should rank definitions above references.
	enriched = ApplyExactMatchBoost(enriched, query)
	enriched = ApplyPDFContentBoost(enriched, query)
	// FEAT-QI4: Apply test file penalty to prioritize real implementations
	enriched = ApplyTestFilePenalty(enriched)
	// BUG-066: Apply path boost to prioritize internal/ over cmd/
	enriched = ApplyPathBoost(enriched)
	// F39: Apply authority/freshness boost after path boosts.
	enriched = ApplyAuthorityBoost(enriched)

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

const (
	subQueryPathCandidatePathLimit = 8
	subQueryPathChunksPerPath      = 3
)

func (e *Engine) addSubQueryPathCandidates(ctx context.Context, results []*SearchResult, subQueries []SubQuery, opts SearchOptions) ([]*SearchResult, error) {
	if len(subQueries) == 0 || !hasPostRetrievalContentFilter(opts) {
		return results, nil
	}

	seenChunks := make(map[string]struct{}, len(results))
	maxScore := 0.0
	for _, result := range results {
		if result == nil || result.Chunk == nil {
			continue
		}
		seenChunks[result.Chunk.ID] = struct{}{}
		if result.Score > maxScore {
			maxScore = result.Score
		}
	}
	if maxScore <= 0 {
		maxScore = 1
	}

	seenPaths := make(map[string]struct{}, len(subQueries))
	addedPaths := 0
	for _, subQuery := range subQueries {
		filePath := strings.TrimSpace(subQuery.Query)
		if !isSafeSubQueryPathHint(filePath) {
			continue
		}
		if _, ok := seenPaths[filePath]; ok {
			continue
		}
		seenPaths[filePath] = struct{}{}
		if addedPaths >= subQueryPathCandidatePathLimit {
			break
		}

		chunks, err := e.metadata.GetChunksByPath(ctx, filePath, subQueryPathChunksPerPath)
		if err != nil {
			return nil, fmt.Errorf("load sub-query path candidates for %s: %w", filePath, err)
		}
		if len(chunks) == 0 {
			continue
		}
		terms := store.TokenizeCode(filePath)
		addedForPath := false
		for _, chunk := range chunks {
			if chunk == nil {
				continue
			}
			if _, ok := seenChunks[chunk.ID]; ok {
				continue
			}
			results = append(results, &SearchResult{
				Chunk:          chunk,
				Score:          maxScore,
				Highlights:     e.calculateHighlights(chunk.Content, terms),
				MatchedTerms:   terms,
				SourceMetadata: SourceMetadataFromChunkWithRules(chunk, e.config.MetadataRules),
			})
			seenChunks[chunk.ID] = struct{}{}
			addedForPath = true
		}
		if addedForPath {
			addedPaths++
		}
	}

	return results, nil
}

func isSafeSubQueryPathHint(filePath string) bool {
	if filePath == "" || strings.HasPrefix(filePath, "/") {
		return false
	}
	if strings.Contains(filePath, "..") {
		return false
	}
	return filePathPattern.MatchString(filePath)
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
		queryType, weights, confidence, confidenceState, err := e.classifyForSearch(ctx, query)
		if err == nil {
			opts.Weights = &weights
			recordQueryClassification(opts, QueryClassification{
				Type:            queryType,
				Confidence:      confidence,
				ConfidenceState: confidenceState,
			})
		}
	}

	// Apply defaults
	opts = e.applyDefaults(opts)

	// Handle BM25-only mode
	if opts.BM25Only {
		candidateLimit := candidateLimitForOptions(query, opts)
		bm25Results, err := e.bm25.Search(ctx, query, candidateLimit)
		if err != nil {
			return nil, fmt.Errorf("BM25 search failed: %w", err)
		}
		fused := e.fuseResults(bm25Results, nil, &Weights{BM25: 1.0, Semantic: 0.0})
		return e.convertToFusedResult(fused), nil
	}

	// Validate dimensions
	if err := e.validateDimensions(ctx); err != nil {
		// Fall back to BM25-only
		candidateLimit := candidateLimitForOptions(query, opts)
		bm25Results, bm25Err := e.bm25.Search(ctx, query, candidateLimit)
		if bm25Err != nil {
			return nil, fmt.Errorf("BM25 search failed: %w", bm25Err)
		}
		fused := e.fuseResults(bm25Results, nil, opts.Weights)
		return e.convertToFusedResult(fused), nil
	}

	// Run parallel search
	candidateLimit := candidateLimitForOptions(query, opts)
	bm25Results, vecResults, _ := e.parallelSearch(ctx, query, candidateLimit)

	// Fuse results
	fused := e.fuseResults(bm25Results, vecResults, opts.Weights)

	// Apply filtering if needed (for multi-query sub-query hints)
	if opts.Filter != "" && opts.Filter != "all" {
		// Enrich to get content type
		enriched, err := e.enrichResults(ctx, fused)
		if err != nil {
			return e.convertToFusedResult(fused), nil // Fall back to unfiltered
		}
		enriched = ApplyExactMatchBoost(enriched, query)
		enriched = ApplyPDFContentBoost(enriched, query)
		enriched = ApplyTestFilePenalty(enriched)
		enriched = ApplyPathBoost(enriched)
		enriched = ApplyAuthorityBoost(enriched)
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
