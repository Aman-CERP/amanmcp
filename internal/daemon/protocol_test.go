package daemon

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequest_JSON(t *testing.T) {
	req := Request{
		JSONRPC: "2.0",
		Method:  MethodSearch,
		Params: SearchParams{
			Query:    "test query",
			RootPath: "/path/to/project",
			Limit:    10,
		},
		ID: "req-1",
	}

	// Marshal to JSON
	data, err := json.Marshal(req)
	require.NoError(t, err)

	// Unmarshal back
	var decoded Request
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "2.0", decoded.JSONRPC)
	assert.Equal(t, MethodSearch, decoded.Method)
	assert.Equal(t, "req-1", decoded.ID)
}

func TestResponse_Success(t *testing.T) {
	results := []SearchResult{
		{FilePath: "/test.go", StartLine: 10, Score: 0.95},
	}

	resp := NewSuccessResponse("req-1", results)

	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, "req-1", resp.ID)
	assert.NotNil(t, resp.Result)
	assert.Nil(t, resp.Error)
}

func TestResponse_Error(t *testing.T) {
	resp := NewErrorResponse("req-1", ErrCodeInvalidParams, "invalid query")

	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, "req-1", resp.ID)
	assert.Nil(t, resp.Result)
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeInvalidParams, resp.Error.Code)
	assert.Equal(t, "invalid query", resp.Error.Message)
}

func TestSearchParams_Validate(t *testing.T) {
	tests := []struct {
		name    string
		params  SearchParams
		wantErr bool
	}{
		{
			name: "valid params",
			params: SearchParams{
				Query:    "test",
				RootPath: "/path",
				Limit:    10,
			},
			wantErr: false,
		},
		{
			name: "empty query",
			params: SearchParams{
				Query:    "",
				RootPath: "/path",
			},
			wantErr: true,
		},
		{
			name: "empty root path",
			params: SearchParams{
				Query:    "test",
				RootPath: "",
			},
			wantErr: true,
		},
		{
			name: "negative limit uses default",
			params: SearchParams{
				Query:    "test",
				RootPath: "/path",
				Limit:    -1,
			},
			wantErr: false, // negative limit is corrected to default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.params.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSearchResult_JSON(t *testing.T) {
	result := SearchResult{
		FilePath:  "/path/to/file.go",
		StartLine: 42,
		EndLine:   50,
		Score:     0.89,
		Content:   "func TestSomething() {",
		Language:  "go",
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var decoded SearchResult
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, result.FilePath, decoded.FilePath)
	assert.Equal(t, result.StartLine, decoded.StartLine)
	assert.Equal(t, result.EndLine, decoded.EndLine)
	assert.InDelta(t, result.Score, decoded.Score, 0.001)
	assert.Equal(t, result.Content, decoded.Content)
	assert.Equal(t, result.Language, decoded.Language)
}

func TestStatusResult_JSON(t *testing.T) {
	status := StatusResult{
		Running:        true,
		PID:            12345,
		Uptime:         "1h30m",
		EmbedderType:   "hugot",
		EmbedderStatus: "ready",
		ProjectsLoaded: 3,
	}

	data, err := json.Marshal(status)
	require.NoError(t, err)

	var decoded StatusResult
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, status.Running, decoded.Running)
	assert.Equal(t, status.PID, decoded.PID)
	assert.Equal(t, status.Uptime, decoded.Uptime)
	assert.Equal(t, status.EmbedderType, decoded.EmbedderType)
	assert.Equal(t, status.EmbedderStatus, decoded.EmbedderStatus)
	assert.Equal(t, status.ProjectsLoaded, decoded.ProjectsLoaded)
}

func TestMethodConstants(t *testing.T) {
	// Ensure method constants are defined
	assert.Equal(t, "search", MethodSearch)
	assert.Equal(t, "status", MethodStatus)
	assert.Equal(t, "ping", MethodPing)
}

func TestErrorCodes(t *testing.T) {
	// Standard JSON-RPC error codes
	assert.Equal(t, -32700, ErrCodeParseError)
	assert.Equal(t, -32600, ErrCodeInvalidRequest)
	assert.Equal(t, -32601, ErrCodeMethodNotFound)
	assert.Equal(t, -32602, ErrCodeInvalidParams)
	assert.Equal(t, -32603, ErrCodeInternalError)

	// Custom error codes
	assert.Equal(t, -32001, ErrCodeProjectNotIndexed)
	assert.Equal(t, -32002, ErrCodeSearchFailed)
}
