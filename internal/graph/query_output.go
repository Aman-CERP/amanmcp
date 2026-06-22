package graph

// QueryToolOutput is the shared graph.query envelope exposed to MCP clients and
// reused by direct graph eval so both surfaces score the same contract.
type QueryToolOutput struct {
	Available  bool            `json:"available"`
	Status     GraphStatus     `json:"status"`
	Degraded   bool            `json:"degraded"`
	Mode       string          `json:"mode"`
	Query      string          `json:"query"`
	Resolution string          `json:"resolution,omitempty"`
	Results    []QueryResult   `json:"results,omitempty"`
	Candidates []Candidate     `json:"candidates,omitempty"`
	Warnings   []StatusWarning `json:"warnings,omitempty"`
}

// NewQueryToolOutput projects a storage/query response into the graph.query
// tool envelope.
func NewQueryToolOutput(response QueryResponse) QueryToolOutput {
	return QueryToolOutput{
		Available:  QueryAvailable(response.Status),
		Status:     response.Status,
		Degraded:   response.Degraded,
		Mode:       response.Mode,
		Query:      response.Query,
		Resolution: response.Resolution,
		Results:    response.Results,
		Candidates: response.Candidates,
		Warnings:   response.Warnings,
	}
}

// NewUnavailableQueryToolOutput returns the graceful graph.query envelope used
// when no graph repository is wired into an MCP server.
func NewUnavailableQueryToolOutput(mode, query, message string) QueryToolOutput {
	return QueryToolOutput{
		Available: false,
		Status:    GraphStatusUnavailable,
		Degraded:  true,
		Mode:      normalizeQueryMode(mode),
		Query:     query,
		Warnings: []StatusWarning{{
			Code:    WarningGraphUnavailable,
			Message: message,
		}},
	}
}
