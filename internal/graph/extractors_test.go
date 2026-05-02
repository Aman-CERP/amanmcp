package graph

import (
	"context"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheapExtractor_ExtractsFoundationEdgeClasses(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)

	files := []SourceFile{
		{
			Path:     "internal/graph/sample.go",
			Language: "go",
			Content: []byte(`package graph

import (
	"context"
	"fmt" // formatting package
)

func Build() {}
`),
			Chunks: []SourceChunk{{
				ID:        "chunk-go-build",
				FilePath:  "internal/graph/sample.go",
				Language:  "go",
				StartLine: 8,
				EndLine:   8,
				Symbols: []SourceSymbol{{
					Name:      "Build",
					Kind:      "function",
					StartLine: 8,
					EndLine:   8,
					Signature: "func Build()",
				}},
			}},
		},
		{
			Path:     "internal/graph/sample_test.go",
			Language: "go",
			Content:  []byte("package graph\n\nfunc TestBuild(t *testing.T) {}\n"),
		},
		{
			Path:        ".amanmcp.yaml",
			Language:    "yaml",
			ContentType: SourceContentTypeConfig,
			Content: []byte(`embedder:
  provider: ollama
search:
  limit: 10
`),
		},
		{
			Path:        "docs/decisions/ADR-001.md",
			Language:    "markdown",
			ContentType: SourceContentTypeMarkdown,
			Content:     []byte("The implementation lives in `internal/graph/sample.go` and config in `.amanmcp.yaml`."),
		},
	}

	require.NoError(t, IndexCheapEdges(ctx, repo, "project-1", files, CheapExtractorOptions{
		Now:        fixedGraphTime,
		StaleAfter: 24 * time.Hour,
	}))

	edges, err := repo.ListEdges(ctx, EdgeQuery{ProjectID: "project-1"})
	require.NoError(t, err)

	byKind := make(map[EdgeKind]int)
	for _, edge := range edges {
		byKind[edge.Kind]++
		require.NotZero(t, edge.Confidence)
		require.NotEmpty(t, edge.ConfidenceLabel)
		require.NotEmpty(t, edge.Evidence.Method)
	}

	assert.Equal(t, 2, byKind[EdgeKindFileDeclaresPackage])
	assert.Equal(t, 2, byKind[EdgeKindPackageImports])
	assert.Equal(t, 1, byKind[EdgeKindFileDefinesSymbol])
	assert.Equal(t, 1, byKind[EdgeKindSymbolHasChunk])
	assert.GreaterOrEqual(t, byKind[EdgeKindFileDefinesConfigKey], 3)
	assert.Equal(t, 1, byKind[EdgeKindTestCoversImplementation])
	assert.Equal(t, 2, byKind[EdgeKindDocMentionsPath])

	assertEdgeEvidence(t, edges, EdgeKindTestCoversImplementation, true, ConfidenceMedium)
	assertEdgeEvidence(t, edges, EdgeKindDocMentionsPath, true, ConfidenceMedium)
	assertEdgeEvidence(t, edges, EdgeKindFileDeclaresPackage, false, ConfidenceHigh)
}

func TestCheapExtractor_WeakEvidenceDoesNotCreateFalsePreciseEdges(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)

	files := []SourceFile{
		{
			Path:     "internal/graph/orphan_test.go",
			Language: "go",
			Content:  []byte("package graph\n\nfunc TestOrphan(t *testing.T) {}\n"),
		},
		{
			Path:        "docs/notes.md",
			Language:    "markdown",
			ContentType: SourceContentTypeMarkdown,
			Content:     []byte("This vaguely mentions sample.go and a non-existent internal/graph/missing.go path."),
		},
	}

	require.NoError(t, IndexCheapEdges(ctx, repo, "project-1", files, CheapExtractorOptions{
		Now:        fixedGraphTime,
		StaleAfter: 24 * time.Hour,
	}))

	edges, err := repo.ListEdges(ctx, EdgeQuery{ProjectID: "project-1"})
	require.NoError(t, err)
	for _, edge := range edges {
		assert.NotEqual(t, EdgeKindTestCoversImplementation, edge.Kind)
		assert.NotEqual(t, EdgeKindDocMentionsPath, edge.Kind)
	}
}

func TestCheapExtractor_DocMentionsRequirePathContext(t *testing.T) {
	pathSet := map[string]SourceFile{
		"cmd/run.go": {Path: "cmd/run.go"},
	}

	for _, content := range []string{
		"the namespace cmd is reserved",
		"temporary backup cmd/run.go.bak should not count",
		"`cmd/run.go.bak` is not the source file",
		"https://example.test/cmd/run.go is an external URL",
	} {
		t.Run(content, func(t *testing.T) {
			assert.Empty(t, mentionedKnownPaths(content, pathSet, "docs/x.md"))
		})
	}

	assert.Equal(t, []knownPathMention{{Path: "cmd/run.go", Line: 1}}, mentionedKnownPaths("see `cmd/run.go` for the entry point", pathSet, "docs/x.md"))
	assert.Equal(t, []knownPathMention{{Path: "cmd/run.go", Line: 2}}, mentionedKnownPaths("see:\ncmd/run.go\n", pathSet, "docs/x.md"))
	assert.Equal(t, []knownPathMention{{Path: "cmd/run.go", Line: 1}}, mentionedKnownPaths("[entry](cmd/run.go \"source\")", pathSet, "docs/x.md"))
}

func TestCheapExtractor_RebuildIsStableAndRemovesStaleSourceEdges(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	files := stableFixtureFiles()

	require.NoError(t, IndexCheapEdges(ctx, repo, "project-1", files, CheapExtractorOptions{
		Now:        fixedGraphTime,
		StaleAfter: 24 * time.Hour,
	}))
	first := sortedEdgeKeys(t, ctx, repo)

	require.NoError(t, IndexCheapEdges(ctx, repo, "project-1", files, CheapExtractorOptions{
		Now:        fixedGraphTime,
		StaleAfter: 24 * time.Hour,
	}))
	second := sortedEdgeKeys(t, ctx, repo)

	assert.Equal(t, first, second)

	require.NoError(t, repo.ReplaceEdges(ctx, EdgeReplacement{
		ProjectID:  "project-1",
		Extractor:  ExtractorCheap,
		SourcePath: "internal/graph/stable.go",
		Edges:      nil,
		Run: ExtractorRun{
			Status:      ExtractorStatusSuccess,
			CompletedAt: fixedGraphTime(),
		},
	}))
	afterDelete := sortedEdgeKeys(t, ctx, repo)
	for _, key := range afterDelete {
		assert.NotContains(t, key, "project-1|cheap|internal/graph/stable.go|")
	}
	assert.Less(t, len(afterDelete), len(first))
}

func TestCheapExtractor_FailureMetadataFlowsToGraphStatus(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)

	files := []SourceFile{{
		Path:        ".amanmcp.yaml",
		Language:    "yaml",
		ContentType: SourceContentTypeConfig,
		Content:     []byte("embedder: [unterminated"),
	}}

	require.NoError(t, IndexCheapEdges(ctx, repo, "project-1", files, CheapExtractorOptions{
		Now:        fixedGraphTime,
		StaleAfter: 24 * time.Hour,
	}))

	snapshot, err := repo.Snapshot(ctx, StatusOptions{
		ProjectID:  "project-1",
		Now:        fixedGraphTime(),
		StaleAfter: 24 * time.Hour,
	})
	require.NoError(t, err)
	assert.Equal(t, GraphStatusPartial, snapshot.Status)
	require.Len(t, snapshot.Extractors, 1)
	assert.Equal(t, ExtractorStatusFailed, snapshot.Extractors[0].Status)
	require.NotEmpty(t, snapshot.Warnings)
	assert.Equal(t, WarningExtractorFailed, snapshot.Warnings[0].Code)
}

func stableFixtureFiles() []SourceFile {
	return []SourceFile{{
		Path:     "internal/graph/stable.go",
		Language: "go",
		Content: []byte(`package graph

import "context"

func Stable() {}
`),
		Chunks: []SourceChunk{{
			ID:        "chunk-stable",
			FilePath:  "internal/graph/stable.go",
			Language:  "go",
			StartLine: 5,
			EndLine:   5,
			Symbols: []SourceSymbol{{
				Name:      "Stable",
				Kind:      "function",
				StartLine: 5,
				EndLine:   5,
				Signature: "func Stable()",
			}},
		}},
	}, {
		Path:     "internal/graph/stable_test.go",
		Language: "go",
		Content:  []byte("package graph\n\nfunc TestStable(t *testing.T) {}\n"),
	}, {
		Path:        filepath.ToSlash("docs/stable.md"),
		Language:    "markdown",
		ContentType: SourceContentTypeMarkdown,
		Content:     []byte("See `internal/graph/stable.go`."),
	}}
}

func sortedEdgeKeys(t *testing.T, ctx context.Context, repo Repository) []string {
	t.Helper()
	edges, err := repo.ListEdges(ctx, EdgeQuery{ProjectID: "project-1"})
	require.NoError(t, err)
	keys := edgeNaturalKeys(edges)
	sort.Strings(keys)
	return keys
}

func assertEdgeEvidence(t *testing.T, edges []Edge, kind EdgeKind, heuristic bool, label ConfidenceLabel) {
	t.Helper()
	for _, edge := range edges {
		if edge.Kind == kind {
			assert.Equal(t, heuristic, edge.Evidence.Heuristic)
			assert.Equal(t, label, edge.ConfidenceLabel)
			return
		}
	}
	require.Failf(t, "edge not found", "kind %s not found", kind)
}
