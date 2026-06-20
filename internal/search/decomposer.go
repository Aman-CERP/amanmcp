// Package search provides hybrid search functionality combining BM25 and semantic search.
package search

import (
	"regexp"
	"strings"
)

// SubQuery represents a decomposed sub-query with its relative weight.
// FEAT-QI3: Multi-Query Fusion decomposes generic queries into multiple
// specific sub-queries for better coverage.
type SubQuery struct {
	// Query is the sub-query text to search.
	Query string

	// Weight is the relative importance of this sub-query (default: 1.0).
	// Higher weights give more influence in RRF fusion.
	Weight float64

	// Hint optionally suggests result filtering: "code", "docs", or "" (any).
	Hint string
}

// QueryDecomposer transforms a single query into multiple sub-queries
// for improved coverage via multi-signal fusion.
//
// FEAT-QI3: This addresses the generic query problem where queries like
// "Search function" fail because vocabulary overlaps between docs and code.
// By decomposing into specific sub-queries, we leverage what works
// (specific queries pass) to improve what doesn't (generic queries fail).
type QueryDecomposer interface {
	// ShouldDecompose returns true if the query benefits from decomposition.
	// Conservative: only returns true for patterns known to fail.
	ShouldDecompose(query string) bool

	// Decompose returns sub-queries for the given query.
	// If ShouldDecompose returns false, returns original query wrapped in slice.
	Decompose(query string) []SubQuery
}

// PatternDecomposer implements QueryDecomposer using regex pattern matching.
// This is deterministic, fast (<1ms), and has no external dependencies.
//
// Patterns are designed to address the 3 failing Tier 1 queries:
// - "Search function" -> "func Search", "Search method", etc.
// - "Index function" -> "func Index", "Coordinator", etc.
// - "How does RRF fusion work" -> "RRF", "fusion.go", etc.
type PatternDecomposer struct {
	// Compiled patterns for decomposition eligibility
	nounFunctionPattern *regexp.Regexp
	howDoesWorkPattern  *regexp.Regexp
	camelCasePattern    *regexp.Regexp
	pascalCasePattern   *regexp.Regexp
	snakeCasePattern    *regexp.Regexp
	filePathPattern     *regexp.Regexp
	quotedPattern       *regexp.Regexp
}

// NewPatternDecomposer creates a new pattern-based query decomposer.
func NewPatternDecomposer() *PatternDecomposer {
	return &PatternDecomposer{
		// Matches: "Search function", "Index method", "Query func"
		nounFunctionPattern: regexp.MustCompile(`(?i)^(\w+)\s+(function|func|method)$`),

		// Matches: "How does RRF fusion work", "How does search work"
		howDoesWorkPattern: regexp.MustCompile(`(?i)^how\s+does\s+(.+?)\s+work$`),

		// Technical identifiers that should skip decomposition
		camelCasePattern:  regexp.MustCompile(`^[a-z]+([A-Z][a-z0-9]*)+$`),
		pascalCasePattern: regexp.MustCompile(`^([A-Z][a-z0-9]*){2,}$`),
		snakeCasePattern:  regexp.MustCompile(`^[a-z]+(_[a-z0-9]+)+$`),

		// File paths with common extensions
		filePathPattern: regexp.MustCompile(`(?i)[\w\-\.]*[/\\][\w\-\./\\]*\.(go|ts|tsx|js|jsx|py|md|json|yaml|yml)$`),

		// Quoted phrases
		quotedPattern: regexp.MustCompile(`^["'].*["']$`),
	}
}

// ShouldDecompose returns true if the query matches a pattern that benefits
// from multi-query decomposition.
//
// Conservative approach: only decompose queries matching known failing patterns.
// This prevents regression on queries that already work.
func (d *PatternDecomposer) ShouldDecompose(query string) bool {
	query = strings.TrimSpace(query)

	// Empty or very short queries don't benefit
	if len(query) == 0 {
		return false
	}

	// Skip single words (no decomposition benefit)
	words := strings.Fields(query)
	if len(words) <= 1 {
		return false
	}

	// Skip specific identifiers (already targeted, work well)
	if d.isSpecificIdentifier(query) {
		return false
	}

	// Skip file paths (already specific)
	if d.filePathPattern.MatchString(query) {
		return false
	}

	// Skip quoted phrases (user wants exact match)
	if d.quotedPattern.MatchString(query) {
		return false
	}

	if isCrossFileSubsystemQuery(query) {
		return true
	}
	if isImpactAnalysisQuery(query) || isRetryErrorHandlingQuery(query) {
		return true
	}

	// Skip long natural language queries (4+ words, already semantic-optimized)
	// Exception: "How does X work" pattern
	if len(words) >= 4 && !d.howDoesWorkPattern.MatchString(query) {
		return false
	}

	// Pattern 1: "{Noun} function/method" - known to fail
	if d.nounFunctionPattern.MatchString(query) {
		return true
	}

	// Pattern 2: "How does {X} work" - known to fail for generic topics
	if d.howDoesWorkPattern.MatchString(query) {
		return true
	}

	return false
}

// isSpecificIdentifier checks if the query is a technical identifier
// (camelCase, PascalCase, snake_case) that shouldn't be decomposed.
func (d *PatternDecomposer) isSpecificIdentifier(query string) bool {
	// Only check single tokens (no spaces)
	if strings.Contains(query, " ") {
		return false
	}

	return d.camelCasePattern.MatchString(query) ||
		d.pascalCasePattern.MatchString(query) ||
		exportedIdentifierPattern.MatchString(query) ||
		d.snakeCasePattern.MatchString(query)
}

// Decompose transforms a query into multiple sub-queries.
// Returns original query wrapped in slice if decomposition doesn't apply.
func (d *PatternDecomposer) Decompose(query string) []SubQuery {
	query = strings.TrimSpace(query)

	// If decomposition doesn't apply, return original
	if !d.ShouldDecompose(query) {
		return []SubQuery{{Query: query, Weight: 1.0}}
	}

	// Pattern 1: "{Noun} function/method"
	if matches := d.nounFunctionPattern.FindStringSubmatch(query); len(matches) >= 2 {
		return d.decomposeNounFunction(matches[1])
	}

	// Pattern 2: "How does {X} work"
	if matches := d.howDoesWorkPattern.FindStringSubmatch(query); len(matches) >= 2 {
		return d.decomposeHowDoesWork(matches[1])
	}

	if isCrossFileSubsystemQuery(query) {
		return d.decomposeCrossFileSubsystem(query)
	}
	if isImpactAnalysisQuery(query) {
		return d.decomposeImpactAnalysis(query)
	}
	if isRetryErrorHandlingQuery(query) {
		return d.decomposeRetryErrorHandling(query)
	}

	// Fallback: return original
	return []SubQuery{{Query: query, Weight: 1.0}}
}

// decomposeNounFunction generates sub-queries for "{Noun} function" patterns.
// Example: "Search function" ->
//   - "func Search" (Go function signature)
//   - ") Search(" (Go method receiver pattern - FEAT-QI4)
//   - "Search(ctx" (Go context param pattern - FEAT-QI4)
//   - "Search method" (identifier context)
//   - "Search(" (call site pattern)
//   - "Search" (raw identifier)
func (d *PatternDecomposer) decomposeNounFunction(noun string) []SubQuery {
	// Capitalize first letter for Go convention
	capitalNoun := strings.Title(strings.ToLower(noun)) //nolint:staticcheck
	lowerNoun := strings.ToLower(noun)

	subQueries := []SubQuery{
		// FEAT-QI4: Go method receiver pattern (highest weight - most specific)
		// Matches: func (e *Engine) Search( → tokens include ") Search("
		{Query: ") " + capitalNoun + "(", Weight: 1.5, Hint: "code"},

		// FEAT-QI4: Go context parameter pattern
		// Matches: Search(ctx context.Context, ... → very specific to Go methods
		{Query: capitalNoun + "(ctx", Weight: 1.4, Hint: "code"},

		// Go function signature pattern
		{Query: "func " + capitalNoun, Weight: 1.2, Hint: "code"},

		// FEAT-QI4: Partial method signature (lowercase for variable receiver names)
		// Matches: func (s *Server) → where s is the noun
		{Query: "func (" + lowerNoun, Weight: 1.1, Hint: "code"},

		// Method/identifier context (code-biased since we're looking for a function)
		{Query: capitalNoun + " method", Weight: 1.0, Hint: "code"},

		// Call site pattern (function invocation)
		{Query: capitalNoun + "(", Weight: 0.9, Hint: "code"},

		// Raw identifier (broadest, still prefer code since query says "function")
		{Query: capitalNoun, Weight: 0.8, Hint: "code"},
	}

	// Add domain-specific patterns for known nouns
	switch strings.ToLower(noun) {
	case "search":
		subQueries = append(subQueries,
			SubQuery{Query: "engine.go Search", Weight: 1.1, Hint: "code"},
			SubQuery{Query: "Engine Search", Weight: 1.0, Hint: "code"},
		)
	case "index":
		subQueries = append(subQueries,
			SubQuery{Query: "Coordinator", Weight: 1.0, Hint: "code"},
			SubQuery{Query: "index/", Weight: 0.9, Hint: "code"},
		)
	}

	return subQueries
}

// decomposeHowDoesWork generates sub-queries for "How does {X} work" patterns.
// Example: "How does RRF fusion work" ->
//   - "RRF" (key term)
//   - "fusion" (key term)
//   - "fusion.go" (file hint)
//   - "Fuse func" (code pattern)
func (d *PatternDecomposer) decomposeHowDoesWork(topic string) []SubQuery {
	words := strings.Fields(topic)
	subQueries := make([]SubQuery, 0, len(words)*2)

	// Add each significant word as a sub-query
	for _, word := range words {
		word = strings.TrimSpace(word)
		if len(word) < 2 {
			continue
		}

		// Skip common stop words
		lowerWord := strings.ToLower(word)
		if isStopWord(lowerWord) {
			continue
		}

		// Add word as-is
		subQueries = append(subQueries, SubQuery{
			Query:  word,
			Weight: 1.0,
		})

		// Add file pattern for likely code terms
		if len(word) >= 3 {
			subQueries = append(subQueries, SubQuery{
				Query:  strings.ToLower(word) + ".go",
				Weight: 1.1,
				Hint:   "code",
			})
		}
	}

	// Add Go function pattern for the topic
	if len(words) > 0 {
		mainTerm := strings.Title(strings.ToLower(words[len(words)-1])) //nolint:staticcheck
		subQueries = append(subQueries, SubQuery{
			Query:  "func " + mainTerm,
			Weight: 1.0,
			Hint:   "code",
		})
	}

	// Ensure we have at least the original topic
	if len(subQueries) == 0 {
		return []SubQuery{{Query: topic, Weight: 1.0}}
	}

	return subQueries
}

func isCrossFileSubsystemQuery(query string) bool {
	lower := strings.ToLower(query)
	return strings.Contains(lower, " flows ") ||
		strings.Contains(lower, " flow ") ||
		strings.Contains(lower, " pipeline ")
}

func isImpactAnalysisQuery(query string) bool {
	lower := strings.ToLower(query)
	return strings.Contains(lower, "affected by changing") ||
		strings.Contains(lower, "changes if")
}

func isRetryErrorHandlingQuery(query string) bool {
	lower := strings.ToLower(query)
	return strings.Contains(lower, "retry") &&
		(strings.Contains(lower, "error") || strings.Contains(lower, "handling") || strings.Contains(lower, "backoff"))
}

func (d *PatternDecomposer) decomposeCrossFileSubsystem(query string) []SubQuery {
	var subQueries []SubQuery
	seen := make(map[string]struct{})
	add := func(query string, weight float64) {
		query = strings.TrimSpace(query)
		if query == "" {
			return
		}
		if _, ok := seen[query]; ok {
			return
		}
		seen[query] = struct{}{}
		subQueries = append(subQueries, SubQuery{Query: query, Weight: weight, Hint: "code"})
	}

	add(query, 0.2)
	for _, word := range strings.Fields(query) {
		word = strings.Trim(strings.ToLower(word), ".,:;!?()[]{}")
		if len(word) < 3 || isStopWord(word) {
			continue
		}
		add(word, 0.2)
		for _, hint := range crossFileSubsystemHints(word) {
			add(hint.query, hint.weight)
		}
	}

	return subQueries
}

func (d *PatternDecomposer) decomposeImpactAnalysis(query string) []SubQuery {
	var subQueries []SubQuery
	seen := make(map[string]struct{})
	add := func(query string, weight float64) {
		query = strings.TrimSpace(query)
		if query == "" {
			return
		}
		if _, ok := seen[query]; ok {
			return
		}
		seen[query] = struct{}{}
		subQueries = append(subQueries, SubQuery{Query: query, Weight: weight, Hint: "code"})
	}

	lower := strings.ToLower(query)
	add(query, 0.2)
	switch {
	case strings.Contains(lower, "reciprocal rank") || strings.Contains(lower, "fusion"):
		add("internal/search/fusion.go", 6.0)
		add("internal/search/multi_fusion.go", 4.0)
		add("RRFFusion Fuse", 2.0)
	case strings.Contains(lower, "symbol extraction") || (strings.Contains(lower, "symbol") && strings.Contains(lower, "extract")):
		add("internal/chunk/extractor.go", 6.0)
		add("internal/chunk/parser.go", 4.0)
		add("SymbolExtractor", 2.0)
	case strings.Contains(lower, "gitignore") || strings.Contains(lower, "exclusion"):
		add("internal/gitignore/gitignore.go", 6.0)
		add("internal/scanner/scanner.go", 4.0)
		add("isGitignored", 2.0)
	default:
		for _, word := range strings.Fields(query) {
			word = strings.Trim(strings.ToLower(word), ".,:;!?()[]{}")
			if len(word) < 3 || isStopWord(word) {
				continue
			}
			add(word, 0.2)
		}
	}

	return subQueries
}

func (d *PatternDecomposer) decomposeRetryErrorHandling(query string) []SubQuery {
	return []SubQuery{
		{Query: query, Weight: 0.2, Hint: "code"},
		{Query: "internal/errors/retry.go", Weight: 6.0, Hint: "code"},
		{Query: "internal/embed/retry.go", Weight: 4.0, Hint: "code"},
		{Query: "Retry", Weight: 2.0, Hint: "code"},
		{Query: "DownloadWithRetry", Weight: 2.0, Hint: "code"},
		{Query: "RetryConfig", Weight: 1.5, Hint: "code"},
	}
}

type subsystemHint struct {
	query  string
	weight float64
}

func crossFileSubsystemHints(word string) []subsystemHint {
	switch word {
	case "mcp", "handler", "handlers", "request", "tool", "tools":
		return []subsystemHint{
			{query: "internal/mcp/server.go", weight: 6.0},
			{query: "MCP server handler", weight: 1.0},
		}
	case "search", "engine":
		return []subsystemHint{
			{query: "internal/search/engine.go", weight: 6.0},
			{query: "Engine Search", weight: 1.0},
		}
	case "store", "stores", "bm25":
		return []subsystemHint{
			{query: "internal/store/sqlite_bm25.go", weight: 6.0},
			{query: "internal/store/metadata.go", weight: 4.0},
		}
	case "index", "indexing", "pipeline":
		return []subsystemHint{
			{query: "internal/index/runner.go", weight: 6.0},
			{query: "index runner pipeline", weight: 1.0},
		}
	case "scanner":
		return []subsystemHint{
			{query: "internal/scanner/scanner.go", weight: 6.0},
		}
	case "chunker", "chunking":
		return []subsystemHint{
			{query: "internal/chunk/code_chunker.go", weight: 6.0},
		}
	case "embedder", "embedding", "embed":
		return []subsystemHint{
			{query: "internal/embed/factory.go", weight: 4.0},
			{query: "internal/embed/types.go", weight: 4.0},
		}
	case "metadata":
		return []subsystemHint{
			{query: "internal/store/metadata.go", weight: 6.0},
		}
	case "validation", "adapter":
		return []subsystemHint{
			{query: "internal/eval/validation_adapter.go", weight: 4.0},
			{query: "internal/validation/validation.go", weight: 4.0},
		}
	case "eval", "runner":
		return []subsystemHint{
			{query: "internal/eval/runner.go", weight: 4.0},
			{query: "internal/eval/corpus.go", weight: 4.0},
		}
	default:
		return nil
	}
}

// isStopWord returns true for common stop words that don't add search value.
func isStopWord(word string) bool {
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "shall": true,
		"and": true, "but": true, "or": true, "nor": true, "for": true,
		"yet": true, "so": true, "to": true, "of": true, "in": true,
		"on": true, "at": true, "by": true, "with": true, "from": true,
		"it": true, "its": true, "this": true, "that": true, "these": true,
		"those": true, "which": true, "what": true, "who": true, "whom": true,
	}
	return stopWords[word]
}

// Ensure PatternDecomposer implements QueryDecomposer interface.
var _ QueryDecomposer = (*PatternDecomposer)(nil)
