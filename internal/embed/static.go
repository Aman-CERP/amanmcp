package embed

import (
	"context"
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
	"sync"
	"unicode"
)

// StaticEmbedder generates embeddings using a hash-based approach.
// Works without external dependencies (no network, no model download).
// Provides deterministic, fast embeddings with reduced semantic quality.
type StaticEmbedder struct {
	mu     sync.RWMutex
	closed bool
}

// programmingStopWords contains common programming language keywords to filter out.
var programmingStopWords = map[string]bool{
	"func": true, "function": true, "def": true, "class": true,
	"return": true, "import": true, "const": true, "var": true,
	"let": true, "int": true, "string": true, "bool": true,
	"void": true, "true": true, "false": true, "nil": true,
	"null": true, "this": true, "self": true, "new": true,
}

// Weights for vector generation
const (
	tokenWeight = 0.7
	ngramWeight = 0.3
	ngramSize   = 3
)

// tokenRegex matches alphanumeric sequences
var tokenRegex = regexp.MustCompile(`[a-zA-Z0-9]+`)

// NewStaticEmbedder creates a new static embedder.
func NewStaticEmbedder() *StaticEmbedder {
	return &StaticEmbedder{}
}

// Embed generates embedding for a single text.
func (e *StaticEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	e.mu.RLock()
	if e.closed {
		e.mu.RUnlock()
		return nil, fmt.Errorf("embedder is closed")
	}
	e.mu.RUnlock()

	// Handle empty/whitespace input
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return make([]float32, StaticDimensions), nil
	}

	// Generate vector
	vector := e.generateVector(trimmed)

	// Normalize
	return normalizeVector(vector), nil
}

// generateVector creates a hash-based vector from text.
func (e *StaticEmbedder) generateVector(text string) []float32 {
	vector := make([]float32, StaticDimensions)

	// Step 1: Tokenize
	tokens := tokenize(text)

	// Step 2: Filter stop words
	tokens = filterStopWords(tokens)

	// Step 3: Add tokens with weight 0.7
	for _, token := range tokens {
		index := hashToIndex(token, StaticDimensions)
		vector[index] += tokenWeight
	}

	// Step 4: Extract n-grams and add with weight 0.3
	normalized := normalizeForNgrams(text)
	ngrams := extractNgrams(normalized, ngramSize)
	for _, ngram := range ngrams {
		index := hashToIndex(ngram, StaticDimensions)
		vector[index] += ngramWeight
	}

	return vector
}

// tokenize splits text into tokens (code-aware).
func tokenize(text string) []string {
	var tokens []string

	// First, split on whitespace and punctuation
	words := tokenRegex.FindAllString(text, -1)

	for _, word := range words {
		// Split camelCase and snake_case
		subTokens := splitCodeToken(word)
		for _, t := range subTokens {
			lower := strings.ToLower(t)
			if lower != "" {
				tokens = append(tokens, lower)
			}
		}
	}

	return tokens
}

// splitCodeToken splits camelCase and snake_case identifiers.
func splitCodeToken(token string) []string {
	var result []string

	// Handle snake_case first
	if strings.Contains(token, "_") {
		parts := strings.Split(token, "_")
		for _, part := range parts {
			if part != "" {
				// Recursively handle camelCase in each part
				result = append(result, splitCamelCase(part)...)
			}
		}
		return result
	}

	return splitCamelCase(token)
}

// splitCamelCase splits camelCase identifiers.
func splitCamelCase(s string) []string {
	// Return empty slice, not nil, for consistent API behavior (DEBT-012)
	if s == "" {
		return []string{}
	}

	var result []string
	var current strings.Builder

	runes := []rune(s)
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prevIsLower := unicode.IsLower(runes[i-1])
			nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])

			// Split if previous is lowercase OR next is lowercase (handles acronyms)
			if prevIsLower || nextIsLower {
				if current.Len() > 0 {
					result = append(result, current.String())
					current.Reset()
				}
			}
		}
		current.WriteRune(r)
	}

	if current.Len() > 0 {
		result = append(result, current.String())
	}

	return result
}

// filterStopWords removes programming stop words.
func filterStopWords(tokens []string) []string {
	var filtered []string
	for _, t := range tokens {
		if !programmingStopWords[t] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// normalizeForNgrams prepares text for n-gram extraction.
func normalizeForNgrams(text string) string {
	var result strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// extractNgrams extracts n-character sliding windows.
func extractNgrams(text string, n int) []string {
	// Return empty slice, not nil, for consistent API behavior (DEBT-012)
	if len(text) < n {
		return []string{}
	}

	ngrams := make([]string, 0, len(text)-n+1)
	for i := 0; i <= len(text)-n; i++ {
		ngrams = append(ngrams, text[i:i+n])
	}
	return ngrams
}

// hashToIndex uses FNV-64 to map a string to an index.
func hashToIndex(s string, size int) int {
	h := fnv.New64()
	_, _ = h.Write([]byte(s))
	return int(h.Sum64() % uint64(size))
}

// EmbedBatch generates embeddings for multiple texts.
func (e *StaticEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	e.mu.RLock()
	if e.closed {
		e.mu.RUnlock()
		return nil, fmt.Errorf("embedder is closed")
	}
	e.mu.RUnlock()

	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	results := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := e.Embed(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("failed to embed text %d: %w", i, err)
		}
		results[i] = emb
	}

	return results, nil
}

// Dimensions returns the embedding dimension.
func (e *StaticEmbedder) Dimensions() int {
	return StaticDimensions
}

// ModelName returns the model identifier.
func (e *StaticEmbedder) ModelName() string {
	return "static"
}

// Available checks if the embedder is ready (always true for static).
func (e *StaticEmbedder) Available(_ context.Context) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return !e.closed
}

// Close releases resources.
func (e *StaticEmbedder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	return nil
}

// SetBatchIndex is a no-op for static embedder (no thermal management needed).
func (e *StaticEmbedder) SetBatchIndex(_ int) {}

// SetFinalBatch is a no-op for static embedder (no thermal management needed).
func (e *StaticEmbedder) SetFinalBatch(_ bool) {}
