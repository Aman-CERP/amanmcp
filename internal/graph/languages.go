package graph

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/Aman-CERP/amanmcp/internal/language"
)

// graphExtractorSupportedLanguages is the set of languages the cheap graph
// extractor provides symbol/import/package semantics for. It mirrors the
// language branches in extractors.go (go, typescript, javascript, python).
var graphExtractorSupportedLanguages = map[string]struct{}{
	"go":         {},
	"typescript": {},
	"javascript": {},
	"python":     {},
}

func graphExtractorSupportsLanguage(lang string) bool {
	lang = normalizeGraphQueryLanguage(lang, "")
	if lang == "" {
		return false
	}
	_, ok := graphExtractorSupportedLanguages[lang]
	return ok
}

// graphLanguageNeedsUnsupportedWarning reports whether a detected language should
// surface the unsupported_language warning. Doc/config/text surfaces are valid
// graph subjects with their own extractors and must not be treated as missing
// code-language extractors.
func graphLanguageNeedsUnsupportedWarning(lang string) bool {
	if lang == "" || graphExtractorSupportsLanguage(lang) {
		return false
	}
	def, ok := language.DefaultRegistry().GetByName(lang)
	if !ok {
		return false
	}
	return def.ContentType == language.ContentTypeCode
}

func inferQueryLanguage(query, subjectType string) (string, bool) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", false
	}
	if subjectType != SubjectTypePath && !looksLikePath(query) && !looksLikeFilename(query) {
		return "", false
	}
	lang := normalizeGraphQueryLanguage(language.DefaultRegistry().Detect(query), query)
	if lang == "" {
		return "", false
	}
	return lang, true
}

// inferLanguageFromSymbolPrefix detects eval-style symbol names such as
// rust_main_entry_only_gra26 where the language prefix is an explicit corpus
// signal. It only fires for registered code languages without graph extractors.
func inferLanguageFromSymbolPrefix(query string) (string, bool) {
	query = strings.ToLower(strings.TrimSpace(query))
	idx := strings.Index(query, "_")
	if idx <= 0 {
		return "", false
	}
	candidate := query[:idx]
	if !graphLanguageNeedsUnsupportedWarning(candidate) {
		return "", false
	}
	return candidate, true
}

func looksLikeFilename(query string) bool {
	base := filepath.Base(query)
	return strings.Contains(base, ".") && !strings.Contains(filepath.ToSlash(query), "/")
}

func normalizeGraphQueryLanguage(languageName, filePath string) string {
	languageName = strings.ToLower(strings.TrimSpace(languageName))
	switch languageName {
	case "ts", "tsx", "typescriptreact":
		return "typescript"
	case "js", "jsx", "javascriptreact", "node":
		return "javascript"
	case "py":
		return "python"
	case "":
		switch strings.ToLower(path.Ext(filePath)) {
		case ".ts", ".tsx":
			return "typescript"
		case ".js", ".jsx", ".mjs", ".cjs":
			return "javascript"
		case ".py":
			return "python"
		}
	}
	return languageName
}

func resolveQueryTargetLanguage(query, subjectType string, resolution subjectResolution) string {
	if lang := languageFromSeeds(resolution.Seeds); lang != "" {
		return lang
	}
	if lang, ok := inferLanguageFromSymbolPrefix(query); ok {
		return lang
	}
	if lang, ok := inferQueryLanguage(query, subjectType); ok {
		return lang
	}
	return ""
}

func languageFromSeeds(seeds []Node) string {
	if len(seeds) == 0 {
		return ""
	}
	lang := normalizeGraphQueryLanguage(seeds[0].Language, seeds[0].SourcePath)
	if lang == "" {
		return ""
	}
	for _, seed := range seeds[1:] {
		if normalizeGraphQueryLanguage(seed.Language, seed.SourcePath) != lang {
			return ""
		}
	}
	return lang
}

func unsupportedLanguageWarning(lang string) StatusWarning {
	display := languageDisplayName(lang)
	return StatusWarning{
		Code: WarningUnsupportedLanguage,
		Message: fmt.Sprintf(
			"%s graph extractor not present; results are based on filename/text-mention heuristics only",
			display,
		),
	}
}

func languageDisplayName(lang string) string {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return "Unknown"
	}
	return strings.ToUpper(lang[:1]) + lang[1:]
}

func appendUnsupportedLanguageWarning(response *QueryResponse, query, subjectType string, resolution subjectResolution) {
	lang := resolveQueryTargetLanguage(query, subjectType, resolution)
	if !graphLanguageNeedsUnsupportedWarning(lang) {
		return
	}
	response.Warnings = append(response.Warnings, unsupportedLanguageWarning(lang))
}
