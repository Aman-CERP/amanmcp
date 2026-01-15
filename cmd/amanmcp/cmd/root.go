// Package cmd provides the CLI commands for AmanMCP.
package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/logging"
	"github.com/Aman-CERP/amanmcp/internal/preflight"
	"github.com/Aman-CERP/amanmcp/internal/profiling"
	"github.com/Aman-CERP/amanmcp/pkg/version"
)

// Profiling flags (F23 Performance Optimization)
var (
	profileCPU   string
	profileMem   string
	profileTrace string
	profiler     = profiling.NewProfiler()
	cpuCleanup   func()
	traceCleanup func()
)

// Debug logging flag
var (
	debugMode     bool
	loggingCleanup func()
)

// NewRootCmd creates the root command for amanmcp CLI.
func NewRootCmd() *cobra.Command {
	var offline bool
	var reindex bool
	var skipCheck bool

	cmd := &cobra.Command{
		Use:   "amanmcp",
		Short: "Local-first RAG MCP server for developers",
		Long: `AmanMCP provides hybrid search (BM25 + semantic) over codebases
for AI coding assistants like Claude Code and Cursor.

It runs entirely locally with zero configuration required.

Just run 'amanmcp' in your project directory to get started.`,
		Version: version.Version,
		RunE: func(cmd *cobra.Command, args []string) error {
			// If help was explicitly requested, show it
			if len(args) > 0 {
				return cmd.Help()
			}
			return runSmartDefault(cmd.Context(), cmd, offline, reindex, skipCheck)
		},
	}

	// Set version template
	cmd.SetVersionTemplate("amanmcp version {{.Version}}\n")

	// Root flags
	cmd.Flags().BoolVar(&offline, "offline", false, "Use static embeddings (skip model download)")
	cmd.Flags().BoolVar(&reindex, "reindex", false, "Force reindex even if index exists")
	cmd.Flags().BoolVar(&skipCheck, "skip-check", false, "Skip pre-flight system checks")

	// Profiling flags (F23 Performance Optimization)
	cmd.PersistentFlags().StringVar(&profileCPU, "profile-cpu", "", "Write CPU profile to file")
	cmd.PersistentFlags().StringVar(&profileMem, "profile-mem", "", "Write memory profile to file")
	cmd.PersistentFlags().StringVar(&profileTrace, "profile-trace", "", "Write execution trace to file")

	// Debug logging flag
	cmd.PersistentFlags().BoolVar(&debugMode, "debug", false, "Enable debug logging to ~/.amanmcp/logs/")

	// Setup profiling and logging hooks
	cmd.PersistentPreRunE = startProfilingAndLogging
	cmd.PersistentPostRunE = stopProfilingAndLogging

	// Add subcommands
	cmd.AddCommand(newServeCmd())
	cmd.AddCommand(newIndexCmd())
	cmd.AddCommand(newSearchCmd())
	cmd.AddCommand(newSetupCmd())
	cmd.AddCommand(newDoctorCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newStatsCmd())
	cmd.AddCommand(newConfigCmd())

	// Session management commands (F27)
	cmd.AddCommand(newSessionsCmd())
	cmd.AddCommand(newResumeCmd())
	cmd.AddCommand(newSwitchCmd())

	// Daemon command (BUG-018 fix)
	cmd.AddCommand(newDaemonCmd())

	// Compact command (BUG-024 fix)
	cmd.AddCommand(newCompactCmd())

	// Version command (F24)
	cmd.AddCommand(newVersionCmd())

	// Init command (simplified setup)
	cmd.AddCommand(newInitCmd())

	// Debug command (FEAT-UNIX4)
	cmd.AddCommand(newDebugCmd())

	return cmd
}

// startProfilingAndLogging starts CPU/trace profiling and debug logging if flags are set.
func startProfilingAndLogging(_ *cobra.Command, _ []string) error {
	var err error

	// Start debug logging if enabled
	if debugMode {
		logger, cleanup, err := logging.Setup(logging.DebugConfig())
		if err != nil {
			return fmt.Errorf("failed to setup debug logging: %w", err)
		}
		loggingCleanup = cleanup
		slog.SetDefault(logger)
		slog.Info("Debug logging enabled",
			slog.String("log_file", logging.DefaultLogPath()),
			slog.String("version", "debug"))
	}

	// Start CPU profiling
	if profileCPU != "" {
		cpuCleanup, err = profiler.StartCPU(profileCPU)
		if err != nil {
			return fmt.Errorf("failed to start CPU profile: %w", err)
		}
	}

	// Start trace profiling
	if profileTrace != "" {
		traceCleanup, err = profiler.StartTrace(profileTrace)
		if err != nil {
			if cpuCleanup != nil {
				cpuCleanup()
			}
			return fmt.Errorf("failed to start trace: %w", err)
		}
	}

	return nil
}

// stopProfilingAndLogging stops profiling and logging, writes memory profile if requested.
func stopProfilingAndLogging(_ *cobra.Command, _ []string) error {
	// Stop CPU profiling
	if cpuCleanup != nil {
		cpuCleanup()
		cpuCleanup = nil
	}

	// Stop tracing
	if traceCleanup != nil {
		traceCleanup()
		traceCleanup = nil
	}

	// Write memory profile if requested
	if profileMem != "" {
		if err := profiler.WriteHeap(profileMem); err != nil {
			return fmt.Errorf("failed to write memory profile: %w", err)
		}
	}

	// Stop debug logging
	if loggingCleanup != nil {
		slog.Info("Debug logging stopped")
		loggingCleanup()
		loggingCleanup = nil
	}

	return nil
}

// Execute runs the root command.
func Execute() error {
	return NewRootCmd().Execute()
}

// runSmartDefault implements the "It Just Works" flow.
func runSmartDefault(ctx context.Context, cmd *cobra.Command, offline, reindex, skipCheck bool) error {
	// BUG-034: MCP protocol requires stdout to be used EXCLUSIVELY for JSON-RPC messages.
	// We must NOT write ANY output to stdout before starting the MCP server.
	// All status output is suppressed in favor of file logging.
	// Use 'amanmcp status' or 'amanmcp doctor' for diagnostics instead.

	// Find project root
	root, err := config.FindProjectRoot(".")
	if err != nil {
		root, _ = os.Getwd()
	}

	dataDir := filepath.Join(root, ".amanmcp")

	// Run preflight checks silently (results logged to file)
	if !skipCheck && preflight.NeedsCheck(dataDir) {
		checker := preflight.New(
			preflight.WithOffline(offline),
			preflight.WithOutput(io.Discard), // Suppress output for MCP mode
		)
		results := checker.RunAll(ctx, root)

		if checker.HasCriticalFailures(results) {
			// Log to file instead of stdout
			slog.Error("System check failed - run 'amanmcp doctor' for diagnostics")
			return fmt.Errorf("system check failed")
		}

		// Mark as passed for future runs
		if err := preflight.MarkPassed(dataDir); err != nil {
			slog.Debug("Failed to mark preflight as passed", slog.String("error", err.Error()))
		}
	}

	// Log embedding mode to file
	if offline {
		slog.Debug("Using offline mode with static embeddings")
	} else {
		slog.Debug("Using default embeddings")
	}

	// Check if index exists and is valid
	metadataPath := filepath.Join(dataDir, "metadata.db")
	needsIndex := reindex || !fileExists(metadataPath)

	if needsIndex {
		slog.Info("Index not found, creating index", slog.String("root", root))

		// Run indexing silently
		if err := runIndexInternal(ctx, cmd, root, offline); err != nil {
			slog.Error("Indexing failed", slog.String("error", err.Error()))
			return fmt.Errorf("indexing failed: %w", err)
		}
		slog.Info("Index complete")
	} else {
		slog.Debug("Index found", slog.String("path", metadataPath))
	}

	// Start MCP server directly - NO stdout output before this point
	return runServe(ctx, "stdio", 0)
}


// fileExists checks if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// runIndexInternal runs the index command logic without creating a new command.
func runIndexInternal(ctx context.Context, cmd *cobra.Command, path string, offline bool) error {
	// Delegate to index command's runIndex function
	// (in same package, so accessible)
	// Pass 0 for resumeFromCheckpoint since this is a fresh index
	// Pass empty string for checkpointEmbedderModel (not resuming)
	return runIndexWithOptions(ctx, cmd, path, offline, false, 0, "")
}
