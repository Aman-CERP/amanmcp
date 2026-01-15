package embed

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// TS01: Correct Dimensions (768)
// ============================================================================

func TestStaticEmbedder768_Embed_ReturnsCorrectDimensions(t *testing.T) {
	// Given: static768 embedder with 768 dimensions
	embedder := NewStaticEmbedder768()
	defer func() { _ = embedder.Close() }()

	// When: I embed "func main() {}"
	embedding, err := embedder.Embed(context.Background(), "func main() {}")

	// Then: a 768-dimension vector is returned
	require.NoError(t, err)
	assert.Len(t, embedding, Static768Dimensions)
	assert.Equal(t, 768, Static768Dimensions, "Static768Dimensions should be 768")
}

func TestStaticEmbedder768_Embed_VectorIsNormalized(t *testing.T) {
	// Given: static768 embedder
	embedder := NewStaticEmbedder768()
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

func TestStaticEmbedder768_Embed_IsDeterministic(t *testing.T) {
	// Given: static768 embedder
	embedder := NewStaticEmbedder768()
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

func TestStaticEmbedder768_Embed_DeterministicAcrossInstances(t *testing.T) {
	// Given: two separate embedder instances
	embedder1 := NewStaticEmbedder768()
	embedder2 := NewStaticEmbedder768()
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
// TS03: Semantic Similarity (same algorithm as StaticEmbedder)
// ============================================================================

func TestStaticEmbedder768_SimilarCode_HasHigherSimilarity(t *testing.T) {
	// Given: static768 embedder and code samples
	embedder := NewStaticEmbedder768()
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
// TS04: ModelName and Dimensions
// ============================================================================

func TestStaticEmbedder768_ModelName_ReturnsStatic768(t *testing.T) {
	embedder := NewStaticEmbedder768()
	defer func() { _ = embedder.Close() }()

	assert.Equal(t, "static768", embedder.ModelName())
}

func TestStaticEmbedder768_Dimensions_Returns768(t *testing.T) {
	embedder := NewStaticEmbedder768()
	defer func() { _ = embedder.Close() }()

	assert.Equal(t, 768, embedder.Dimensions())
}

// ============================================================================
// TS05: Empty Input
// ============================================================================

func TestStaticEmbedder768_Embed_EmptyInput_ReturnsZeroVector(t *testing.T) {
	// Given: static768 embedder
	embedder := NewStaticEmbedder768()
	defer func() { _ = embedder.Close() }()

	// When: I embed empty string
	embedding, err := embedder.Embed(context.Background(), "")

	// Then: a 768-dimension zero vector is returned
	require.NoError(t, err)
	assert.Len(t, embedding, Static768Dimensions)

	for i, v := range embedding {
		assert.Equal(t, float32(0), v, "element %d should be zero", i)
	}
}

func TestStaticEmbedder768_Embed_WhitespaceOnly_ReturnsZeroVector(t *testing.T) {
	// Given: static768 embedder
	embedder := NewStaticEmbedder768()
	defer func() { _ = embedder.Close() }()

	// When: I embed whitespace-only string
	embedding, err := embedder.Embed(context.Background(), "   \t\n  ")

	// Then: a zero vector is returned
	require.NoError(t, err)
	assert.Len(t, embedding, Static768Dimensions)

	for _, v := range embedding {
		assert.Equal(t, float32(0), v)
	}
}

// ============================================================================
// TS06: Interface Compliance
// ============================================================================

func TestStaticEmbedder768_ImplementsEmbedderInterface(t *testing.T) {
	embedder := NewStaticEmbedder768()
	defer func() { _ = embedder.Close() }()

	// Verify interface compliance at compile time
	var _ Embedder = embedder
}

// ============================================================================
// TS07: Batch Embedding
// ============================================================================

func TestStaticEmbedder768_EmbedBatch_ReturnsCorrectCount(t *testing.T) {
	// Given: static768 embedder
	embedder := NewStaticEmbedder768()
	defer func() { _ = embedder.Close() }()

	texts := []string{"func add()", "func sub()", "class User"}

	// When: I call EmbedBatch
	embeddings, err := embedder.EmbedBatch(context.Background(), texts)

	// Then: 3 embeddings are returned
	require.NoError(t, err)
	assert.Len(t, embeddings, 3)

	// And: each is 768 dimensions
	for i, emb := range embeddings {
		assert.Len(t, emb, Static768Dimensions, "embedding %d should have 768 dimensions", i)
	}
}

func TestStaticEmbedder768_EmbedBatch_EmptyList_ReturnsEmpty(t *testing.T) {
	// Given: static768 embedder
	embedder := NewStaticEmbedder768()
	defer func() { _ = embedder.Close() }()

	// When: I call EmbedBatch with empty list
	embeddings, err := embedder.EmbedBatch(context.Background(), []string{})

	// Then: empty result returned without error
	require.NoError(t, err)
	assert.Empty(t, embeddings)
}

func TestStaticEmbedder768_EmbedBatch_HandlesEmptyStringsInBatch(t *testing.T) {
	// Given: batch with empty strings mixed in
	embedder := NewStaticEmbedder768()
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
// TS08: Closed State
// ============================================================================

func TestStaticEmbedder768_Embed_AfterClose_ReturnsError(t *testing.T) {
	embedder := NewStaticEmbedder768()
	_ = embedder.Close()

	// When: I try to embed after close
	_, err := embedder.Embed(context.Background(), "test")

	// Then: error is returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestStaticEmbedder768_Available_AfterClose_ReturnsFalse(t *testing.T) {
	embedder := NewStaticEmbedder768()
	_ = embedder.Close()

	// When: I check Available after close
	available := embedder.Available(context.Background())

	// Then: returns false
	assert.False(t, available)
}

func TestStaticEmbedder768_Close_IsIdempotent(t *testing.T) {
	embedder := NewStaticEmbedder768()

	// Should not panic on multiple closes
	err1 := embedder.Close()
	err2 := embedder.Close()
	err3 := embedder.Close()

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.NoError(t, err3)
}

// ============================================================================
// TS09: Performance
// ============================================================================

func TestStaticEmbedder768_Performance(t *testing.T) {
	// Given: static768 embedder
	embedder := NewStaticEmbedder768()
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
// TS10: Available with Cancelled Context
// ============================================================================

func TestStaticEmbedder768_Available_TrueEvenWithCancelledContext(t *testing.T) {
	// Given: static768 embedder
	embedder := NewStaticEmbedder768()
	defer func() { _ = embedder.Close() }()

	// When: I check Available() with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	available := embedder.Available(ctx)

	// Then: result is still true (no external dependencies)
	assert.True(t, available, "static768 embedder should be available even with cancelled context")
}

// ============================================================================
// TS11: CamelCase and SnakeCase Tokenization
// ============================================================================

func TestStaticEmbedder768_CamelCase_Tokenization(t *testing.T) {
	// Given: static768 embedder
	embedder := NewStaticEmbedder768()
	defer func() { _ = embedder.Close() }()

	// When: I embed "getUserById" and "get user by id"
	camelEmb, _ := embedder.Embed(context.Background(), "getUserById")
	spaceEmb, _ := embedder.Embed(context.Background(), "get user by id")

	// Then: similarity is > 0.3 (reasonable match)
	similarity := cosineSimilarity(camelEmb, spaceEmb)
	assert.Greater(t, similarity, float64(0.3),
		"camelCase should tokenize similarly to space-separated (similarity: %.4f)", similarity)
}

func TestStaticEmbedder768_SnakeCase_Tokenization(t *testing.T) {
	// Given: static768 embedder
	embedder := NewStaticEmbedder768()
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
// TS12: Unicode and Long Text
// ============================================================================

func TestStaticEmbedder768_Embed_UnicodeText_NoError(t *testing.T) {
	embedder := NewStaticEmbedder768()
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
			assert.Len(t, embedding, Static768Dimensions)
		})
	}
}

func TestStaticEmbedder768_Embed_LongText_NoError(t *testing.T) {
	embedder := NewStaticEmbedder768()
	defer func() { _ = embedder.Close() }()

	// Generate long text
	longText := ""
	for i := 0; i < 10000; i++ {
		longText += "word "
	}

	embedding, err := embedder.Embed(context.Background(), longText)
	require.NoError(t, err)
	assert.Len(t, embedding, Static768Dimensions)
	assert.InDelta(t, 1.0, vectorMagnitude(embedding), 0.001)
}
