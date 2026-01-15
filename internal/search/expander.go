package search

import (
	"strings"
	"unicode"
)

// QueryExpander expands search queries with code-aware synonyms.
// This addresses vocabulary mismatch (RCA-010) where user terms
// don't match code terminology.
//
// Example:
//
//	Input:  "Search function"
//	Output: "Search function func method def Search"
//
// Research basis:
// - Neural Query Expansion: ml4code.github.io/publications/liu2019neural/
// - CodeSearchNet vocabulary gap: arxiv.org/pdf/1909.09436
// - Query expansion techniques: opensourceconnections.com/blog/2021/10/19/fundamentals-of-query-rewriting-part-1-introduction-to-query-expansion/
type QueryExpander struct {
	synonyms       map[string][]string
	maxExpansions  int  // Max synonyms per term (default: 3)
	includeCasing  bool // Include case variants (default: true)
}

// QueryExpanderOption configures the query expander.
type QueryExpanderOption func(*QueryExpander)

// WithMaxExpansions sets the maximum synonyms per term.
func WithMaxExpansions(n int) QueryExpanderOption {
	return func(e *QueryExpander) {
		e.maxExpansions = n
	}
}

// WithCasingVariants enables/disables case variant expansion.
func WithCasingVariants(enabled bool) QueryExpanderOption {
	return func(e *QueryExpander) {
		e.includeCasing = enabled
	}
}

// WithCustomSynonyms adds custom synonym mappings.
func WithCustomSynonyms(synonyms map[string][]string) QueryExpanderOption {
	return func(e *QueryExpander) {
		for k, v := range synonyms {
			e.synonyms[k] = append(e.synonyms[k], v...)
		}
	}
}

// NewQueryExpander creates a new query expander with default code synonyms.
func NewQueryExpander(opts ...QueryExpanderOption) *QueryExpander {
	e := &QueryExpander{
		synonyms:      make(map[string][]string),
		maxExpansions: 3,
		includeCasing: true,
	}

	// Copy default synonyms
	for k, v := range CodeSynonyms {
		e.synonyms[k] = v
	}

	// Apply options
	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Expand expands a query with code-aware synonyms.
// Returns the expanded query suitable for BM25 search.
//
// The expansion strategy:
// 1. Keep original query terms (for exact matches)
// 2. Add synonym expansions (for vocabulary bridging)
// 3. Add casing variants (for Go naming conventions)
// 4. Deduplicate terms
func (e *QueryExpander) Expand(query string) string {
	terms := tokenize(query)
	if len(terms) == 0 {
		return query
	}

	seen := make(map[string]bool)
	var expanded []string

	// First, add original terms
	for _, term := range terms {
		lowerTerm := strings.ToLower(term)
		if !seen[lowerTerm] {
			expanded = append(expanded, term)
			seen[lowerTerm] = true
		}
	}

	// Then, add synonym expansions
	for _, term := range terms {
		lowerTerm := strings.ToLower(term)
		synonyms := e.getSynonyms(lowerTerm)

		added := 0
		for _, syn := range synonyms {
			lowerSyn := strings.ToLower(syn)
			if !seen[lowerSyn] && added < e.maxExpansions {
				expanded = append(expanded, syn)
				seen[lowerSyn] = true
				added++
			}
		}
	}

	// Add casing variants for Go conventions
	if e.includeCasing {
		for _, term := range terms {
			variants := generateCasingVariants(term)
			for _, v := range variants {
				lowerV := strings.ToLower(v)
				if !seen[lowerV] {
					expanded = append(expanded, v)
					seen[lowerV] = true
				}
			}
		}
	}

	return strings.Join(expanded, " ")
}

// ExpandToTerms returns expanded terms as a slice (useful for multi-query search).
func (e *QueryExpander) ExpandToTerms(query string) []string {
	expanded := e.Expand(query)
	return tokenize(expanded)
}

// getSynonyms retrieves synonyms for a term.
func (e *QueryExpander) getSynonyms(term string) []string {
	if syns, ok := e.synonyms[term]; ok {
		return syns
	}
	return nil
}

// tokenize splits a query into terms.
// Handles camelCase, snake_case, and regular spacing.
func tokenize(query string) []string {
	// First split by whitespace and punctuation
	var tokens []string
	var current strings.Builder

	for _, r := range query {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			current.WriteRune(r)
		} else if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	// Then split camelCase and snake_case within each token
	var result []string
	for _, token := range tokens {
		parts := splitCamelSnake(token)
		result = append(result, parts...)
	}

	return result
}

// splitCamelSnake splits a token on camelCase and snake_case boundaries.
// Example: "searchFunction" → ["search", "Function"]
// Example: "search_function" → ["search", "function"]
func splitCamelSnake(token string) []string {
	// Handle snake_case
	if strings.Contains(token, "_") {
		parts := strings.Split(token, "_")
		var result []string
		for _, p := range parts {
			if p != "" {
				result = append(result, p)
			}
		}
		return result
	}

	// Handle camelCase and PascalCase
	var parts []string
	var current strings.Builder

	for i, r := range token {
		if i > 0 && unicode.IsUpper(r) && current.Len() > 0 {
			parts = append(parts, current.String())
			current.Reset()
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// generateCasingVariants generates common Go casing variants.
// Example: "search" → ["Search", "SEARCH"]
// Example: "Search" → ["search"]
func generateCasingVariants(term string) []string {
	if len(term) == 0 {
		return nil
	}

	var variants []string
	lower := strings.ToLower(term)
	upper := strings.ToUpper(term)
	title := strings.Title(lower) //nolint:staticcheck // Title is fine for single words

	// Add variants that don't match the original
	if term != lower {
		variants = append(variants, lower)
	}
	if term != upper && len(term) <= 4 { // Only uppercase short terms (abbreviations)
		variants = append(variants, upper)
	}
	if term != title {
		variants = append(variants, title)
	}

	return variants
}
