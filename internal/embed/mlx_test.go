package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestMLXEmbedderInterface verifies MLXEmbedder implements Embedder interface
func TestMLXEmbedderInterface(t *testing.T) {
	// This is a compile-time check - if it compiles, MLXEmbedder implements Embedder
	var _ Embedder = (*MLXEmbedder)(nil)
}

// TestNewMLXEmbedder tests MLXEmbedder creation with mock server
func TestNewMLXEmbedder(t *testing.T) {
	// Create mock MLX server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":       "healthy",
				"model_status": "ready",
				"loaded_model": "large",
	})
		case "/models":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"models": map[string]interface{}{
					"small":  map[string]int{"dimensions": 1024},
					"medium": map[string]int{"dimensions": 2560},
					"large":  map[string]int{"dimensions": 4096},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := MLXConfig{
		Endpoint: server.URL,
		Model:    "large",
	}

	ctx := context.Background()
	embedder, err := NewMLXEmbedder(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create MLXEmbedder: %v", err)
	}
	defer embedder.Close()

	// Verify dimensions
	if embedder.Dimensions() != 4096 {
		t.Errorf("expected dimensions 4096, got %d", embedder.Dimensions())
	}

	// Verify model name
	if embedder.ModelName() != "mlx-qwen3-embedding-large" {
		t.Errorf("expected model name mlx-qwen3-embedding-large, got %s", embedder.ModelName())
	}
}

// TestMLXEmbedder_Embed tests single text embedding
func TestMLXEmbedder_Embed(t *testing.T) {
	// Create mock server that returns embeddings
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":       "healthy",
				"model_status": "ready",
				"loaded_model": "large",
	})
		case "/models":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"models": map[string]interface{}{
					"large": map[string]int{"dimensions": 4096},
				},
			})
		case "/embed":
			// Return a 4096-dimensional embedding
			embedding := make([]float64, 4096)
			for i := range embedding {
				embedding[i] = float64(i) / 4096.0
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"embedding": embedding,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := MLXConfig{
		Endpoint: server.URL,
		Model:    "large",
	}

	ctx := context.Background()
	embedder, err := NewMLXEmbedder(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create MLXEmbedder: %v", err)
	}
	defer embedder.Close()

	// Test single embedding
	embedding, err := embedder.Embed(ctx, "Hello world")
	if err != nil {
		t.Fatalf("failed to embed text: %v", err)
	}

	if len(embedding) != 4096 {
		t.Errorf("expected embedding length 4096, got %d", len(embedding))
	}
}

// TestMLXEmbedder_EmbedBatch tests batch embedding
func TestMLXEmbedder_EmbedBatch(t *testing.T) {
	// Create mock server that returns batch embeddings
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":       "healthy",
				"model_status": "ready",
				"loaded_model": "large",
	})
		case "/models":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"models": map[string]interface{}{
					"large": map[string]int{"dimensions": 4096},
				},
			})
		case "/embed_batch":
			// Decode request to get number of texts
			var req map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&req)
			texts := req["texts"].([]interface{})

			// Return embeddings for each text
			embeddings := make([][]float64, len(texts))
			for i := range embeddings {
				embeddings[i] = make([]float64, 4096)
				for j := range embeddings[i] {
					embeddings[i][j] = float64(i*1000+j) / 4096.0
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"embeddings": embeddings,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := MLXConfig{
		Endpoint: server.URL,
		Model:    "large",
	}

	ctx := context.Background()
	embedder, err := NewMLXEmbedder(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create MLXEmbedder: %v", err)
	}
	defer embedder.Close()

	// Test batch embedding
	texts := []string{"Hello", "World", "Test"}
	embeddings, err := embedder.EmbedBatch(ctx, texts)
	if err != nil {
		t.Fatalf("failed to embed batch: %v", err)
	}

	if len(embeddings) != 3 {
		t.Errorf("expected 3 embeddings, got %d", len(embeddings))
	}

	for i, emb := range embeddings {
		if len(emb) != 4096 {
			t.Errorf("embedding %d: expected length 4096, got %d", i, len(emb))
		}
	}
}

// TestMLXEmbedder_Available tests availability check
func TestMLXEmbedder_Available(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		available bool
	}{
		{
			name: "healthy server",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/health":
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"status":       "healthy",
						"model_status": "ready",
					})
				case "/models":
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"models": map[string]interface{}{
							"large": map[string]int{"dimensions": 4096},
						},
					})
				}
			},
			available: true,
		},
		{
			name: "unhealthy server",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/health":
					w.WriteHeader(http.StatusServiceUnavailable)
				case "/models":
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"models": map[string]interface{}{
							"large": map[string]int{"dimensions": 4096},
						},
					})
				}
			},
			available: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			cfg := MLXConfig{
				Endpoint:        server.URL,
				Model:           "large",
				SkipHealthCheck: true, // Skip health check during creation
			}

			ctx := context.Background()
			embedder, err := NewMLXEmbedder(ctx, cfg)
			if err != nil {
				t.Fatalf("failed to create MLXEmbedder: %v", err)
			}
			defer embedder.Close()

			available := embedder.Available(ctx)
			if available != tt.available {
				t.Errorf("expected available=%v, got %v", tt.available, available)
			}
		})
	}
}

// TestMLXEmbedder_EmptyTexts tests handling of empty input
func TestMLXEmbedder_EmptyTexts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "healthy", "model_status": "ready", "loaded_model": "large",
	})
		case "/models":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"models": map[string]interface{}{"large": map[string]int{"dimensions": 4096}},
			})
		}
	}))
	defer server.Close()

	cfg := MLXConfig{
		Endpoint: server.URL,
		Model:    "large",
	}

	ctx := context.Background()
	embedder, err := NewMLXEmbedder(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create MLXEmbedder: %v", err)
	}
	defer embedder.Close()

	// Empty batch should return empty result
	embeddings, err := embedder.EmbedBatch(ctx, []string{})
	if err != nil {
		t.Fatalf("unexpected error for empty batch: %v", err)
	}
	if len(embeddings) != 0 {
		t.Errorf("expected 0 embeddings for empty batch, got %d", len(embeddings))
	}
}

// TestMLXEmbedder_ModelSizes tests different model sizes
func TestMLXEmbedder_ModelSizes(t *testing.T) {
	tests := []struct {
		model      string
		dimensions int
	}{
		{"small", 1024},
		{"medium", 2560},
		{"large", 4096},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/health":
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"status":       "healthy",
						"model_status": "ready",
						"loaded_model": tt.model,
					})
				case "/models":
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"models": map[string]interface{}{
							"small":  map[string]int{"dimensions": 1024},
							"medium": map[string]int{"dimensions": 2560},
							"large":  map[string]int{"dimensions": 4096},
						},
					})
				}
			}))
			defer server.Close()

			cfg := MLXConfig{
				Endpoint: server.URL,
				Model:    tt.model,
							}

			ctx := context.Background()
			embedder, err := NewMLXEmbedder(ctx, cfg)
			if err != nil {
				t.Fatalf("failed to create MLXEmbedder: %v", err)
			}
			defer embedder.Close()

			if embedder.Dimensions() != tt.dimensions {
				t.Errorf("expected dimensions %d for model %s, got %d",
					tt.dimensions, tt.model, embedder.Dimensions())
			}
		})
	}
}

// TestMLXConfig_Defaults tests default configuration values
func TestMLXConfig_Defaults(t *testing.T) {
	cfg := DefaultMLXConfig()

	if cfg.Endpoint != "http://localhost:9659" {
		t.Errorf("expected default endpoint http://localhost:9659, got %s", cfg.Endpoint)
	}
	// TASK-MEM1: Default changed from "large" (8B) to "small" (0.6B) for memory efficiency
	if cfg.Model != "small" {
		t.Errorf("expected default model small (TASK-MEM1), got %s", cfg.Model)
	}
	// Note: Timeout field removed - now uses per-request context timeout via getProgressiveTimeout()
}

// TestMLXEmbedder_SetBatchIndex tests thermal management interface
func TestMLXEmbedder_SetBatchIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "healthy", "model_status": "ready", "loaded_model": "large",
	})
		case "/models":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"models": map[string]interface{}{"large": map[string]int{"dimensions": 4096}},
			})
		}
	}))
	defer server.Close()

	cfg := MLXConfig{
		Endpoint: server.URL,
		Model:    "large",
	}

	ctx := context.Background()
	embedder, err := NewMLXEmbedder(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create MLXEmbedder: %v", err)
	}
	defer embedder.Close()

	// These should update the embedder's internal state for progressive timeout
	embedder.SetBatchIndex(10)
	embedder.SetFinalBatch(true)
}

// TestMLXEmbedder_ProgressiveTimeout tests timeout scaling based on batch progress
func TestMLXEmbedder_ProgressiveTimeout(t *testing.T) {
	// Create embedder with skip health check (no server needed for this test)
	cfg := MLXConfig{
		Endpoint:        "http://localhost:9659",
		Model:           "large",
		SkipHealthCheck: true,
	}

	ctx := context.Background()
	embedder, err := NewMLXEmbedder(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create MLXEmbedder: %v", err)
	}
	defer embedder.Close()

	tests := []struct {
		name         string
		batchIndex   int
		isFinalBatch bool
		minTimeout   time.Duration
		maxTimeout   time.Duration
	}{
		{
			name:       "early batch (index 0)",
			batchIndex: 0,
			minTimeout: 60 * time.Second,
			maxTimeout: 61 * time.Second, // Base timeout with minimal progression
		},
		{
			name:       "middle batch (index 50)",
			batchIndex: 50,
			minTimeout: 100 * time.Second, // ~1.8x progression
			maxTimeout: 120 * time.Second,
		},
		{
			name:       "late batch (index 100) - capped at 2x",
			batchIndex: 100,
			minTimeout: 120 * time.Second, // 2x cap
			maxTimeout: 121 * time.Second,
		},
		{
			name:         "final batch with boost",
			batchIndex:   100,
			isFinalBatch: true,
			minTimeout:   180 * time.Second, // 120s * 1.5 final boost
			maxTimeout:   181 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			embedder.SetBatchIndex(tt.batchIndex)
			embedder.SetFinalBatch(tt.isFinalBatch)

			timeout := embedder.getProgressiveTimeout()

			if timeout < tt.minTimeout {
				t.Errorf("timeout %v is less than expected minimum %v", timeout, tt.minTimeout)
			}
			if timeout > tt.maxTimeout {
				t.Errorf("timeout %v is greater than expected maximum %v", timeout, tt.maxTimeout)
			}
		})
	}
}

// TestMLXEmbedder_RetryOnTimeout tests that EmbedBatch retries on transient failures
func TestMLXEmbedder_RetryOnTimeout(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "healthy", "model_status": "ready", "loaded_model": "large",
	})
		case "/models":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"models": map[string]interface{}{"large": map[string]int{"dimensions": 4096}},
			})
		case "/embed_batch":
			attempts++
			if attempts < 2 {
				// First attempt fails with service unavailable
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			// Second attempt succeeds
			var req map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&req)
			texts := req["texts"].([]interface{})
			embeddings := make([][]float64, len(texts))
			for i := range embeddings {
				embeddings[i] = make([]float64, 4096)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"embeddings": embeddings,
			})
		}
	}))
	defer server.Close()

	cfg := MLXConfig{
		Endpoint: server.URL,
		Model:    "large",
	}

	ctx := context.Background()
	embedder, err := NewMLXEmbedder(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create MLXEmbedder: %v", err)
	}
	defer embedder.Close()

	// Should succeed on retry
	embeddings, err := embedder.EmbedBatch(ctx, []string{"test"})
	if err != nil {
		t.Fatalf("expected success on retry, got error: %v", err)
	}
	if len(embeddings) != 1 {
		t.Errorf("expected 1 embedding, got %d", len(embeddings))
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}
