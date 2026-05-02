package mcp

import (
	"context"
	"encoding/json"
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
	assert.Equal(t, 1, decoded.Confidence[string(graph.ConfidenceHigh)])
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
