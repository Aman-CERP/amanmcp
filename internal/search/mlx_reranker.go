package search

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

// MLX reranker configuration defaults
const (
	DefaultRerankerEndpoint = "http://localhost:9659" // Same as embeddings
	DefaultRerankerModel    = "reranker-small"        // Qwen3-Reranker-0.6B
	DefaultRerankerTimeout  = 30 * time.Second        // Timeout for rerank requests
	DefaultRerankerPoolSize = 50                      // Default candidates to rerank
)

// MLXRerankerConfig holds configuration for MLX reranker
type MLXRerankerConfig struct {
	// Endpoint is the MLX server URL (default: http://localhost:9659)
	Endpoint string

	// Model is the reranker model alias (default: reranker-small)
	Model string

	// Timeout is the request timeout (default: 30s)
	Timeout time.Duration

	// PoolSize is the default number of candidates to rerank (default: 50)
	PoolSize int

	// SkipHealthCheck skips health check during creation (for testing)
	SkipHealthCheck bool

	// Instruction is the custom instruction for reranking (optional)
	// Default: "Given a search query, retrieve relevant code or documentation..."
	Instruction string
}

// DefaultMLXRerankerConfig returns default reranker configuration
func DefaultMLXRerankerConfig() MLXRerankerConfig {
	return MLXRerankerConfig{
		Endpoint: DefaultRerankerEndpoint,
		Model:    DefaultRerankerModel,
		Timeout:  DefaultRerankerTimeout,
		PoolSize: DefaultRerankerPoolSize,
	}
}

// MLXReranker implements cross-encoder reranking via MLX server.
// FEAT-RR1: Closes the 25% validation gap by reranking generic queries.
type MLXReranker struct {
	client   *http.Client
	config   MLXRerankerConfig
	mu       sync.RWMutex
	closed   bool
	endpoint string
}

// Verify interface implementation at compile time
var _ Reranker = (*MLXReranker)(nil)

// NewMLXReranker creates a new MLX reranker client
func NewMLXReranker(ctx context.Context, cfg MLXRerankerConfig) (*MLXReranker, error) {
	// Apply defaults
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultRerankerEndpoint
	}
	if cfg.Model == "" {
		cfg.Model = DefaultRerankerModel
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultRerankerTimeout
	}
	if cfg.PoolSize == 0 {
		cfg.PoolSize = DefaultRerankerPoolSize
	}

	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	r := &MLXReranker{
		client:   client,
		config:   cfg,
		endpoint: cfg.Endpoint,
	}

	// Health check (unless skipped)
	if !cfg.SkipHealthCheck {
		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		if err := r.healthCheck(checkCtx); err != nil {
			return nil, fmt.Errorf("MLX reranker health check failed: %w", err)
		}
	}

	slog.Debug("mlx_reranker_created",
		slog.String("endpoint", cfg.Endpoint),
		slog.String("model", cfg.Model),
		slog.Duration("timeout", cfg.Timeout),
		slog.Int("pool_size", cfg.PoolSize))

	return r, nil
}

// healthCheck verifies MLX server has reranking capability
func (r *MLXReranker) healthCheck(ctx context.Context) error {
	url := r.endpoint + "/health"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to MLX server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("MLX server unhealthy (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// rerankRequest is the JSON request to /rerank endpoint
type rerankRequest struct {
	Query       string   `json:"query"`
	Documents   []string `json:"documents"`
	Model       string   `json:"model,omitempty"`
	Instruction string   `json:"instruction,omitempty"`
	TopK        int      `json:"top_k,omitempty"`
}

// rerankResponse is the JSON response from /rerank endpoint
type rerankResponse struct {
	Results []struct {
		Index    int     `json:"index"`
		Score    float64 `json:"score"`
		Document string  `json:"document"`
	} `json:"results"`
	Model            string  `json:"model"`
	Query            string  `json:"query"`
	Count            int     `json:"count"`
	ProcessingTimeMs float64 `json:"processing_time_ms"`
}

// Rerank scores and reorders documents by relevance to the query.
// DEBT-024: Instrumented with detailed timing for latency investigation.
func (r *MLXReranker) Rerank(ctx context.Context, query string, documents []string, topK int) ([]RerankResult, error) {
	overallStart := time.Now()

	r.mu.RLock()
	if r.closed {
		r.mu.RUnlock()
		return nil, fmt.Errorf("reranker is closed")
	}
	r.mu.RUnlock()

	if len(documents) == 0 {
		return []RerankResult{}, nil
	}

	// DEBT-024: Measure request preparation
	prepStart := time.Now()

	// Prepare request
	reqBody := rerankRequest{
		Query:     query,
		Documents: documents,
		Model:     r.config.Model,
	}
	if r.config.Instruction != "" {
		reqBody.Instruction = r.config.Instruction
	}
	if topK > 0 {
		reqBody.TopK = topK
	}

	// DEBT-024: Measure JSON marshal time
	marshalStart := time.Now()
	jsonData, err := json.Marshal(reqBody)
	marshalDuration := time.Since(marshalStart)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal rerank request: %w", err)
	}
	payloadSize := len(jsonData)
	prepDuration := time.Since(prepStart)

	// Create request with timeout
	url := r.endpoint + "/rerank"
	timeoutCtx, cancel := context.WithTimeout(ctx, r.config.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create rerank request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// DEBT-024: Measure HTTP round-trip time
	httpStart := time.Now()
	resp, err := r.client.Do(req)
	httpDuration := time.Since(httpStart)
	if err != nil {
		return nil, fmt.Errorf("rerank request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("rerank failed (status %d): %s", resp.StatusCode, string(body))
	}

	// DEBT-024: Measure JSON unmarshal time
	unmarshalStart := time.Now()
	var result rerankResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode rerank response: %w", err)
	}
	unmarshalDuration := time.Since(unmarshalStart)

	// Convert to RerankResult slice
	results := make([]RerankResult, len(result.Results))
	for i, r := range result.Results {
		results[i] = RerankResult{
			Index:    r.Index,
			Score:    r.Score,
			Document: r.Document,
		}
	}

	totalDuration := time.Since(overallStart)

	// DEBT-024: Enhanced telemetry with HTTP timing breakdown
	slog.Debug("reranker_http_timing",
		slog.String("query", truncateQuery(query, 50)),
		slog.Int("doc_count", len(documents)),
		slog.Int("payload_bytes", payloadSize),
		slog.Duration("prep", prepDuration),
		slog.Duration("json_marshal", marshalDuration),
		slog.Duration("http_request", httpDuration),
		slog.Duration("json_unmarshal", unmarshalDuration),
		slog.Duration("total", totalDuration),
		slog.Float64("server_time_ms", result.ProcessingTimeMs),
		slog.Float64("overhead_ms", float64(totalDuration.Milliseconds())-result.ProcessingTimeMs))

	return results, nil
}

// Available checks if the reranker service is available
func (r *MLXReranker) Available(ctx context.Context) bool {
	r.mu.RLock()
	if r.closed {
		r.mu.RUnlock()
		return false
	}
	r.mu.RUnlock()

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return r.healthCheck(checkCtx) == nil
}

// Close releases resources
func (r *MLXReranker) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true

	// Close idle connections
	if transport, ok := r.client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}

	return nil
}

// truncateQuery truncates a query string for logging
func truncateQuery(q string, maxLen int) string {
	if len(q) <= maxLen {
		return q
	}
	return q[:maxLen] + "..."
}
