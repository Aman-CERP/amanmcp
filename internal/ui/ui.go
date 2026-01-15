// Package ui provides terminal UI components for progress and status display.
package ui

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/mattn/go-isatty"
)

// Stage represents an indexing stage.
type Stage int

const (
	// StageScanning is the file scanning stage.
	StageScanning Stage = iota
	// StageChunking is the code chunking stage.
	StageChunking
	// StageContextual is the CR-1 contextual enrichment stage.
	StageContextual
	// StageEmbedding is the embedding generation stage.
	StageEmbedding
	// StageIndexing is the index building stage.
	StageIndexing
	// StageComplete indicates indexing is complete.
	StageComplete
)

// String returns the human-readable stage name.
func (s Stage) String() string {
	switch s {
	case StageScanning:
		return "Scanning"
	case StageChunking:
		return "Chunking"
	case StageContextual:
		return "Contextual"
	case StageEmbedding:
		return "Embedding"
	case StageIndexing:
		return "Indexing"
	case StageComplete:
		return "Complete"
	default:
		return "Unknown"
	}
}

// Icon returns the short stage icon for plain text output.
func (s Stage) Icon() string {
	switch s {
	case StageScanning:
		return "SCAN"
	case StageChunking:
		return "CHUNK"
	case StageContextual:
		return "CTX"
	case StageEmbedding:
		return "EMBED"
	case StageIndexing:
		return "INDEX"
	case StageComplete:
		return "DONE"
	default:
		return "???"
	}
}

// ProgressEvent represents a progress update.
type ProgressEvent struct {
	Stage       Stage
	Current     int
	Total       int
	CurrentFile string
	Message     string
}

// ErrorEvent represents an error during processing.
type ErrorEvent struct {
	File   string
	Err    error
	IsWarn bool
}

// StageTimings tracks duration for each indexing stage.
type StageTimings struct {
	Scan    time.Duration // File scanning
	Chunk   time.Duration // Code chunking
	Context time.Duration // CR-1 contextual enrichment
	Embed   time.Duration // Embedding generation
	Index   time.Duration // BM25 + vector index building
}

// EmbedderInfo contains embedder backend details.
type EmbedderInfo struct {
	Backend    string // "mlx", "ollama", or "static"
	Model      string // Model name (e.g., "qwen3-embedding:0.6b")
	Dimensions int    // Embedding dimensions
}

// CompletionStats contains final indexing statistics.
type CompletionStats struct {
	Files    int
	Chunks   int
	Duration time.Duration
	Errors   int
	Warnings int
	Stages   StageTimings // Per-stage timing breakdown
	Embedder EmbedderInfo // Embedder backend info
}

// Renderer defines the interface for progress display.
type Renderer interface {
	// Start initializes the renderer.
	Start(ctx context.Context) error

	// UpdateProgress updates progress display.
	UpdateProgress(event ProgressEvent)

	// AddError adds an error to display.
	AddError(event ErrorEvent)

	// Complete marks rendering as complete with summary.
	Complete(stats CompletionStats)

	// Stop stops the renderer and cleans up.
	Stop() error
}

// Config configures the UI renderer.
type Config struct {
	Output       io.Writer
	ForcePlain   bool
	NoColor      bool
	SpinnerStyle string
	ProjectDir   string // Project directory path to display in header
}

// ConfigOption is a function that modifies Config.
type ConfigOption func(*Config)

// WithForcePlain forces plain text output.
func WithForcePlain(force bool) ConfigOption {
	return func(c *Config) {
		c.ForcePlain = force
	}
}

// WithNoColor disables color output.
func WithNoColor(noColor bool) ConfigOption {
	return func(c *Config) {
		c.NoColor = noColor
	}
}

// WithSpinnerStyle sets the spinner style.
func WithSpinnerStyle(style string) ConfigOption {
	return func(c *Config) {
		c.SpinnerStyle = style
	}
}

// WithProjectDir sets the project directory path to display in header.
func WithProjectDir(dir string) ConfigOption {
	return func(c *Config) {
		c.ProjectDir = dir
	}
}

// NewConfig creates a new Config with the given output and options.
func NewConfig(output io.Writer, opts ...ConfigOption) Config {
	cfg := Config{
		Output:       output,
		ForcePlain:   false,
		NoColor:      false,
		SpinnerStyle: "dots",
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	return cfg
}

// NewRenderer creates an appropriate renderer based on config and environment.
// It returns a TUI renderer for interactive terminals, and a plain text
// renderer for CI environments, pipes, or when --no-tui is specified.
func NewRenderer(cfg Config) Renderer {
	// Force plain mode if requested
	if cfg.ForcePlain {
		return NewPlainRenderer(cfg)
	}

	// Use plain mode for non-TTY outputs
	if !IsTTY(cfg.Output) {
		return NewPlainRenderer(cfg)
	}

	// Use plain mode in CI environments
	if DetectCI() {
		return NewPlainRenderer(cfg)
	}

	// Try TUI mode, fall back to plain on failure
	tui, err := NewTUIRenderer(cfg)
	if err != nil {
		return NewPlainRenderer(cfg)
	}

	return tui
}

// IsTTY checks if output is a terminal.
func IsTTY(w io.Writer) bool {
	if w == nil {
		return false
	}

	// Check if it's a file that's a terminal
	if f, ok := w.(*os.File); ok {
		return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
	}

	return false
}

// DetectNoColor checks if NO_COLOR environment variable is set.
func DetectNoColor() bool {
	_, exists := os.LookupEnv("NO_COLOR")
	return exists
}

// DetectCI checks if running in a CI environment.
func DetectCI() bool {
	ciVars := []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI", "JENKINS_URL", "TRAVIS"}
	for _, v := range ciVars {
		if _, exists := os.LookupEnv(v); exists {
			return true
		}
	}
	return false
}
