package mcp

import (
	"context"
	"testing"

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
			GraphPath:  []string{"symbol:QueryService", string(graph.EdgeKindSymbolHasChunk), "chunk:runner"},
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
