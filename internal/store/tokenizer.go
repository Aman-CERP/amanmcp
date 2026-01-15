package store

import (
	"regexp"
	"strings"
	"unicode"
)

// tokenRegex matches alphanumeric sequences (including underscores for initial split).
var tokenRegex = regexp.MustCompile(`[a-zA-Z0-9_]+`)

// TokenizeCode splits text with code-aware rules.
// It handles camelCase, PascalCase, snake_case, and filters short tokens.
// All tokens are lowercased.
func TokenizeCode(text string) []string {
	var tokens []string

	// Split on whitespace and punctuation first
	words := tokenRegex.FindAllString(text, -1)

	for _, word := range words {
		// Split camelCase and snake_case
		subTokens := SplitCodeToken(word)
		for _, t := range subTokens {
			lower := strings.ToLower(t)
			// Filter tokens < 2 chars
			if len(lower) >= 2 {
				tokens = append(tokens, lower)
			}
		}
	}

	return tokens
}

// SplitCodeToken splits camelCase and snake_case identifiers.
func SplitCodeToken(token string) []string {
	var result []string

	// Handle snake_case first
	if strings.Contains(token, "_") {
		parts := strings.Split(token, "_")
		for _, part := range parts {
			if part != "" {
				// Recursively handle camelCase in each part
				result = append(result, SplitCamelCase(part)...)
			}
		}
		return result
	}

	return SplitCamelCase(token)
}

// SplitCamelCase splits camelCase and PascalCase identifiers.
// Examples:
//   - "getUserById" -> ["get", "User", "By", "Id"]
//   - "HTTPHandler" -> ["HTTP", "Handler"]
//   - "parseHTTPRequest" -> ["parse", "HTTP", "Request"]
func SplitCamelCase(s string) []string {
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

// FilterStopWords removes stop words from a token list.
func FilterStopWords(tokens []string, stopWords map[string]struct{}) []string {
	result := make([]string, 0, len(tokens))
	for _, token := range tokens {
		lower := strings.ToLower(token)
		if _, isStop := stopWords[lower]; !isStop {
			result = append(result, token)
		}
	}
	return result
}

// BuildStopWordMap converts a slice of stop words to a map for efficient lookup.
func BuildStopWordMap(stopWords []string) map[string]struct{} {
	m := make(map[string]struct{}, len(stopWords))
	for _, word := range stopWords {
		m[strings.ToLower(word)] = struct{}{}
	}
	return m
}
