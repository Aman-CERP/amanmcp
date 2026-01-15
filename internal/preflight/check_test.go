package preflight

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckStatus_String(t *testing.T) {
	tests := []struct {
		status CheckStatus
		want   string
	}{
		{StatusPass, "PASS"},
		{StatusWarn, "WARN"},
		{StatusFail, "FAIL"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.String())
		})
	}
}

func TestCheckResult_IsCritical(t *testing.T) {
	tests := []struct {
		name     string
		result   CheckResult
		expected bool
	}{
		{
			name:     "required pass is not critical",
			result:   CheckResult{Status: StatusPass, Required: true},
			expected: false,
		},
		{
			name:     "required fail is critical",
			result:   CheckResult{Status: StatusFail, Required: true},
			expected: true,
		},
		{
			name:     "optional fail is not critical",
			result:   CheckResult{Status: StatusFail, Required: false},
			expected: false,
		},
		{
			name:     "required warn is not critical",
			result:   CheckResult{Status: StatusWarn, Required: true},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.result.IsCritical())
		})
	}
}

func TestChecker_New(t *testing.T) {
	// Given: default options
	checker := New()

	// Then: checker is created with defaults
	assert.NotNil(t, checker)
	assert.False(t, checker.offline)
	assert.False(t, checker.verbose)
}

func TestChecker_NewWithOptions(t *testing.T) {
	// Given: custom options
	buf := &bytes.Buffer{}
	checker := New(
		WithOffline(true),
		WithVerbose(true),
		WithOutput(buf),
	)

	// Then: options are applied
	assert.True(t, checker.offline)
	assert.True(t, checker.verbose)
	assert.Equal(t, buf, checker.output)
}

func TestChecker_HasCriticalFailures(t *testing.T) {
	checker := New()

	tests := []struct {
		name     string
		results  []CheckResult
		expected bool
	}{
		{
			name:     "no results",
			results:  []CheckResult{},
			expected: false,
		},
		{
			name: "all pass",
			results: []CheckResult{
				{Status: StatusPass, Required: true},
				{Status: StatusPass, Required: true},
			},
			expected: false,
		},
		{
			name: "warning only",
			results: []CheckResult{
				{Status: StatusPass, Required: true},
				{Status: StatusWarn, Required: false},
			},
			expected: false,
		},
		{
			name: "optional failure",
			results: []CheckResult{
				{Status: StatusPass, Required: true},
				{Status: StatusFail, Required: false},
			},
			expected: false,
		},
		{
			name: "required failure",
			results: []CheckResult{
				{Status: StatusPass, Required: true},
				{Status: StatusFail, Required: true},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, checker.HasCriticalFailures(tt.results))
		})
	}
}

func TestChecker_CheckWritePermissions_Writable(t *testing.T) {
	// Given: a writable directory
	tmpDir := t.TempDir()

	// When: checking write permissions
	checker := New()
	result := checker.CheckWritePermissions(tmpDir)

	// Then: passes
	assert.Equal(t, StatusPass, result.Status)
	assert.Equal(t, "write_permissions", result.Name)
	assert.True(t, result.Required)
}

func TestChecker_CheckWritePermissions_ReadOnly(t *testing.T) {
	// Given: a read-only directory (skip on CI/root)
	if os.Getuid() == 0 {
		t.Skip("Skipping read-only test when running as root")
	}

	tmpDir := t.TempDir()
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	require.NoError(t, os.Mkdir(readOnlyDir, 0555))
	defer func() { _ = os.Chmod(readOnlyDir, 0755) }() // Restore for cleanup

	// When: checking write permissions
	checker := New()
	result := checker.CheckWritePermissions(readOnlyDir)

	// Then: fails
	assert.Equal(t, StatusFail, result.Status)
	assert.Contains(t, result.Message, "permission denied")
}

func TestChecker_RunAll_ReturnsAllChecks(t *testing.T) {
	// Given: a valid directory
	tmpDir := t.TempDir()
	checker := New(WithOffline(true)) // Run in offline mode

	// When: running all checks
	ctx := context.Background()
	results := checker.RunAll(ctx, tmpDir)

	// Then: returns multiple check results
	assert.NotEmpty(t, results)

	// Verify expected checks are present
	checkNames := make(map[string]bool)
	for _, r := range results {
		checkNames[r.Name] = true
	}

	assert.True(t, checkNames["disk_space"], "disk_space check missing")
	assert.True(t, checkNames["memory"], "memory check missing")
	assert.True(t, checkNames["write_permissions"], "write_permissions check missing")
	assert.True(t, checkNames["file_descriptors"], "file_descriptors check missing")
}

func TestChecker_PrintResults(t *testing.T) {
	// Given: some check results
	results := []CheckResult{
		{Name: "disk_space", Status: StatusPass, Message: "50 GB free"},
		{Name: "embedder", Status: StatusWarn, Message: "Using static fallback"},
		{Name: "memory", Status: StatusFail, Message: "Insufficient", Required: true},
	}

	buf := &bytes.Buffer{}
	checker := New(WithOutput(buf))

	// When: printing results
	checker.PrintResults(results)

	// Then: output contains formatted results
	output := buf.String()
	assert.Contains(t, output, "[PASS]")
	assert.Contains(t, output, "[WARN]")
	assert.Contains(t, output, "[FAIL]")
	assert.Contains(t, output, "disk_space")
}

func TestChecker_SummaryStatus(t *testing.T) {
	checker := New()

	tests := []struct {
		name     string
		results  []CheckResult
		expected string
	}{
		{
			name: "all pass",
			results: []CheckResult{
				{Status: StatusPass},
				{Status: StatusPass},
			},
			expected: "ready",
		},
		{
			name: "with warnings",
			results: []CheckResult{
				{Status: StatusPass},
				{Status: StatusWarn},
			},
			expected: "ready_with_warnings",
		},
		{
			name: "with critical failure",
			results: []CheckResult{
				{Status: StatusPass},
				{Status: StatusFail, Required: true},
			},
			expected: "failed",
		},
		{
			name: "with optional failure",
			results: []CheckResult{
				{Status: StatusPass},
				{Status: StatusFail, Required: false},
			},
			expected: "ready_with_warnings",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, checker.SummaryStatus(tt.results))
		})
	}
}
