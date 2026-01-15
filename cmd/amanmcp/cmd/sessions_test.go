package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Sessions CLI Tests
// DEBT-028: Test coverage for sessions commands
// ============================================================================

func TestSessionsCmd_HasSubcommands(t *testing.T) {
	// Given: root command
	cmd := NewRootCmd()

	// When: finding sessions command
	sessionsCmd, _, err := cmd.Find([]string{"sessions"})
	require.NoError(t, err)

	// Then: sessions command should have subcommands
	subcommands := sessionsCmd.Commands()
	assert.GreaterOrEqual(t, len(subcommands), 2, "sessions should have delete, prune subcommands")

	names := make(map[string]bool)
	for _, sc := range subcommands {
		names[sc.Name()] = true
	}
	assert.True(t, names["delete"], "should have delete command")
	assert.True(t, names["prune"], "should have prune command")
}

func TestSessionsPruneCmd_HasOlderThanFlag(t *testing.T) {
	// Given: root command
	cmd := NewRootCmd()

	// When: finding sessions prune command
	pruneCmd, _, err := cmd.Find([]string{"sessions", "prune"})
	require.NoError(t, err)

	// Then: should have --older-than flag
	flag := pruneCmd.Flags().Lookup("older-than")
	assert.NotNil(t, flag, "should have --older-than flag")
	assert.Equal(t, "30d", flag.DefValue, "default should be 30 days")
}

func TestRunSessionsList_EmptySessions(t *testing.T) {
	// Given: empty sessions directory
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	require.NoError(t, os.MkdirAll(sessionsDir, 0755))

	// Override config to use temp directory
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", tmpDir)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"sessions"})

	// When: listing sessions
	err := cmd.Execute()

	// Then: should succeed and show no sessions
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "No sessions found", "should indicate no sessions")
}

func TestRunSessionsDelete_NotFound(t *testing.T) {
	// Given: empty sessions directory
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	require.NoError(t, os.MkdirAll(sessionsDir, 0755))

	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", tmpDir)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"sessions", "delete", "nonexistent"})

	// When: trying to delete non-existent session
	err := cmd.Execute()

	// Then: should fail with not found error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found", "should indicate session not found")
}

func TestRunSessionsPrune_NoSessions(t *testing.T) {
	// Given: empty sessions directory
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	require.NoError(t, os.MkdirAll(sessionsDir, 0755))

	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", tmpDir)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"sessions", "prune", "--older-than=1d"})

	// When: pruning with no sessions
	err := cmd.Execute()

	// Then: should succeed and report no sessions to prune
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "No sessions to prune", "should indicate nothing to prune")
}

func TestParseDuration_Days(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{
			name:     "30 days",
			input:    "30d",
			expected: 30 * 24 * time.Hour,
		},
		{
			name:     "7 days",
			input:    "7d",
			expected: 7 * 24 * time.Hour,
		},
		{
			name:     "1 day",
			input:    "1d",
			expected: 24 * time.Hour,
		},
		{
			name:     "standard duration hours",
			input:    "24h",
			expected: 24 * time.Hour,
		},
		{
			name:     "standard duration minutes",
			input:    "30m",
			expected: 30 * time.Minute,
		},
		{
			name:    "invalid format",
			input:   "abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDuration(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestFormatTimeAgo(t *testing.T) {
	tests := []struct {
		name     string
		time     time.Time
		expected string
	}{
		{
			name:     "just now",
			time:     time.Now().Add(-30 * time.Second),
			expected: "just now",
		},
		{
			name:     "1 minute ago",
			time:     time.Now().Add(-1 * time.Minute),
			expected: "1 minute ago",
		},
		{
			name:     "5 minutes ago",
			time:     time.Now().Add(-5 * time.Minute),
			expected: "5 minutes ago",
		},
		{
			name:     "1 hour ago",
			time:     time.Now().Add(-1 * time.Hour),
			expected: "1 hour ago",
		},
		{
			name:     "3 hours ago",
			time:     time.Now().Add(-3 * time.Hour),
			expected: "3 hours ago",
		},
		{
			name:     "1 day ago",
			time:     time.Now().Add(-24 * time.Hour),
			expected: "1 day ago",
		},
		{
			name:     "3 days ago",
			time:     time.Now().Add(-72 * time.Hour),
			expected: "3 days ago",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTimeAgo(tt.time)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestFormatTimeAgo_OldDate(t *testing.T) {
	// Given: a date older than 7 days
	oldTime := time.Now().Add(-30 * 24 * time.Hour)

	// When: formatting
	result := formatTimeAgo(oldTime)

	// Then: should return formatted date
	assert.NotContains(t, result, "ago", "old dates should use date format, not 'ago'")
	assert.Contains(t, result, ",", "should contain comma in date format")
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{
			name:     "bytes",
			bytes:    500,
			expected: "500 B",
		},
		{
			name:     "kilobytes",
			bytes:    1536,
			expected: "1.5 KB",
		},
		{
			name:     "megabytes",
			bytes:    1572864,
			expected: "1.5 MB",
		},
		{
			name:     "gigabytes",
			bytes:    1610612736,
			expected: "1.5 GB",
		},
		{
			name:     "zero",
			bytes:    0,
			expected: "0 B",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatSize(tt.bytes)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestSessionsDeleteCmd_RequiresArgument(t *testing.T) {
	// Given: root command
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"sessions", "delete"})

	// When: running delete without argument
	err := cmd.Execute()

	// Then: should fail with missing argument error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg", "should require exactly 1 argument")
}
