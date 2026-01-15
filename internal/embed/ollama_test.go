package embed

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// TS01: OllamaConfig Defaults
// ============================================================================

func TestDefaultOllamaConfig_HasCorrectDefaults(t *testing.T) {
	// Given: default config
	cfg := DefaultOllamaConfig()

	// Then: all defaults are set correctly
	assert.Equal(t, "http://localhost:11434", cfg.Host)
	// Using 0.6B due to 24GB RAM constraint (8B causes system freeze)
	assert.Equal(t, "qwen3-embedding:0.6b", cfg.Model)
	assert.Equal(t, 0, cfg.Dimensions, "dimensions should default to 0 (auto-detect)")
	assert.Equal(t, DefaultBatchSize, cfg.BatchSize)
	assert.Equal(t, 60*time.Second, cfg.Timeout)
	assert.Equal(t, 5*time.Second, cfg.ConnectTimeout)
	assert.Equal(t, 3, cfg.MaxRetries)
	assert.Equal(t, 4, cfg.PoolSize)
}

func TestDefaultOllamaConfig_HasFallbackModels(t *testing.T) {
	// Given: default config
	cfg := DefaultOllamaConfig()

	// Then: fallback models are configured (code-optimized models only)
	// Note: nomic-embed-text excluded - it's a general text model, NOT code-optimized
	assert.NotEmpty(t, cfg.FallbackModels)
	assert.Contains(t, cfg.FallbackModels, "embeddinggemma")
}

// ============================================================================
// TS02: OllamaEmbedder Interface Compliance
// ============================================================================

func TestOllamaEmbedder_ImplementsEmbedderInterface(t *testing.T) {
	// This test verifies at compile time that OllamaEmbedder implements Embedder
	var _ Embedder = (*OllamaEmbedder)(nil)
}

// ============================================================================
// TS03: Basic Embedding with Mock Server
// ============================================================================

func TestOllamaEmbedder_Embed_ReturnsCorrectDimensions(t *testing.T) {
	// Given: mock Ollama server returning 768-dim embeddings
	dims := 768
	server := mockOllamaServer(t, dims, nil)
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL
	cfg.SkipHealthCheck = true
	cfg.Dimensions = dims

	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// When: embedding text
	embedding, err := embedder.Embed(context.Background(), "func main() {}")

	// Then: returns correct dimensions
	require.NoError(t, err)
	assert.Len(t, embedding, dims)
}

func TestOllamaEmbedder_Embed_VectorIsNormalized(t *testing.T) {
	// Given: mock Ollama server
	dims := 768
	server := mockOllamaServer(t, dims, nil)
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL
	cfg.SkipHealthCheck = true
	cfg.Dimensions = dims

	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// When: embedding text
	embedding, err := embedder.Embed(context.Background(), "func main() {}")
	require.NoError(t, err)

	// Then: vector is normalized
	magnitude := vectorMagnitude(embedding)
	assert.InDelta(t, 1.0, magnitude, 0.001, "vector should be normalized")
}

// ============================================================================
// TS04: Empty/Whitespace Input
// ============================================================================

func TestOllamaEmbedder_Embed_EmptyString_ReturnsZeroVector(t *testing.T) {
	// Given: mock Ollama server
	dims := 768
	server := mockOllamaServer(t, dims, nil)
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL
	cfg.SkipHealthCheck = true
	cfg.Dimensions = dims

	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// When: embedding empty string
	embedding, err := embedder.Embed(context.Background(), "")

	// Then: returns zero vector
	require.NoError(t, err)
	assert.Len(t, embedding, dims)
	for _, v := range embedding {
		assert.Equal(t, float32(0), v)
	}
}

func TestOllamaEmbedder_Embed_WhitespaceOnly_ReturnsZeroVector(t *testing.T) {
	// Given: mock Ollama server
	dims := 768
	server := mockOllamaServer(t, dims, nil)
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL
	cfg.SkipHealthCheck = true
	cfg.Dimensions = dims

	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// When: embedding whitespace
	embedding, err := embedder.Embed(context.Background(), "   \t\n  ")

	// Then: returns zero vector
	require.NoError(t, err)
	assert.Len(t, embedding, dims)
}

// ============================================================================
// TS05: Batch Embedding
// ============================================================================

func TestOllamaEmbedder_EmbedBatch_ReturnsCorrectCount(t *testing.T) {
	// Given: mock Ollama server
	dims := 768
	server := mockOllamaServer(t, dims, nil)
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL
	cfg.SkipHealthCheck = true
	cfg.Dimensions = dims

	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	texts := []string{"text one", "text two", "text three"}

	// When: batch embedding
	embeddings, err := embedder.EmbedBatch(context.Background(), texts)

	// Then: returns correct count
	require.NoError(t, err)
	assert.Len(t, embeddings, len(texts))
	for _, emb := range embeddings {
		assert.Len(t, emb, dims)
	}
}

func TestOllamaEmbedder_EmbedBatch_EmptyList_ReturnsEmpty(t *testing.T) {
	// Given: mock Ollama server
	dims := 768
	server := mockOllamaServer(t, dims, nil)
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL
	cfg.SkipHealthCheck = true
	cfg.Dimensions = dims

	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// When: batch embedding empty list
	embeddings, err := embedder.EmbedBatch(context.Background(), []string{})

	// Then: returns empty list
	require.NoError(t, err)
	assert.Empty(t, embeddings)
}

func TestOllamaEmbedder_EmbedBatch_HandlesEmptyStringsInBatch(t *testing.T) {
	// Given: mock Ollama server
	dims := 768
	server := mockOllamaServer(t, dims, nil)
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL
	cfg.SkipHealthCheck = true
	cfg.Dimensions = dims

	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	texts := []string{"text one", "", "text three"}

	// When: batch embedding with empty string
	embeddings, err := embedder.EmbedBatch(context.Background(), texts)

	// Then: returns correct count with zero vector for empty
	require.NoError(t, err)
	assert.Len(t, embeddings, 3)
	// Middle embedding (empty string) should be zero vector
	for _, v := range embeddings[1] {
		assert.Equal(t, float32(0), v)
	}
}

// ============================================================================
// TS06: Error Handling
// ============================================================================

func TestOllamaEmbedder_OllamaUnavailable_ReturnsError(t *testing.T) {
	// Given: config pointing to unavailable server
	cfg := DefaultOllamaConfig()
	cfg.Host = "http://localhost:59999" // Unlikely to be running
	cfg.ConnectTimeout = 100 * time.Millisecond

	// When: creating embedder
	_, err := NewOllamaEmbedder(context.Background(), cfg)

	// Then: returns error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect")
}

func TestOllamaEmbedder_ServerReturns500_ReturnsError(t *testing.T) {
	// Given: mock server returning 500 for embed requests
	var embedCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{{"name": "qwen3-embedding:8b"}},
			})
			return
		}
		if r.URL.Path == "/api/embed" {
			embedCalled = true
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("internal error"))
			return
		}
	}))
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL
	cfg.SkipHealthCheck = true
	cfg.Dimensions = 768
	cfg.MaxRetries = 1

	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// When: embedding
	_, err = embedder.Embed(context.Background(), "test")

	// Then: returns error
	require.Error(t, err)
	assert.True(t, embedCalled, "embed endpoint should be called")
	assert.Contains(t, err.Error(), "500")
}

// ============================================================================
// TS07: Metadata Methods
// ============================================================================

func TestOllamaEmbedder_Dimensions_ReturnsConfigured(t *testing.T) {
	// Given: embedder with known dimensions
	dims := 1024
	server := mockOllamaServer(t, dims, nil)
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL
	cfg.SkipHealthCheck = true
	cfg.Dimensions = dims

	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// Then: returns configured dimensions
	assert.Equal(t, dims, embedder.Dimensions())
}

func TestOllamaEmbedder_ModelName_ReturnsModel(t *testing.T) {
	// Given: embedder
	server := mockOllamaServer(t, 768, nil)
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL
	cfg.SkipHealthCheck = true
	cfg.Dimensions = 768

	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// Then: returns model name
	assert.NotEmpty(t, embedder.ModelName())
}

// ============================================================================
// TS08: Resource Cleanup
// ============================================================================

func TestOllamaEmbedder_Close_IsIdempotent(t *testing.T) {
	// Given: embedder
	server := mockOllamaServer(t, 768, nil)
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL
	cfg.SkipHealthCheck = true
	cfg.Dimensions = 768

	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)

	// When: closing multiple times
	err1 := embedder.Close()
	err2 := embedder.Close()
	err3 := embedder.Close()

	// Then: no errors
	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.NoError(t, err3)
}

func TestOllamaEmbedder_Embed_AfterClose_ReturnsError(t *testing.T) {
	// Given: closed embedder
	server := mockOllamaServer(t, 768, nil)
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL
	cfg.SkipHealthCheck = true
	cfg.Dimensions = 768

	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)
	_ = embedder.Close()

	// When: embedding after close
	_, err = embedder.Embed(context.Background(), "test")

	// Then: returns error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

// ============================================================================
// TS09: Context Cancellation
// ============================================================================

func TestOllamaEmbedder_Embed_ContextCancellation(t *testing.T) {
	// Given: slow mock server for embed requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{{"name": "qwen3-embedding:8b"}},
			})
			return
		}
		if r.URL.Path == "/api/embed" {
			// Slow response to trigger timeout
			time.Sleep(5 * time.Second)
		}
	}))
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL
	cfg.SkipHealthCheck = true
	cfg.Dimensions = 768
	cfg.Timeout = 10 * time.Second
	cfg.MaxRetries = 1

	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// When: embedding with cancelled context
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = embedder.Embed(ctx, "test")

	// Then: returns context error
	require.Error(t, err)
}

func TestOllamaEmbedder_Embed_ContextCancellation_ExitsQuickly(t *testing.T) {
	// Given: mock server that delays response (simulates slow GPU processing)
	// Use 2s delay - long enough to test cancellation, short enough for fast test
	serverDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{{"name": "qwen3-embedding:8b"}},
			})
			return
		}
		if r.URL.Path == "/api/embed" {
			// Wait until test signals completion or short timeout for fast cleanup
			select {
			case <-serverDone:
				return
			case <-time.After(2 * time.Second):
			}
		}
	}))
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL
	cfg.SkipHealthCheck = true
	cfg.Dimensions = 768
	cfg.MaxRetries = 1

	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// When: embedding with context that will be cancelled after 200ms
	ctx, cancel := context.WithCancel(context.Background())

	var embedErr error
	var duration time.Duration
	done := make(chan struct{})

	go func() {
		start := time.Now()
		_, embedErr = embedder.Embed(ctx, "test")
		duration = time.Since(start)
		close(done)
	}()

	// Cancel context after 200ms (simulating Ctrl+C)
	time.Sleep(200 * time.Millisecond)
	cancel()

	// Wait for embed to return
	select {
	case <-done:
		// Success - embed returned
	case <-time.After(2 * time.Second):
		t.Fatal("Embed did not exit within 2 seconds after context cancellation")
	}

	// Then: should exit within 500ms of cancellation and return context error
	assert.ErrorIs(t, embedErr, context.Canceled)
	assert.Less(t, duration, 500*time.Millisecond, "Should exit quickly after cancellation, not wait for HTTP timeout")

	// Signal server handler to exit for fast cleanup
	close(serverDone)
}

// ============================================================================
// TS10: Health Check and Model Discovery
// ============================================================================

func TestOllamaEmbedder_HealthCheck_FindsModel(t *testing.T) {
	// Given: mock server with model available
	var healthChecked atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			healthChecked.Store(true)
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{
					{"name": "qwen3-embedding:8b"},
				},
			})
			return
		}
		if r.URL.Path == "/api/embed" {
			// Return embedding for dimension detection
			json.NewEncoder(w).Encode(map[string]any{
				"embeddings": [][]float64{make([]float64, 768)},
			})
			return
		}
	}))
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL

	// When: creating embedder
	embedder, err := NewOllamaEmbedder(context.Background(), cfg)

	// Then: health check was performed
	require.NoError(t, err)
	assert.True(t, healthChecked.Load())
	defer func() { _ = embedder.Close() }()
}

func TestOllamaEmbedder_Available_ReturnsTrueWhenReady(t *testing.T) {
	// Given: working embedder
	server := mockOllamaServerWithTags(t, 768, []string{"qwen3-embedding:8b"})
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL

	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// Then: Available returns true
	assert.True(t, embedder.Available(context.Background()))
}

// ============================================================================
// TS11: Dimension Auto-Detection
// ============================================================================

func TestOllamaEmbedder_DimensionAutoDetection(t *testing.T) {
	// Given: mock server returning 4096-dim embeddings
	expectedDims := 4096
	server := mockOllamaServerWithTags(t, expectedDims, []string{"qwen3-embedding:8b"})
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL
	cfg.Dimensions = 0 // Auto-detect

	// When: creating embedder
	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// Then: dimensions are auto-detected
	assert.Equal(t, expectedDims, embedder.Dimensions())
}

// ============================================================================
// TS12: Retry Logic
// ============================================================================

func TestOllamaEmbedder_RetryOnTransientError(t *testing.T) {
	// Given: server that fails twice then succeeds for embed calls
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{{"name": "qwen3-embedding:8b"}},
			})
			return
		}
		if r.URL.Path == "/api/embed" {
			count := attempts.Add(1)
			if count < 3 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			// Success on 3rd attempt
			embedding := make([]float64, 768)
			for i := range embedding {
				embedding[i] = 0.1
			}
			json.NewEncoder(w).Encode(map[string]any{
				"embeddings": [][]float64{embedding},
			})
		}
	}))
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL
	cfg.SkipHealthCheck = true
	cfg.Dimensions = 768
	cfg.MaxRetries = 3

	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// When: embedding
	_, err = embedder.Embed(context.Background(), "test")

	// Then: succeeds after retries
	require.NoError(t, err)
	assert.Equal(t, int32(3), attempts.Load())
}

// ============================================================================
// TS13: Native Batch API Usage
// ============================================================================

func TestOllamaEmbedder_EmbedBatch_UsesNativeBatchAPI(t *testing.T) {
	// Given: mock server that checks for batch input
	var receivedBatch atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{{"name": "qwen3-embedding:8b"}},
			})
			return
		}
		if r.URL.Path == "/api/embed" {
			body, _ := io.ReadAll(r.Body)
			var req map[string]any
			json.Unmarshal(body, &req)

			// Check if input is an array (batch)
			if input, ok := req["input"].([]any); ok && len(input) > 1 {
				receivedBatch.Store(true)
			}

			// Return embeddings
			inputLen := 1
			if arr, ok := req["input"].([]any); ok {
				inputLen = len(arr)
			}

			embeddings := make([][]float64, inputLen)
			for i := range embeddings {
				embeddings[i] = make([]float64, 768)
				for j := range embeddings[i] {
					embeddings[i][j] = 0.1
				}
			}
			json.NewEncoder(w).Encode(map[string]any{
				"embeddings": embeddings,
			})
		}
	}))
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL
	cfg.BatchSize = 10

	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// When: batch embedding multiple texts
	texts := []string{"one", "two", "three"}
	_, err = embedder.EmbedBatch(context.Background(), texts)

	// Then: uses native batch API
	require.NoError(t, err)
	assert.True(t, receivedBatch.Load(), "should use native batch API")
}

// ============================================================================
// TS14: BUG-052 - Context Timeout Not Overridden by HTTP Client
// ============================================================================

func TestOllamaEmbedder_ContextTimeout_NotOverriddenByHTTPClient(t *testing.T) {
	// BUG-052: Verify that context timeout is respected, not overridden by http.Client.Timeout
	// This test ensures the fix works: HTTP client should have no static timeout,
	// allowing context-based progressive timeouts to control request duration.

	// Given: mock server that takes 65 seconds to respond (> old 60s http.Client.Timeout)
	// Using a shorter delay for actual test (2s) to keep test fast
	requestDuration := 2 * time.Second
	var requestReceived bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{{"name": "qwen3-embedding:8b"}},
			})
			return
		}
		if r.URL.Path == "/api/embed" {
			requestReceived = true
			// Simulate slow response (but faster than context timeout)
			time.Sleep(requestDuration)

			embedding := make([]float64, 768)
			for i := range embedding {
				embedding[i] = 0.1
			}
			json.NewEncoder(w).Encode(map[string]any{
				"embeddings": [][]float64{embedding},
			})
		}
	}))
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL
	cfg.SkipHealthCheck = true
	cfg.Dimensions = 768
	cfg.MaxRetries = 1
	// Old bug: cfg.Timeout = 60s would cause http.Client to abort at 60s
	// even if context timeout was longer. Now we don't set http.Client.Timeout.

	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// When: embedding with context timeout longer than request duration
	// The context timeout (5s) > request duration (2s), so it should succeed
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	embedding, err := embedder.Embed(ctx, "test")
	duration := time.Since(start)

	// Then: request succeeds (not aborted by fixed HTTP client timeout)
	require.NoError(t, err, "BUG-052: request should not be aborted by HTTP client timeout")
	assert.True(t, requestReceived, "server should have received request")
	assert.NotEmpty(t, embedding, "embedding should be returned")
	assert.GreaterOrEqual(t, duration, requestDuration, "should have waited for response")
}

func TestOllamaEmbedder_ProgressiveTimeout_UsedInsteadOfFixedTimeout(t *testing.T) {
	// BUG-052: Verify that progressive timeout is calculated and used

	// Given: embedder with batch tracking
	dims := 768
	server := mockOllamaServer(t, dims, nil)
	defer server.Close()

	cfg := DefaultOllamaConfig()
	cfg.Host = server.URL
	cfg.SkipHealthCheck = true
	cfg.Dimensions = dims
	cfg.TimeoutProgression = 2.0     // 100% increase per 1000 chunks
	cfg.RetryTimeoutMultiplier = 1.5 // 50% increase per retry

	embedder, err := NewOllamaEmbedder(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// When: simulating batch 198 (6336 chunks at batch size 32)
	embedder.SetBatchIndex(198)
	embedder.SetFinalBatch(true)

	// Then: progressive timeout should be much higher than default 60s
	// Expected: baseTimeout(120s) * progression(3.0 capped) * finalBoost(1.5) = 540s
	timeout := embedder.getProgressiveTimeout(0)

	// The timeout should be significantly higher than old 60s limit
	assert.Greater(t, timeout, 60*time.Second,
		"BUG-052: progressive timeout should exceed old 60s HTTP client timeout")
	assert.GreaterOrEqual(t, timeout, 120*time.Second,
		"Progressive timeout should be at least base timeout")
}

// ============================================================================
// Test Helpers
// ============================================================================

// mockOllamaServer creates a mock Ollama server for testing
func mockOllamaServer(t *testing.T, dims int, customHandler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if customHandler != nil {
			customHandler(w, r)
			return
		}

		if r.URL.Path == "/api/tags" {
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{{"name": "qwen3-embedding:8b"}},
			})
			return
		}

		if r.URL.Path == "/api/embed" {
			body, _ := io.ReadAll(r.Body)
			var req map[string]any
			json.Unmarshal(body, &req)

			// Determine number of embeddings to return
			count := 1
			if input, ok := req["input"].([]any); ok {
				count = len(input)
			}

			embeddings := make([][]float64, count)
			for i := range embeddings {
				embeddings[i] = generateMockEmbedding(fmt.Sprintf("text%d", i), dims)
			}

			json.NewEncoder(w).Encode(map[string]any{
				"model":      "qwen3-embedding:8b",
				"embeddings": embeddings,
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
}

// mockOllamaServerWithTags creates a mock server with specific models
func mockOllamaServerWithTags(t *testing.T, dims int, models []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			modelList := make([]map[string]any, len(models))
			for i, m := range models {
				modelList[i] = map[string]any{"name": m}
			}
			json.NewEncoder(w).Encode(map[string]any{"models": modelList})
			return
		}

		if r.URL.Path == "/api/embed" {
			body, _ := io.ReadAll(r.Body)
			var req map[string]any
			json.Unmarshal(body, &req)

			count := 1
			if input, ok := req["input"].([]any); ok {
				count = len(input)
			}

			embeddings := make([][]float64, count)
			for i := range embeddings {
				embeddings[i] = generateMockEmbedding(fmt.Sprintf("text%d", i), dims)
			}

			json.NewEncoder(w).Encode(map[string]any{
				"model":      models[0],
				"embeddings": embeddings,
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
}

// generateMockEmbedding creates a deterministic mock embedding
func generateMockEmbedding(text string, dims int) []float64 {
	embedding := make([]float64, dims)
	if strings.TrimSpace(text) == "" {
		return embedding
	}

	// Create deterministic non-zero values
	for i := range embedding {
		charSum := 0.0
		for j, c := range text {
			charSum += float64(c) * float64(j+1)
		}
		embedding[i] = float64(i+1) / float64(dims) * (charSum / 1000.0)
	}

	// Normalize
	var sumSq float64
	for _, v := range embedding {
		sumSq += v * v
	}
	if sumSq > 0 {
		mag := 1.0 / (sumSq * sumSq)
		for i := range embedding {
			embedding[i] *= mag
		}
	}

	return embedding
}
