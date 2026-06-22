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
	assert.Equal(t, 2, byKind[EdgeKindFileImports])
	assert.Equal(t, 1, byKind[EdgeKindFileDefinesSymbol])
	assert.Equal(t, 1, byKind[EdgeKindSymbolHasChunk])
	assert.GreaterOrEqual(t, byKind[EdgeKindFileDefinesConfigKey], 3)
	assert.Equal(t, 1, byKind[EdgeKindTestCoversImplementation])
	assert.Equal(t, 2, byKind[EdgeKindDocMentionsFile])

	assertEdgeEvidence(t, edges, EdgeKindTestCoversImplementation, true, ConfidenceMedium)
	assertEdgeEvidence(t, edges, EdgeKindDocMentionsFile, false, ConfidenceMedium)
	assertEdgeEvidence(t, edges, EdgeKindFileDeclaresPackage, false, ConfidenceExact)
}

// TestCheapExtractor_DedupesDriftedDuplicateSymbolNodes is the DEBT-041 contract:
// when a single file's extraction receives the same declaration twice at
// overlapping but line-drifted ranges (the shape produced when a stale chunk and
// its re-indexed successor both survive in the store and are rebuilt together),
// the graph must emit exactly ONE symbol node for that declaration — while still
// keeping genuinely distinct same-name declarations whose ranges are disjoint
// (e.g. two different Close methods in one file).
func TestCheapExtractor_DedupesDriftedDuplicateSymbolNodes(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)

	files := []SourceFile{{
		Path:     "internal/graph/dup.go",
		Language: "go",
		Content:  []byte("package graph\n"),
		Chunks: []SourceChunk{
			// Stale chunk: relatedResults at the pre-edit lines.
			{
				ID: "chunk-rr-stale", FilePath: "internal/graph/dup.go", Language: "go",
				StartLine: 241, EndLine: 281,
				Symbols: []SourceSymbol{{Name: "relatedResults", Kind: "function", StartLine: 241, EndLine: 281, Signature: "func relatedResults()"}},
			},
			// Current chunk: same declaration, drifted +2 lines (heavily overlaps the stale one).
			{
				ID: "chunk-rr-current", FilePath: "internal/graph/dup.go", Language: "go",
				StartLine: 243, EndLine: 284,
				Symbols: []SourceSymbol{{Name: "relatedResults", Kind: "function", StartLine: 243, EndLine: 284, Signature: "func relatedResults()"}},
			},
			// Two genuinely DISTINCT Close methods (disjoint ranges) — both must survive.
			{
				ID: "chunk-close-a", FilePath: "internal/graph/dup.go", Language: "go",
				StartLine: 50, EndLine: 55,
				Symbols: []SourceSymbol{{Name: "Close", Kind: "method", StartLine: 50, EndLine: 55, Signature: "func (a *A) Close()"}},
			},
			{
				ID: "chunk-close-b", FilePath: "internal/graph/dup.go", Language: "go",
				StartLine: 120, EndLine: 125,
				Symbols: []SourceSymbol{{Name: "Close", Kind: "method", StartLine: 120, EndLine: 125, Signature: "func (b *B) Close()"}},
			},
		},
	}}

	require.NoError(t, IndexCheapEdges(ctx, repo, "project-dedup", files, CheapExtractorOptions{Now: fixedGraphTime}))

	nodes, err := repo.ListNodes(ctx, NodeQuery{ProjectID: "project-dedup", Kind: NodeKindSymbol})
	require.NoError(t, err)
	byName := make(map[string][]Node)
	for _, n := range nodes {
		byName[n.Name] = append(byName[n.Name], n)
	}

	require.Len(t, byName["relatedResults"], 1,
		"a single declaration drifted across two overlapping chunks must collapse to one symbol node")
	assert.Equal(t, 241, byName["relatedResults"][0].StartLine,
		"dedup keeps the declaration line (lowest start) of the overlapping group")
	assert.Len(t, byName["Close"], 2,
		"two distinct same-name declarations at disjoint ranges must both survive")

	// The surviving symbol must still be wired: exactly one file_defines_symbol edge
	// per kept symbol (no edges to the dropped stale duplicate).
	edges, err := repo.ListEdges(ctx, EdgeQuery{ProjectID: "project-dedup", Kind: EdgeKindFileDefinesSymbol})
	require.NoError(t, err)
	assert.Len(t, edges, 3, "one file_defines_symbol edge per surviving symbol (relatedResults + 2x Close)")
}

func TestConfidenceLabelFor_UsesStableADRBands(t *testing.T) {
	tests := []struct {
		name       string
		confidence float64
		want       ConfidenceLabel
	}{
		{name: "exact", confidence: 1.0, want: ConfidenceExact},
		{name: "near exact is high", confidence: 0.999, want: ConfidenceHigh},
		{name: "high boundary", confidence: 0.9, want: ConfidenceHigh},
		{name: "medium below high", confidence: 0.89, want: ConfidenceMedium},
		{name: "medium boundary", confidence: 0.7, want: ConfidenceMedium},
		{name: "low below medium", confidence: 0.69, want: ConfidenceLow},
		{name: "zero is low", confidence: 0, want: ConfidenceLow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := confidenceLabelFor(tt.confidence)
			assert.Equal(t, tt.want, got)
			assert.NotEqual(t, ConfidenceLabel("heuristic"), got, "heuristic is an orthogonal evidence flag, not a label")
		})
	}
}

func TestCheapExtractor_ExtractsADR041CoreEdgeSet(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)

	files := []SourceFile{
		{
			Path:     "internal/graph/sample.go",
			Language: "go",
			Content: []byte(`package graph

import (
	"context"
	"fmt"
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
			Path:     "web/component.tsx",
			Language: "typescript",
			Content:  []byte("import React from 'react';\nimport { helper } from './helper';\nexport function Component() { return helper(); }\n"),
		},
		{
			Path:     "scripts/tool.js",
			Language: "javascript",
			Content:  []byte("const fs = require('fs');\nexport { run } from './run.js';\n"),
		},
		{
			Path:     "pkg/service.py",
			Language: "python",
			Content:  []byte("import os\nfrom pkg.helpers import value\n\ndef run():\n    return value\n"),
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
			Path:        "docs/graph.md",
			Language:    "markdown",
			ContentType: SourceContentTypeMarkdown,
			Content:     []byte("See [`sample`](../internal/graph/sample.go), `Build`, and `embedder.provider`.\n"),
		},
	}

	require.NoError(t, IndexCheapEdges(ctx, repo, "project-1", files, CheapExtractorOptions{
		Now:        fixedGraphTime,
		StaleAfter: 24 * time.Hour,
	}))

	nodes, err := repo.ListNodes(ctx, NodeQuery{ProjectID: "project-1"})
	require.NoError(t, err)
	primaryKindsByPath := map[string][]NodeKind{}
	for _, node := range nodes {
		switch node.Kind {
		case NodeKindFile, NodeKindTestFile, NodeKindDoc, NodeKindConfigFile:
			primaryKindsByPath[node.SourcePath] = append(primaryKindsByPath[node.SourcePath], node.Kind)
		}
	}
	assert.Equal(t, []NodeKind{NodeKindFile}, primaryKindsByPath["internal/graph/sample.go"])
	assert.Equal(t, []NodeKind{NodeKindTestFile}, primaryKindsByPath["internal/graph/sample_test.go"])
	assert.Equal(t, []NodeKind{NodeKindFile}, primaryKindsByPath["web/component.tsx"])
	assert.Equal(t, []NodeKind{NodeKindFile}, primaryKindsByPath["scripts/tool.js"])
	assert.Equal(t, []NodeKind{NodeKindFile}, primaryKindsByPath["pkg/service.py"])
	assert.Equal(t, []NodeKind{NodeKindConfigFile}, primaryKindsByPath[".amanmcp.yaml"])
	assert.Equal(t, []NodeKind{NodeKindDoc}, primaryKindsByPath["docs/graph.md"])

	edges, err := repo.ListEdges(ctx, EdgeQuery{ProjectID: "project-1"})
	require.NoError(t, err)
	byKind := edgeCountByKind(edges)

	assert.Equal(t, len(files), byKind[EdgeKindProjectContainsFile])
	assert.Equal(t, 2, byKind[EdgeKindFileDeclaresPackage])
	assert.GreaterOrEqual(t, byKind[EdgeKindFileImports], 8)
	assert.Equal(t, 1, byKind[EdgeKindFileDefinesSymbol])
	assert.Equal(t, 1, byKind[EdgeKindSymbolHasChunk])
	assert.GreaterOrEqual(t, byKind[EdgeKindFileDefinesConfigKey], 3)
	assert.Equal(t, 1, byKind[EdgeKindTestCoversImplementation])
	assert.Equal(t, 1, byKind[EdgeKindDocMentionsFile])
	assert.Equal(t, 1, byKind[EdgeKindDocMentionsSymbol])
	assert.Equal(t, 1, byKind[EdgeKindDocMentionsConfigKey])
	assert.Zero(t, byKind[EdgeKindPackageImports], "package_imports must not be the Sprint 19 import acceptance signal")
	assert.Zero(t, byKind[EdgeKindDocMentionsPath], "doc_mentions_path must not be the Sprint 19 doc acceptance signal")

	assertEdgeEvidence(t, edges, EdgeKindProjectContainsFile, false, ConfidenceExact)
	assertEdgeEvidence(t, edges, EdgeKindFileImports, false, ConfidenceHigh)
	assertEdgeEvidence(t, edges, EdgeKindFileDefinesSymbol, false, ConfidenceHigh)
	assertEdgeEvidence(t, edges, EdgeKindSymbolHasChunk, false, ConfidenceExact)
	assertEdgeEvidence(t, edges, EdgeKindDocMentionsFile, false, ConfidenceHigh)
	assertEdgeEvidence(t, edges, EdgeKindDocMentionsSymbol, true, ConfidenceMedium)
	assertEdgeEvidence(t, edges, EdgeKindDocMentionsConfigKey, true, ConfidenceMedium)
	assertEveryEdgeHasSourceRange(t, edges)
}

func TestCheapExtractor_TestImplementationEdgesAreLanguageAwareAndConservative(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	files := []SourceFile{
		{Path: "pkg/math.go", Language: "go", Content: []byte("package pkg\n")},
		{Path: "pkg/math_test.go", Language: "go", Content: []byte("package pkg\n")},
		{Path: "src/user.py", Language: "python", Content: []byte("def load_user():\n    return None\n")},
		{Path: "tests/test_user.py", Language: "python", Content: []byte("from src.user import load_user\n")},
		{Path: "web/Button.tsx", Language: "typescript", Content: []byte("export function Button() { return null }\n")},
		{Path: "web/Button.test.tsx", Language: "typescript", Content: []byte("import { Button } from './Button'\n")},
		{Path: "ui/Card.jsx", Language: "javascript", Content: []byte("export function Card() { return null }\n")},
		{Path: "ui/Card.test.jsx", Language: "javascript", Content: []byte("import { Card } from './Card'\n")},
		{Path: "lib/format.js", Language: "javascript", Content: []byte("exports.format = () => ''\n")},
		{Path: "lib/__tests__/format.spec.js", Language: "javascript", Content: []byte("const { format } = require('../format')\n")},
		{Path: "lib/__tests__/orphan.spec.js", Language: "javascript", Content: []byte("test('nothing', () => {})\n")},
	}

	require.NoError(t, IndexCheapEdges(ctx, repo, "project-1", files, CheapExtractorOptions{Now: fixedGraphTime}))

	edges, err := repo.ListEdges(ctx, EdgeQuery{ProjectID: "project-1", Kind: EdgeKindTestCoversImplementation})
	require.NoError(t, err)
	require.Len(t, edges, 5)
	byConfidence := map[ConfidenceLabel]int{}
	for _, edge := range edges {
		byConfidence[edge.ConfidenceLabel]++
		assert.True(t, edge.Evidence.Heuristic)
		assert.NotEmpty(t, edge.Evidence.Method)
		assert.NotContains(t, edge.SourcePath, "orphan")
	}
	assert.Equal(t, 4, byConfidence[ConfidenceMedium])
	assert.Equal(t, 1, byConfidence[ConfidenceLow])

	nodes, err := repo.ListNodes(ctx, NodeQuery{ProjectID: "project-1"})
	require.NoError(t, err)
	byID := nodesByID(nodes)
	for _, edge := range edges {
		assert.Equal(t, NodeKindTestFile, byID[edge.FromNodeID].Kind)
		assert.Equal(t, NodeKindFile, byID[edge.ToNodeID].Kind)
	}
}

func TestCheapExtractor_TestImplementationEdgesAreCappedAndDeterministic(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	files := []SourceFile{
		{Path: "tests/suite.spec.js", Language: "javascript", Content: []byte("require('../src/a')\nrequire('../src/b')\nrequire('../src/c')\nrequire('../src/d')\nrequire('../src/e')\nrequire('../src/f')\n")},
	}
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		files = append(files, SourceFile{
			Path:     "src/" + name + ".js",
			Language: "javascript",
			Content:  []byte("exports." + name + " = () => ''\n"),
		})
	}

	require.NoError(t, IndexCheapEdges(ctx, repo, "project-1", files, CheapExtractorOptions{Now: fixedGraphTime}))

	edges, err := repo.ListEdges(ctx, EdgeQuery{ProjectID: "project-1", Kind: EdgeKindTestCoversImplementation})
	require.NoError(t, err)
	require.Len(t, edges, maxTestImplementationTargets)
	got := make([]string, 0, len(edges))
	for _, edge := range edges {
		got = append(got, edge.ToNodeID)
		assert.Equal(t, ConfidenceLow, edge.ConfidenceLabel)
	}
	assert.Equal(t, append([]string(nil), got...), sortedStrings(got), "test edge targets must be emitted in deterministic order")
}

func TestCheapExtractor_TestImplementationImportDerivedEdgesUseLowConfidence(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	files := []SourceFile{
		{Path: "lib/format.js", Language: "javascript", Content: []byte("exports.format = () => ''\n")},
		{Path: "lib/__tests__/formatter.spec.js", Language: "javascript", Content: []byte("const { format } = require('../format')\n")},
	}

	require.NoError(t, IndexCheapEdges(ctx, repo, "project-1", files, CheapExtractorOptions{Now: fixedGraphTime}))

	edges, err := repo.ListEdges(ctx, EdgeQuery{ProjectID: "project-1", Kind: EdgeKindTestCoversImplementation})
	require.NoError(t, err)
	require.Len(t, edges, 1)
	assert.Equal(t, ConfidenceLow, edges[0].ConfidenceLabel)
	assert.Equal(t, "test_import_reference", edges[0].Evidence.Method)
	assert.True(t, edges[0].Evidence.Heuristic)
}

func TestCheapExtractor_DocAndConfigEdgesUseCanonicalTargets(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	files := []SourceFile{
		{
			Path:     "cmd/root.go",
			Language: "go",
			Content:  []byte("package cmd\nfunc Execute() {}\n"),
			Chunks: []SourceChunk{{
				ID:        "chunk-execute",
				FilePath:  "cmd/root.go",
				Language:  "go",
				StartLine: 2,
				EndLine:   2,
				Symbols: []SourceSymbol{{
					Name:      "Execute",
					Kind:      "function",
					StartLine: 2,
					EndLine:   2,
					Signature: "func Execute()",
				}},
			}},
		},
		{Path: ".env", Language: "dotenv", ContentType: SourceContentTypeConfig, Content: []byte("AMAN_PROVIDER=ollama\nNESTED_VALUE='safe'\n")},
		{Path: "app.properties", Language: "properties", ContentType: SourceContentTypeConfig, Content: []byte("search.limit=10\nsearch.mode=hybrid\n")},
		{Path: "settings.json", Language: "json", ContentType: SourceContentTypeConfig, Content: []byte(`{"server":{"host":"localhost"}}`)},
		{Path: "pyproject.toml", Language: "toml", ContentType: SourceContentTypeConfig, Content: []byte("[tool.aman]\nmode = \"static\"\n")},
		{
			Path:        "docs/root.md",
			Language:    "markdown",
			ContentType: SourceContentTypeMarkdown,
			Content:     []byte("Use [root](../cmd/root.go), call `Execute`, configure `AMAN_PROVIDER`, and ignore `missing.symbol`.\n"),
		},
	}

	require.NoError(t, IndexCheapEdges(ctx, repo, "project-1", files, CheapExtractorOptions{Now: fixedGraphTime}))

	edges, err := repo.ListEdges(ctx, EdgeQuery{ProjectID: "project-1"})
	require.NoError(t, err)
	byKind := edgeCountByKind(edges)
	assert.Equal(t, 1, byKind[EdgeKindDocMentionsFile])
	assert.Equal(t, 1, byKind[EdgeKindDocMentionsSymbol])
	assert.Equal(t, 1, byKind[EdgeKindDocMentionsConfigKey])
	assert.GreaterOrEqual(t, byKind[EdgeKindFileDefinesConfigKey], 8)
	assert.Zero(t, byKind[EdgeKindDocMentionsPath])
}

func TestCheapExtractor_MalformedConfigRecordsExtractorFailureAndKeepsOtherResults(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	files := []SourceFile{
		{Path: "good.yaml", Language: "yaml", ContentType: SourceContentTypeConfig, Content: []byte("server:\n  port: 8080\n")},
		{Path: "bad.yaml", Language: "yaml", ContentType: SourceContentTypeConfig, Content: []byte(":\n")},
	}

	summary, err := UpdateCheapEdgesWithSummary(ctx, repo, "project-1", files, CheapExtractorOptions{Now: fixedGraphTime})
	require.NoError(t, err)
	assert.True(t, summary.HadErrors)

	goodEdges, err := repo.ListEdges(ctx, EdgeQuery{ProjectID: "project-1", SourcePath: "good.yaml", Kind: EdgeKindFileDefinesConfigKey})
	require.NoError(t, err)
	require.NotEmpty(t, goodEdges)

	snapshot, err := repo.Snapshot(ctx, StatusOptions{ProjectID: "project-1", Now: fixedGraphTime()})
	require.NoError(t, err)
	var badRun *ExtractorSummary
	for i := range snapshot.Extractors {
		if snapshot.Extractors[i].SourcePath == "bad.yaml" {
			badRun = &snapshot.Extractors[i]
			break
		}
	}
	require.NotNil(t, badRun)
	assert.Equal(t, ExtractorStatusFailed, badRun.Status)
	assert.Equal(t, 1, badRun.ErrorCount)
	assert.Contains(t, badRun.Message, "parse config bad.yaml")
}

func TestCheapExtractor_DocMentionsSkipAmbiguousSymbolNames(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	files := []SourceFile{
		{
			Path:     "internal/a/close.go",
			Language: "go",
			Content:  []byte("package a\nfunc Close() {}\n"),
			Chunks: []SourceChunk{{
				ID:        "chunk-close-a",
				FilePath:  "internal/a/close.go",
				Language:  "go",
				StartLine: 2,
				EndLine:   2,
				Symbols: []SourceSymbol{{
					Name:      "Close",
					Kind:      "function",
					StartLine: 2,
					EndLine:   2,
					Signature: "func Close()",
				}},
			}},
		},
		{
			Path:     "internal/b/close.go",
			Language: "go",
			Content:  []byte("package b\nfunc Close() {}\n"),
			Chunks: []SourceChunk{{
				ID:        "chunk-close-b",
				FilePath:  "internal/b/close.go",
				Language:  "go",
				StartLine: 2,
				EndLine:   2,
				Symbols: []SourceSymbol{{
					Name:      "Close",
					Kind:      "function",
					StartLine: 2,
					EndLine:   2,
					Signature: "func Close()",
				}},
			}},
		},
		{
			Path:        "docs/close.md",
			Language:    "markdown",
			ContentType: SourceContentTypeMarkdown,
			Content:     []byte("The `Close` hook is intentionally ambiguous here.\n"),
		},
	}

	require.NoError(t, IndexCheapEdges(ctx, repo, "project-1", files, CheapExtractorOptions{Now: fixedGraphTime}))

	edges, err := repo.ListEdges(ctx, EdgeQuery{ProjectID: "project-1", SourcePath: "docs/close.md", Kind: EdgeKindDocMentionsSymbol})
	require.NoError(t, err)
	assert.Empty(t, edges)
}

func TestCheapExtractor_GoPackageNodesAreScopedByDirectory(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	files := []SourceFile{
		{Path: "internal/util/a.go", Language: "go", Content: []byte("package util\n")},
		{Path: "pkg/util/b.go", Language: "go", Content: []byte("package util\n")},
	}

	require.NoError(t, IndexCheapEdges(ctx, repo, "project-1", files, CheapExtractorOptions{Now: fixedGraphTime}))

	nodes, err := repo.ListNodes(ctx, NodeQuery{ProjectID: "project-1", Kind: NodeKindPackage})
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	keys := []string{nodes[0].Key, nodes[1].Key}
	assert.ElementsMatch(t, []string{"internal/util#util", "pkg/util#util"}, keys)
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
		assert.NotEqual(t, EdgeKindDocMentionsFile, edge.Kind)
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

	assertKnownPathMentions(t, []knownPathMention{{Path: "cmd/run.go", Line: 1}}, mentionedKnownPaths("see `cmd/run.go` for the entry point", pathSet, "docs/x.md"))
	assertKnownPathMentions(t, []knownPathMention{{Path: "cmd/run.go", Line: 2}}, mentionedKnownPaths("see:\ncmd/run.go\n", pathSet, "docs/x.md"))
	assertKnownPathMentions(t, []knownPathMention{{Path: "cmd/run.go", Line: 1}}, mentionedKnownPaths("[entry](cmd/run.go \"source\")", pathSet, "docs/x.md"))
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

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func assertEdgeEvidence(t *testing.T, edges []Edge, kind EdgeKind, heuristic bool, label ConfidenceLabel) {
	t.Helper()
	for _, edge := range edges {
		if edge.Kind == kind {
			assert.Equal(t, heuristic, edge.Evidence.Heuristic)
			assert.Equal(t, label, edge.ConfidenceLabel)
			assert.NotEmpty(t, edge.Evidence.SourcePath)
			assert.NotZero(t, edge.Evidence.LineStart)
			assert.NotZero(t, edge.Evidence.LineEnd)
			return
		}
	}
	require.Failf(t, "edge not found", "kind %s not found", kind)
}

func edgeCountByKind(edges []Edge) map[EdgeKind]int {
	counts := make(map[EdgeKind]int)
	for _, edge := range edges {
		counts[edge.Kind]++
	}
	return counts
}

func assertEveryEdgeHasSourceRange(t *testing.T, edges []Edge) {
	t.Helper()
	for _, edge := range edges {
		assert.NotEmpty(t, edge.Evidence.SourcePath, "edge %s", edge.NaturalKey())
		assert.Greater(t, edge.Evidence.LineStart, 0, "edge %s", edge.NaturalKey())
		assert.GreaterOrEqual(t, edge.Evidence.LineEnd, edge.Evidence.LineStart, "edge %s", edge.NaturalKey())
	}
}

func assertKnownPathMentions(t *testing.T, want, got []knownPathMention) {
	t.Helper()
	require.Len(t, got, len(want))
	for i := range want {
		assert.Equal(t, want[i].Path, got[i].Path)
		assert.Equal(t, want[i].Line, got[i].Line)
	}
}
