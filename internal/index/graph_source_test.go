package index

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/chunk"
	"github.com/Aman-CERP/amanmcp/internal/graph"
	"github.com/Aman-CERP/amanmcp/internal/scanner"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

func TestGraphSourceFromChunkedFile_PreservesContentTypeSymbolsAndPaths(t *testing.T) {
	file := &scanner.FileInfo{
		Path:        "internal/index/runner.go",
		Language:    "go",
		ContentType: scanner.ContentTypeCode,
	}
	content := []byte("package index\n\nfunc Run() {}\n")
	chunks := []*chunk.Chunk{
		{
			ID:          "chunk-1",
			FilePath:    "internal/index/runner.go",
			Language:    "go",
			StartLine:   3,
			EndLine:     3,
			ContentType: chunk.ContentTypeCode,
			Symbols: []*chunk.Symbol{
				{
					Name:      "Run",
					Type:      chunk.SymbolTypeFunction,
					StartLine: 3,
					EndLine:   3,
					Signature: "func Run()",
				},
			},
		},
	}

	source, ok := graphSourceFromChunkedFile(file, content, chunks)

	require.True(t, ok)
	assert.Equal(t, "internal/index/runner.go", source.Path)
	assert.Equal(t, "go", source.Language)
	assert.Equal(t, graph.SourceContentTypeCode, source.ContentType)
	assert.Equal(t, content, source.Content)
	require.Len(t, source.Chunks, 1)
	assert.Equal(t, "chunk-1", source.Chunks[0].ID)
	assert.Equal(t, "internal/index/runner.go", source.Chunks[0].FilePath)
	require.Len(t, source.Chunks[0].Symbols, 1)
	assert.Equal(t, graph.SourceSymbol{
		Name:      "Run",
		Kind:      "function",
		StartLine: 3,
		EndLine:   3,
		Signature: "func Run()",
	}, source.Chunks[0].Symbols[0])
}

func TestGraphSourceFromStoreFile_PreservesConfigWithoutChunks(t *testing.T) {
	file := &store.File{
		Path:        ".amanmcp.yaml",
		Language:    "yaml",
		ContentType: string(scanner.ContentTypeConfig),
	}
	content := []byte("embeddings:\n  provider: static\n")

	source, ok := graphSourceFromStoreFile(file, content, nil)

	require.True(t, ok)
	assert.Equal(t, ".amanmcp.yaml", source.Path)
	assert.Equal(t, "yaml", source.Language)
	assert.Equal(t, graph.SourceContentTypeConfig, source.ContentType)
	assert.Equal(t, content, source.Content)
	assert.Empty(t, source.Chunks)
}

func TestReadIndexedSourceFile_RejectsPathsEscapingProjectRoot(t *testing.T) {
	root := t.TempDir()

	for _, path := range []string{"../secret.go", "/tmp/secret.go"} {
		t.Run(path, func(t *testing.T) {
			content, err := readIndexedSourceFile(root, path)

			require.Error(t, err)
			assert.Nil(t, content)
			assert.Contains(t, err.Error(), "escapes project root")
		})
	}
}
