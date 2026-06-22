package mcp

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/graph"
	"github.com/Aman-CERP/amanmcp/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandContextOutput_ProjectsMCPEnvelopeFields(t *testing.T) {
	response := graph.ExpandContextResponse{
		Available:  true,
		Status:     graph.GraphStatusFresh,
		Degraded:   false,
		Seed:       "NewQueryService",
		Resolution: graph.ResolutionResolved,
		Pack: []graph.PackItem{{
			NodeID: "node:chunk:1", NodeKind: graph.NodeKindChunk,
			SourcePath: "internal/graph/service.go",
			Roles:      []graph.RoleAssignment{{Role: graph.ContextRoleImplementation, ConfidenceLabel: graph.ConfidenceHigh}},
			Path: graph.GraphPath{
				From: graph.GraphNodeEvidence{ID: "node:symbol:1"},
				To:   graph.GraphNodeEvidence{ID: "node:chunk:1"},
			},
		}},
	}
	output := NewExpandContextOutput(response)
	assert.True(t, output.Available)
	assert.Equal(t, graph.GraphStatusFresh, output.Status)
	assert.Equal(t, graph.ResolutionResolved, output.Resolution)
	require.Len(t, output.Pack, 1)
	assert.Equal(t, "node:chunk:1", output.Pack[0].NodeID)
}

func TestHandleExpandContextTool_HydratesChunkContent(t *testing.T) {
	ctx := context.Background()
	repo, err := graph.OpenSQLiteRepository(filepath.Join(t.TempDir(), "graph.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repo.Close()) })

	chunkID := "chunk-hydrate-1"
	file, err := repo.UpsertNode(ctx, graph.Node{
		ProjectID: "project-1", Kind: graph.NodeKindFile,
		Key: "internal/graph/service.go", SourcePath: "internal/graph/service.go",
	})
	require.NoError(t, err)
	symbol, err := repo.UpsertNode(ctx, graph.Node{
		ProjectID: "project-1", Kind: graph.NodeKindSymbol,
		Key: "internal/graph/service.go#NewQueryService:12", SourcePath: "internal/graph/service.go",
		Name: "NewQueryService", StartLine: 12,
	})
	require.NoError(t, err)
	chunk, err := repo.UpsertNode(ctx, graph.Node{
		ProjectID: "project-1", Kind: graph.NodeKindChunk,
		Key: chunkID, SourcePath: "internal/graph/service.go", StartLine: 12, EndLine: 18,
	})
	require.NoError(t, err)
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, graph.Edge{
		ProjectID: "project-1", Kind: graph.EdgeKindFileDefinesSymbol,
		FromNodeID: file.ID, ToNodeID: symbol.ID, Extractor: graph.ExtractorCheap,
		SourcePath: "internal/graph/service.go", Confidence: 0.95,
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, graph.Edge{
		ProjectID: "project-1", Kind: graph.EdgeKindSymbolHasChunk,
		FromNodeID: symbol.ID, ToNodeID: chunk.ID, Extractor: graph.ExtractorCheap,
		SourcePath: "internal/graph/service.go", Confidence: 0.92,
	}))
	now := func() time.Time { return time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC) }
	require.NoError(t, repo.RecordBuild(ctx, graph.BuildMetadata{
		ProjectID: "project-1", Status: graph.GraphStatusFresh, CompletedAt: now(),
	}))

	metadata := &MockMetadataStore{
		Chunks: []*store.Chunk{{
			ID: chunkID, FilePath: "internal/graph/service.go",
			Content: "func NewQueryService() {}", StartLine: 12, EndLine: 18,
		}},
	}
	srv, err := NewServer(&MockSearchEngine{}, metadata, &MockEmbedder{}, nil, "")
	require.NoError(t, err)
	srv.SetGraphQueryService(graph.NewQueryService(repo, graph.QueryServiceOptions{Now: now}))

	output, err := srv.handleExpandContextTool(ctx, ExpandContextInput{
		ProjectID: "project-1",
		Seed:      "NewQueryService",
		SeedType:  graph.SubjectTypeSymbol,
		Depth:     1,
	})
	require.NoError(t, err)
	require.NotEmpty(t, output.Pack)

	var hydrated *ContextItem
	for i := range output.Pack {
		if output.Pack[i].NodeID == chunk.ID {
			hydrated = &output.Pack[i]
			break
		}
	}
	require.NotNil(t, hydrated, "expected chunk pack item")
	assert.Equal(t, chunkID, hydrated.ContentRef)
	assert.Contains(t, hydrated.Content, "func NewQueryService")
}

func TestHandleExpandContextTool_NoServiceUsesUnavailableEnvelope(t *testing.T) {
	srv := newTestServer(t)
	output, err := srv.handleExpandContextTool(context.Background(), ExpandContextInput{
		Seed: "internal/graph/query.go",
	})
	require.NoError(t, err)
	assert.False(t, output.Available)
	assert.True(t, output.Degraded)
	assert.Empty(t, output.Pack)
	require.NotEmpty(t, output.Warnings)
}

func TestExpandContextInput_IncludesSeedTypeSchema(t *testing.T) {
	field, ok := reflect.TypeOf(ExpandContextInput{}).FieldByName("SeedType")
	require.True(t, ok)
	assert.Equal(t, "seed_type,omitempty", field.Tag.Get("json"))
	schema := field.Tag.Get("jsonschema")
	for _, snippet := range []string{"auto", "path", "symbol", "result_id"} {
		assert.True(t, strings.Contains(schema, snippet), "schema %q must document %s", schema, snippet)
	}
}

func TestCallTool_ExpandContextRegistered(t *testing.T) {
	srv := newTestServer(t)
	names := map[string]bool{}
	for _, tool := range srv.ListTools() {
		names[tool.Name] = true
	}
	assert.True(t, names["expand_context"])
}

func TestCallTool_ExpandContextArgs(t *testing.T) {
	ctx := context.Background()
	repo, err := graph.OpenSQLiteRepository(filepath.Join(t.TempDir(), "graph.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repo.Close()) })

	file, err := repo.UpsertNode(ctx, graph.Node{
		ProjectID: "project-1", Kind: graph.NodeKindFile,
		Key: "internal/graph/service.go", SourcePath: "internal/graph/service.go",
	})
	require.NoError(t, err)
	require.NoError(t, repo.RecordBuild(ctx, graph.BuildMetadata{
		ProjectID: "project-1", Status: graph.GraphStatusFresh,
		CompletedAt: time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC),
	}))

	srv := newTestServer(t)
	srv.SetGraphQueryService(graph.NewQueryService(repo, graph.QueryServiceOptions{
		Now: func() time.Time { return time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC) },
	}))

	output, err := srv.CallTool(ctx, "expand_context", map[string]any{
		"project_id": "project-1",
		"seed":       file.SourcePath,
		"seed_type":  graph.SubjectTypePath,
		"depth":      float64(1),
	})
	require.NoError(t, err)
	typed, ok := output.(ExpandContextOutput)
	require.True(t, ok)
	assert.Equal(t, graph.ResolutionResolved, typed.Resolution)
}

func TestBestChunkForItem_SelectsLineOverlap(t *testing.T) {
	chunks := []*store.Chunk{
		{ID: "c1", Content: "package graph", StartLine: 1, EndLine: 5},
		{ID: "c2", Content: "func Search() {}", StartLine: 40, EndLine: 55},
	}
	chunk := bestChunkForItem(chunks, 42, 50)
	require.NotNil(t, chunk)
	assert.Equal(t, "c2", chunk.ID)
}

func TestBestChunkForItem_FallsBackWhenNoOverlap(t *testing.T) {
	chunks := []*store.Chunk{
		{ID: "c1", Content: "first", StartLine: 1, EndLine: 5},
		{ID: "c2", Content: "second", StartLine: 40, EndLine: 55},
	}
	chunk := bestChunkForItem(chunks, 20, 25)
	require.NotNil(t, chunk)
	assert.Equal(t, "c1", chunk.ID)
}

func TestLineOverlap_ComputesSharedLines(t *testing.T) {
	assert.Equal(t, 4, lineOverlap(10, 15, 12, 20))
	assert.Equal(t, 0, lineOverlap(10, 15, 20, 25))
}