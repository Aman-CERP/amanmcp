package graph

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultExpandContextLimit = 10
	defaultExpandContextDepth = 2
)

// ExpandContextRequest is the graph-layer contract for neighborhood expansion.
type ExpandContextRequest struct {
	ProjectID    string
	Seed         string
	SeedType     string
	Limit        int
	Depth        int
	IncludeStale bool
}

// ExpandContextResponse is the role-labeled context pack envelope.
type ExpandContextResponse struct {
	Available  bool            `json:"available"`
	Status     GraphStatus     `json:"status"`
	Degraded   bool            `json:"degraded"`
	Seed       string          `json:"seed"`
	Resolution string          `json:"resolution,omitempty"`
	Pack       []PackItem      `json:"pack,omitempty"`
	Candidates []Candidate     `json:"candidates,omitempty"`
	Warnings   []StatusWarning `json:"warnings,omitempty"`
}

// PackItem is one graph-expanded context item before MCP content hydration.
type PackItem struct {
	NodeID               string           `json:"node_id"`
	NodeKind             NodeKind         `json:"node_kind"`
	Name                 string           `json:"name,omitempty"`
	SourcePath           string           `json:"source_path,omitempty"`
	StartLine            int              `json:"start_line,omitempty"`
	EndLine              int              `json:"end_line,omitempty"`
	Roles                []RoleAssignment `json:"roles"`
	Relation             EdgeKind         `json:"relation"`
	Confidence           float64          `json:"confidence"`
	ConfidenceLabel      ConfidenceLabel  `json:"confidence_label"`
	Heuristic            bool             `json:"heuristic,omitempty"`
	EvidenceMethod       string           `json:"evidence_method,omitempty"`
	Path                 GraphPath        `json:"path"`
	ContentRef           string           `json:"content_ref,omitempty"`
	AdditionalPathsCount int              `json:"additional_paths_count,omitempty"`
	AdditionalRelations  []EdgeKind       `json:"additional_relations,omitempty"`
}

// ExpandContext resolves a seed and expands its graph neighborhood into a pack.
func (s *QueryService) ExpandContext(ctx context.Context, req ExpandContextRequest) (ExpandContextResponse, error) {
	if s == nil || s.repo == nil {
		return ExpandContextResponse{}, fmt.Errorf("graph repository is required")
	}
	normalized, err := s.validateExpandContextRequest(req)
	if err != nil {
		return ExpandContextResponse{}, err
	}

	snapshot, err := s.repo.Snapshot(ctx, StatusOptions{
		ProjectID:  normalized.ProjectID,
		Now:        s.now(),
		StaleAfter: s.staleAfter,
	})
	if err != nil {
		return ExpandContextResponse{}, fmt.Errorf("read graph status: %w", err)
	}
	response := ExpandContextResponse{
		Available: QueryAvailable(snapshot.Status),
		Status:    snapshot.Status,
		Degraded:  graphStatusDegraded(snapshot.Status),
		Seed:      normalized.Seed,
		Warnings:  append([]StatusWarning(nil), snapshot.Warnings...),
	}
	if !graphStatusUsable(snapshot.Status) {
		if len(response.Warnings) == 0 {
			response.Warnings = append(response.Warnings, StatusWarning{
				Code:    WarningGraphUnavailable,
				Message: fmt.Sprintf("expand_context skipped because graph status is %s", snapshot.Status),
			})
		}
		return response, nil
	}

	nodes, err := s.repo.ListNodes(ctx, NodeQuery{ProjectID: normalized.ProjectID})
	if err != nil {
		return ExpandContextResponse{}, fmt.Errorf("list graph nodes: %w", err)
	}
	edges, err := s.repo.ListEdges(ctx, EdgeQuery{ProjectID: normalized.ProjectID, ExcludeStale: !normalized.IncludeStale})
	if err != nil {
		return ExpandContextResponse{}, fmt.Errorf("list graph edges: %w", err)
	}

	resolution := resolveSubjectByType(nodes, normalized.Seed, QueryModeFindReferences, normalized.SeedType, resolveOptions{
		CandidateLimit: defaultGraphQueryLimit,
		HintLimit:      defaultSubjectHintLimit,
	})
	response.Resolution = resolution.Outcome
	if resolution.Outcome != ResolutionResolved {
		if normalized.SeedType == SubjectTypeResultID && resolution.Outcome == ResolutionSubjectNotFound {
			response.Warnings = append(response.Warnings, StatusWarning{
				Code: WarningCode("graph_result_id_not_found"),
				Message: fmt.Sprintf("result_id %q is not in graph; v1 accepts stable graph node ids, not search-result hashes",
					normalized.Seed),
			})
		}
		response.Candidates = resolution.Candidates
		if resolution.CandidatesTotal > len(resolution.Candidates) {
			response.Warnings = append(response.Warnings, StatusWarning{
				Code: WarningCode("graph_candidates_truncated"),
				Message: fmt.Sprintf("seed %q matched %d candidates; showing the top %d — narrow the seed",
					normalized.Seed, resolution.CandidatesTotal, len(resolution.Candidates)),
			})
		}
		return response, nil
	}

	edges = mergeCompetingEdges(edges)
	nodeIndex := nodesByID(nodes)
	results, expandWarnings, err := collectExpandResults(normalized, resolution.Seeds, edges, nodeIndex)
	if err != nil {
		return ExpandContextResponse{}, err
	}
	response.Warnings = append(response.Warnings, expandWarnings...)
	results = deduplicateResultsByTarget(results)
	sort.Slice(results, func(i, j int) bool {
		if results[i].Confidence != results[j].Confidence {
			return results[i].Confidence > results[j].Confidence
		}
		if results[i].SourcePath != results[j].SourcePath {
			return results[i].SourcePath < results[j].SourcePath
		}
		return results[i].NodeID < results[j].NodeID
	})
	if normalized.Limit > 0 && len(results) > normalized.Limit {
		results = results[:normalized.Limit]
		response.Warnings = append(response.Warnings, StatusWarning{
			Code:    WarningTraversalBudgetExhausted,
			Message: fmt.Sprintf("expand_context truncated pack to %d items", normalized.Limit),
		})
	}
	response.Pack = packItemsFromResults(results, nodeIndex)
	return response, nil
}

func (s *QueryService) validateExpandContextRequest(req ExpandContextRequest) (ExpandContextRequest, error) {
	projectID, err := validateRequiredSearchText("project_id", req.ProjectID, s.maxQueryLength)
	if err != nil {
		return ExpandContextRequest{}, fmt.Errorf("%w: %w", ErrInvalidQueryParams, err)
	}
	seed, err := validateRequiredSearchText("seed", req.Seed, s.maxQueryLength)
	if err != nil {
		return ExpandContextRequest{}, fmt.Errorf("%w: %w", ErrInvalidQueryParams, err)
	}
	if filepath.IsAbs(seed) || strings.Contains(filepath.ToSlash(seed), "../") {
		return ExpandContextRequest{}, fmt.Errorf("seed must be project-relative and safe: %w", ErrInvalidQueryParams)
	}
	switch normalizeExpandSeedType(req.SeedType) {
	case SubjectTypeAuto, SubjectTypePath, SubjectTypeSymbol, SubjectTypeResultID:
	default:
		return ExpandContextRequest{}, fmt.Errorf("unsupported expand_context seed_type %q: %w", req.SeedType, ErrInvalidQueryParams)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = defaultExpandContextLimit
	}
	if limit > s.maxLimit {
		return ExpandContextRequest{}, fmt.Errorf("limit must be <= %d: %w", s.maxLimit, ErrInvalidQueryParams)
	}
	depth := req.Depth
	if depth <= 0 {
		depth = defaultExpandContextDepth
	}
	if depth > s.maxDepth {
		return ExpandContextRequest{}, fmt.Errorf("depth must be <= %d: %w", s.maxDepth, ErrInvalidQueryParams)
	}
	req.ProjectID = projectID
	req.Seed = seed
	req.SeedType = normalizeExpandSeedType(req.SeedType)
	req.Limit = limit
	req.Depth = depth
	return req, nil
}

func normalizeExpandSeedType(seedType string) string {
	seedType = strings.TrimSpace(seedType)
	if seedType == "" {
		return SubjectTypeAuto
	}
	return seedType
}

func collectExpandResults(
	req ExpandContextRequest,
	seeds []Node,
	edges []Edge,
	nodes map[string]Node,
) ([]QueryResult, []StatusWarning, error) {
	results, nodesExhausted := relatedResults(nodes, edges, seeds, QueryModeFindReferences, req.Limit)
	var warnings []StatusWarning
	if nodesExhausted {
		warnings = append(warnings, newTraversalBudgetWarning(TraversalBudgetNodes, req.Limit))
	}
	if req.Depth <= 1 {
		return results, warnings, nil
	}

	index, err := newGraphIndex(nodesSlice(nodes), edges)
	if err != nil {
		return nil, warnings, fmt.Errorf("build expand index: %w", err)
	}

	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		seen[result.NodeID] = struct{}{}
	}

	remainingBudget := req.Limit - len(seen)
	if remainingBudget < 0 {
		remainingBudget = 0
	}
	multiHopResults, multiHopExhausted := collectMultiHopResults(index, seeds, req.Depth, remainingBudget, seen)
	results = append(results, multiHopResults...)
	if multiHopExhausted {
		warnings = append(warnings, newTraversalBudgetWarning(TraversalBudgetNodes, req.Limit))
	}
	return results, warnings, nil
}

func collectMultiHopResults(
	index *graphIndex,
	seeds []Node,
	maxDepth int,
	budget int,
	packSeen map[string]struct{},
) ([]QueryResult, bool) {
	if index == nil || maxDepth <= 1 || budget <= 0 {
		return nil, false
	}
	var results []QueryResult
	exhausted := false
	for _, seed := range seeds {
		type state struct {
			node Node
			path []adjacency
		}
		queue := []state{{node: seed}}
		visited := map[string]struct{}{seed.ID: {}}
		for len(queue) > 0 && len(results) < budget {
			current := queue[0]
			queue = queue[1:]
			if len(current.path) >= maxDepth {
				continue
			}
			neighborBudgetHit := false
			for _, next := range index.neighbors[current.node.ID] {
				if next.node.ID == seed.ID {
					continue
				}
				pathToNext := appendPath(current.path, next)
				if len(pathToNext) >= 2 {
					if _, ok := packSeen[next.node.ID]; !ok {
						if len(results) >= budget {
							exhausted = true
							neighborBudgetHit = true
							break
						}
						results = append(results, queryResultFromAdjacencyPath(seed, pathToNext))
						packSeen[next.node.ID] = struct{}{}
					}
				}
				if neighborBudgetHit {
					break
				}
				if _, ok := visited[next.node.ID]; ok {
					continue
				}
				visited[next.node.ID] = struct{}{}
				queue = append(queue, state{node: next.node, path: pathToNext})
			}
			if neighborBudgetHit {
				break
			}
		}
		if len(queue) > 0 && len(results) >= budget {
			exhausted = true
		}
	}
	return results, exhausted
}

func queryResultFromAdjacencyPath(seed Node, path []adjacency) QueryResult {
	last := path[len(path)-1]
	target := last.node
	edge := last.edge
	return QueryResult{
		NodeID:          target.ID,
		NodeKind:        target.Kind,
		Name:            target.Name,
		SourcePath:      target.SourcePath,
		Role:            roleFor(edge.Kind, QueryModeFindReferences),
		Relation:        edge.Kind,
		Confidence:      edge.Confidence,
		ConfidenceLabel: edge.ConfidenceLabel,
		Heuristic:       edge.Evidence.Heuristic,
		EvidenceMethod:  edge.Evidence.Method,
		EvidenceSnippet: safeEvidenceSnippet(edge.Evidence.Snippet),
		Stale:           edge.Stale,
		Path: GraphPath{
			From:            nodeEvidence(seed),
			To:              nodeEvidence(target),
			Hops:            pathHopEvidence(seed, path),
			Explanation:     graphPathExplanation(seed, path),
			ConfidenceLabel: pathConfidenceLabel(path),
		},
	}
}

func nodesSlice(nodes map[string]Node) []Node {
	out := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node)
	}
	sort.Slice(out, func(i, j int) bool {
		return nodeSortKey(out[i]) < nodeSortKey(out[j])
	})
	return out
}

func packItemsFromResults(results []QueryResult, nodes map[string]Node) []PackItem {
	pack := make([]PackItem, 0, len(results))
	for _, result := range results {
		target, ok := nodes[result.NodeID]
		if !ok {
			target = Node{
				ID:         result.NodeID,
				Kind:       result.NodeKind,
				Name:       result.Name,
				SourcePath: result.SourcePath,
			}
		}
		seedID := result.Path.From.ID
		directedHops := hopsToPathInput(seedID, result.Path)
		lastEdge := directedEdgeFromPath(directedHops, result)
		seedNode, ok := nodes[seedID]
		if !ok {
			seedNode = Node{ID: seedID, Kind: NodeKind(result.Path.From.Kind)}
		}
		pack = append(pack, PackItem{
			NodeID:               result.NodeID,
			NodeKind:             target.Kind,
			Name:                 target.Name,
			SourcePath:           target.SourcePath,
			StartLine:            target.StartLine,
			EndLine:              target.EndLine,
			Roles: ClassifyContextRoles(ClassifyContextInput{
				SeedID:         seedID,
				ImportAnchorID: importAnchorID(seedNode, directedHops),
				Target:         target,
				LastEdge:       lastEdge,
				Path:           directedHops,
			}),
			Relation:             result.Relation,
			Confidence:           result.Confidence,
			ConfidenceLabel:      result.ConfidenceLabel,
			Heuristic:            result.Heuristic,
			EvidenceMethod:       result.EvidenceMethod,
			Path:                 result.Path,
			ContentRef:           contentRefForNode(target),
			AdditionalPathsCount: result.AdditionalPathsCount,
			AdditionalRelations:  result.AdditionalRelations,
		})
	}
	return pack
}

func directedEdgeFromPath(hops []PathHop, result QueryResult) Edge {
	if len(hops) > 0 {
		return hops[len(hops)-1].Edge
	}
	return Edge{
		Kind:            result.Relation,
		Confidence:      result.Confidence,
		ConfidenceLabel: result.ConfidenceLabel,
		Evidence: Evidence{
			Method:    result.EvidenceMethod,
			Snippet:   result.EvidenceSnippet,
			Heuristic: result.Heuristic,
		},
	}
}

func hopsToPathInput(seedID string, path GraphPath) []PathHop {
	hops := make([]PathHop, 0, len(path.Hops))
	fromID := path.From.ID
	if fromID == "" {
		fromID = seedID
	}
	for _, hop := range path.Hops {
		edgeFrom := hop.EdgeFromNodeID
		edgeTo := hop.EdgeToNodeID
		if edgeFrom == "" || edgeTo == "" {
			edgeTo = hop.Node.ID
			edgeFrom = fromID
		}
		hops = append(hops, PathHop{Edge: Edge{
			Kind:            EdgeKind(hop.Relation),
			FromNodeID:      edgeFrom,
			ToNodeID:        edgeTo,
			SourcePath:      hop.EdgeSourcePath,
			ConfidenceLabel: hop.ConfidenceLabel,
			Evidence:        hop.EdgeEvidence,
			Confidence:      edgeConfidenceFromLabel(hop.ConfidenceLabel),
		}})
		fromID = hop.Node.ID
	}
	return hops
}

func contentRefForNode(node Node) string {
	if node.Kind == NodeKindChunk && node.Key != "" {
		return node.Key
	}
	return ""
}

func edgeConfidenceFromLabel(label ConfidenceLabel) float64 {
	switch label {
	case ConfidenceExact:
		return 1
	case ConfidenceHigh:
		return 0.9
	case ConfidenceMedium:
		return 0.7
	case ConfidenceLow:
		return 0.5
	default:
		return 0
	}
}