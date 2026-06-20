package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Aman-CERP/amanmcp/internal/graph"
)

type GraphQueryInput struct {
	ProjectID    string `json:"project_id,omitempty" jsonschema:"AmanMCP project id; defaults to the server project when available"`
	Mode         string `json:"mode,omitempty" jsonschema:"graph query mode: find_references, explain_symbol, or impact_analysis"`
	Query        string `json:"query" jsonschema:"symbol, project-relative path, or stable graph/search identifier to query"`
	Limit        int    `json:"limit,omitempty" jsonschema:"maximum number of graph evidence results, default 10, maximum 50"`
	IncludeStale bool   `json:"include_stale,omitempty" jsonschema:"include stale graph edges that point at deleted or replaced sources"`
}

type GraphQueryOutput struct {
	Available bool                  `json:"available"`
	Status    graph.GraphStatus     `json:"status"`
	Degraded  bool                  `json:"degraded"`
	Mode      string                `json:"mode"`
	Query     string                `json:"query"`
	Results   []graph.QueryResult   `json:"results,omitempty"`
	Warnings  []graph.StatusWarning `json:"warnings,omitempty"`
}

func (s *Server) handleGraphQueryArgs(ctx context.Context, args map[string]any) (GraphQueryOutput, error) {
	input := GraphQueryInput{
		ProjectID:    stringArg(args, "project_id"),
		Mode:         stringArg(args, "mode"),
		Query:        stringArg(args, "query"),
		Limit:        intArg(args, "limit"),
		IncludeStale: boolArg(args, "include_stale"),
	}
	return s.handleGraphQueryTool(ctx, input)
}

func (s *Server) handleGraphQueryTool(ctx context.Context, input GraphQueryInput) (GraphQueryOutput, error) {
	s.mu.RLock()
	service := s.graphQuery
	defaultProjectID := s.projectID
	s.mu.RUnlock()

	if input.ProjectID == "" {
		input.ProjectID = defaultProjectID
	}
	if service == nil {
		if input.Mode == "" {
			input.Mode = graph.QueryModeFindReferences
		}
		return GraphQueryOutput{
			Available: false,
			Status:    graph.GraphStatusUnavailable,
			Degraded:  true,
			Mode:      input.Mode,
			Query:     input.Query,
			Warnings: []graph.StatusWarning{{
				Code:    graph.WarningGraphUnavailable,
				Message: "graph.query is unavailable because no graph repository is configured",
			}},
		}, nil
	}

	response, err := service.Query(ctx, graph.QueryRequest{
		ProjectID:    input.ProjectID,
		Mode:         input.Mode,
		Query:        input.Query,
		Limit:        input.Limit,
		IncludeStale: input.IncludeStale,
	})
	if err != nil {
		return GraphQueryOutput{}, NewInvalidParamsError(err.Error())
	}
	return graphQueryOutput(response), nil
}

func (s *Server) mcpGraphQueryHandler(ctx context.Context, _ *mcp.CallToolRequest, input GraphQueryInput) (
	*mcp.CallToolResult,
	GraphQueryOutput,
	error,
) {
	output, err := s.handleGraphQueryTool(ctx, input)
	if err != nil {
		return nil, GraphQueryOutput{}, err
	}
	return nil, output, nil
}

func graphQueryOutput(response graph.QueryResponse) GraphQueryOutput {
	return GraphQueryOutput{
		Available: response.Status != graph.GraphStatusUnavailable &&
			response.Status != graph.GraphStatusIncompatible &&
			response.Status != graph.GraphStatusEmpty,
		Status:   response.Status,
		Degraded: response.Degraded,
		Mode:     response.Mode,
		Query:    response.Query,
		Results:  response.Results,
		Warnings: response.Warnings,
	}
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	value, ok := args[key].(string)
	if !ok {
		return ""
	}
	return value
}

func intArg(args map[string]any, key string) int {
	if args == nil {
		return 0
	}
	switch value := args[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func boolArg(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	value, ok := args[key].(bool)
	return ok && value
}
