package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// Default classifier configuration values.
const (
	DefaultClassifierModel     = "llama3.2:1b"
	DefaultClassifierTimeout   = 2 * time.Second
	DefaultClassifierCacheSize = 10000 // QW-2: Increased from 1000 for better hit rate (~100KB additional memory)
	DefaultOllamaHost          = "http://localhost:11434"
)

// ClassifierConfig holds configuration for the query classifier.
type ClassifierConfig struct {
	// Model is the Ollama model to use for classification (default: llama3.2:1b).
	Model string

	// Timeout is the maximum time to wait for LLM response (default: 2s).
	Timeout time.Duration

	// CacheSize is the LRU cache size for classification results (default: 1000).
	CacheSize int

	// OllamaHost is the Ollama API base URL (default: http://localhost:11434).
	OllamaHost string
}

// DefaultClassifierConfig returns sensible defaults for the classifier.
func DefaultClassifierConfig() ClassifierConfig {
	return ClassifierConfig{
		Model:      DefaultClassifierModel,
		Timeout:    DefaultClassifierTimeout,
		CacheSize:  DefaultClassifierCacheSize,
		OllamaHost: DefaultOllamaHost,
	}
}

// classificationResult holds cached classification data.
type classificationResult struct {
	queryType QueryType
	weights   Weights
}

// HybridClassifier tries LLM classification first, falls back to patterns.
// Results are cached in an LRU cache for performance.
type HybridClassifier struct {
	llm      *LLMClassifier
	patterns *PatternClassifier
	cache    *lru.Cache[string, classificationResult]
}

// NewHybridClassifier creates a classifier that tries LLM first, then patterns.
// If llm is nil, only pattern-based classification is used.
func NewHybridClassifier(llm *LLMClassifier) *HybridClassifier {
	cache, _ := lru.New[string, classificationResult](DefaultClassifierCacheSize)
	return &HybridClassifier{
		llm:      llm,
		patterns: NewPatternClassifier(),
		cache:    cache,
	}
}

// NewHybridClassifierWithConfig creates a classifier with custom configuration.
func NewHybridClassifierWithConfig(llm *LLMClassifier, config ClassifierConfig) *HybridClassifier {
	cacheSize := config.CacheSize
	if cacheSize <= 0 {
		cacheSize = DefaultClassifierCacheSize
	}
	cache, _ := lru.New[string, classificationResult](cacheSize)
	return &HybridClassifier{
		llm:      llm,
		patterns: NewPatternClassifier(),
		cache:    cache,
	}
}

// Classify determines the query type and optimal weights.
// Uses LRU cache, tries LLM first (if available), falls back to patterns.
func (h *HybridClassifier) Classify(ctx context.Context, query string) (QueryType, Weights, error) {
	// Normalize query for cache key
	cacheKey := normalizeQuery(query)
	if cacheKey == "" {
		return QueryTypeMixed, WeightsForQueryType(QueryTypeMixed), nil
	}

	// Check cache first
	if result, ok := h.cache.Get(cacheKey); ok {
		return result.queryType, result.weights, nil
	}

	// Try LLM classification if available
	var qt QueryType
	var weights Weights
	var err error

	if h.llm != nil {
		qt, weights, err = h.llm.Classify(ctx, query)
		if err == nil {
			// Cache and return LLM result
			h.cache.Add(cacheKey, classificationResult{qt, weights})
			return qt, weights, nil
		}
		// LLM failed, fall through to patterns
	}

	// Fallback to pattern classification
	qt, weights, err = h.patterns.Classify(ctx, query)
	if err == nil {
		h.cache.Add(cacheKey, classificationResult{qt, weights})
	}
	return qt, weights, err
}

// normalizeQuery normalizes a query for cache key.
func normalizeQuery(query string) string {
	return strings.ToLower(strings.TrimSpace(query))
}

// Ensure HybridClassifier implements Classifier interface.
var _ Classifier = (*HybridClassifier)(nil)

// =============================================================================
// LLMClassifier
// =============================================================================

// LLMClassifier uses Ollama LLM for query classification.
type LLMClassifier struct {
	client  *http.Client
	config  ClassifierConfig
	prompt  string
}

// generateRequest is the Ollama /api/generate request body.
type generateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// generateResponse is the Ollama /api/generate response body.
type generateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// NewLLMClassifier creates a new LLM-based classifier.
func NewLLMClassifier(config ClassifierConfig) *LLMClassifier {
	// Apply defaults
	if config.Model == "" {
		config.Model = DefaultClassifierModel
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultClassifierTimeout
	}
	if config.OllamaHost == "" {
		config.OllamaHost = DefaultOllamaHost
	}

	client := &http.Client{
		Timeout: config.Timeout,
	}

	return &LLMClassifier{
		client: client,
		config: config,
		prompt: classificationPrompt,
	}
}

// classificationPrompt is the prompt template for query classification.
const classificationPrompt = `You are a search query classifier. Classify the given query into exactly ONE of these categories:

LEXICAL - The query needs exact/keyword matching. Examples:
- Error codes: ERR_CONNECTION_REFUSED, E0001
- Function/variable names: getUserById, handle_auth
- File paths: src/auth/handler.go
- Quoted phrases: "exact match"

SEMANTIC - The query is natural language seeking meaning. Examples:
- Questions: "how does authentication work"
- Conceptual: "explain the search algorithm"
- Descriptions: "find code that handles errors"

MIXED - The query benefits from both approaches. Examples:
- Short technical terms: "useEffect cleanup"
- Ambiguous: "authentication" (could be code or concept)

Respond with ONLY one word: LEXICAL, SEMANTIC, or MIXED.

Query: %s

Classification:`

// Classify uses Ollama LLM to classify the query.
func (l *LLMClassifier) Classify(ctx context.Context, query string) (QueryType, Weights, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return QueryTypeMixed, WeightsForQueryType(QueryTypeMixed), nil
	}

	// Build prompt
	prompt := fmt.Sprintf(l.prompt, query)

	// Create request
	reqBody := generateRequest{
		Model:  l.config.Model,
		Prompt: prompt,
		Stream: false,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return QueryTypeMixed, WeightsForQueryType(QueryTypeMixed), fmt.Errorf("marshal request: %w", err)
	}

	url := l.config.OllamaHost + "/api/generate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return QueryTypeMixed, WeightsForQueryType(QueryTypeMixed), fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := l.client.Do(req)
	if err != nil {
		return QueryTypeMixed, WeightsForQueryType(QueryTypeMixed), fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return QueryTypeMixed, WeightsForQueryType(QueryTypeMixed), fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var result generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return QueryTypeMixed, WeightsForQueryType(QueryTypeMixed), fmt.Errorf("decode response: %w", err)
	}

	// Parse classification from response
	qt := parseClassificationResponse(result.Response)
	return qt, WeightsForQueryType(qt), nil
}

// parseClassificationResponse extracts the query type from LLM response.
func parseClassificationResponse(response string) QueryType {
	response = strings.ToUpper(strings.TrimSpace(response))

	// Check for exact match first
	switch response {
	case "LEXICAL":
		return QueryTypeLexical
	case "SEMANTIC":
		return QueryTypeSemantic
	case "MIXED":
		return QueryTypeMixed
	}

	// Check if response contains the classification
	if strings.Contains(response, "LEXICAL") {
		return QueryTypeLexical
	}
	if strings.Contains(response, "SEMANTIC") {
		return QueryTypeSemantic
	}
	if strings.Contains(response, "MIXED") {
		return QueryTypeMixed
	}

	// Default to MIXED
	return QueryTypeMixed
}

// Available checks if Ollama is available and the model is loaded.
func (l *LLMClassifier) Available(ctx context.Context) bool {
	url := l.config.OllamaHost + "/api/tags"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	resp, err := l.client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode == http.StatusOK
}

// Ensure LLMClassifier implements Classifier interface.
var _ Classifier = (*LLMClassifier)(nil)
