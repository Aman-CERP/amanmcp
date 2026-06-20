package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/graph"
)

type fakeGraphStatusProvider struct {
	snapshot *graph.StatusSnapshot
	err      error
}

func (f fakeGraphStatusProvider) Snapshot(_ context.Context, _ graph.StatusOptions) (*graph.StatusSnapshot, error) {
	return f.snapshot, f.err
}

func TestGraphStatusResource_ReturnsStructuredCompactJSON(t *testing.T) {
	srv := newTestServer(t)
	srv.SetGraphStatusProvider(fakeGraphStatusProvider{
		snapshot: &graph.StatusSnapshot{
			Available:     true,
			SchemaVersion: graph.SchemaVersion,
			Status:        graph.GraphStatusFresh,
			GeneratedAt:   time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
			Freshness: graph.Freshness{
				State: graph.FreshnessFresh,
			},
			LastFullBuild: &graph.BuildTiming{
				StartedAt:     "2026-05-01T11:59:00Z",
				CompletedAt:   "2026-05-01T12:00:00Z",
				SourceVersion: "full-v1",
			},
			LastIncrementalUpdate: &graph.BuildTiming{
				StartedAt:     "2026-05-01T12:01:00Z",
				CompletedAt:   "2026-05-01T12:01:01Z",
				SourceVersion: "inc-v1",
			},
			Nodes: graph.CountSummary{
				Total: 2,
				ByKind: map[string]int{
					string(graph.NodeKindFile):   1,
					string(graph.NodeKindSymbol): 1,
				},
			},
			Edges: graph.CountSummary{
				Total: 1,
				ByKind: map[string]int{
					string(graph.EdgeKindFileDefinesSymbol): 1,
				},
			},
			ActiveEdges: graph.CountSummary{
				Total: 1,
				ByKind: map[string]int{
					string(graph.EdgeKindFileDefinesSymbol): 1,
				},
			},
			StaleEdges: graph.CountSummary{ByKind: map[string]int{}},
			Confidence: map[string]int{
				string(graph.ConfidenceHigh): 1,
			},
			Extractors: []graph.ExtractorSummary{{
				Name:      graph.ExtractorCheap,
				Status:    graph.ExtractorStatusSuccess,
				EdgeCount: 1,
			}},
		},
	})

	result, err := srv.handleGraphStatusResource(context.Background())
	require.NoError(t, err)
	require.Len(t, result.Contents, 1)
	assert.Equal(t, "amanmcp://graph_status", result.Contents[0].URI)
	assert.Equal(t, "application/json", result.Contents[0].MIMEType)
	assert.NotContains(t, result.Contents[0].Text, "\n  ")

	var decoded graph.StatusSnapshot
	require.NoError(t, json.Unmarshal([]byte(result.Contents[0].Text), &decoded))
	assert.True(t, decoded.Available)
	assert.Equal(t, graph.GraphStatusFresh, decoded.Status)
	assert.Equal(t, 1, decoded.Edges.ByKind[string(graph.EdgeKindFileDefinesSymbol)])
	assert.Equal(t, 1, decoded.ActiveEdges.ByKind[string(graph.EdgeKindFileDefinesSymbol)])
	assert.Equal(t, 0, decoded.StaleEdges.Total)
	assert.Equal(t, 1, decoded.Confidence[string(graph.ConfidenceHigh)])
	require.NotNil(t, decoded.LastFullBuild)
	assert.Equal(t, "full-v1", decoded.LastFullBuild.SourceVersion)
	require.NotNil(t, decoded.LastIncrementalUpdate)
	assert.Equal(t, "inc-v1", decoded.LastIncrementalUpdate.SourceVersion)
}

func TestGraphStatusResource_RealSQLiteReportsActiveAndStaleEdgeCounts(t *testing.T) {
	ctx := context.Background()
	repo, err := graph.OpenSQLiteRepository(filepath.Join(t.TempDir(), "graph.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, repo.Close())
	})
	srv := newTestServer(t)
	srv.SetGraphStatusProvider(repo)

	doc := upsertGraphStatusTestNode(t, ctx, repo, "project-1", graph.NodeKindDoc, "docs/design.md")
	active := upsertGraphStatusTestNode(t, ctx, repo, "project-1", graph.NodeKindFile, "active.go")
	stale := upsertGraphStatusTestNode(t, ctx, repo, "project-1", graph.NodeKindFile, "stale.go")
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, graph.Edge{
		ProjectID:       "project-1",
		Kind:            graph.EdgeKindDocMentionsFile,
		FromNodeID:      doc.ID,
		ToNodeID:        active.ID,
		Extractor:       graph.ExtractorCheap,
		SourcePath:      "docs/design.md",
		Confidence:      0.9,
		ConfidenceLabel: graph.ConfidenceHigh,
		Evidence:        graph.Evidence{Method: "test", SourcePath: "docs/design.md", LineStart: 1, LineEnd: 1},
	}))
	require.NoError(t, repo.UpsertEdgeOnlyForTest(ctx, graph.Edge{
		ProjectID:       "project-1",
		Kind:            graph.EdgeKindDocMentionsFile,
		FromNodeID:      doc.ID,
		ToNodeID:        stale.ID,
		Extractor:       graph.ExtractorCheap,
		SourcePath:      "docs/design.md",
		Confidence:      0.9,
		ConfidenceLabel: graph.ConfidenceHigh,
		Stale:           true,
		Evidence:        graph.Evidence{Method: "test", SourcePath: "docs/design.md", LineStart: 1, LineEnd: 1},
	}))
	require.NoError(t, repo.RecordBuild(ctx, graph.BuildMetadata{
		ProjectID:   "project-1",
		Status:      graph.GraphStatusFresh,
		CompletedAt: time.Now().UTC(),
	}))

	result, err := srv.handleGraphStatusResource(ctx)
	require.NoError(t, err)
	require.Len(t, result.Contents, 1)

	var decoded graph.StatusSnapshot
	require.NoError(t, json.Unmarshal([]byte(result.Contents[0].Text), &decoded))
	assert.Equal(t, 1, decoded.ActiveEdges.ByKind[string(graph.EdgeKindDocMentionsFile)])
	assert.Equal(t, 1, decoded.StaleEdges.ByKind[string(graph.EdgeKindDocMentionsFile)])
	assert.NotContains(t, result.Contents[0].Text, "docs/design.md#")
}

func upsertGraphStatusTestNode(t *testing.T, ctx context.Context, repo *graph.SQLiteRepository, projectID string, kind graph.NodeKind, key string) graph.Node {
	t.Helper()
	node, err := repo.UpsertNode(ctx, graph.Node{
		ProjectID:  projectID,
		Kind:       kind,
		Key:        key,
		SourcePath: key,
		Name:       filepath.Base(key),
	})
	require.NoError(t, err)
	return node
}

func TestGraphStatusResource_ReportsUnavailableWithoutProvider(t *testing.T) {
	srv := newTestServer(t)

	result, err := srv.handleGraphStatusResource(context.Background())
	require.NoError(t, err)
	require.Len(t, result.Contents, 1)

	var decoded graph.StatusSnapshot
	require.NoError(t, json.Unmarshal([]byte(result.Contents[0].Text), &decoded))
	assert.False(t, decoded.Available)
	assert.Equal(t, graph.GraphStatusUnavailable, decoded.Status)
	require.NotEmpty(t, decoded.Warnings)
	assert.Equal(t, graph.WarningGraphUnavailable, decoded.Warnings[0].Code)
}

func TestGraphStatusResource_IsRegisteredWithoutProvider(t *testing.T) {
	srv := newTestServer(t)

	ctx := context.Background()
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := srv.MCPServer().Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer serverSession.Close()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "graph-status-test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer clientSession.Close()

	list, err := clientSession.ListResources(ctx, nil)
	require.NoError(t, err)
	seen := false
	for _, resource := range list.Resources {
		if resource.URI == graphStatusURI {
			seen = true
			break
		}
	}
	require.True(t, seen, "missing graph_status resource")

	result, err := clientSession.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: graphStatusURI})
	require.NoError(t, err)
	require.Len(t, result.Contents, 1)

	var decoded graph.StatusSnapshot
	require.NoError(t, json.Unmarshal([]byte(result.Contents[0].Text), &decoded))
	assert.False(t, decoded.Available)
	assert.Equal(t, graph.GraphStatusUnavailable, decoded.Status)
}
