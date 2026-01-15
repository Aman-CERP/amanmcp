// Package lifecycle provides Ollama lifecycle management for zero-config UX.
// It handles detection, startup, model pulling, and health checking.
package lifecycle

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Constants for Ollama lifecycle management
const (
	// DefaultHost is the default Ollama API endpoint
	DefaultHost = "http://localhost:11434"

	// DefaultModel is the recommended embedding model
	DefaultModel = "qwen3-embedding:0.6b"

	// StartupTimeout is how long to wait for Ollama to start
	StartupTimeout = 30 * time.Second

	// ReadyPollInterval is the initial polling interval for WaitForReady
	ReadyPollInterval = 100 * time.Millisecond

	// MaxReadyPollInterval caps exponential backoff
	MaxReadyPollInterval = 2 * time.Second

	// PullTimeout is how long to wait for model pull (large models can take a while)
	PullTimeout = 10 * time.Minute
)

// OllamaManager handles Ollama lifecycle operations
type OllamaManager struct {
	host    string
	client  *http.Client
	timeout time.Duration

	// For testing: override command execution
	execCommand func(name string, args ...string) *exec.Cmd
	lookPath    func(file string) (string, error)
	fileExists  func(path string) bool
}

// OllamaStatus represents the current state of Ollama
type OllamaStatus struct {
	Installed     bool
	InstalledPath string // Path to ollama binary or app
	Running       bool
	Models        []string // Available models
	HasModel      bool     // Target model is available
	TargetModel   string
}

// PullProgress represents model pull progress
type PullProgress struct {
	Status    string
	Digest    string
	Total     int64
	Completed int64
	Percent   float64
}

// EnsureOpts configures EnsureReady behavior
type EnsureOpts struct {
	// AutoStart enables automatic Ollama startup (default: true)
	AutoStart bool
	// AutoPull enables automatic model pulling (default: true)
	AutoPull bool
	// ProgressFunc receives pull progress updates
	ProgressFunc func(PullProgress)
	// Stdout for progress output (default: os.Stdout)
	Stdout io.Writer
	// Stderr for error output (default: os.Stderr)
	Stderr io.Writer
}

// DefaultEnsureOpts returns sensible defaults
func DefaultEnsureOpts() EnsureOpts {
	return EnsureOpts{
		AutoStart: true,
		AutoPull:  true,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
	}
}

// NewOllamaManager creates a new lifecycle manager
func NewOllamaManager() *OllamaManager {
	return NewOllamaManagerWithHost(DefaultHost)
}

// NewOllamaManagerWithHost creates a manager with custom host
func NewOllamaManagerWithHost(host string) *OllamaManager {
	if host == "" {
		host = DefaultHost
	}

	// Check for environment override
	if envHost := os.Getenv("AMANMCP_OLLAMA_HOST"); envHost != "" {
		host = envHost
	}

	return &OllamaManager{
		host: host,
		client: &http.Client{
			Timeout: 5 * time.Second, // Short timeout for health checks
		},
		timeout:     StartupTimeout,
		execCommand: exec.Command,
		lookPath:    exec.LookPath,
		fileExists:  fileExists,
	}
}

// fileExists checks if a path exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Host returns the configured Ollama host
func (m *OllamaManager) Host() string {
	return m.host
}

// IsInstalled checks if Ollama is installed on the system
func (m *OllamaManager) IsInstalled() (bool, string, error) {
	// Check for ollama CLI in PATH
	if path, err := m.lookPath("ollama"); err == nil {
		return true, path, nil
	}

	// macOS: Check for Ollama.app
	if runtime.GOOS == "darwin" {
		appPaths := []string{
			"/Applications/Ollama.app",
			filepath.Join(os.Getenv("HOME"), "Applications", "Ollama.app"),
		}
		for _, p := range appPaths {
			if m.fileExists(p) {
				return true, p, nil
			}
		}
	}

	// Linux: Check common installation paths
	if runtime.GOOS == "linux" {
		paths := []string{
			"/usr/local/bin/ollama",
			"/usr/bin/ollama",
			filepath.Join(os.Getenv("HOME"), ".local", "bin", "ollama"),
		}
		for _, p := range paths {
			if m.fileExists(p) {
				return true, p, nil
			}
		}
	}

	return false, "", nil
}

// IsRunning checks if Ollama API is responding
func (m *OllamaManager) IsRunning() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.host+"/api/tags", nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		// Connection refused or timeout = not running
		return false, nil
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

// ListModels returns available models from Ollama
func (m *OllamaManager) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.host+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]string, len(result.Models))
	for i, m := range result.Models {
		models[i] = m.Name
	}
	return models, nil
}

// HasModel checks if a specific model is available
func (m *OllamaManager) HasModel(ctx context.Context, model string) (bool, error) {
	models, err := m.ListModels(ctx)
	if err != nil {
		return false, err
	}

	// Normalize model name for comparison
	modelLower := strings.ToLower(model)
	modelBase := strings.Split(modelLower, ":")[0]

	for _, available := range models {
		availableLower := strings.ToLower(available)
		availableBase := strings.Split(availableLower, ":")[0]

		// Match exact name or base name
		if availableLower == modelLower || availableBase == modelBase {
			return true, nil
		}
	}

	return false, nil
}

// Status returns comprehensive Ollama status
func (m *OllamaManager) Status(ctx context.Context, targetModel string) (*OllamaStatus, error) {
	status := &OllamaStatus{
		TargetModel: targetModel,
	}

	// Check installation
	installed, path, err := m.IsInstalled()
	if err != nil {
		return nil, fmt.Errorf("failed to check installation: %w", err)
	}
	status.Installed = installed
	status.InstalledPath = path

	// Check if running
	running, err := m.IsRunning()
	if err != nil {
		return nil, fmt.Errorf("failed to check if running: %w", err)
	}
	status.Running = running

	// If running, check models
	if running {
		models, err := m.ListModels(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list models: %w", err)
		}
		status.Models = models

		hasModel, err := m.HasModel(ctx, targetModel)
		if err != nil {
			return nil, fmt.Errorf("failed to check model: %w", err)
		}
		status.HasModel = hasModel
	}

	return status, nil
}

// Start attempts to start Ollama
func (m *OllamaManager) Start() error {
	installed, path, err := m.IsInstalled()
	if err != nil {
		return fmt.Errorf("failed to check installation: %w", err)
	}
	if !installed {
		return fmt.Errorf("ollama is not installed")
	}

	// Check if already running
	running, _ := m.IsRunning()
	if running {
		return nil // Already running
	}

	// Platform-specific startup
	switch runtime.GOOS {
	case "darwin":
		return m.startMacOS(path)
	case "linux":
		return m.startLinux(path)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// startMacOS starts Ollama on macOS
func (m *OllamaManager) startMacOS(path string) error {
	// Prefer opening the app if it's an .app bundle
	if strings.HasSuffix(path, ".app") {
		cmd := m.execCommand("open", "-a", "Ollama")
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to open Ollama.app: %w", err)
		}
		return nil
	}

	// Try opening via app name (if installed but path is the CLI)
	if m.fileExists("/Applications/Ollama.app") {
		cmd := m.execCommand("open", "-a", "Ollama")
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to open Ollama.app: %w", err)
		}
		return nil
	}

	// Fall back to CLI
	return m.startOllamaServe(path)
}

// startLinux starts Ollama on Linux
func (m *OllamaManager) startLinux(path string) error {
	// Try systemd first
	cmd := m.execCommand("systemctl", "is-active", "--quiet", "ollama")
	if err := cmd.Run(); err == nil {
		// Service exists, try to start it
		cmd = m.execCommand("systemctl", "start", "ollama")
		if err := cmd.Run(); err != nil {
			// If systemctl start fails, try user service
			cmd = m.execCommand("systemctl", "--user", "start", "ollama")
			if err := cmd.Run(); err != nil {
				// Fall back to direct serve
				return m.startOllamaServe(path)
			}
		}
		return nil
	}

	// No systemd service, start directly
	return m.startOllamaServe(path)
}

// startOllamaServe starts ollama serve in background
func (m *OllamaManager) startOllamaServe(path string) error {
	cmd := m.execCommand(path, "serve")
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ollama serve: %w", err)
	}

	// Don't wait for the process - it runs in background
	// Release the process so it doesn't become a zombie
	go func() {
		_ = cmd.Wait()
	}()

	return nil
}

// WaitForReady polls until Ollama is responding or timeout
func (m *OllamaManager) WaitForReady(ctx context.Context, timeout time.Duration) error {
	if timeout == 0 {
		timeout = StartupTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	interval := ReadyPollInterval
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for Ollama to start: %w", ctx.Err())
		default:
		}

		running, _ := m.IsRunning()
		if running {
			return nil
		}

		// Exponential backoff
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for Ollama to start: %w", ctx.Err())
		case <-time.After(interval):
		}

		interval *= 2
		if interval > MaxReadyPollInterval {
			interval = MaxReadyPollInterval
		}
	}
}

// PullModel pulls a model with progress reporting
func (m *OllamaManager) PullModel(ctx context.Context, model string, progressFunc func(PullProgress)) error {
	// Check if model already exists
	hasModel, err := m.HasModel(ctx, model)
	if err != nil {
		return fmt.Errorf("failed to check model: %w", err)
	}
	if hasModel {
		return nil // Already have it
	}

	// Use streaming pull API
	reqBody := struct {
		Name   string `json:"name"`
		Stream bool   `json:"stream"`
	}{
		Name:   model,
		Stream: true,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.host+"/api/pull", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Use longer timeout client for pull
	pullClient := &http.Client{Timeout: 0} // No timeout - streaming
	resp, err := pullClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to start pull: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pull failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse streaming response
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		var progress struct {
			Status    string `json:"status"`
			Digest    string `json:"digest"`
			Total     int64  `json:"total"`
			Completed int64  `json:"completed"`
		}
		if err := json.Unmarshal([]byte(line), &progress); err != nil {
			continue // Skip malformed lines
		}

		if progressFunc != nil {
			percent := 0.0
			if progress.Total > 0 {
				percent = float64(progress.Completed) / float64(progress.Total) * 100
			}
			progressFunc(PullProgress{
				Status:    progress.Status,
				Digest:    progress.Digest,
				Total:     progress.Total,
				Completed: progress.Completed,
				Percent:   percent,
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading pull response: %w", err)
	}

	return nil
}

// EnsureReady ensures Ollama is running and has the required model
func (m *OllamaManager) EnsureReady(ctx context.Context, model string, opts EnsureOpts) error {
	if model == "" {
		model = DefaultModel
	}

	// Set defaults for opts
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}

	// Check installation
	installed, _, err := m.IsInstalled()
	if err != nil {
		return fmt.Errorf("failed to check installation: %w", err)
	}
	if !installed {
		return &NotInstalledError{}
	}

	// Check if running
	running, err := m.IsRunning()
	if err != nil {
		return fmt.Errorf("failed to check if running: %w", err)
	}

	// Start if needed
	if !running {
		if !opts.AutoStart {
			return &NotRunningError{}
		}

		fmt.Fprintln(opts.Stdout, "Ollama is installed but not running. Starting...")
		if err := m.Start(); err != nil {
			return fmt.Errorf("failed to start Ollama: %w", err)
		}

		// Wait for ready
		fmt.Fprintln(opts.Stdout, "Waiting for Ollama to be ready...")
		if err := m.WaitForReady(ctx, StartupTimeout); err != nil {
			return fmt.Errorf("ollama failed to start: %w", err)
		}
		fmt.Fprintln(opts.Stdout, "Ollama started successfully.")
	}

	// Check for model
	hasModel, err := m.HasModel(ctx, model)
	if err != nil {
		return fmt.Errorf("failed to check model: %w", err)
	}

	if !hasModel {
		if !opts.AutoPull {
			return &ModelNotFoundError{Model: model}
		}

		fmt.Fprintf(opts.Stdout, "Pulling embedding model %s...\n", model)

		// Create progress function that prints progress bar
		progressFunc := opts.ProgressFunc
		if progressFunc == nil {
			lastPercent := -1.0
			progressFunc = func(p PullProgress) {
				if p.Total > 0 && p.Percent != lastPercent {
					lastPercent = p.Percent
					// Simple progress output
					fmt.Fprintf(opts.Stdout, "\r%s: %.0f%% (%d/%d MB)",
						p.Status, p.Percent, p.Completed/(1024*1024), p.Total/(1024*1024))
				}
			}
		}

		if err := m.PullModel(ctx, model, progressFunc); err != nil {
			return fmt.Errorf("failed to pull model: %w", err)
		}
		fmt.Fprintln(opts.Stdout) // Newline after progress
		fmt.Fprintf(opts.Stdout, "Model %s ready.\n", model)
	}

	return nil
}

// Error types for specific conditions

// NotInstalledError indicates Ollama is not installed
type NotInstalledError struct{}

func (e *NotInstalledError) Error() string {
	return "ollama is not installed"
}

// NotRunningError indicates Ollama is installed but not running
type NotRunningError struct{}

func (e *NotRunningError) Error() string {
	return "ollama is not running"
}

// ModelNotFoundError indicates the required model is not available
type ModelNotFoundError struct {
	Model string
}

func (e *ModelNotFoundError) Error() string {
	return fmt.Sprintf("model %s not found", e.Model)
}

// InstallInstructions returns platform-specific install instructions
func InstallInstructions() string {
	switch runtime.GOOS {
	case "darwin":
		return `Ollama is required for semantic search.

Install options:
  1. Download from: https://ollama.com/download
  2. Or via Homebrew: brew install ollama

After installation, run: amanmcp init`
	case "linux":
		return `Ollama is required for semantic search.

Install:
  curl -fsSL https://ollama.com/install.sh | sh

After installation, run: amanmcp init`
	default:
		return `Ollama is required for semantic search.

Download from: https://ollama.com/download

After installation, run: amanmcp init`
	}
}

// IsRemoteHost checks if the configured host is not localhost
func (m *OllamaManager) IsRemoteHost() bool {
	return !strings.Contains(m.host, "localhost") && !strings.Contains(m.host, "127.0.0.1")
}
