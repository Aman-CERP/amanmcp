package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TS-TOK-01: Basic tokenization - split on whitespace and delimiters
func TestTokenizeCode_SplitsOnWhitespace(t *testing.T) {
	// Given: text with whitespace
	text := "hello world"

	// When: tokenizing
	tokens := TokenizeCode(text)

	// Then: splits into separate tokens
	require.Len(t, tokens, 2)
	assert.Equal(t, "hello", tokens[0])
	assert.Equal(t, "world", tokens[1])
}

func TestTokenizeCode_SplitsOnDelimiters(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect []string
	}{
		{
			name:   "parentheses",
			input:  "func(arg)",
			expect: []string{"func", "arg"},
		},
		{
			name:   "brackets",
			input:  "array[index]",
			expect: []string{"array", "index"},
		},
		{
			name:   "dots",
			input:  "object.method",
			expect: []string{"object", "method"},
		},
		{
			name:   "mixed delimiters",
			input:  "foo.bar(baz, qux)",
			expect: []string{"foo", "bar", "baz", "qux"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := TokenizeCode(tt.input)
			assert.Equal(t, tt.expect, tokens)
		})
	}
}

// TS-TOK-02: CamelCase splitting
func TestTokenizeCode_SplitsCamelCase(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect []string
	}{
		{
			name:   "simple camelCase",
			input:  "getUserById",
			expect: []string{"get", "user", "by", "id"},
		},
		{
			name:   "PascalCase",
			input:  "UserAuthManager",
			expect: []string{"user", "auth", "manager"},
		},
		{
			name:   "with acronym",
			input:  "parseHTTPRequest",
			expect: []string{"parse", "http", "request"},
		},
		{
			name:   "acronym at start",
			input:  "HTTPHandler",
			expect: []string{"http", "handler"},
		},
		{
			name:   "single word",
			input:  "hello",
			expect: []string{"hello"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := TokenizeCode(tt.input)
			assert.Equal(t, tt.expect, tokens)
		})
	}
}

// TS-TOK-03: snake_case splitting
func TestTokenizeCode_SplitsSnakeCase(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect []string
	}{
		{
			name:   "simple snake_case",
			input:  "get_user_by_id",
			expect: []string{"get", "user", "by", "id"},
		},
		{
			name:   "double underscore",
			input:  "foo__bar",
			expect: []string{"foo", "bar"},
		},
		{
			name:   "leading underscore",
			input:  "_private_method",
			expect: []string{"private", "method"},
		},
		{
			name:   "mixed snake and camel",
			input:  "get_UserById",
			expect: []string{"get", "user", "by", "id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := TokenizeCode(tt.input)
			assert.Equal(t, tt.expect, tokens)
		})
	}
}

// TS-TOK-04: Filter short tokens
func TestTokenizeCode_FiltersShortTokens(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect []string
	}{
		{
			name:   "filters single char",
			input:  "a getUserById b",
			expect: []string{"get", "user", "by", "id"}, // "a" and "b" filtered
		},
		{
			name:   "keeps 2+ char tokens",
			input:  "go is ok",
			expect: []string{"go", "is", "ok"},
		},
		{
			name:   "handles numbers",
			input:  "item1 item2",
			expect: []string{"item1", "item2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := TokenizeCode(tt.input)
			assert.Equal(t, tt.expect, tokens)
		})
	}
}

// Test splitCamelCase helper directly
func TestSplitCamelCase(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect []string
	}{
		{
			name:   "empty string",
			input:  "",
			expect: []string{}, // Returns empty slice, not nil (DEBT-012)
		},
		{
			name:   "all lowercase",
			input:  "hello",
			expect: []string{"hello"},
		},
		{
			name:   "camelCase",
			input:  "camelCase",
			expect: []string{"camel", "Case"},
		},
		{
			name:   "PascalCase",
			input:  "PascalCase",
			expect: []string{"Pascal", "Case"},
		},
		{
			name:   "multiple words",
			input:  "getUserById",
			expect: []string{"get", "User", "By", "Id"},
		},
		{
			name:   "acronym in middle",
			input:  "parseHTTPRequest",
			expect: []string{"parse", "HTTP", "Request"},
		},
		{
			name:   "acronym at start",
			input:  "HTTPHandler",
			expect: []string{"HTTP", "Handler"},
		},
		{
			name:   "all caps",
			input:  "HTTP",
			expect: []string{"HTTP"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SplitCamelCase(tt.input)
			assert.Equal(t, tt.expect, result)
		})
	}
}

// Test splitCodeToken helper directly
func TestSplitCodeToken(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect []string
	}{
		{
			name:   "simple word",
			input:  "hello",
			expect: []string{"hello"},
		},
		{
			name:   "snake_case",
			input:  "get_user",
			expect: []string{"get", "user"},
		},
		{
			name:   "camelCase",
			input:  "getUser",
			expect: []string{"get", "User"},
		},
		{
			name:   "mixed",
			input:  "get_UserById",
			expect: []string{"get", "User", "By", "Id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SplitCodeToken(tt.input)
			assert.Equal(t, tt.expect, result)
		})
	}
}

// Test stop word filtering
func TestFilterStopWords(t *testing.T) {
	// Given: tokens including stop words
	tokens := []string{"func", "getUserById", "return", "data", "user", "name"}
	stopWords := map[string]struct{}{
		"func": {}, "return": {}, "data": {},
	}

	// When: filtering
	result := FilterStopWords(tokens, stopWords)

	// Then: stop words are removed
	assert.Equal(t, []string{"getUserById", "user", "name"}, result)
}

// Benchmark tokenization
func BenchmarkTokenizeCode(b *testing.B) {
	input := "func getUserById(ctx context.Context, id string) (*User, error)"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		TokenizeCode(input)
	}
}
