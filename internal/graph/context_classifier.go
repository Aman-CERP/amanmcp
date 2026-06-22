package graph

import (
	"path/filepath"
	"strings"
)

// ContextRole labels why an expand_context pack item is included for agent consumption.
type ContextRole string

const (
	ContextRoleEntrypoint       ContextRole = "entrypoint"
	ContextRoleImplementation   ContextRole = "implementation"
	ContextRoleCaller           ContextRole = "caller"
	ContextRoleTest             ContextRole = "test"
	ContextRoleDocOrADR         ContextRole = "doc_or_adr"
	ContextRoleConfig           ContextRole = "config"
	ContextRoleRelatedPMMemory  ContextRole = "related_pm_memory"
	ContextRoleRelatedDocMemory ContextRole = "related_doc_memory"
)

// RoleAssignment is one role label with explicit confidence and heuristic flags.
type RoleAssignment struct {
	Role            ContextRole     `json:"role"`
	ConfidenceLabel ConfidenceLabel `json:"confidence_label"`
	Heuristic       bool            `json:"heuristic"`
	Flags           []string        `json:"flags,omitempty"`
}

// PathHop carries one hop of graph path evidence for multi-hop role classification.
type PathHop struct {
	Edge Edge
}

// ClassifyContextInput is the evidence bundle used to derive one-or-more context roles.
type ClassifyContextInput struct {
	SeedID         string
	ImportAnchorID string
	Target         Node
	LastEdge       Edge
	Path           []PathHop
}

// ClassifyContextRoles maps graph node/edge/path evidence to a deduped role set.
func ClassifyContextRoles(input ClassifyContextInput) []RoleAssignment {
	roles := map[ContextRole]RoleAssignment{}
	add := func(role ContextRole, label ConfidenceLabel, heuristic bool, flags ...string) {
		existing, ok := roles[role]
		if ok {
			if confidenceRank(label) > confidenceRank(existing.ConfidenceLabel) {
				existing.ConfidenceLabel = label
			}
			if heuristic {
				existing.Heuristic = true
			}
			existing.Flags = mergeFlags(existing.Flags, flags)
			roles[role] = existing
			return
		}
		roles[role] = RoleAssignment{
			Role:            role,
			ConfidenceLabel: label,
			Heuristic:       heuristic,
			Flags:           uniqueFlags(flags),
		}
	}

	if classifyPreciseCaller(input) {
		add(ContextRoleCaller, ConfidenceExact, false)
	} else if classifyImportProxyCaller(input) {
		add(ContextRoleCaller, ConfidenceLow, true, "import-proxy")
	}
	if classifyImplementation(input) {
		add(ContextRoleImplementation, confidenceFromEdge(input.LastEdge), input.LastEdge.Evidence.Heuristic)
	}
	if classifyTest(input) {
		add(ContextRoleTest, confidenceFromEdge(input.LastEdge), input.LastEdge.Evidence.Heuristic || input.LastEdge.Kind == EdgeKindTestCoversImplementation)
	}
	if classifyDocOrADR(input) {
		flags := docFlags(input.LastEdge.SourcePath)
		add(ContextRoleDocOrADR, confidenceFromEdge(input.LastEdge), docMentionHeuristic(input.LastEdge), flags...)
	}
	if classifyConfig(input) {
		add(ContextRoleConfig, confidenceFromEdge(input.LastEdge), input.LastEdge.Evidence.Heuristic)
	}
	if classifyPMMemory(input) {
		add(ContextRoleRelatedPMMemory, ConfidenceMedium, true)
	}
	if classifyArchivedDocMemory(input) {
		add(ContextRoleRelatedDocMemory, ConfidenceMedium, true)
	}
	if classifyEntrypoint(input) {
		add(ContextRoleEntrypoint, ConfidenceMedium, true)
	}

	return append([]RoleAssignment(nil), sortedContextRoles(roles)...)
}

func classifyImplementation(input ClassifyContextInput) bool {
	if input.Target.Kind == NodeKindTestFile {
		return false
	}
	for _, hop := range append(input.Path, PathHop{Edge: input.LastEdge}) {
		switch hop.Edge.Kind {
		case EdgeKindFileDefinesSymbol, EdgeKindSymbolHasChunk:
			if !isTestSourcePath(input.Target.SourcePath) {
				return true
			}
		}
	}
	return false
}

func classifyTest(input ClassifyContextInput) bool {
	if input.Target.Kind == NodeKindTestFile {
		return true
	}
	for _, hop := range append(input.Path, PathHop{Edge: input.LastEdge}) {
		if hop.Edge.Kind == EdgeKindTestCoversImplementation {
			return true
		}
	}
	return isTestSourcePath(input.Target.SourcePath)
}

func classifyDocOrADR(input ClassifyContextInput) bool {
	for _, hop := range append(input.Path, PathHop{Edge: input.LastEdge}) {
		switch hop.Edge.Kind {
		case EdgeKindDocMentionsFile, EdgeKindDocMentionsSymbol, EdgeKindDocMentionsConfigKey, EdgeKindDocMentionsPath:
			return true
		}
	}
	return false
}

func classifyConfig(input ClassifyContextInput) bool {
	if input.Target.Kind == NodeKindConfigKey {
		return true
	}
	for _, hop := range append(input.Path, PathHop{Edge: input.LastEdge}) {
		switch hop.Edge.Kind {
		case EdgeKindFileDefinesConfigKey, EdgeKindDocMentionsConfigKey:
			return true
		}
	}
	return false
}

func classifyPreciseCaller(input ClassifyContextInput) bool {
	if input.SeedID == "" || input.Target.ID == "" {
		return false
	}
	for _, hop := range append(input.Path, PathHop{Edge: input.LastEdge}) {
		if hop.Edge.Kind != EdgeKindSymbolCalls || hop.Edge.Evidence.Heuristic {
			continue
		}
		if hop.Edge.ToNodeID == input.SeedID && hop.Edge.FromNodeID == input.Target.ID {
			return true
		}
	}
	return false
}

func classifyImportProxyCaller(input ClassifyContextInput) bool {
	if classifyPreciseCaller(input) || input.SeedID == "" || input.Target.ID == "" {
		return false
	}
	anchor := input.ImportAnchorID
	if anchor == "" {
		anchor = input.SeedID
	}
	for _, hop := range append(input.Path, PathHop{Edge: input.LastEdge}) {
		if hop.Edge.Kind != EdgeKindFileImports && hop.Edge.Kind != EdgeKindPackageImports {
			continue
		}
		// Inbound import to the seed scope: target imports the anchor file/package.
		if hop.Edge.ToNodeID == anchor && hop.Edge.FromNodeID == input.Target.ID {
			return true
		}
	}
	return false
}

func importAnchorID(seed Node, hops []PathHop) string {
	switch seed.Kind {
	case NodeKindFile, NodeKindPackage:
		return seed.ID
	}
	for _, hop := range hops {
		if hop.Edge.Kind == EdgeKindFileDefinesSymbol && hop.Edge.ToNodeID == seed.ID {
			return hop.Edge.FromNodeID
		}
	}
	return seed.ID
}

func classifyPMMemory(input ClassifyContextInput) bool {
	for _, hop := range append(input.Path, PathHop{Edge: input.LastEdge}) {
		if isPMItemPath(hop.Edge.SourcePath) && isDocMentionEdge(hop.Edge.Kind) {
			return true
		}
	}
	return false
}

func classifyArchivedDocMemory(input ClassifyContextInput) bool {
	for _, hop := range append(input.Path, PathHop{Edge: input.LastEdge}) {
		if isArchivedDocPath(hop.Edge.SourcePath) && isDocMentionEdge(hop.Edge.Kind) {
			return true
		}
	}
	return false
}

func classifyEntrypoint(input ClassifyContextInput) bool {
	path := filepath.ToSlash(strings.TrimSpace(input.Target.SourcePath))
	if path == "" {
		return false
	}
	if strings.HasPrefix(path, "cmd/") {
		return true
	}
	if strings.Contains(path, "/mcp/") && strings.HasSuffix(path, "_tools.go") {
		return true
	}
	if input.Target.Name == "main" && strings.Contains(path, "main.go") {
		return true
	}
	if input.Target.Name == "registerTools" && strings.Contains(path, "internal/mcp/") {
		return true
	}
	return false
}

func isDocMentionEdge(kind EdgeKind) bool {
	switch kind {
	case EdgeKindDocMentionsFile, EdgeKindDocMentionsSymbol, EdgeKindDocMentionsConfigKey, EdgeKindDocMentionsPath:
		return true
	default:
		return false
	}
}

func isPMItemPath(path string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	return strings.HasPrefix(path, ".aman-pm/backlog/")
}

func isArchivedDocPath(path string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	return strings.HasPrefix(path, "archive/")
}

func isTestSourcePath(path string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	return strings.HasSuffix(path, "_test.go") || strings.Contains(path, "/testdata/")
}

func docMentionHeuristic(edge Edge) bool {
	return edge.Evidence.Heuristic || edge.Confidence < 0.9
}

func docFlags(sourcePath string) []string {
	path := filepath.ToSlash(strings.ToUpper(sourcePath))
	if strings.Contains(path, "ADR-") || strings.Contains(path, "/DECISIONS/") {
		return []string{"adr"}
	}
	return nil
}

func confidenceFromEdge(edge Edge) ConfidenceLabel {
	if edge.ConfidenceLabel != "" {
		return edge.ConfidenceLabel
	}
	switch {
	case edge.Confidence >= 1:
		return ConfidenceExact
	case edge.Confidence >= 0.9:
		return ConfidenceHigh
	case edge.Confidence >= 0.7:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}

func sortedContextRoles(roles map[ContextRole]RoleAssignment) []RoleAssignment {
	order := []ContextRole{
		ContextRoleEntrypoint,
		ContextRoleImplementation,
		ContextRoleCaller,
		ContextRoleTest,
		ContextRoleDocOrADR,
		ContextRoleConfig,
		ContextRoleRelatedPMMemory,
		ContextRoleRelatedDocMemory,
	}
	out := make([]RoleAssignment, 0, len(roles))
	for _, role := range order {
		if assignment, ok := roles[role]; ok {
			out = append(out, assignment)
		}
	}
	return out
}

func mergeFlags(existing, extra []string) []string {
	return uniqueFlags(append(append([]string(nil), existing...), extra...))
}

func uniqueFlags(flags []string) []string {
	if len(flags) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(flags))
	out := make([]string, 0, len(flags))
	for _, flag := range flags {
		flag = strings.TrimSpace(flag)
		if flag == "" {
			continue
		}
		if _, ok := seen[flag]; ok {
			continue
		}
		seen[flag] = struct{}{}
		out = append(out, flag)
	}
	return out
}