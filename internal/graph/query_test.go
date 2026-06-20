package graph

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryService_ReturnsBoundedSourceCitedEvidence(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-graph-query"

	file := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "internal/search/engine.go")
	symbol := upsertTestNode(t, ctx, repo, projectID, NodeKindSymbol, "internal/search/engine.go#Search:42")
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID:       projectID,
		Kind:            EdgeKindFileDefinesSymbol,
		FromNodeID:      file.ID,
		ToNodeID:        symbol.ID,
		Extractor:       ExtractorCheap,
		SourcePath:      "internal/search/engine.go",
		Confidence:      0.95,
		ConfidenceLabel: ConfidenceHigh,
		Evidence:        Evidence{Method: "chunk_symbol", Snippet: "func Search(ctx context.Context)"},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID:   projectID,
		Status:      GraphStatusFresh,
		CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{
		ProjectID: projectID,
		Mode:      QueryModeFindReferences,
		Query:     "engine.go",
		Limit:     1,
	})

	require.NoError(t, err)
	assert.Equal(t, GraphStatusFresh, got.Status)
	assert.False(t, got.Degraded)
	require.Len(t, got.Results, 1)
	assert.Equal(t, "internal/search/engine.go", got.Results[0].SourcePath)
	assert.Equal(t, EdgeKindFileDefinesSymbol, got.Results[0].Relation)
	assert.Equal(t, ConfidenceHigh, got.Results[0].ConfidenceLabel)
	assert.Equal(t, "chunk_symbol", got.Results[0].EvidenceMethod)
	assert.NotEmpty(t, got.Results[0].GraphPath)
}

func TestQueryService_DegradesForStaleGraphButReturnsEvidence(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-stale"
	file := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "a.go")
	symbol := upsertTestNode(t, ctx, repo, projectID, NodeKindSymbol, "a.go#A:1")
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID:  projectID,
		Kind:       EdgeKindFileDefinesSymbol,
		FromNodeID: file.ID,
		ToNodeID:   symbol.ID,
		Extractor:  ExtractorCheap,
		SourcePath: "a.go",
		Confidence: 0.9,
		Evidence:   Evidence{Method: "test"},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID:   projectID,
		Status:      GraphStatusFresh,
		CompletedAt: fixedGraphTime().Add(-48 * time.Hour),
	}))

	service := NewQueryService(repo, QueryServiceOptions{
		Now:        fixedGraphTime,
		StaleAfter: time.Hour,
	})
	got, err := service.Query(ctx, QueryRequest{ProjectID: projectID, Query: "a.go"})

	require.NoError(t, err)
	assert.Equal(t, GraphStatusStale, got.Status)
	assert.True(t, got.Degraded)
	assert.NotEmpty(t, got.Warnings)
	assert.NotEmpty(t, got.Results)
}

func TestQueryService_ExcludesStaleEdgesByDefaultAndIncludesOnOptIn(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-stale-edge-query"
	doc := upsertTestNode(t, ctx, repo, projectID, NodeKindDoc, "docs/design.md")
	file := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "internal/impl.go")
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
		ProjectID:       projectID,
		Kind:            EdgeKindDocMentionsFile,
		FromNodeID:      doc.ID,
		ToNodeID:        file.ID,
		Extractor:       ExtractorCheap,
		SourcePath:      "docs/design.md",
		Confidence:      0.9,
		ConfidenceLabel: ConfidenceHigh,
		Stale:           true,
		Evidence:        Evidence{Method: "test", SourcePath: "docs/design.md", LineStart: 1, LineEnd: 1},
	}))
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID:   projectID,
		Status:      GraphStatusFresh,
		CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	defaultResult, err := service.Query(ctx, QueryRequest{ProjectID: projectID, Query: "impl.go"})
	require.NoError(t, err)
	assert.Empty(t, defaultResult.Results)

	withStale, err := service.Query(ctx, QueryRequest{ProjectID: projectID, Query: "impl.go", IncludeStale: true})
	require.NoError(t, err)
	require.Len(t, withStale.Results, 1)
	assert.True(t, withStale.Results[0].Stale)
}

func TestQueryService_UnusableGraphReturnsVisibleWarning(t *testing.T) {
	service := NewQueryService(newTestSQLiteRepository(t), QueryServiceOptions{Now: fixedGraphTime})

	got, err := service.Query(context.Background(), QueryRequest{
		ProjectID: "empty-project",
		Query:     "anything",
	})

	require.NoError(t, err)
	assert.Equal(t, GraphStatusEmpty, got.Status)
	assert.True(t, got.Degraded)
	assert.Empty(t, got.Results)
	assert.NotEmpty(t, got.Warnings)
}

func TestQueryService_ValidatesInputs(t *testing.T) {
	service := NewQueryService(newTestSQLiteRepository(t), QueryServiceOptions{})

	tests := []QueryRequest{
		{ProjectID: "", Query: "x"},
		{ProjectID: "p", Query: ""},
		{ProjectID: "p", Query: "../x"},
		{ProjectID: "p", Query: "x", Mode: "unknown"},
		{ProjectID: "p", Query: "x", Limit: -1},
		{ProjectID: "p", Query: "x", Limit: 51},
	}
	for _, tt := range tests {
		_, err := service.Query(context.Background(), tt)
		require.Error(t, err)
	}
}

func TestQueryService_TruncatesResultsWithWarning(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	projectID := "project-truncate"
	file := upsertTestNode(t, ctx, repo, projectID, NodeKindFile, "a.go")
	for _, key := range []string{"a.go#A:1", "a.go#B:2"} {
		symbol := upsertTestNode(t, ctx, repo, projectID, NodeKindSymbol, key)
		require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, Edge{
			ProjectID:  projectID,
			Kind:       EdgeKindFileDefinesSymbol,
			FromNodeID: file.ID,
			ToNodeID:   symbol.ID,
			Extractor:  ExtractorCheap,
			SourcePath: "a.go",
			Confidence: 0.9,
			Evidence:   Evidence{Method: "test"},
		}))
	}
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID:   projectID,
		Status:      GraphStatusFresh,
		CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})
	got, err := service.Query(ctx, QueryRequest{ProjectID: projectID, Query: "a.go", Limit: 1})

	require.NoError(t, err)
	assert.Len(t, got.Results, 1)
	assert.NotEmpty(t, got.Warnings)
	assert.Equal(t, WarningCode("graph_results_truncated"), got.Warnings[0].Code)
}
