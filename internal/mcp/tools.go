package mcp

// SearchCodeInput defines the input schema for the search_code tool.
type SearchCodeInput struct {
	Query      string   `json:"query" jsonschema:"the code search query to execute"`
	Language   string   `json:"language,omitempty" jsonschema:"filter by programming language (go, typescript, python)"`
	SymbolType string   `json:"symbol_type,omitempty" jsonschema:"filter by symbol type: function, class, interface, type, method, or any"`
	Limit      int      `json:"limit,omitempty" jsonschema:"maximum number of results, default 10"`
	Scope      []string `json:"scope,omitempty" jsonschema:"filter by path prefixes (OR logic)"`
}

// SearchDocsInput defines the input schema for the search_docs tool.
type SearchDocsInput struct {
	Query string   `json:"query" jsonschema:"the documentation search query to execute"`
	Limit int      `json:"limit,omitempty" jsonschema:"maximum number of results, default 10"`
	Scope []string `json:"scope,omitempty" jsonschema:"filter by path prefixes (OR logic)"`
}

// IndexStatusInput defines the input schema for the index_status tool (no parameters).
type IndexStatusInput struct{}

// IndexStatusOutput defines the output schema for the index_status tool.
type IndexStatusOutput struct {
	Project    ProjectInfo       `json:"project"`
	Stats      IndexStats        `json:"stats"`
	Embeddings EmbeddingInfo     `json:"embeddings"`
	Indexing   *IndexingProgress `json:"indexing,omitempty"` // Present during background indexing
}

// IndexingProgress contains information about ongoing background indexing.
type IndexingProgress struct {
	Status         string  `json:"status"`                     // "indexing", "ready", or "error"
	Stage          string  `json:"stage,omitempty"`            // "scanning", "chunking", "embedding", "indexing"
	FilesTotal     int     `json:"files_total"`                // Total files to process
	FilesProcessed int     `json:"files_processed"`            // Files processed so far
	ChunksIndexed  int     `json:"chunks_indexed"`             // Chunks indexed so far
	ProgressPct    float64 `json:"progress_pct"`               // Progress percentage (0-100)
	ElapsedSeconds int     `json:"elapsed_seconds"`            // Time since indexing started
	ErrorMessage   string  `json:"error_message,omitempty"`    // Error message if status is "error"
}

// ProjectInfo contains information about the indexed project.
type ProjectInfo struct {
	Name     string `json:"name"`
	RootPath string `json:"root_path"`
	Type     string `json:"type"`
}

// IndexStats contains statistics about the index.
type IndexStats struct {
	FileCount      int    `json:"file_count"`
	ChunkCount     int    `json:"chunk_count"`
	IndexSizeBytes int64  `json:"index_size_bytes"`
	LastIndexed    string `json:"last_indexed"`
}

// EmbeddingInfo contains information about the embedding configuration.
type EmbeddingInfo struct {
	// Config values
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Status   string `json:"status"`

	// Runtime state - allows AI clients to adjust search strategy
	ActualProvider   string `json:"actual_provider"`    // "hugot" or "static"
	ActualModel      string `json:"actual_model"`       // e.g., "embeddinggemma-300m" or "static"
	Dimensions       int    `json:"dimensions"`         // 768 (hugot) or 256 (static)
	IsFallbackActive bool   `json:"is_fallback_active"` // true if using static fallback
	SemanticQuality  string `json:"semantic_quality"`   // "high" (hugot) or "low" (static)
}
