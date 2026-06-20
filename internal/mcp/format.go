package mcp

import (
	"fmt"
	"strings"

	"github.com/Aman-CERP/amanmcp/internal/search"
)

// FormatSearchResults formats generic search results as markdown.
func FormatSearchResults(query string, results []*search.SearchResult) string {
	return FormatSearchResultsWithProfileMismatches(query, results, nil)
}

// FormatSearchResultsWithProfileMismatches formats generic search results as
// markdown and includes actionable profile-filter diagnostics when available.
func FormatSearchResultsWithProfileMismatches(query string, results []*search.SearchResult, mismatches []search.ProfileMismatch) string {
	// Filter out nil chunks
	validResults := filterValidResults(results)

	if len(validResults) == 0 {
		if len(mismatches) > 0 {
			return fmt.Sprintf("No results found for \"%s\"\n\n%s", query, formatProfileMismatchHint(mismatches))
		}
		return fmt.Sprintf("No results found for \"%s\"", query)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Search Results for \"%s\"\n\n", query))
	if len(mismatches) > 0 {
		sb.WriteString(formatProfileMismatchHint(mismatches))
		sb.WriteString("\n\n")
	}
	sb.WriteString(fmt.Sprintf("Found %d result", len(validResults)))
	if len(validResults) != 1 {
		sb.WriteString("s")
	}
	sb.WriteString("\n\n")

	for i, r := range validResults {
		formatResult(&sb, i+1, r)
	}

	return sb.String()
}

func formatProfileMismatchHint(mismatches []search.ProfileMismatch) string {
	if len(mismatches) == 0 {
		return ""
	}
	first := mismatches[0]
	return fmt.Sprintf(
		"**Profile mismatch:** %d result(s) were excluded by the selected profile. Select profile `%s` to inspect `%s`.",
		len(mismatches),
		first.RequiredProfile,
		first.SourcePath,
	)
}

// FormatCodeResults formats code-specific results with syntax highlighting.
func FormatCodeResults(query string, results []*search.SearchResult, langFilter string) string {
	// Filter out nil chunks
	validResults := filterValidResults(results)

	if len(validResults) == 0 {
		msg := fmt.Sprintf("No code results found for \"%s\"", query)
		if langFilter != "" {
			msg += fmt.Sprintf(" in %s files", langFilter)
		}
		return msg
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Code Search Results for \"%s\"\n\n", query))
	if langFilter != "" {
		sb.WriteString(fmt.Sprintf("Language filter: `%s`\n\n", langFilter))
	}
	sb.WriteString(fmt.Sprintf("Found %d result", len(validResults)))
	if len(validResults) != 1 {
		sb.WriteString("s")
	}
	sb.WriteString("\n\n")

	for i, r := range validResults {
		formatResult(&sb, i+1, r)
	}

	return sb.String()
}

// FormatDocsResults formats documentation results preserving section hierarchy.
func FormatDocsResults(query string, results []*search.SearchResult) string {
	// Filter out nil chunks
	validResults := filterValidResults(results)

	if len(validResults) == 0 {
		return fmt.Sprintf("No documentation found for \"%s\"", query)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Documentation Results for \"%s\"\n\n", query))
	sb.WriteString(fmt.Sprintf("Found %d result", len(validResults)))
	if len(validResults) != 1 {
		sb.WriteString("s")
	}
	sb.WriteString("\n\n")

	for i, r := range validResults {
		formatDocsResult(&sb, i+1, r)
	}

	return sb.String()
}

// filterValidResults removes results with nil chunks.
func filterValidResults(results []*search.SearchResult) []*search.SearchResult {
	valid := make([]*search.SearchResult, 0, len(results))
	for _, r := range results {
		if r != nil && r.Chunk != nil {
			valid = append(valid, r)
		}
	}
	return valid
}

// formatResult formats a single generic result.
func formatResult(sb *strings.Builder, num int, r *search.SearchResult) {
	if r.Chunk == nil {
		return
	}

	// Header with file path, line numbers, score
	fmt.Fprintf(sb, "### %d. %s:%d-%d (score: %.2f)\n",
		num,
		r.Chunk.FilePath,
		r.Chunk.StartLine,
		r.Chunk.EndLine,
		r.Score,
	)

	// Symbol names if available
	if len(r.Chunk.Symbols) > 0 {
		names := make([]string, len(r.Chunk.Symbols))
		for j, sym := range r.Chunk.Symbols {
			names[j] = fmt.Sprintf("`%s`", sym.Name)
		}
		fmt.Fprintf(sb, "**Symbols:** %s\n\n", strings.Join(names, ", "))
	}

	// Code block with language hint
	lang := r.Chunk.Language
	if lang == "" {
		lang = "text"
	}

	// Use RawContent for code, Content for docs/text
	content := r.Chunk.RawContent
	if content == "" {
		content = r.Chunk.Content
	}

	fmt.Fprintf(sb, "```%s\n%s\n```\n\n", lang, content)
}

// formatDocsResult formats a documentation result preserving structure.
func formatDocsResult(sb *strings.Builder, num int, r *search.SearchResult) {
	if r.Chunk == nil {
		return
	}

	fmt.Fprintf(sb, "### %d. %s (score: %.2f)\n\n",
		num,
		r.Chunk.FilePath,
		r.Score,
	)

	// For markdown, preserve the content as-is (no code block wrapping)
	if r.Chunk.Language == "markdown" || r.Chunk.Language == "md" {
		sb.WriteString(r.Chunk.Content)
		sb.WriteString("\n\n---\n\n")
	} else {
		fmt.Fprintf(sb, "```\n%s\n```\n\n", r.Chunk.Content)
	}
}

// clampLimit ensures limit is within bounds.
func clampLimit(limit, defaultVal, min, max int) int {
	if limit <= 0 {
		return defaultVal
	}
	if limit < min {
		return min
	}
	if limit > max {
		return max
	}
	return limit
}

// ToSearchResultOutput converts a search result to the enhanced output format.
// UX-1: Returns context-rich metadata explaining WHY results matched.
func ToSearchResultOutput(r *search.SearchResult) SearchResultOutput {
	if r == nil || r.Chunk == nil {
		return SearchResultOutput{}
	}

	output := SearchResultOutput{
		FilePath:            r.Chunk.FilePath,
		Content:             r.Chunk.Content,
		Score:               r.Score,
		Language:            r.Chunk.Language,
		LanguageSupportTier: languageSupportTierForChunk(r.Chunk),
		ContentType:         string(r.Chunk.ContentType),
		MatchedTerms:        r.MatchedTerms,
		InBothLists:         r.InBothLists,
	}
	if r.Chunk.Metadata != nil {
		output.Chunker = r.Chunk.Metadata["chunker"]
		output.PageNumber = r.Chunk.Metadata["page_number"]
		output.PageStart = r.Chunk.Metadata["page_start"]
		output.PageEnd = r.Chunk.Metadata["page_end"]
		if contentType := r.Chunk.Metadata["content_type"]; contentType != "" {
			output.ContentType = contentType
		}
	}

	sourceMeta := r.SourceMetadata
	if sourceMeta.SourceClass == "" {
		sourceMeta = search.SourceMetadataFromChunk(r.Chunk)
	}
	output.SourceClass = string(sourceMeta.SourceClass)
	output.Authority = string(sourceMeta.Authority)
	output.Profile = string(sourceMeta.Profile)
	output.SourcePath = sourceMeta.SourcePath
	output.GitStatus = sourceMeta.GitStatus
	output.SourceHash = sourceMeta.SourceHash
	output.Generated = sourceMeta.Generated
	output.Stale = sourceMeta.Stale
	output.FreshnessReason = sourceMeta.FreshnessReason
	output.DecisionStatus = string(sourceMeta.DecisionStatus)
	output.Supersedes = append([]string(nil), sourceMeta.Supersedes...)
	output.SupersededBy = append([]string(nil), sourceMeta.SupersededBy...)
	if sourceMeta.LastModified != nil {
		output.LastModified = sourceMeta.LastModified.Format("2006-01-02T15:04:05Z07:00")
	}
	if sourceMeta.CurrentAsOf != nil {
		output.CurrentAsOf = sourceMeta.CurrentAsOf.Format("2006-01-02T15:04:05Z07:00")
	}

	// Extract primary symbol info if available
	if len(r.Chunk.Symbols) > 0 {
		sym := r.Chunk.Symbols[0]
		output.Symbol = sym.Name
		output.SymbolType = string(sym.Type)
		output.Signature = sym.Signature
	}

	// Generate human-readable match reason
	output.MatchReason = generateMatchReason(r)

	return output
}

// generateMatchReason creates a human-readable explanation of why a result matched.
func generateMatchReason(r *search.SearchResult) string {
	if r == nil || r.Chunk == nil {
		return ""
	}

	var parts []string

	// Symbol-based reason
	if len(r.Chunk.Symbols) > 0 {
		sym := r.Chunk.Symbols[0]
		parts = append(parts, fmt.Sprintf("%s '%s'", sym.Type, sym.Name))
		if sym.DocComment != "" {
			// Extract first line of docstring if present
			docLine := sym.DocComment
			if idx := strings.Index(docLine, "\n"); idx > 0 {
				docLine = docLine[:idx]
			}
			if len(docLine) > 50 {
				docLine = docLine[:47] + "..."
			}
			parts = append(parts, fmt.Sprintf("documented as: %s", docLine))
		}
	}

	// Term-based reason
	if len(r.MatchedTerms) > 0 {
		terms := r.MatchedTerms
		if len(terms) > 5 {
			terms = terms[:5]
		}
		parts = append(parts, fmt.Sprintf("matched: %s", strings.Join(terms, ", ")))
	}

	// Both lists indicator
	if r.InBothLists {
		parts = append(parts, "found in both keyword and semantic search")
	}

	if len(parts) == 0 {
		return "matched content"
	}

	return strings.Join(parts, "; ")
}
