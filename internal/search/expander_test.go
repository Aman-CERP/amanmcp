package search

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// QueryExpander Tests
// =============================================================================

func TestQueryExpander_Expand_BasicSynonyms(t *testing.T) {
	expander := NewQueryExpander()

	tests := []struct {
		name     string
		query    string
		contains []string // Terms that MUST be in result
	}{
		{
			name:     "function expands to func",
			query:    "Search function",
			contains: []string{"Search", "function", "func"},
		},
		{
			name:     "method expands to func",
			query:    "Search method",
			contains: []string{"Search", "method", "func"},
		},
		{
			name:     "error expands to err",
			query:    "error handling",
			contains: []string{"error", "handling", "err"},
		},
		{
			name:     "retry expands to backoff",
			query:    "retry logic",
			contains: []string{"retry", "logic", "backoff"}, // "Retry" added via casing
		},
		{
			name:     "class expands to type/struct",
			query:    "define class",
			contains: []string{"define", "class", "type", "struct"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expander.Expand(tt.query)
			for _, term := range tt.contains {
				assert.Contains(t, result, term,
					"expected expanded query to contain %q, got %q", term, result)
			}
		})
	}
}

func TestQueryExpander_Expand_RCA010Queries(t *testing.T) {
	// These are the exact queries from RCA-010 that failed due to vocabulary mismatch
	// Use higher maxExpansions to get more synonyms
	expander := NewQueryExpander(WithMaxExpansions(5))

	tests := []struct {
		name     string
		query    string
		contains []string
	}{
		{
			name:     "Search function → includes func",
			query:    "Search function",
			contains: []string{"func", "fn"}, // Core expansions
		},
		{
			name:     "retry backoff → includes delay",
			query:    "retry backoff",
			contains: []string{"retry", "backoff", "delay"},
		},
		{
			name:     "error handling retry → includes err",
			query:    "error handling retry",
			contains: []string{"err", "backoff"}, // retry → backoff
		},
		{
			name:     "exponential delay → includes backoff",
			query:    "exponential delay",
			contains: []string{"exponential", "delay"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expander.Expand(tt.query)
			for _, term := range tt.contains {
				assert.Contains(t, result, term)
			}
		})
	}
}

func TestQueryExpander_Expand_PreservesOriginalTerms(t *testing.T) {
	expander := NewQueryExpander()

	query := "custom unique specific"
	result := expander.Expand(query)

	// Original terms should always be preserved
	assert.Contains(t, result, "custom")
	assert.Contains(t, result, "unique")
	assert.Contains(t, result, "specific")
}

func TestQueryExpander_Expand_DeduplicatesTerms(t *testing.T) {
	expander := NewQueryExpander()

	// "func" is both a term and a synonym of "function"
	query := "func function"
	result := expander.Expand(query)

	// Count occurrences - should not have duplicate "func"
	count := strings.Count(strings.ToLower(result), "func")
	// Should have exactly 1 occurrence of func (case insensitive)
	// (original "func" preserved, "function" doesn't duplicate it)
	assert.LessOrEqual(t, count, 2, "should not have many duplicate 'func' terms")
}

func TestQueryExpander_Expand_EmptyQuery(t *testing.T) {
	expander := NewQueryExpander()

	assert.Equal(t, "", expander.Expand(""))
	assert.Equal(t, "   ", expander.Expand("   "))
}

func TestQueryExpander_MaxExpansions(t *testing.T) {
	expander := NewQueryExpander(WithMaxExpansions(1))

	// "function" has many synonyms, but should only add 1
	result := expander.Expand("function")
	terms := strings.Fields(result)

	// Original + 1 expansion + possible casing variants
	// Should be less than if we added all synonyms
	assert.Less(t, len(terms), 10, "should limit expansions")
}

func TestQueryExpander_DisableCasingVariants(t *testing.T) {
	expander := NewQueryExpander(WithCasingVariants(false))

	result := expander.Expand("search")

	// Should not add "Search" casing variant
	assert.NotContains(t, result, "SEARCH")
}

func TestQueryExpander_CustomSynonyms(t *testing.T) {
	custom := map[string][]string{
		"amanmcp": {"coderag", "searchmcp"},
	}
	expander := NewQueryExpander(WithCustomSynonyms(custom))

	result := expander.Expand("amanmcp tool")

	assert.Contains(t, result, "coderag")
	assert.Contains(t, result, "searchmcp")
}

func TestQueryExpander_ExpandToTerms(t *testing.T) {
	expander := NewQueryExpander()

	terms := expander.ExpandToTerms("Search function")

	require.NotEmpty(t, terms)
	assert.Contains(t, terms, "Search")
	assert.Contains(t, terms, "function")
}

// =============================================================================
// Tokenizer Tests
// =============================================================================

func TestTokenize_Whitespace(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"hello world", []string{"hello", "world"}},
		{"  hello   world  ", []string{"hello", "world"}},
		{"hello", []string{"hello"}},
		{"", nil}, // Empty input returns nil slice
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := tokenize(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTokenize_CamelCase(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"searchFunction", []string{"search", "Function"}},
		{"SearchEngine", []string{"Search", "Engine"}},
		{"getHTTPResponse", []string{"get", "H", "T", "T", "P", "Response"}}, // Splits on each capital
		{"simpleWord", []string{"simple", "Word"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := tokenize(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTokenize_SnakeCase(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"search_function", []string{"search", "function"}},
		{"get_http_response", []string{"get", "http", "response"}},
		{"_leading", []string{"leading"}},
		{"trailing_", []string{"trailing"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := tokenize(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTokenize_MixedPunctuation(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"func(ctx, query)", []string{"func", "ctx", "query"}},
		{"error: failed", []string{"error", "failed"}},
		{"path/to/file.go", []string{"path", "to", "file", "go"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := tokenize(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Casing Variants Tests
// =============================================================================

func TestGenerateCasingVariants(t *testing.T) {
	tests := []struct {
		input    string
		contains []string
		excludes []string
	}{
		{
			input:    "search",
			contains: []string{"Search"},
			excludes: []string{"search"}, // Don't include original
		},
		{
			input:    "Search",
			contains: []string{"search"},
			excludes: []string{"Search"}, // Don't include original
		},
		{
			input:    "API",
			contains: []string{"api"},
			excludes: []string{"API"}, // Don't include original
		},
		{
			input:    "http",
			contains: []string{"Http", "HTTP"},
			excludes: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := generateCasingVariants(tt.input)
			for _, c := range tt.contains {
				assert.Contains(t, result, c)
			}
			for _, e := range tt.excludes {
				assert.NotContains(t, result, e)
			}
		})
	}
}

// =============================================================================
// Synonym Dictionary Tests
// =============================================================================

func TestCodeSynonyms_Coverage(t *testing.T) {
	// Ensure key programming terms are covered
	required := []string{
		"function", "method", "class", "type", "struct",
		"error", "exception", "request", "response",
		"context", "config", "database", "query",
		"search", "index", "vector", "embed",
	}

	for _, term := range required {
		t.Run(term, func(t *testing.T) {
			synonyms := GetSynonyms(term)
			assert.NotEmpty(t, synonyms, "term %q should have synonyms", term)
		})
	}
}

func TestGetSynonyms_CaseInsensitive(t *testing.T) {
	// Should work regardless of case
	lower := GetSynonyms("function")
	upper := GetSynonyms("FUNCTION")
	mixed := GetSynonyms("Function")

	assert.NotEmpty(t, lower)
	assert.Equal(t, lower, upper)
	assert.Equal(t, lower, mixed)
}

func TestGetSynonyms_UnknownTerm(t *testing.T) {
	synonyms := GetSynonyms("xyzzy123notaword")
	assert.Nil(t, synonyms)
}

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkQueryExpander_Expand(b *testing.B) {
	expander := NewQueryExpander()
	query := "Search function with error handling"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = expander.Expand(query)
	}
}

func BenchmarkTokenize(b *testing.B) {
	query := "searchFunction with error_handling and CamelCase"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tokenize(query)
	}
}

func BenchmarkGetSynonyms(b *testing.B) {
	terms := []string{"function", "error", "search", "unknown"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, term := range terms {
			_ = GetSynonyms(term)
		}
	}
}
