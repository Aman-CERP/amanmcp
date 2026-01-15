package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Aman-CERP/amanmcp/internal/daemon"
	"github.com/Aman-CERP/amanmcp/internal/logging"
	"github.com/Aman-CERP/amanmcp/internal/output"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the background search daemon",
		Long: `The daemon keeps the embedding model loaded in memory for fast CLI searches.

Commands:
  start   Start the daemon (runs in background by default)
  stop    Stop the running daemon
  status  Show daemon status and health

The daemon provides instant search responses by keeping the embedder loaded,
avoiding the 30+ second initialization time on each search.

Examples:
  amanmcp daemon start      # Start daemon in background
  amanmcp daemon start -f   # Run in foreground (for debugging)
  amanmcp daemon status     # Check if daemon is running
  amanmcp daemon stop       # Stop the daemon`,
	}

	cmd.AddCommand(newDaemonStartCmd())
	cmd.AddCommand(newDaemonStopCmd())
	cmd.AddCommand(newDaemonStatusCmd())

	return cmd
}

func newDaemonStartCmd() *cobra.Command {
	var foreground bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the background daemon",
		Long: `Start the search daemon in the background.

The daemon keeps the embedding model loaded in memory, allowing fast
CLI search responses. By default, it runs in the background.

Use --foreground for debugging or to see logs in real-time.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonStart(cmd.Context(), cmd, foreground)
		},
	}

	cmd.Flags().BoolVarP(&foreground, "foreground", "f", false, "Run in foreground (don't daemonize)")
	return cmd
}

func newDaemonStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running daemon",
		Long: `Stop the running search daemon.

Sends SIGTERM to the daemon process for graceful shutdown.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonStop(cmd)
		},
	}
}

func newDaemonStatusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		Long: `Show the current status of the search daemon.

Displays whether the daemon is running, its process ID, uptime,
embedder status, and number of loaded projects.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonStatus(cmd.Context(), cmd, jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return cmd
}

func runDaemonStart(ctx context.Context, cmd *cobra.Command, foreground bool) error {
	out := output.New(cmd.OutOrStdout())
	cfg := daemon.DefaultConfig()

	// Check if already running
	client := daemon.NewClient(cfg)
	if client.IsRunning() {
		out.Status("", "Daemon is already running")
		return nil
	}

	if foreground {
		// Initialize logging for daemon (BUG-040: daemon wasn't logging to file)
		logCfg := logging.DefaultConfig()
		logCfg.Level = "debug"
		logCfg.WriteToStderr = true // Also write to stderr in foreground mode
		if logger, cleanup, err := logging.Setup(logCfg); err == nil {
			slog.SetDefault(logger)
			defer cleanup()
		}

		// Run in foreground
		out.Status("", "Starting daemon in foreground...")
		out.Status("", fmt.Sprintf("Socket: %s", cfg.SocketPath))
		out.Status("", fmt.Sprintf("Logs: %s", logging.DefaultLogPath()))
		out.Status("", "Press Ctrl+C to stop")
		out.Newline()

		slog.Info("Daemon starting in foreground mode",
			slog.String("socket", cfg.SocketPath),
			slog.String("log_file", logging.DefaultLogPath()))

		d, err := daemon.NewDaemon(cfg)
		if err != nil {
			slog.Error("Failed to create daemon", slog.String("error", err.Error()))
			return fmt.Errorf("failed to create daemon: %w", err)
		}

		return d.Start(ctx)
	}

	// Run in background
	out.Status("", "Starting daemon in background...")

	// Re-execute self with foreground flag
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Create background process
	bgCmd := exec.Command(execPath, "daemon", "start", "--foreground")
	bgCmd.Stdout = nil
	bgCmd.Stderr = nil
	bgCmd.Stdin = nil

	// Detach from parent
	bgCmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := bgCmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Monitor child exit in background to prevent zombie processes
	// and detect premature failures (BUG-038 fix)
	done := make(chan error, 1)
	go func() { done <- bgCmd.Wait() }()

	// Wait for daemon to be ready
	for i := 0; i < 20; i++ {
		select {
		case err := <-done:
			// Child exited prematurely - this is an error condition
			if err != nil {
				return fmt.Errorf("daemon process exited unexpectedly: %w", err)
			}
			return fmt.Errorf("daemon process exited unexpectedly with code 0")
		default:
			// Child still running, continue checking connectivity
		}

		time.Sleep(100 * time.Millisecond)
		if client.IsRunning() {
			out.Success(fmt.Sprintf("Daemon started (pid: %d)", bgCmd.Process.Pid))
			return nil // Goroutine will reap zombie when daemon eventually exits
		}
	}

	return fmt.Errorf("daemon failed to start within timeout")
}

func runDaemonStop(cmd *cobra.Command) error {
	out := output.New(cmd.OutOrStdout())
	cfg := daemon.DefaultConfig()

	pidFile := daemon.NewPIDFile(cfg.PIDPath)

	if !pidFile.IsRunning() {
		out.Status("", "Daemon is not running")
		return nil
	}

	pid, err := pidFile.Read()
	if err != nil {
		return fmt.Errorf("failed to read PID: %w", err)
	}

	// Send SIGTERM
	if err := pidFile.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to stop daemon: %w", err)
	}

	// Wait for process to exit
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		if !pidFile.IsRunning() {
			out.Success(fmt.Sprintf("Daemon stopped (was pid: %d)", pid))
			return nil
		}
	}

	// Force kill if still running
	out.Status("", "Daemon not responding, sending SIGKILL...")
	if err := pidFile.Signal(syscall.SIGKILL); err != nil {
		return fmt.Errorf("failed to kill daemon: %w", err)
	}

	out.Success("Daemon killed")
	return nil
}

func runDaemonStatus(ctx context.Context, cmd *cobra.Command, jsonOutput bool) error {
	out := output.New(cmd.OutOrStdout())
	cfg := daemon.DefaultConfig()

	client := daemon.NewClient(cfg)

	if !client.IsRunning() {
		if jsonOutput {
			status := daemon.StatusResult{Running: false}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(status)
		}
		out.Status("", "Daemon is not running")
		out.Status("", "Run 'amanmcp daemon start' to start it")
		return nil
	}

	status, err := client.Status(ctx)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}

	out.Status("", "Daemon is running")
	out.Status("", fmt.Sprintf("  PID:             %d", status.PID))
	out.Status("", fmt.Sprintf("  Uptime:          %s", status.Uptime))
	out.Status("", fmt.Sprintf("  Embedder:        %s (%s)", status.EmbedderType, status.EmbedderStatus))
	out.Status("", fmt.Sprintf("  Projects loaded: %d", status.ProjectsLoaded))
	out.Status("", fmt.Sprintf("  Socket:          %s", cfg.SocketPath))

	return nil
}
