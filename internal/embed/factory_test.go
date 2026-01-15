package embed

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Factory Environment Variable Tests
// ============================================================================

func TestNewEmbedder_OllamaTimeoutEnvVar(t *testing.T) {
	// Skip if Ollama is not available (this is an integration test pattern)
	// For unit testing, we just verify the config is applied correctly

	tests := []struct {
		name     string
		envValue string
		want     time.Duration
	}{
		{
			name:     "valid duration seconds",
			envValue: "120s",
			want:     120 * time.Second,
		},
		{
			name:     "valid duration minutes",
			envValue: "5m",
			want:     5 * time.Minute,
		},
		{
			name:     "invalid duration uses default",
			envValue: "invalid",
			want:     DefaultTimeout, // Should fall back to default
		},
		{
			name:     "empty uses default",
			envValue: "",
			want:     DefaultTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore env
			orig := os.Getenv("AMANMCP_OLLAMA_TIMEOUT")
			defer os.Setenv("AMANMCP_OLLAMA_TIMEOUT", orig)

			if tt.envValue != "" {
				os.Setenv("AMANMCP_OLLAMA_TIMEOUT", tt.envValue)
			} else {
				os.Unsetenv("AMANMCP_OLLAMA_TIMEOUT")
			}

			// Create config and apply env var logic (extracted from factory)
			cfg := DefaultOllamaConfig()
			if timeoutStr := os.Getenv("AMANMCP_OLLAMA_TIMEOUT"); timeoutStr != "" {
				if timeout, err := time.ParseDuration(timeoutStr); err == nil {
					cfg.Timeout = timeout
				}
			}

			assert.Equal(t, tt.want, cfg.Timeout)
		})
	}
}

func TestDefaultTimeout_IsNowSixtySeconds(t *testing.T) {
	// Verify the constant change
	assert.Equal(t, 60*time.Second, DefaultTimeout,
		"DefaultTimeout should be 60s to handle large batch embeddings")
}

func TestNewEmbedder_StaticProvider_DoesNotNeedTimeout(t *testing.T) {
	// Static embedder should work regardless of timeout settings
	ctx := context.Background()
	embedder, err := NewEmbedder(ctx, ProviderStatic, "")
	require.NoError(t, err)
	defer embedder.Close()

	assert.Equal(t, "static768", embedder.ModelName())
	assert.True(t, embedder.Available(ctx))
}

// ============================================================================
// BUG-052: Thermal Config Tests
// ============================================================================

func TestSetThermalConfig_AppliesConfigFileSettings(t *testing.T) {
	// Save original state
	origConfig := globalThermalConfig
	defer func() { globalThermalConfig = origConfig }()

	// Given: thermal config from config.yaml
	cfg := ThermalConfig{
		InterBatchDelay:        500 * time.Millisecond,
		TimeoutProgression:     2.0,
		RetryTimeoutMultiplier: 1.5,
	}

	// When: setting thermal config
	SetThermalConfig(cfg)

	// Then: global config is updated
	assert.Equal(t, 500*time.Millisecond, globalThermalConfig.InterBatchDelay)
	assert.Equal(t, 2.0, globalThermalConfig.TimeoutProgression)
	assert.Equal(t, 1.5, globalThermalConfig.RetryTimeoutMultiplier)
}

func TestSetThermalConfig_EnvVarsOverrideConfigFile(t *testing.T) {
	// Save and restore env vars
	origDelay := os.Getenv("AMANMCP_INTER_BATCH_DELAY")
	origProg := os.Getenv("AMANMCP_TIMEOUT_PROGRESSION")
	origRetry := os.Getenv("AMANMCP_RETRY_TIMEOUT_MULTIPLIER")
	defer func() {
		os.Setenv("AMANMCP_INTER_BATCH_DELAY", origDelay)
		os.Setenv("AMANMCP_TIMEOUT_PROGRESSION", origProg)
		os.Setenv("AMANMCP_RETRY_TIMEOUT_MULTIPLIER", origRetry)
	}()

	// Save original state
	origConfig := globalThermalConfig
	defer func() { globalThermalConfig = origConfig }()

	// Given: config file sets values
	SetThermalConfig(ThermalConfig{
		InterBatchDelay:        200 * time.Millisecond,
		TimeoutProgression:     1.5,
		RetryTimeoutMultiplier: 1.2,
	})

	// And: env vars set different values
	os.Setenv("AMANMCP_INTER_BATCH_DELAY", "1s")
	os.Setenv("AMANMCP_TIMEOUT_PROGRESSION", "2.5")
	os.Setenv("AMANMCP_RETRY_TIMEOUT_MULTIPLIER", "1.8")

	// When: creating Ollama config
	cfg := DefaultOllamaConfig()

	// Apply global config first
	if globalThermalConfig.InterBatchDelay > 0 {
		cfg.InterBatchDelay = globalThermalConfig.InterBatchDelay
	}
	if globalThermalConfig.TimeoutProgression >= 1.0 {
		cfg.TimeoutProgression = globalThermalConfig.TimeoutProgression
	}
	if globalThermalConfig.RetryTimeoutMultiplier >= 1.0 {
		cfg.RetryTimeoutMultiplier = globalThermalConfig.RetryTimeoutMultiplier
	}

	// Apply env var overrides (simulating factory logic)
	if delayStr := os.Getenv("AMANMCP_INTER_BATCH_DELAY"); delayStr != "" {
		if delay, err := time.ParseDuration(delayStr); err == nil {
			cfg.InterBatchDelay = delay
		}
	}
	if progStr := os.Getenv("AMANMCP_TIMEOUT_PROGRESSION"); progStr != "" {
		if prog, err := parseFloat64(progStr); err == nil {
			cfg.TimeoutProgression = prog
		}
	}
	if retryStr := os.Getenv("AMANMCP_RETRY_TIMEOUT_MULTIPLIER"); retryStr != "" {
		if mult, err := parseFloat64(retryStr); err == nil {
			cfg.RetryTimeoutMultiplier = mult
		}
	}

	// Then: env vars take precedence over config file
	assert.Equal(t, 1*time.Second, cfg.InterBatchDelay, "env var should override config file")
	assert.Equal(t, 2.5, cfg.TimeoutProgression, "env var should override config file")
	assert.Equal(t, 1.8, cfg.RetryTimeoutMultiplier, "env var should override config file")
}

func TestDefaultTimeouts_IncreasedForThermalThrottling(t *testing.T) {
	// BUG-052: Verify increased default timeouts
	assert.Equal(t, 120*time.Second, DefaultWarmTimeout,
		"DefaultWarmTimeout should be 120s for thermal throttling")
	assert.Equal(t, 180*time.Second, DefaultColdTimeout,
		"DefaultColdTimeout should be 180s for slower hardware")
}

// ============================================================================
// MLX Config Tests
// ============================================================================

func TestSetMLXConfig_AppliesConfigFileSettings(t *testing.T) {
	// Save original state
	origConfig := globalMLXConfig
	defer func() { globalMLXConfig = origConfig }()

	// Given: MLX config from config.yaml
	cfg := MLXServerConfig{
		Endpoint: "http://my-server:9000",
		Model:    "medium",
	}

	// When: setting MLX config
	SetMLXConfig(cfg)

	// Then: global config is updated
	assert.Equal(t, "http://my-server:9000", globalMLXConfig.Endpoint)
	assert.Equal(t, "medium", globalMLXConfig.Model)
}

func TestSetMLXConfig_EnvVarsOverrideConfigFile(t *testing.T) {
	// Save and restore env vars
	origEndpoint := os.Getenv("AMANMCP_MLX_ENDPOINT")
	origModel := os.Getenv("AMANMCP_MLX_MODEL")
	defer func() {
		os.Setenv("AMANMCP_MLX_ENDPOINT", origEndpoint)
		os.Setenv("AMANMCP_MLX_MODEL", origModel)
	}()

	// Save original state
	origConfig := globalMLXConfig
	defer func() { globalMLXConfig = origConfig }()

	// Given: config file sets values
	SetMLXConfig(MLXServerConfig{
		Endpoint: "http://config-server:8000",
		Model:    "small",
	})

	// And: env vars set different values
	os.Setenv("AMANMCP_MLX_ENDPOINT", "http://env-server:9000")
	os.Setenv("AMANMCP_MLX_MODEL", "large")

	// When: creating MLX config in newMLXWithFallback
	// (We simulate what newMLXWithFallback does)
	cfg := DefaultMLXConfig()

	// Apply global config first
	if globalMLXConfig.Endpoint != "" {
		cfg.Endpoint = globalMLXConfig.Endpoint
	}
	if globalMLXConfig.Model != "" {
		cfg.Model = globalMLXConfig.Model
	}

	// Environment variables override config file settings
	if endpoint := os.Getenv("AMANMCP_MLX_ENDPOINT"); endpoint != "" {
		cfg.Endpoint = endpoint
	}
	if model := os.Getenv("AMANMCP_MLX_MODEL"); model != "" {
		cfg.Model = model
	}

	// Then: env vars take precedence over config file
	assert.Equal(t, "http://env-server:9000", cfg.Endpoint, "env var should override config file")
	assert.Equal(t, "large", cfg.Model, "env var should override config file")
}

// ============================================================================
// BUG-041: Explicit Embedder Selection Tests (No Silent Fallback)
// ============================================================================

func TestNewEmbedder_ExplicitOllama_OllamaUnavailable_ReturnsError(t *testing.T) {
	// Save and restore env vars
	origEmbedder := os.Getenv("AMANMCP_EMBEDDER")
	origHost := os.Getenv("AMANMCP_OLLAMA_HOST")
	defer func() {
		os.Setenv("AMANMCP_EMBEDDER", origEmbedder)
		os.Setenv("AMANMCP_OLLAMA_HOST", origHost)
	}()

	// Given: User explicitly requests Ollama
	os.Setenv("AMANMCP_EMBEDDER", "ollama")
	// And: Ollama is unavailable (point to non-existent server)
	os.Setenv("AMANMCP_OLLAMA_HOST", "http://localhost:59999")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// When: Creating embedder
	embedder, err := NewEmbedder(ctx, ProviderOllama, "")

	// Then: Should return error (NOT silently fallback to static)
	require.Error(t, err, "explicit embedder should error when unavailable, not fallback")
	assert.Nil(t, embedder)
	assert.Contains(t, err.Error(), "ollama unavailable")
}

func TestNewEmbedder_AutoDetect_OllamaFails_ReturnsError(t *testing.T) {
	// BUG-073: Auto-detect no longer falls back to static - returns error
	// Save and restore env vars
	origEmbedder := os.Getenv("AMANMCP_EMBEDDER")
	origHost := os.Getenv("AMANMCP_OLLAMA_HOST")
	defer func() {
		os.Setenv("AMANMCP_EMBEDDER", origEmbedder)
		os.Setenv("AMANMCP_OLLAMA_HOST", origHost)
	}()

	// Given: No explicit embedder selection (auto-detect)
	os.Unsetenv("AMANMCP_EMBEDDER")
	// And: Ollama is unavailable
	os.Setenv("AMANMCP_OLLAMA_HOST", "http://localhost:59999")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// When: Creating embedder
	embedder, err := NewEmbedder(ctx, ProviderOllama, "")

	// Then: Should return error with helpful message (BUG-073: no silent fallback)
	require.Error(t, err, "auto-detect should error when embedder unavailable")
	assert.Nil(t, embedder)
	assert.Contains(t, err.Error(), "ollama unavailable")
	assert.Contains(t, err.Error(), "ollama serve") // Helpful fix suggestion
}

func TestNewEmbedder_ExplicitStatic_AlwaysSucceeds(t *testing.T) {
	// Save and restore env var
	origEmbedder := os.Getenv("AMANMCP_EMBEDDER")
	defer os.Setenv("AMANMCP_EMBEDDER", origEmbedder)

	// Given: User explicitly requests static
	os.Setenv("AMANMCP_EMBEDDER", "static")

	ctx := context.Background()

	// When: Creating embedder
	embedder, err := NewEmbedder(ctx, ProviderOllama, "")

	// Then: Should return static embedder
	require.NoError(t, err)
	require.NotNil(t, embedder)
	defer func() { _ = embedder.Close() }()
	assert.Equal(t, "static768", embedder.ModelName())
}

func TestNewEmbedder_ExplicitMLX_MLXUnavailable_ReturnsError(t *testing.T) {
	// Save and restore env vars
	origEmbedder := os.Getenv("AMANMCP_EMBEDDER")
	origEndpoint := os.Getenv("AMANMCP_MLX_ENDPOINT")
	defer func() {
		os.Setenv("AMANMCP_EMBEDDER", origEmbedder)
		os.Setenv("AMANMCP_MLX_ENDPOINT", origEndpoint)
	}()

	// Given: User explicitly requests MLX
	os.Setenv("AMANMCP_EMBEDDER", "mlx")
	// And: MLX server is unavailable
	os.Setenv("AMANMCP_MLX_ENDPOINT", "http://localhost:59998")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// When: Creating embedder
	embedder, err := NewEmbedder(ctx, ProviderMLX, "")

	// Then: Should return error (NOT silently fallback)
	require.Error(t, err, "explicit MLX should error when unavailable, not fallback")
	assert.Nil(t, embedder)
	assert.Contains(t, err.Error(), "mlx unavailable")
}

// ============================================================================
// isOllamaModelName Tests
// ============================================================================

func TestIsOllamaModelName_WithTag(t *testing.T) {
	// Models with colon tag are definitely Ollama
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{
			name:  "ollama model with tag",
			model: "nomic-embed-text:latest",
			want:  true,
		},
		{
			name:  "qwen3 with size tag",
			model: "qwen3-embedding:8b",
			want:  true,
		},
		{
			name:  "model with version tag",
			model: "bge-small:v1.5",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isOllamaModelName(tt.model)
			assert.Equal(t, tt.want, got, "isOllamaModelName(%q)", tt.model)
		})
	}
}

func TestIsOllamaModelName_GGUFExtension(t *testing.T) {
	// Models with .gguf extension are NOT Ollama
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{
			name:  "gguf file",
			model: "model.gguf",
			want:  false,
		},
		{
			name:  "gguf with path",
			model: "/path/to/nomic-embed-text.gguf",
			want:  false,
		},
		{
			name:  "uppercase GGUF",
			model: "model.GGUF",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isOllamaModelName(tt.model)
			assert.Equal(t, tt.want, got, "isOllamaModelName(%q)", tt.model)
		})
	}
}

func TestIsOllamaModelName_VersionPattern(t *testing.T) {
	// Models with -vX.Y version pattern are likely GGUF, not Ollama
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{
			name:  "model with version number",
			model: "nomic-embed-text-v1.5",
			want:  false,
		},
		{
			name:  "bge with version",
			model: "bge-small-en-v1.5",
			want:  false,
		},
		{
			name:  "v1 suffix",
			model: "model-v1",
			want:  false,
		},
		{
			name:  "v2 suffix",
			model: "model-v2",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isOllamaModelName(tt.model)
			assert.Equal(t, tt.want, got, "isOllamaModelName(%q)", tt.model)
		})
	}
}

func TestIsOllamaModelName_PlainNames(t *testing.T) {
	// Plain model names without indicators return false (conservative)
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{
			name:  "plain name no tag",
			model: "nomic-embed-text",
			want:  false, // Conservative: no indicators = not Ollama
		},
		{
			name:  "single word",
			model: "embedding",
			want:  false,
		},
		{
			name:  "empty string",
			model: "",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isOllamaModelName(tt.model)
			assert.Equal(t, tt.want, got, "isOllamaModelName(%q)", tt.model)
		})
	}
}
