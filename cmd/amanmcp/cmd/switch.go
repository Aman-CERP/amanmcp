package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/session"
)

func newSwitchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "switch NAME",
		Short: "Switch to a different session",
		Long: `Switch to a different session.

This command is intended to be run while an MCP server is active.
It saves the current session (if any) and prints instructions to start
the target session.

Note: Due to MCP server architecture, hot-swapping sessions is not supported.
The server must be restarted to switch sessions.

Example:
  amanmcp switch work-api

  # Output:
  # To start session 'work-api', run:
  #   amanmcp resume work-api`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSwitch(cmd, args[0])
		},
	}
}

func runSwitch(cmd *cobra.Command, targetName string) error {
	cfg := config.NewConfig()

	mgr, err := session.NewManager(session.ManagerConfig{
		StoragePath: cfg.Sessions.StoragePath,
		MaxSessions: cfg.Sessions.MaxSessions,
	})
	if err != nil {
		return fmt.Errorf("failed to create session manager: %w", err)
	}

	// Check if target session exists
	if !mgr.Exists(targetName) {
		return fmt.Errorf("session '%s' not found\n\nRun 'amanmcp sessions' to list available sessions", targetName)
	}

	// Print instructions to resume target session
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "To start session '%s', run:\n", targetName)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  amanmcp resume %s\n", targetName)

	return nil
}
