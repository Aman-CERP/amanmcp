package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

// OllamaEmbedder generates embeddings using Ollama's HTTP API
type OllamaEmbedder struct {
	client    *http.Client
	transport *http.Transport // Store for connection cleanup
	config    OllamaConfig
	modelName string
	dims      int

	mu           sync.RWMutex
	closed       bool
	lastCall     time.Time // QW-4: Track last call for warm/cold timeout detection
	batchIndex   int       // Track batch progress for progressive timeout
	isFinalBatch bool      // BUG-050: Track if processing final batch for timeout boost
}

// Verify interface implementation at compile time
var _ Embedder = (*OllamaEmbedder)(nil)

// NewOllamaEmbedder creates a new Ollama embedder
func NewOllamaEmbedder(ctx context.Context, cfg OllamaConfig) (*OllamaEmbedder, error) {
	// Apply defaults
	if cfg.Host == "" {
		cfg.Host = DefaultOllamaHost
	}
	if cfg.Model == "" {
		cfg.Model = DefaultOllamaModel
	}
	if cfg.FallbackModels == nil {
		cfg.FallbackModels = FallbackOllamaModels
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultBatchSize
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = OllamaConnectTimeout
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	if cfg.PoolSize <= 0 {
		cfg.PoolSize = OllamaPoolSize
	}

	// Create HTTP client with connection pooling
	// IdleConnTimeout is set to 10s (not 90s) because CLI indexing is short-lived.
	// Short timeout ensures connections are cleaned up quickly after Ctrl+C.
	transport := &http.Transport{
		MaxIdleConns:        cfg.PoolSize,
		MaxIdleConnsPerHost: cfg.PoolSize,
		MaxConnsPerHost:     cfg.PoolSize * 2,
		IdleConnTimeout:     10 * time.Second,
		DisableKeepAlives:   false,
	}

	// BUG-052: Do NOT set http.Client.Timeout - it overrides context timeouts
	// We use context.WithTimeout() per-request in doEmbedWithRetry() to enable
	// progressive timeout scaling based on batch progress and thermal state.
	// Setting a static client timeout would bypass all progressive timeout logic.
	// See: https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/
	client := &http.Client{
		Transport: transport,
	}

	e := &OllamaEmbedder{
		client:    client,
		transport: transport,
		config:    cfg,
		modelName: cfg.Model,
		dims:      cfg.Dimensions,
	}

	// Health check and model discovery (unless skipped for testing)
	// BUG-041: Use DefaultColdTimeout (180s) instead of ConnectTimeout (5s) for health check.
	// Cold model loads can take 30-60+ seconds; 5s is insufficient.
	if !cfg.SkipHealthCheck {
		checkCtx, cancel := context.WithTimeout(ctx, DefaultColdTimeout)
		defer cancel()

		modelName, err := e.findAvailableModel(checkCtx)
		if err != nil {
			transport.CloseIdleConnections()
			return nil, fmt.Errorf("failed to connect to Ollama or find model: %w", err)
		}
		e.modelName = modelName

		// Auto-detect dimensions from first embedding (use timeout context)
		if cfg.Dimensions == 0 {
			dims, err := e.detectDimensions(checkCtx)
			if err != nil {
				transport.CloseIdleConnections()
				return nil, fmt.Errorf("failed to detect embedding dimensions: %w", err)
			}
			e.dims = dims
		}
	}

	// Fallback to default dimensions if still not set
	if e.dims == 0 {
		e.dims = DefaultDimensions
	}

	return e, nil
}

// listModels gets available models from Ollama
func (e *OllamaEmbedder) listModels(ctx context.Context) ([]OllamaModelInfo, error) {
	url := e.config.Host + "/api/tags"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ollama: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var result OllamaModelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Models, nil
}

// findAvailableModel finds a suitable embedding model
func (e *OllamaEmbedder) findAvailableModel(ctx context.Context) (string, error) {
	models, err := e.listModels(ctx)
	if err != nil {
		return "", err
	}

	// Build set of available model names (normalized)
	available := make(map[string]string) // normalized -> actual
	for _, m := range models {
		name := strings.ToLower(m.Name)
		// Store both full name and base name (without tag)
		available[name] = m.Name
		base := strings.Split(name, ":")[0]
		if _, exists := available[base]; !exists {
			available[base] = m.Name
		}
	}

	// Check for primary model
	primaryName := strings.ToLower(e.config.Model)
	if actual, ok := available[primaryName]; ok {
		return actual, nil
	}
	primaryBase := strings.Split(primaryName, ":")[0]
	if actual, ok := available[primaryBase]; ok {
		return actual, nil
	}

	// Check fallback models
	for _, fallback := range e.config.FallbackModels {
		name := strings.ToLower(fallback)
		if actual, ok := available[name]; ok {
			return actual, nil
		}
		base := strings.Split(name, ":")[0]
		if actual, ok := available[base]; ok {
			return actual, nil
		}
	}

	return "", fmt.Errorf("no embedding model available (tried %s and %v)", e.config.Model, e.config.FallbackModels)
}

// detectDimensions auto-detects embedding dimensions from a test embedding
func (e *OllamaEmbedder) detectDimensions(ctx context.Context) (int, error) {
	testText := "dimension detection"

	url := e.config.Host + "/api/embed"
	reqBody := OllamaEmbedRequest{
		Model: e.modelName,
		Input: testText,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("embedding failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result OllamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(result.Embeddings) == 0 || len(result.Embeddings[0]) == 0 {
		return 0, fmt.Errorf("empty embedding returned")
	}

	return len(result.Embeddings[0]), nil
}

// Embed generates embedding for a single text
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	e.mu.RLock()
	if e.closed {
		e.mu.RUnlock()
		return nil, fmt.Errorf("embedder is closed")
	}
	e.mu.RUnlock()

	// Handle empty/whitespace input
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return make([]float32, e.dims), nil
	}

	embeddings, err := e.doEmbedWithRetry(ctx, []string{text})
	if err != nil {
		return nil, err
	}

	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}

	return embeddings[0], nil
}

// EmbedBatch generates embeddings for multiple texts using Ollama's batch API
func (e *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	e.mu.RLock()
	if e.closed {
		e.mu.RUnlock()
		return nil, fmt.Errorf("embedder is closed")
	}
	e.mu.RUnlock()

	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	// Track which indices need API calls vs zero vectors
	type indexedText struct {
		idx  int
		text string
	}
	var nonEmpty []indexedText
	results := make([][]float32, len(texts))

	for i, text := range texts {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			results[i] = make([]float32, e.dims)
		} else {
			nonEmpty = append(nonEmpty, indexedText{i, text})
		}
	}

	if len(nonEmpty) == 0 {
		return results, nil
	}

	// Process in batches
	for start := 0; start < len(nonEmpty); start += e.config.BatchSize {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		end := start + e.config.BatchSize
		if end > len(nonEmpty) {
			end = len(nonEmpty)
		}

		batch := nonEmpty[start:end]
		batchTexts := make([]string, len(batch))
		for i, it := range batch {
			batchTexts[i] = it.text
		}

		embeddings, err := e.doEmbedWithRetry(ctx, batchTexts)
		if err != nil {
			return nil, fmt.Errorf("failed to embed batch: %w", err)
		}

		for i, emb := range embeddings {
			results[batch[i].idx] = emb
		}

		// Track batch progress for progressive timeout calculation
		e.IncrementBatchIndex()

		// Report progress if callback is set
		if e.config.ProgressFunc != nil {
			e.config.ProgressFunc(end, len(nonEmpty))
		}
	}

	return results, nil
}

// getTimeout returns the appropriate timeout based on cold/warm state.
// QW-4: Uses shorter warm timeout (15s) for subsequent queries,
// longer cold timeout (20s) when model may need loading.
func (e *OllamaEmbedder) getTimeout() time.Duration {
	e.mu.RLock()
	lastCall := e.lastCall
	e.mu.RUnlock()

	// First call or model likely unloaded (>5 min since last call)
	if lastCall.IsZero() || time.Since(lastCall) > ModelUnloadThreshold {
		return DefaultColdTimeout
	}
	return DefaultWarmTimeout
}

// updateLastCall records the time of a successful embedding call.
func (e *OllamaEmbedder) updateLastCall() {
	e.mu.Lock()
	e.lastCall = time.Now()
	e.mu.Unlock()
}

// getProgressiveTimeout returns timeout adjusted for thermal throttling on sustained workloads.
// As indexing progresses, GPUs (especially Apple Silicon) may throttle due to heat.
// This increases timeout for later batches and retries to accommodate slower processing.
func (e *OllamaEmbedder) getProgressiveTimeout(attempt int) time.Duration {
	baseTimeout := e.getTimeout()

	// Calculate progression factor based on batch index
	// Formula: progressionFactor = 1 + (batchProgress * (TimeoutProgression - 1))
	// Example: At batch 50 (1600 chunks), with TimeoutProgression=1.5:
	//   progressionFactor = 1 + (1.6 * 0.5) = 1.8x timeout
	progressionFactor := 1.0
	if e.config.TimeoutProgression > 1.0 {
		e.mu.RLock()
		batchIdx := e.batchIndex
		e.mu.RUnlock()

		batchProgress := float64(batchIdx*e.config.BatchSize) / 1000.0
		progressionFactor = 1.0 + batchProgress*(e.config.TimeoutProgression-1.0)
		// BUG-051: Cap at 3x to allow thermal progression for large codebases
		// The old cap at TimeoutProgression value was too restrictive for late batches
		if progressionFactor > 3.0 {
			progressionFactor = 3.0
		}
	}

	// Calculate retry factor (exponential increase per retry)
	// Formula: retryFactor = RetryTimeoutMultiplier ^ attempt
	// Example: With RetryTimeoutMultiplier=1.5, attempt=2: retryFactor = 2.25x
	retryFactor := 1.0
	if e.config.RetryTimeoutMultiplier > 1.0 && attempt > 0 {
		retryFactor = math.Pow(e.config.RetryTimeoutMultiplier, float64(attempt))
		if retryFactor > MaxRetryTimeoutMultiplier {
			retryFactor = MaxRetryTimeoutMultiplier
		}
	}

	// BUG-051: Apply 1.5x boost for final batch to handle peak thermal throttling
	// The final batch often fails because the GPU is at maximum thermal throttling
	// after processing thousands of chunks
	e.mu.RLock()
	isFinal := e.isFinalBatch
	e.mu.RUnlock()

	finalBoost := 1.0
	if isFinal {
		finalBoost = 1.5
	}

	return time.Duration(float64(baseTimeout) * progressionFactor * retryFactor * finalBoost)
}

// IncrementBatchIndex tracks batch progress for progressive timeout calculation.
// Call this after each batch completes to adjust timeout for thermal throttling.
func (e *OllamaEmbedder) IncrementBatchIndex() {
	e.mu.Lock()
	e.batchIndex++
	e.mu.Unlock()
}

// ResetBatchIndex resets batch counter, typically at the start of a new indexing session.
func (e *OllamaEmbedder) ResetBatchIndex() {
	e.mu.Lock()
	e.batchIndex = 0
	e.mu.Unlock()
}

// SetBatchIndex sets the batch index to a specific value.
// Used when resuming from checkpoint to maintain correct thermal progression.
// BUG-051: Without this, resume starts batchIndex at 0 instead of the actual batch number,
// causing incorrect (too short) timeout calculation for late batches.
func (e *OllamaEmbedder) SetBatchIndex(idx int) {
	e.mu.Lock()
	e.batchIndex = idx
	e.mu.Unlock()
}

// SetFinalBatch marks the embedder as processing the final batch.
// This triggers a 1.5x timeout boost to handle accumulated thermal throttling.
// BUG-051: The final batch often times out because the GPU is at peak thermal throttling
// after processing thousands of chunks, but the default timeout doesn't account for this.
func (e *OllamaEmbedder) SetFinalBatch(isFinal bool) {
	e.mu.Lock()
	e.isFinalBatch = isFinal
	e.mu.Unlock()
}

// GetInterBatchDelay returns the configured inter-batch delay for thermal management.
func (e *OllamaEmbedder) GetInterBatchDelay() time.Duration {
	return e.config.InterBatchDelay
}

// doEmbedWithRetry performs embedding with retry logic and progressive timeout.
// Progressive timeout adjusts for thermal throttling on sustained GPU workloads.
func (e *OllamaEmbedder) doEmbedWithRetry(ctx context.Context, texts []string) ([][]float32, error) {
	var lastErr error

	for attempt := 0; attempt < e.config.MaxRetries; attempt++ {
		// Check parent context first - allows clean exit on Ctrl+C
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if attempt > 0 {
			// Exponential backoff: 100ms * 2^attempt
			backoff := time.Duration(100<<attempt) * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		// QW-4: Apply dynamic timeout based on warm/cold state and thermal progression
		// Progressive timeout increases for later batches and retries to handle thermal throttling
		timeout := e.getProgressiveTimeout(attempt)
		timeoutCtx, cancel := context.WithTimeout(ctx, timeout)

		// BUG-052: Diagnostic logging for debugging timeout issues
		slog.Debug("embedding_attempt",
			slog.Int("attempt", attempt+1),
			slog.Int("max_retries", e.config.MaxRetries),
			slog.Int("batch_index", e.batchIndex),
			slog.Duration("timeout", timeout),
			slog.Bool("final_batch", e.isFinalBatch),
			slog.Int("texts_count", len(texts)))

		embeddings, err := e.doEmbed(timeoutCtx, texts)
		cancel() // Clean up timeout context

		if err == nil {
			e.updateLastCall() // QW-4: Track successful call for warm/cold detection
			return embeddings, nil
		}
		lastErr = err

		// BUG-052: Log retry failure for debugging
		slog.Debug("embedding_attempt_failed",
			slog.Int("attempt", attempt+1),
			slog.Int("batch_index", e.batchIndex),
			slog.Duration("timeout_used", timeout),
			slog.String("error", err.Error()))

		// Check parent context after failed attempt
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	return nil, fmt.Errorf("failed after %d attempts: %w", e.config.MaxRetries, lastErr)
}

// doEmbed performs a single batch embedding request with cancellation support.
// It uses a goroutine to run the HTTP request and watches for context cancellation,
// allowing Ctrl+C to exit quickly instead of waiting for HTTP timeout.
func (e *OllamaEmbedder) doEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	url := e.config.Host + "/api/embed"

	// Use array input for batch, single string for single text
	var input any
	if len(texts) == 1 {
		input = texts[0]
	} else {
		input = texts
	}

	reqBody := OllamaEmbedRequest{
		Model: e.modelName,
		Input: input,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Result type for goroutine communication
	type result struct {
		embeddings [][]float32
		err        error
	}
	resultCh := make(chan result, 1)

	// Run HTTP request in goroutine so we can watch for context cancellation
	go func() {
		resp, err := e.client.Do(req)
		if err != nil {
			resultCh <- result{nil, err}
			return
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			resultCh <- result{nil, fmt.Errorf("embedding failed with status %d: %s", resp.StatusCode, string(respBody))}
			return
		}

		var apiResult OllamaEmbedResponse
		if err := json.NewDecoder(resp.Body).Decode(&apiResult); err != nil {
			resultCh <- result{nil, fmt.Errorf("failed to decode response: %w", err)}
			return
		}

		// Convert float64 to float32 and normalize
		embeddings := make([][]float32, len(apiResult.Embeddings))
		for i, emb := range apiResult.Embeddings {
			embedding := make([]float32, len(emb))
			for j, v := range emb {
				embedding[j] = float32(v)
			}
			embeddings[i] = normalizeVector(embedding)
		}

		resultCh <- result{embeddings, nil}
	}()

	// Wait for result or context cancellation
	select {
	case <-ctx.Done():
		// Context cancelled (Ctrl+C) - force close connections to unblock goroutine
		e.ForceCloseConnections()
		// Brief wait for goroutine cleanup
		select {
		case <-resultCh:
			// Goroutine exited cleanly
		case <-time.After(100 * time.Millisecond):
			// Goroutine still blocked, continue anyway
		}
		return nil, ctx.Err()
	case r := <-resultCh:
		return r.embeddings, r.err
	}
}

// Dimensions returns the embedding dimension
func (e *OllamaEmbedder) Dimensions() int {
	return e.dims
}

// ModelName returns the model identifier
func (e *OllamaEmbedder) ModelName() string {
	return e.modelName
}

// Available checks if Ollama is running and model is available
func (e *OllamaEmbedder) Available(ctx context.Context) bool {
	e.mu.RLock()
	if e.closed {
		e.mu.RUnlock()
		return false
	}
	e.mu.RUnlock()

	models, err := e.listModels(ctx)
	if err != nil {
		return false
	}

	modelLower := strings.ToLower(e.modelName)
	for _, m := range models {
		if strings.Contains(strings.ToLower(m.Name), modelLower) ||
			strings.Contains(modelLower, strings.ToLower(m.Name)) {
			return true
		}
	}
	return false
}

// SetProgressFunc sets the progress callback for batch embedding.
// The callback receives (completed, total) counts after each batch.
func (e *OllamaEmbedder) SetProgressFunc(fn func(completed, total int)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config.ProgressFunc = fn
}

// Close releases resources
func (e *OllamaEmbedder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil
	}
	e.closed = true

	// Close idle connections to release resources immediately
	if e.transport != nil {
		e.transport.CloseIdleConnections()
	}

	return nil
}

// ForceCloseConnections forcibly closes all HTTP connections including active ones.
// Called during shutdown to interrupt in-flight requests. Unlike CloseIdleConnections(),
// this replaces the transport to cancel pending reads, allowing Ctrl+C to exit quickly.
func (e *OllamaEmbedder) ForceCloseConnections() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.transport != nil {
		e.transport.CloseIdleConnections()
		// Replace transport to force-close active connections.
		// Existing goroutines reading from old connections will get EOF/error.
		e.transport = &http.Transport{
			MaxIdleConns:        e.config.PoolSize,
			MaxIdleConnsPerHost: e.config.PoolSize,
			MaxConnsPerHost:     e.config.PoolSize * 2,
			IdleConnTimeout:     10 * time.Second,
			DisableKeepAlives:   true, // Don't reuse connections during shutdown
		}
		e.client.Transport = e.transport
	}
}
