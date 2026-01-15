package embed

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Static768Dimensions is the embedding dimension for dimension-compatible static embedder.
// This matches HugotEmbedder (768 dims) for seamless fallback without re-indexing.
const Static768Dimensions = 768

// StaticEmbedder768 generates 768-dimensional embeddings using a hash-based approach.
// This provides dimension compatibility with HugotEmbedder for seamless fallback.
// Uses the same algorithm as StaticEmbedder but with 768 dimensions instead of 256.
type StaticEmbedder768 struct {
	mu     sync.RWMutex
	closed bool
}

// NewStaticEmbedder768 creates a new dimension-compatible static embedder.
func NewStaticEmbedder768() *StaticEmbedder768 {
	return &StaticEmbedder768{}
}

// Embed generates embedding for a single text.
func (e *StaticEmbedder768) Embed(ctx context.Context, text string) ([]float32, error) {
	e.mu.RLock()
	if e.closed {
		e.mu.RUnlock()
		return nil, fmt.Errorf("embedder is closed")
	}
	e.mu.RUnlock()

	// Handle empty/whitespace input
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return make([]float32, Static768Dimensions), nil
	}

	// Generate vector
	vector := e.generateVector(trimmed)

	// Normalize
	return normalizeVector(vector), nil
}

// generateVector creates a hash-based vector from text.
// Uses the same algorithm as StaticEmbedder but with 768 dimensions.
func (e *StaticEmbedder768) generateVector(text string) []float32 {
	vector := make([]float32, Static768Dimensions)

	// Step 1: Tokenize (reuse existing code-aware tokenization)
	tokens := tokenize(text)

	// Step 2: Filter stop words
	tokens = filterStopWords(tokens)

	// Step 3: Add tokens with weight 0.7
	for _, token := range tokens {
		index := hashToIndex(token, Static768Dimensions)
		vector[index] += tokenWeight
	}

	// Step 4: Extract n-grams and add with weight 0.3
	normalized := normalizeForNgrams(text)
	ngrams := extractNgrams(normalized, ngramSize)
	for _, ngram := range ngrams {
		index := hashToIndex(ngram, Static768Dimensions)
		vector[index] += ngramWeight
	}

	return vector
}

// EmbedBatch generates embeddings for multiple texts.
func (e *StaticEmbedder768) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	e.mu.RLock()
	if e.closed {
		e.mu.RUnlock()
		return nil, fmt.Errorf("embedder is closed")
	}
	e.mu.RUnlock()

	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	results := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := e.Embed(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("failed to embed text %d: %w", i, err)
		}
		results[i] = emb
	}

	return results, nil
}

// Dimensions returns the embedding dimension.
func (e *StaticEmbedder768) Dimensions() int {
	return Static768Dimensions
}

// ModelName returns the model identifier.
func (e *StaticEmbedder768) ModelName() string {
	return "static768"
}

// Available checks if the embedder is ready (always true for static).
func (e *StaticEmbedder768) Available(_ context.Context) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return !e.closed
}

// Close releases resources.
func (e *StaticEmbedder768) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	return nil
}

// SetBatchIndex is a no-op for static embedder (no thermal management needed).
func (e *StaticEmbedder768) SetBatchIndex(_ int) {}

// SetFinalBatch is a no-op for static embedder (no thermal management needed).
func (e *StaticEmbedder768) SetFinalBatch(_ bool) {}
