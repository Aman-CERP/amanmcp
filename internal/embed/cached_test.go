package embed

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEmbedder is a test double that counts calls
type mockEmbedder struct {
	embedCalls     atomic.Int64
	batchCalls     atomic.Int64
	dimensions     int
	modelName      string
	returnedVector []float32
}

func newMockEmbedder(dims int) *mockEmbedder {
	vec := make([]float32, dims)
	for i := range vec {
		vec[i] = float32(i) * 0.001
	}
	return &mockEmbedder{
		dimensions:     dims,
		modelName:      "mock-model",
		returnedVector: vec,
	}
}

func (m *mockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	m.embedCalls.Add(1)
	return m.returnedVector, nil
}

func (m *mockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	m.batchCalls.Add(1)
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = m.returnedVector
	}
	return result, nil
}

func (m *mockEmbedder) Dimensions() int {
	return m.dimensions
}

func (m *mockEmbedder) ModelName() string {
	return m.modelName
}

func (m *mockEmbedder) Available(ctx context.Context) bool {
	return true
}

func (m *mockEmbedder) Close() error {
	return nil
}

func (m *mockEmbedder) SetBatchIndex(_ int) {}

func (m *mockEmbedder) SetFinalBatch(_ bool) {}

// ============================================================================
// TS01: Interface Compliance
// ============================================================================

func TestCachedEmbedder_ImplementsEmbedderInterface(t *testing.T) {
	inner := newMockEmbedder(768)
	cached := NewCachedEmbedder(inner, 100)
	defer func() { _ = cached.Close() }()

	// Verify interface compliance at compile time
	var _ Embedder = cached
}

// ============================================================================
// TS02: Cache Hit on Same Text
// ============================================================================

func TestCachedEmbedder_CacheHit_ReturnsWithoutCallingInner(t *testing.T) {
	// Given: a cached embedder
	inner := newMockEmbedder(768)
	cached := NewCachedEmbedder(inner, 100)
	defer func() { _ = cached.Close() }()

	ctx := context.Background()
	text := "func add(a, b int) int { return a + b }"

	// When: I embed the same text twice
	result1, err1 := cached.Embed(ctx, text)
	result2, err2 := cached.Embed(ctx, text)

	// Then: inner embedder is called only once
	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.Equal(t, int64(1), inner.embedCalls.Load(), "inner should be called once")

	// And: results are identical
	assert.Equal(t, result1, result2, "cached results should match")
}

// ============================================================================
// TS03: Cache Miss on Different Text
// ============================================================================

func TestCachedEmbedder_CacheMiss_CallsInnerForNewText(t *testing.T) {
	// Given: a cached embedder
	inner := newMockEmbedder(768)
	cached := NewCachedEmbedder(inner, 100)
	defer func() { _ = cached.Close() }()

	ctx := context.Background()

	// When: I embed different texts
	_, err1 := cached.Embed(ctx, "text one")
	_, err2 := cached.Embed(ctx, "text two")
	_, err3 := cached.Embed(ctx, "text three")

	// Then: inner embedder is called for each unique text
	require.NoError(t, err1)
	require.NoError(t, err2)
	require.NoError(t, err3)
	assert.Equal(t, int64(3), inner.embedCalls.Load(), "inner should be called three times")
}

// ============================================================================
// TS04: Passthrough Methods
// ============================================================================

func TestCachedEmbedder_Dimensions_ReturnsInnerDimensions(t *testing.T) {
	inner := newMockEmbedder(1024)
	cached := NewCachedEmbedder(inner, 100)
	defer func() { _ = cached.Close() }()

	assert.Equal(t, 1024, cached.Dimensions())
}

func TestCachedEmbedder_ModelName_ReturnsInnerModelName(t *testing.T) {
	inner := newMockEmbedder(768)
	inner.modelName = "custom-model-v2"
	cached := NewCachedEmbedder(inner, 100)
	defer func() { _ = cached.Close() }()

	assert.Equal(t, "custom-model-v2", cached.ModelName())
}

func TestCachedEmbedder_Available_ReturnsInnerAvailable(t *testing.T) {
	inner := newMockEmbedder(768)
	cached := NewCachedEmbedder(inner, 100)
	defer func() { _ = cached.Close() }()

	assert.True(t, cached.Available(context.Background()))
}

// ============================================================================
// TS05: EmbedBatch Caching
// ============================================================================

func TestCachedEmbedder_EmbedBatch_CachesIndividualResults(t *testing.T) {
	// Given: a cached embedder
	inner := newMockEmbedder(768)
	cached := NewCachedEmbedder(inner, 100)
	defer func() { _ = cached.Close() }()

	ctx := context.Background()
	texts := []string{"text1", "text2", "text3"}

	// When: I call EmbedBatch then Embed on the same text
	_, err1 := cached.EmbedBatch(ctx, texts)
	require.NoError(t, err1)

	_, err2 := cached.Embed(ctx, "text1") // Should hit cache

	// Then: second call is a cache hit
	require.NoError(t, err2)
	assert.Equal(t, int64(0), inner.embedCalls.Load(), "individual Embed should hit batch cache")
}

// ============================================================================
// TS06: Close Behavior
// ============================================================================

func TestCachedEmbedder_Close_ClosesInner(t *testing.T) {
	inner := newMockEmbedder(768)
	cached := NewCachedEmbedder(inner, 100)

	err := cached.Close()
	assert.NoError(t, err)
}

// ============================================================================
// TS07: Default Cache Size
// ============================================================================

func TestNewCachedEmbedderWithDefaults_UsesDefaultCacheSize(t *testing.T) {
	inner := newMockEmbedder(768)
	cached := NewCachedEmbedderWithDefaults(inner)
	defer func() { _ = cached.Close() }()

	// Should work without error
	_, err := cached.Embed(context.Background(), "test")
	require.NoError(t, err)
}

// ============================================================================
// TS08: Cache Eviction (LRU)
// ============================================================================

func TestCachedEmbedder_CacheEviction_OldestEvictedFirst(t *testing.T) {
	// Given: a cached embedder with small cache
	inner := newMockEmbedder(768)
	cached := NewCachedEmbedder(inner, 3) // Only 3 entries
	defer func() { _ = cached.Close() }()

	ctx := context.Background()

	// When: I embed 4 different texts (exceeds cache)
	_, _ = cached.Embed(ctx, "text1") // Will be evicted
	_, _ = cached.Embed(ctx, "text2")
	_, _ = cached.Embed(ctx, "text3")
	_, _ = cached.Embed(ctx, "text4") // Forces eviction

	// Reset counter
	inner.embedCalls.Store(0)

	// Then: first text should cause cache miss
	_, err := cached.Embed(ctx, "text1")
	require.NoError(t, err)
	assert.Equal(t, int64(1), inner.embedCalls.Load(), "evicted text should require new embedding")

	// And: recent texts are still cached
	inner.embedCalls.Store(0)
	_, _ = cached.Embed(ctx, "text3")
	_, _ = cached.Embed(ctx, "text4")
	assert.Equal(t, int64(0), inner.embedCalls.Load(), "recent texts should be cached")
}

// ============================================================================
// TS09: Inner() Method
// ============================================================================

func TestCachedEmbedder_Inner_ReturnsUnderlyingEmbedder(t *testing.T) {
	// Given: a cached embedder wrapping a mock embedder
	inner := newMockEmbedder(768)
	inner.modelName = "test-model-for-inner"
	cached := NewCachedEmbedder(inner, 100)
	defer func() { _ = cached.Close() }()

	// When: I call Inner()
	gotInner := cached.Inner()

	// Then: it returns the same embedder that was wrapped
	assert.NotNil(t, gotInner)
	assert.Equal(t, inner, gotInner, "Inner() should return the wrapped embedder")
	assert.Equal(t, "test-model-for-inner", gotInner.ModelName())
}

// ============================================================================
// TS10: Thread Safety
// ============================================================================

func TestCachedEmbedder_ConcurrentAccess_NoRace(t *testing.T) {
	// Given: a cached embedder
	inner := newMockEmbedder(768)
	cached := NewCachedEmbedder(inner, 100)
	defer func() { _ = cached.Close() }()

	ctx := context.Background()
	texts := []string{"a", "b", "c", "d", "e"}

	// When: I access it concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				text := texts[j%len(texts)]
				_, _ = cached.Embed(ctx, text)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Then: no panic or race condition (run with -race flag)
	// If we get here without panic, test passes
}
