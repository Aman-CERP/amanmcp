package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"time"
)

// Client connects to the daemon for search operations.
type Client struct {
	socketPath string
	timeout    time.Duration
	requestID  atomic.Uint64
}

// NewClient creates a new daemon client.
func NewClient(cfg Config) *Client {
	return &Client{
		socketPath: cfg.SocketPath,
		timeout:    cfg.Timeout,
	}
}

// Connect establishes a connection to the daemon.
func (c *Client) Connect() (net.Conn, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, c.timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	return conn, nil
}

// IsRunning checks if the daemon is accepting connections.
func (c *Client) IsRunning() bool {
	conn, err := c.Connect()
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Ping checks if the daemon is responsive.
func (c *Client) Ping(ctx context.Context) error {
	conn, err := c.Connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	// Set deadline from context or timeout
	deadline := time.Now().Add(c.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("failed to set deadline: %w", err)
	}

	req := Request{
		JSONRPC: "2.0",
		Method:  MethodPing,
		ID:      c.nextID(),
	}

	if err := c.send(conn, req); err != nil {
		return err
	}

	resp, err := c.receive(conn)
	if err != nil {
		return err
	}

	if resp.Error != nil {
		return fmt.Errorf("ping failed: %s", resp.Error.Message)
	}

	return nil
}

// Search sends a search request to the daemon.
func (c *Client) Search(ctx context.Context, params SearchParams) ([]SearchResult, error) {
	if err := params.Validate(); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	conn, err := c.Connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// Set deadline from context or timeout
	deadline := time.Now().Add(c.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("failed to set deadline: %w", err)
	}

	req := Request{
		JSONRPC: "2.0",
		Method:  MethodSearch,
		Params:  params,
		ID:      c.nextID(),
	}

	if err := c.send(conn, req); err != nil {
		return nil, err
	}

	resp, err := c.receive(conn)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("search failed: %s (code: %d)", resp.Error.Message, resp.Error.Code)
	}

	// Decode results
	resultData, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var results []SearchResult
	if err := json.Unmarshal(resultData, &results); err != nil {
		return nil, fmt.Errorf("failed to decode results: %w", err)
	}

	return results, nil
}

// Status retrieves daemon status.
func (c *Client) Status(ctx context.Context) (*StatusResult, error) {
	conn, err := c.Connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// Set deadline from context or timeout
	deadline := time.Now().Add(c.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("failed to set deadline: %w", err)
	}

	req := Request{
		JSONRPC: "2.0",
		Method:  MethodStatus,
		ID:      c.nextID(),
	}

	if err := c.send(conn, req); err != nil {
		return nil, err
	}

	resp, err := c.receive(conn)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("status failed: %s", resp.Error.Message)
	}

	// Decode status
	resultData, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var status StatusResult
	if err := json.Unmarshal(resultData, &status); err != nil {
		return nil, fmt.Errorf("failed to decode status: %w", err)
	}

	return &status, nil
}

// send encodes and writes a request to the connection.
func (c *Client) send(conn net.Conn, req Request) error {
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	return nil
}

// receive reads and decodes a response from the connection.
func (c *Client) receive(conn net.Conn) (*Response, error) {
	decoder := json.NewDecoder(conn)
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to receive response: %w", err)
	}
	return &resp, nil
}

// nextID generates a unique request ID.
func (c *Client) nextID() string {
	id := c.requestID.Add(1)
	return fmt.Sprintf("req-%d", id)
}
