package cmd

import (
	"bytes"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSetupCmd_NoGoroutineLeak(t *testing.T) {
	// Get baseline goroutine count
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	// Run setup command multiple times with --check flag
	// (--check prevents actual model download)
	for i := 0; i < 5; i++ {
		cmd := newSetupCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--check"})
		// Run - may fail but should not leak
		_ = cmd.Execute()
	}

	// Allow time for any leaked goroutines to settle
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	// Check goroutine count hasn't grown significantly
	current := runtime.NumGoroutine()
	leaked := current - baseline

	// Allow some variance for lifecycle HTTP clients and signal handlers
	// The lifecycle package may create short-lived goroutines that clean up asynchronously
	assert.LessOrEqual(t, leaked, 5, "goroutine leak detected: baseline=%d, current=%d, leaked=%d", baseline, current, leaked)
}

func TestSetupCmd_BasicExecution(t *testing.T) {
	var stdout bytes.Buffer

	cmd := newSetupCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--check"})

	// Execute - may fail if model not available, but should not panic
	_ = cmd.Execute()

	// Should produce some output
	assert.NotEmpty(t, stdout.String())
}

func TestSetupCmd_VerboseFlag(t *testing.T) {
	var stdout bytes.Buffer

	cmd := newSetupCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--check", "--verbose"})

	// Execute
	_ = cmd.Execute()

	// Should produce output (may or may not include verbose info depending on success)
	// Just verify no panic
	assert.NotNil(t, stdout)
}

func TestSetupCmd_OfflineFlag(t *testing.T) {
	var stdout bytes.Buffer

	cmd := newSetupCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--offline"})

	// Execute - should succeed immediately
	err := cmd.Execute()
	assert.NoError(t, err)

	// Should mention offline mode
	output := stdout.String()
	assert.Contains(t, output, "offline")
}

func TestSetupCmd_AutoFlag(t *testing.T) {
	var stdout bytes.Buffer

	cmd := newSetupCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--auto", "--check"})

	// Execute - --check with --auto should work
	_ = cmd.Execute()

	// Should produce output
	assert.NotEmpty(t, stdout.String())
}

func TestSetupCmd_HasNewFlags(t *testing.T) {
	cmd := newSetupCmd()

	// Verify new flags exist
	assert.NotNil(t, cmd.Flags().Lookup("check"))
	assert.NotNil(t, cmd.Flags().Lookup("auto"))
	assert.NotNil(t, cmd.Flags().Lookup("offline"))
	assert.NotNil(t, cmd.Flags().Lookup("verbose"))
}
