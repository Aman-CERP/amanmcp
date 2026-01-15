package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testSocketPath creates a unique socket path that's short enough for Unix sockets.
func testSocketPath(t *testing.T) string {
	t.Helper()
	socketPath := filepath.Join("/tmp", fmt.Sprintf("amanmcp-test-%d.sock", time.Now().UnixNano()))
	t.Cleanup(func() { os.Remove(socketPath) })
	return socketPath
}

func TestNewClient(t *testing.T) {
	cfg := DefaultConfig()
	client := NewClient(cfg)

	assert.NotNil(t, client)
	assert.Equal(t, cfg.SocketPath, client.socketPath)
	assert.Equal(t, cfg.Timeout, client.timeout)
}

func TestClient_IsRunning_NoSocket(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := Config{
		SocketPath: filepath.Join(tmpDir, "nonexistent.sock"),
		Timeout:    5 * time.Second,
	}

	client := NewClient(cfg)
	assert.False(t, client.IsRunning(), "Should return false when socket doesn't exist")
}

func TestClient_IsRunning_WithSocket(t *testing.T) {
	socketPath := testSocketPath(t)

	// Start a test server
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	cfg := Config{
		SocketPath: socketPath,
		Timeout:    5 * time.Second,
	}

	client := NewClient(cfg)
	assert.True(t, client.IsRunning(), "Should return true when socket is listening")
}

func TestClient_Ping_Success(t *testing.T) {
	socketPath := testSocketPath(t)

	// Start a mock server that responds to ping
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read request
		decoder := json.NewDecoder(conn)
		var req Request
		if err := decoder.Decode(&req); err != nil {
			return
		}

		// Send ping response
		resp := NewSuccessResponse(req.ID, PingResult{Pong: true})
		encoder := json.NewEncoder(conn)
		_ = encoder.Encode(resp)
	}()

	cfg := Config{
		SocketPath: socketPath,
		Timeout:    5 * time.Second,
	}

	client := NewClient(cfg)
	ctx := context.Background()

	err = client.Ping(ctx)
	require.NoError(t, err)
}

func TestClient_Search_Success(t *testing.T) {
	socketPath := testSocketPath(t)

	expectedResults := []SearchResult{
		{FilePath: "/test.go", StartLine: 10, Score: 0.95, Content: "test content"},
	}

	// Start a mock server that responds to search
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read request
		decoder := json.NewDecoder(conn)
		var req Request
		if err := decoder.Decode(&req); err != nil {
			return
		}

		// Send search results
		resp := NewSuccessResponse(req.ID, expectedResults)
		encoder := json.NewEncoder(conn)
		_ = encoder.Encode(resp)
	}()

	cfg := Config{
		SocketPath: socketPath,
		Timeout:    5 * time.Second,
	}

	client := NewClient(cfg)
	ctx := context.Background()

	params := SearchParams{
		Query:    "test",
		RootPath: "/path/to/project",
		Limit:    10,
	}

	results, err := client.Search(ctx, params)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "/test.go", results[0].FilePath)
	assert.Equal(t, 10, results[0].StartLine)
	assert.InDelta(t, 0.95, results[0].Score, 0.001)
}

func TestClient_Search_Error(t *testing.T) {
	socketPath := testSocketPath(t)

	// Start a mock server that returns an error
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read request
		decoder := json.NewDecoder(conn)
		var req Request
		if err := decoder.Decode(&req); err != nil {
			return
		}

		// Send error response
		resp := NewErrorResponse(req.ID, ErrCodeProjectNotIndexed, "project not indexed")
		encoder := json.NewEncoder(conn)
		_ = encoder.Encode(resp)
	}()

	cfg := Config{
		SocketPath: socketPath,
		Timeout:    5 * time.Second,
	}

	client := NewClient(cfg)
	ctx := context.Background()

	params := SearchParams{
		Query:    "test",
		RootPath: "/nonexistent",
	}

	_, err = client.Search(ctx, params)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project not indexed")
}

func TestClient_Status_Success(t *testing.T) {
	socketPath := testSocketPath(t)

	expectedStatus := StatusResult{
		Running:        true,
		PID:            12345,
		Uptime:         "5m",
		EmbedderType:   "hugot",
		EmbedderStatus: "ready",
		ProjectsLoaded: 2,
	}

	// Start a mock server
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		decoder := json.NewDecoder(conn)
		var req Request
		if err := decoder.Decode(&req); err != nil {
			return
		}

		resp := NewSuccessResponse(req.ID, expectedStatus)
		encoder := json.NewEncoder(conn)
		_ = encoder.Encode(resp)
	}()

	cfg := Config{
		SocketPath: socketPath,
		Timeout:    5 * time.Second,
	}

	client := NewClient(cfg)
	ctx := context.Background()

	status, err := client.Status(ctx)
	require.NoError(t, err)
	assert.True(t, status.Running)
	assert.Equal(t, 12345, status.PID)
	assert.Equal(t, "hugot", status.EmbedderType)
}

func TestClient_Connect_Timeout(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "nonexistent.sock")

	cfg := Config{
		SocketPath: socketPath,
		Timeout:    100 * time.Millisecond, // Short timeout
	}

	client := NewClient(cfg)

	_, err := client.Connect()
	require.Error(t, err)
}
