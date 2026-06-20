package scanner

import (
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/language"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectLanguageWithRegistry_PreservesCurrentAliases(t *testing.T) {
	registry := language.DefaultRegistry()

	tests := []struct {
		path     string
		language string
	}{
		{path: "Component.tsx", language: "typescript"},
		{path: "Component.jsx", language: "javascript"},
		{path: "script.py", language: "python"},
		{path: "script.pyw", language: "python"},
		{path: "types.pyi", language: "python"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.language, DetectLanguageWithRegistry(tt.path, registry))
		})
	}
}

func TestDetectLanguageWithRegistry_DetectsPDFAsFirstClassContent(t *testing.T) {
	registry := language.DefaultRegistry()

	assert.Equal(t, "pdf", DetectLanguageWithRegistry("docs/spec.pdf", registry))
	assert.Equal(t, ContentTypePDF, DetectContentTypeWithRegistry("pdf", registry))
}

func TestDetectLanguageWithRegistry_ConfigAddedLineFallbackLanguage(t *testing.T) {
	registry, err := language.NewRegistry([]language.Definition{{
		Name:        "elixir_custom",
		Extensions:  []string{"exx"},
		ContentType: string(language.ContentTypeCode),
		Parser:      language.ParserLineFallback,
	}})
	require.NoError(t, err)

	assert.Equal(t, "elixir_custom", DetectLanguageWithRegistry("lib/example.exx", registry))
	assert.Equal(t, ContentTypeCode, DetectContentTypeWithRegistry("elixir_custom", registry))
}
