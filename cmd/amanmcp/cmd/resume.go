package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/session"
)

func newResumeCmd() *cobra.Command {
	var transport string
	var port int

	cmd := &cobra.Command{
		Use:   "resume NAME",
		Short: "Resume a saved session",
		Long: `Resume a previously saved session.

This loads the saved index data and starts the MCP server for the project
associated with the session.

If the project directory no longer exists, an error is returned with
instructions to delete the orphaned session.

Example:
  # Resume the work-api session
  amanmcp resume work-api

  # List available sessions first
  amanmcp sessions`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runResume(cmd, args[0], transport, port)
		},
	}

	cmd.Flags().StringVar(&transport, "transport", "stdio", "Transport type (stdio|sse)")
	cmd.Flags().IntVar(&port, "port", 8765, "Port for SSE transport")

	return cmd
}

func runResume(cmd *cobra.Command, name, transport string, port int) error {
	cfg := config.NewConfig()

	mgr, err := session.NewManager(session.ManagerConfig{
		StoragePath: cfg.Sessions.StoragePath,
		MaxSessions: cfg.Sessions.MaxSessions,
	})
	if err != nil {
		return fmt.Errorf("failed to create session manager: %w", err)
	}

	// Get the session
	sess, err := mgr.Get(name)
	if err != nil {
		return fmt.Errorf("session not found: %s\n\nRun 'amanmcp sessions' to list available sessions", name)
	}

	// Check if project still exists
	if _, err := os.Stat(sess.ProjectPath); os.IsNotExist(err) {
		return fmt.Errorf("project directory no longer exists: %s\n\nTo remove this session, run:\n  amanmcp sessions delete %s",
			sess.ProjectPath, name)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Resuming session '%s' for %s\n", name, sess.ProjectPath)

	// Run serve with session
	return runServeWithSession(cmd.Context(), name, sess.ProjectPath, transport, port)
}
