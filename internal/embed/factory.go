package embed

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// ProviderType represents an embedding provider
type ProviderType string

const (
	// ProviderOllama uses Ollama API for embeddings (default on all platforms, cross-platform)
	ProviderOllama ProviderType = "ollama"

	// ProviderMLX uses MLX for embeddings (opt-in on Apple Silicon, ~1.7x faster but higher RAM)
	ProviderMLX ProviderType = "mlx"

	// ProviderStatic uses hash-based embeddings (fallback when all others unavailable)
	ProviderStatic ProviderType = "static"
)

// NewEmbedder creates an embedder based on provider type with automatic fallback.
// The AMANMCP_EMBEDDER environment variable can override the provider:
//   - "ollama": Use OllamaEmbedder (default on all platforms, cross-platform)
//   - "mlx": Use MLXEmbedder (opt-in on Apple Silicon, ~1.7x faster but higher RAM)
//   - "static": Use StaticEmbedder768 (fallback when all others unavailable)
//
// ADR-037: Ollama is now the default on all platforms for lower RAM usage.
// MLX is still available via AMANMCP_EMBEDDER=mlx for users who prefer speed over RAM.
//
// Query embedding caching is enabled by default (saves 50-200ms per repeated query).
// Set AMANMCP_EMBED_CACHE=false to disable caching.
func NewEmbedder(ctx context.Context, provider ProviderType, model string) (Embedder, error) {
	var embedder Embedder
	var err error

	// Check for environment variable override
	// BUG-041: Track explicit selection to prevent silent fallback
	envProvider := os.Getenv("AMANMCP_EMBEDDER")
	explicitSelection := envProvider != ""
	if envProvider != "" {
		switch strings.ToLower(envProvider) {
		case "mlx":
			embedder, err = newMLXWithFallback(ctx, explicitSelection)
		case "ollama":
			embedder, err = newOllamaWithFallback(ctx, model, explicitSelection)
		case "static":
			embedder, err = NewStaticEmbedder768(), nil
		}
	}

	// If no override or unrecognized, use provider switch
	// These are auto-detection scenarios, so allow fallback (explicitSelection = false)
	if embedder == nil && err == nil {
		switch provider {
		case ProviderMLX:
			embedder, err = newMLXWithFallback(ctx, false)

		case ProviderOllama:
			embedder, err = newOllamaWithFallback(ctx, model, false)

		case ProviderStatic:
			embedder, err = NewStaticEmbedder768(), nil

		default:
			// Default to MLX on Apple Silicon, otherwise Ollama
			embedder, err = newDefaultWithFallback(ctx, model)
		}
	}

	if err != nil {
		return nil, err
	}

	// Wrap with cache unless disabled (QW-1: saves 50-200ms per repeated query)
	if !isCacheDisabled() {
		embedder = NewCachedEmbedderWithDefaults(embedder)
	}

	return embedder, nil
}

// isCacheDisabled checks if embedding cache is disabled via environment.
func isCacheDisabled() bool {
	v := strings.ToLower(os.Getenv("AMANMCP_EMBED_CACHE"))
	return v == "false" || v == "0" || v == "off" || v == "disabled"
}

// newMLXWithFallback creates MLX embedder.
// BUG-073: No longer falls back to Ollama/static - returns error if MLX unavailable.
// Users must explicitly use --backend=ollama or --backend=static.
func newMLXWithFallback(ctx context.Context, _ bool) (Embedder, error) {
	cfg := DefaultMLXConfig()

	// Apply config file settings first (set via SetMLXConfig)
	if globalMLXConfig.Endpoint != "" {
		cfg.Endpoint = globalMLXConfig.Endpoint
	}
	if globalMLXConfig.Model != "" {
		cfg.Model = globalMLXConfig.Model
	}

	// Environment variables override config file settings (highest priority)
	if endpoint := os.Getenv("AMANMCP_MLX_ENDPOINT"); endpoint != "" {
		cfg.Endpoint = endpoint
	}
	if model := os.Getenv("AMANMCP_MLX_MODEL"); model != "" {
		cfg.Model = model
	}

	embedder, err := NewMLXEmbedder(ctx, cfg)
	if err != nil {
		// BUG-073: No silent fallback - return clear error message
		return nil, fmt.Errorf("mlx unavailable: %w\n\nTo fix:\n  1. Start MLX server: mlx-embedding-server\n  2. Or use Ollama: amanmcp index --backend=ollama\n  3. Or use BM25-only: amanmcp index --backend=static", err)
	}
	return embedder, nil
}

// newDefaultWithFallback selects the default embedder with fallback chain.
// All platforms: Ollama → Static768
// MLX is opt-in via AMANMCP_EMBEDDER=mlx or config (faster but higher RAM usage).
// This is always auto-detection, so allow fallback (explicitSelection = false).
// ADR-037: Ollama is default for cross-platform compatibility and lower RAM usage.
func newDefaultWithFallback(ctx context.Context, model string) (Embedder, error) {
	// Always default to Ollama - MLX is opt-in only
	// MLX is ~1.7x faster but consumes significantly more RAM
	return newOllamaWithFallback(ctx, model, false)
}

// newOllamaWithFallback creates Ollama embedder.
// BUG-073: No longer falls back to static embeddings - returns error if Ollama unavailable.
// Users must explicitly use --backend=static for BM25-only mode.
func newOllamaWithFallback(ctx context.Context, model string, _ bool) (Embedder, error) {
	cfg := DefaultOllamaConfig()
	// Only override model if it looks like an Ollama model name
	// (contains ":" tag or is a known Ollama embedding model)
	// Ignore GGUF model names like "nomic-embed-text-v1.5" from config
	if model != "" && isOllamaModelName(model) {
		cfg.Model = model
	}

	// Check for host override
	if host := os.Getenv("AMANMCP_OLLAMA_HOST"); host != "" {
		cfg.Host = host
	}

	// Check for model override (highest priority)
	if modelOverride := os.Getenv("AMANMCP_OLLAMA_MODEL"); modelOverride != "" {
		cfg.Model = modelOverride
	}

	// Check for timeout override (e.g., "120s", "2m")
	if timeoutStr := os.Getenv("AMANMCP_OLLAMA_TIMEOUT"); timeoutStr != "" {
		if timeout, err := time.ParseDuration(timeoutStr); err == nil {
			cfg.Timeout = timeout
		}
	}

	// Thermal management settings (for Apple Silicon and other GPUs under sustained load)
	// These help prevent timeout failures during long indexing operations
	// BUG-052: Now reads from config.yaml via SetThermalConfig(), with env vars as override

	// Apply config file settings first (set via SetThermalConfig)
	if globalThermalConfig.InterBatchDelay > 0 {
		delay := globalThermalConfig.InterBatchDelay
		if delay > MaxInterBatchDelay {
			delay = MaxInterBatchDelay
		}
		cfg.InterBatchDelay = delay
	}
	if globalThermalConfig.TimeoutProgression >= 1.0 {
		progression := globalThermalConfig.TimeoutProgression
		if progression > MaxTimeoutProgression {
			progression = MaxTimeoutProgression
		}
		cfg.TimeoutProgression = progression
	}
	if globalThermalConfig.RetryTimeoutMultiplier >= 1.0 {
		mult := globalThermalConfig.RetryTimeoutMultiplier
		if mult > MaxRetryTimeoutMultiplier {
			mult = MaxRetryTimeoutMultiplier
		}
		cfg.RetryTimeoutMultiplier = mult
	}

	// Environment variables override config file settings
	if delayStr := os.Getenv("AMANMCP_INTER_BATCH_DELAY"); delayStr != "" {
		if delay, err := time.ParseDuration(delayStr); err == nil && delay >= 0 {
			if delay > MaxInterBatchDelay {
				delay = MaxInterBatchDelay
			}
			cfg.InterBatchDelay = delay
		}
	}

	if progressionStr := os.Getenv("AMANMCP_TIMEOUT_PROGRESSION"); progressionStr != "" {
		if progression, err := parseFloat64(progressionStr); err == nil && progression >= 1.0 {
			if progression > MaxTimeoutProgression {
				progression = MaxTimeoutProgression
			}
			cfg.TimeoutProgression = progression
		}
	}

	if retryMultStr := os.Getenv("AMANMCP_RETRY_TIMEOUT_MULTIPLIER"); retryMultStr != "" {
		if mult, err := parseFloat64(retryMultStr); err == nil && mult >= 1.0 {
			if mult > MaxRetryTimeoutMultiplier {
				mult = MaxRetryTimeoutMultiplier
			}
			cfg.RetryTimeoutMultiplier = mult
		}
	}

	embedder, err := NewOllamaEmbedder(ctx, cfg)
	if err != nil {
		// BUG-073: No silent fallback - return clear error message
		return nil, fmt.Errorf("ollama unavailable: %w\n\nTo fix:\n  1. Start Ollama: ollama serve\n  2. Or use BM25-only: amanmcp index --backend=static", err)
	}
	return embedder, nil
}

// ThermalConfig holds thermal management settings loaded from config.yaml.
// BUG-052: These settings are now wired from config file, not just env vars.
type ThermalConfig struct {
	InterBatchDelay        time.Duration // Pause between batches for GPU cooling
	TimeoutProgression     float64       // Timeout multiplier for later batches (1.0-3.0)
	RetryTimeoutMultiplier float64       // Timeout multiplier per retry (1.0-2.0)
}

// globalThermalConfig holds config file settings set via SetThermalConfig.
// Env vars take precedence over these values.
var globalThermalConfig ThermalConfig

// SetThermalConfig sets thermal management config from the user's config.yaml.
// This should be called before NewEmbedder() to ensure config file settings are used.
// Environment variables still take precedence over config file settings.
// BUG-052: Fixes issue where config.yaml thermal settings were ignored.
func SetThermalConfig(cfg ThermalConfig) {
	globalThermalConfig = cfg
	if cfg.InterBatchDelay > 0 || cfg.TimeoutProgression != 0 || cfg.RetryTimeoutMultiplier != 0 {
		slog.Debug("thermal_config_set",
			slog.Duration("inter_batch_delay", cfg.InterBatchDelay),
			slog.Float64("timeout_progression", cfg.TimeoutProgression),
			slog.Float64("retry_timeout_multiplier", cfg.RetryTimeoutMultiplier))
	}
}

// MLXServerConfig holds MLX server settings loaded from config.yaml.
type MLXServerConfig struct {
	Endpoint string // MLX server endpoint (default: http://localhost:9659)
	Model    string // Model size: "small", "medium", "large" (default: "large")
}

// globalMLXConfig holds config file settings set via SetMLXConfig.
// Env vars take precedence over these values.
var globalMLXConfig MLXServerConfig

// SetMLXConfig sets MLX server config from the user's config.yaml.
// This should be called before NewEmbedder() to ensure config file settings are used.
// Environment variables still take precedence over config file settings.
func SetMLXConfig(cfg MLXServerConfig) {
	globalMLXConfig = cfg
	if cfg.Endpoint != "" || cfg.Model != "" {
		slog.Debug("mlx_config_set",
			slog.String("endpoint", cfg.Endpoint),
			slog.String("model", cfg.Model))
	}
}

// NewDefaultEmbedder creates a static embedder (768 dimensions).
//
// Deprecated: This function ignores user configuration and always returns
// StaticEmbedder768, which can cause dimension mismatches if the index was
// built with a different embedder (e.g., Ollama with 4096 dims).
// Use NewEmbedder(ctx, ParseProvider(cfg.Embeddings.Provider), cfg.Embeddings.Model) instead.
func NewDefaultEmbedder(ctx context.Context) (Embedder, error) {
	return NewEmbedder(ctx, ProviderStatic, "")
}

// ParseProvider converts a string to ProviderType
func ParseProvider(s string) ProviderType {
	switch strings.ToLower(s) {
	case "mlx":
		return ProviderMLX
	case "ollama", "llama":
		// "llama" mapped to Ollama for backwards compatibility (BUG-021 resolved)
		return ProviderOllama
	case "static":
		return ProviderStatic
	default:
		// ADR-037: Always default to Ollama for cross-platform compatibility and lower RAM
		// MLX is opt-in via explicit config (faster but higher RAM usage)
		return ProviderOllama
	}
}

// String returns the string representation of ProviderType
func (p ProviderType) String() string {
	return string(p)
}

// isOllamaModelName checks if a model name looks like an Ollama model
// Ollama models have a ":" tag (e.g., "qwen3-embedding:8b")
// GGUF models have version numbers (e.g., "nomic-embed-text-v1.5")
func isOllamaModelName(model string) bool {
	// Has tag separator - definitely Ollama (e.g., "qwen3-embedding:8b")
	if strings.Contains(model, ":") {
		return true
	}

	// Has version number pattern - likely GGUF, not Ollama
	// e.g., "nomic-embed-text-v1.5", "bge-small-en-v1.5"
	if strings.Contains(model, "-v") && (strings.Contains(model, ".") || strings.HasSuffix(model, "-v1") || strings.HasSuffix(model, "-v2")) {
		return false
	}

	// Has .gguf extension - definitely not Ollama
	if strings.HasSuffix(strings.ToLower(model), ".gguf") {
		return false
	}

	return false
}

// ValidProviders returns all valid provider names
func ValidProviders() []string {
	return []string{
		string(ProviderMLX),
		string(ProviderOllama),
		string(ProviderStatic),
	}
}

// IsValidProvider checks if a provider name is valid
func IsValidProvider(s string) bool {
	lower := strings.ToLower(s)
	for _, p := range ValidProviders() {
		if lower == p {
			return true
		}
	}
	return false
}

// EmbedderInfo contains information about an embedder
type EmbedderInfo struct {
	Provider   ProviderType
	Model      string
	Dimensions int
	Available  bool
}

// GetInfo returns information about an embedder
func GetInfo(ctx context.Context, embedder Embedder) EmbedderInfo {
	info := EmbedderInfo{
		Model:      embedder.ModelName(),
		Dimensions: embedder.Dimensions(),
		Available:  embedder.Available(ctx),
	}

	// Unwrap cached embedder to get underlying type
	inner := embedder
	if cached, ok := embedder.(*CachedEmbedder); ok {
		inner = cached.inner
	}

	// Determine provider type from embedder type or model name
	switch inner.(type) {
	case *MLXEmbedder:
		info.Provider = ProviderMLX
	case *OllamaEmbedder:
		info.Provider = ProviderOllama
	default:
		switch embedder.ModelName() {
		case "static", "static768":
			info.Provider = ProviderStatic
		default:
			info.Provider = ProviderStatic
		}
	}

	return info
}

// MustNewEmbedder creates an embedder and panics on failure
// Use only in tests or initialization code where failure is fatal
func MustNewEmbedder(ctx context.Context, provider ProviderType, model string) Embedder {
	embedder, err := NewEmbedder(ctx, provider, model)
	if err != nil {
		panic(fmt.Sprintf("failed to create embedder: %v", err))
	}
	return embedder
}

// parseFloat64 parses a string to float64, used for thermal config parsing
func parseFloat64(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}
