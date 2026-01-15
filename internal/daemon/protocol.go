package daemon

import "fmt"

// JSON-RPC 2.0 method names.
const (
	MethodSearch = "search"
	MethodStatus = "status"
	MethodPing   = "ping"
)

// Standard JSON-RPC 2.0 error codes.
const (
	ErrCodeParseError     = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternalError  = -32603
)

// Custom error codes for daemon-specific errors.
const (
	ErrCodeProjectNotIndexed = -32001
	ErrCodeSearchFailed      = -32002
)

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
	ID      string `json:"id"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string `json:"jsonrpc"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
	ID      string `json:"id"`
}

// Error represents a JSON-RPC 2.0 error.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// NewSuccessResponse creates a successful response.
func NewSuccessResponse(id string, result any) Response {
	return Response{
		JSONRPC: "2.0",
		Result:  result,
		ID:      id,
	}
}

// NewErrorResponse creates an error response.
func NewErrorResponse(id string, code int, message string) Response {
	return Response{
		JSONRPC: "2.0",
		Error: &Error{
			Code:    code,
			Message: message,
		},
		ID: id,
	}
}

// SearchParams are the parameters for the search method.
type SearchParams struct {
	// Query is the search query (required).
	Query string `json:"query"`

	// RootPath is the project root path (required).
	RootPath string `json:"root_path"`

	// Limit is the maximum number of results (default: 10).
	Limit int `json:"limit,omitempty"`

	// Filter filters by content type: "all", "code", "docs" (default: "all").
	Filter string `json:"filter,omitempty"`

	// Language filters by programming language (optional).
	Language string `json:"language,omitempty"`

	// Scopes filters by path prefixes (optional).
	Scopes []string `json:"scopes,omitempty"`

	// BM25Only forces keyword-only search, skipping semantic search.
	// FEAT-DIM1: Useful when embedder is unavailable or for exact keyword matching.
	BM25Only bool `json:"bm25_only,omitempty"`

	// Explain enables detailed search explanation mode.
	// FEAT-UNIX3: When true, returns ExplainData with search decision details.
	Explain bool `json:"explain,omitempty"`
}

// Validate checks that required fields are present.
func (p *SearchParams) Validate() error {
	if p.Query == "" {
		return fmt.Errorf("query is required")
	}
	if p.RootPath == "" {
		return fmt.Errorf("root_path is required")
	}
	// Correct negative limit to default
	if p.Limit < 0 {
		p.Limit = 10
	}
	return nil
}

// SearchResult represents a single search result.
type SearchResult struct {
	FilePath  string  `json:"file_path"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Score     float64 `json:"score"`
	Content   string  `json:"content"`
	Language  string  `json:"language,omitempty"`

	// FEAT-UNIX3: Explain mode fields (only populated when Explain=true)
	BM25Score float64      `json:"bm25_score,omitempty"`
	VecScore  float64      `json:"vec_score,omitempty"`
	BM25Rank  int          `json:"bm25_rank,omitempty"`
	VecRank   int          `json:"vec_rank,omitempty"`
	Explain   *ExplainData `json:"explain,omitempty"` // Only on first result
}

// ExplainData contains detailed search decision information.
// FEAT-UNIX3: Implements Unix Rule of Transparency for debugging.
type ExplainData struct {
	Query                string   `json:"query"`
	BM25ResultCount      int      `json:"bm25_result_count"`
	VectorResultCount    int      `json:"vector_result_count"`
	BM25Weight           float64  `json:"bm25_weight"`
	SemanticWeight       float64  `json:"semantic_weight"`
	RRFConstant          int      `json:"rrf_constant"`
	BM25Only             bool     `json:"bm25_only,omitempty"`
	DimensionMismatch    bool     `json:"dimension_mismatch,omitempty"`
	MultiQueryDecomposed bool     `json:"multi_query_decomposed,omitempty"`
	SubQueries           []string `json:"sub_queries,omitempty"`
}

// StatusResult contains daemon status information.
type StatusResult struct {
	Running        bool   `json:"running"`
	PID            int    `json:"pid"`
	Uptime         string `json:"uptime"`
	EmbedderType   string `json:"embedder_type"`
	EmbedderStatus string `json:"embedder_status"` // "ready", "recovering", "fallback"
	ProjectsLoaded int    `json:"projects_loaded"`
}

// PingResult is the response to a ping request.
type PingResult struct {
	Pong bool `json:"pong"`
}
