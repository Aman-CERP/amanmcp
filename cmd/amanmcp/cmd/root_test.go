package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootCmd_SmartDefault_NoStdoutOutput(t *testing.T) {
	// BUG-034: MCP protocol requires stdout to be used EXCLUSIVELY for JSON-RPC.
	// The smart default mode (no args) must NOT write any status messages to stdout.
	// All logging goes to file instead.

	// Given: a root command in a temp directory
	tmpDir := t.TempDir()
	oldDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldDir) }()

	// When: executing with no arguments
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{})

	_ = cmd.Execute() // May fail due to no files to index, that's OK

	// Then: it should NOT produce any status output to stdout
	// (MCP mode requires clean stdio for JSON-RPC)
	output := buf.String()
	// Should NOT contain status messages that would corrupt MCP protocol
	assert.NotContains(t, output, "🚀", "Should not write status emojis to stdout")
	assert.NotContains(t, output, "Hugot", "Should not write embedder status to stdout")
	assert.NotContains(t, output, "Starting MCP", "Should not write MCP status to stdout")
}

func TestRootCmd_SmartDefault_Offline_NoStdoutOutput(t *testing.T) {
	// BUG-034: Even with --offline flag, MCP mode must not write to stdout.
	// Status messages are logged to file instead.

	// Given: a root command with --offline in a temp directory
	tmpDir := t.TempDir()
	oldDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldDir) }()

	// When: executing with --offline (no index exists)
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--offline"})

	_ = cmd.Execute() // May fail (no project to index), that's OK

	// Then: it should NOT produce any status output to stdout
	output := buf.String()
	// Should NOT contain status messages (offline mode is logged to file)
	assert.NotContains(t, output, "🚀", "Should not write status emojis to stdout")
	assert.NotContains(t, output, "Starting MCP", "Should not write MCP status to stdout")
}

func TestRootCmd_ShowsHelp(t *testing.T) {
	// Given: a root command

	// When: executing with --help
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()

	// Then: it should show usage information
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "amanmcp", "Help should mention program name")
	assert.Contains(t, output, "Usage:", "Help should show usage")
}

func TestRootCmd_ShowsVersion(t *testing.T) {
	// Given: a root command

	// When: executing with --version
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--version"})

	err := cmd.Execute()

	// Then: it should show version
	require.NoError(t, err)
	output := buf.String()
	// Accept either semantic version (0.1.x) or "dev" for test builds without ldflags
	hasVersion := strings.Contains(output, "0.1") || strings.Contains(output, "dev")
	assert.True(t, hasVersion, "Version output should contain version number (0.1.x) or 'dev'")
	assert.Contains(t, output, "amanmcp", "Version output should mention program name")
}

func TestRootCmd_HasSubcommands(t *testing.T) {
	// Given: a root command

	// When: checking available commands
	cmd := NewRootCmd()
	subcommands := cmd.Commands()

	// Then: serve, index, and search subcommands should exist
	var commandNames []string
	for _, subcmd := range subcommands {
		commandNames = append(commandNames, subcmd.Name())
	}

	assert.Contains(t, commandNames, "serve", "Should have serve subcommand")
	assert.Contains(t, commandNames, "index", "Should have index subcommand")
	assert.Contains(t, commandNames, "search", "Should have search subcommand")
	assert.Contains(t, commandNames, "setup", "Should have setup subcommand")
}

func TestRootCmd_HasOfflineFlag(t *testing.T) {
	// Given: a root command
	cmd := NewRootCmd()

	// Then: it should have --offline flag
	flag := cmd.Flags().Lookup("offline")
	assert.NotNil(t, flag, "Should have --offline flag")
	assert.Equal(t, "false", flag.DefValue)
}

func TestRootCmd_HasReindexFlag(t *testing.T) {
	// Given: a root command
	cmd := NewRootCmd()

	// Then: it should have --reindex flag
	flag := cmd.Flags().Lookup("reindex")
	assert.NotNil(t, flag, "Should have --reindex flag")
	assert.Equal(t, "false", flag.DefValue)
}

func TestServeCmd_ShowsHelp(t *testing.T) {
	// Given: a root command

	// When: executing serve --help
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"serve", "--help"})

	err := cmd.Execute()

	// Then: it should show serve usage
	require.NoError(t, err)
	output := buf.String()
	assert.True(t, strings.Contains(output, "serve") || strings.Contains(output, "MCP"),
		"Serve help should mention serve or MCP")
}

func TestIndexCmd_ShowsHelp(t *testing.T) {
	// Given: a root command

	// When: executing index --help
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", "--help"})

	err := cmd.Execute()

	// Then: it should show index usage
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "index", "Index help should mention index")
}

func TestSearchCmd_ShowsHelp(t *testing.T) {
	// Given: a root command

	// When: executing search --help
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"search", "--help"})

	err := cmd.Execute()

	// Then: it should show search usage
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "search", "Search help should mention search")
}
