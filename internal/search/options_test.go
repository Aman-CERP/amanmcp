package search

import (
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/store"
	"github.com/stretchr/testify/assert"
)

// =============================================================================
// NormalizeScope Tests
// =============================================================================

func TestNormalizeScope(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no slashes",
			input:    "services/api",
			expected: "services/api",
		},
		{
			name:     "leading slash",
			input:    "/services/api",
			expected: "services/api",
		},
		{
			name:     "trailing slash",
			input:    "services/api/",
			expected: "services/api",
		},
		{
			name:     "both slashes",
			input:    "/services/api/",
			expected: "services/api",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "just slash",
			input:    "/",
			expected: "",
		},
		{
			name:     "multiple leading slashes",
			input:    "///services/api",
			expected: "services/api",
		},
		{
			name:     "multiple trailing slashes",
			input:    "services/api///",
			expected: "services/api",
		},
		{
			name:     "nested path",
			input:    "services/api/v2/handlers",
			expected: "services/api/v2/handlers",
		},
		{
			name:     "single directory",
			input:    "src",
			expected: "src",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeScope(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// =============================================================================
// scopeFilter Tests
// =============================================================================

func TestScopeFilter_SingleScope(t *testing.T) {
	filter := scopeFilter([]string{"services/api"})

	tests := []struct {
		name     string
		filePath string
		expected bool
	}{
		{
			name:     "exact directory match",
			filePath: "services/api/auth.go",
			expected: true,
		},
		{
			name:     "nested match",
			filePath: "services/api/v2/handler.go",
			expected: true,
		},
		{
			name:     "no match different service",
			filePath: "services/web/index.ts",
			expected: false,
		},
		{
			name:     "partial no match - similar prefix",
			filePath: "services/api-v2/file.go",
			expected: false,
		},
		{
			name:     "completely different path",
			filePath: "lib/utils/helper.go",
			expected: false,
		},
		{
			name:     "match with leading slash in path",
			filePath: "/services/api/handler.go",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &SearchResult{
				Chunk: &store.Chunk{FilePath: tt.filePath},
			}
			assert.Equal(t, tt.expected, filter(result))
		})
	}
}

func TestScopeFilter_MultipleScopes_ORLogic(t *testing.T) {
	filter := scopeFilter([]string{"services/api", "services/web", "lib"})

	tests := []struct {
		name     string
		filePath string
		expected bool
	}{
		{
			name:     "matches first scope",
			filePath: "services/api/auth.go",
			expected: true,
		},
		{
			name:     "matches second scope",
			filePath: "services/web/index.ts",
			expected: true,
		},
		{
			name:     "matches third scope",
			filePath: "lib/utils.go",
			expected: true,
		},
		{
			name:     "matches none",
			filePath: "services/db/query.go",
			expected: false,
		},
		{
			name:     "matches none - root level",
			filePath: "main.go",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &SearchResult{
				Chunk: &store.Chunk{FilePath: tt.filePath},
			}
			assert.Equal(t, tt.expected, filter(result))
		})
	}
}

func TestScopeFilter_NilChunk(t *testing.T) {
	filter := scopeFilter([]string{"services"})

	result := &SearchResult{Chunk: nil}
	assert.False(t, filter(result))
}

func TestScopeFilter_EmptyScopes(t *testing.T) {
	filter := scopeFilter([]string{})

	// Empty scopes should match everything (no filtering)
	result := &SearchResult{
		Chunk: &store.Chunk{FilePath: "any/path/file.go"},
	}
	assert.True(t, filter(result))
}

func TestScopeFilter_OnlyEmptyStrings(t *testing.T) {
	filter := scopeFilter([]string{"", "", "/"})

	// All empty/invalid scopes should match everything
	result := &SearchResult{
		Chunk: &store.Chunk{FilePath: "any/path/file.go"},
	}
	assert.True(t, filter(result))
}

func TestScopeFilter_MixedEmptyAndValid(t *testing.T) {
	filter := scopeFilter([]string{"", "services/api", "/"})

	tests := []struct {
		name     string
		filePath string
		expected bool
	}{
		{
			name:     "matches valid scope",
			filePath: "services/api/handler.go",
			expected: true,
		},
		{
			name:     "no match",
			filePath: "lib/utils.go",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &SearchResult{
				Chunk: &store.Chunk{FilePath: tt.filePath},
			}
			assert.Equal(t, tt.expected, filter(result))
		})
	}
}

func TestScopeFilter_CaseSensitive(t *testing.T) {
	filter := scopeFilter([]string{"Services/API"})

	tests := []struct {
		name     string
		filePath string
		expected bool
	}{
		{
			name:     "exact case match",
			filePath: "Services/API/handler.go",
			expected: true,
		},
		{
			name:     "lowercase no match",
			filePath: "services/api/handler.go",
			expected: false,
		},
		{
			name:     "mixed case no match",
			filePath: "Services/api/handler.go",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &SearchResult{
				Chunk: &store.Chunk{FilePath: tt.filePath},
			}
			assert.Equal(t, tt.expected, filter(result))
		})
	}
}

// =============================================================================
// ApplyFilters with Scopes Tests
// =============================================================================

func TestApplyFilters_WithScopes(t *testing.T) {
	// Given: results from different directories
	results := []*SearchResult{
		{Chunk: &store.Chunk{FilePath: "services/api/handler.go", ContentType: store.ContentTypeCode}},
		{Chunk: &store.Chunk{FilePath: "services/web/index.ts", ContentType: store.ContentTypeCode}},
		{Chunk: &store.Chunk{FilePath: "services/db/query.go", ContentType: store.ContentTypeCode}},
		{Chunk: &store.Chunk{FilePath: "lib/utils.go", ContentType: store.ContentTypeCode}},
	}

	// When: filtering with scope
	opts := SearchOptions{
		Scopes: []string{"services/api", "lib"},
	}
	filtered := ApplyFilters(results, opts)

	// Then: only matching scopes returned
	assert.Len(t, filtered, 2)
	assert.Equal(t, "services/api/handler.go", filtered[0].Chunk.FilePath)
	assert.Equal(t, "lib/utils.go", filtered[1].Chunk.FilePath)
}

func TestApplyFilters_ScopesWithOtherFilters(t *testing.T) {
	// Given: results with different content types and paths
	results := []*SearchResult{
		{Chunk: &store.Chunk{FilePath: "services/api/handler.go", ContentType: store.ContentTypeCode, Language: "go"}},
		{Chunk: &store.Chunk{FilePath: "services/api/README.md", ContentType: store.ContentTypeMarkdown}},
		{Chunk: &store.Chunk{FilePath: "services/web/server.ts", ContentType: store.ContentTypeCode, Language: "typescript"}},
	}

	// When: filtering with scope AND content type
	opts := SearchOptions{
		Filter: "code",
		Scopes: []string{"services/api"},
	}
	filtered := ApplyFilters(results, opts)

	// Then: only code in services/api returned (AND logic between filter types)
	assert.Len(t, filtered, 1)
	assert.Equal(t, "services/api/handler.go", filtered[0].Chunk.FilePath)
}

func TestApplyFilters_EmptyScopes_NoFiltering(t *testing.T) {
	// Given: results
	results := []*SearchResult{
		{Chunk: &store.Chunk{FilePath: "a.go", ContentType: store.ContentTypeCode}},
		{Chunk: &store.Chunk{FilePath: "b.go", ContentType: store.ContentTypeCode}},
	}

	// When: no scopes specified
	opts := SearchOptions{
		Scopes: []string{},
	}
	filtered := ApplyFilters(results, opts)

	// Then: all results returned (no filtering)
	assert.Len(t, filtered, 2)
}

func TestApplyFilters_InvalidScope_ReturnsEmpty(t *testing.T) {
	// Given: results
	results := []*SearchResult{
		{Chunk: &store.Chunk{FilePath: "services/api/handler.go"}},
		{Chunk: &store.Chunk{FilePath: "lib/utils.go"}},
	}

	// When: filtering with non-existent scope
	opts := SearchOptions{
		Scopes: []string{"nonexistent/path"},
	}
	filtered := ApplyFilters(results, opts)

	// Then: empty results, no error
	assert.Empty(t, filtered)
}

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkNormalizeScope(b *testing.B) {
	scope := "/services/api/v2/"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NormalizeScope(scope)
	}
}

func BenchmarkScopeFilter_SingleScope(b *testing.B) {
	filter := scopeFilter([]string{"services/api"})
	result := &SearchResult{Chunk: &store.Chunk{FilePath: "services/api/handler.go"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = filter(result)
	}
}

func BenchmarkScopeFilter_MultipleScopes(b *testing.B) {
	filter := scopeFilter([]string{
		"services/api",
		"services/web",
		"services/db",
		"lib/utils",
		"lib/core",
	})
	result := &SearchResult{Chunk: &store.Chunk{FilePath: "lib/core/types.go"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = filter(result)
	}
}

func BenchmarkApplyFilters_WithScope_100Results(b *testing.B) {
	// Create 100 results
	results := make([]*SearchResult, 100)
	for i := 0; i < 100; i++ {
		path := "services/api/handler.go"
		if i%2 == 0 {
			path = "services/web/server.go"
		}
		results[i] = &SearchResult{
			Chunk: &store.Chunk{
				FilePath:    path,
				ContentType: store.ContentTypeCode,
			},
		}
	}

	opts := SearchOptions{
		Filter: "code",
		Scopes: []string{"services/api"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ApplyFilters(results, opts)
	}
}

// =============================================================================
// FEAT-QI4: Test File Penalty Tests
// =============================================================================

func TestIsTestFile_Go(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		expected bool
	}{
		{
			name:     "go test file",
			filePath: "internal/search/engine_test.go",
			expected: true,
		},
		{
			name:     "go implementation file",
			filePath: "internal/search/engine.go",
			expected: false,
		},
		{
			name:     "nested test file",
			filePath: "pkg/utils/helpers_test.go",
			expected: true,
		},
		{
			name:     "file with test in name but not suffix",
			filePath: "internal/testutils/helper.go",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsTestFile(tt.filePath)
			assert.Equal(t, tt.expected, got, "IsTestFile(%q)", tt.filePath)
		})
	}
}

func TestIsTestFile_JavaScript(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		expected bool
	}{
		{
			name:     "jest test file",
			filePath: "src/components/Button.test.js",
			expected: true,
		},
		{
			name:     "jest test tsx file",
			filePath: "src/components/Button.test.tsx",
			expected: true,
		},
		{
			name:     "spec file",
			filePath: "src/utils/helpers.spec.ts",
			expected: true,
		},
		{
			name:     "implementation file",
			filePath: "src/components/Button.tsx",
			expected: false,
		},
		{
			name:     "__tests__ directory",
			filePath: "src/__tests__/integration.js",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsTestFile(tt.filePath)
			assert.Equal(t, tt.expected, got, "IsTestFile(%q)", tt.filePath)
		})
	}
}

func TestIsTestFile_Python(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		expected bool
	}{
		{
			name:     "test_ prefix",
			filePath: "tests/test_utils.py",
			expected: true,
		},
		{
			name:     "_test suffix",
			filePath: "src/utils_test.py",
			expected: true,
		},
		{
			name:     "implementation file",
			filePath: "src/utils.py",
			expected: false,
		},
		{
			name:     "tests directory",
			filePath: "tests/conftest.py",
			expected: true,
		},
		{
			name:     "test directory singular",
			filePath: "test/helpers.py",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsTestFile(tt.filePath)
			assert.Equal(t, tt.expected, got, "IsTestFile(%q)", tt.filePath)
		})
	}
}

func TestApplyTestFilePenalty_Basic(t *testing.T) {
	// Given: results with test and implementation files
	results := []*SearchResult{
		{
			Chunk: &store.Chunk{FilePath: "internal/search/engine_test.go"},
			Score: 1.0,
		},
		{
			Chunk: &store.Chunk{FilePath: "internal/search/engine.go"},
			Score: 0.9,
		},
		{
			Chunk: &store.Chunk{FilePath: "internal/mcp/server_test.go"},
			Score: 0.8,
		},
	}

	// When: applying test file penalty
	penalized := ApplyTestFilePenalty(results)

	// Then: implementation file should be ranked first (test files penalized)
	assert.Equal(t, "internal/search/engine.go", penalized[0].Chunk.FilePath)
	assert.Equal(t, 0.9, penalized[0].Score) // Unchanged

	// Test files should be penalized
	assert.Equal(t, "internal/search/engine_test.go", penalized[1].Chunk.FilePath)
	assert.Equal(t, 0.5, penalized[1].Score) // 1.0 * 0.5

	assert.Equal(t, "internal/mcp/server_test.go", penalized[2].Chunk.FilePath)
	assert.Equal(t, 0.4, penalized[2].Score) // 0.8 * 0.5
}

func TestApplyTestFilePenalty_NoTestFiles(t *testing.T) {
	// Given: results with no test files
	results := []*SearchResult{
		{
			Chunk: &store.Chunk{FilePath: "internal/search/engine.go"},
			Score: 1.0,
		},
		{
			Chunk: &store.Chunk{FilePath: "internal/mcp/server.go"},
			Score: 0.9,
		},
	}

	// When: applying test file penalty
	penalized := ApplyTestFilePenalty(results)

	// Then: order and scores unchanged
	assert.Equal(t, "internal/search/engine.go", penalized[0].Chunk.FilePath)
	assert.Equal(t, 1.0, penalized[0].Score)
	assert.Equal(t, "internal/mcp/server.go", penalized[1].Chunk.FilePath)
	assert.Equal(t, 0.9, penalized[1].Score)
}

func TestApplyTestFilePenalty_EmptyResults(t *testing.T) {
	// Given: empty results
	results := []*SearchResult{}

	// When: applying test file penalty
	penalized := ApplyTestFilePenalty(results)

	// Then: empty results returned
	assert.Empty(t, penalized)
}

func TestApplyTestFilePenalty_NilChunk(t *testing.T) {
	// Given: results with nil chunk
	results := []*SearchResult{
		{Chunk: nil, Score: 1.0},
		{Chunk: &store.Chunk{FilePath: "engine.go"}, Score: 0.9},
	}

	// When: applying test file penalty
	penalized := ApplyTestFilePenalty(results)

	// Then: nil chunk result is handled gracefully
	assert.Len(t, penalized, 2)
	assert.Nil(t, penalized[0].Chunk)
	assert.Equal(t, 1.0, penalized[0].Score) // Unchanged (nil chunk not penalized)
}

func TestApplyTestFilePenalty_ReorderByScore(t *testing.T) {
	// Given: results where test file has highest score
	results := []*SearchResult{
		{
			Chunk: &store.Chunk{FilePath: "engine_test.go"},
			Score: 1.0, // Highest score but test file
		},
		{
			Chunk: &store.Chunk{FilePath: "engine.go"},
			Score: 0.6, // Lower score but implementation
		},
	}

	// When: applying test file penalty
	penalized := ApplyTestFilePenalty(results)

	// Then: implementation file should now be first
	// engine_test.go: 1.0 * 0.5 = 0.5
	// engine.go: 0.6 (unchanged)
	assert.Equal(t, "engine.go", penalized[0].Chunk.FilePath)
	assert.Equal(t, 0.6, penalized[0].Score)
	assert.Equal(t, "engine_test.go", penalized[1].Chunk.FilePath)
	assert.Equal(t, 0.5, penalized[1].Score)
}

// BenchmarkIsTestFile measures test file detection performance.
func BenchmarkIsTestFile(b *testing.B) {
	paths := []string{
		"internal/search/engine_test.go",
		"internal/search/engine.go",
		"src/components/Button.test.tsx",
		"tests/test_utils.py",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, p := range paths {
			_ = IsTestFile(p)
		}
	}
}

// =============================================================================
// BUG-066: Path Boost Tests
// =============================================================================

func TestIsImplementationPath(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		expected bool
	}{
		{
			name:     "internal package",
			filePath: "internal/search/engine.go",
			expected: true,
		},
		{
			name:     "nested internal",
			filePath: "pkg/internal/utils.go",
			expected: true,
		},
		{
			name:     "cmd package",
			filePath: "cmd/amanmcp/main.go",
			expected: false,
		},
		{
			name:     "root file",
			filePath: "main.go",
			expected: false,
		},
		{
			name:     "pkg file",
			filePath: "pkg/version/version.go",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsImplementationPath(tt.filePath)
			assert.Equal(t, tt.expected, got, "IsImplementationPath(%q)", tt.filePath)
		})
	}
}

func TestIsWrapperPath(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		expected bool
	}{
		{
			name:     "cmd package",
			filePath: "cmd/amanmcp/main.go",
			expected: true,
		},
		{
			name:     "nested cmd",
			filePath: "cmd/amanmcp/cmd/search.go",
			expected: true,
		},
		{
			name:     "internal package",
			filePath: "internal/search/engine.go",
			expected: false,
		},
		{
			name:     "pkg file",
			filePath: "pkg/version/version.go",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsWrapperPath(tt.filePath)
			assert.Equal(t, tt.expected, got, "IsWrapperPath(%q)", tt.filePath)
		})
	}
}

func TestApplyPathBoost_Basic(t *testing.T) {
	// Given: results with wrapper ranking above implementation
	results := []*SearchResult{
		{
			Chunk: &store.Chunk{FilePath: "cmd/amanmcp/cmd/search.go"},
			Score: 1.0, // CLI wrapper with high score
		},
		{
			Chunk: &store.Chunk{FilePath: "internal/search/engine.go"},
			Score: 0.8, // Implementation with lower score
		},
	}

	// When: applying path boost
	boosted := ApplyPathBoost(results)

	// Then: implementation should rank higher
	// cmd/search.go: 1.0 * 0.6 = 0.6 (penalized)
	// engine.go: 0.8 * 1.3 = 1.04 (boosted)
	assert.Equal(t, "internal/search/engine.go", boosted[0].Chunk.FilePath)
	assert.InDelta(t, 1.04, boosted[0].Score, 0.01)
	assert.Equal(t, "cmd/amanmcp/cmd/search.go", boosted[1].Chunk.FilePath)
	assert.InDelta(t, 0.6, boosted[1].Score, 0.01)
}

func TestApplyPathBoost_NoChange(t *testing.T) {
	// Given: results where both are internal
	results := []*SearchResult{
		{
			Chunk: &store.Chunk{FilePath: "internal/search/engine.go"},
			Score: 1.0,
		},
		{
			Chunk: &store.Chunk{FilePath: "internal/mcp/server.go"},
			Score: 0.9,
		},
	}

	// When: applying path boost
	boosted := ApplyPathBoost(results)

	// Then: relative order unchanged (both boosted equally)
	assert.Equal(t, "internal/search/engine.go", boosted[0].Chunk.FilePath)
	assert.InDelta(t, 1.3, boosted[0].Score, 0.01) // 1.0 * 1.3
	assert.Equal(t, "internal/mcp/server.go", boosted[1].Chunk.FilePath)
	assert.InDelta(t, 1.17, boosted[1].Score, 0.01) // 0.9 * 1.3
}

func TestApplyPathBoost_EmptyResults(t *testing.T) {
	results := []*SearchResult{}
	boosted := ApplyPathBoost(results)
	assert.Empty(t, boosted)
}

func TestApplyPathBoost_NilChunk(t *testing.T) {
	results := []*SearchResult{
		{Chunk: nil, Score: 1.0},
		{Chunk: &store.Chunk{FilePath: "internal/search/engine.go"}, Score: 0.9},
	}

	boosted := ApplyPathBoost(results)

	assert.Len(t, boosted, 2)
	// internal file should be first after boost
	assert.Equal(t, "internal/search/engine.go", boosted[0].Chunk.FilePath)
	assert.InDelta(t, 1.17, boosted[0].Score, 0.01)
}

func TestApplyPathBoost_RealScenario_BUG066(t *testing.T) {
	// Given: realistic BUG-066 scenario - wrapper outranks implementation
	// due to multi-query consensus boost
	results := []*SearchResult{
		{
			Chunk: &store.Chunk{FilePath: "cmd/amanmcp/cmd/search.go"},
			Score: 0.95, // High due to consensus boost (appears in all sub-queries)
		},
		{
			Chunk: &store.Chunk{FilePath: "internal/search/engine.go"},
			Score: 0.85, // Lower due to fewer sub-query hits
		},
		{
			Chunk: &store.Chunk{FilePath: "pkg/version/version.go"},
			Score: 0.5, // Neutral file
		},
	}

	// When: applying path boost
	boosted := ApplyPathBoost(results)

	// Then: engine.go should rank #1
	// engine.go: 0.85 * 1.3 = 1.105
	// search.go: 0.95 * 0.6 = 0.57
	// version.go: 0.5 (unchanged)
	assert.Equal(t, "internal/search/engine.go", boosted[0].Chunk.FilePath)
	assert.Equal(t, "cmd/amanmcp/cmd/search.go", boosted[1].Chunk.FilePath)
	assert.Equal(t, "pkg/version/version.go", boosted[2].Chunk.FilePath)
}

// BenchmarkApplyPathBoost measures path boost performance.
func BenchmarkApplyPathBoost(b *testing.B) {
	results := make([]*SearchResult, 20)
	for i := 0; i < 20; i++ {
		path := "internal/search/engine.go"
		if i%3 == 0 {
			path = "cmd/amanmcp/cmd/search.go"
		}
		results[i] = &SearchResult{
			Chunk: &store.Chunk{FilePath: path},
			Score: float64(20-i) / 20.0,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy := make([]*SearchResult, len(results))
		for j, r := range results {
			copy[j] = &SearchResult{
				Chunk: r.Chunk,
				Score: float64(20-j) / 20.0,
			}
		}
		_ = ApplyPathBoost(copy)
	}
}

// BenchmarkApplyTestFilePenalty measures penalty application performance.
func BenchmarkApplyTestFilePenalty(b *testing.B) {
	results := make([]*SearchResult, 20)
	for i := 0; i < 20; i++ {
		path := "internal/search/engine.go"
		if i%3 == 0 {
			path = "internal/search/engine_test.go"
		}
		results[i] = &SearchResult{
			Chunk: &store.Chunk{FilePath: path},
			Score: float64(20-i) / 20.0,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Create a copy to avoid modifying the original
		copy := make([]*SearchResult, len(results))
		for j, r := range results {
			copy[j] = &SearchResult{
				Chunk: r.Chunk,
				Score: float64(20-j) / 20.0,
			}
		}
		_ = ApplyTestFilePenalty(copy)
	}
}

// =============================================================================
// DEBT-028: ValidateOptions Tests
// =============================================================================

func TestValidateOptions_ValidFilters(t *testing.T) {
	tests := []struct {
		name   string
		filter string
	}{
		{"empty filter", ""},
		{"all filter", "all"},
		{"code filter", "code"},
		{"docs filter", "docs"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := SearchOptions{Filter: tc.filter}
			err := ValidateOptions(opts)
			assert.NoError(t, err)
		})
	}
}

func TestValidateOptions_UnknownFilter(t *testing.T) {
	// Unknown filters are accepted but treated as "all"
	opts := SearchOptions{Filter: "unknown"}
	err := ValidateOptions(opts)
	assert.NoError(t, err, "unknown filters should be accepted")
}

// =============================================================================
// DEBT-028: contentTypeFilter Tests
// =============================================================================

func TestContentTypeFilter_CodeFilter(t *testing.T) {
	filter := contentTypeFilter("code")

	tests := []struct {
		name        string
		contentType store.ContentType
		expected    bool
	}{
		{"code matches", store.ContentTypeCode, true},
		{"markdown no match", store.ContentTypeMarkdown, false},
		{"text no match", store.ContentTypeText, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := &SearchResult{
				Chunk: &store.Chunk{ContentType: tc.contentType},
			}
			assert.Equal(t, tc.expected, filter(result))
		})
	}
}

func TestContentTypeFilter_DocsFilter(t *testing.T) {
	filter := contentTypeFilter("docs")

	tests := []struct {
		name        string
		contentType store.ContentType
		expected    bool
	}{
		{"markdown matches", store.ContentTypeMarkdown, true},
		{"text matches", store.ContentTypeText, true},
		{"code no match", store.ContentTypeCode, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := &SearchResult{
				Chunk: &store.Chunk{ContentType: tc.contentType},
			}
			assert.Equal(t, tc.expected, filter(result))
		})
	}
}

func TestContentTypeFilter_DefaultFilter(t *testing.T) {
	// Default/unknown filter matches all
	filter := contentTypeFilter("all")

	result := &SearchResult{
		Chunk: &store.Chunk{ContentType: store.ContentTypeCode},
	}
	assert.True(t, filter(result), "default filter should match all")
}

func TestContentTypeFilter_NilChunk(t *testing.T) {
	filter := contentTypeFilter("code")
	result := &SearchResult{Chunk: nil}
	assert.False(t, filter(result), "nil chunk should return false")
}

// =============================================================================
// DEBT-028: languageFilter Tests
// =============================================================================

func TestLanguageFilter_Matches(t *testing.T) {
	filter := languageFilter("go")

	result := &SearchResult{
		Chunk: &store.Chunk{Language: "go"},
	}
	assert.True(t, filter(result))
}

func TestLanguageFilter_NoMatch(t *testing.T) {
	filter := languageFilter("go")

	result := &SearchResult{
		Chunk: &store.Chunk{Language: "python"},
	}
	assert.False(t, filter(result))
}

func TestLanguageFilter_NilChunk(t *testing.T) {
	filter := languageFilter("go")
	result := &SearchResult{Chunk: nil}
	assert.False(t, filter(result), "nil chunk should return false")
}

// =============================================================================
// DEBT-028: symbolTypeFilter Tests
// =============================================================================

func TestSymbolTypeFilter_Matches(t *testing.T) {
	filter := symbolTypeFilter("function")

	result := &SearchResult{
		Chunk: &store.Chunk{
			Symbols: []*store.Symbol{
				{Type: store.SymbolTypeFunction, Name: "TestFunc"},
			},
		},
	}
	assert.True(t, filter(result))
}

func TestSymbolTypeFilter_NoMatch(t *testing.T) {
	filter := symbolTypeFilter("function")

	result := &SearchResult{
		Chunk: &store.Chunk{
			Symbols: []*store.Symbol{
				{Type: store.SymbolTypeClass, Name: "TestClass"},
			},
		},
	}
	assert.False(t, filter(result))
}

func TestSymbolTypeFilter_EmptySymbols(t *testing.T) {
	filter := symbolTypeFilter("function")

	result := &SearchResult{
		Chunk: &store.Chunk{Symbols: []*store.Symbol{}},
	}
	assert.False(t, filter(result))
}

func TestSymbolTypeFilter_NilChunk(t *testing.T) {
	filter := symbolTypeFilter("function")
	result := &SearchResult{Chunk: nil}
	assert.False(t, filter(result))
}

func TestSymbolTypeFilter_MultipleSymbols(t *testing.T) {
	filter := symbolTypeFilter("function")

	result := &SearchResult{
		Chunk: &store.Chunk{
			Symbols: []*store.Symbol{
				{Type: store.SymbolTypeClass, Name: "TestClass"},
				{Type: store.SymbolTypeFunction, Name: "TestFunc"},
			},
		},
	}
	assert.True(t, filter(result), "should match if any symbol matches")
}
