package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Config CLI Tests
// DEBT-028: Test coverage for config commands
// ============================================================================

func TestConfigCmd_HasSubcommands(t *testing.T) {
	// Given: root command
	cmd := NewRootCmd()

	// When: finding config command
	configCmd, _, err := cmd.Find([]string{"config"})
	require.NoError(t, err)

	// Then: config command should have subcommands
	subcommands := configCmd.Commands()
	assert.GreaterOrEqual(t, len(subcommands), 3, "config should have init, show, path subcommands")

	names := make(map[string]bool)
	for _, sc := range subcommands {
		names[sc.Name()] = true
	}
	assert.True(t, names["init"], "should have init command")
	assert.True(t, names["show"], "should have show command")
	assert.True(t, names["path"], "should have path command")
}

func TestConfigInitCmd_HasForceFlag(t *testing.T) {
	// Given: root command
	cmd := NewRootCmd()

	// When: finding config init command
	initCmd, _, err := cmd.Find([]string{"config", "init"})
	require.NoError(t, err)

	// Then: should have --force flag
	flag := initCmd.Flags().Lookup("force")
	assert.NotNil(t, flag, "should have --force flag")
	assert.Equal(t, "false", flag.DefValue, "default should be false")
}

func TestConfigShowCmd_HasFlags(t *testing.T) {
	// Given: root command
	cmd := NewRootCmd()

	// When: finding config show command
	showCmd, _, err := cmd.Find([]string{"config", "show"})
	require.NoError(t, err)

	// Then: should have --json flag
	jsonFlag := showCmd.Flags().Lookup("json")
	assert.NotNil(t, jsonFlag, "should have --json flag")
	assert.Equal(t, "false", jsonFlag.DefValue, "default should be false")

	// And: should have --source flag
	sourceFlag := showCmd.Flags().Lookup("source")
	assert.NotNil(t, sourceFlag, "should have --source flag")
	assert.Equal(t, "merged", sourceFlag.DefValue, "default should be merged")
}

func TestConfigPathCmd_OutputsPath(t *testing.T) {
	// Given: temp home directory
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"config", "path"})

	// When: running config path
	err := cmd.Execute()

	// Then: should succeed and output a path
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "amanmcp", "should contain amanmcp in path")
	assert.Contains(t, output, "config.yaml", "should contain config.yaml")
}

func TestRunConfigInit_NewFile(t *testing.T) {
	// Given: empty config directory
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".config", "amanmcp")
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"config", "init"})

	// When: running config init
	err := cmd.Execute()

	// Then: should succeed and create config file
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Created", "should indicate creation")

	// And: file should exist
	configPath := filepath.Join(configDir, "config.yaml")
	_, err = os.Stat(configPath)
	assert.NoError(t, err, "config file should exist")
}

func TestRunConfigInit_AlreadyExists(t *testing.T) {
	// Given: existing config file
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".config", "amanmcp")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	configPath := filepath.Join(configDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("existing: config"), 0644))

	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"config", "init"})

	// When: running config init without --force
	err := cmd.Execute()

	// Then: should succeed but not overwrite (just warn)
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "already exists", "should indicate config already exists")
	assert.Contains(t, output, "--force", "should mention --force flag")

	// And: original file should be unchanged
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, "existing: config", string(data), "file should be unchanged")
}

func TestRunConfigShow_Defaults(t *testing.T) {
	// Given: clean environment
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"config", "show", "--source=defaults"})

	// When: showing default config
	err := cmd.Execute()

	// Then: should succeed and show defaults
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "defaults", "should indicate defaults source")
	// Default config should have some standard keys
	assert.Contains(t, output, "embeddings", "should contain embeddings section")
}

func TestRunConfigShow_JSONOutput(t *testing.T) {
	// Given: clean environment
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"config", "show", "--source=defaults", "--json"})

	// When: showing default config as JSON
	err := cmd.Execute()

	// Then: should succeed and output valid JSON
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "{", "should be JSON object")
	assert.Contains(t, output, "}", "should be JSON object")
}

func TestRunConfigShow_InvalidSource(t *testing.T) {
	// Given: invalid source parameter
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"config", "show", "--source=invalid"})

	// When: showing config with invalid source
	err := cmd.Execute()

	// Then: should fail with invalid source error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid source", "should indicate invalid source")
}

func TestRunConfigShow_UserNotExists(t *testing.T) {
	// Given: no user config file
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"config", "show", "--source=user"})

	// When: showing user config that doesn't exist
	err := cmd.Execute()

	// Then: should succeed but indicate no file found
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "No user configuration", "should indicate no user config")
}
