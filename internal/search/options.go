package search

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

// Score adjustment constants for ranking optimization.
const (
	// TestFilePenalty reduces test file scores to prioritize real implementations.
	// FEAT-QI4: Test files get penalized to prioritize real implementations.
	TestFilePenalty = 0.5

	// InternalPathBoost increases scores for implementation code in internal/.
	// BUG-066: Implementation code should rank higher than CLI wrappers.
	InternalPathBoost = 1.3

	// CmdPathPenalty reduces scores for CLI wrapper code in cmd/.
	// BUG-066: Wrappers match many patterns but users want implementations.
	CmdPathPenalty = 0.6

	// AuthorityRankScale caps metadata priority influence so authority breaks
	// close relevance ties without mutating the public relevance score.
	AuthorityRankScale = 0.12

	// ExactSymbolBoost lifts chunks that define the exact queried symbol above
	// incidental references when the user issues a lexical-exact lookup.
	ExactSymbolBoost = 4.0

	// ExactPathBoost lifts exact file-path matches for path lookup queries.
	ExactPathBoost = 4.0

	// ExactQuotedContentBoost lifts chunks containing the exact quoted phrase.
	ExactQuotedContentBoost = 1.2

	// PDFContentBoost lifts PDF chunks when the user explicitly asks about PDF
	// content inside a broader docs search.
	PDFContentBoost = 3.0

	// DeclarationSymbolTypeMismatchPenalty demotes methods/functions with the
	// requested name when the user asked for a type declaration.
	DeclarationSymbolTypeMismatchPenalty = 0.2
)

// FilterFunc checks if a search result matches filter criteria.
type FilterFunc func(result *SearchResult) bool

// ApplyFilters filters results based on search options.
// Filters use AND logic - results must match all specified criteria.
func ApplyFilters(results []*SearchResult, opts SearchOptions) []*SearchResult {
	if opts.Filter == "all" && opts.Language == "" && opts.SymbolType == "" && len(opts.Scopes) == 0 && opts.Profile == "" && opts.Mode == "" {
		filtered, mismatches := ApplyProfileEligibility(results, opts)
		recordProfileMismatches(opts, mismatches)
		return filtered
	}

	filters := buildFilters(opts)
	if len(filters) == 0 {
		filtered, mismatches := ApplyProfileEligibility(results, opts)
		recordProfileMismatches(opts, mismatches)
		return filtered
	}

	filtered := make([]*SearchResult, 0, len(results))
	for _, r := range results {
		if matchesAllFilters(r, filters) {
			filtered = append(filtered, r)
		}
	}

	if opts.Mode == SearchModeDecisionHistory {
		ApplyAuthorityBoost(filtered)
		return filtered
	}

	profiled, mismatches := ApplyProfileEligibility(filtered, opts)
	recordProfileMismatches(opts, mismatches)
	return profiled
}

// buildFilters creates filter functions based on options.
func buildFilters(opts SearchOptions) []FilterFunc {
	var filters []FilterFunc

	// Content type filter
	if opts.Filter != "" && opts.Filter != "all" {
		filters = append(filters, contentTypeFilter(opts.Filter))
	}

	// Language filter
	if opts.Language != "" {
		filters = append(filters, languageFilter(opts.Language))
	}

	// Symbol type filter
	if opts.SymbolType != "" {
		filters = append(filters, symbolTypeFilter(opts.SymbolType))
	}

	// Scope filter
	if len(opts.Scopes) > 0 {
		filters = append(filters, scopeFilter(opts.Scopes))
	}

	if opts.Mode != "" {
		filters = append(filters, modeFilter(opts.Mode))
	}

	return filters
}

// matchesAllFilters checks if a result passes all filters (AND logic).
func matchesAllFilters(result *SearchResult, filters []FilterFunc) bool {
	for _, f := range filters {
		if !f(result) {
			return false
		}
	}
	return true
}

// contentTypeFilter creates a filter for content type.
func contentTypeFilter(filter string) FilterFunc {
	return func(r *SearchResult) bool {
		if r.Chunk == nil {
			return false
		}

		switch filter {
		case "code":
			return r.Chunk.ContentType == store.ContentTypeCode
		case "docs":
			return r.Chunk.ContentType == store.ContentTypeMarkdown ||
				r.Chunk.ContentType == store.ContentTypePDF ||
				r.Chunk.ContentType == store.ContentTypeText
		default:
			return true
		}
	}
}

// languageFilter creates a filter for programming language.
func languageFilter(lang string) FilterFunc {
	return func(r *SearchResult) bool {
		if r.Chunk == nil {
			return false
		}
		return r.Chunk.Language == lang
	}
}

// symbolTypeFilter creates a filter for symbol type.
func symbolTypeFilter(symbolType string) FilterFunc {
	return func(r *SearchResult) bool {
		if r.Chunk == nil || len(r.Chunk.Symbols) == 0 {
			return false
		}

		targetType := store.SymbolType(symbolType)
		for _, s := range r.Chunk.Symbols {
			if s.Type == targetType {
				return true
			}
		}
		return false
	}
}

// ValidateOptions checks if search options are valid.
func ValidateOptions(opts SearchOptions) error {
	if _, err := ParseProfile(string(opts.Profile)); err != nil {
		return err
	}
	if _, err := ParseMode(string(opts.Mode)); err != nil {
		return err
	}

	// Validate filter value
	switch opts.Filter {
	case "", "all", "code", "docs":
		// Valid
	default:
		// Accept unknown filters but treat as "all"
	}

	return nil
}

func ParseMode(value string) (SearchMode, error) {
	mode := SearchMode(strings.TrimSpace(value))
	switch mode {
	case "", SearchModeDecisions, SearchModeDecisionHistory:
		return mode, nil
	default:
		return "", fmt.Errorf("unknown search mode %q; use one of: decisions, decision-history", value)
	}
}

func recordProfileMismatches(opts SearchOptions, mismatches []ProfileMismatch) {
	if opts.ProfileMismatches == nil || len(mismatches) == 0 {
		return
	}

	seen := make(map[string]struct{}, len(*opts.ProfileMismatches)+len(mismatches))
	for _, mismatch := range *opts.ProfileMismatches {
		seen[profileMismatchKey(mismatch)] = struct{}{}
	}
	for _, mismatch := range mismatches {
		key := profileMismatchKey(mismatch)
		if _, ok := seen[key]; ok {
			continue
		}
		*opts.ProfileMismatches = append(*opts.ProfileMismatches, mismatch)
		seen[key] = struct{}{}
	}
}

func profileMismatchKey(mismatch ProfileMismatch) string {
	return strings.Join([]string{
		mismatch.SourcePath,
		string(mismatch.RequestedProfile),
		string(mismatch.RequiredProfile),
		string(mismatch.SourceClass),
		string(mismatch.Authority),
	}, "\x00")
}

// NormalizeScope ensures consistent path format for matching.
// Strips leading and trailing slashes.
func NormalizeScope(scope string) string {
	return strings.Trim(scope, "/")
}

// scopeFilter creates a filter for path scope prefixes.
// Multiple scopes use OR logic - matches if path starts with ANY scope.
func scopeFilter(scopes []string) FilterFunc {
	// Pre-normalize all scopes once for performance
	// Add trailing slash to ensure directory boundary matching
	// e.g., "services/api" becomes "services/api/" to avoid matching "services/api-v2"
	normalized := make([]string, 0, len(scopes))
	for _, s := range scopes {
		if n := NormalizeScope(s); n != "" {
			normalized = append(normalized, n+"/")
		}
	}

	// If no valid scopes after normalization, match everything
	if len(normalized) == 0 {
		return func(*SearchResult) bool { return true }
	}

	return func(r *SearchResult) bool {
		if r.Chunk == nil {
			return false
		}
		// Normalize file path and add trailing slash for consistent matching
		filePath := NormalizeScope(r.Chunk.FilePath) + "/"
		for _, scope := range normalized {
			if strings.HasPrefix(filePath, scope) {
				return true
			}
		}
		return false
	}
}

func modeFilter(mode SearchMode) FilterFunc {
	return func(r *SearchResult) bool {
		if r == nil {
			return false
		}
		ensureResultMetadata(r)
		switch mode {
		case SearchModeDecisions:
			return r.SourceMetadata.SourceClass == SourceClassADR &&
				!r.SourceMetadata.Generated &&
				!r.SourceMetadata.Stale &&
				r.SourceMetadata.DecisionStatus != DecisionStatusSuperseded &&
				r.SourceMetadata.DecisionStatus != DecisionStatusDeprecated
		case SearchModeDecisionHistory:
			return r.SourceMetadata.SourceClass == SourceClassADR
		default:
			return true
		}
	}
}

// ApplyTestFilePenalty adjusts scores to deprioritize test files.
// FEAT-QI4: Test files contain mock implementations that often outrank real code
// because they have multiple copies of the same method signatures.
// This function applies a 0.5x penalty to test files and re-sorts by adjusted score.
//
// Problem: Query "Search function" returns engine_test.go (MockBM25Index.Search)
// instead of engine.go (Engine.Search) because test files have more term matches.
//
// Solution: Penalize _test.go files to prioritize real implementations.
func ApplyTestFilePenalty(results []*SearchResult) []*SearchResult {
	if len(results) == 0 {
		return results
	}

	// Apply penalty to test files
	for _, r := range results {
		if r.Chunk == nil {
			continue
		}
		if IsTestFile(r.Chunk.FilePath) {
			r.Score *= TestFilePenalty
		}
	}

	// Re-sort by adjusted score (descending)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

// ApplyExactMatchBoost prioritizes exact lexical hits before broader ranking
// adjustments. It is intentionally narrow: identifier queries boost exact
// symbol definitions, path queries boost exact file paths, and quoted queries
// boost exact phrase containment.
func ApplyExactMatchBoost(results []*SearchResult, query string) []*SearchResult {
	if len(results) == 0 {
		return results
	}

	needle, quoted := exactMatchNeedle(query)
	if needle == "" {
		return results
	}
	declarationTypes := declarationSymbolTypes(query)

	for _, r := range results {
		if r == nil || r.Chunk == nil {
			continue
		}

		switch {
		case r.Chunk.FilePath == needle:
			r.Score *= ExactPathBoost
		case len(declarationTypes) > 0 && hasExactSymbolOfType(r.Chunk, needle, declarationTypes):
			r.Score *= ExactSymbolBoost
		case len(declarationTypes) > 0 && hasExactSymbol(r.Chunk, needle):
			r.Score *= DeclarationSymbolTypeMismatchPenalty
		case len(declarationTypes) == 0 && hasExactSymbol(r.Chunk, needle):
			r.Score *= ExactSymbolBoost
		case quoted && strings.Contains(r.Chunk.Content, needle):
			r.Score *= ExactQuotedContentBoost
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

func ApplyPDFContentBoost(results []*SearchResult, query string) []*SearchResult {
	if len(results) == 0 || !shouldBoostPDFContent(query) {
		return results
	}

	for _, r := range results {
		if r == nil || r.Chunk == nil {
			continue
		}
		if r.Chunk.ContentType == store.ContentTypePDF {
			r.Score *= PDFContentBoost
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

func exactMatchNeedle(query string) (string, bool) {
	query = strings.TrimSpace(query)
	if needle := declarationSymbolNeedle(query); needle != "" {
		return needle, false
	}
	if !shouldPreserveExactLexicalQuery(query) {
		return "", false
	}

	if quotedPattern.MatchString(query) && len(query) >= 2 {
		return query[1 : len(query)-1], true
	}

	return query, false
}

func declarationSymbolTypes(query string) []store.SymbolType {
	fields := strings.Fields(strings.TrimSpace(query))
	if len(fields) != 3 || !strings.EqualFold(fields[0], "type") || !isASCIIGoIdentifier(fields[1]) {
		return nil
	}
	switch strings.ToLower(fields[2]) {
	case "struct":
		return []store.SymbolType{store.SymbolTypeType, store.SymbolTypeClass}
	case "interface":
		return []store.SymbolType{store.SymbolTypeInterface, store.SymbolTypeType}
	default:
		return nil
	}
}

func hasExactSymbol(chunk *store.Chunk, name string) bool {
	for _, symbol := range chunk.Symbols {
		if symbol != nil && symbol.Name == name {
			return true
		}
	}
	return false
}

func hasExactSymbolOfType(chunk *store.Chunk, name string, symbolTypes []store.SymbolType) bool {
	for _, symbol := range chunk.Symbols {
		if symbol == nil || symbol.Name != name {
			continue
		}
		for _, symbolType := range symbolTypes {
			if symbol.Type == symbolType {
				return true
			}
		}
	}
	return false
}

// IsTestFile checks if a file path is a test file.
// Supports Go (_test.go), JavaScript/TypeScript (.test.js, .spec.ts, etc.),
// and Python (test_*.py, *_test.py).
func IsTestFile(filePath string) bool {
	// Go test files
	if strings.HasSuffix(filePath, "_test.go") {
		return true
	}

	// JavaScript/TypeScript test files
	if strings.Contains(filePath, ".test.") || strings.Contains(filePath, ".spec.") {
		return true
	}

	// Python test files (test_*.py or *_test.py)
	fileName := filePath
	if idx := strings.LastIndex(filePath, "/"); idx >= 0 {
		fileName = filePath[idx+1:]
	}
	if strings.HasPrefix(fileName, "test_") && strings.HasSuffix(fileName, ".py") {
		return true
	}
	if strings.HasSuffix(fileName, "_test.py") {
		return true
	}

	// Test directories (with or without leading slash)
	if strings.Contains(filePath, "/test/") || strings.Contains(filePath, "/tests/") {
		return true
	}
	if strings.HasPrefix(filePath, "test/") || strings.HasPrefix(filePath, "tests/") {
		return true
	}
	if strings.Contains(filePath, "/__tests__/") || strings.HasPrefix(filePath, "__tests__/") {
		return true
	}

	return false
}

// ApplyPathBoost adjusts scores based on file path to prioritize implementations.
// BUG-066: Multi-query consensus favors wrappers over implementations because
// wrapper files (cmd/) appear in ALL sub-queries while implementations (internal/)
// only appear in a few. This gives wrappers an unfair 1.7x consensus boost.
//
// Solution: Boost internal/ paths (1.3x) and penalize cmd/ paths (0.6x).
// Combined effect: internal/ gets 2.17x advantage over cmd/ (1.3/0.6),
// which overcomes the ~1.55x consensus boost disadvantage.
func ApplyPathBoost(results []*SearchResult) []*SearchResult {
	if len(results) == 0 {
		return results
	}

	for _, r := range results {
		if r.Chunk == nil {
			continue
		}

		path := r.Chunk.FilePath

		// Boost implementation code
		if IsImplementationPath(path) {
			r.Score *= InternalPathBoost
		}

		// Penalize wrapper/CLI code
		if IsWrapperPath(path) {
			r.Score *= CmdPathPenalty
		}
	}

	// Re-sort by adjusted score (descending)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

// ApplyAuthorityBoost sorts results using source authority and freshness
// metadata as a secondary ranking signal. It leaves public scores unchanged so
// RRF/reranker score semantics remain stable for callers.
func ApplyAuthorityBoost(results []*SearchResult) []*SearchResult {
	if len(results) == 0 {
		return results
	}

	for _, r := range results {
		if r == nil {
			continue
		}
		ensureResultMetadata(r)
	}

	sort.Slice(results, func(i, j int) bool {
		return authorityRankScore(results[i]) > authorityRankScore(results[j])
	})

	return results
}

func authorityRankScore(result *SearchResult) float64 {
	if result == nil {
		return 0
	}
	return result.Score + (float64(MetadataPriority(result.SourceMetadata))/100.0)*AuthorityRankScale
}

// IsImplementationPath checks if a path is implementation code (internal/).
func IsImplementationPath(filePath string) bool {
	return strings.HasPrefix(filePath, "internal/") ||
		strings.Contains(filePath, "/internal/")
}

// IsWrapperPath checks if a path is CLI wrapper code (cmd/).
func IsWrapperPath(filePath string) bool {
	return strings.HasPrefix(filePath, "cmd/") ||
		strings.Contains(filePath, "/cmd/")
}
