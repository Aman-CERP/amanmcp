// Package preflight provides system validation and pre-flight checks
// to ensure AmanMCP can run successfully before starting operations.
//
// The package validates:
//   - Disk space availability (minimum 100MB)
//   - Memory availability (minimum 1GB)
//   - Write permissions in project directory
//   - File descriptor limits (minimum 1024)
//   - Configuration validity
//
// Use the Checker type to run all validations:
//
//	checker := preflight.New()
//	results := checker.RunAll(ctx, "/path/to/project")
//	if checker.HasCriticalFailures(results) {
//	    // Handle failures
//	}
package preflight
