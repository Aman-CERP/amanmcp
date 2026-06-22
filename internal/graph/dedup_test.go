package graph

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeduplicateResultsByTarget_CollapsesMultiplePaths proves GRA25: the same
// target reachable via multiple edge kinds appears once with suppressed metadata.
func TestDeduplicateResultsByTarget_CollapsesMultiplePaths(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-target-dedup"

	doc := upsertTestNode(t, ctx, repo, projectID, NodeKindDoc, "docs/design.md")
	file := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "internal/impl.go")
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: projectID, Kind: EdgeKindDocMentionsFile,
		FromNodeID: doc.ID, ToNodeID: file.ID, Extractor: ExtractorCheap,
		SourcePath: "docs/design.md", Confidence: 0.95,
		Evidence: Evidence{Method: "doc_link"},
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: projectID, Kind: EdgeKindDocMentionsPath,
		FromNodeID: doc.ID, ToNodeID: file.ID, Extractor: ExtractorCheap,
		SourcePath: "docs/design.md", Confidence: 0.7,
		Evidence: Evidence{Method: "doc_path"},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{ProjectID: projectID, Query: "docs/design.md"})
	require.NoError(t, err)
	require.Len(t, got.Results, 1, "same target must collapse to one result")
	assert.Equal(t, file.ID, got.Results[0].NodeID)
	assert.Equal(t, EdgeKindDocMentionsFile, got.Results[0].Relation, "highest-confidence path is kept")
	assert.Equal(t, 1, got.Results[0].AdditionalPathsCount)
	assert.ElementsMatch(t, []EdgeKind{EdgeKindDocMentionsPath}, got.Results[0].AdditionalRelations)
}

// TestDeduplicateResultsByTarget_SinglePathUnchanged proves single-path targets
// carry zero suppressed-path metadata.
func TestDeduplicateResultsByTarget_SinglePathUnchanged(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-target-dedup-single"

	file := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "a.go")
	symbol := upsertTestNode(t, ctx, repo, projectID, NodeKindSymbol, "a.go#A:1")
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID: projectID, Kind: EdgeKindFileDefinesSymbol,
		FromNodeID: file.ID, ToNodeID: symbol.ID, Extractor: ExtractorCheap,
		SourcePath: "a.go", Confidence: 0.9, Evidence: Evidence{Method: "test"},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{ProjectID: projectID, Query: "a.go"})
	require.NoError(t, err)
	require.Len(t, got.Results, 1)
	assert.Equal(t, 0, got.Results[0].AdditionalPathsCount)
	assert.Empty(t, got.Results[0].AdditionalRelations)
}

// TestDeduplicateBeforeTruncation proves dedup runs before the result limit so
// distinct targets occupy slots, not raw paths.
func TestDeduplicateBeforeTruncation(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-dedup-before-truncate"

	seed := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "seed.go")
	targetA := upsertTestNode(t, ctx, repo, projectID, NodeKindSymbol, "seed.go#A:1")
	targetB := upsertTestNode(t, ctx, repo, projectID, NodeKindSymbol, "seed.go#B:2")
	for _, edge := range []struct {
		kind EdgeKind
		to   Node
		conf float64
	}{
		{EdgeKindFileDefinesSymbol, targetA, 0.95},
		{EdgeKindSymbolHasChunk, targetA, 0.5},
		{EdgeKindFileDefinesSymbol, targetB, 0.9},
	} {
		require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
			ProjectID: projectID, Kind: edge.kind,
			FromNodeID: seed.ID, ToNodeID: edge.to.ID, Extractor: ExtractorCheap,
			SourcePath: "seed.go", Confidence: edge.conf, Evidence: Evidence{Method: "test"},
		}))
	}
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID: projectID, Status: GraphStatusFresh, CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID, Query: "seed.go", Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, got.Results, 1)
	assert.Equal(t, targetA.ID, got.Results[0].NodeID)
	assert.Equal(t, 1, got.Results[0].AdditionalPathsCount)
}