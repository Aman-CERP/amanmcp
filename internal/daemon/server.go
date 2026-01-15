package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"
)

// RequestHandler handles incoming RPC requests.
type RequestHandler interface {
	HandleSearch(ctx context.Context, params SearchParams) ([]SearchResult, error)
	GetStatus() StatusResult
}

// Server listens on a Unix socket and handles RPC requests.
type Server struct {
	socketPath string
	listener   net.Listener
	handler    RequestHandler
	started    time.Time

	mu       sync.Mutex
	shutdown bool
	wg       sync.WaitGroup
}

// NewServer creates a new server that listens on the given socket path.
func NewServer(socketPath string) (*Server, error) {
	return &Server{
		socketPath: socketPath,
	}, nil
}

// SetHandler sets the request handler for search operations.
func (s *Server) SetHandler(h RequestHandler) {
	s.handler = h
}

// ListenAndServe starts the server and blocks until context is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	// Clean up any stale socket
	_ = os.Remove(s.socketPath)

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.socketPath, err)
	}
	s.listener = listener
	s.started = time.Now()

	// Clean up socket on exit
	defer func() {
		_ = listener.Close()
		_ = os.Remove(s.socketPath)
	}()

	slog.Info("Server listening", slog.String("socket", s.socketPath))

	// Handle shutdown
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		s.shutdown = true
		s.mu.Unlock()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			s.mu.Lock()
			shutdown := s.shutdown
			s.mu.Unlock()
			if shutdown {
				break
			}
			slog.Error("Accept error", slog.String("error", err.Error()))
			continue
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConnection(ctx, conn)
		}()
	}

	// Wait for active connections to finish
	s.wg.Wait()

	return ctx.Err()
}

// handleConnection processes a single client connection.
func (s *Server) handleConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	// Set read deadline
	if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		slog.Warn("Failed to set connection deadline", slog.String("error", err.Error()))
	}

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	var req Request
	if err := decoder.Decode(&req); err != nil {
		resp := NewErrorResponse("", ErrCodeParseError, "failed to parse request")
		_ = encoder.Encode(resp)
		return
	}

	resp := s.handleRequest(ctx, req)
	_ = encoder.Encode(resp)
}

// handleRequest dispatches a request to the appropriate handler.
func (s *Server) handleRequest(ctx context.Context, req Request) Response {
	switch req.Method {
	case MethodPing:
		return NewSuccessResponse(req.ID, PingResult{Pong: true})

	case MethodStatus:
		status := s.getStatus()
		return NewSuccessResponse(req.ID, status)

	case MethodSearch:
		return s.handleSearch(ctx, req)

	default:
		return NewErrorResponse(req.ID, ErrCodeMethodNotFound, fmt.Sprintf("method not found: %s", req.Method))
	}
}

// handleSearch processes a search request.
func (s *Server) handleSearch(ctx context.Context, req Request) Response {
	if s.handler == nil {
		return NewErrorResponse(req.ID, ErrCodeInternalError, "no search handler configured")
	}

	// Decode params
	paramsData, err := json.Marshal(req.Params)
	if err != nil {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams, "failed to encode params")
	}

	var params SearchParams
	if err := json.Unmarshal(paramsData, &params); err != nil {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams, "failed to decode params")
	}

	if err := params.Validate(); err != nil {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams, err.Error())
	}

	results, err := s.handler.HandleSearch(ctx, params)
	if err != nil {
		return NewErrorResponse(req.ID, ErrCodeSearchFailed, err.Error())
	}

	return NewSuccessResponse(req.ID, results)
}

// getStatus returns the current server status.
func (s *Server) getStatus() StatusResult {
	status := StatusResult{
		Running:        true,
		PID:            os.Getpid(),
		Uptime:         time.Since(s.started).Round(time.Second).String(),
		EmbedderType:   "static",
		EmbedderStatus: "ready",
		ProjectsLoaded: 0,
	}

	if s.handler != nil {
		// Get status from handler
		handlerStatus := s.handler.GetStatus()
		status.EmbedderType = handlerStatus.EmbedderType
		status.EmbedderStatus = handlerStatus.EmbedderStatus
		status.ProjectsLoaded = handlerStatus.ProjectsLoaded
	}

	return status
}

// Close stops the server.
func (s *Server) Close() error {
	s.mu.Lock()
	s.shutdown = true
	s.mu.Unlock()

	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}
