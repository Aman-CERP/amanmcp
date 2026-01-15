package embed

import (
	"context"
	"math"
	"time"
)

// Common embedding constants
const (
	// MinBatchSize is the minimum allowed batch size
	MinBatchSize = 1

	// MaxBatchSize is the maximum allowed batch size (prevents memory exhaustion)
	MaxBatchSize = 256

	// DefaultBatchSize is the default batch size for embedding requests
	DefaultBatchSize = 32

	// DefaultTimeout is the default timeout for embedding requests
	// Deprecated: Use DefaultWarmTimeout and DefaultColdTimeout instead
	DefaultTimeout = 60 * time.Second

	// DefaultWarmTimeout is the timeout for subsequent queries when model is loaded
	// BUG-052: Increased from 60s to 120s to accommodate thermal throttling on
	// large codebases (6000+ chunks). GPU thermal throttling can push embedding
	// time to 90-120s per batch near completion of long indexing operations.
	DefaultWarmTimeout = 120 * time.Second

	// DefaultColdTimeout is the timeout for first query when model may need loading
	// BUG-052: Increased from 120s to 180s for safety margin on slower hardware
	// or with larger embedding models (e.g., 8B parameter models)
	DefaultColdTimeout = 180 * time.Second

	// ModelUnloadThreshold is the duration after which a model is considered "cold"
	// Ollama unloads models after ~5 minutes of inactivity
	ModelUnloadThreshold = 5 * time.Minute

	// DefaultMaxRetries is the default number of retry attempts
	DefaultMaxRetries = 3
)

// Thermal-aware indexing constants
// These help prevent timeout failures during long indexing operations on Apple Silicon
const (
	// DefaultInterBatchDelay is the default pause between embedding batches
	// Set to 0 (disabled) by default - most users don't need this
	DefaultInterBatchDelay = 0 * time.Millisecond

	// MaxInterBatchDelay caps the cooling delay to prevent excessive slowdown
	MaxInterBatchDelay = 5 * time.Second

	// DefaultTimeoutProgression controls how much timeout increases per 1000 chunks
	// 1.0 = no progression (disabled), 1.5 = 50% increase per 1000 chunks
	// Default 1.5 for thermal adaptation on large codebases (98% of users)
	DefaultTimeoutProgression = 1.5

	// MaxTimeoutProgression caps the timeout multiplier to prevent excessive waits
	MaxTimeoutProgression = 3.0

	// DefaultRetryTimeoutMultiplier scales timeout on each retry attempt
	// 1.0 = no scaling (disabled), 1.5 = 50% increase per retry
	DefaultRetryTimeoutMultiplier = 1.0

	// MaxRetryTimeoutMultiplier caps the retry timeout scaling
	MaxRetryTimeoutMultiplier = 2.0
)

// EmbeddingGemma constants (default)
const (
	// DefaultDimensions is the embedding dimension for EmbeddingGemma
	DefaultDimensions = 768

	// DefaultContext is the context window for EmbeddingGemma (4x larger than MiniLM)
	DefaultContext = 2048
)

// Static embedder constants
const (
	// StaticDimensions is the embedding dimension for static embedder
	StaticDimensions = 256
)

// Embedder generates vector embeddings for text
type Embedder interface {
	// Embed generates embedding for a single text
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch generates embeddings for multiple texts
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the embedding dimension
	Dimensions() int

	// ModelName returns the model identifier
	ModelName() string

	// Available checks if the embedder is ready
	Available(ctx context.Context) bool

	// Close releases resources
	Close() error

	// SetBatchIndex sets the batch index for thermal timeout progression.
	// BUG-050: Used when resuming from checkpoint to maintain correct batch position.
	SetBatchIndex(idx int)

	// SetFinalBatch marks the embedder as processing the final batch.
	// BUG-050: Triggers a 1.5x timeout boost for peak thermal throttling.
	SetFinalBatch(isFinal bool)
}

// normalizeVector normalizes a vector to unit length.
func normalizeVector(v []float32) []float32 {
	var sumSquares float64
	for _, val := range v {
		sumSquares += float64(val) * float64(val)
	}

	magnitude := math.Sqrt(sumSquares)
	if magnitude == 0 {
		return v // Return as-is if zero vector
	}

	normalized := make([]float32, len(v))
	for i, val := range v {
		normalized[i] = float32(float64(val) / magnitude)
	}
	return normalized
}
