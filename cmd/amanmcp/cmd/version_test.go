package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/pkg/version"
)

func TestVersionCmd_DefaultOutput(t *testing.T) {
	// Given: a version command
	cmd := newVersionCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{})

	// When: executing without flags
	err := cmd.Execute()

	// Then: it should output version string
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "amanmcp", "Output should contain program name")
	assert.Contains(t, output, version.Version, "Output should contain version")
	assert.Contains(t, output, "commit", "Output should contain commit info")
}

func TestVersionCmd_ShortOutput(t *testing.T) {
	// Given: a version command with --short flag
	cmd := newVersionCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--short"})

	// When: executing with --short
	err := cmd.Execute()

	// Then: it should output only version number
	require.NoError(t, err)
	output := strings.TrimSpace(buf.String())
	assert.Equal(t, version.Version, output, "Short output should be just version")
}

func TestVersionCmd_JSONOutput(t *testing.T) {
	// Given: a version command with --json flag
	cmd := newVersionCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--json"})

	// When: executing with --json
	err := cmd.Execute()

	// Then: it should output valid JSON with all fields
	require.NoError(t, err)
	output := buf.String()

	var info map[string]string
	err = json.Unmarshal([]byte(output), &info)
	require.NoError(t, err, "Output should be valid JSON")

	assert.Equal(t, version.Version, info["version"], "JSON should contain version")
	assert.Contains(t, info, "commit", "JSON should contain commit field")
	assert.Contains(t, info, "date", "JSON should contain date field")
	assert.Contains(t, info, "go_version", "JSON should contain go_version field")
	assert.Contains(t, info, "os", "JSON should contain os field")
	assert.Contains(t, info, "arch", "JSON should contain arch field")
}

func TestVersionCmd_AddedToRoot(t *testing.T) {
	// Given: the root command
	rootCmd := NewRootCmd()

	// When: looking for version subcommand
	versionCmd, _, err := rootCmd.Find([]string{"version"})

	// Then: version command should exist
	require.NoError(t, err)
	assert.Equal(t, "version", versionCmd.Name(), "Version command should be named 'version'")
}
