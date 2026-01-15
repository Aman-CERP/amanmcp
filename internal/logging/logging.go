package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Config contains logging configuration.
type Config struct {
	// Level is the minimum log level (debug, info, warn, error).
	Level string
	// FilePath is the path to the log file. Empty means no file logging.
	FilePath string
	// MaxSizeMB is the maximum size in MB before rotation (default: 10).
	MaxSizeMB int
	// MaxFiles is the maximum number of rotated files to keep (default: 5).
	MaxFiles int
	// WriteToStderr whether to also write to stderr (default: true).
	WriteToStderr bool
}

// DefaultConfig returns sensible defaults for file logging.
func DefaultConfig() Config {
	return Config{
		Level:         "info",
		FilePath:      DefaultLogPath(),
		MaxSizeMB:     10,
		MaxFiles:      5,
		WriteToStderr: true,
	}
}

// DebugConfig returns configuration for debug mode.
func DebugConfig() Config {
	cfg := DefaultConfig()
	cfg.Level = "debug"
	return cfg
}

// Setup initializes file-based logging and returns a cleanup function.
// The cleanup function should be called to close the log file.
// Returns the configured logger and cleanup function.
func Setup(cfg Config) (*slog.Logger, func(), error) {
	// Ensure log directory exists
	if err := EnsureLogDir(); err != nil {
		return nil, nil, err
	}

	// Create rotating writer
	writer, err := NewRotatingWriter(cfg.FilePath, cfg.MaxSizeMB, cfg.MaxFiles)
	if err != nil {
		return nil, nil, err
	}

	// Build multi-writer if stderr is enabled
	var output io.Writer = writer
	if cfg.WriteToStderr {
		output = io.MultiWriter(writer, os.Stderr)
	}

	// Parse log level
	level := parseLevel(cfg.Level)

	// Create JSON handler for structured logging
	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{
		Level: level,
	})

	logger := slog.New(handler)

	// Cleanup function
	cleanup := func() {
		_ = writer.Sync()
		_ = writer.Close()
	}

	return logger, cleanup, nil
}

// SetupDefault sets up logging with default configuration and sets as default logger.
// Returns cleanup function.
func SetupDefault() (func(), error) {
	logger, cleanup, err := Setup(DebugConfig())
	if err != nil {
		return nil, err
	}

	slog.SetDefault(logger)
	return cleanup, nil
}

// parseLevel converts string level to slog.Level.
func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// LevelFromString converts string level to slog.Level (exported for use by log viewer).
func LevelFromString(level string) slog.Level {
	return parseLevel(level)
}
