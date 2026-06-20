package graph

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	QueryModeFindReferences = "find_references"
	QueryModeExplainSymbol  = "explain_symbol"
	QueryModeImpactAnalysis = "impact_analysis"

	defaultGraphQueryLimit  = 10
	maxGraphQueryLimit      = 50
	defaultGraphQueryDepth  = 1
	defaultMaxGraphDepth    = 5
	defaultMaxQueryByteSize = 512
)

// QueryService provides bounded graph relationship queries over Repository.
type QueryService struct {
	repo           Repository
	staleAfter     time.Duration
	now            func() time.Time
	maxLimit       int
	maxDepth       int
	maxQueryLength int
}

// NewQueryService constructs a graph query service.
func NewQueryService(repo Repository, opts QueryServiceOptions) *QueryService {
	if opts.StaleAfter <= 0 {
		opts.StaleAfter = DefaultStaleAfter
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	if opts.MaxLimit <= 0 {
		opts.MaxLimit = maxGraphQueryLimit
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = defaultMaxGraphDepth
	}
	if opts.MaxQueryLength <= 0 {
		opts.MaxQueryLength = defaultMaxQueryByteSize
	}
	return &QueryService{
		repo:           repo,
		staleAfter:     opts.StaleAfter,
		now:            opts.Now,
		maxLimit:       opts.MaxLimit,
		maxDepth:       opts.MaxDepth,
		maxQueryLength: opts.MaxQueryLength,
	}
}

// QueryServiceOptions controls graph query status checks.
type QueryServiceOptions struct {
	StaleAfter     time.Duration
	Now            func() time.Time
	MaxLimit       int
	MaxDepth       int
	MaxQueryLength int
}

// QueryRequest is the input contract for graph relationship queries.
type QueryRequest struct {
	ProjectID    string
	Mode         string
	Query        string
	Limit        int
	IncludeStale bool
}

// QueryResponse is the compact graph evidence envelope.
type QueryResponse struct {
	Status   GraphStatus     `json:"status"`
	Degraded bool            `json:"degraded"`
	Mode     string          `json:"mode"`
	Query    string          `json:"query"`
	Results  []QueryResult   `json:"results,omitempty"`
	Warnings []StatusWarning `json:"warnings,omitempty"`
}

// QueryResult is one graph-backed relationship result.
type QueryResult struct {
	NodeID          string          `json:"node_id"`
	NodeKind        NodeKind        `json:"node_kind"`
	Name            string          `json:"name,omitempty"`
	SourcePath      string          `json:"source_path,omitempty"`
	Role            string          `json:"role"`
	Relation        EdgeKind        `json:"relation"`
	Confidence      float64         `json:"confidence"`
	ConfidenceLabel ConfidenceLabel `json:"confidence_label"`
	EvidenceMethod  string          `json:"evidence_method"`
	EvidenceSnippet string          `json:"evidence_snippet,omitempty"`
	Stale           bool            `json:"stale,omitempty"`
	GraphPath       []string        `json:"graph_path"`
}

// Query executes a bounded graph relationship query.
func (s *QueryService) Query(ctx context.Context, req QueryRequest) (QueryResponse, error) {
	if s == nil || s.repo == nil {
		return QueryResponse{}, fmt.Errorf("graph repository is required")
	}
	req.Mode = normalizeQueryMode(req.Mode)
	req.Query = strings.TrimSpace(req.Query)
	if err := validateQueryRequest(req); err != nil {
		return QueryResponse{}, err
	}
	if req.Limit == 0 {
		req.Limit = defaultGraphQueryLimit
	}

	snapshot, err := s.repo.Snapshot(ctx, StatusOptions{
		ProjectID:  req.ProjectID,
		Now:        s.now(),
		StaleAfter: s.staleAfter,
	})
	if err != nil {
		return QueryResponse{}, fmt.Errorf("read graph status: %w", err)
	}
	response := QueryResponse{
		Status:   snapshot.Status,
		Degraded: graphStatusDegraded(snapshot.Status),
		Mode:     req.Mode,
		Query:    req.Query,
		Warnings: append([]StatusWarning(nil), snapshot.Warnings...),
	}
	if !graphStatusUsable(snapshot.Status) {
		if len(response.Warnings) == 0 {
			response.Warnings = append(response.Warnings, StatusWarning{
				Code:    WarningGraphUnavailable,
				Message: fmt.Sprintf("graph query skipped because graph status is %s", snapshot.Status),
			})
		}
		return response, nil
	}

	nodes, err := s.repo.ListNodes(ctx, NodeQuery{ProjectID: req.ProjectID})
	if err != nil {
		return QueryResponse{}, fmt.Errorf("list graph nodes: %w", err)
	}
	edges, err := s.repo.ListEdges(ctx, EdgeQuery{ProjectID: req.ProjectID, ExcludeStale: !req.IncludeStale})
	if err != nil {
		return QueryResponse{}, fmt.Errorf("list graph edges: %w", err)
	}

	seeds := matchingNodes(nodes, req.Query, req.Mode)
	if len(seeds) == 0 {
		return response, nil
	}
	results := relatedResults(nodesByID(nodes), edges, seeds, req.Mode)
	sort.Slice(results, func(i, j int) bool {
		if results[i].Confidence != results[j].Confidence {
			return results[i].Confidence > results[j].Confidence
		}
		if results[i].SourcePath != results[j].SourcePath {
			return results[i].SourcePath < results[j].SourcePath
		}
		return results[i].NodeID < results[j].NodeID
	})
	if len(results) > req.Limit {
		results = results[:req.Limit]
		response.Warnings = append(response.Warnings, StatusWarning{
			Code:    WarningCode("graph_results_truncated"),
			Message: fmt.Sprintf("graph results truncated to limit %d", req.Limit),
		})
	}
	response.Results = results
	return response, nil
}

func validateQueryRequest(req QueryRequest) error {
	if strings.TrimSpace(req.ProjectID) == "" {
		return fmt.Errorf("project_id is required")
	}
	switch req.Mode {
	case QueryModeFindReferences, QueryModeExplainSymbol, QueryModeImpactAnalysis:
	default:
		return fmt.Errorf("unsupported graph query mode %q", req.Mode)
	}
	if req.Query == "" {
		return fmt.Errorf("query is required")
	}
	if strings.Contains(req.Query, "\x00") {
		return fmt.Errorf("query contains unsafe NUL byte")
	}
	if filepath.IsAbs(req.Query) || strings.Contains(filepath.ToSlash(req.Query), "../") {
		return fmt.Errorf("query must be project-relative and safe")
	}
	if req.Limit < 0 || req.Limit > maxGraphQueryLimit {
		return fmt.Errorf("limit must be between 0 and %d", maxGraphQueryLimit)
	}
	return nil
}

func normalizeQueryMode(mode string) string {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return QueryModeFindReferences
	}
	return mode
}

func graphStatusUsable(status GraphStatus) bool {
	return status == GraphStatusFresh || status == GraphStatusStale || status == GraphStatusPartial
}

func graphStatusDegraded(status GraphStatus) bool {
	return status != GraphStatusFresh
}

func nodesByID(nodes []Node) map[string]Node {
	byID := make(map[string]Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	return byID
}

func matchingNodes(nodes []Node, query string, mode string) []Node {
	queryLower := strings.ToLower(query)
	var matches []Node
	for _, node := range nodes {
		if mode == QueryModeExplainSymbol && node.Kind != NodeKindSymbol {
			continue
		}
		if strings.Contains(strings.ToLower(node.Key), queryLower) ||
			strings.Contains(strings.ToLower(node.Name), queryLower) ||
			strings.Contains(strings.ToLower(node.SourcePath), queryLower) {
			matches = append(matches, node)
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].ID < matches[j].ID })
	return matches
}

func relatedResults(nodes map[string]Node, edges []Edge, seeds []Node, mode string) []QueryResult {
	seedIDs := make(map[string]Node, len(seeds))
	for _, seed := range seeds {
		seedIDs[seed.ID] = seed
	}
	seen := map[string]bool{}
	var results []QueryResult
	for _, edge := range edges {
		seed, relatedID, ok := relationshipForMode(edge, seedIDs, mode)
		if !ok {
			continue
		}
		related, ok := nodes[relatedID]
		if !ok {
			continue
		}
		key := string(edge.Kind) + "|" + related.ID + "|" + seed.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		sourcePath := related.SourcePath
		if edge.SourcePath != "" {
			sourcePath = edge.SourcePath
		}
		results = append(results, QueryResult{
			NodeID:          related.ID,
			NodeKind:        related.Kind,
			Name:            related.Name,
			SourcePath:      sourcePath,
			Role:            roleFor(edge.Kind, mode),
			Relation:        edge.Kind,
			Confidence:      edge.Confidence,
			ConfidenceLabel: edge.ConfidenceLabel,
			EvidenceMethod:  edge.Evidence.Method,
			EvidenceSnippet: safeEvidenceSnippet(edge.Evidence.Snippet),
			Stale:           edge.Stale,
			GraphPath:       []string{seed.ID, string(edge.Kind), related.ID},
		})
	}
	return results
}

func relationshipForMode(edge Edge, seeds map[string]Node, mode string) (Node, string, bool) {
	if seed, ok := seeds[edge.FromNodeID]; ok {
		return seed, edge.ToNodeID, true
	}
	if mode != QueryModeImpactAnalysis {
		if seed, ok := seeds[edge.ToNodeID]; ok {
			return seed, edge.FromNodeID, true
		}
	}
	return Node{}, "", false
}

func roleFor(kind EdgeKind, mode string) string {
	switch mode {
	case QueryModeImpactAnalysis:
		return "downstream"
	case QueryModeExplainSymbol:
		return "symbol_context"
	default:
		switch kind {
		case EdgeKindTestCoversImplementation:
			return "test_or_implementation"
		case EdgeKindDocMentionsPath, EdgeKindDocMentionsFile, EdgeKindDocMentionsSymbol, EdgeKindDocMentionsConfigKey:
			return "documented_reference"
		default:
			return "related"
		}
	}
}

func safeEvidenceSnippet(snippet string) string {
	snippet = strings.TrimSpace(snippet)
	if len(snippet) > 160 {
		return snippet[:160]
	}
	return snippet
}
