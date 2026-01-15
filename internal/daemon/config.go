// Package daemon provides a background service for fast CLI search.
// The daemon keeps the embedder loaded in memory, allowing CLI search
// commands to connect via Unix socket instead of reinitializing the
// embedder on every invocation.
package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Config holds configuration for the daemon service.
type Config struct {
	// SocketPath is the Unix domain socket path for IPC.
	// Default: ~/.amanmcp/daemon.sock
	SocketPath string

	// PIDPath is the file path for storing the daemon's process ID.
	// Default: ~/.amanmcp/daemon.pid
	PIDPath string

	// Timeout is the maximum duration for client-daemon communication.
	// Default: 30s
	Timeout time.Duration

	// ShutdownGracePeriod is the time to wait for graceful shutdown.
	// Default: 10s
	ShutdownGracePeriod time.Duration

	// MaxProjects is the maximum number of projects to keep loaded.
	// Uses LRU eviction when exceeded.
	// Default: 5
	MaxProjects int

	// AutoStart enables auto-starting daemon from CLI if not running.
	// Default: false
	AutoStart bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}

	amanmcpDir := filepath.Join(home, ".amanmcp")

	return Config{
		SocketPath:          filepath.Join(amanmcpDir, "daemon.sock"),
		PIDPath:             filepath.Join(amanmcpDir, "daemon.pid"),
		Timeout:             30 * time.Second,
		ShutdownGracePeriod: 10 * time.Second,
		MaxProjects:         5,
		AutoStart:           false,
	}
}

// Validate checks that the configuration is valid.
func (c Config) Validate() error {
	if c.SocketPath == "" {
		return fmt.Errorf("socket path cannot be empty")
	}
	if c.PIDPath == "" {
		return fmt.Errorf("PID path cannot be empty")
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	if c.ShutdownGracePeriod <= 0 {
		return fmt.Errorf("shutdown grace period must be positive")
	}
	if c.MaxProjects <= 0 {
		return fmt.Errorf("max projects must be positive")
	}
	return nil
}

// EnsureDir creates the directory for socket and PID files if it doesn't exist.
func (c Config) EnsureDir() error {
	// Get directory from socket path
	socketDir := filepath.Dir(c.SocketPath)
	if err := os.MkdirAll(socketDir, 0755); err != nil {
		return fmt.Errorf("failed to create socket directory: %w", err)
	}

	// Get directory from PID path (might be different)
	pidDir := filepath.Dir(c.PIDPath)
	if pidDir != socketDir {
		if err := os.MkdirAll(pidDir, 0755); err != nil {
			return fmt.Errorf("failed to create PID directory: %w", err)
		}
	}

	return nil
}
