package graph

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateQueryRequest_WrapsTypedSentinel proves every input-validation
// rejection wraps the exported graph.ErrInvalidQueryParams sentinel so eval
// classification can use errors.Is instead of brittle message-substring matching
// (DEBT-037 finding #3). A valid request returns nil, and the human-readable
// message is preserved so operator-facing logs stay informative.
func TestValidateQueryRequest_WrapsTypedSentinel(t *testing.T) {
	cases := []struct {
		name string
		req  QueryRequest
	}{
		{"missing project_id", QueryRequest{Mode: QueryModeFindReferences, Query: "internal/x.go"}},
		{"unsupported mode", QueryRequest{ProjectID: "p", Mode: "bogus_mode", Query: "internal/x.go"}},
		{"missing query", QueryRequest{ProjectID: "p", Mode: QueryModeFindReferences, Query: ""}},
		{"nul byte", QueryRequest{ProjectID: "p", Mode: QueryModeFindReferences, Query: "internal/\x00.go"}},
		{"absolute path", QueryRequest{ProjectID: "p", Mode: QueryModeFindReferences, Query: "/etc/passwd"}},
		{"path traversal", QueryRequest{ProjectID: "p", Mode: QueryModeFindReferences, Query: "../secrets.go"}},
		{"limit over cap", QueryRequest{ProjectID: "p", Mode: QueryModeFindReferences, Query: "internal/x.go", Limit: maxGraphQueryLimit + 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateQueryRequest(tc.req)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrInvalidQueryParams),
				"validation error %q must wrap ErrInvalidQueryParams", err)
			assert.NotEmpty(t, err.Error(), "human-readable message must be preserved")
		})
	}

	valid := QueryRequest{ProjectID: "p", Mode: QueryModeFindReferences, Query: "internal/x.go"}
	require.NoError(t, validateQueryRequest(valid))
}

// TestErrInvalidQueryParams_NotMatchedByInfraErrors guards the boundary: a
// non-validation (transport/infra) error must NOT be classified as invalid
// params, so the eval taxonomy keeps the two failure modes distinct.
func TestErrInvalidQueryParams_NotMatchedByInfraErrors(t *testing.T) {
	infra := fmt.Errorf("list graph edges: %w", errors.New("disk i/o error"))
	assert.False(t, errors.Is(infra, ErrInvalidQueryParams))
}

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

func TestQueryServable_OnlyFreshStalePartialServeRealAnswers(t *testing.T) {
	// QueryServable is the servability SSOT shared by the query service
	// short-circuit and direct graph eval measurement accounting. `failed` keeps
	// available=true (QueryAvailable) but serves no real answer, so it must not
	// be servable.
	servable := []GraphStatus{GraphStatusFresh, GraphStatusStale, GraphStatusPartial}
	notServable := []GraphStatus{GraphStatusUnavailable, GraphStatusIncompatible, GraphStatusEmpty, GraphStatusFailed}

	for _, status := range servable {
		assert.True(t, QueryServable(status), "status %s should be servable", status)
	}
	for _, status := range notServable {
		assert.False(t, QueryServable(status), "status %s should not be servable", status)
	}
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
