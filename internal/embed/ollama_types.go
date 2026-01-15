package embed

import "time"

// Ollama API constants
const (
	// DefaultOllamaHost is the default Ollama API endpoint
	DefaultOllamaHost = "http://localhost:11434"

	// DefaultOllamaModel is the recommended embedding model for code+docs
	// Using 0.6B variant due to 24GB RAM constraint (8B causes system freeze)
	// MTEB-Code: 74.57 (same as MLX qwen3-embedding-0.6b)
	DefaultOllamaModel = "qwen3-embedding:0.6b"

	// OllamaConnectTimeout for initial health check
	OllamaConnectTimeout = 5 * time.Second

	// OllamaPoolSize for connection pool
	OllamaPoolSize = 4
)

// FallbackOllamaModels are tried in order if primary model unavailable.
// Note: Only code-optimized embedding models included. nomic-embed-text
// is a general text model and NOT suitable for code search.
var FallbackOllamaModels = []string{
	"embeddinggemma",     // 308M params, MTEB-Code: 68.14, MRL support
	"mxbai-embed-large",  // Fallback for non-code scenarios
}

// OllamaConfig configures the Ollama embedder
type OllamaConfig struct {
	// Host is the Ollama API endpoint (default: http://localhost:11434)
	Host string

	// Model is the embedding model to use (default: qwen3-embedding:8b)
	Model string

	// FallbackModels are tried in order if primary model unavailable
	FallbackModels []string

	// Dimensions can be set to override auto-detection (0 = auto-detect)
	Dimensions int

	// BatchSize for batch embedding requests (default: 32)
	BatchSize int

	// Timeout for API requests (default: 30s)
	Timeout time.Duration

	// ConnectTimeout for initial health check (default: 5s)
	ConnectTimeout time.Duration

	// MaxRetries for transient failures (default: 3)
	MaxRetries int

	// PoolSize for HTTP connection pool (default: 4)
	PoolSize int

	// SkipHealthCheck skips initial Ollama availability check (for testing)
	SkipHealthCheck bool

	// ProgressFunc is called after each batch with (completed, total) counts
	// This allows callers to display progress during embedding
	ProgressFunc func(completed, total int)

	// Thermal management settings for sustained GPU workloads (Apple Silicon)
	// InterBatchDelay is the pause between embedding batches (default: 0, disabled)
	InterBatchDelay time.Duration

	// TimeoutProgression increases timeout for later batches (1.0 = no increase)
	// Formula: effectiveTimeout = baseTimeout * (1 + (batchIndex*BatchSize/1000) * (TimeoutProgression - 1))
	TimeoutProgression float64

	// RetryTimeoutMultiplier scales timeout on each retry (1.0 = no scaling)
	// Formula: retryTimeout = baseTimeout * (RetryTimeoutMultiplier ^ attemptNumber)
	RetryTimeoutMultiplier float64
}

// DefaultOllamaConfig returns sensible defaults
func DefaultOllamaConfig() OllamaConfig {
	return OllamaConfig{
		Host:           DefaultOllamaHost,
		Model:          DefaultOllamaModel,
		FallbackModels: FallbackOllamaModels,
		Dimensions:     0, // Auto-detect
		BatchSize:      DefaultBatchSize,
		Timeout:        DefaultTimeout,
		ConnectTimeout: OllamaConnectTimeout,
		MaxRetries:     DefaultMaxRetries,
		PoolSize:       OllamaPoolSize,
		// Thermal management defaults (disabled - most users don't need these)
		InterBatchDelay:        DefaultInterBatchDelay,
		TimeoutProgression:     DefaultTimeoutProgression,
		RetryTimeoutMultiplier: DefaultRetryTimeoutMultiplier,
	}
}

// OllamaEmbedRequest is the Ollama /api/embed request
type OllamaEmbedRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"` // string or []string for batch
}

// OllamaEmbedResponse is the Ollama /api/embed response
type OllamaEmbedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float64 `json:"embeddings"`
}

// OllamaModelListResponse is the Ollama /api/tags response
type OllamaModelListResponse struct {
	Models []OllamaModelInfo `json:"models"`
}

// OllamaModelInfo describes an installed model
type OllamaModelInfo struct {
	Name       string    `json:"name"`
	ModifiedAt time.Time `json:"modified_at"`
	Size       int64     `json:"size"`
}
