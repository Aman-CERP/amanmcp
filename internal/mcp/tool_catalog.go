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
			Description: "Graph-native relationship query with find_references, explain_symbol, and impact_analysis modes. Returns bounded role-labeled evidence, graph path hints, source paths, confidence labels, status, and warnings.",
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
