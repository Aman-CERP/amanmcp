package logging

import (
	"log/slog"
)

// SetupMCPMode initializes logging for MCP server mode.
// This is critical for MCP protocol compliance:
// - Logs ONLY to file (never stdout/stderr)
// - Uses JSON format for structured logs
// - Always enables debug level for complete diagnostics
//
// BUG-034: MCP protocol requires stdout to be used EXCLUSIVELY for JSON-RPC.
// Any writes to stdout/stderr before or during MCP operation will corrupt
// the protocol stream and cause "Failed to connect" errors.
func SetupMCPMode() (func(), error) {
	cfg := Config{
		Level:         "debug", // Always debug in MCP mode for full diagnostics
		FilePath:      DefaultLogPath(),
		MaxSizeMB:     10,
		MaxFiles:      5,
		WriteToStderr: false, // CRITICAL: Never write to stderr in MCP mode
	}

	logger, cleanup, err := Setup(cfg)
	if err != nil {
		return nil, err
	}

	slog.SetDefault(logger)

	// Log that MCP mode logging is initialized
	slog.Info("MCP mode logging initialized",
		slog.String("log_file", cfg.FilePath),
		slog.String("level", cfg.Level),
		slog.Bool("stderr_disabled", true))

	return cleanup, nil
}

// SetupMCPModeWithLevel initializes MCP-safe logging with a specific level.
func SetupMCPModeWithLevel(level string) (func(), error) {
	cfg := Config{
		Level:         level,
		FilePath:      DefaultLogPath(),
		MaxSizeMB:     10,
		MaxFiles:      5,
		WriteToStderr: false, // CRITICAL: Never write to stderr in MCP mode
	}

	logger, cleanup, err := Setup(cfg)
	if err != nil {
		return nil, err
	}

	slog.SetDefault(logger)
	return cleanup, nil
}
