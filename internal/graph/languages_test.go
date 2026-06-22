package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraphExtractorSupportsLanguage_KnownSupportedLanguages(t *testing.T) {
	for _, lang := range []string{"go", "typescript", "javascript", "python"} {
		assert.True(t, graphExtractorSupportsLanguage(lang), "expected %q supported", lang)
	}
}

func TestGraphExtractorSupportsLanguage_UnsupportedLanguages(t *testing.T) {
	for _, lang := range []string{"rust", "ruby", "java", ""} {
		assert.False(t, graphExtractorSupportsLanguage(lang), "expected %q unsupported", lang)
	}
}

func TestInferQueryLanguage_PathAndFilename(t *testing.T) {
	cases := []struct {
		name        string
		query       string
		subjectType string
		want        string
	}{
		{name: "rust path", query: "src/main.rs", subjectType: SubjectTypePath, want: "rust"},
		{name: "rust filename", query: "main.rs", subjectType: SubjectTypeAuto, want: "rust"},
		{name: "go path", query: "internal/graph/query.go", subjectType: SubjectTypePath, want: "go"},
		{name: "markdown doc path", query: "docs/changelog.md", subjectType: SubjectTypePath, want: "markdown"},
		{name: "bare symbol", query: "rust_main_entry_only_gra26", subjectType: SubjectTypeSymbol, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := inferQueryLanguage(tc.query, tc.subjectType)
			if tc.want == "" {
				assert.False(t, ok)
				assert.Empty(t, got)
				return
			}
			require.True(t, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestGraphLanguageNeedsUnsupportedWarning_ExcludesDocAndConfigSurfaces(t *testing.T) {
	assert.False(t, graphLanguageNeedsUnsupportedWarning("markdown"))
	assert.False(t, graphLanguageNeedsUnsupportedWarning("yaml"))
	assert.True(t, graphLanguageNeedsUnsupportedWarning("rust"))
	assert.False(t, graphLanguageNeedsUnsupportedWarning("go"))
}

func TestInferLanguageFromSymbolPrefix_DetectsUnsupportedCodeLanguages(t *testing.T) {
	got, ok := inferLanguageFromSymbolPrefix("rust_main_entry_only_gra26")
	require.True(t, ok)
	assert.Equal(t, "rust", got)

	_, ok = inferLanguageFromSymbolPrefix("Search")
	assert.False(t, ok)

	_, ok = inferLanguageFromSymbolPrefix("python_handler")
	assert.False(t, ok)
}

func TestResolveQueryTargetLanguage_SymbolPrefixForAutoSubject(t *testing.T) {
	lang := resolveQueryTargetLanguage("rust_main_entry_only_gra26", SubjectTypeAuto, subjectResolution{
		Outcome: ResolutionSubjectNotFound,
	})
	assert.Equal(t, "rust", lang)
}

func TestUnsupportedLanguageWarning_MessageIsExplicit(t *testing.T) {
	warning := unsupportedLanguageWarning("rust")
	assert.Equal(t, WarningUnsupportedLanguage, warning.Code)
	assert.Contains(t, warning.Message, "Rust")
	assert.Contains(t, warning.Message, "extractor not present")
	assert.Contains(t, warning.Message, "filename/text-mention heuristics")
}
