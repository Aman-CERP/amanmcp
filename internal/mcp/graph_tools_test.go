package mcp

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraphQueryOutput_UsesSharedGraphToolProjection(t *testing.T) {
	response := graph.QueryResponse{
		Status:   graph.GraphStatusStale,
		Degraded: true,
		Mode:     graph.QueryModeImpactAnalysis,
		Query:    "QueryService",
		Results: []graph.QueryResult{{
			NodeID:     "file:internal/eval/graph_runner.go",
			NodeKind:   graph.NodeKindFile,
			SourcePath: "internal/eval/graph_runner.go",
			Role:       "downstream",
			Relation:   graph.EdgeKindSymbolHasChunk,
			Path: graph.GraphPath{
				From: graph.GraphNodeEvidence{ID: "symbol:QueryService", Kind: graph.NodeKindSymbol},
				To:   graph.GraphNodeEvidence{ID: "chunk:runner", Kind: graph.NodeKindChunk},
				Hops: []graph.GraphEvidence{{
					Node:     graph.GraphNodeEvidence{ID: "chunk:runner", Kind: graph.NodeKindChunk},
					Relation: string(graph.EdgeKindSymbolHasChunk),
				}},
			},
		}},
		Warnings: []graph.StatusWarning{{
			Code:    graph.WarningGraphStale,
			Message: "graph is stale",
		}},
	}

	assert.Equal(t, graph.NewQueryToolOutput(response), graphQueryOutput(response))
}

func TestHandleGraphQueryTool_NoServiceUsesSharedUnavailableProjection(t *testing.T) {
	srv := newTestServer(t)

	output, err := srv.handleGraphQueryTool(context.Background(), GraphQueryInput{
		Query: "internal/graph/query.go",
	})

	require.NoError(t, err)
	assert.Equal(t, graph.NewUnavailableQueryToolOutput(
		graph.QueryModeFindReferences,
		"internal/graph/query.go",
		"graph.query is unavailable because no graph repository is configured",
	), output)
}

func TestGraphQueryInput_IncludesSubjectTypeSchema(t *testing.T) {
	field, ok := reflect.TypeOf(GraphQueryInput{}).FieldByName("SubjectType")
	require.True(t, ok, "GraphQueryInput must expose SubjectType")
	assert.Equal(t, "subject_type,omitempty", field.Tag.Get("json"))

	schema := field.Tag.Get("jsonschema")
	for _, snippet := range []string{"auto", "path", "symbol", "package", "result_id"} {
		assert.True(t, strings.Contains(schema, snippet), "schema %q must document %s", schema, snippet)
	}

	queryField, ok := reflect.TypeOf(GraphQueryInput{}).FieldByName("Query")
	require.True(t, ok, "GraphQueryInput must expose Query")
	querySchema := queryField.Tag.Get("jsonschema")
	assert.Contains(t, querySchema, "stable graph node id")
	assert.NotContains(t, querySchema, "search identifier",
		"v1 result_id resolves graph node ids only; public search hashes are irreversible")
}

func TestHandleGraphQueryArgs_ForwardsExplicitSubjectTypes(t *testing.T) {
	ctx := context.Background()
	repo, err := graph.OpenSQLiteRepository(filepath.Join(t.TempDir(), "graph.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repo.Close()) })

	file, err := repo.UpsertNode(ctx, graph.Node{
		ProjectID: "project-1", Kind: graph.NodeKindFile,
		Key: "internal/search/engine.go", SourcePath: "internal/search/engine.go", Name: "engine.go",
	})
	require.NoError(t, err)
	symbol, err := repo.UpsertNode(ctx, graph.Node{
		ProjectID: "project-1", Kind: graph.NodeKindSymbol,
		Key: "internal/search/engine.go#Search:42", SourcePath: "internal/search/engine.go",
		Name: "Search", SymbolKind: "function", StartLine: 42,
	})
	require.NoError(t, err)
	pkg, err := repo.UpsertNode(ctx, graph.Node{
		ProjectID: "project-1", Kind: graph.NodeKindPackage,
		Key: "internal/search#search", Name: "search",
	})
	require.NoError(t, err)
	chunk, err := repo.UpsertNode(ctx, graph.Node{
		ProjectID: "project-1", Kind: graph.NodeKindChunk,
		Key: "internal/search/engine.go#chunk:1", SourcePath: "internal/search/engine.go",
	})
	require.NoError(t, err)
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, graph.Edge{
		ProjectID: "project-1", Kind: graph.EdgeKindFileDefinesSymbol, FromNodeID: file.ID, ToNodeID: symbol.ID,
		Extractor: graph.ExtractorCheap, SourcePath: "internal/search/engine.go", Confidence: 0.95,
		Evidence: graph.Evidence{Method: "chunk_symbol", SourcePath: "internal/search/engine.go", LineStart: 42, LineEnd: 42},
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, graph.Edge{
		ProjectID: "project-1", Kind: graph.EdgeKindFileDeclaresPackage, FromNodeID: file.ID, ToNodeID: pkg.ID,
		Extractor: graph.ExtractorCheap, SourcePath: "internal/search/engine.go", Confidence: 1,
		Evidence: graph.Evidence{Method: "go_package_declaration", SourcePath: "internal/search/engine.go", LineStart: 1, LineEnd: 1},
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, graph.Edge{
		ProjectID: "project-1", Kind: graph.EdgeKindSymbolHasChunk, FromNodeID: symbol.ID, ToNodeID: chunk.ID,
		Extractor: graph.ExtractorCheap, SourcePath: "internal/search/engine.go", Confidence: 1,
		Evidence: graph.Evidence{Method: "chunk_symbol_membership", SourcePath: "internal/search/engine.go", LineStart: 42, LineEnd: 42},
	}))
	now := func() time.Time { return time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC) }
	require.NoError(t, repo.RecordBuild(ctx, graph.BuildMetadata{
		ProjectID: "project-1", Status: graph.GraphStatusFresh, CompletedAt: now(),
	}))

	srv := newTestServer(t)
	srv.SetGraphQueryService(graph.NewQueryService(repo, graph.QueryServiceOptions{Now: now}))

	tests := []struct {
		name        string
		subjectType string
		query       string
	}{
		{name: "path", subjectType: graph.SubjectTypePath, query: "internal/search/engine.go"},
		{name: "symbol", subjectType: graph.SubjectTypeSymbol, query: "Search"},
		{name: "package", subjectType: graph.SubjectTypePackage, query: "internal/search#search"},
		{name: "result id", subjectType: graph.SubjectTypeResultID, query: chunk.ID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := srv.handleGraphQueryArgs(ctx, map[string]any{
				"project_id":   "project-1",
				"mode":         graph.QueryModeFindReferences,
				"query":        tt.query,
				"subject_type": tt.subjectType,
			})

			require.NoError(t, err)
			assert.Equal(t, graph.ResolutionResolved, output.Resolution)
			assert.NotEmpty(t, output.Results)
		})
	}

	output, err := srv.handleGraphQueryArgs(ctx, map[string]any{
		"project_id":   "project-1",
		"mode":         graph.QueryModeFindReferences,
		"query":        "engine.go",
		"subject_type": graph.SubjectTypePath,
	})
	require.NoError(t, err)
	assert.Equal(t, graph.ResolutionSubjectNotFound, output.Resolution,
		"subject_type=path must be forwarded, not treated as auto substring matching")
	assert.Empty(t, output.Results)
}
