package graph

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMergeCompetingEdges_PrefersPreciseOverHeuristic proves GRA24: when two
// extractors emit the same (from, to, kind) relationship, only the
// highest-confidence non-heuristic edge surfaces in graph.query results.
func TestMergeCompetingEdges_PrefersPreciseOverHeuristic(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-edge-merge-precise"

	file := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "internal/pkg/a.go")
	symbol := upsertTestNode(t, ctx, repo, projectID, NodeKindSymbol, "internal/pkg/a.go#Foo:10")
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID:  projectID,
		Kind:       EdgeKindFileDefinesSymbol,
		FromNodeID: file.ID,
		ToNodeID:   symbol.ID,
		Extractor:  ExtractorCheap,
		SourcePath: "internal/pkg/a.go",
		Confidence: 0.45,
		Evidence:   Evidence{Method: "heuristic_guess", Heuristic: true},
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID:  projectID,
		Kind:       EdgeKindFileDefinesSymbol,
		FromNodeID: file.ID,
		ToNodeID:   symbol.ID,
		Extractor:  ExtractorSCIPGo,
		SourcePath: "internal/pkg/a.go",
		Confidence: 1.0,
		Evidence:   Evidence{Method: "scip", Heuristic: false},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Mode: QueryModeFindReferences, Query: "internal/pkg/a.go",
	})
	require.NoError(t, err)
	require.Len(t, got.Results, 1)
	assert.Equal(t, "scip", got.Results[0].EvidenceMethod)
	assert.Equal(t, 1.0, got.Results[0].Confidence)
	assert.False(t, got.Results[0].Heuristic)
}

// TestMergeCompetingEdges_PreservesDifferentKinds proves edges that share the
// same endpoints but represent different relationships are never merged.
func TestMergeCompetingEdges_PreservesDifferentKinds(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-edge-merge-kinds"

	doc := upsertTestNode(t, ctx, repo, projectID, NodeKindDoc, "docs/design.md")
	fileA := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "internal/a.go")
	fileB := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "internal/b.go")
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: projectID, Kind: EdgeKindDocMentionsFile,
		FromNodeID: doc.ID, ToNodeID: fileA.ID, Extractor: ExtractorCheap,
		SourcePath: "docs/design.md", Confidence: 0.8,
		Evidence: Evidence{Method: "doc_link"},
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: projectID, Kind: EdgeKindDocMentionsPath,
		FromNodeID: doc.ID, ToNodeID: fileB.ID, Extractor: ExtractorCheap,
		SourcePath: "docs/design.md", Confidence: 0.75,
		Evidence: Evidence{Method: "doc_path"},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{ProjectID: projectID, Query: "docs/design.md"})
	require.NoError(t, err)
	require.Len(t, got.Results, 2, "different edge kinds must both survive merge")
	kinds := []EdgeKind{got.Results[0].Relation, got.Results[1].Relation}
	assert.Contains(t, kinds, EdgeKindDocMentionsFile)
	assert.Contains(t, kinds, EdgeKindDocMentionsPath)
}

func TestPreferEdge_TieBreaksTowardNonHeuristic(t *testing.T) {
	heuristic := Edge{Confidence: 0.9, Evidence: Evidence{Heuristic: true}, ID: "a"}
	precise := Edge{Confidence: 0.9, Evidence: Evidence{Heuristic: false}, ID: "b"}
	assert.True(t, preferEdge(precise, heuristic))
	assert.False(t, preferEdge(heuristic, precise))
}