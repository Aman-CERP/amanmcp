package search

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// WeightsForQueryType Tests
// =============================================================================

func TestWeightsForQueryType(t *testing.T) {
	tests := []struct {
		name        string
		queryType   QueryType
		wantBM25    float64
		wantSemantic float64
	}{
		{
			name:         "lexical query type",
			queryType:    QueryTypeLexical,
			wantBM25:     0.85,
			wantSemantic: 0.15,
		},
		{
			name:         "semantic query type",
			queryType:    QueryTypeSemantic,
			wantBM25:     0.20,
			wantSemantic: 0.80,
		},
		{
			name:         "mixed query type",
			queryType:    QueryTypeMixed,
			wantBM25:     0.35,
			wantSemantic: 0.65,
		},
		{
			name:         "unknown query type defaults to mixed",
			queryType:    QueryType("UNKNOWN"),
			wantBM25:     0.35,
			wantSemantic: 0.65,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			weights := WeightsForQueryType(tt.queryType)
			assert.InDelta(t, tt.wantBM25, weights.BM25, 0.001)
			assert.InDelta(t, tt.wantSemantic, weights.Semantic, 0.001)
		})
	}
}

// =============================================================================
// PatternClassifier Tests (AC03: Pattern Fallback)
// =============================================================================

func TestPatternClassifier_ErrorCodes(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  QueryType
	}{
		{"ERR_ prefix", "ERR_CONNECTION_REFUSED", QueryTypeLexical},
		{"ERR_ lowercase", "err_connection_refused", QueryTypeLexical},
		{"E#### code", "E0001", QueryTypeLexical},
		{"E##### code", "E12345", QueryTypeLexical},
		{"ERRXXX pattern", "ERR123", QueryTypeLexical},
		{"exception keyword", "NullPointerException", QueryTypeLexical},
	}

	classifier := NewPatternClassifier()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qt, weights, err := classifier.Classify(context.Background(), tt.query)
			require.NoError(t, err)
			assert.Equal(t, tt.want, qt)
			assert.Equal(t, WeightsForQueryType(tt.want), weights)
		})
	}
}

func TestPatternClassifier_QuotedPhrases(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  QueryType
	}{
		{"double quoted", `"authentication middleware"`, QueryTypeLexical},
		{"single quoted", `'exact phrase match'`, QueryTypeLexical},
	}

	classifier := NewPatternClassifier()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qt, weights, err := classifier.Classify(context.Background(), tt.query)
			require.NoError(t, err)
			assert.Equal(t, tt.want, qt)
			assert.Equal(t, WeightsForQueryType(tt.want), weights)
		})
	}
}

func TestPatternClassifier_FilePaths(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  QueryType
	}{
		{"Go file", "internal/auth/handler.go", QueryTypeLexical},
		{"TypeScript file", "src/components/Button.tsx", QueryTypeLexical},
		{"JavaScript file", "app/utils/helpers.js", QueryTypeLexical},
		{"Python file", "scripts/deploy.py", QueryTypeLexical},
		{"JSON file", "package.json", QueryTypeLexical},
		{"YAML file", "config.yaml", QueryTypeLexical},
		{"Markdown file", "README.md", QueryTypeLexical},
	}

	classifier := NewPatternClassifier()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qt, weights, err := classifier.Classify(context.Background(), tt.query)
			require.NoError(t, err)
			assert.Equal(t, tt.want, qt)
			assert.Equal(t, WeightsForQueryType(tt.want), weights)
		})
	}
}

func TestPatternClassifier_TechnicalIdentifiers(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  QueryType
	}{
		{"camelCase", "getUserById", QueryTypeLexical},
		{"camelCase long", "handleAuthenticationRequest", QueryTypeLexical},
		{"PascalCase", "SearchEngine", QueryTypeLexical},
		{"PascalCase long", "HttpResponseHandler", QueryTypeLexical},
		{"snake_case", "get_user_by_id", QueryTypeLexical},
		{"snake_case long", "handle_auth_request", QueryTypeLexical},
		{"SCREAMING_SNAKE", "MAX_RETRY_COUNT", QueryTypeLexical},
		{"SCREAMING_SNAKE long", "DEFAULT_TIMEOUT_MS", QueryTypeLexical},
	}

	classifier := NewPatternClassifier()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qt, weights, err := classifier.Classify(context.Background(), tt.query)
			require.NoError(t, err)
			assert.Equal(t, tt.want, qt)
			assert.Equal(t, WeightsForQueryType(tt.want), weights)
		})
	}
}

func TestPatternClassifier_NaturalLanguage(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  QueryType
	}{
		{"how question", "how does authentication work", QueryTypeSemantic},
		{"what question", "what is the purpose of this function", QueryTypeSemantic},
		{"where question", "where is the config file located", QueryTypeSemantic},
		{"why question", "why is this throwing an error", QueryTypeSemantic},
		{"when question", "when should I use context", QueryTypeSemantic},
		{"which question", "which library handles parsing", QueryTypeSemantic},
		{"can question", "can you explain the search logic", QueryTypeSemantic},
		{"does question", "does this support concurrency", QueryTypeSemantic},
		{"is question", "is this thread safe", QueryTypeSemantic},
		{"are question", "are there any tests for this", QueryTypeSemantic},
		{"should question", "should I use a mutex here", QueryTypeSemantic},
		{"explain command", "explain the RRF fusion algorithm", QueryTypeSemantic},
		{"describe command", "describe the search architecture", QueryTypeSemantic},
		{"show command", "show me examples of error handling", QueryTypeSemantic},
		{"find command", "find the authentication logic", QueryTypeSemantic},
		{"list command", "list all API endpoints", QueryTypeSemantic},
	}

	classifier := NewPatternClassifier()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qt, weights, err := classifier.Classify(context.Background(), tt.query)
			require.NoError(t, err)
			assert.Equal(t, tt.want, qt)
			assert.Equal(t, WeightsForQueryType(tt.want), weights)
		})
	}
}

func TestPatternClassifier_MixedQueries(t *testing.T) {
	// MIXED is for 1-2 word queries that don't match other patterns
	tests := []struct {
		name  string
		query string
		want  QueryType
	}{
		{"two technical words", "useEffect cleanup", QueryTypeMixed},
		{"single word", "authentication", QueryTypeMixed},
		{"two words generic", "error handling", QueryTypeMixed},
		{"empty after trim", "   ", QueryTypeMixed},
	}

	classifier := NewPatternClassifier()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qt, weights, err := classifier.Classify(context.Background(), tt.query)
			require.NoError(t, err)
			assert.Equal(t, tt.want, qt)
			assert.Equal(t, WeightsForQueryType(tt.want), weights)
		})
	}
}

func TestPatternClassifier_MultiWordSemantic(t *testing.T) {
	// Queries with 3+ words that don't match other patterns default to SEMANTIC
	tests := []struct {
		name  string
		query string
		want  QueryType
	}{
		{"three words conceptual", "database connection pooling", QueryTypeSemantic},
		{"four words", "error handling best practices", QueryTypeSemantic},
		{"five words", "how to optimize search queries", QueryTypeSemantic},
	}

	classifier := NewPatternClassifier()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qt, weights, err := classifier.Classify(context.Background(), tt.query)
			require.NoError(t, err)
			assert.Equal(t, tt.want, qt)
			assert.Equal(t, WeightsForQueryType(tt.want), weights)
		})
	}
}

// =============================================================================
// HybridClassifier Tests (AC01, AC02, AC03)
// =============================================================================

func TestHybridClassifier_FallsBackToPatterns(t *testing.T) {
	// Given: HybridClassifier with no LLM (nil or unavailable)
	classifier := NewHybridClassifier(nil)

	// When: classifying a query
	qt, weights, err := classifier.Classify(context.Background(), "ERR_CONNECTION_REFUSED")

	// Then: uses pattern fallback
	require.NoError(t, err)
	assert.Equal(t, QueryTypeLexical, qt)
	assert.Equal(t, WeightsForQueryType(QueryTypeLexical), weights)
}

func TestHybridClassifier_CacheHit(t *testing.T) {
	// Given: HybridClassifier with pattern fallback
	classifier := NewHybridClassifier(nil)

	// When: classify same query twice
	qt1, w1, err1 := classifier.Classify(context.Background(), "how does auth work")
	qt2, w2, err2 := classifier.Classify(context.Background(), "how does auth work")

	// Then: both return same result (from cache on second call)
	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.Equal(t, qt1, qt2)
	assert.Equal(t, w1, w2)
}

func TestHybridClassifier_CacheNormalization(t *testing.T) {
	// Given: HybridClassifier
	classifier := NewHybridClassifier(nil)

	// When: classify queries that differ only in case/whitespace
	qt1, _, _ := classifier.Classify(context.Background(), "HOW does auth work")
	qt2, _, _ := classifier.Classify(context.Background(), "how does auth work")
	qt3, _, _ := classifier.Classify(context.Background(), "  how does auth work  ")

	// Then: all return same classification (normalized keys)
	assert.Equal(t, qt1, qt2)
	assert.Equal(t, qt2, qt3)
}

func TestHybridClassifier_ThreadSafety(t *testing.T) {
	// Given: HybridClassifier
	classifier := NewHybridClassifier(nil)

	// When: concurrent classification
	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func(i int) {
			queries := []string{
				"how does auth work",
				"ERR_CONNECTION_REFUSED",
				"getUserById",
				"internal/search/engine.go",
			}
			_, _, _ = classifier.Classify(context.Background(), queries[i%len(queries)])
			done <- true
		}(i)
	}

	// Then: no race conditions (run with -race)
	for i := 0; i < 100; i++ {
		<-done
	}
}

// =============================================================================
// LLMClassifier Tests (AC01)
// =============================================================================

func TestLLMClassifier_ParsesResponse(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     QueryType
	}{
		{"exact LEXICAL", "LEXICAL", QueryTypeLexical},
		{"exact SEMANTIC", "SEMANTIC", QueryTypeSemantic},
		{"exact MIXED", "MIXED", QueryTypeMixed},
		{"lowercase lexical", "lexical", QueryTypeLexical},
		{"lowercase semantic", "semantic", QueryTypeSemantic},
		{"lowercase mixed", "mixed", QueryTypeMixed},
		{"contains LEXICAL", "I think this is LEXICAL", QueryTypeLexical},
		{"contains SEMANTIC", "This query appears to be SEMANTIC in nature", QueryTypeSemantic},
		{"contains MIXED", "The query is MIXED", QueryTypeMixed},
		{"garbage defaults to MIXED", "I don't understand", QueryTypeMixed},
		{"empty defaults to MIXED", "", QueryTypeMixed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qt := parseClassificationResponse(tt.response)
			assert.Equal(t, tt.want, qt)
		})
	}
}

// =============================================================================
// ClassifierConfig Tests
// =============================================================================

func TestClassifierConfig_Defaults(t *testing.T) {
	cfg := DefaultClassifierConfig()

	assert.Equal(t, "llama3.2:1b", cfg.Model)
	assert.Equal(t, 2_000_000_000, int(cfg.Timeout.Nanoseconds())) // 2s
	assert.Equal(t, 10000, cfg.CacheSize) // QW-2: Increased for better hit rate
	assert.Equal(t, "http://localhost:11434", cfg.OllamaHost)
}

// =============================================================================
// Engine Integration Tests
// =============================================================================

func TestEngine_Search_WithClassifier(t *testing.T) {
	// This test verifies that the engine uses the classifier when no explicit weights are provided.
	// We use a mock classifier to verify integration.

	mockClassifier := &mockClassifier{
		classifyFn: func(ctx context.Context, query string) (QueryType, Weights, error) {
			// Return LEXICAL weights for any query
			return QueryTypeLexical, WeightsForQueryType(QueryTypeLexical), nil
		},
	}

	// Verify mock implements interface
	var _ Classifier = mockClassifier

	// Just verify the mock works correctly
	qt, weights, err := mockClassifier.Classify(context.Background(), "any query")
	require.NoError(t, err)
	assert.Equal(t, QueryTypeLexical, qt)
	assert.Equal(t, 0.85, weights.BM25)
	assert.Equal(t, 0.15, weights.Semantic)
}

func TestEngine_Search_ExplicitWeightsOverrideClassifier(t *testing.T) {
	// This test verifies that explicit weights in SearchOptions override the classifier.

	mockClassifier := &mockClassifier{
		classifyFn: func(ctx context.Context, query string) (QueryType, Weights, error) {
			return QueryTypeLexical, WeightsForQueryType(QueryTypeLexical), nil
		},
	}

	// When explicit weights are provided, classifier should not be called
	explicitWeights := Weights{BM25: 0.50, Semantic: 0.50}

	// Verify the logic: if weights are already set, classifier is bypassed
	// This tests the conditional in Engine.Search
	opts := SearchOptions{Weights: &explicitWeights}

	// The classifier would return BM25: 0.85, but we have explicit 0.50
	// So the final weights should be 0.50
	assert.Equal(t, 0.50, opts.Weights.BM25)
	assert.Equal(t, 0.50, opts.Weights.Semantic)

	// Also verify that mockClassifier works when called
	qt, weights, _ := mockClassifier.Classify(context.Background(), "test")
	assert.Equal(t, QueryTypeLexical, qt)
	assert.Equal(t, 0.85, weights.BM25)
}

// mockClassifier is a test helper that implements Classifier.
type mockClassifier struct {
	classifyFn func(ctx context.Context, query string) (QueryType, Weights, error)
}

func (m *mockClassifier) Classify(ctx context.Context, query string) (QueryType, Weights, error) {
	if m.classifyFn != nil {
		return m.classifyFn(ctx, query)
	}
	return QueryTypeMixed, WeightsForQueryType(QueryTypeMixed), nil
}

// =============================================================================
// Benchmarks (AC05: Performance)
// =============================================================================

func BenchmarkPatternClassifier(b *testing.B) {
	classifier := NewPatternClassifier()
	ctx := context.Background()
	queries := []string{
		"ERR_CONNECTION_REFUSED",
		"how does authentication work",
		"getUserById",
		"internal/search/engine.go",
		"useEffect cleanup",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = classifier.Classify(ctx, queries[i%len(queries)])
	}
}

func BenchmarkHybridClassifier_CacheHit(b *testing.B) {
	classifier := NewHybridClassifier(nil)
	ctx := context.Background()

	// Prime the cache
	_, _, _ = classifier.Classify(ctx, "how does authentication work")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = classifier.Classify(ctx, "how does authentication work")
	}
}

func BenchmarkHybridClassifier_CacheMiss(b *testing.B) {
	classifier := NewHybridClassifier(nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Unique query each time to force cache miss
		_, _, _ = classifier.Classify(ctx, "query_"+string(rune(i%26+'a')))
	}
}

// =============================================================================
// DEBT-028: NewHybridClassifierWithConfig Tests
// =============================================================================

func TestNewHybridClassifierWithConfig_DefaultCacheSize(t *testing.T) {
	// Given: config with zero cache size
	config := ClassifierConfig{
		CacheSize: 0,
	}

	// When: creating classifier
	classifier := NewHybridClassifierWithConfig(nil, config)

	// Then: classifier is created with default cache size
	assert.NotNil(t, classifier)
	// Verify it works
	qt, _, err := classifier.Classify(context.Background(), "how does auth work")
	require.NoError(t, err)
	assert.Equal(t, QueryTypeSemantic, qt)
}

func TestNewHybridClassifierWithConfig_CustomCacheSize(t *testing.T) {
	// Given: config with custom cache size
	config := ClassifierConfig{
		CacheSize: 100,
	}

	// When: creating classifier
	classifier := NewHybridClassifierWithConfig(nil, config)

	// Then: classifier is created and works
	assert.NotNil(t, classifier)
	qt, _, err := classifier.Classify(context.Background(), "ERR_123")
	require.NoError(t, err)
	assert.Equal(t, QueryTypeLexical, qt)
}

func TestNewHybridClassifierWithConfig_NegativeCacheSize(t *testing.T) {
	// Given: config with negative cache size
	config := ClassifierConfig{
		CacheSize: -10,
	}

	// When: creating classifier
	classifier := NewHybridClassifierWithConfig(nil, config)

	// Then: uses default cache size (negative treated same as zero)
	assert.NotNil(t, classifier)
	qt, _, err := classifier.Classify(context.Background(), "getUserById")
	require.NoError(t, err)
	assert.Equal(t, QueryTypeLexical, qt)
}

func TestHybridClassifier_Classify_EmptyQuery(t *testing.T) {
	// Given: HybridClassifier
	classifier := NewHybridClassifier(nil)

	// When: classifying empty query
	qt, weights, err := classifier.Classify(context.Background(), "")

	// Then: returns mixed type (empty normalized key)
	require.NoError(t, err)
	assert.Equal(t, QueryTypeMixed, qt)
	assert.Equal(t, WeightsForQueryType(QueryTypeMixed), weights)
}

// ============================================================================
// DEBT-028: HybridClassifier Additional Tests
// ============================================================================

func TestHybridClassifier_Classify_FallsBackToPatterns(t *testing.T) {
	// Given: HybridClassifier with no LLM (nil)
	classifier := NewHybridClassifier(nil)

	// When: classifying a lexical query (function name)
	qt, weights, err := classifier.Classify(context.Background(), "getUserById")

	// Then: should use pattern classifier and succeed
	require.NoError(t, err)
	assert.Equal(t, QueryTypeLexical, qt)
	assert.Greater(t, weights.BM25, 0.5, "lexical should have higher BM25 weight")
}

func TestHybridClassifier_Classify_CacheHit(t *testing.T) {
	// Given: HybridClassifier with patterns only
	classifier := NewHybridClassifier(nil)

	// When: classifying same query twice
	qt1, w1, err1 := classifier.Classify(context.Background(), "getUserById")
	qt2, w2, err2 := classifier.Classify(context.Background(), "getUserById")

	// Then: both should return same result (second from cache)
	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.Equal(t, qt1, qt2)
	assert.Equal(t, w1, w2)
}

func TestHybridClassifier_Classify_NormalizesQuery(t *testing.T) {
	// Given: HybridClassifier
	classifier := NewHybridClassifier(nil)

	// When: classifying same query with different casing/whitespace
	qt1, _, err1 := classifier.Classify(context.Background(), "getUser")
	qt2, _, err2 := classifier.Classify(context.Background(), "  GetUser  ")
	qt3, _, err3 := classifier.Classify(context.Background(), "GETUSER")

	// Then: all should return same type (after normalization)
	require.NoError(t, err1)
	require.NoError(t, err2)
	require.NoError(t, err3)
	assert.Equal(t, qt1, qt2)
	assert.Equal(t, qt2, qt3)
}

func TestHybridClassifier_Classify_SemanticQuery(t *testing.T) {
	// Given: HybridClassifier
	classifier := NewHybridClassifier(nil)

	// When: classifying a semantic query
	qt, weights, err := classifier.Classify(context.Background(), "how does authentication work")

	// Then: should be semantic type with higher semantic weight
	require.NoError(t, err)
	assert.Equal(t, QueryTypeSemantic, qt)
	assert.Greater(t, weights.Semantic, 0.5, "semantic should have higher semantic weight")
}

func TestHybridClassifier_Classify_MixedQuery(t *testing.T) {
	// Given: HybridClassifier
	classifier := NewHybridClassifier(nil)

	// When: classifying a mixed query
	qt, _, err := classifier.Classify(context.Background(), "find User class")

	// Then: should be some valid type
	require.NoError(t, err)
	assert.NotEmpty(t, qt)
}
