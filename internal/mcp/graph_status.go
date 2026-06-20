package mcp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Aman-CERP/amanmcp/internal/graph"
)

const graphStatusURI = "amanmcp://graph_status"

// SetGraphStatusProvider registers the graph_status MCP resource when graph storage is available.
func (s *Server) SetGraphStatusProvider(provider graph.StatusProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.graphStatus = provider
	if repo, ok := provider.(graph.Repository); ok {
		s.graphQuery = graph.NewQueryService(repo, graph.QueryServiceOptions{})
	}
}

func (s *Server) registerGraphStatusResource() {
	s.mcp.AddResource(
		&mcp.Resource{
			Name:        "graph_status",
			URI:         graphStatusURI,
			Description: "AmanGraph health, freshness, edge counts, extractor status, and warnings",
			MIMEType:    "application/json",
		},
		func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return s.handleGraphStatusResource(ctx)
		},
	)
}

func (s *Server) handleGraphStatusResource(ctx context.Context) (*mcp.ReadResourceResult, error) {
	s.mu.RLock()
	provider := s.graphStatus
	s.mu.RUnlock()

	var snapshot *graph.StatusSnapshot
	if provider == nil {
		now := time.Now().UTC()
		snapshot = &graph.StatusSnapshot{
			Available:     false,
			SchemaVersion: graph.SchemaVersion,
			Status:        graph.GraphStatusUnavailable,
			GeneratedAt:   now,
			Freshness: graph.Freshness{
				State: graph.FreshnessUnknown,
			},
			Nodes:       graph.CountSummary{ByKind: map[string]int{}},
			Edges:       graph.CountSummary{ByKind: map[string]int{}},
			ActiveEdges: graph.CountSummary{ByKind: map[string]int{}},
			StaleEdges:  graph.CountSummary{ByKind: map[string]int{}},
			Confidence:  map[string]int{},
			Warnings: []graph.StatusWarning{{
				Code:    graph.WarningGraphUnavailable,
				Message: "graph status provider is not configured",
			}},
		}
	} else {
		var err error
		snapshot, err = provider.Snapshot(ctx, graph.StatusOptions{
			Now:        time.Now().UTC(),
			StaleAfter: graph.DefaultStaleAfter,
		})
		if err != nil {
			return nil, MapError(err)
		}
	}

	content, err := json.Marshal(snapshot)
	if err != nil {
		return nil, MapError(err)
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      graphStatusURI,
			MIMEType: "application/json",
			Text:     string(content),
		}},
	}, nil
}
