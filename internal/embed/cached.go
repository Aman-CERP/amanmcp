package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	lru "github.com/hashicorp/golang-lru/v2"
)

// Cache configuration constants.
const (
	// DefaultEmbeddingCacheSize is the default number of embeddings to cache.
	// At 768 dimensions * 4 bytes * 1000 entries ≈ 3MB memory.
	DefaultEmbeddingCacheSize = 1000
)

// CachedEmbedder wraps an Embedder with LRU caching to avoid redundant
// embedding computations. Same queries return cached results, saving
// 50-200ms per repeated query.
type CachedEmbedder struct {
	inner Embedder
	cache *lru.Cache[string, []float32]
}

// NewCachedEmbedder creates a cached embedder wrapping the given embedder.
// Cache size determines the number of unique query embeddings to keep in memory.
func NewCachedEmbedder(inner Embedder, cacheSize int) *CachedEmbedder {
	if cacheSize <= 0 {
		cacheSize = DefaultEmbeddingCacheSize
	}
	cache, _ := lru.New[string, []float32](cacheSize)
	return &CachedEmbedder{
		inner: inner,
		cache: cache,
	}
}

// NewCachedEmbedderWithDefaults creates a cached embedder with default settings.
func NewCachedEmbedderWithDefaults(inner Embedder) *CachedEmbedder {
	return NewCachedEmbedder(inner, DefaultEmbeddingCacheSize)
}

// cacheKey generates a unique key for the cache based on text and model.
// Using SHA256 ensures consistent key length and handles arbitrary text.
func (c *CachedEmbedder) cacheKey(text string) string {
	combined := text + "\x00" + c.inner.ModelName()
	hash := sha256.Sum256([]byte(combined))
	return hex.EncodeToString(hash[:])
}

// Embed returns cached embedding if available, otherwise computes and caches.
func (c *CachedEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	key := c.cacheKey(text)

	// Check cache first
	if vec, ok := c.cache.Get(key); ok {
		return vec, nil
	}

	// Cache miss: compute embedding
	vec, err := c.inner.Embed(ctx, text)
	if err != nil {
		return nil, err
	}

	// Store in cache
	c.cache.Add(key, vec)
	return vec, nil
}

// EmbedBatch generates embeddings for multiple texts, caching each result.
// Individual texts are checked/cached separately for maximum cache reuse.
func (c *CachedEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	results := make([][]float32, len(texts))
	uncachedIndices := make([]int, 0, len(texts))
	uncachedTexts := make([]string, 0, len(texts))

	// First pass: check cache for each text
	for i, text := range texts {
		key := c.cacheKey(text)
		if vec, ok := c.cache.Get(key); ok {
			results[i] = vec
		} else {
			uncachedIndices = append(uncachedIndices, i)
			uncachedTexts = append(uncachedTexts, text)
		}
	}

	// If all cached, we're done
	if len(uncachedTexts) == 0 {
		return results, nil
	}

	// Batch embed uncached texts
	newEmbeddings, err := c.inner.EmbedBatch(ctx, uncachedTexts)
	if err != nil {
		return nil, err
	}

	// Store results and update cache
	for j, idx := range uncachedIndices {
		results[idx] = newEmbeddings[j]
		key := c.cacheKey(texts[idx])
		c.cache.Add(key, newEmbeddings[j])
	}

	return results, nil
}

// Dimensions returns the embedding dimension (passthrough to inner).
func (c *CachedEmbedder) Dimensions() int {
	return c.inner.Dimensions()
}

// ModelName returns the model identifier (passthrough to inner).
func (c *CachedEmbedder) ModelName() string {
	return c.inner.ModelName()
}

// Available checks if the embedder is ready (passthrough to inner).
func (c *CachedEmbedder) Available(ctx context.Context) bool {
	return c.inner.Available(ctx)
}

// Close releases resources and closes the inner embedder.
func (c *CachedEmbedder) Close() error {
	return c.inner.Close()
}

// Inner returns the underlying embedder.
// This allows callers to access embedder-specific features like progress callbacks
// that are not part of the Embedder interface.
func (c *CachedEmbedder) Inner() Embedder {
	return c.inner
}

// SetBatchIndex passes through to the inner embedder for thermal timeout progression.
func (c *CachedEmbedder) SetBatchIndex(idx int) {
	c.inner.SetBatchIndex(idx)
}

// SetFinalBatch passes through to the inner embedder for final batch timeout boost.
func (c *CachedEmbedder) SetFinalBatch(isFinal bool) {
	c.inner.SetFinalBatch(isFinal)
}
