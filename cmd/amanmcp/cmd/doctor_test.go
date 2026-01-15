package cmd

import (
	"bytes"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDoctorCmd_NoGoroutineLeak(t *testing.T) {
	// Get baseline goroutine count
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	// Run doctor command multiple times
	for i := 0; i < 5; i++ {
		cmd := newDoctorCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		// Run without arguments - should complete quickly
		_ = cmd.Execute()
	}

	// Allow time for any leaked goroutines to settle
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	// Check goroutine count hasn't grown significantly
	// Allow some variance for runtime goroutines
	current := runtime.NumGoroutine()
	leaked := current - baseline

	// Should not leak more than 1 goroutine per invocation
	// With the buggy implementation, each call leaks 1 goroutine
	assert.LessOrEqual(t, leaked, 2, "goroutine leak detected: baseline=%d, current=%d, leaked=%d", baseline, current, leaked)
}

func TestDoctorCmd_BasicExecution(t *testing.T) {
	var stdout bytes.Buffer

	cmd := newDoctorCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})

	// Execute - may fail due to system checks, but should not panic
	_ = cmd.Execute()

	// Should produce some output
	assert.NotEmpty(t, stdout.String())
}

func TestDoctorCmd_JSONOutput(t *testing.T) {
	var stdout bytes.Buffer

	cmd := newDoctorCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--json"})

	// Execute
	_ = cmd.Execute()

	output := stdout.String()
	// JSON output should contain expected structure
	assert.Contains(t, output, `"status"`)
	assert.Contains(t, output, `"checks"`)
}
