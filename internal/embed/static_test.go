package embed

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// TS01: Basic Embedding
// ============================================================================

func TestStaticEmbedder_Embed_ReturnsCorrectDimensions(t *testing.T) {
	// Given: static embedder with 256 dimensions
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	// When: I embed "func main() {}"
	embedding, err := embedder.Embed(context.Background(), "func main() {}")

	// Then: a 256-dimension vector is returned
	require.NoError(t, err)
	assert.Len(t, embedding, StaticDimensions)
}

func TestStaticEmbedder_Embed_VectorIsNormalized(t *testing.T) {
	// Given: static embedder
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	// When: I embed text
	embedding, err := embedder.Embed(context.Background(), "func main() {}")
	require.NoError(t, err)

	// Then: vector magnitude is ~1.0 (normalized)
	magnitude := vectorMagnitude(embedding)
	assert.InDelta(t, 1.0, magnitude, 0.001, "vector should be normalized to unit length")
}

// ============================================================================
// TS02: Deterministic Output
// ============================================================================

func TestStaticEmbedder_Embed_IsDeterministic(t *testing.T) {
	// Given: static embedder
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	text := "func add(a, b int) int { return a + b }"

	// When: I embed same text twice
	emb1, err1 := embedder.Embed(context.Background(), text)
	emb2, err2 := embedder.Embed(context.Background(), text)

	// Then: identical vectors are returned
	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.Equal(t, emb1, emb2, "same text should produce identical vectors")
}

func TestStaticEmbedder_Embed_DeterministicAcrossInstances(t *testing.T) {
	// Given: two separate embedder instances
	embedder1 := NewStaticEmbedder()
	embedder2 := NewStaticEmbedder()
	defer func() { _ = embedder1.Close() }()
	defer func() { _ = embedder2.Close() }()

	text := "func getUserById(id string) (*User, error)"

	// When: I embed same text with different instances
	emb1, _ := embedder1.Embed(context.Background(), text)
	emb2, _ := embedder2.Embed(context.Background(), text)

	// Then: identical vectors are returned
	assert.Equal(t, emb1, emb2, "same text should produce identical vectors across instances")
}

// ============================================================================
// TS03: Different Texts Differ
// ============================================================================

func TestStaticEmbedder_Embed_DifferentTextsProduceDifferentVectors(t *testing.T) {
	// Given: static embedder
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	// When: I embed "func add()" and "class Database"
	emb1, _ := embedder.Embed(context.Background(), "func add()")
	emb2, _ := embedder.Embed(context.Background(), "class Database")

	// Then: different vectors are returned
	assert.NotEqual(t, emb1, emb2, "different texts should produce different vectors")
}

// ============================================================================
// TS04: Empty Input
// ============================================================================

func TestStaticEmbedder_Embed_EmptyInput_ReturnsZeroVector(t *testing.T) {
	// Given: static embedder
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	// When: I embed empty string
	embedding, err := embedder.Embed(context.Background(), "")

	// Then: a 256-dimension zero vector is returned
	require.NoError(t, err)
	assert.Len(t, embedding, StaticDimensions)

	for i, v := range embedding {
		assert.Equal(t, float32(0), v, "element %d should be zero", i)
	}
}

func TestStaticEmbedder_Embed_WhitespaceOnly_ReturnsZeroVector(t *testing.T) {
	// Given: static embedder
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	// When: I embed whitespace-only string
	embedding, err := embedder.Embed(context.Background(), "   \t\n  ")

	// Then: a zero vector is returned
	require.NoError(t, err)
	assert.Len(t, embedding, StaticDimensions)

	for _, v := range embedding {
		assert.Equal(t, float32(0), v)
	}
}

// ============================================================================
// TS05: Similar Code Has Higher Similarity
// ============================================================================

func TestStaticEmbedder_SimilarCode_HasHigherSimilarity(t *testing.T) {
	// Given: static embedder and code samples
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	add := "func add(a, b int) int { return a + b }"
	sum := "func sum(x, y int) int { return x + y }"
	repository := "class UserRepository { findById() }"

	// When: I compute embeddings
	addEmb, _ := embedder.Embed(context.Background(), add)
	sumEmb, _ := embedder.Embed(context.Background(), sum)
	repoEmb, _ := embedder.Embed(context.Background(), repository)

	// Then: add/sum similarity > add/repository similarity
	addSumSim := cosineSimilarity(addEmb, sumEmb)
	addRepoSim := cosineSimilarity(addEmb, repoEmb)

	assert.Greater(t, addSumSim, addRepoSim,
		"similar code should have higher similarity (add/sum: %.4f) than different code (add/repo: %.4f)",
		addSumSim, addRepoSim)
}

// ============================================================================
// TS06: CamelCase Tokenization
// ============================================================================

func TestStaticEmbedder_CamelCase_Tokenization(t *testing.T) {
	// Given: static embedder
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	// When: I embed "getUserById" and "get user by id"
	camelEmb, _ := embedder.Embed(context.Background(), "getUserById")
	spaceEmb, _ := embedder.Embed(context.Background(), "get user by id")

	// Then: similarity is > 0.3 (reasonable match)
	similarity := cosineSimilarity(camelEmb, spaceEmb)
	assert.Greater(t, similarity, float64(0.3),
		"camelCase should tokenize similarly to space-separated (similarity: %.4f)", similarity)
}

// ============================================================================
// TS07: snake_case Tokenization
// ============================================================================

func TestStaticEmbedder_SnakeCase_Tokenization(t *testing.T) {
	// Given: static embedder
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	// When: I embed "get_user_by_id" and "get user by id"
	snakeEmb, _ := embedder.Embed(context.Background(), "get_user_by_id")
	spaceEmb, _ := embedder.Embed(context.Background(), "get user by id")

	// Then: similarity is > 0.3 (reasonable match)
	similarity := cosineSimilarity(snakeEmb, spaceEmb)
	assert.Greater(t, similarity, float64(0.3),
		"snake_case should tokenize similarly to space-separated (similarity: %.4f)", similarity)
}

// ============================================================================
// TS08: Always Available
// ============================================================================

func TestStaticEmbedder_Available_AlwaysTrue(t *testing.T) {
	// Given: static embedder
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	// When: I check Available()
	available := embedder.Available(context.Background())

	// Then: result is always true
	assert.True(t, available, "static embedder should always be available")
}

func TestStaticEmbedder_Available_TrueEvenWithCancelledContext(t *testing.T) {
	// Given: static embedder
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	// When: I check Available() with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	available := embedder.Available(ctx)

	// Then: result is still true (no external dependencies)
	assert.True(t, available, "static embedder should be available even with cancelled context")
}

// ============================================================================
// TS09: Performance
// ============================================================================

func TestStaticEmbedder_Performance(t *testing.T) {
	// Given: static embedder
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	texts := make([]string, 1000)
	for i := range texts {
		texts[i] = "func test" + string(rune('A'+i%26)) + "() { return i + 1 }"
	}

	// When: I embed 1000 texts
	start := time.Now()
	for _, text := range texts {
		_, err := embedder.Embed(context.Background(), text)
		require.NoError(t, err)
	}
	elapsed := time.Since(start)

	// Then: total time is < 1 second (< 1ms each)
	assert.Less(t, elapsed, 1*time.Second,
		"embedding 1000 texts should take < 1s (took %v)", elapsed)
}

// ============================================================================
// Interface Compliance
// ============================================================================

func TestStaticEmbedder_ImplementsEmbedderInterface(t *testing.T) {
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	// Verify interface compliance at compile time
	var _ Embedder = embedder
}

func TestStaticEmbedder_Dimensions_Returns256(t *testing.T) {
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	assert.Equal(t, StaticDimensions, embedder.Dimensions())
}

func TestStaticEmbedder_ModelName_ReturnsStatic(t *testing.T) {
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	assert.Equal(t, "static", embedder.ModelName())
}

// ============================================================================
// Batch Embedding
// ============================================================================

func TestStaticEmbedder_EmbedBatch_ReturnsCorrectCount(t *testing.T) {
	// Given: static embedder
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	texts := []string{"func add()", "func sub()", "class User"}

	// When: I call EmbedBatch
	embeddings, err := embedder.EmbedBatch(context.Background(), texts)

	// Then: 3 embeddings are returned
	require.NoError(t, err)
	assert.Len(t, embeddings, 3)

	// And: each is 256 dimensions
	for i, emb := range embeddings {
		assert.Len(t, emb, StaticDimensions, "embedding %d should have correct dimensions", i)
	}
}

func TestStaticEmbedder_EmbedBatch_EmptyList_ReturnsEmpty(t *testing.T) {
	// Given: static embedder
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	// When: I call EmbedBatch with empty list
	embeddings, err := embedder.EmbedBatch(context.Background(), []string{})

	// Then: empty result returned without error
	require.NoError(t, err)
	assert.Empty(t, embeddings)
}

func TestStaticEmbedder_EmbedBatch_HandlesEmptyStringsInBatch(t *testing.T) {
	// Given: batch with empty strings mixed in
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	texts := []string{
		"func add(a, b int) int { return a + b }",
		"", // Empty string
		"func multiply(a, b int) int { return a * b }",
	}

	// When: I call EmbedBatch
	embeddings, err := embedder.EmbedBatch(context.Background(), texts)

	// Then: all embeddings returned
	require.NoError(t, err)
	assert.Len(t, embeddings, 3)

	// And: empty string produces zero vector
	for _, v := range embeddings[1] {
		assert.Equal(t, float32(0), v)
	}
}

// ============================================================================
// Edge Cases
// ============================================================================

func TestStaticEmbedder_Close_IsIdempotent(t *testing.T) {
	embedder := NewStaticEmbedder()

	// Should not panic on multiple closes
	err1 := embedder.Close()
	err2 := embedder.Close()
	err3 := embedder.Close()

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.NoError(t, err3)
}

func TestStaticEmbedder_Embed_AfterClose_ReturnsError(t *testing.T) {
	embedder := NewStaticEmbedder()
	_ = embedder.Close()

	// When: I try to embed after close
	_, err := embedder.Embed(context.Background(), "test")

	// Then: error is returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestStaticEmbedder_Available_AfterClose_ReturnsFalse(t *testing.T) {
	embedder := NewStaticEmbedder()
	_ = embedder.Close()

	// When: I check Available after close
	available := embedder.Available(context.Background())

	// Then: returns false
	assert.False(t, available)
}

// ============================================================================
// Tokenization Tests
// ============================================================================

func TestStaticEmbedder_Tokenize_CamelCase(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
	}{
		{
			name:     "basic camelCase",
			input:    "getUserById",
			contains: []string{"get", "user", "id"},
		},
		{
			name:     "acronym at start",
			input:    "HTTPRequest",
			contains: []string{"http", "request"},
		},
		{
			name:     "acronym in middle",
			input:    "parseJSONData",
			contains: []string{"parse", "json", "data"},
		},
	}

	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Embed the camelCase and the expected tokens
			camelEmb, _ := embedder.Embed(context.Background(), tt.input)
			tokensEmb, _ := embedder.Embed(context.Background(), joinStrings(tt.contains, " "))

			// They should have reasonable similarity
			similarity := cosineSimilarity(camelEmb, tokensEmb)
			assert.Greater(t, similarity, float64(0.2),
				"camelCase '%s' should match tokens (similarity: %.4f)", tt.input, similarity)
		})
	}
}

func TestStaticEmbedder_Tokenize_SnakeCase(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
	}{
		{
			name:     "basic snake_case",
			input:    "get_user_by_id",
			contains: []string{"get", "user", "id"},
		},
		{
			name:     "uppercase snake_case",
			input:    "MAX_BUFFER_SIZE",
			contains: []string{"max", "buffer", "size"},
		},
	}

	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Embed the snake_case and the expected tokens
			snakeEmb, _ := embedder.Embed(context.Background(), tt.input)
			tokensEmb, _ := embedder.Embed(context.Background(), joinStrings(tt.contains, " "))

			// They should have reasonable similarity
			similarity := cosineSimilarity(snakeEmb, tokensEmb)
			assert.Greater(t, similarity, float64(0.2),
				"snake_case '%s' should match tokens (similarity: %.4f)", tt.input, similarity)
		})
	}
}

func TestStaticEmbedder_StopWordFiltering(t *testing.T) {
	// Given: static embedder
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	// When: I embed text with and without stop words
	withStopWords := "func return int string bool void"
	withoutStopWords := "calculate process validate"

	embWith, _ := embedder.Embed(context.Background(), withStopWords)
	embWithout, _ := embedder.Embed(context.Background(), withoutStopWords)

	// Then: stop words text should produce different (sparser) embedding
	// The vectors should be quite different
	similarity := cosineSimilarity(embWith, embWithout)
	assert.Less(t, similarity, float64(0.5),
		"stop words should be filtered, making vectors different (similarity: %.4f)", similarity)
}

// ============================================================================
// Unicode and Special Characters
// ============================================================================

func TestStaticEmbedder_Embed_UnicodeText_NoError(t *testing.T) {
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	// Unicode text should not cause panic
	texts := []string{
		"func 日本語() {}",
		"// Комментарий на русском",
		"const emoji = '🚀'",
	}

	for _, text := range texts {
		t.Run(text, func(t *testing.T) {
			embedding, err := embedder.Embed(context.Background(), text)
			require.NoError(t, err)
			assert.Len(t, embedding, StaticDimensions)
		})
	}
}

func TestStaticEmbedder_Embed_LongText_NoError(t *testing.T) {
	embedder := NewStaticEmbedder()
	defer func() { _ = embedder.Close() }()

	// Generate long text
	longText := ""
	for i := 0; i < 10000; i++ {
		longText += "word "
	}

	embedding, err := embedder.Embed(context.Background(), longText)
	require.NoError(t, err)
	assert.Len(t, embedding, StaticDimensions)
	assert.InDelta(t, 1.0, vectorMagnitude(embedding), 0.001)
}

// ============================================================================
// Helper Functions
// ============================================================================

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
