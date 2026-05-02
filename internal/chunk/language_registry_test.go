package chunk

import (
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/language"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLanguageRegistry_DefaultsSupportCurrentLanguages(t *testing.T) {
	registry := NewLanguageRegistry()

	tests := []struct {
		extension string
		name      string
		parser    string
	}{
		{extension: ".go", name: "go", parser: language.ParserGo},
		{extension: ".ts", name: "typescript", parser: language.ParserTypeScript},
		{extension: ".tsx", name: "tsx", parser: language.ParserTSX},
		{extension: ".js", name: "javascript", parser: language.ParserJavaScript},
		{extension: ".jsx", name: "jsx", parser: language.ParserJavaScript},
		{extension: ".py", name: "python", parser: language.ParserPython},
		{extension: ".pyw", name: "python", parser: language.ParserPython},
		{extension: ".pyi", name: "python", parser: language.ParserPython},
	}

	for _, tt := range tests {
		t.Run(tt.extension, func(t *testing.T) {
			cfg, ok := registry.GetByExtension(tt.extension)
			require.True(t, ok)
			assert.Equal(t, tt.name, cfg.Name)
			assert.Equal(t, tt.parser, cfg.Parser)
		})
	}
}

func TestLanguageRegistry_ConfigAddedLineFallbackLanguage(t *testing.T) {
	registry, err := NewLanguageRegistryFromDefinitions([]language.Definition{{
		Name:        "elixir_custom",
		Extensions:  []string{"exx"},
		ContentType: string(language.ContentTypeCode),
		Parser:      language.ParserLineFallback,
	}})
	require.NoError(t, err)

	cfg, ok := registry.GetByExtension(".exx")
	require.True(t, ok)
	assert.Equal(t, "elixir_custom", cfg.Name)
	assert.True(t, cfg.LineFallback)
	assert.Equal(t, "config", cfg.ConfigSource)
}

func TestLanguageRegistry_RejectsDuplicateExtension(t *testing.T) {
	_, err := NewLanguageRegistryFromDefinitions([]language.Definition{{
		Name:        "custom_go",
		Extensions:  []string{".go"},
		ContentType: string(language.ContentTypeCode),
		Parser:      language.ParserGo,
	}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "extension .go")
}

func TestLanguageRegistry_RejectsUnknownParser(t *testing.T) {
	_, err := NewLanguageRegistryFromDefinitions([]language.Definition{{
		Name:        "custom",
		Extensions:  []string{".custom"},
		ContentType: string(language.ContentTypeCode),
		Parser:      "remote_parser",
	}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown parser")
}
