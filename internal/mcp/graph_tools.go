package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Aman-CERP/amanmcp/internal/graph"
)

type GraphQueryInput struct {
	ProjectID    string `json:"project_id,omitempty" jsonschema:"AmanMCP project id; defaults to the server project when available"`
	Mode         string `json:"mode,omitempty" jsonschema:"graph query mode: find_references, explain_symbol, or impact_analysis"`
	Query        string `json:"query" jsonschema:"subject value to query: symbol, project-relative path, package name/key, or stable graph node id"`
	SubjectType  string `json:"subject_type,omitempty" jsonschema:"subject resolver type: auto (default), path, symbol, package, or result_id"`
	Limit            int  `json:"limit,omitempty" jsonschema:"maximum number of graph evidence results, default 10, maximum 50"`
	IncludeStale     bool `json:"include_stale,omitempty" jsonschema:"include stale graph edges that point at deleted or replaced sources"`
	MaxNodes         *int `json:"max_nodes,omitempty" jsonschema:"optional traversal budget override for post-resolution node expansion"`
	MaxPerEdgeKind   *int `json:"max_per_edge_kind,omitempty" jsonschema:"optional traversal budget override for results per edge kind"`
	MaxTokens        *int `json:"max_tokens,omitempty" jsonschema:"optional traversal budget override for serialized result size in bytes"`
	MaxDepth         *int `json:"max_depth,omitempty" jsonschema:"optional traversal depth override for the multi-hop engine; inert for single-hop graph.query"`
}

type GraphQueryOutput = graph.QueryToolOutput

func (s *Server) handleGraphQueryArgs(ctx context.Context, args map[string]any) (GraphQueryOutput, error) {
	input := GraphQueryInput{
		ProjectID:      stringArg(args, "project_id"),
		Mode:           stringArg(args, "mode"),
		Query:          stringArg(args, "query"),
		SubjectType:    stringArg(args, "subject_type"),
		Limit:          intArg(args, "limit"),
		IncludeStale:   boolArg(args, "include_stale"),
		MaxNodes:       intPtrArg(args, "max_nodes"),
		MaxPerEdgeKind: intPtrArg(args, "max_per_edge_kind"),
		MaxTokens:      intPtrArg(args, "max_tokens"),
		MaxDepth:       intPtrArg(args, "max_depth"),
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
		return graph.NewUnavailableQueryToolOutput(
			input.Mode,
			input.Query,
			"graph.query is unavailable because no graph repository is configured",
		), nil
	}

	response, err := service.Query(ctx, graph.QueryRequest{
		ProjectID:    input.ProjectID,
		Mode:         input.Mode,
		Query:        input.Query,
		SubjectType:  input.SubjectType,
		Limit:        input.Limit,
		IncludeStale: input.IncludeStale,
		BudgetOverrides: graph.TraversalBudgetOverrides{
			MaxNodes:       input.MaxNodes,
			MaxPerEdgeKind: input.MaxPerEdgeKind,
			MaxTokens:      input.MaxTokens,
			MaxDepth:         input.MaxDepth,
		},
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
	return graph.NewQueryToolOutput(response)
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

func intPtrArg(args map[string]any, key string) *int {
	if args == nil {
		return nil
	}
	raw, ok := args[key]
	if !ok {
		return nil
	}
	switch value := raw.(type) {
	case int:
		return &value
	case int64:
		v := int(value)
		return &v
	case float64:
		v := int(value)
		return &v
	default:
		return nil
	}
}
