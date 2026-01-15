package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// MLX model dimensions
const (
	MLXSmallDimensions  = 1024 // Qwen3-Embedding-0.6B
	MLXMediumDimensions = 2560 // Qwen3-Embedding-4B
	MLXLargeDimensions  = 4096 // Qwen3-Embedding-8B
)

// MLX default configuration
const (
	DefaultMLXEndpoint    = "http://localhost:9659" // Port 9659 to avoid conflicts with common port 8000
	DefaultMLXModel       = "small"                 // 0.6B model: 94% quality, 5x less memory (TASK-MEM1)
	DefaultMLXBaseTimeout = 60 * time.Second        // Base timeout for progressive scaling
	DefaultMLXMaxRetries  = 2                       // Retry attempts for transient failures
	DefaultMLXBatchSize   = 32                      // Assumed batch size for timeout calculation
)

// MLXConfig holds configuration for MLX embedder
type MLXConfig struct {
	// Endpoint is the MLX server URL (default: http://localhost:9659)
	Endpoint string

	// Model is the model size: "small" (0.6B), "medium" (4B), or "large" (8B)
	Model string

	// SkipHealthCheck skips health check during creation (for testing)
	SkipHealthCheck bool
}

// DefaultMLXConfig returns default MLX configuration
func DefaultMLXConfig() MLXConfig {
	return MLXConfig{
		Endpoint: DefaultMLXEndpoint,
		Model:    DefaultMLXModel,
	}
}

// MLXEmbedder generates embeddings using MLX server
// MLX provides ~55x faster embedding than Ollama on Apple Silicon
type MLXEmbedder struct {
	client       *http.Client
	config       MLXConfig
	dims         int
	model        string
	mu           sync.RWMutex
	closed       bool
	batchIndex   int  // Track batch progress for thermal timeout scaling
	isFinalBatch bool // Final batch gets timeout boost
}

// Verify interface implementation at compile time
var _ Embedder = (*MLXEmbedder)(nil)

// NewMLXEmbedder creates a new MLX embedder
func NewMLXEmbedder(ctx context.Context, cfg MLXConfig) (*MLXEmbedder, error) {
	// Apply defaults
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultMLXEndpoint
	}
	if cfg.Model == "" {
		cfg.Model = DefaultMLXModel
	}

	// BUG-052 pattern: Do NOT set http.Client.Timeout - it overrides context timeouts
	// We use context.WithTimeout() per-request in EmbedBatch to enable
	// progressive timeout scaling based on batch progress and thermal state.
	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	e := &MLXEmbedder{
		client: client,
		config: cfg,
		model:  cfg.Model,
	}

	// Set dimensions based on model
	switch cfg.Model {
	case "small":
		e.dims = MLXSmallDimensions
	case "medium":
		e.dims = MLXMediumDimensions
	case "large":
		e.dims = MLXLargeDimensions
	default:
		e.dims = MLXLargeDimensions // Default to large
	}

	// Health check (unless skipped)
	if !cfg.SkipHealthCheck {
		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		if err := e.healthCheck(checkCtx); err != nil {
			return nil, fmt.Errorf("MLX health check failed: %w", err)
		}

		// Try to get actual dimensions from server
		if dims, err := e.getDimensionsFromServer(checkCtx); err == nil {
			e.dims = dims
		}
	}

	slog.Debug("mlx_embedder_created",
		slog.String("endpoint", cfg.Endpoint),
		slog.String("model", cfg.Model),
		slog.Int("dimensions", e.dims))

	return e, nil
}

// healthCheck verifies MLX server is running and healthy
func (e *MLXEmbedder) healthCheck(ctx context.Context) error {
	url := e.config.Endpoint + "/health"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to MLX server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("MLX server unhealthy (status %d): %s", resp.StatusCode, string(body))
	}

	var health mlxHealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return fmt.Errorf("failed to decode health response: %w", err)
	}

	if health.Status != "healthy" {
		return fmt.Errorf("MLX server status: %s", health.Status)
	}

	return nil
}

// getDimensionsFromServer gets the dimensions for the configured model
func (e *MLXEmbedder) getDimensionsFromServer(ctx context.Context) (int, error) {
	url := e.config.Endpoint + "/models"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("failed to get models: status %d", resp.StatusCode)
	}

	var result mlxModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	if model, ok := result.Models[e.config.Model]; ok {
		return model.Dimensions, nil
	}

	return 0, fmt.Errorf("model %s not found", e.config.Model)
}

// Embed generates embedding for a single text
func (e *MLXEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	e.mu.RLock()
	if e.closed {
		e.mu.RUnlock()
		return nil, fmt.Errorf("embedder is closed")
	}
	e.mu.RUnlock()

	url := e.config.Endpoint + "/embed"

	reqBody := mlxEmbedRequest{
		Text:  text,
		Model: e.config.Model,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get embedding: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result mlxEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert float64 to float32
	embedding := make([]float32, len(result.Embedding))
	for i, v := range result.Embedding {
		embedding[i] = float32(v)
	}

	return embedding, nil
}

// EmbedBatch generates embeddings for multiple texts with retry logic
// and progressive timeout scaling for thermal throttling.
func (e *MLXEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	e.mu.RLock()
	if e.closed {
		e.mu.RUnlock()
		return nil, fmt.Errorf("embedder is closed")
	}
	e.mu.RUnlock()

	var lastErr error
	for attempt := 0; attempt < DefaultMLXMaxRetries; attempt++ {
		// Check context cancellation (Ctrl+C support)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Exponential backoff between retries
		if attempt > 0 {
			backoff := time.Duration(500<<attempt) * time.Millisecond // 1s, 2s
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		// Progressive timeout based on batch progress
		timeout := e.getProgressiveTimeout()
		timeoutCtx, cancel := context.WithTimeout(ctx, timeout)

		slog.Debug("mlx_embedding_attempt",
			slog.Int("attempt", attempt+1),
			slog.Int("batch_index", e.batchIndex),
			slog.Duration("timeout", timeout),
			slog.Bool("final_batch", e.isFinalBatch),
			slog.Int("texts_count", len(texts)))

		embeddings, err := e.doEmbedBatch(timeoutCtx, texts)
		cancel()

		if err == nil {
			return embeddings, nil
		}
		lastErr = err

		slog.Debug("mlx_embedding_attempt_failed",
			slog.Int("attempt", attempt+1),
			slog.Duration("timeout_used", timeout),
			slog.String("error", err.Error()))
	}

	return nil, fmt.Errorf("failed after %d attempts: %w", DefaultMLXMaxRetries, lastErr)
}

// doEmbedBatch performs the actual HTTP request to MLX server
func (e *MLXEmbedder) doEmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	url := e.config.Endpoint + "/embed_batch"

	reqBody := mlxEmbedBatchRequest{
		Texts: texts,
		Model: e.config.Model,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get batch embeddings: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("batch embedding failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result mlxEmbedBatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert float64 to float32
	embeddings := make([][]float32, len(result.Embeddings))
	for i, emb := range result.Embeddings {
		embeddings[i] = make([]float32, len(emb))
		for j, v := range emb {
			embeddings[i][j] = float32(v)
		}
	}

	return embeddings, nil
}

// Dimensions returns the embedding dimension
func (e *MLXEmbedder) Dimensions() int {
	return e.dims
}

// ModelName returns the model identifier
func (e *MLXEmbedder) ModelName() string {
	return fmt.Sprintf("mlx-qwen3-embedding-%s", e.model)
}

// Available checks if the embedder is ready
func (e *MLXEmbedder) Available(ctx context.Context) bool {
	e.mu.RLock()
	if e.closed {
		e.mu.RUnlock()
		return false
	}
	e.mu.RUnlock()

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return e.healthCheck(checkCtx) == nil
}

// Close releases resources
func (e *MLXEmbedder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil
	}
	e.closed = true

	// Close idle connections
	if transport, ok := e.client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}

	return nil
}

// SetBatchIndex sets the batch index for thermal timeout progression.
// Used for progressive timeout scaling during sustained indexing workloads.
func (e *MLXEmbedder) SetBatchIndex(idx int) {
	e.mu.Lock()
	e.batchIndex = idx
	e.mu.Unlock()
}

// SetFinalBatch marks the embedder as processing the final batch.
// Final batch gets 1.5x timeout boost due to peak thermal throttling.
func (e *MLXEmbedder) SetFinalBatch(isFinal bool) {
	e.mu.Lock()
	e.isFinalBatch = isFinal
	e.mu.Unlock()
}

// getProgressiveTimeout calculates timeout based on batch progress.
// Thermal throttling causes later batches to take longer, so we scale timeout:
// - Base: 60s (enough for early batches at ~5-8s actual)
// - Progression: scales up to 2x based on chunks processed
// - Final boost: 1.5x for the last batch (peak thermal)
func (e *MLXEmbedder) getProgressiveTimeout() time.Duration {
	baseTimeout := DefaultMLXBaseTimeout // 60s

	e.mu.RLock()
	batchIdx := e.batchIndex
	isFinal := e.isFinalBatch
	e.mu.RUnlock()

	// Progressive scaling: 1.0 to 2.0 based on batch progress
	// Every 2000 chunks processed, double the timeout
	progression := 1.0 + float64(batchIdx*DefaultMLXBatchSize)/2000.0
	if progression > 2.0 {
		progression = 2.0 // Cap at 2x
	}

	// Final batch boost - thermal throttling peaks at end
	finalBoost := 1.0
	if isFinal {
		finalBoost = 1.5
	}

	return time.Duration(float64(baseTimeout) * progression * finalBoost)
}

// MLX API request/response types

type mlxHealthResponse struct {
	Status      string `json:"status"`
	ModelStatus string `json:"model_status"`
	LoadedModel string `json:"loaded_model"`
}

type mlxModelsResponse struct {
	Models map[string]mlxModelInfo `json:"models"`
}

type mlxModelInfo struct {
	Dimensions int `json:"dimensions"`
}

type mlxEmbedRequest struct {
	Text  string `json:"text"`
	Model string `json:"model"`
}

type mlxEmbedResponse struct {
	Embedding []float64 `json:"embedding"`
}

type mlxEmbedBatchRequest struct {
	Texts []string `json:"texts"`
	Model string   `json:"model"`
}

type mlxEmbedBatchResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}
