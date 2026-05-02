package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_SearchLanguages_LineFallbackRegistration(t *testing.T) {
	tmpDir := t.TempDir()
	configContent := `
version: 1
search:
  languages:
    - name: elixir_custom
      extensions: [exx]
      content_type: code
      parser: line_fallback
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yaml"), []byte(configContent), 0o644))

	cfg, err := Load(tmpDir)

	require.NoError(t, err)
	require.Len(t, cfg.Search.Languages, 1)
	assert.Equal(t, "elixir_custom", cfg.Search.Languages[0].Name)
	assert.Equal(t, []string{".exx"}, cfg.Search.Languages[0].Extensions)
	assert.Equal(t, "code", cfg.Search.Languages[0].ContentType)
	assert.Equal(t, "line_fallback", cfg.Search.Languages[0].Parser)
}

func TestLoad_SearchLanguages_RejectsDuplicateExtension(t *testing.T) {
	tmpDir := t.TempDir()
	configContent := `
version: 1
search:
  languages:
    - name: custom_go
      extensions: [.go]
      content_type: code
      parser: go
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yaml"), []byte(configContent), 0o644))

	cfg, err := Load(tmpDir)

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "extension .go")
}

func TestLoad_SearchLanguages_RejectsUnknownParser(t *testing.T) {
	tmpDir := t.TempDir()
	configContent := `
version: 1
search:
  languages:
    - name: unsafe_parser
      extensions: [.unsafe]
      content_type: code
      parser: tree-sitter-from-path
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yaml"), []byte(configContent), 0o644))

	cfg, err := Load(tmpDir)

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "unknown parser")
}

func TestLoad_SearchLanguages_RejectsUnknownSymbolNodeKind(t *testing.T) {
	tmpDir := t.TempDir()
	configContent := `
version: 1
search:
  languages:
    - name: go_custom
      extensions: [.gox]
      content_type: code
      parser: go
      function_types: [definitely_not_a_tree_sitter_node]
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yaml"), []byte(configContent), 0o644))

	cfg, err := Load(tmpDir)

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "unknown symbol node kind")
}
