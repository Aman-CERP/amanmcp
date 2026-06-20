package graph

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// GraphRole labels why a node is present in graph query output.
type GraphRole string

const (
	RoleQueryMatch         GraphRole = "query_match"
	RoleDeclaresPackage    GraphRole = "declares_package"
	RoleDeclaredBy         GraphRole = "declared_by"
	RoleImports            GraphRole = "imports"
	RoleImportedBy         GraphRole = "imported_by"
	RoleDefines            GraphRole = "defines"
	RoleDefinedBy          GraphRole = "defined_by"
	RoleHasChunk           GraphRole = "has_chunk"
	RoleChunkOf            GraphRole = "chunk_of"
	RoleDefinesConfigKey   GraphRole = "defines_config_key"
	RoleConfigKeyDefinedBy GraphRole = "config_key_defined_by"
	RoleTestCovers         GraphRole = "test_covers"
	RoleCoveredByTest      GraphRole = "covered_by_test"
	RoleMentionsPath       GraphRole = "mentions_path"
	RoleMentionedByDoc     GraphRole = "mentioned_by_doc"
	RoleRelated            GraphRole = "related"
)

// NeighborRequest asks for bounded graph neighbors around nodes matching Query.
type NeighborRequest struct {
	ProjectID  string `json:"project_id"`
	Query      string `json:"query"`
	SourcePath string `json:"source_path,omitempty"`
	Limit      int    `json:"limit"`
	Depth      int    `json:"depth"`
}

// PathRequest asks for bounded graph paths between matching nodes.
type PathRequest struct {
	ProjectID string `json:"project_id"`
	FromQuery string `json:"from_query"`
	ToQuery   string `json:"to_query"`
	Limit     int    `json:"limit"`
	Depth     int    `json:"depth"`
}

// TraversalResult is the compact status-aware graph traversal response.
type TraversalResult struct {
	ProjectID string          `json:"project_id"`
	Operation string          `json:"operation"`
	Query     string          `json:"query,omitempty"`
	Status    GraphStatus     `json:"status"`
	Available bool            `json:"available"`
	Degraded  bool            `json:"degraded"`
	Warnings  []StatusWarning `json:"warnings,omitempty"`
	Limit     int             `json:"limit"`
	Depth     int             `json:"depth"`
	Truncated bool            `json:"truncated,omitempty"`
	Evidence  []GraphEvidence `json:"evidence,omitempty"`
	Paths     []GraphPath     `json:"paths,omitempty"`
}

// GraphNodeEvidence is the compact node projection used in query evidence.
type GraphNodeEvidence struct {
	ID         string   `json:"id"`
	Kind       NodeKind `json:"kind"`
	Key        string   `json:"key"`
	Name       string   `json:"name,omitempty"`
	SourcePath string   `json:"source_path,omitempty"`
	Language   string   `json:"language,omitempty"`
	SymbolKind string   `json:"symbol_kind,omitempty"`
	StartLine  int      `json:"start_line,omitempty"`
	EndLine    int      `json:"end_line,omitempty"`
}

// GraphEvidence is one role-labeled node or edge-backed graph fact.
type GraphEvidence struct {
	Node            GraphNodeEvidence `json:"node"`
	Relation        string            `json:"relation,omitempty"`
	Role            GraphRole         `json:"role"`
	ConfidenceLabel ConfidenceLabel   `json:"confidence_label,omitempty"`
	EdgeSourcePath  string            `json:"edge_source_path,omitempty"`
	EdgeEvidence    Evidence          `json:"edge_evidence,omitempty"`
	PathExplanation string            `json:"path_explanation,omitempty"`
}

// GraphPath is a bounded path with hop-level edge evidence.
type GraphPath struct {
	From            GraphNodeEvidence `json:"from"`
	To              GraphNodeEvidence `json:"to"`
	Hops            []GraphEvidence   `json:"hops"`
	Explanation     string            `json:"explanation"`
	ConfidenceLabel ConfidenceLabel   `json:"confidence_label,omitempty"`
}

// Neighbors returns nodes adjacent to graph nodes matching req.Query.
//
// This traversal API is intentionally kept behind the package boundary for the
// Sprint 14 graph.query rollout. The exposed MCP tool uses Query() only; future
// FEAT-SYN9 work can promote Neighbors/Path to distinct tools after catalog and
// eval evidence exists.
func (s *QueryService) Neighbors(ctx context.Context, req NeighborRequest) (TraversalResult, error) {
	normalized, err := s.validateNeighborRequest(req)
	if err != nil {
		return TraversalResult{}, err
	}
	snapshot, err := s.snapshot(ctx, normalized.ProjectID)
	if err != nil {
		return TraversalResult{}, err
	}
	result := newQueryResult("neighbors", normalized.ProjectID, normalized.Query, normalized.Limit, normalized.Depth, snapshot)
	if !statusAllowsEvidence(snapshot) {
		return result, nil
	}

	index, err := s.loadGraph(ctx, normalized.ProjectID)
	if err != nil {
		return TraversalResult{}, err
	}
	seeds := index.matchNodes(normalized.Query, normalized.SourcePath)
	visited := make(map[string]struct{}, len(seeds))
	for _, seed := range seeds {
		if !appendGraphEvidence(&result, queryMatchEvidence(seed)) {
			break
		}
		visited[seed.ID] = struct{}{}
		if !s.walkNeighbors(&result, index, seed, visited, normalized.Depth) {
			break
		}
	}
	if len(seeds) > len(result.Evidence) {
		result.Truncated = true
	}
	return result, nil
}

// Path returns bounded graph paths between nodes matching req.FromQuery and req.ToQuery.
func (s *QueryService) Path(ctx context.Context, req PathRequest) (TraversalResult, error) {
	normalized, err := s.validatePathRequest(req)
	if err != nil {
		return TraversalResult{}, err
	}
	query := normalized.FromQuery + " -> " + normalized.ToQuery
	snapshot, err := s.snapshot(ctx, normalized.ProjectID)
	if err != nil {
		return TraversalResult{}, err
	}
	result := newQueryResult("path", normalized.ProjectID, query, normalized.Limit, normalized.Depth, snapshot)
	if !statusAllowsEvidence(snapshot) {
		return result, nil
	}

	index, err := s.loadGraph(ctx, normalized.ProjectID)
	if err != nil {
		return TraversalResult{}, err
	}
	fromNodes := index.matchNodes(normalized.FromQuery, "")
	toNodes := index.matchNodes(normalized.ToQuery, "")
	targetIDs := make(map[string]struct{}, len(toNodes))
	for _, node := range toNodes {
		targetIDs[node.ID] = struct{}{}
	}
	seenPaths := map[string]struct{}{}
	for _, from := range fromNodes {
		if !s.findPaths(&result, index, from, targetIDs, normalized.Depth, seenPaths) {
			break
		}
	}
	return result, nil
}

func (s *QueryService) validateNeighborRequest(req NeighborRequest) (NeighborRequest, error) {
	projectID, err := validateRequiredSearchText("project_id", req.ProjectID, s.maxQueryLength)
	if err != nil {
		return NeighborRequest{}, err
	}
	query, err := validateRequiredSearchText("query", req.Query, s.maxQueryLength)
	if err != nil {
		return NeighborRequest{}, err
	}
	sourcePath, err := normalizeOptionalSourcePath(req.SourcePath)
	if err != nil {
		return NeighborRequest{}, err
	}
	limit, depth, err := s.validateBounds(req.Limit, req.Depth)
	if err != nil {
		return NeighborRequest{}, err
	}
	req.ProjectID = projectID
	req.Query = query
	req.SourcePath = sourcePath
	req.Limit = limit
	req.Depth = depth
	return req, nil
}

func (s *QueryService) validatePathRequest(req PathRequest) (PathRequest, error) {
	projectID, err := validateRequiredSearchText("project_id", req.ProjectID, s.maxQueryLength)
	if err != nil {
		return PathRequest{}, err
	}
	fromQuery, err := validateRequiredSearchText("from_query", req.FromQuery, s.maxQueryLength)
	if err != nil {
		return PathRequest{}, err
	}
	toQuery, err := validateRequiredSearchText("to_query", req.ToQuery, s.maxQueryLength)
	if err != nil {
		return PathRequest{}, err
	}
	limit, depth, err := s.validateBounds(req.Limit, req.Depth)
	if err != nil {
		return PathRequest{}, err
	}
	req.ProjectID = projectID
	req.FromQuery = fromQuery
	req.ToQuery = toQuery
	req.Limit = limit
	req.Depth = depth
	return req, nil
}

func (s *QueryService) validateBounds(limit, depth int) (int, int, error) {
	if limit <= 0 {
		return 0, 0, fmt.Errorf("limit must be positive")
	}
	if depth <= 0 {
		return 0, 0, fmt.Errorf("depth must be positive")
	}
	if limit > s.maxLimit {
		return 0, 0, fmt.Errorf("limit must be <= %d", s.maxLimit)
	}
	if depth > s.maxDepth {
		return 0, 0, fmt.Errorf("depth must be <= %d", s.maxDepth)
	}
	return limit, depth, nil
}

func (s *QueryService) snapshot(ctx context.Context, projectID string) (*StatusSnapshot, error) {
	if s == nil || s.repo == nil {
		now := time.Now().UTC()
		if s != nil && s.now != nil {
			now = s.now().UTC()
		}
		return &StatusSnapshot{
			Available:     false,
			SchemaVersion: SchemaVersion,
			Status:        GraphStatusUnavailable,
			GeneratedAt:   now,
			Freshness:     Freshness{State: FreshnessUnknown},
			Nodes:         CountSummary{ByKind: map[string]int{}},
			Edges:         CountSummary{ByKind: map[string]int{}},
			Confidence:    map[string]int{},
			Warnings: []StatusWarning{{
				Code:    WarningGraphUnavailable,
				Message: "graph repository is not configured",
			}},
		}, nil
	}
	snapshot, err := s.repo.Snapshot(ctx, StatusOptions{
		ProjectID:  projectID,
		Now:        s.now(),
		StaleAfter: s.staleAfter,
	})
	if err != nil {
		return nil, fmt.Errorf("read graph status snapshot: %w", err)
	}
	if snapshot == nil {
		return nil, fmt.Errorf("read graph status snapshot: nil snapshot")
	}
	return snapshot, nil
}

func newQueryResult(operation, projectID, query string, limit, depth int, snapshot *StatusSnapshot) TraversalResult {
	return TraversalResult{
		ProjectID: projectID,
		Operation: operation,
		Query:     query,
		Status:    snapshot.Status,
		Available: snapshot.Available,
		Degraded:  snapshot.Status != GraphStatusFresh || !snapshot.Available || len(snapshot.Warnings) > 0,
		Warnings:  append([]StatusWarning(nil), snapshot.Warnings...),
		Limit:     limit,
		Depth:     depth,
	}
}

func statusAllowsEvidence(snapshot *StatusSnapshot) bool {
	if snapshot == nil || !snapshot.Available {
		return false
	}
	switch snapshot.Status {
	case GraphStatusUnavailable, GraphStatusIncompatible, GraphStatusEmpty:
		return false
	default:
		return true
	}
}

func (s *QueryService) loadGraph(ctx context.Context, projectID string) (*graphIndex, error) {
	nodes, err := s.repo.ListNodes(ctx, NodeQuery{ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("list graph nodes: %w", err)
	}
	edges, err := s.repo.ListEdges(ctx, EdgeQuery{ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("list graph edges: %w", err)
	}
	return newGraphIndex(nodes, edges)
}

func (s *QueryService) walkNeighbors(result *TraversalResult, index *graphIndex, seed Node, visited map[string]struct{}, maxDepth int) bool {
	type state struct {
		node Node
		path []adjacency
	}
	queue := []state{{node: seed}}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if len(current.path) >= maxDepth {
			continue
		}
		for _, next := range index.neighbors[current.node.ID] {
			if _, ok := visited[next.node.ID]; ok {
				continue
			}
			pathToNext := appendPath(current.path, next)
			evidence := edgeGraphEvidence(seed, pathToNext)
			if !appendGraphEvidence(result, evidence) {
				return false
			}
			visited[next.node.ID] = struct{}{}
			queue = append(queue, state{node: next.node, path: pathToNext})
		}
	}
	return true
}

func (s *QueryService) findPaths(result *TraversalResult, index *graphIndex, from Node, targetIDs map[string]struct{}, maxDepth int, seenPaths map[string]struct{}) bool {
	type state struct {
		node    Node
		path    []adjacency
		visited map[string]struct{}
	}
	queue := []state{{
		node:    from,
		visited: map[string]struct{}{from.ID: {}},
	}}
	if _, ok := targetIDs[from.ID]; ok {
		pathResult := GraphPath{
			From:            nodeEvidence(from),
			To:              nodeEvidence(from),
			Explanation:     graphPathExplanation(from, nil),
			ConfidenceLabel: ConfidenceHigh,
		}
		return appendGraphPath(result, pathResult)
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if len(current.path) >= maxDepth {
			continue
		}
		for _, next := range index.neighbors[current.node.ID] {
			if _, ok := current.visited[next.node.ID]; ok {
				continue
			}
			pathToNext := appendPath(current.path, next)
			if _, ok := targetIDs[next.node.ID]; ok {
				signature := pathSignature(pathToNext)
				if _, seen := seenPaths[signature]; !seen {
					seenPaths[signature] = struct{}{}
					pathResult := GraphPath{
						From:            nodeEvidence(from),
						To:              nodeEvidence(next.node),
						Hops:            pathHopEvidence(from, pathToNext),
						Explanation:     graphPathExplanation(from, pathToNext),
						ConfidenceLabel: pathConfidenceLabel(pathToNext),
					}
					if !appendGraphPath(result, pathResult) {
						return false
					}
				}
			}
			if len(pathToNext) < maxDepth {
				visited := copyVisited(current.visited)
				visited[next.node.ID] = struct{}{}
				queue = append(queue, state{node: next.node, path: pathToNext, visited: visited})
			}
		}
	}
	return true
}

type graphIndex struct {
	nodes     []Node
	byID      map[string]Node
	neighbors map[string][]adjacency
}

type adjacency struct {
	edge Edge
	node Node
	role GraphRole
}

func newGraphIndex(nodes []Node, edges []Edge) (*graphIndex, error) {
	index := &graphIndex{
		nodes:     append([]Node(nil), nodes...),
		byID:      make(map[string]Node, len(nodes)),
		neighbors: map[string][]adjacency{},
	}
	sort.Slice(index.nodes, func(i, j int) bool {
		return nodeSortKey(index.nodes[i]) < nodeSortKey(index.nodes[j])
	})
	for _, node := range index.nodes {
		index.byID[node.ID] = node
	}
	for _, edge := range edges {
		from, fromOK := index.byID[edge.FromNodeID]
		to, toOK := index.byID[edge.ToNodeID]
		if !fromOK || !toOK {
			return nil, fmt.Errorf("graph edge %s references missing endpoint", edge.ID)
		}
		index.neighbors[from.ID] = append(index.neighbors[from.ID], adjacency{
			edge: edge,
			node: to,
			role: outgoingRole(edge.Kind),
		})
		index.neighbors[to.ID] = append(index.neighbors[to.ID], adjacency{
			edge: edge,
			node: from,
			role: incomingRole(edge.Kind),
		})
	}
	for nodeID := range index.neighbors {
		sort.Slice(index.neighbors[nodeID], func(i, j int) bool {
			return adjacencySortKey(index.neighbors[nodeID][i]) < adjacencySortKey(index.neighbors[nodeID][j])
		})
	}
	return index, nil
}

func (g *graphIndex) matchNodes(query, sourcePath string) []Node {
	type scoredNode struct {
		node  Node
		score int
	}
	var scored []scoredNode
	for _, node := range g.nodes {
		if sourcePath != "" && filepath.ToSlash(node.SourcePath) != sourcePath {
			continue
		}
		score, ok := nodeMatchScore(node, query)
		if !ok {
			continue
		}
		scored = append(scored, scoredNode{node: node, score: score})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score < scored[j].score
		}
		if kindRank(scored[i].node.Kind) != kindRank(scored[j].node.Kind) {
			return kindRank(scored[i].node.Kind) < kindRank(scored[j].node.Kind)
		}
		return nodeSortKey(scored[i].node) < nodeSortKey(scored[j].node)
	})
	matches := make([]Node, 0, len(scored))
	for _, item := range scored {
		matches = append(matches, item.node)
	}
	return matches
}

func nodeMatchScore(node Node, query string) (int, bool) {
	normalizedQuery := filepath.ToSlash(query)
	lowerQuery := strings.ToLower(normalizedQuery)
	fields := []struct {
		value string
		score int
	}{
		{node.Key, 0},
		{node.SourcePath, 1},
		{node.Name, 2},
		{node.ID, 3},
	}
	for _, field := range fields {
		if field.value == "" {
			continue
		}
		if filepath.ToSlash(field.value) == normalizedQuery {
			return field.score, true
		}
	}
	for _, field := range fields {
		if field.value == "" {
			continue
		}
		value := strings.ToLower(filepath.ToSlash(field.value))
		if value == lowerQuery {
			return field.score + 4, true
		}
		if strings.Contains(value, lowerQuery) {
			return field.score + 8, true
		}
	}
	return 0, false
}

func appendGraphEvidence(result *TraversalResult, evidence GraphEvidence) bool {
	if len(result.Evidence) >= result.Limit {
		result.Truncated = true
		return false
	}
	result.Evidence = append(result.Evidence, evidence)
	return true
}

func appendGraphPath(result *TraversalResult, path GraphPath) bool {
	if len(result.Paths) >= result.Limit {
		result.Truncated = true
		return false
	}
	result.Paths = append(result.Paths, path)
	return true
}

func queryMatchEvidence(node Node) GraphEvidence {
	return GraphEvidence{
		Node:            nodeEvidence(node),
		Role:            RoleQueryMatch,
		PathExplanation: fmt.Sprintf("matched %s %q", node.Kind, preferredNodeLabel(node)),
	}
}

func edgeGraphEvidence(seed Node, path []adjacency) GraphEvidence {
	last := path[len(path)-1]
	return GraphEvidence{
		Node:            nodeEvidence(last.node),
		Relation:        string(last.edge.Kind),
		Role:            last.role,
		ConfidenceLabel: last.edge.ConfidenceLabel,
		EdgeSourcePath:  last.edge.SourcePath,
		EdgeEvidence:    last.edge.Evidence,
		PathExplanation: graphPathExplanation(seed, path),
	}
}

func pathHopEvidence(seed Node, path []adjacency) []GraphEvidence {
	hops := make([]GraphEvidence, 0, len(path))
	for i := range path {
		hops = append(hops, edgeGraphEvidence(seed, path[:i+1]))
	}
	return hops
}

func nodeEvidence(node Node) GraphNodeEvidence {
	return GraphNodeEvidence{
		ID:         node.ID,
		Kind:       node.Kind,
		Key:        node.Key,
		Name:       node.Name,
		SourcePath: node.SourcePath,
		Language:   node.Language,
		SymbolKind: node.SymbolKind,
		StartLine:  node.StartLine,
		EndLine:    node.EndLine,
	}
}

func graphPathExplanation(seed Node, path []adjacency) string {
	parts := []string{preferredNodeLabel(seed)}
	for _, hop := range path {
		parts = append(parts, fmt.Sprintf(
			"-%s/%s/%s-> %s",
			hop.role,
			hop.edge.Kind,
			hop.edge.ConfidenceLabel,
			preferredNodeLabel(hop.node),
		))
	}
	return strings.Join(parts, " ")
}

func preferredNodeLabel(node Node) string {
	if node.Name != "" {
		return node.Name
	}
	if node.Key != "" {
		return node.Key
	}
	return node.ID
}

func pathConfidenceLabel(path []adjacency) ConfidenceLabel {
	label := ConfidenceHigh
	for _, hop := range path {
		if confidenceRank(hop.edge.ConfidenceLabel) < confidenceRank(label) {
			label = hop.edge.ConfidenceLabel
		}
	}
	return label
}

func confidenceRank(label ConfidenceLabel) int {
	switch label {
	case ConfidenceExact:
		return 4
	case ConfidenceHigh:
		return 3
	case ConfidenceMedium:
		return 2
	case ConfidenceLow:
		return 1
	default:
		return 0
	}
}

func outgoingRole(kind EdgeKind) GraphRole {
	switch kind {
	case EdgeKindFileDeclaresPackage:
		return RoleDeclaresPackage
	case EdgeKindPackageImports, EdgeKindFileImports:
		return RoleImports
	case EdgeKindFileDefinesSymbol:
		return RoleDefines
	case EdgeKindSymbolHasChunk:
		return RoleHasChunk
	case EdgeKindFileDefinesConfigKey:
		return RoleDefinesConfigKey
	case EdgeKindTestCoversImplementation:
		return RoleTestCovers
	case EdgeKindDocMentionsPath, EdgeKindDocMentionsFile, EdgeKindDocMentionsSymbol, EdgeKindDocMentionsConfigKey:
		return RoleMentionsPath
	default:
		return RoleRelated
	}
}

func incomingRole(kind EdgeKind) GraphRole {
	switch kind {
	case EdgeKindFileDeclaresPackage:
		return RoleDeclaredBy
	case EdgeKindPackageImports, EdgeKindFileImports:
		return RoleImportedBy
	case EdgeKindFileDefinesSymbol:
		return RoleDefinedBy
	case EdgeKindSymbolHasChunk:
		return RoleChunkOf
	case EdgeKindFileDefinesConfigKey:
		return RoleConfigKeyDefinedBy
	case EdgeKindTestCoversImplementation:
		return RoleCoveredByTest
	case EdgeKindDocMentionsPath, EdgeKindDocMentionsFile, EdgeKindDocMentionsSymbol, EdgeKindDocMentionsConfigKey:
		return RoleMentionedByDoc
	default:
		return RoleRelated
	}
}

func appendPath(path []adjacency, next adjacency) []adjacency {
	extended := make([]adjacency, 0, len(path)+1)
	extended = append(extended, path...)
	extended = append(extended, next)
	return extended
}

func pathSignature(path []adjacency) string {
	parts := make([]string, 0, len(path))
	for _, hop := range path {
		parts = append(parts, hop.edge.ID+":"+string(hop.role)+":"+hop.node.ID)
	}
	return strings.Join(parts, "|")
}

func copyVisited(visited map[string]struct{}) map[string]struct{} {
	copied := make(map[string]struct{}, len(visited)+1)
	for key := range visited {
		copied[key] = struct{}{}
	}
	return copied
}

func validateRequiredSearchText(field, value string, maxLength int) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if len(trimmed) > maxLength {
		return "", fmt.Errorf("%s must be <= %d bytes", field, maxLength)
	}
	if err := validateSafeText(field, trimmed); err != nil {
		return "", err
	}
	return trimmed, nil
}

func validateSafeText(field, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", field)
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%s contains unsafe control character", field)
		}
	}
	return nil
}

func normalizeOptionalSourcePath(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	if err := validateSafeText("source_path", trimmed); err != nil {
		return "", err
	}
	if filepath.IsAbs(trimmed) || isWindowsAbsolutePath(trimmed) {
		return "", fmt.Errorf("source_path must be relative")
	}
	normalized := filepath.ToSlash(trimmed)
	if hasParentPathSegment(normalized) {
		return "", fmt.Errorf("source_path must be relative")
	}
	cleaned := path.Clean(normalized)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("source_path must be relative")
	}
	return cleaned, nil
}

func isWindowsAbsolutePath(value string) bool {
	if strings.HasPrefix(value, `\\`) || strings.HasPrefix(value, `//`) {
		return true
	}
	return len(value) >= 3 && value[1] == ':' && (value[2] == '\\' || value[2] == '/')
}

func hasParentPathSegment(value string) bool {
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func nodeSortKey(node Node) string {
	return fmt.Sprintf("%03d|%s|%s|%s", kindRank(node.Kind), node.SourcePath, node.Key, node.ID)
}

func adjacencySortKey(item adjacency) string {
	return strings.Join([]string{
		string(item.role),
		item.edge.SourcePath,
		string(item.edge.Kind),
		item.node.ID,
		item.edge.NaturalKey(),
	}, "|")
}

func kindRank(kind NodeKind) int {
	switch kind {
	case NodeKindFile:
		return 0
	case NodeKindPackage:
		return 1
	case NodeKindImport:
		return 2
	case NodeKindSymbol:
		return 3
	case NodeKindChunk:
		return 4
	case NodeKindConfigKey:
		return 5
	default:
		return 99
	}
}
