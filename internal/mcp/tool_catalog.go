package mcp

import mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

const legacyCallToolDeprecationPrefix = "DEPRECATED compatibility wrapper: "

func sdkRegisteredTools() []*mcpsdk.Tool {
	return []*mcpsdk.Tool{
		{
			Name:        "search",
			Description: "Primary search tool. Instantly finds code and documentation using a full-codebase index. Use this for 95% of your search tasks - faster and smarter than grep. Understands code semantics, not just keywords.",
		},
		{
			Name:        "search_code",
			Description: "Code-specialized search. Finds functions, classes, and implementations by meaning, not just text matching. Use when you need to understand HOW something is implemented. Supports language and symbol type filtering.",
		},
		{
			Name:        "search_docs",
			Description: "Documentation search with context. Finds architecture decisions, design rationale, and guides. Preserves section hierarchy so you understand WHERE in the doc structure a match appears.",
		},
		{
			Name:        "index_status",
			Description: "Check if the codebase index is ready and which embedder is active. Use before searching to verify the index is complete.",
		},
		{
			Name:        "graph.query",
			Description: "Graph-native relationship query with find_references, explain_symbol, and impact_analysis modes. Resolves the subject before traversing and reports the outcome in `resolution`: `resolved` (one unambiguous subject — `results` holds bounded role-labeled evidence with graph path hints, source paths, confidence labels, and heuristic flags), `disambiguation_required` (the subject matched several distinct nodes — `results` is empty and `candidates` lists up to a bounded number of them, each with its qualified name, kind, source path, and line so you can re-query a specific subject; a `graph_candidates_truncated` warning signals when more matched than were returned), or `subject_not_found` (no match — `candidates` carries near-miss hints). Optional `subject_type` selects the resolver: auto (default), path, symbol, package, or result_id. Optional traversal budget overrides within policy: `max_nodes`, `max_per_edge_kind`, `max_tokens`, and `max_depth` (multi-hop only). Budget exhaustion returns partial `results` plus `traversal_budget_exhausted` warnings with structured `budget_reason` and `budget_limit`. Package resolution tries exact key/name, exact directory, then case-folded key/name/directory; ambiguous matches return candidates. Examples: {\"subject_type\":\"auto\",\"query\":\"QueryService\"}; {\"subject_type\":\"path\",\"query\":\"internal/graph/query.go\"}; {\"subject_type\":\"symbol\",\"query\":\"QueryService\"}; {\"subject_type\":\"package\",\"query\":\"internal/graph#graph\"}; {\"subject_type\":\"result_id\",\"query\":\"node:symbol:project-1:internal/graph/query.go#Query:1\"}. result_id v1 accepts stable graph node IDs only, not public search-result hashes. Also returns status and warnings.",
		},
		{
			Name:        "expand_context",
			Description: "Graph-native context pack assembly. Resolves a seed (search result id, symbol, or path), expands its multi-hop graph neighborhood by node-id traversal, and returns a role-labeled context pack with an explicit GraphPath on every item tracing back to the seed. Seed resolution reuses graph.query subject handling: auto (default), path, symbol, or result_id. On success `pack` holds bounded items with roles (implementation, test, doc_or_adr, config, entrypoint, caller, related_pm_memory, related_doc_memory), source paths, confidence labels, heuristic flags, and hydrated chunk content when available. Role notes: `caller` uses inbound import-proxy until precise `symbol_calls` edges land; `entrypoint`, `related_pm_memory`, and `related_doc_memory` use layout heuristics (cmd/, .aman-pm/, archive/) and may be empty on repos that do not match. On `disambiguation_required` or `subject_not_found`, `pack` is empty and `candidates` carries competing subjects or near-miss hints — the tool never guesses. Degraded or empty graphs return structured warnings. Examples: {\"seed_type\":\"symbol\",\"seed\":\"NewQueryService\"}; {\"seed_type\":\"path\",\"seed\":\"internal/graph/query.go\"}; {\"seed_type\":\"result_id\",\"seed\":\"node:chunk:project-1:internal/graph/query.go#chunk:1\"}.",
		},
	}
}

func legacyCallToolInfo(tool *mcpsdk.Tool) ToolInfo {
	return ToolInfo{
		Name:        tool.Name,
		Description: legacyCallToolDeprecationPrefix + tool.Description,
		Meta:        legacyCallToolDeprecationMeta(tool.Name),
	}
}

func legacyCallToolDeprecationMeta(toolName string) map[string]any {
	return map[string]any{
		"deprecated":         true,
		"deprecation_notice": "Server.CallTool returns legacy markdown for compatibility. Agent integrations should use the structured SDK registered tool instead.",
		"removal_target":     "post-v1.0.0",
		"replacement":        "SDK registered tool " + toolName,
	}
}
