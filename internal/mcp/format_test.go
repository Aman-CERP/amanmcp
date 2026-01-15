package mcp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Aman-CERP/amanmcp/internal/search"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

func TestFormatSearchResults_Basic(t *testing.T) {
	// Given: search results with chunks
	results := []*search.SearchResult{
		{
			Chunk: &store.Chunk{
				FilePath:  "internal/auth/handler.go",
				StartLine: 42,
				EndLine:   78,
				Content:   "func AuthMiddleware() {}",
				Language:  "go",
				Symbols: []*store.Symbol{
					{Name: "AuthMiddleware", Type: store.SymbolTypeFunction},
				},
			},
			Score: 0.95,
		},
	}

	// When: formatting results
	markdown := FormatSearchResults("authentication", results)

	// Then: markdown contains expected elements
	assert.Contains(t, markdown, "## Search Results")
	assert.Contains(t, markdown, `"authentication"`)
	assert.Contains(t, markdown, "Found 1 result")
	assert.Contains(t, markdown, "internal/auth/handler.go:42-78")
	assert.Contains(t, markdown, "score: 0.95")
	assert.Contains(t, markdown, "```go")
	assert.Contains(t, markdown, "`AuthMiddleware`")
}

func TestFormatSearchResults_MultipleResults(t *testing.T) {
	// Given: multiple search results
	results := []*search.SearchResult{
		{
			Chunk: &store.Chunk{
				FilePath:  "file1.go",
				StartLine: 10,
				EndLine:   20,
				Content:   "func First() {}",
				Language:  "go",
			},
			Score: 0.9,
		},
		{
			Chunk: &store.Chunk{
				FilePath:  "file2.go",
				StartLine: 30,
				EndLine:   40,
				Content:   "func Second() {}",
				Language:  "go",
			},
			Score: 0.8,
		},
	}

	// When: formatting results
	markdown := FormatSearchResults("test", results)

	// Then: both results included
	assert.Contains(t, markdown, "Found 2 results")
	assert.Contains(t, markdown, "file1.go:10-20")
	assert.Contains(t, markdown, "file2.go:30-40")
	assert.Contains(t, markdown, "### 1.")
	assert.Contains(t, markdown, "### 2.")
}

func TestFormatSearchResults_EmptyResults(t *testing.T) {
	// Given: no results
	results := []*search.SearchResult{}

	// When: formatting empty results
	markdown := FormatSearchResults("xyznonexistent", results)

	// Then: friendly message
	assert.Contains(t, markdown, "No results found")
	assert.Contains(t, markdown, "xyznonexistent")
	assert.NotContains(t, markdown, "###")
}

func TestFormatSearchResults_NilChunk(t *testing.T) {
	// Given: result with nil chunk
	results := []*search.SearchResult{
		{Chunk: nil, Score: 0.5},
	}

	// When: formatting
	markdown := FormatSearchResults("test", results)

	// Then: nil chunk is skipped gracefully
	assert.Contains(t, markdown, "No results found")
}

func TestFormatCodeResults_WithLanguageFilter(t *testing.T) {
	// Given: code results
	results := []*search.SearchResult{
		{
			Chunk: &store.Chunk{
				FilePath:   "handler.go",
				StartLine:  10,
				EndLine:    25,
				Content:    "func Handle() {}",
				RawContent: "func Handle() {\n\t// implementation\n}",
				Language:   "go",
				Symbols: []*store.Symbol{
					{Name: "Handle", Type: store.SymbolTypeFunction},
				},
			},
			Score: 0.92,
		},
	}

	// When: formatting code results with language filter
	markdown := FormatCodeResults("handler", results, "go")

	// Then: includes language filter info and uses RawContent
	assert.Contains(t, markdown, "## Code Search Results")
	assert.Contains(t, markdown, "Language filter: `go`")
	assert.Contains(t, markdown, "```go")
	assert.Contains(t, markdown, "func Handle()")
}

func TestFormatCodeResults_NoLanguageFilter(t *testing.T) {
	// Given: code results
	results := []*search.SearchResult{
		{
			Chunk: &store.Chunk{
				FilePath:  "handler.go",
				StartLine: 10,
				EndLine:   25,
				Content:   "func Handle() {}",
				Language:  "go",
			},
			Score: 0.92,
		},
	}

	// When: formatting without language filter
	markdown := FormatCodeResults("handler", results, "")

	// Then: no language filter line
	assert.Contains(t, markdown, "## Code Search Results")
	assert.NotContains(t, markdown, "Language filter:")
}

func TestFormatCodeResults_EmptyResults(t *testing.T) {
	// Given: no code results
	results := []*search.SearchResult{}

	// When: formatting with language filter
	markdown := FormatCodeResults("handler", results, "python")

	// Then: message includes language info
	assert.Contains(t, markdown, "No code results found")
	assert.Contains(t, markdown, "in python files")
}

func TestFormatDocsResults_PreservesMarkdown(t *testing.T) {
	// Given: markdown documentation result
	results := []*search.SearchResult{
		{
			Chunk: &store.Chunk{
				FilePath: "docs/installation.md",
				Content:  "## Installation\n\nRun `go install`...",
				Language: "markdown",
			},
			Score: 0.88,
		},
	}

	// When: formatting docs results
	markdown := FormatDocsResults("installation", results)

	// Then: markdown content preserved (not wrapped in code block)
	assert.Contains(t, markdown, "## Documentation Results")
	assert.Contains(t, markdown, "docs/installation.md")
	assert.Contains(t, markdown, "## Installation")
	assert.Contains(t, markdown, "Run `go install`")
	// Should have horizontal rule separator
	assert.Contains(t, markdown, "---")
}

func TestFormatDocsResults_NonMarkdown(t *testing.T) {
	// Given: text documentation (not markdown)
	results := []*search.SearchResult{
		{
			Chunk: &store.Chunk{
				FilePath: "README.txt",
				Content:  "This is plain text documentation.",
				Language: "text",
			},
			Score: 0.75,
		},
	}

	// When: formatting
	markdown := FormatDocsResults("readme", results)

	// Then: wrapped in code block
	assert.Contains(t, markdown, "```")
	assert.Contains(t, markdown, "This is plain text documentation.")
}

func TestFormatDocsResults_Empty(t *testing.T) {
	// Given: no docs results
	results := []*search.SearchResult{}

	// When: formatting
	markdown := FormatDocsResults("nonexistent", results)

	// Then: friendly message
	assert.Contains(t, markdown, "No documentation found")
	assert.Contains(t, markdown, "nonexistent")
}

func TestClampLimit(t *testing.T) {
	tests := []struct {
		name       string
		limit      int
		defaultVal int
		min        int
		max        int
		want       int
	}{
		{"zero uses default", 0, 10, 1, 50, 10},
		{"negative uses default", -5, 10, 1, 50, 10},
		{"below min clamps to min", 0, 10, 1, 50, 10},
		{"above max clamps to max", 100, 10, 1, 50, 50},
		{"valid value unchanged", 25, 10, 1, 50, 25},
		{"at min boundary", 1, 10, 1, 50, 1},
		{"at max boundary", 50, 10, 1, 50, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampLimit(tt.limit, tt.defaultVal, tt.min, tt.max)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatSearchResults_LargeResults(t *testing.T) {
	// Given: 50 results
	results := make([]*search.SearchResult, 50)
	for i := 0; i < 50; i++ {
		results[i] = &search.SearchResult{
			Chunk: &store.Chunk{
				FilePath:  "file.go",
				StartLine: i * 10,
				EndLine:   i*10 + 10,
				Content:   "func Test() {}",
				Language:  "go",
			},
			Score: float64(50-i) / 50.0,
		}
	}

	// When: formatting
	markdown := FormatSearchResults("test", results)

	// Then: all 50 results included
	assert.Contains(t, markdown, "Found 50 results")
	assert.Equal(t, 50, strings.Count(markdown, "### "))
}

func TestFormatSearchResults_UsesRawContentWhenAvailable(t *testing.T) {
	// Given: chunk with both Content and RawContent
	results := []*search.SearchResult{
		{
			Chunk: &store.Chunk{
				FilePath:   "handler.go",
				StartLine:  10,
				EndLine:    20,
				Content:    "processed content",
				RawContent: "original raw content with formatting",
				Language:   "go",
			},
			Score: 0.9,
		},
	}

	// When: formatting
	markdown := FormatSearchResults("test", results)

	// Then: uses RawContent
	assert.Contains(t, markdown, "original raw content with formatting")
	assert.NotContains(t, markdown, "processed content")
}

func TestFormatSearchResults_FallsBackToContent(t *testing.T) {
	// Given: chunk with only Content (no RawContent)
	results := []*search.SearchResult{
		{
			Chunk: &store.Chunk{
				FilePath:  "handler.go",
				StartLine: 10,
				EndLine:   20,
				Content:   "only content available",
				Language:  "go",
			},
			Score: 0.9,
		},
	}

	// When: formatting
	markdown := FormatSearchResults("test", results)

	// Then: uses Content as fallback
	assert.Contains(t, markdown, "only content available")
}

func TestFormatSearchResults_DefaultsToTextLanguage(t *testing.T) {
	// Given: chunk without language
	results := []*search.SearchResult{
		{
			Chunk: &store.Chunk{
				FilePath:  "unknown.xyz",
				StartLine: 1,
				EndLine:   5,
				Content:   "some content",
				Language:  "", // empty language
			},
			Score: 0.8,
		},
	}

	// When: formatting
	markdown := FormatSearchResults("test", results)

	// Then: defaults to text for code block
	assert.Contains(t, markdown, "```text")
}

// =============================================================================
// UX-1: ToSearchResultOutput Tests
// =============================================================================

func TestToSearchResultOutput_BasicFields(t *testing.T) {
	// Given: a search result with basic fields
	result := &search.SearchResult{
		Chunk: &store.Chunk{
			FilePath: "internal/auth/handler.go",
			Content:  "func AuthMiddleware() {}",
			Language: "go",
		},
		Score:        0.95,
		MatchedTerms: []string{"auth", "middleware"},
		InBothLists:  true,
	}

	// When: converting to output format
	output := ToSearchResultOutput(result)

	// Then: basic fields are populated
	assert.Equal(t, "internal/auth/handler.go", output.FilePath)
	assert.Equal(t, "func AuthMiddleware() {}", output.Content)
	assert.Equal(t, 0.95, output.Score)
	assert.Equal(t, "go", output.Language)
	assert.Equal(t, []string{"auth", "middleware"}, output.MatchedTerms)
	assert.True(t, output.InBothLists)
}

func TestToSearchResultOutput_WithSymbol(t *testing.T) {
	// Given: a search result with symbol info
	result := &search.SearchResult{
		Chunk: &store.Chunk{
			FilePath: "internal/errors/retry.go",
			Content:  "func Retry(ctx context.Context, cfg RetryConfig, fn func() error) error { ... }",
			Language: "go",
			Symbols: []*store.Symbol{
				{
					Name:       "Retry",
					Type:       store.SymbolTypeFunction,
					Signature:  "func Retry(ctx context.Context, cfg RetryConfig, fn func() error) error",
					DocComment: "Retry executes fn with exponential backoff",
				},
			},
		},
		Score: 0.85,
	}

	// When: converting to output format
	output := ToSearchResultOutput(result)

	// Then: symbol info is extracted
	assert.Equal(t, "Retry", output.Symbol)
	assert.Equal(t, "function", output.SymbolType)
	assert.Equal(t, "func Retry(ctx context.Context, cfg RetryConfig, fn func() error) error", output.Signature)
	assert.Contains(t, output.MatchReason, "function 'Retry'")
}

func TestToSearchResultOutput_NilResult(t *testing.T) {
	// Given: nil result
	var result *search.SearchResult = nil

	// When: converting
	output := ToSearchResultOutput(result)

	// Then: returns empty output
	assert.Empty(t, output.FilePath)
	assert.Empty(t, output.Content)
}

func TestToSearchResultOutput_NilChunk(t *testing.T) {
	// Given: result with nil chunk
	result := &search.SearchResult{
		Chunk: nil,
		Score: 0.5,
	}

	// When: converting
	output := ToSearchResultOutput(result)

	// Then: returns empty output
	assert.Empty(t, output.FilePath)
}

func TestGenerateMatchReason_WithSymbolAndTerms(t *testing.T) {
	// Given: result with symbol and matched terms
	result := &search.SearchResult{
		Chunk: &store.Chunk{
			Symbols: []*store.Symbol{
				{Name: "Retry", Type: store.SymbolTypeFunction},
			},
		},
		MatchedTerms: []string{"retry", "backoff"},
		InBothLists:  true,
	}

	// When: generating match reason
	reason := generateMatchReason(result)

	// Then: includes all context
	assert.Contains(t, reason, "function 'Retry'")
	assert.Contains(t, reason, "matched: retry, backoff")
	assert.Contains(t, reason, "both keyword and semantic search")
}

func TestGenerateMatchReason_TermsOnly(t *testing.T) {
	// Given: result with only matched terms
	result := &search.SearchResult{
		Chunk: &store.Chunk{
			FilePath: "test.go",
			Content:  "some content",
		},
		MatchedTerms: []string{"error", "handling"},
		InBothLists:  false,
	}

	// When: generating match reason
	reason := generateMatchReason(result)

	// Then: shows terms
	assert.Contains(t, reason, "matched: error, handling")
	assert.NotContains(t, reason, "both keyword")
}

func TestGenerateMatchReason_NoMatchContext(t *testing.T) {
	// Given: result without match context
	result := &search.SearchResult{
		Chunk: &store.Chunk{
			FilePath: "test.go",
			Content:  "some content",
		},
		MatchedTerms: nil,
		InBothLists:  false,
	}

	// When: generating match reason
	reason := generateMatchReason(result)

	// Then: returns default
	assert.Equal(t, "matched content", reason)
}

func TestGenerateMatchReason_TruncatesLongDocstring(t *testing.T) {
	// Given: symbol with very long docstring
	result := &search.SearchResult{
		Chunk: &store.Chunk{
			Symbols: []*store.Symbol{
				{
					Name:       "LongFunction",
					Type:       store.SymbolTypeFunction,
					DocComment: "This is a very long documentation string that describes what this function does in great detail and should be truncated",
				},
			},
		},
	}

	// When: generating match reason
	reason := generateMatchReason(result)

	// Then: docstring is truncated
	assert.Contains(t, reason, "...")
	assert.Less(t, len(reason), 200) // Should be reasonable length
}

func TestGenerateMatchReason_LimitsManyTerms(t *testing.T) {
	// Given: result with many matched terms
	result := &search.SearchResult{
		Chunk: &store.Chunk{
			FilePath: "test.go",
		},
		MatchedTerms: []string{"term1", "term2", "term3", "term4", "term5", "term6", "term7"},
	}

	// When: generating match reason
	reason := generateMatchReason(result)

	// Then: only first 5 terms shown
	assert.Contains(t, reason, "term1")
	assert.Contains(t, reason, "term5")
	assert.NotContains(t, reason, "term6")
}
