package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyContextRoles_ImplementationFromFileDefinesSymbol(t *testing.T) {
	roles := ClassifyContextRoles(ClassifyContextInput{
		Target: Node{
			Kind:       NodeKindSymbol,
			SourcePath: "internal/search/engine.go",
			Name:       "Search",
		},
		LastEdge: Edge{
			Kind:       EdgeKindFileDefinesSymbol,
			Confidence: 0.95,
			Evidence:   Evidence{Method: "go_symbol", Heuristic: false},
		},
	})
	require.NotEmpty(t, roles)
	assertRole(t, roles, ContextRoleImplementation, ConfidenceHigh, false)
}

func TestClassifyContextRoles_TestFromTestCoversImplementation(t *testing.T) {
	roles := ClassifyContextRoles(ClassifyContextInput{
		Target: Node{Kind: NodeKindTestFile, SourcePath: "internal/search/engine_test.go"},
		LastEdge: Edge{
			Kind:       EdgeKindTestCoversImplementation,
			Confidence: 0.72,
			Evidence:   Evidence{Method: "test_name_match", Heuristic: true},
		},
	})
	assertRole(t, roles, ContextRoleTest, ConfidenceMedium, true)
}

func TestClassifyContextRoles_DocOrADRFromDocMentions(t *testing.T) {
	roles := ClassifyContextRoles(ClassifyContextInput{
		Target: Node{Kind: NodeKindFile, SourcePath: "internal/graph/query.go"},
		LastEdge: Edge{
			Kind:       EdgeKindDocMentionsFile,
			SourcePath: "docs/reference/decisions/ADR-0042-graph.md",
			Confidence: 0.8,
			Evidence:   Evidence{Method: "doc_link"},
		},
	})
	assertRole(t, roles, ContextRoleDocOrADR, ConfidenceMedium, true)
	assert.Contains(t, roleFlags(roles, ContextRoleDocOrADR), "adr")
}

func TestClassifyContextRoles_ConfigFromDefinesConfigKey(t *testing.T) {
	roles := ClassifyContextRoles(ClassifyContextInput{
		Target: Node{Kind: NodeKindConfigKey, SourcePath: ".amanmcp.yaml", Name: "search.bm25_weight"},
		LastEdge: Edge{
			Kind:       EdgeKindFileDefinesConfigKey,
			Confidence: 0.9,
			Evidence:   Evidence{Method: "yaml_key"},
		},
	})
	assertRole(t, roles, ContextRoleConfig, ConfidenceHigh, false)
}

func TestClassifyContextRoles_EntrypointHeuristicCmdMain(t *testing.T) {
	roles := ClassifyContextRoles(ClassifyContextInput{
		Target: Node{
			Kind:       NodeKindSymbol,
			SourcePath: "cmd/amanmcp/main.go",
			Name:       "main",
			SymbolKind: "function",
		},
		LastEdge: Edge{Kind: EdgeKindFileDefinesSymbol, Confidence: 1},
	})
	assertRole(t, roles, ContextRoleEntrypoint, ConfidenceMedium, true)
}

func TestClassifyContextRoles_CallerImportProxySymbolSeedUsesFileAnchor(t *testing.T) {
	roles := ClassifyContextRoles(ClassifyContextInput{
		SeedID:         "symbol-seed",
		ImportAnchorID: "file-seed",
		Target:         Node{ID: "file-caller", Kind: NodeKindFile, SourcePath: "internal/mcp/server.go"},
		LastEdge: Edge{
			Kind:       EdgeKindFileImports,
			FromNodeID: "file-caller",
			ToNodeID:   "file-seed",
			Confidence: 0.6,
			Evidence:   Evidence{Method: "go_import", Heuristic: true},
		},
		Path: []PathHop{
			{Edge: Edge{Kind: EdgeKindFileDefinesSymbol, FromNodeID: "file-seed", ToNodeID: "symbol-seed"}},
			{Edge: Edge{Kind: EdgeKindFileImports, FromNodeID: "file-caller", ToNodeID: "file-seed", Evidence: Evidence{Heuristic: true}}},
		},
	})
	assertRole(t, roles, ContextRoleCaller, ConfidenceLow, true)
}

func TestImportAnchorID_DerivesFileFromSymbolPath(t *testing.T) {
	anchor := importAnchorID(Node{ID: "symbol-seed", Kind: NodeKindSymbol}, []PathHop{
		{Edge: Edge{Kind: EdgeKindFileDefinesSymbol, FromNodeID: "file-seed", ToNodeID: "symbol-seed"}},
	})
	assert.Equal(t, "file-seed", anchor)
}

func TestClassifyContextRoles_CallerImportProxyInbound(t *testing.T) {
	roles := ClassifyContextRoles(ClassifyContextInput{
		SeedID: "seed-a",
		Target: Node{ID: "file-b", Kind: NodeKindFile, SourcePath: "internal/mcp/server.go"},
		LastEdge: Edge{
			Kind:       EdgeKindFileImports,
			FromNodeID: "file-b",
			ToNodeID:   "seed-a",
			Confidence: 0.6,
			Evidence:   Evidence{Method: "go_import", Heuristic: true},
		},
	})
	assertRole(t, roles, ContextRoleCaller, ConfidenceLow, true)
	assert.Contains(t, roleFlags(roles, ContextRoleCaller), "import-proxy")
}

func TestClassifyContextRoles_CallerDirection_DependencyNotCaller(t *testing.T) {
	roles := ClassifyContextRoles(ClassifyContextInput{
		SeedID: "seed-a",
		Target: Node{ID: "file-b", Kind: NodeKindFile, SourcePath: "internal/graph/query.go"},
		LastEdge: Edge{
			Kind:       EdgeKindFileImports,
			FromNodeID: "seed-a",
			ToNodeID:   "file-b",
			Confidence: 0.6,
			Evidence:   Evidence{Method: "go_import", Heuristic: true},
		},
	})
	assert.NotContains(t, roleNames(roles), ContextRoleCaller)
}

func TestClassifyContextRoles_CallerPrefersPreciseSymbolCalls(t *testing.T) {
	roles := ClassifyContextRoles(ClassifyContextInput{
		SeedID: "seed-callee",
		Target: Node{ID: "symbol-caller", Kind: NodeKindSymbol, SourcePath: "internal/graph/query.go", Name: "Query"},
		LastEdge: Edge{
			Kind:       EdgeKindSymbolCalls,
			FromNodeID: "symbol-caller",
			ToNodeID:   "seed-callee",
			Confidence: 1,
			Evidence:   Evidence{Method: "scip", Heuristic: false},
			Extractor:  ExtractorSCIPGo,
		},
	})
	assertRole(t, roles, ContextRoleCaller, ConfidenceExact, false)
	assert.NotContains(t, roleFlags(roles, ContextRoleCaller), "import-proxy")
}

func TestClassifyContextRoles_RelatedPMMemoryFromBodyMention(t *testing.T) {
	roles := ClassifyContextRoles(ClassifyContextInput{
		Target: Node{Kind: NodeKindFile, SourcePath: "internal/graph/expand_context.go"},
		LastEdge: Edge{
			Kind:       EdgeKindDocMentionsFile,
			SourcePath: ".aman-pm/backlog/tasks/active/TASK-GRA27-expand-context-mcp-tool.md",
			Confidence: 0.75,
			Evidence:   Evidence{Method: "doc_body_mention"},
		},
	})
	assertRole(t, roles, ContextRoleRelatedPMMemory, ConfidenceMedium, true)
}

func TestClassifyContextRoles_RelatedDocMemoryFromArchiveMention(t *testing.T) {
	roles := ClassifyContextRoles(ClassifyContextInput{
		Target: Node{Kind: NodeKindSymbol, SourcePath: "internal/search/engine.go", Name: "Search"},
		LastEdge: Edge{
			Kind:       EdgeKindDocMentionsSymbol,
			SourcePath: "archive/review_feedback/old-notes.md",
			Confidence: 0.7,
			Evidence:   Evidence{Method: "doc_body_mention"},
		},
	})
	assertRole(t, roles, ContextRoleRelatedDocMemory, ConfidenceMedium, true)
}

func TestClassifyContextRoles_MultiRoleImplementationAndInboundCaller(t *testing.T) {
	roles := ClassifyContextRoles(ClassifyContextInput{
		SeedID: "seed-a",
		Target: Node{ID: "file-b", Kind: NodeKindFile, SourcePath: "internal/graph/service.go"},
		LastEdge: Edge{
			Kind:       EdgeKindFileImports,
			FromNodeID: "file-b",
			ToNodeID:   "seed-a",
			Confidence: 0.6,
			Evidence:   Evidence{Heuristic: true},
		},
		Path: []PathHop{
			{Edge: Edge{Kind: EdgeKindFileDefinesSymbol, FromNodeID: "seed-a", ToNodeID: "file-b", Evidence: Evidence{Heuristic: false}}},
			{Edge: Edge{Kind: EdgeKindFileImports, FromNodeID: "file-b", ToNodeID: "seed-a", Evidence: Evidence{Heuristic: true}}},
		},
	})
	roleNames := roleNames(roles)
	assert.Contains(t, roleNames, ContextRoleImplementation)
	assert.Contains(t, roleNames, ContextRoleCaller)
}

func TestClassifyContextRoles_MergeFlagsOnRepeatedRole(t *testing.T) {
	roles := ClassifyContextRoles(ClassifyContextInput{
		SeedID: "seed-a",
		Target: Node{ID: "file-b", Kind: NodeKindFile, SourcePath: "internal/graph/query.go"},
		LastEdge: Edge{
			Kind:       EdgeKindDocMentionsFile,
			SourcePath: "docs/reference/decisions/ADR-0001.md",
			Confidence: 0.95,
			Evidence:   Evidence{Method: "doc_link"},
		},
		Path: []PathHop{
			{Edge: Edge{Kind: EdgeKindDocMentionsFile, SourcePath: "docs/reference/decisions/ADR-0002.md", Confidence: 0.8, Evidence: Evidence{Method: "doc_link"}}},
		},
	})
	assert.Contains(t, roleFlags(roles, ContextRoleDocOrADR), "adr")
}

func assertRole(t *testing.T, roles []RoleAssignment, want ContextRole, wantConf ConfidenceLabel, wantHeuristic bool) {
	t.Helper()
	for _, role := range roles {
		if role.Role == want {
			assert.Equal(t, wantConf, role.ConfidenceLabel)
			assert.Equal(t, wantHeuristic, role.Heuristic)
			return
		}
	}
	t.Fatalf("missing role %q in %#v", want, roles)
}

func roleNames(roles []RoleAssignment) []ContextRole {
	out := make([]ContextRole, 0, len(roles))
	for _, role := range roles {
		out = append(out, role.Role)
	}
	return out
}

func roleFlags(roles []RoleAssignment, want ContextRole) []string {
	for _, role := range roles {
		if role.Role == want {
			return role.Flags
		}
	}
	return nil
}