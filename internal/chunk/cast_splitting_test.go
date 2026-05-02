package chunk

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodeChunker_RecursiveCASTSplitting_TypeScriptClassSplitsByMethods(t *testing.T) {
	methods := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		methods = append(methods, fmt.Sprintf(`
	method%d(): number {
		const values = [
			"%s",
		];
		return values.length;
	}
`, i, strings.Repeat("x", 320)))
	}
	source := `export class LargeService {
` + strings.Join(methods, "\n") + `
}
`
	chunker := NewCodeChunkerWithOptions(CodeChunkerOptions{MaxChunkTokens: 140})
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "large-service.ts",
		Content:  []byte(source),
		Language: "typescript",
	})

	require.NoError(t, err)
	require.Greater(t, len(chunks), 1)
	assert.Contains(t, chunks[0].Metadata, "parent_symbol")
	assert.Equal(t, "LargeService", chunks[0].Metadata["parent_symbol"])
	assert.Equal(t, "ast_recursive", chunks[0].Metadata["split_strategy"])

	names := collectSymbolNames(chunks)
	assert.Contains(t, names, "LargeService")
	assert.Contains(t, names, "method0")
	assert.Contains(t, names, "method4")

	for _, c := range chunks {
		if strings.Contains(c.RawContent, "method0") {
			assert.NotContains(t, c.RawContent, "method4")
		}
	}
}

func TestCodeChunker_RecursiveCASTSplitting_PythonClassSplitsByMethods(t *testing.T) {
	methods := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		methods = append(methods, fmt.Sprintf(`
    def step_%d(self):
        payload = "%s"
        return len(payload)
`, i, strings.Repeat("p", 280)))
	}
	source := "class Pipeline:\n" + strings.Join(methods, "\n")
	chunker := NewCodeChunkerWithOptions(CodeChunkerOptions{MaxChunkTokens: 120})
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "pipeline.py",
		Content:  []byte(source),
		Language: "python",
	})

	require.NoError(t, err)
	require.Greater(t, len(chunks), 1)
	assert.Equal(t, "Pipeline", chunks[0].Metadata["parent_symbol"])
	assert.Equal(t, "ast_recursive", chunks[0].Metadata["split_strategy"])

	names := collectSymbolNames(chunks)
	assert.Contains(t, names, "Pipeline")
	assert.Contains(t, names, "step_0")
	assert.Contains(t, names, "step_3")
}

func TestCodeChunker_RecursiveCASTSplitting_FallsBackToLineSplitWhenNoSemanticBoundary(t *testing.T) {
	lines := make([]string, 0, 160)
	for i := 0; i < 160; i++ {
		lines = append(lines, fmt.Sprintf("\tfmt.Println(%q)", strings.Repeat("g", 80)))
	}
	source := `package main

import "fmt"

func LargeFlatFunction() {
` + strings.Join(lines, "\n") + `
}
`
	chunker := NewCodeChunkerWithOptions(CodeChunkerOptions{MaxChunkTokens: 180})
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "flat.go",
		Content:  []byte(source),
		Language: "go",
	})

	require.NoError(t, err)
	require.Greater(t, len(chunks), 1)
	assert.Equal(t, "line_fallback", chunks[0].Metadata["split_strategy"])
	assert.Equal(t, "no_semantic_children", chunks[0].Metadata["split_reason"])
	assert.Equal(t, "LargeFlatFunction", chunks[0].Metadata["parent_symbol"])

	names := collectSymbolNames(chunks)
	assert.Contains(t, names, "LargeFlatFunction")
	assert.Contains(t, names, "LargeFlatFunction_part1")
}

func TestCodeChunker_RecursiveCASTSplitting_FirstOversizedChildKeepsParentSymbol(t *testing.T) {
	largeStatements := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		largeStatements = append(largeStatements, fmt.Sprintf(`		const value%d = "%s";`, i, strings.Repeat("x", 60)))
	}
	source := `export class LargeService {
	init(): number {
` + strings.Join(largeStatements, "\n") + `
		return 1;
	}

	tiny(): number {
		return 2;
	}
}
`
	chunker := NewCodeChunkerWithOptions(CodeChunkerOptions{MaxChunkTokens: 120})
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "large-service.ts",
		Content:  []byte(source),
		Language: "typescript",
	})

	require.NoError(t, err)
	require.Greater(t, len(chunks), 1)
	assert.Contains(t, collectChunkSymbolNames(chunks[0]), "LargeService")
}

func TestCodeChunker_RecursiveCASTSplitting_SplitsOversizedChildrenBeforeLineFallback(t *testing.T) {
	nestedFunctions := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		nestedFunctions = append(nestedFunctions, fmt.Sprintf(`
		const nested%d = () => {
			const payload = "%s";
			return payload.length;
		};
`, i, strings.Repeat("n", 220)))
	}
	source := `export class NestedService {
	huge(): number {
` + strings.Join(nestedFunctions, "\n") + `
		return 1;
	}
}
`
	chunker := NewCodeChunkerWithOptions(CodeChunkerOptions{MaxChunkTokens: 120})
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "nested-service.ts",
		Content:  []byte(source),
		Language: "typescript",
	})

	require.NoError(t, err)
	require.Greater(t, len(chunks), 1)

	names := collectSymbolNames(chunks)
	assert.Contains(t, names, "NestedService")
	assert.Contains(t, names, "huge")
	assert.Contains(t, names, "nested0")
	assert.Contains(t, names, "nested3")

	for _, c := range chunks {
		assert.LessOrEqual(t, estimateTokens(c.RawContent), 120, "chunk %s exceeded token budget", c.ID)
	}
}

func TestCodeChunker_RecursiveCASTSplitting_LineFallbackHonorsTokenBudgetForLongLines(t *testing.T) {
	source := `package main

func DenseLiteral() {
	_ = "` + strings.Repeat("x", 1500) + `"
}
`
	chunker := NewCodeChunkerWithOptions(CodeChunkerOptions{MaxChunkTokens: 80})
	defer chunker.Close()

	chunks, err := chunker.Chunk(context.Background(), &FileInput{
		Path:     "dense.go",
		Content:  []byte(source),
		Language: "go",
	})

	require.NoError(t, err)
	require.Greater(t, len(chunks), 1)
	ids := make(map[string]struct{}, len(chunks))
	for _, c := range chunks {
		assert.LessOrEqual(t, estimateTokens(c.RawContent), 80, "chunk %s exceeded token budget", c.ID)
		if _, exists := ids[c.ID]; exists {
			t.Fatalf("duplicate chunk ID %s", c.ID)
		}
		ids[c.ID] = struct{}{}
	}
}

func collectChunkSymbolNames(chunk *Chunk) []string {
	names := make([]string, 0, len(chunk.Symbols))
	for _, sym := range chunk.Symbols {
		names = append(names, sym.Name)
	}
	return names
}

func collectSymbolNames(chunks []*Chunk) []string {
	names := make([]string, 0)
	for _, c := range chunks {
		for _, sym := range c.Symbols {
			names = append(names, sym.Name)
		}
	}
	return names
}
