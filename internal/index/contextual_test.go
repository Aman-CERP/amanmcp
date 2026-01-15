package index

import (
	"context"
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testConfigWithCodeChunks creates a test config with CodeChunks enabled.
// This maintains backward compatibility for existing tests.
func testConfigWithCodeChunks() *config.Config {
	cfg := config.NewConfig()
	cfg.Contextual.CodeChunks = true // Enable for backward compatibility in tests
	return cfg
}

// =============================================================================
// CR-1: Contextual Retrieval Tests
// =============================================================================

func TestEnrichChunkWithContext_PrependsContext(t *testing.T) {
	// Given: a chunk with content
	chunk := &store.Chunk{
		ID:         "test-chunk",
		FilePath:   "internal/search/engine.go",
		RawContent: "func (e *Engine) Search(ctx context.Context) {}",
		Content:    "package search\n\nfunc (e *Engine) Search(ctx context.Context) {}",
	}

	// When: enriching with context
	generatedContext := "This function implements hybrid search combining BM25 and semantic search."
	EnrichChunkWithContext(chunk, generatedContext)

	// Then: context is prepended
	assert.True(t, len(chunk.Content) > len(chunk.RawContent),
		"enriched content should be longer")
	assert.Contains(t, chunk.Content, generatedContext,
		"content should contain generated context")
	assert.Contains(t, chunk.Content, "func (e *Engine) Search",
		"content should contain original code")
	assert.Equal(t, generatedContext, chunk.Metadata["contextual_context"],
		"metadata should store context for debugging")
}

func TestEnrichChunkWithContext_EmptyContext(t *testing.T) {
	// Given: a chunk
	original := "original content"
	chunk := &store.Chunk{
		Content:    original,
		RawContent: original,
	}

	// When: enriching with empty context
	EnrichChunkWithContext(chunk, "")

	// Then: content is unchanged
	assert.Equal(t, original, chunk.Content,
		"content should be unchanged with empty context")
}

func TestEnrichChunkWithContext_NilChunk(t *testing.T) {
	// Should not panic
	EnrichChunkWithContext(nil, "some context")
}

func TestExtractDocumentContext_CodeFile(t *testing.T) {
	// Given: code chunks with imports
	chunks := []*store.Chunk{
		{
			FilePath:    "internal/search/engine.go",
			ContentType: store.ContentTypeCode,
			Context:     "package search\n\nimport (\n\t\"context\"\n)",
		},
	}

	// When: extracting document context
	ctx := ExtractDocumentContext(chunks)

	// Then: includes file path and imports
	assert.Contains(t, ctx, "File: internal/search/engine.go")
	assert.Contains(t, ctx, "package search")
	assert.Contains(t, ctx, "import")
}

func TestExtractDocumentContext_MarkdownFile(t *testing.T) {
	// Given: markdown chunks with section headers
	chunks := []*store.Chunk{
		{
			FilePath:    "README.md",
			ContentType: store.ContentTypeMarkdown,
			Symbols: []*store.Symbol{
				{Name: "Installation", Type: store.SymbolTypeFunction},
			},
		},
		{
			FilePath:    "README.md",
			ContentType: store.ContentTypeMarkdown,
			Symbols: []*store.Symbol{
				{Name: "Usage", Type: store.SymbolTypeFunction},
			},
		},
	}

	// When: extracting document context
	ctx := ExtractDocumentContext(chunks)

	// Then: includes file path and section headers
	assert.Contains(t, ctx, "Document: README.md")
	assert.Contains(t, ctx, "Installation")
	assert.Contains(t, ctx, "Usage")
}

func TestExtractDocumentContext_EmptyChunks(t *testing.T) {
	ctx := ExtractDocumentContext([]*store.Chunk{})
	assert.Equal(t, "", ctx)
}

func TestGroupChunksByFile(t *testing.T) {
	// Given: chunks from multiple files
	chunks := []*store.Chunk{
		{FilePath: "a.go", ID: "a1"},
		{FilePath: "b.go", ID: "b1"},
		{FilePath: "a.go", ID: "a2"},
		{FilePath: "c.go", ID: "c1"},
		{FilePath: "b.go", ID: "b2"},
	}

	// When: grouping by file
	grouped := GroupChunksByFile(chunks)

	// Then: chunks are grouped correctly
	assert.Len(t, grouped, 3)
	assert.Len(t, grouped["a.go"], 2)
	assert.Len(t, grouped["b.go"], 2)
	assert.Len(t, grouped["c.go"], 1)
}

// =============================================================================
// Pattern-Based Fallback Generator Tests
// =============================================================================

func TestPatternContextGenerator_GenerateContext_Function(t *testing.T) {
	// Given: a code chunk with function symbol
	chunk := &store.Chunk{
		FilePath:    "internal/search/engine.go",
		ContentType: store.ContentTypeCode,
		Language:    "go",
		Symbols: []*store.Symbol{
			{
				Name:       "Search",
				Type:       store.SymbolTypeFunction,
				DocComment: "Search executes a hybrid search combining BM25 and semantic search.",
			},
		},
	}

	// When: generating context with pattern generator
	gen := NewPatternContextGenerator(testConfigWithCodeChunks())
	ctx, err := gen.GenerateContext(context.Background(), chunk, "package search")

	// Then: context includes symbol info
	require.NoError(t, err)
	assert.Contains(t, ctx, "internal/search/engine.go")
	assert.Contains(t, ctx, "Search")
	assert.Contains(t, ctx, "function")
}

func TestPatternContextGenerator_GenerateContext_NoSymbols(t *testing.T) {
	// Given: a chunk without symbols
	chunk := &store.Chunk{
		FilePath:    "config.yaml",
		ContentType: store.ContentTypeText,
	}

	// When: generating context
	gen := NewPatternContextGenerator(testConfigWithCodeChunks())
	ctx, err := gen.GenerateContext(context.Background(), chunk, "")

	// Then: context includes file path only
	require.NoError(t, err)
	assert.Contains(t, ctx, "config.yaml")
}

func TestPatternContextGenerator_GenerateBatch(t *testing.T) {
	// Given: multiple chunks
	chunks := []*store.Chunk{
		{
			FilePath: "a.go",
			Symbols:  []*store.Symbol{{Name: "FuncA", Type: store.SymbolTypeFunction}},
		},
		{
			FilePath: "a.go",
			Symbols:  []*store.Symbol{{Name: "FuncB", Type: store.SymbolTypeFunction}},
		},
	}

	// When: generating context for batch
	gen := NewPatternContextGenerator(testConfigWithCodeChunks())
	contexts, err := gen.GenerateBatch(context.Background(), chunks, "package main")

	// Then: all chunks get context
	require.NoError(t, err)
	assert.Len(t, contexts, 2)
	assert.Contains(t, contexts[0], "FuncA")
	assert.Contains(t, contexts[1], "FuncB")
}

func TestPatternContextGenerator_Available(t *testing.T) {
	gen := NewPatternContextGenerator(testConfigWithCodeChunks())
	assert.True(t, gen.Available(context.Background()),
		"pattern generator should always be available")
}

func TestPatternContextGenerator_ModelName(t *testing.T) {
	gen := NewPatternContextGenerator(testConfigWithCodeChunks())
	assert.Equal(t, "pattern-based", gen.ModelName())
}

// =============================================================================
// Hybrid Generator Tests
// =============================================================================

func TestHybridContextGenerator_FallsBackOnLLMFailure(t *testing.T) {
	// Given: a hybrid generator where LLM is unavailable
	chunk := &store.Chunk{
		FilePath:    "test.go",
		ContentType: store.ContentTypeCode,
		Symbols:     []*store.Symbol{{Name: "TestFunc", Type: store.SymbolTypeFunction}},
	}

	// When: creating hybrid with nil LLM (simulating unavailable)
	hybrid := NewHybridContextGenerator(nil, testConfigWithCodeChunks())
	ctx, err := hybrid.GenerateContext(context.Background(), chunk, "")

	// Then: falls back to pattern generator
	require.NoError(t, err)
	assert.Contains(t, ctx, "TestFunc",
		"should fall back to pattern generator")
}

// =============================================================================
// RCA-015: CodeChunks Configuration Tests
// =============================================================================

func TestPatternContextGenerator_SkipsCodeWhenDisabled(t *testing.T) {
	// Given: a code chunk with CodeChunks=false (default)
	cfg := config.NewConfig() // CodeChunks defaults to false
	gen := NewPatternContextGenerator(cfg)

	chunk := &store.Chunk{
		FilePath:    "internal/store/hnsw.go",
		ContentType: store.ContentTypeCode,
		Language:    "go",
		Symbols:     []*store.Symbol{{Name: "NewHNSWStore", Type: store.SymbolTypeFunction}},
	}

	// When: generating context
	ctx, err := gen.GenerateContext(context.Background(), chunk, "package store")

	// Then: empty context is returned (no prefix for code)
	require.NoError(t, err)
	assert.Empty(t, ctx, "Code chunks should have no context when CodeChunks=false")
}

func TestPatternContextGenerator_GeneratesContextForDocsWhenCodeDisabled(t *testing.T) {
	// Given: a markdown chunk with CodeChunks=false (default)
	cfg := config.NewConfig() // CodeChunks defaults to false
	gen := NewPatternContextGenerator(cfg)

	chunk := &store.Chunk{
		FilePath:    "docs/architecture.md",
		ContentType: store.ContentTypeMarkdown,
	}

	// When: generating context
	ctx, err := gen.GenerateContext(context.Background(), chunk, "")

	// Then: context is generated for markdown (not affected by CodeChunks)
	require.NoError(t, err)
	assert.NotEmpty(t, ctx, "Markdown chunks should still get context")
	assert.Contains(t, ctx, "docs/architecture.md")
}

func TestHybridContextGenerator_SkipsCodeWhenDisabled(t *testing.T) {
	// Given: a code chunk with CodeChunks=false (default)
	cfg := config.NewConfig() // CodeChunks defaults to false
	hybrid := NewHybridContextGenerator(nil, cfg)

	chunk := &store.Chunk{
		FilePath:    "internal/search/engine.go",
		ContentType: store.ContentTypeCode,
		Language:    "go",
		Symbols:     []*store.Symbol{{Name: "Search", Type: store.SymbolTypeFunction}},
	}

	// When: generating context
	ctx, err := hybrid.GenerateContext(context.Background(), chunk, "package search")

	// Then: empty context is returned (no prefix for code)
	require.NoError(t, err)
	assert.Empty(t, ctx, "Code chunks should have no context when CodeChunks=false")
}

func TestPatternContextGenerator_GeneratesCodeContextWhenEnabled(t *testing.T) {
	// Given: a code chunk with CodeChunks=true
	cfg := config.NewConfig()
	cfg.Contextual.CodeChunks = true // Explicitly enable
	gen := NewPatternContextGenerator(cfg)

	chunk := &store.Chunk{
		FilePath:    "internal/store/hnsw.go",
		ContentType: store.ContentTypeCode,
		Language:    "go",
		Symbols:     []*store.Symbol{{Name: "NewHNSWStore", Type: store.SymbolTypeFunction}},
	}

	// When: generating context
	ctx, err := gen.GenerateContext(context.Background(), chunk, "package store")

	// Then: context is generated
	require.NoError(t, err)
	assert.NotEmpty(t, ctx, "Code chunks should get context when CodeChunks=true")
	assert.Contains(t, ctx, "NewHNSWStore")
}

// =============================================================================
// DEBT-028: Additional Coverage Tests for contextual.go
// =============================================================================

func TestDefaultContextGeneratorConfig(t *testing.T) {
	// When: getting default config
	cfg := DefaultContextGeneratorConfig()

	// Then: defaults are set correctly
	assert.Equal(t, "http://localhost:11434", cfg.OllamaHost, "default Ollama host")
	assert.Equal(t, "qwen3:0.6b", cfg.Model, "default model")
	assert.Equal(t, "5s", cfg.Timeout, "default timeout")
	assert.Equal(t, 8, cfg.BatchSize, "default batch size")
	assert.False(t, cfg.FallbackOnly, "default fallback only")
}

func TestExtractDocumentContext_DefaultContentType(t *testing.T) {
	// Given: chunks with unknown content type (defaults to text)
	chunks := []*store.Chunk{
		{
			FilePath:    "data.txt",
			ContentType: store.ContentTypeText, // Default/text type
		},
	}

	// When: extracting document context
	ctx := ExtractDocumentContext(chunks)

	// Then: uses default format
	assert.Contains(t, ctx, "File: data.txt")
}

func TestExtractDocumentContext_CodeFileNoContext(t *testing.T) {
	// Given: code chunk without Context field
	chunks := []*store.Chunk{
		{
			FilePath:    "main.go",
			ContentType: store.ContentTypeCode,
			Context:     "", // Empty context
		},
	}

	// When: extracting document context
	ctx := ExtractDocumentContext(chunks)

	// Then: only includes file path
	assert.Equal(t, "File: main.go", ctx)
}

func TestExtractDocumentContext_MarkdownManyHeaders(t *testing.T) {
	// Given: markdown with more than 5 section headers
	chunks := make([]*store.Chunk, 8)
	for i := 0; i < 8; i++ {
		chunks[i] = &store.Chunk{
			FilePath:    "DOCS.md",
			ContentType: store.ContentTypeMarkdown,
			Symbols: []*store.Symbol{
				{Name: "Section " + string(rune('A'+i)), Type: store.SymbolTypeFunction},
			},
		}
	}

	// When: extracting document context
	ctx := ExtractDocumentContext(chunks)

	// Then: truncates to first 5 headers + ellipsis
	assert.Contains(t, ctx, "Document: DOCS.md")
	assert.Contains(t, ctx, "Section A")
	assert.Contains(t, ctx, "Section D") // 5th item (index 4)
	assert.Contains(t, ctx, "...", "should contain ellipsis for truncation")
	assert.NotContains(t, ctx, "Section F", "should not contain headers beyond limit")
}

// =============================================================================
// DEBT-028: Additional Coverage Tests for contextual_pattern.go
// =============================================================================

func TestPatternContextGenerator_Close(t *testing.T) {
	gen := NewPatternContextGenerator(testConfigWithCodeChunks())

	// Close should return nil (no-op)
	err := gen.Close()
	assert.NoError(t, err, "Close should return nil")
}

func TestPatternContextGenerator_GenerateContext_NilChunk(t *testing.T) {
	gen := NewPatternContextGenerator(testConfigWithCodeChunks())

	// When: generating context for nil chunk
	ctx, err := gen.GenerateContext(context.Background(), nil, "")

	// Then: returns empty string, no error
	require.NoError(t, err)
	assert.Empty(t, ctx, "nil chunk should return empty context")
}

func TestPatternContextGenerator_GenerateContext_NilConfig(t *testing.T) {
	// Given: generator with nil config
	gen := NewPatternContextGenerator(nil)

	chunk := &store.Chunk{
		FilePath:    "test.go",
		ContentType: store.ContentTypeCode,
		Language:    "go",
		Symbols:     []*store.Symbol{{Name: "TestFunc", Type: store.SymbolTypeFunction}},
	}

	// When: generating context
	ctx, err := gen.GenerateContext(context.Background(), chunk, "")

	// Then: should still work (nil config treated as enabled)
	require.NoError(t, err)
	assert.Contains(t, ctx, "test.go")
}

func TestExtractFirstSentence_LongText(t *testing.T) {
	// Given: text longer than 100 characters without sentence end
	longText := "This is a very long piece of text that continues without any periods or newlines and keeps going on and on for quite some time"

	// When: extracting first sentence
	result := extractFirstSentence(longText)

	// Then: truncates to 100 chars + ellipsis
	assert.True(t, len(result) > 100, "should be over 100 chars with ellipsis")
	assert.True(t, len(result) <= 104, "should not be excessively long")
	assert.True(t, result[len(result)-3:] == "...", "should end with ellipsis")
}

func TestExtractFirstSentence_WithPeriod(t *testing.T) {
	text := "First sentence. Second sentence."
	result := extractFirstSentence(text)
	assert.Equal(t, "First sentence", result, "should extract first sentence without trailing period")
}

func TestExtractFirstSentence_WithNewline(t *testing.T) {
	text := "First line\nSecond line"
	result := extractFirstSentence(text)
	assert.Equal(t, "First line", result, "should extract up to newline")
}

func TestExtractFirstSentence_DocCommentPrefixes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"single line comment", "// This is a doc comment.", "This is a doc comment"},
		{"block comment start", "/* Block comment.", "Block comment"},
		{"block comment full", "/* Full block comment */", "Full block comment"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := extractFirstSentence(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestExtractFirstSentence_EmptyAndWhitespace(t *testing.T) {
	assert.Empty(t, extractFirstSentence(""))
	assert.Empty(t, extractFirstSentence("   "))
}

// =============================================================================
// DEBT-028: Hybrid Generator Additional Tests
// =============================================================================

// mockContextGenerator is a test implementation for hybrid generator tests.
type mockContextGenerator struct {
	availableFn       func(ctx context.Context) bool
	generateFn        func(ctx context.Context, chunk *store.Chunk, docContext string) (string, error)
	generateBatchFn   func(ctx context.Context, chunks []*store.Chunk, docContext string) ([]string, error)
	modelName         string
	closeFn           func() error
}

func (m *mockContextGenerator) Available(ctx context.Context) bool {
	if m.availableFn != nil {
		return m.availableFn(ctx)
	}
	return true
}

func (m *mockContextGenerator) GenerateContext(ctx context.Context, chunk *store.Chunk, docContext string) (string, error) {
	if m.generateFn != nil {
		return m.generateFn(ctx, chunk, docContext)
	}
	return "mock context", nil
}

func (m *mockContextGenerator) GenerateBatch(ctx context.Context, chunks []*store.Chunk, docContext string) ([]string, error) {
	if m.generateBatchFn != nil {
		return m.generateBatchFn(ctx, chunks, docContext)
	}
	results := make([]string, len(chunks))
	for i := range chunks {
		results[i] = "mock batch context"
	}
	return results, nil
}

func (m *mockContextGenerator) ModelName() string {
	if m.modelName != "" {
		return m.modelName
	}
	return "mock-model"
}

func (m *mockContextGenerator) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

func TestHybridContextGenerator_Available_WithLLM(t *testing.T) {
	// Given: hybrid with available LLM
	llm := &mockContextGenerator{
		availableFn: func(ctx context.Context) bool { return true },
	}
	hybrid := NewHybridContextGenerator(llm, testConfigWithCodeChunks())

	// Then: should be available
	assert.True(t, hybrid.Available(context.Background()))
}

func TestHybridContextGenerator_Available_LLMUnavailable(t *testing.T) {
	// Given: hybrid with unavailable LLM
	llm := &mockContextGenerator{
		availableFn: func(ctx context.Context) bool { return false },
	}
	hybrid := NewHybridContextGenerator(llm, testConfigWithCodeChunks())

	// Then: should still be available (pattern generator fallback)
	assert.True(t, hybrid.Available(context.Background()))
}

func TestHybridContextGenerator_ModelName_WithLLM(t *testing.T) {
	// Given: hybrid with LLM
	llm := &mockContextGenerator{modelName: "test-llm"}
	hybrid := NewHybridContextGenerator(llm, testConfigWithCodeChunks())

	// Then: model name includes LLM + pattern
	name := hybrid.ModelName()
	assert.Equal(t, "test-llm+pattern", name)
}

func TestHybridContextGenerator_ModelName_NoLLM(t *testing.T) {
	// Given: hybrid without LLM
	hybrid := NewHybridContextGenerator(nil, testConfigWithCodeChunks())

	// Then: model name is pattern-based
	name := hybrid.ModelName()
	assert.Equal(t, "pattern-based", name)
}

func TestHybridContextGenerator_Close_WithLLM(t *testing.T) {
	// Given: hybrid with LLM that tracks close calls
	closeCalled := false
	llm := &mockContextGenerator{
		closeFn: func() error {
			closeCalled = true
			return nil
		},
	}
	hybrid := NewHybridContextGenerator(llm, testConfigWithCodeChunks())

	// When: closing
	err := hybrid.Close()

	// Then: LLM.Close is called
	assert.NoError(t, err)
	assert.True(t, closeCalled, "LLM.Close should be called")
}

func TestHybridContextGenerator_Close_NoLLM(t *testing.T) {
	// Given: hybrid without LLM
	hybrid := NewHybridContextGenerator(nil, testConfigWithCodeChunks())

	// When: closing
	err := hybrid.Close()

	// Then: no error
	assert.NoError(t, err)
}

func TestHybridContextGenerator_GenerateContext_UsesLLM(t *testing.T) {
	// Given: hybrid with available LLM
	llm := &mockContextGenerator{
		availableFn: func(ctx context.Context) bool { return true },
		generateFn: func(ctx context.Context, chunk *store.Chunk, docContext string) (string, error) {
			return "LLM generated context", nil
		},
	}
	hybrid := NewHybridContextGenerator(llm, testConfigWithCodeChunks())

	chunk := &store.Chunk{
		FilePath:    "test.go",
		ContentType: store.ContentTypeCode,
		Symbols:     []*store.Symbol{{Name: "Test", Type: store.SymbolTypeFunction}},
	}

	// When: generating context
	ctx, err := hybrid.GenerateContext(context.Background(), chunk, "")

	// Then: uses LLM
	require.NoError(t, err)
	assert.Equal(t, "LLM generated context", ctx)
}

func TestHybridContextGenerator_GenerateContext_FallsBackOnLLMError(t *testing.T) {
	// Given: hybrid with LLM that returns error
	llm := &mockContextGenerator{
		availableFn: func(ctx context.Context) bool { return true },
		generateFn: func(ctx context.Context, chunk *store.Chunk, docContext string) (string, error) {
			return "", assert.AnError
		},
	}
	hybrid := NewHybridContextGenerator(llm, testConfigWithCodeChunks())

	chunk := &store.Chunk{
		FilePath:    "test.go",
		ContentType: store.ContentTypeCode,
		Symbols:     []*store.Symbol{{Name: "TestFunc", Type: store.SymbolTypeFunction}},
	}

	// When: generating context
	ctx, err := hybrid.GenerateContext(context.Background(), chunk, "")

	// Then: falls back to pattern generator
	require.NoError(t, err)
	assert.Contains(t, ctx, "test.go", "should fall back to pattern generator")
	assert.Contains(t, ctx, "TestFunc")
}

func TestHybridContextGenerator_GenerateBatch_UsesLLM(t *testing.T) {
	// Given: hybrid with available LLM
	llm := &mockContextGenerator{
		availableFn: func(ctx context.Context) bool { return true },
		generateBatchFn: func(ctx context.Context, chunks []*store.Chunk, docContext string) ([]string, error) {
			results := make([]string, len(chunks))
			for i := range chunks {
				results[i] = "LLM batch " + chunks[i].FilePath
			}
			return results, nil
		},
	}
	hybrid := NewHybridContextGenerator(llm, testConfigWithCodeChunks())

	chunks := []*store.Chunk{
		{FilePath: "a.go"},
		{FilePath: "b.go"},
	}

	// When: generating batch context
	contexts, err := hybrid.GenerateBatch(context.Background(), chunks, "")

	// Then: uses LLM
	require.NoError(t, err)
	assert.Len(t, contexts, 2)
	assert.Equal(t, "LLM batch a.go", contexts[0])
	assert.Equal(t, "LLM batch b.go", contexts[1])
}

func TestHybridContextGenerator_GenerateBatch_FallsBackOnLLMError(t *testing.T) {
	// Given: hybrid with LLM that returns error
	llm := &mockContextGenerator{
		availableFn: func(ctx context.Context) bool { return true },
		generateBatchFn: func(ctx context.Context, chunks []*store.Chunk, docContext string) ([]string, error) {
			return nil, assert.AnError
		},
	}
	hybrid := NewHybridContextGenerator(llm, testConfigWithCodeChunks())

	chunks := []*store.Chunk{
		{FilePath: "a.go", Symbols: []*store.Symbol{{Name: "A", Type: store.SymbolTypeFunction}}},
		{FilePath: "b.go", Symbols: []*store.Symbol{{Name: "B", Type: store.SymbolTypeFunction}}},
	}

	// When: generating batch context
	contexts, err := hybrid.GenerateBatch(context.Background(), chunks, "")

	// Then: falls back to pattern generator
	require.NoError(t, err)
	assert.Len(t, contexts, 2)
	assert.Contains(t, contexts[0], "a.go", "should fall back to pattern generator")
	assert.Contains(t, contexts[1], "b.go")
}
