package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Aman-CERP/amanmcp/internal/graph"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

// ExpandContextInput is the MCP contract for graph-native context expansion.
type ExpandContextInput struct {
	ProjectID    string `json:"project_id,omitempty" jsonschema:"AmanMCP project id; defaults to the server project when available"`
	Seed         string `json:"seed" jsonschema:"seed value to expand: symbol, project-relative path, or stable graph node id"`
	SeedType     string `json:"seed_type,omitempty" jsonschema:"seed resolver type: auto (default), path, symbol, or result_id"`
	Limit        int    `json:"limit,omitempty" jsonschema:"maximum number of context pack items, default 10"`
	Depth        int    `json:"depth,omitempty" jsonschema:"graph expansion depth for multi-hop neighborhood traversal, default 2"`
	IncludeStale bool   `json:"include_stale,omitempty" jsonschema:"include stale graph edges that point at deleted or replaced sources"`
}

// ContextItem is one role-labeled, graph-path-backed context pack entry.
type ContextItem struct {
	NodeID               string                 `json:"node_id"`
	NodeKind             graph.NodeKind         `json:"node_kind"`
	Name                 string                 `json:"name,omitempty"`
	SourcePath           string                 `json:"source_path,omitempty"`
	StartLine            int                    `json:"start_line,omitempty"`
	EndLine              int                    `json:"end_line,omitempty"`
	Roles                []graph.RoleAssignment `json:"roles"`
	Relation             graph.EdgeKind         `json:"relation,omitempty"`
	ConfidenceLabel      graph.ConfidenceLabel  `json:"confidence_label,omitempty"`
	Path                 graph.GraphPath        `json:"path"`
	Content              string                 `json:"content,omitempty"`
	ContentRef           string                 `json:"content_ref,omitempty"`
	AdditionalPathsCount int                    `json:"additional_paths_count,omitempty"`
	AdditionalRelations  []graph.EdgeKind       `json:"additional_relations,omitempty"`
}

// ExpandContextOutput is the typed expand_context tool envelope.
type ExpandContextOutput struct {
	Available  bool                   `json:"available"`
	Status     graph.GraphStatus      `json:"status"`
	Degraded   bool                   `json:"degraded"`
	Seed       string                 `json:"seed"`
	Resolution string                 `json:"resolution,omitempty"`
	Pack       []ContextItem          `json:"pack,omitempty"`
	Candidates []graph.Candidate      `json:"candidates,omitempty"`
	Warnings   []graph.StatusWarning  `json:"warnings,omitempty"`
}

func (s *Server) handleExpandContextArgs(ctx context.Context, args map[string]any) (ExpandContextOutput, error) {
	input := ExpandContextInput{
		ProjectID:    stringArg(args, "project_id"),
		Seed:         stringArg(args, "seed"),
		SeedType:     stringArg(args, "seed_type"),
		Limit:        intArg(args, "limit"),
		Depth:        intArg(args, "depth"),
		IncludeStale: boolArg(args, "include_stale"),
	}
	return s.handleExpandContextTool(ctx, input)
}

func (s *Server) handleExpandContextTool(ctx context.Context, input ExpandContextInput) (ExpandContextOutput, error) {
	s.mu.RLock()
	service := s.graphQuery
	metadata := s.metadata
	defaultProjectID := s.projectID
	s.mu.RUnlock()

	if input.ProjectID == "" {
		input.ProjectID = defaultProjectID
	}
	if service == nil {
		return NewUnavailableExpandContextOutput(
			input.Seed,
			"expand_context is unavailable because no graph repository is configured",
		), nil
	}

	response, err := service.ExpandContext(ctx, graph.ExpandContextRequest{
		ProjectID:    input.ProjectID,
		Seed:         input.Seed,
		SeedType:     input.SeedType,
		Limit:        input.Limit,
		Depth:        input.Depth,
		IncludeStale: input.IncludeStale,
	})
	if err != nil {
		return ExpandContextOutput{}, NewInvalidParamsError(err.Error())
	}
	return s.hydrateExpandContextOutput(ctx, NewExpandContextOutput(response), metadata), nil
}

func (s *Server) mcpExpandContextHandler(ctx context.Context, _ *mcp.CallToolRequest, input ExpandContextInput) (
	*mcp.CallToolResult,
	ExpandContextOutput,
	error,
) {
	output, err := s.handleExpandContextTool(ctx, input)
	if err != nil {
		return nil, ExpandContextOutput{}, err
	}
	return nil, output, nil
}

// NewExpandContextOutput projects a graph expand response into the MCP envelope.
func NewExpandContextOutput(response graph.ExpandContextResponse) ExpandContextOutput {
	pack := make([]ContextItem, 0, len(response.Pack))
	for _, item := range response.Pack {
		pack = append(pack, ContextItem{
			NodeID:               item.NodeID,
			NodeKind:             item.NodeKind,
			Name:                 item.Name,
			SourcePath:           item.SourcePath,
			StartLine:            item.StartLine,
			EndLine:              item.EndLine,
			Roles:                item.Roles,
			Relation:             item.Relation,
			ConfidenceLabel:      item.ConfidenceLabel,
			Path:                 item.Path,
			ContentRef:           item.ContentRef,
			AdditionalPathsCount: item.AdditionalPathsCount,
			AdditionalRelations:  item.AdditionalRelations,
		})
	}
	return ExpandContextOutput{
		Available:  response.Available,
		Status:     response.Status,
		Degraded:   response.Degraded,
		Seed:       response.Seed,
		Resolution: response.Resolution,
		Pack:       pack,
		Candidates: response.Candidates,
		Warnings:   response.Warnings,
	}
}

// NewUnavailableExpandContextOutput returns the graceful expand_context envelope.
func NewUnavailableExpandContextOutput(seed, message string) ExpandContextOutput {
	return ExpandContextOutput{
		Available: false,
		Status:    graph.GraphStatusUnavailable,
		Degraded:  true,
		Seed:      seed,
		Warnings: []graph.StatusWarning{{
			Code:    graph.WarningGraphUnavailable,
			Message: message,
		}},
	}
}

func (s *Server) hydrateExpandContextOutput(ctx context.Context, output ExpandContextOutput, metadata store.MetadataStore) ExpandContextOutput {
	if metadata == nil || len(output.Pack) == 0 {
		return output
	}
	for i := range output.Pack {
		item := &output.Pack[i]
		if item.Content != "" {
			continue
		}
		if item.ContentRef != "" {
			chunk, err := metadata.GetChunk(ctx, item.ContentRef)
			if err == nil && chunk != nil {
				item.Content = chunk.Content
				continue
			}
		}
		if item.SourcePath == "" {
			continue
		}
		chunks, err := metadata.GetChunksByPath(ctx, item.SourcePath, 0)
		if err != nil || len(chunks) == 0 {
			continue
		}
		chunk := bestChunkForItem(chunks, item.StartLine, item.EndLine)
		if chunk == nil {
			continue
		}
		item.ContentRef = chunk.ID
		item.Content = chunk.Content
	}
	return output
}

func bestChunkForItem(chunks []*store.Chunk, startLine, endLine int) *store.Chunk {
	if len(chunks) == 0 {
		return nil
	}
	if startLine <= 0 && endLine <= 0 {
		return chunks[0]
	}
	var best *store.Chunk
	bestOverlap := -1
	for _, chunk := range chunks {
		if chunk == nil {
			continue
		}
		overlap := lineOverlap(startLine, endLine, chunk.StartLine, chunk.EndLine)
		if overlap > bestOverlap {
			bestOverlap = overlap
			best = chunk
		}
	}
	if best != nil {
		return best
	}
	return chunks[0]
}

func lineOverlap(aStart, aEnd, bStart, bEnd int) int {
	if aStart <= 0 {
		aStart = aEnd
	}
	if aEnd <= 0 {
		aEnd = aStart
	}
	if aStart <= 0 || bStart <= 0 {
		return 0
	}
	start := max(aStart, bStart)
	end := min(aEnd, bEnd)
	if end < start {
		return 0
	}
	return end - start + 1
}