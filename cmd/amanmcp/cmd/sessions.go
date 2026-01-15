package cmd

import (
	"fmt"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/session"
)

func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Manage named sessions",
		Long: `List, delete, or prune named sessions.

Sessions allow you to persist index state for different projects and quickly
switch between them without re-indexing.

Examples:
  # List all sessions
  amanmcp sessions

  # Delete a specific session
  amanmcp sessions delete work-api

  # Remove sessions older than 30 days
  amanmcp sessions prune --older-than=30d`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSessionsList(cmd)
		},
	}

	// Add subcommands
	cmd.AddCommand(newSessionsDeleteCmd())
	cmd.AddCommand(newSessionsPruneCmd())

	return cmd
}

func newSessionsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a session and all its data",
		Long: `Delete a session and all its data.

This permanently removes the session directory including:
- Session metadata
- BM25 index
- Vector store
- SQLite metadata

Example:
  amanmcp sessions delete old-project`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsDelete(cmd, args[0])
		},
	}
}

func newSessionsPruneCmd() *cobra.Command {
	var olderThan string

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove old sessions",
		Long: `Remove sessions that haven't been used within the specified duration.

Examples:
  # Remove sessions not used in 30 days
  amanmcp sessions prune --older-than=30d

  # Remove sessions not used in 7 days
  amanmcp sessions prune --older-than=7d`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSessionsPrune(cmd, olderThan)
		},
	}

	cmd.Flags().StringVar(&olderThan, "older-than", "30d", "Remove sessions older than this duration (e.g., 7d, 30d)")

	return cmd
}

func runSessionsList(cmd *cobra.Command) error {
	mgr, err := getSessionManager()
	if err != nil {
		return err
	}

	sessions, err := mgr.List()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	if len(sessions) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No sessions found.")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Create a session with: amanmcp serve --session=NAME")
		return nil
	}

	// Sort by last used (most recent first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastUsed.After(sessions[j].LastUsed)
	})

	// Print table
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tPROJECT\tLAST USED\tSIZE\tSTATUS")
	_, _ = fmt.Fprintln(w, "----\t-------\t---------\t----\t------")

	for _, s := range sessions {
		status := "valid"
		if !s.Valid {
			status = "project missing"
		}

		lastUsed := formatTimeAgo(s.LastUsed)
		size := formatSize(s.Size)

		// Truncate project path if too long
		projectPath := s.ProjectPath
		if len(projectPath) > 40 {
			projectPath = "..." + projectPath[len(projectPath)-37:]
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			s.Name, projectPath, lastUsed, size, status)
	}
	_ = w.Flush()

	return nil
}

func runSessionsDelete(cmd *cobra.Command, name string) error {
	mgr, err := getSessionManager()
	if err != nil {
		return err
	}

	if !mgr.Exists(name) {
		return fmt.Errorf("session '%s' not found", name)
	}

	if err := mgr.Delete(name); err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Session '%s' deleted.\n", name)
	return nil
}

func runSessionsPrune(cmd *cobra.Command, olderThan string) error {
	duration, err := parseDuration(olderThan)
	if err != nil {
		return fmt.Errorf("invalid duration '%s': %w", olderThan, err)
	}

	mgr, err := getSessionManager()
	if err != nil {
		return err
	}

	count, err := mgr.Prune(duration)
	if err != nil {
		return fmt.Errorf("failed to prune sessions: %w", err)
	}

	if count == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No sessions to prune.")
	} else {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Pruned %d session(s).\n", count)
	}

	return nil
}

// getSessionManager creates a session manager using the default config.
func getSessionManager() (*session.Manager, error) {
	cfg := config.NewConfig()

	return session.NewManager(session.ManagerConfig{
		StoragePath: cfg.Sessions.StoragePath,
		MaxSessions: cfg.Sessions.MaxSessions,
	})
}

// parseDuration parses a duration string like "30d", "7d", "24h".
func parseDuration(s string) (time.Duration, error) {
	// Check for day suffix
	if len(s) > 1 && s[len(s)-1] == 'd' {
		days := 0
		_, err := fmt.Sscanf(s, "%dd", &days)
		if err != nil {
			return 0, fmt.Errorf("invalid day format")
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}

	// Try standard Go duration
	return time.ParseDuration(s)
}

// formatTimeAgo formats a time as a human-readable "time ago" string.
func formatTimeAgo(t time.Time) string {
	d := time.Since(t)

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case d < 24*time.Hour:
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case d < 7*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	default:
		return t.Format("Jan 2, 2006")
	}
}

// formatSize formats a byte size as a human-readable string.
func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
