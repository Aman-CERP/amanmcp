package preflight

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CheckStatus represents the result of a preflight check.
type CheckStatus int

const (
	// StatusPass indicates the check passed successfully.
	StatusPass CheckStatus = iota
	// StatusWarn indicates a non-critical warning.
	StatusWarn
	// StatusFail indicates the check failed.
	StatusFail
)

// String returns the string representation of a CheckStatus.
func (s CheckStatus) String() string {
	switch s {
	case StatusPass:
		return "PASS"
	case StatusWarn:
		return "WARN"
	case StatusFail:
		return "FAIL"
	default:
		return "UNKNOWN"
	}
}

// CheckResult holds the result of a single preflight check.
type CheckResult struct {
	Name     string      `json:"name"`
	Status   CheckStatus `json:"status"`
	Message  string      `json:"message"`
	Details  string      `json:"details,omitempty"`
	Required bool        `json:"required"`
}

// IsCritical returns true if this is a required check that failed.
func (r CheckResult) IsCritical() bool {
	return r.Required && r.Status == StatusFail
}

// Checker performs preflight validation checks.
type Checker struct {
	offline bool
	verbose bool
	output  io.Writer
}

// Option configures a Checker.
type Option func(*Checker)

// WithOffline sets offline mode (reserved for future use).
func WithOffline(offline bool) Option {
	return func(c *Checker) {
		c.offline = offline
	}
}

// WithVerbose enables verbose output.
func WithVerbose(verbose bool) Option {
	return func(c *Checker) {
		c.verbose = verbose
	}
}

// WithOutput sets the output writer.
func WithOutput(w io.Writer) Option {
	return func(c *Checker) {
		c.output = w
	}
}

// New creates a new Checker with the given options.
func New(opts ...Option) *Checker {
	c := &Checker{
		output: os.Stdout,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// RunAll runs all preflight checks and returns the results.
func (c *Checker) RunAll(_ context.Context, projectPath string) []CheckResult {
	var results []CheckResult

	// Disk space check
	results = append(results, c.CheckDiskSpace(projectPath))

	// Memory check
	results = append(results, c.CheckMemory())

	// Write permissions check
	results = append(results, c.CheckWritePermissions(projectPath))

	// File descriptors check
	results = append(results, c.CheckFileDescriptors())

	// Embedder checks (non-critical - can fall back to static)
	results = append(results, c.CheckEmbedderModel())
	results = append(results, c.CheckEmbedderDiskSpace())

	return results
}

// HasCriticalFailures returns true if any required check failed.
func (c *Checker) HasCriticalFailures(results []CheckResult) bool {
	for _, r := range results {
		if r.IsCritical() {
			return true
		}
	}
	return false
}

// SummaryStatus returns a summary status string for the results.
func (c *Checker) SummaryStatus(results []CheckResult) string {
	hasWarnings := false
	hasCriticalFailure := false

	for _, r := range results {
		if r.IsCritical() {
			hasCriticalFailure = true
		}
		if r.Status == StatusWarn || (r.Status == StatusFail && !r.Required) {
			hasWarnings = true
		}
	}

	if hasCriticalFailure {
		return "failed"
	}
	if hasWarnings {
		return "ready_with_warnings"
	}
	return "ready"
}

// PrintResults prints check results to the configured output.
func (c *Checker) PrintResults(results []CheckResult) {
	_, _ = fmt.Fprintln(c.output, "AmanMCP System Check")
	_, _ = fmt.Fprintln(c.output, "====================")
	_, _ = fmt.Fprintln(c.output)

	for _, r := range results {
		icon := c.statusIcon(r.Status)
		_, _ = fmt.Fprintf(c.output, "[%s] %s: %s\n", icon, r.Name, r.Message)
		if c.verbose && r.Details != "" {
			_, _ = fmt.Fprintf(c.output, "      %s\n", r.Details)
		}
	}

	_, _ = fmt.Fprintln(c.output)
	status := c.SummaryStatus(results)
	_, _ = fmt.Fprintf(c.output, "Status: %s\n", strings.ToUpper(status))

	// Print summary of issues
	var warnings, errors []string
	for _, r := range results {
		if r.IsCritical() {
			errors = append(errors, r.Name+": "+r.Message)
		} else if r.Status == StatusWarn {
			warnings = append(warnings, r.Name+": "+r.Message)
		}
	}

	if len(errors) > 0 {
		_, _ = fmt.Fprintln(c.output)
		_, _ = fmt.Fprintf(c.output, "%d error(s):\n", len(errors))
		for _, e := range errors {
			_, _ = fmt.Fprintf(c.output, "  - %s\n", e)
		}
	}

	if len(warnings) > 0 {
		_, _ = fmt.Fprintln(c.output)
		_, _ = fmt.Fprintf(c.output, "%d warning(s):\n", len(warnings))
		for _, w := range warnings {
			_, _ = fmt.Fprintf(c.output, "  - %s\n", w)
		}
	}
}

func (c *Checker) statusIcon(status CheckStatus) string {
	switch status {
	case StatusPass:
		return "PASS"
	case StatusWarn:
		return "WARN"
	case StatusFail:
		return "FAIL"
	default:
		return "????"
	}
}

// CheckWritePermissions checks if we can write to the project directory.
func (c *Checker) CheckWritePermissions(path string) CheckResult {
	result := CheckResult{
		Name:     "write_permissions",
		Required: true,
	}

	// Try to create a temp file
	testFile := filepath.Join(path, ".amanmcp-preflight-test")
	f, err := os.Create(testFile)
	if err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("permission denied: %v", err)
		return result
	}
	_ = f.Close()
	_ = os.Remove(testFile)

	result.Status = StatusPass
	result.Message = "OK"
	return result
}

