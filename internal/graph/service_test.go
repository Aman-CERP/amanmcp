package graph

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPathConfidenceLabel_IsBottleneckIncludingExact guards the GRA21 contract
// that a path's confidence label is the bottleneck (minimum) across its hops, so a
// single-hop path's label equals that one edge's label — INCLUDING exact. The
// prior implementation initialized the label to high and only lowered it, so an
// exact single edge collapsed to high, disagreeing with the flat result.confidence_label.
func TestPathConfidenceLabel_IsBottleneckIncludingExact(t *testing.T) {
	hop := func(label ConfidenceLabel) adjacency {
		return adjacency{edge: Edge{ConfidenceLabel: label}}
	}
	tests := []struct {
		name string
		path []adjacency
		want ConfidenceLabel
	}{
		{"single exact hop stays exact", []adjacency{hop(ConfidenceExact)}, ConfidenceExact},
		{"single high hop stays high", []adjacency{hop(ConfidenceHigh)}, ConfidenceHigh},
		{"single low hop stays low", []adjacency{hop(ConfidenceLow)}, ConfidenceLow},
		{"bottleneck of exact then high is high", []adjacency{hop(ConfidenceExact), hop(ConfidenceHigh)}, ConfidenceHigh},
		{"bottleneck of high then low is low", []adjacency{hop(ConfidenceHigh), hop(ConfidenceLow)}, ConfidenceLow},
		{"bottleneck of all exact is exact", []adjacency{hop(ConfidenceExact), hop(ConfidenceExact)}, ConfidenceExact},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pathConfidenceLabel(tt.path))
		})
	}
}

func TestQueryService_Neighbors_ReturnsSourceCitedEvidence(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	fixture := seedGraphServiceFixture(t, ctx, repo, GraphStatusFresh)

	service := NewQueryService(repo, QueryServiceOptions{
		Now:        func() time.Time { return fixedGraphTime().Add(time.Minute) },
		StaleAfter: 24 * time.Hour,
	})

	result, err := service.Neighbors(ctx, NeighborRequest{
		ProjectID: "project-1",
		Query:     "NewQueryService",
		Limit:     5,
		Depth:     1,
	})
	require.NoError(t, err)

	assert.Equal(t, GraphStatusFresh, result.Status)
	assert.False(t, result.Degraded)
	assert.False(t, result.Truncated)
	require.NotEmpty(t, result.Evidence)

	seed := findGraphEvidenceByRole(t, result.Evidence, RoleQueryMatch)
	assert.Equal(t, fixture.symbol.ID, seed.Node.ID)
	assert.Equal(t, "internal/graph/service.go", seed.Node.SourcePath)

	definedBy := findGraphEvidenceByRole(t, result.Evidence, RoleDefinedBy)
	assert.Equal(t, string(EdgeKindFileDefinesSymbol), definedBy.Relation)
	assert.Equal(t, ConfidenceHigh, definedBy.ConfidenceLabel)
	assert.Equal(t, "internal/graph/service.go", definedBy.EdgeSourcePath)
	assert.Equal(t, "go_symbol", definedBy.EdgeEvidence.Method)
	assert.Equal(t, 12, definedBy.EdgeEvidence.Line)
	assert.Contains(t, definedBy.EdgeEvidence.Snippet, "func NewQueryService")
	assert.Contains(t, definedBy.PathExplanation, "NewQueryService")
	assert.Contains(t, definedBy.PathExplanation, string(EdgeKindFileDefinesSymbol))
}

func TestQueryService_Path_ReturnsBoundedGraphPathExplanation(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	fixture := seedGraphServiceFixture(t, ctx, repo, GraphStatusFresh)

	service := NewQueryService(repo, QueryServiceOptions{
		Now:        func() time.Time { return fixedGraphTime().Add(time.Minute) },
		StaleAfter: 24 * time.Hour,
	})

	result, err := service.Path(ctx, PathRequest{
		ProjectID: "project-1",
		FromQuery: "internal/graph/service.go",
		ToQuery:   fixture.chunk.Key,
		Limit:     1,
		Depth:     2,
	})
	require.NoError(t, err)

	assert.Equal(t, GraphStatusFresh, result.Status)
	assert.False(t, result.Degraded)
	require.Len(t, result.Paths, 1)
	path := result.Paths[0]
	assert.Equal(t, fixture.file.ID, path.From.ID)
	assert.Equal(t, fixture.chunk.ID, path.To.ID)
	assert.Equal(t, ConfidenceHigh, path.ConfidenceLabel)
	assert.Contains(t, path.Explanation, string(EdgeKindFileDefinesSymbol))
	assert.Contains(t, path.Explanation, string(EdgeKindSymbolHasChunk))
	require.Len(t, path.Hops, 2)
	assert.Equal(t, RoleDefines, path.Hops[0].Role)
	assert.Equal(t, RoleHasChunk, path.Hops[1].Role)
}

func TestQueryService_RejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	service := NewQueryService(newTestSQLiteRepository(t), QueryServiceOptions{
		MaxLimit: 2,
		MaxDepth: 2,
	})

	tests := []struct {
		name string
		req  NeighborRequest
		want string
	}{
		{
			name: "project id required",
			req:  NeighborRequest{Query: "service", Limit: 1, Depth: 1},
			want: "project_id is required",
		},
		{
			name: "query required",
			req:  NeighborRequest{ProjectID: "project-1", Query: " \t ", Limit: 1, Depth: 1},
			want: "query is required",
		},
		{
			name: "limit positive",
			req:  NeighborRequest{ProjectID: "project-1", Query: "service", Limit: 0, Depth: 1},
			want: "limit must be positive",
		},
		{
			name: "depth positive",
			req:  NeighborRequest{ProjectID: "project-1", Query: "service", Limit: 1, Depth: 0},
			want: "depth must be positive",
		},
		{
			name: "limit bounded",
			req:  NeighborRequest{ProjectID: "project-1", Query: "service", Limit: 3, Depth: 1},
			want: "limit must be <= 2",
		},
		{
			name: "unsafe query",
			req:  NeighborRequest{ProjectID: "project-1", Query: "service\x00name", Limit: 1, Depth: 1},
			want: "query contains unsafe control character",
		},
		{
			name: "unsafe source path",
			req:  NeighborRequest{ProjectID: "project-1", Query: "service", SourcePath: "../outside.go", Limit: 1, Depth: 1},
			want: "source_path must be relative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.Neighbors(ctx, tt.req)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}

	_, err := service.Path(ctx, PathRequest{
		ProjectID: "project-1",
		FromQuery: "service",
		ToQuery:   "",
		Limit:     1,
		Depth:     1,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "to_query is required")
}

func TestQueryService_DegradesUnavailableIncompatibleAndEmptyGraph(t *testing.T) {
	ctx := context.Background()

	t.Run("unavailable", func(t *testing.T) {
		service := NewQueryService(nil, QueryServiceOptions{})
		result, err := service.Neighbors(ctx, NeighborRequest{
			ProjectID: "project-1",
			Query:     "service",
			Limit:     1,
			Depth:     1,
		})
		require.NoError(t, err)
		assert.Equal(t, GraphStatusUnavailable, result.Status)
		assert.False(t, result.Available)
		assert.True(t, result.Degraded)
		require.Len(t, result.Warnings, 1)
		assert.Equal(t, WarningGraphUnavailable, result.Warnings[0].Code)
		assert.Empty(t, result.Evidence)
	})

	t.Run("incompatible", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		require.NoError(t, repo.setSchemaVersionForTest(ctx, SchemaVersion+1))
		service := NewQueryService(repo, QueryServiceOptions{})

		result, err := service.Neighbors(ctx, NeighborRequest{
			ProjectID: "project-1",
			Query:     "service",
			Limit:     1,
			Depth:     1,
		})
		require.NoError(t, err)
		assert.Equal(t, GraphStatusIncompatible, result.Status)
		assert.False(t, result.Available)
		assert.True(t, result.Degraded)
		require.NotEmpty(t, result.Warnings)
		assert.Equal(t, WarningSchemaIncompatible, result.Warnings[0].Code)
		assert.Empty(t, result.Evidence)
	})

	t.Run("empty", func(t *testing.T) {
		service := NewQueryService(newTestSQLiteRepository(t), QueryServiceOptions{})

		result, err := service.Neighbors(ctx, NeighborRequest{
			ProjectID: "project-1",
			Query:     "service",
			Limit:     1,
			Depth:     1,
		})
		require.NoError(t, err)
		assert.Equal(t, GraphStatusEmpty, result.Status)
		assert.True(t, result.Degraded)
		assert.Empty(t, result.Evidence)
	})
}

func TestQueryService_DegradesStalePartialAndFailedGraphStates(t *testing.T) {
	ctx := context.Background()

	t.Run("stale", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		seedGraphServiceFixture(t, ctx, repo, GraphStatusFresh)
		require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
			ProjectID:   "project-1",
			Status:      GraphStatusFresh,
			StartedAt:   fixedGraphTime().Add(-49 * time.Hour),
			CompletedAt: fixedGraphTime().Add(-48 * time.Hour),
		}))
		service := NewQueryService(repo, QueryServiceOptions{
			Now:        func() time.Time { return fixedGraphTime() },
			StaleAfter: 24 * time.Hour,
		})

		result, err := service.Neighbors(ctx, NeighborRequest{ProjectID: "project-1", Query: "NewQueryService", Limit: 3, Depth: 1})
		require.NoError(t, err)
		assert.Equal(t, GraphStatusStale, result.Status)
		assert.True(t, result.Degraded)
		assertWarningCode(t, result.Warnings, WarningGraphStale)
		assert.NotEmpty(t, result.Evidence)
	})

	t.Run("partial", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		seedGraphServiceFixture(t, ctx, repo, GraphStatusPartial)
		require.NoError(t, repo.RecordExtractorRun(ctx, ExtractorRun{
			ProjectID:   "project-1",
			Extractor:   ExtractorCheap,
			SourcePath:  "internal/graph/service.go",
			Status:      ExtractorStatusPartial,
			CompletedAt: fixedGraphTime(),
			Warnings:    []string{"symbol extraction skipped generated block"},
		}))
		service := NewQueryService(repo, QueryServiceOptions{
			Now:        func() time.Time { return fixedGraphTime().Add(time.Minute) },
			StaleAfter: 24 * time.Hour,
		})

		result, err := service.Neighbors(ctx, NeighborRequest{ProjectID: "project-1", Query: "NewQueryService", Limit: 3, Depth: 1})
		require.NoError(t, err)
		assert.Equal(t, GraphStatusPartial, result.Status)
		assert.True(t, result.Degraded)
		assertWarningCode(t, result.Warnings, WarningExtractorPartial)
		assert.NotEmpty(t, result.Evidence)
	})

	t.Run("failed", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		seedGraphServiceFixture(t, ctx, repo, GraphStatusFailed)
		service := NewQueryService(repo, QueryServiceOptions{
			Now:        func() time.Time { return fixedGraphTime().Add(time.Minute) },
			StaleAfter: 24 * time.Hour,
		})

		result, err := service.Neighbors(ctx, NeighborRequest{ProjectID: "project-1", Query: "NewQueryService", Limit: 3, Depth: 1})
		require.NoError(t, err)
		assert.Equal(t, GraphStatusFailed, result.Status)
		assert.True(t, result.Degraded)
		assertWarningCode(t, result.Warnings, WarningBuildFailed)
		assert.NotEmpty(t, result.Evidence)
	})
}

func TestQueryService_Neighbors_TruncatesAtLimit(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	file := upsertTestNode(t, ctx, repo, "project-1", NodeKindFile, "internal/graph/source.go")
	for _, symbolName := range []string{"A", "B", "C"} {
		symbol := upsertTestNode(t, ctx, repo, "project-1", NodeKindSymbol, "internal/graph/source.go#"+symbolName+":1")
		_, err := repo.UpsertEdge(ctx, Edge{
			ProjectID:  "project-1",
			Kind:       EdgeKindFileDefinesSymbol,
			FromNodeID: file.ID,
			ToNodeID:   symbol.ID,
			Extractor:  ExtractorCheap,
			SourcePath: "internal/graph/source.go",
			Confidence: 0.95,
			Evidence:   Evidence{Method: "test_symbol", Line: 1, Snippet: "func " + symbolName + "()"},
		})
		require.NoError(t, err)
	}
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID:   "project-1",
		Status:      GraphStatusFresh,
		StartedAt:   fixedGraphTime().Add(-time.Second),
		CompletedAt: fixedGraphTime(),
	}))

	service := NewQueryService(repo, QueryServiceOptions{
		Now:        func() time.Time { return fixedGraphTime().Add(time.Minute) },
		StaleAfter: 24 * time.Hour,
	})

	result, err := service.Neighbors(ctx, NeighborRequest{
		ProjectID: "project-1",
		Query:     "internal/graph/source.go",
		Limit:     2,
		Depth:     1,
	})
	require.NoError(t, err)

	assert.True(t, result.Truncated)
	assert.Len(t, result.Evidence, 2)
}

func TestQueryService_Neighbors_HandlesCyclesAndSelfLoopsDeterministically(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepository(t)
	a := upsertTestNode(t, ctx, repo, "project-1", NodeKindFile, "a.go")
	b := upsertTestNode(t, ctx, repo, "project-1", NodeKindFile, "b.go")
	c := upsertTestNode(t, ctx, repo, "project-1", NodeKindFile, "c.go")
	for _, edge := range []Edge{
		{ProjectID: "project-1", Kind: EdgeKindDocMentionsPath, FromNodeID: a.ID, ToNodeID: b.ID, Extractor: ExtractorCheap, SourcePath: "a.go", Confidence: 0.9, Evidence: Evidence{Method: "fixture"}},
		{ProjectID: "project-1", Kind: EdgeKindDocMentionsPath, FromNodeID: b.ID, ToNodeID: c.ID, Extractor: ExtractorCheap, SourcePath: "b.go", Confidence: 0.9, Evidence: Evidence{Method: "fixture"}},
		{ProjectID: "project-1", Kind: EdgeKindDocMentionsPath, FromNodeID: c.ID, ToNodeID: a.ID, Extractor: ExtractorCheap, SourcePath: "c.go", Confidence: 0.9, Evidence: Evidence{Method: "fixture"}},
		{ProjectID: "project-1", Kind: EdgeKindDocMentionsPath, FromNodeID: a.ID, ToNodeID: a.ID, Extractor: ExtractorCheap, SourcePath: "a.go", Confidence: 0.9, Evidence: Evidence{Method: "self"}},
	} {
		_, err := repo.UpsertEdge(ctx, edge)
		require.NoError(t, err)
	}
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID:   "project-1",
		Status:      GraphStatusFresh,
		CompletedAt: fixedGraphTime(),
	}))
	service := NewQueryService(repo, QueryServiceOptions{Now: fixedGraphTime})

	result, err := service.Neighbors(ctx, NeighborRequest{ProjectID: "project-1", Query: "a.go", Limit: 10, Depth: 3})

	require.NoError(t, err)
	assert.False(t, result.Truncated)
	seen := map[string]bool{}
	for _, evidence := range result.Evidence {
		if seen[evidence.Node.ID] {
			t.Fatalf("duplicate evidence node %q in %#v", evidence.Node.ID, result.Evidence)
		}
		seen[evidence.Node.ID] = true
	}
	assert.True(t, seen[a.ID])
	assert.True(t, seen[b.ID])
	assert.True(t, seen[c.ID])
}

func TestQueryService_Neighbors_RejectsOrphanEdgesDeterministically(t *testing.T) {
	file := Node{ID: "file-a", ProjectID: "project-1", Kind: NodeKindFile, Key: "a.go"}

	_, err := newGraphIndex([]Node{file}, []Edge{{
		ProjectID:  "project-1",
		Kind:       EdgeKindDocMentionsPath,
		FromNodeID: file.ID,
		ToNodeID:   "missing-node",
		Extractor:  ExtractorCheap,
		SourcePath: "a.go",
		Confidence: 0.9,
		Evidence:   Evidence{Method: "fixture"},
	}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "references missing endpoint")
}

type graphServiceFixture struct {
	file   Node
	symbol Node
	chunk  Node
}

func seedGraphServiceFixture(t *testing.T, ctx context.Context, repo Repository, status GraphStatus) graphServiceFixture {
	t.Helper()

	file := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID:  "project-1",
		Kind:       NodeKindFile,
		Key:        "internal/graph/service.go",
		SourcePath: "internal/graph/service.go",
		Name:       "service.go",
		Language:   "go",
	})
	symbol := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID:  "project-1",
		Kind:       NodeKindSymbol,
		Key:        "internal/graph/service.go#NewQueryService:12",
		SourcePath: "internal/graph/service.go",
		Name:       "NewQueryService",
		Language:   "go",
		SymbolKind: "function",
		StartLine:  12,
		EndLine:    18,
	})
	chunk := upsertFixtureNode(t, ctx, repo, Node{
		ProjectID:  "project-1",
		Kind:       NodeKindChunk,
		Key:        "chunk:service:12-18",
		SourcePath: "internal/graph/service.go",
		Name:       "NewQueryService chunk",
		Language:   "go",
		StartLine:  12,
		EndLine:    18,
	})

	_, err := repo.UpsertEdge(ctx, Edge{
		ProjectID:  "project-1",
		Kind:       EdgeKindFileDefinesSymbol,
		FromNodeID: file.ID,
		ToNodeID:   symbol.ID,
		Extractor:  ExtractorCheap,
		SourcePath: "internal/graph/service.go",
		Confidence: 0.95,
		Evidence: Evidence{
			Method:  "go_symbol",
			Line:    12,
			Snippet: "func NewQueryService(repo Repository, opts QueryServiceOptions) *QueryService",
		},
	})
	require.NoError(t, err)
	_, err = repo.UpsertEdge(ctx, Edge{
		ProjectID:  "project-1",
		Kind:       EdgeKindSymbolHasChunk,
		FromNodeID: symbol.ID,
		ToNodeID:   chunk.ID,
		Extractor:  ExtractorCheap,
		SourcePath: "internal/graph/service.go",
		Confidence: 0.92,
		Evidence: Evidence{
			Method:  "chunk_symbol",
			Line:    12,
			Snippet: "NewQueryService chunk",
		},
	})
	require.NoError(t, err)

	message := ""
	if status == GraphStatusFailed {
		message = "cheap graph build failed"
	}
	require.NoError(t, repo.RecordBuild(ctx, BuildMetadata{
		ProjectID:   "project-1",
		Status:      status,
		StartedAt:   fixedGraphTime().Add(-time.Second),
		CompletedAt: fixedGraphTime(),
		Message:     message,
	}))
	return graphServiceFixture{file: file, symbol: symbol, chunk: chunk}
}

func upsertFixtureNode(t *testing.T, ctx context.Context, repo Repository, node Node) Node {
	t.Helper()
	stored, err := repo.UpsertNode(ctx, node)
	require.NoError(t, err)
	return stored
}

func findGraphEvidenceByRole(t *testing.T, evidence []GraphEvidence, role GraphRole) GraphEvidence {
	t.Helper()
	for _, item := range evidence {
		if item.Role == role {
			return item
		}
	}
	require.FailNowf(t, "missing graph evidence role", "role %q not found in %#v", role, evidence)
	return GraphEvidence{}
}

func assertWarningCode(t *testing.T, warnings []StatusWarning, code WarningCode, messageContains ...string) {
	t.Helper()
	for _, warning := range warnings {
		if warning.Code == code {
			if len(messageContains) > 0 && messageContains[0] != "" {
				assert.Contains(t, warning.Message, messageContains[0])
			}
			return
		}
	}
	require.FailNowf(t, "missing graph warning", "code %q not found in %#v", code, warnings)
}
