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

// serverTestSocketPath creates a unique socket path for server tests.
func serverTestSocketPath(t *testing.T) string {
	t.Helper()
	socketPath := filepath.Join("/tmp", fmt.Sprintf("amanmcp-server-test-%d.sock", time.Now().UnixNano()))
	t.Cleanup(func() { os.Remove(socketPath) })
	return socketPath
}

func TestNewServer(t *testing.T) {
	socketPath := serverTestSocketPath(t)

	srv, err := NewServer(socketPath)
	require.NoError(t, err)
	assert.NotNil(t, srv)
	assert.Equal(t, socketPath, srv.socketPath)
}

func TestServer_ListenAndServe(t *testing.T) {
	socketPath := serverTestSocketPath(t)

	srv, err := NewServer(socketPath)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe(ctx)
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Socket should exist
	_, err = os.Stat(socketPath)
	require.NoError(t, err)

	// Cancel and wait for server to stop
	cancel()
	select {
	case err := <-errCh:
		// Context cancelled is expected
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop")
	}
}

func TestServer_HandlePing(t *testing.T) {
	socketPath := serverTestSocketPath(t)

	srv, err := NewServer(socketPath)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server
	go func() {
		_ = srv.ListenAndServe(ctx)
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Connect and send ping
	conn, err := net.Dial("unix", socketPath)
	require.NoError(t, err)
	defer conn.Close()

	req := Request{
		JSONRPC: "2.0",
		Method:  MethodPing,
		ID:      "test-1",
	}

	encoder := json.NewEncoder(conn)
	err = encoder.Encode(req)
	require.NoError(t, err)

	// Read response
	decoder := json.NewDecoder(conn)
	var resp Response
	err = decoder.Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, "test-1", resp.ID)
	assert.Nil(t, resp.Error)
}

func TestServer_HandleUnknownMethod(t *testing.T) {
	socketPath := serverTestSocketPath(t)

	srv, err := NewServer(socketPath)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server
	go func() {
		_ = srv.ListenAndServe(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("unix", socketPath)
	require.NoError(t, err)
	defer conn.Close()

	req := Request{
		JSONRPC: "2.0",
		Method:  "unknownMethod",
		ID:      "test-2",
	}

	encoder := json.NewEncoder(conn)
	err = encoder.Encode(req)
	require.NoError(t, err)

	decoder := json.NewDecoder(conn)
	var resp Response
	err = decoder.Decode(&resp)
	require.NoError(t, err)

	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeMethodNotFound, resp.Error.Code)
}

func TestServer_HandleStatus(t *testing.T) {
	socketPath := serverTestSocketPath(t)

	srv, err := NewServer(socketPath)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = srv.ListenAndServe(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("unix", socketPath)
	require.NoError(t, err)
	defer conn.Close()

	req := Request{
		JSONRPC: "2.0",
		Method:  MethodStatus,
		ID:      "test-3",
	}

	encoder := json.NewEncoder(conn)
	err = encoder.Encode(req)
	require.NoError(t, err)

	decoder := json.NewDecoder(conn)
	var resp Response
	err = decoder.Decode(&resp)
	require.NoError(t, err)

	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)
}

func TestServer_CleansUpSocket(t *testing.T) {
	socketPath := serverTestSocketPath(t)

	srv, err := NewServer(socketPath)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	// Socket should exist
	_, err = os.Stat(socketPath)
	require.NoError(t, err)

	// Stop server
	cancel()
	<-errCh

	// Give cleanup time
	time.Sleep(50 * time.Millisecond)

	// Socket should be removed
	_, err = os.Stat(socketPath)
	assert.True(t, os.IsNotExist(err), "socket should be cleaned up")
}

func TestServer_ConcurrentConnections(t *testing.T) {
	socketPath := serverTestSocketPath(t)

	srv, err := NewServer(socketPath)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = srv.ListenAndServe(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	// Send multiple concurrent requests
	const numClients = 5
	done := make(chan bool, numClients)

	for i := 0; i < numClients; i++ {
		go func(id int) {
			conn, err := net.Dial("unix", socketPath)
			if err != nil {
				done <- false
				return
			}
			defer conn.Close()

			req := Request{
				JSONRPC: "2.0",
				Method:  MethodPing,
				ID:      fmt.Sprintf("client-%d", id),
			}

			encoder := json.NewEncoder(conn)
			if err := encoder.Encode(req); err != nil {
				done <- false
				return
			}

			decoder := json.NewDecoder(conn)
			var resp Response
			if err := decoder.Decode(&resp); err != nil {
				done <- false
				return
			}

			done <- resp.Error == nil
		}(i)
	}

	// Wait for all clients
	successCount := 0
	for i := 0; i < numClients; i++ {
		if <-done {
			successCount++
		}
	}

	assert.Equal(t, numClients, successCount, "all clients should succeed")
}
