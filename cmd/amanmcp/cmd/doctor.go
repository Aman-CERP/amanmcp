package cmd

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/preflight"
)

func newDoctorCmd() *cobra.Command {
	var (
		verbose    bool
		jsonOutput bool
		offline    bool
	)

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check system requirements and diagnose issues",
		Long: `Run system diagnostics to ensure AmanMCP can operate correctly.

Checks:
  - Disk space (100MB minimum)
  - Memory availability (1GB minimum)
  - Write permissions
  - File descriptor limits (1024 minimum)
  - Embedder model status (downloaded/missing)
  - Embedder disk space

Note: Embedder checks are non-critical warnings.
If embedder model fails to download, AmanMCP falls back to static embeddings.

Use --verbose for detailed diagnostic information.
Use --json for machine-readable output.`,
		Example: `  # Run diagnostics
  amanmcp doctor

  # Verbose output with details
  amanmcp doctor --verbose

  # JSON output for scripting
  amanmcp doctor --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDoctor(cmd, verbose, jsonOutput, offline)
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed diagnostic info")
	cmd.Flags().Bool("json", false, "Output as JSON")
	// Note: --offline flag kept for backwards compatibility but has no effect
	cmd.Flags().BoolVar(&offline, "offline", false, "Reserved for future use")

	// Bind --json flag manually since it's a reserved word
	_ = cmd.Flags().Lookup("json").Value.Set("false")
	cmd.PreRunE = func(cmd *cobra.Command, _ []string) error {
		jsonOutput, _ = cmd.Flags().GetBool("json")
		return nil
	}

	return cmd
}

func runDoctor(cmd *cobra.Command, verbose, jsonOutput, offline bool) error {
	// Set up context with signal handling (uses signal.NotifyContext to prevent goroutine leaks)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Find project root
	root, err := config.FindProjectRoot(".")
	if err != nil {
		root, _ = os.Getwd()
	}

	// Create checker
	checker := preflight.New(
		preflight.WithOffline(offline),
		preflight.WithVerbose(verbose),
		preflight.WithOutput(cmd.OutOrStdout()),
	)

	// Run all checks
	results := checker.RunAll(ctx, root)

	// Output results
	if jsonOutput {
		return outputJSON(cmd, checker, results)
	}

	checker.PrintResults(results)

	// Check for marker status
	dataDir := filepath.Join(root, ".amanmcp")
	if !preflight.NeedsCheck(dataDir) {
		age := preflight.MarkerAge(dataDir)
		if age > 0 {
			cmd.Printf("\nLast successful check: %s ago\n", formatDuration(age))
		}
	}

	// Return error if critical failures
	if checker.HasCriticalFailures(results) {
		return &doctorError{message: "system check failed"}
	}

	return nil
}

// doctorError is a custom error for doctor command failures.
type doctorError struct {
	message string
}

func (e *doctorError) Error() string {
	return e.message
}

// JSONOutput is the structure for JSON output.
type JSONOutput struct {
	Status   string             `json:"status"`
	Checks   []JSONCheckResult  `json:"checks"`
	Warnings []string           `json:"warnings,omitempty"`
	Errors   []string           `json:"errors,omitempty"`
}

// JSONCheckResult is a single check result for JSON output.
type JSONCheckResult struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Message  string `json:"message"`
	Required bool   `json:"required"`
	Details  string `json:"details,omitempty"`
}

func outputJSON(cmd *cobra.Command, checker *preflight.Checker, results []preflight.CheckResult) error {
	output := JSONOutput{
		Status: checker.SummaryStatus(results),
		Checks: make([]JSONCheckResult, len(results)),
	}

	for i, r := range results {
		output.Checks[i] = JSONCheckResult{
			Name:     r.Name,
			Status:   statusToString(r.Status),
			Message:  r.Message,
			Required: r.Required,
			Details:  r.Details,
		}

		if r.IsCritical() {
			output.Errors = append(output.Errors, r.Name+": "+r.Message)
		} else if r.Status == preflight.StatusWarn {
			output.Warnings = append(output.Warnings, r.Name+": "+r.Message)
		}
	}

	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

func statusToString(s preflight.CheckStatus) string {
	switch s {
	case preflight.StatusPass:
		return "pass"
	case preflight.StatusWarn:
		return "warn"
	case preflight.StatusFail:
		return "fail"
	default:
		return "unknown"
	}
}

func formatDuration(d interface{ Hours() float64 }) string {
	hours := d.Hours()
	if hours < 1 {
		return "less than 1 hour"
	}
	if hours < 24 {
		return formatHours(int(hours))
	}
	days := int(hours / 24)
	if days == 1 {
		return "1 day"
	}
	return formatDays(days)
}

func formatHours(h int) string {
	if h == 1 {
		return "1 hour"
	}
	return string(rune('0'+h/10)) + string(rune('0'+h%10)) + " hours"
}

func formatDays(d int) string {
	if d < 10 {
		return string(rune('0'+d)) + " days"
	}
	return string(rune('0'+d/10)) + string(rune('0'+d%10)) + " days"
}
