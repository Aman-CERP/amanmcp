package graph

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/config"
)

// ErrInvalidQueryParams is the typed sentinel for graph.query input-validation
// rejections (missing/oversized/unsafe params). Every validateQueryRequest error
// wraps it so callers — notably the eval degradation classifier — can use
// errors.Is instead of matching on message text that can silently drift
// (DEBT-037 finding #3). It marks an authoring/config failure, never an
// infrastructure failure; infra errors (status/node/edge reads) are wrapped
// with their own context and never wrap this sentinel.
var ErrInvalidQueryParams = errors.New("invalid graph query params")

const (
	QueryModeFindReferences = "find_references"
	QueryModeExplainSymbol  = "explain_symbol"
	QueryModeImpactAnalysis = "impact_analysis"

	SubjectTypeAuto     = "auto"
	SubjectTypePath     = "path"
	SubjectTypeSymbol   = "symbol"
	SubjectTypePackage  = "package"
	SubjectTypeResultID = "result_id"

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
	traversal      config.GraphTraversalConfig
}

// NewQueryService constructs a graph query service.
func NewQueryService(repo Repository, opts QueryServiceOptions) *QueryService {
	if opts.StaleAfter <= 0 {
		opts.StaleAfter = DefaultStaleAfter
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	if opts.MaxQueryLength <= 0 {
		opts.MaxQueryLength = defaultMaxQueryByteSize
	}
	config.NormalizeGraphTraversalConfig(&opts.Traversal)
	if opts.Traversal.Policy.MaxResults == 0 {
		opts.Traversal = config.DefaultGraphTraversalConfig()
	}
	if opts.MaxLimit <= 0 {
		opts.MaxLimit = opts.Traversal.Policy.MaxResults
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = opts.Traversal.Policy.MaxDepth
	}
	return &QueryService{
		repo:           repo,
		staleAfter:     opts.StaleAfter,
		now:            opts.Now,
		maxLimit:       opts.MaxLimit,
		maxDepth:       opts.MaxDepth,
		maxQueryLength: opts.MaxQueryLength,
		traversal:      opts.Traversal,
	}
}

// QueryServiceOptions controls graph query status checks.
type QueryServiceOptions struct {
	StaleAfter     time.Duration
	Now            func() time.Time
	MaxLimit       int
	MaxDepth       int
	MaxQueryLength int
	Traversal      config.GraphTraversalConfig
}

// QueryRequest is the input contract for graph relationship queries.
type QueryRequest struct {
	ProjectID       string
	Mode            string
	Query           string
	SubjectType     string
	Limit           int
	IncludeStale    bool
	BudgetOverrides TraversalBudgetOverrides
}

// QueryResponse is the compact graph evidence envelope.
//
// Resolution and Candidates are additive (GRA19): a subject now resolves before
// traversal. Resolution is omitted on the degraded/unusable short-circuits that
// never reach resolution, so an absent value keeps the legacy shape; on the
// query path it is one of resolved / disambiguation_required / subject_not_found.
// Results is non-empty only when Resolution == resolved.
type QueryResponse struct {
	Status     GraphStatus     `json:"status"`
	Degraded   bool            `json:"degraded"`
	Mode       string          `json:"mode"`
	Query      string          `json:"query"`
	Resolution string          `json:"resolution,omitempty"`
	Results    []QueryResult   `json:"results,omitempty"`
	Candidates []Candidate     `json:"candidates,omitempty"`
	Warnings   []StatusWarning `json:"warnings,omitempty"`
}

// QueryResult is one graph-backed relationship result.
//
// The flat top-level fields (node_id, node_kind, source_path, role, relation,
// confidence, confidence_label, evidence_method, evidence_snippet, stale) are
// retained for backward-compatible eval scoring. Path (GRA21) replaces the legacy
// flat []string{seed, edge, related} "graph_path" array with the structured,
// source-citable GraphPath the multi-hop engine already uses — single-hop this
// sprint, so a future multi-hop promotion (FEAT-SYN9) needs no second schema.
type QueryResult struct {
	NodeID               string          `json:"node_id"`
	NodeKind             NodeKind        `json:"node_kind"`
	Name                 string          `json:"name,omitempty"`
	SourcePath           string          `json:"source_path,omitempty"`
	Role                 string          `json:"role"`
	Relation             EdgeKind        `json:"relation"`
	Confidence           float64         `json:"confidence"`
	ConfidenceLabel      ConfidenceLabel `json:"confidence_label"`
	Heuristic            bool            `json:"heuristic,omitempty"`
	EvidenceMethod       string          `json:"evidence_method"`
	EvidenceSnippet      string          `json:"evidence_snippet,omitempty"`
	Stale                bool            `json:"stale,omitempty"`
	Path                 GraphPath       `json:"path"`
	AdditionalPathsCount int             `json:"additional_paths_count,omitempty"`
	AdditionalRelations  []EdgeKind      `json:"additional_relations,omitempty"`
}

// Query executes a bounded graph relationship query.
func (s *QueryService) Query(ctx context.Context, req QueryRequest) (QueryResponse, error) {
	if s == nil || s.repo == nil {
		return QueryResponse{}, fmt.Errorf("graph repository is required")
	}
	req.Mode = normalizeQueryMode(req.Mode)
	req.Query = strings.TrimSpace(req.Query)
	req.SubjectType = normalizeSubjectType(req.SubjectType)
	if err := validateQueryRequest(req, s.policyMaxResults()); err != nil {
		return QueryResponse{}, err
	}
	budget, err := resolveTraversalBudgets(req.Mode, s.traversal, req.BudgetOverrides)
	if err != nil {
		return QueryResponse{}, err
	}
	if req.Limit > 0 {
		budget.MaxResults = req.Limit
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

	resolution := resolveSubjectByType(nodes, req.Query, req.Mode, req.SubjectType, resolveOptions{
		// Candidate count is a distinct axis from the result limit: a caller who
		// sets Limit=1 to keep results small still needs to see the competing
		// subjects to disambiguate, so candidates use their own fixed cap.
		CandidateLimit: defaultGraphQueryLimit,
		HintLimit:      defaultSubjectHintLimit,
	})
	response.Resolution = resolution.Outcome
	if resolution.Outcome != ResolutionResolved {
		if req.SubjectType == SubjectTypeResultID && resolution.Outcome == ResolutionSubjectNotFound {
			response.Warnings = append(response.Warnings, StatusWarning{
				Code: WarningCode("graph_result_id_not_found"),
				Message: fmt.Sprintf("result_id %q is not in graph; v1 accepts stable graph node ids, not search-result hashes",
					req.Query),
			})
		}
		// Disambiguation and not-found never traverse: emit the candidates/hints and
		// leave Results empty so the caller can re-query a specific subject rather
		// than receive a merged or silent answer.
		response.Candidates = resolution.Candidates
		if resolution.CandidatesTotal > len(resolution.Candidates) {
			response.Warnings = append(response.Warnings, StatusWarning{
				Code: WarningCode("graph_candidates_truncated"),
				Message: fmt.Sprintf("subject %q matched %d candidates; showing the top %d — narrow the query",
					req.Query, resolution.CandidatesTotal, len(resolution.Candidates)),
			})
		}
		appendUnsupportedLanguageWarning(&response, req.Query, req.SubjectType, resolution)
		return response, nil
	}
	edges = mergeCompetingEdges(edges)
	results, nodesExhausted := relatedResults(nodesByID(nodes), edges, resolution.Seeds, req.Mode, budget.MaxNodes)
	if nodesExhausted {
		response.Warnings = append(response.Warnings, newTraversalBudgetWarning(TraversalBudgetNodes, budget.MaxNodes))
	}
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
	budgeted := applyTraversalBudgets(results, budget)
	response.Results = budgeted.results
	response.Warnings = append(response.Warnings, budgeted.warnings...)
	appendUnsupportedLanguageWarning(&response, req.Query, req.SubjectType, resolution)
	return response, nil
}

func (s *QueryService) policyMaxResults() int {
	if s == nil {
		return maxGraphQueryLimit
	}
	if s.traversal.Policy.MaxResults > 0 {
		return s.traversal.Policy.MaxResults
	}
	return maxGraphQueryLimit
}

func validateQueryRequest(req QueryRequest, policyMaxResults int) error {
	if strings.TrimSpace(req.ProjectID) == "" {
		return fmt.Errorf("project_id is required: %w", ErrInvalidQueryParams)
	}
	switch req.Mode {
	case QueryModeFindReferences, QueryModeExplainSymbol, QueryModeImpactAnalysis:
	default:
		return fmt.Errorf("unsupported graph query mode %q: %w", req.Mode, ErrInvalidQueryParams)
	}
	switch normalizeSubjectType(req.SubjectType) {
	case SubjectTypeAuto, SubjectTypePath, SubjectTypeSymbol, SubjectTypePackage, SubjectTypeResultID:
	default:
		return fmt.Errorf("unsupported graph query subject_type %q: %w", req.SubjectType, ErrInvalidQueryParams)
	}
	if req.Query == "" {
		return fmt.Errorf("query is required: %w", ErrInvalidQueryParams)
	}
	if strings.Contains(req.Query, "\x00") {
		return fmt.Errorf("query contains unsafe NUL byte: %w", ErrInvalidQueryParams)
	}
	if filepath.IsAbs(req.Query) || strings.Contains(filepath.ToSlash(req.Query), "../") {
		return fmt.Errorf("query must be project-relative and safe: %w", ErrInvalidQueryParams)
	}
	if policyMaxResults <= 0 {
		policyMaxResults = maxGraphQueryLimit
	}
	if req.Limit < 0 || req.Limit > policyMaxResults {
		return fmt.Errorf("limit must be between 0 and %d: %w", policyMaxResults, ErrInvalidQueryParams)
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

func normalizeSubjectType(subjectType string) string {
	subjectType = strings.TrimSpace(subjectType)
	if subjectType == "" {
		return SubjectTypeAuto
	}
	return subjectType
}

func resolveSubjectByType(nodes []Node, query, mode, subjectType string, opts resolveOptions) subjectResolution {
	switch subjectType {
	case SubjectTypePath:
		return resolvePathSubject(nodes, query, opts)
	case SubjectTypeSymbol:
		return resolveSubject(filterNodesByKind(nodes, NodeKindSymbol), query, mode, opts)
	case SubjectTypePackage:
		return resolvePackageSubject(nodes, query, opts)
	case SubjectTypeResultID:
		return resolveResultIDSubject(nodes, query)
	default:
		return resolveAutoSubject(nodes, query, mode, opts)
	}
}

func resolveAutoSubject(nodes []Node, query, mode string, opts resolveOptions) subjectResolution {
	// explain_symbol is symbol-scoped by contract. Let the GRA19 resolver enforce
	// that filter; otherwise path-shaped auto queries would resolve a file subject
	// and bypass symbol disambiguation.
	if mode != QueryModeExplainSymbol && looksLikePath(query) {
		resolution := resolvePathSubject(nodes, query, opts)
		if resolution.Outcome == ResolutionResolved {
			return resolution
		}
	}
	return resolveSubject(nodes, query, mode, opts)
}

func looksLikePath(query string) bool {
	normalized := filepath.ToSlash(strings.TrimSpace(query))
	return strings.Contains(normalized, "/")
}

func resolvePathSubject(nodes []Node, query string, opts resolveOptions) subjectResolution {
	pathNodes := filterNodes(nodes, func(node Node) bool {
		return isPathSubjectKind(node.Kind)
	})
	normalized := filepath.ToSlash(strings.TrimSpace(query))
	var matched []Node
	for _, node := range pathNodes {
		if filepath.ToSlash(node.SourcePath) == normalized {
			matched = append(matched, node)
		}
	}
	if len(matched) == 0 {
		return subjectResolution{
			Outcome:    ResolutionSubjectNotFound,
			Candidates: nearestSubjectHints(pathNodes, query, QueryModeFindReferences, opts.HintLimit),
		}
	}
	return resolutionFromMatchedNodes(matched, nodes, opts)
}

func isPathSubjectKind(kind NodeKind) bool {
	switch kind {
	case NodeKindFile, NodeKindTestFile, NodeKindDoc, NodeKindConfigFile:
		return true
	default:
		return false
	}
}

func resolvePackageSubject(nodes []Node, query string, opts resolveOptions) subjectResolution {
	packageNodes := filterNodesByKind(nodes, NodeKindPackage)
	trimmed := strings.TrimSpace(query)
	// Resolution order is intentionally strictest-first:
	//  1. exact package key or name (`dir#pkg` / `pkg`)
	//  2. exact package directory (`dir`)
	//  3. case-folded key/name/directory for editor/client case drift
	// Multiple matches at any tier still disambiguate instead of guessing.
	matched := exactPackageMatches(packageNodes, trimmed)
	if len(matched) == 0 {
		matched = exactPackageDirectoryMatches(packageNodes, trimmed)
	}
	if len(matched) == 0 {
		matched = foldedPackageMatches(packageNodes, trimmed)
	}
	if len(matched) == 0 {
		return subjectResolution{
			Outcome:    ResolutionSubjectNotFound,
			Candidates: nearestSubjectHints(packageNodes, query, QueryModeFindReferences, opts.HintLimit),
		}
	}
	return resolutionFromMatchedNodes(matched, nodes, opts)
}

func exactPackageMatches(nodes []Node, query string) []Node {
	var matched []Node
	for _, node := range nodes {
		if node.Key == query || node.Name == query {
			matched = append(matched, node)
		}
	}
	return matched
}

func exactPackageDirectoryMatches(nodes []Node, query string) []Node {
	var matched []Node
	normalized := filepath.ToSlash(query)
	for _, node := range nodes {
		dir, _, ok := strings.Cut(filepath.ToSlash(node.Key), "#")
		if ok && dir == normalized {
			matched = append(matched, node)
		}
	}
	return matched
}

func foldedPackageMatches(nodes []Node, query string) []Node {
	var matched []Node
	normalized := filepath.ToSlash(query)
	for _, node := range nodes {
		dir, _, hasDir := strings.Cut(filepath.ToSlash(node.Key), "#")
		if strings.EqualFold(node.Key, query) ||
			strings.EqualFold(node.Name, query) ||
			(hasDir && strings.EqualFold(dir, normalized)) {
			matched = append(matched, node)
		}
	}
	return matched
}

func resolveResultIDSubject(nodes []Node, query string) subjectResolution {
	for _, node := range nodes {
		if node.ID == query {
			return subjectResolution{
				Outcome: ResolutionResolved,
				Seeds:   seedScope(node, nodes),
			}
		}
	}
	return subjectResolution{Outcome: ResolutionSubjectNotFound}
}

func resolutionFromMatchedNodes(matched, seedUniverse []Node, opts resolveOptions) subjectResolution {
	sort.Slice(matched, func(i, j int) bool {
		return nodeSortKey(matched[i]) < nodeSortKey(matched[j])
	})
	if len(matched) == 1 {
		return subjectResolution{
			Outcome: ResolutionResolved,
			Seeds:   seedScope(matched[0], seedUniverse),
		}
	}
	limit := opts.CandidateLimit
	if limit <= 0 {
		limit = defaultGraphQueryLimit
	}
	capped := matched
	if len(capped) > limit {
		capped = capped[:limit]
	}
	candidates := make([]Candidate, 0, len(capped))
	for _, node := range capped {
		candidates = append(candidates, newCandidate(node, disambiguationHint(node)))
	}
	return subjectResolution{
		Outcome:         ResolutionDisambiguationRequired,
		Candidates:      candidates,
		CandidatesTotal: len(matched),
	}
}

func filterNodesByKind(nodes []Node, kind NodeKind) []Node {
	return filterNodes(nodes, func(node Node) bool {
		return node.Kind == kind
	})
}

func filterNodes(nodes []Node, keep func(Node) bool) []Node {
	filtered := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if keep(node) {
			filtered = append(filtered, node)
		}
	}
	return filtered
}

// QueryServable reports whether a graph status can serve a real graph.query
// answer rather than a degraded/empty envelope. It is stricter than
// QueryAvailable (which still reports available=true for a `failed` build): only
// fresh, stale, and partial graphs are queried. This is the servability SSOT
// shared by the query service short-circuit and direct graph eval measurement
// accounting so the two surfaces cannot drift.
func QueryServable(status GraphStatus) bool {
	return status == GraphStatusFresh || status == GraphStatusStale || status == GraphStatusPartial
}

func graphStatusUsable(status GraphStatus) bool {
	return QueryServable(status)
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

func relatedResults(nodes map[string]Node, edges []Edge, seeds []Node, mode string, maxNodes int) ([]QueryResult, bool) {
	seedIDs := make(map[string]Node, len(seeds))
	for _, seed := range seeds {
		seedIDs[seed.ID] = seed
	}
	seen := map[string]bool{}
	relatedSeen := map[string]struct{}{}
	var results []QueryResult
	nodesExhausted := false
	for _, edge := range edges {
		seed, relatedID, role, ok := relationshipForMode(edge, seedIDs, mode)
		if !ok {
			continue
		}
		related, ok := nodes[relatedID]
		if !ok {
			continue
		}
		if maxNodes > 0 {
			if _, counted := relatedSeen[related.ID]; !counted {
				if len(relatedSeen) >= maxNodes {
					nodesExhausted = true
					break
				}
				relatedSeen[related.ID] = struct{}{}
			}
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
			Heuristic:       edge.Evidence.Heuristic,
			EvidenceMethod:  edge.Evidence.Method,
			EvidenceSnippet: safeEvidenceSnippet(edge.Evidence.Snippet),
			Stale:           edge.Stale,
			Path:            singleHopPath(seed, related, edge, role),
		})
	}
	return results, nodesExhausted
}

// singleHopPath builds the structured GRA21 GraphPath for one query result. It
// reuses the multi-hop engine's projection helpers (nodeEvidence, pathHopEvidence,
// graphPathExplanation, pathConfidenceLabel) over a one-element adjacency path so
// single-hop and future multi-hop results share exactly one GraphPath shape. The
// edge snippet is capped to the flat-field length (safeEvidenceSnippet) to keep
// the structured projection compact for the graph.query token budget (GRA23).
func singleHopPath(seed, related Node, edge Edge, role GraphRole) GraphPath {
	hopEdge := edge
	hopEdge.Evidence.Snippet = safeEvidenceSnippet(edge.Evidence.Snippet)
	path := []adjacency{{edge: hopEdge, node: related, role: role}}
	return GraphPath{
		From:            nodeEvidence(seed),
		To:              nodeEvidence(related),
		Hops:            pathHopEvidence(seed, path),
		Explanation:     graphPathExplanation(seed, path),
		ConfidenceLabel: pathConfidenceLabel(path),
	}
}

func relationshipForMode(edge Edge, seeds map[string]Node, mode string) (Node, string, GraphRole, bool) {
	if seed, ok := seeds[edge.FromNodeID]; ok {
		return seed, edge.ToNodeID, outgoingRole(edge.Kind), true
	}
	if mode != QueryModeImpactAnalysis {
		if seed, ok := seeds[edge.ToNodeID]; ok {
			return seed, edge.FromNodeID, incomingRole(edge.Kind), true
		}
	}
	return Node{}, "", "", false
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
