// Package main provides the amanmcp-logs command - a log viewer for AmanMCP.
//
// Usage:
//
//	amanmcp-logs [flags]
//
// Flags:
//
//	-f, --follow         Follow log output (like tail -f)
//	-n, --lines int      Number of lines to show (default 50)
//	    --level string   Filter by level (debug|info|warn|error)
//	    --filter string  Filter by pattern (regex)
//	    --no-color       Disable colored output
//	    --file string    Custom log file path
//	    --source string  Log source: go, mlx, or all (default: go)
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/Aman-CERP/amanmcp/internal/logging"
	"github.com/Aman-CERP/amanmcp/pkg/version"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var (
		follow  bool
		lines   int
		level   string
		filter  string
		noColor bool
		logFile string
		source  string
	)

	cmd := &cobra.Command{
		Use:   "amanmcp-logs",
		Short: "View AmanMCP server logs",
		Long: `View and tail AmanMCP server logs from Go and MLX servers.

By default, shows the last 50 lines of the Go server log. Use -f to follow
new log entries in real-time (like 'tail -f').

Log Sources:
  go   - Go server logs (~/.amanmcp/logs/server.log)
  mlx  - MLX Python server logs (~/.amanmcp/logs/mlx-server.log)
  all  - Both sources merged by timestamp

Examples:
  amanmcp-logs                    # Show last 50 lines (Go server)
  amanmcp-logs --source mlx       # Show MLX server logs
  amanmcp-logs --source all       # Show all logs merged by timestamp
  amanmcp-logs --source all -f    # Follow all logs in real-time
  amanmcp-logs -n 100             # Show last 100 lines
  amanmcp-logs -f                 # Follow logs in real-time
  amanmcp-logs --level error      # Show only error logs
  amanmcp-logs --filter "search"  # Filter by pattern`,
		Version: version.Version,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogs(cmd.Context(), logsOptions{
				follow:  follow,
				lines:   lines,
				level:   level,
				filter:  filter,
				noColor: noColor,
				logFile: logFile,
				source:  source,
			})
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output (like tail -f)")
	cmd.Flags().IntVarP(&lines, "lines", "n", 50, "Number of lines to show")
	cmd.Flags().StringVar(&level, "level", "", "Filter by log level (debug|info|warn|error)")
	cmd.Flags().StringVar(&filter, "filter", "", "Filter by keyword/pattern (regex)")
	cmd.Flags().BoolVar(&noColor, "no-color", false, "Disable colored output")
	cmd.Flags().StringVar(&logFile, "file", "", "Path to log file (overrides --source)")
	cmd.Flags().StringVar(&source, "source", "go", "Log source: go, mlx, or all")

	return cmd
}

type logsOptions struct {
	follow  bool
	lines   int
	level   string
	filter  string
	noColor bool
	logFile string
	source  string
}

func runLogs(ctx context.Context, opts logsOptions) error {
	// Parse source
	logSource := logging.ParseLogSource(opts.source)

	// Find log files based on source
	paths, err := logging.FindLogFileBySource(logSource, opts.logFile)
	if err != nil {
		return err
	}

	// Parse filter pattern if provided
	var pattern *regexp.Regexp
	if opts.filter != "" {
		pattern, err = regexp.Compile(opts.filter)
		if err != nil {
			return fmt.Errorf("invalid filter pattern: %w", err)
		}
	}

	// Determine if we should show source labels (when viewing multiple sources)
	showSource := logSource == logging.LogSourceAll || len(paths) > 1

	// Create viewer
	viewer := logging.NewViewer(logging.ViewerConfig{
		Level:      opts.level,
		Pattern:    pattern,
		NoColor:    opts.noColor,
		ShowSource: showSource,
	}, os.Stdout)

	// Show log file paths
	if len(paths) == 1 {
		fmt.Fprintf(os.Stderr, "Log file: %s\n", paths[0])
	} else {
		fmt.Fprintf(os.Stderr, "Log files: %s\n", strings.Join(paths, ", "))
	}
	if opts.follow {
		fmt.Fprintf(os.Stderr, "Following... (Ctrl+C to stop)\n")
	}
	fmt.Fprintln(os.Stderr, "---")

	if opts.follow {
		// Follow mode
		if len(paths) == 1 {
			return runFollow(ctx, viewer, paths[0])
		}
		return runFollowMultiple(ctx, viewer, paths)
	}

	// Tail mode - show last N lines
	var entries []logging.LogEntry
	if len(paths) == 1 {
		entries, err = viewer.Tail(paths[0], opts.lines)
	} else {
		entries, err = viewer.TailMultiple(paths, opts.lines)
	}
	if err != nil {
		return err
	}

	viewer.Print(entries)
	return nil
}

func runFollow(ctx context.Context, viewer *logging.Viewer, path string) error {
	// Setup signal handling
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	entries := make(chan logging.LogEntry, 100)
	errCh := make(chan error, 1)

	go func() {
		errCh <- viewer.Follow(ctx, path, entries)
	}()

	for {
		select {
		case entry := <-entries:
			fmt.Println(viewer.FormatEntry(entry))
		case err := <-errCh:
			return err
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "\n---")
			fmt.Fprintln(os.Stderr, "Stopped.")
			return nil
		}
	}
}

func runFollowMultiple(ctx context.Context, viewer *logging.Viewer, paths []string) error {
	// Setup signal handling
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	entries := make(chan logging.LogEntry, 100)
	errCh := make(chan error, 1)

	go func() {
		errCh <- viewer.FollowMultiple(ctx, paths, entries)
	}()

	for {
		select {
		case entry := <-entries:
			fmt.Println(viewer.FormatEntry(entry))
		case err := <-errCh:
			return err
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "\n---")
			fmt.Fprintln(os.Stderr, "Stopped.")
			return nil
		}
	}
}
