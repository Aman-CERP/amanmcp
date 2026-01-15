package search

import (
	"context"
	"regexp"
	"strings"
)

// Compiled regex patterns for query classification.
// Compiled at package init for performance.
var (
	// Error codes: ERR_*, E0001, ERRXXX, Exception types
	errorCodePattern = regexp.MustCompile(`(?i)^(ERR_\w+|E\d{4,5}|[A-Z]{2,}\d{3,}|\w+Exception)$`)

	// Quoted exact phrases: "..." or '...'
	quotedPattern = regexp.MustCompile(`^["'].*["']$`)

	// File paths: path/to/file.ext (common extensions)
	filePathPattern = regexp.MustCompile(`(?i)^[\w\-\./\\]+\.(go|ts|tsx|js|jsx|py|md|json|yaml|yml|toml|css|scss|html|rs|java|kt|c|cpp|h|hpp|rb|php|swift|sh|bash|zsh)$`)

	// Technical identifiers
	camelCasePattern      = regexp.MustCompile(`^[a-z]+([A-Z][a-z0-9]*)+$`)
	pascalCasePattern     = regexp.MustCompile(`^([A-Z][a-z0-9]*){2,}$`)
	snakeCasePattern      = regexp.MustCompile(`^[a-z]+(_[a-z0-9]+)+$`)
	screamingSnakePattern = regexp.MustCompile(`^[A-Z]+(_[A-Z0-9]+)+$`)

	// Natural language starters (questions, commands)
	naturalLanguagePattern = regexp.MustCompile(`(?i)^(how|what|where|why|when|which|can|does|is|are|should|explain|describe|show|find|list)\s`)
)

// PatternClassifier classifies queries using regex pattern matching.
// This is the fallback classifier when LLM is unavailable.
type PatternClassifier struct{}

// NewPatternClassifier creates a new pattern-based classifier.
func NewPatternClassifier() *PatternClassifier {
	return &PatternClassifier{}
}

// Classify determines the query type using pattern matching.
// Returns (QueryType, Weights, nil) - never returns an error.
func (p *PatternClassifier) Classify(_ context.Context, query string) (QueryType, Weights, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return QueryTypeMixed, WeightsForQueryType(QueryTypeMixed), nil
	}

	qt := p.classifyQuery(query)
	return qt, WeightsForQueryType(qt), nil
}

// classifyQuery determines the query type based on patterns.
func (p *PatternClassifier) classifyQuery(query string) QueryType {
	// Check lexical patterns first (most specific)
	if p.isLexicalQuery(query) {
		return QueryTypeLexical
	}

	// Check natural language patterns
	if p.isSemanticQuery(query) {
		return QueryTypeSemantic
	}

	// Multi-word queries (3+) that don't match other patterns → SEMANTIC
	wordCount := len(strings.Fields(query))
	if wordCount >= 3 {
		return QueryTypeSemantic
	}

	// Default to MIXED for 1-2 word queries
	return QueryTypeMixed
}

// isLexicalQuery checks if the query matches lexical patterns.
func (p *PatternClassifier) isLexicalQuery(query string) bool {
	// Error codes
	if errorCodePattern.MatchString(query) {
		return true
	}

	// Quoted phrases
	if quotedPattern.MatchString(query) {
		return true
	}

	// File paths
	if filePathPattern.MatchString(query) {
		return true
	}

	// Technical identifiers (single word only)
	if !strings.Contains(query, " ") {
		if camelCasePattern.MatchString(query) ||
			pascalCasePattern.MatchString(query) ||
			snakeCasePattern.MatchString(query) ||
			screamingSnakePattern.MatchString(query) {
			return true
		}
	}

	return false
}

// isSemanticQuery checks if the query matches semantic (natural language) patterns.
func (p *PatternClassifier) isSemanticQuery(query string) bool {
	return naturalLanguagePattern.MatchString(query)
}

// Ensure PatternClassifier implements Classifier interface.
var _ Classifier = (*PatternClassifier)(nil)
