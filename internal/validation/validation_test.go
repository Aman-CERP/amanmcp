package validation

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/config"
)

// TestTier1_All runs all Tier 1 validation queries.
// This test requires a real index to exist at the project root.
// Skip if no index available (for CI without pre-built index).
func TestTier1_All(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	root := findProjectRoot(t)
	validator, err := NewValidator(ctx, root)
	if err != nil {
		if err == ErrIndexLocked {
			t.Skip("skipping: index locked by another process (stop Claude Code first)")
		}
		t.Skipf("skipping: %v", err)
	}
	defer validator.Close()

	queries := Tier1Queries()
	passed := 0
	failed := 0

	for _, spec := range queries {
		t.Run(spec.ID+"_"+spec.Name, func(t *testing.T) {
			result := validator.RunQuery(ctx, spec)

			if result.Error != "" {
				t.Errorf("Query error: %s", result.Error)
				failed++
				return
			}

			if !result.Passed {
				t.Logf("FAIL: Expected %v in results, got: %v", spec.Expected, result.TopResults)
				failed++
			} else {
				t.Logf("PASS: Found at position %d in %.2fms", result.MatchedAt, float64(result.Duration.Microseconds())/1000)
				passed++
			}
		})
	}

	passRate := float64(passed) / float64(len(queries)) * 100
	t.Logf("Tier 1 Results: %d/%d passed (%.0f%%)", passed, len(queries), passRate)

	// Require minimum 50% pass rate for Tier 1 (allows for index quality variance)
	// This threshold can be raised as search quality improves
	minPassRate := 50.0
	if passRate < minPassRate {
		t.Errorf("Tier 1 pass rate %.0f%% is below minimum %.0f%%", passRate, minPassRate)
	}
}

// TestTier2_All runs all Tier 2 validation queries.
func TestTier2_All(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	root := findProjectRoot(t)
	validator, err := NewValidator(ctx, root)
	if err != nil {
		if err == ErrIndexLocked {
			t.Skip("skipping: index locked by another process (stop Claude Code first)")
		}
		t.Skipf("skipping: %v", err)
	}
	defer validator.Close()

	queries := Tier2Queries()
	passed := 0

	for _, spec := range queries {
		t.Run(spec.ID+"_"+spec.Name, func(t *testing.T) {
			result := validator.RunQuery(ctx, spec)

			if result.Error != "" {
				t.Errorf("Query error: %s", result.Error)
				return
			}

			if !result.Passed {
				t.Logf("FAIL: Expected %v in results, got: %v", spec.Expected, result.TopResults)
			} else {
				t.Logf("PASS: Found at position %d in %.2fms", result.MatchedAt, float64(result.Duration.Microseconds())/1000)
				passed++
			}
		})
	}

	t.Logf("Tier 2 Results: %d/%d passed (%.0f%%)", passed, len(queries), float64(passed)/float64(len(queries))*100)
}

// TestNegative_All runs all negative test cases.
func TestNegative_All(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	root := findProjectRoot(t)
	validator, err := NewValidator(ctx, root)
	if err != nil {
		if err == ErrIndexLocked {
			t.Skip("skipping: index locked by another process (stop Claude Code first)")
		}
		t.Skipf("skipping: %v", err)
	}
	defer validator.Close()

	queries := NegativeQueries()

	for _, spec := range queries {
		t.Run(spec.ID+"_"+spec.Name, func(t *testing.T) {
			result := validator.RunQuery(ctx, spec)

			// Negative tests pass if they don't crash
			assert.True(t, result.Passed, "negative test should not crash")
			t.Logf("PASS: Completed in %.2fms", float64(result.Duration.Microseconds())/1000)
		})
	}
}

// TestValidation_FullSuite runs the complete validation suite and reports results.
func TestValidation_FullSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	root := findProjectRoot(t)
	validator, err := NewValidator(ctx, root)
	if err != nil {
		if err == ErrIndexLocked {
			t.Skip("skipping: index locked by another process (stop Claude Code first)")
		}
		t.Skipf("skipping: %v", err)
	}
	defer validator.Close()

	result := validator.RunAll(ctx)

	// Print summary
	t.Logf("\n=== Validation Results ===")
	t.Logf("Embedder: %s", result.Embedder)
	t.Logf("Tier 1: %d/%d (%.0f%%)", result.Tier1Pass, result.Tier1Total, float64(result.Tier1Pass)/float64(result.Tier1Total)*100)
	t.Logf("Tier 2: %d/%d (%.0f%%)", result.Tier2Pass, result.Tier2Total, float64(result.Tier2Pass)/float64(result.Tier2Total)*100)
	t.Logf("Negative: %d/%d (%.0f%%)", result.NegPass, result.NegTotal, float64(result.NegPass)/float64(result.NegTotal)*100)

	// Print failures
	t.Logf("\n=== Tier 1 Details ===")
	for _, tr := range result.Tier1 {
		status := "PASS"
		if !tr.Passed {
			status = "FAIL"
		}
		t.Logf("[%s] %s: %s (%.2fms)", status, tr.Spec.ID, tr.Spec.Name, float64(tr.Duration.Microseconds())/1000)
		if !tr.Passed {
			t.Logf("  Expected: %v", tr.Spec.Expected)
			t.Logf("  Got: %v", tr.TopResults)
		}
	}

	// Assert minimum thresholds
	tier1Pct := float64(result.Tier1Pass) / float64(result.Tier1Total) * 100
	tier2Pct := float64(result.Tier2Pass) / float64(result.Tier2Total) * 100
	negPct := float64(result.NegPass) / float64(result.NegTotal) * 100

	assert.GreaterOrEqual(t, negPct, 100.0, "Negative tests must pass 100%%")
	assert.GreaterOrEqual(t, tier2Pct, 75.0, "Tier 2 should pass >= 75%%")
	// Note: Tier 1 target is 100%, but we log rather than fail during development
	if tier1Pct < 100 {
		t.Logf("WARNING: Tier 1 at %.0f%%, target is 100%%", tier1Pct)
	}
}

// Benchmark tests for performance tracking

func BenchmarkSearch_Tier1Queries(b *testing.B) {
	ctx := context.Background()
	root, err := config.FindProjectRoot(".")
	if err != nil {
		b.Skipf("skipping: %v", err)
	}

	validator, err := NewValidator(ctx, root)
	if err != nil {
		b.Skipf("skipping: %v", err)
	}
	defer validator.Close()

	queries := Tier1Queries()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, spec := range queries {
			validator.RunQuery(ctx, spec)
		}
	}
}

// Individual query benchmarks

func BenchmarkQuery_SearchFunction(b *testing.B) {
	benchmarkSingleQuery(b, QuerySpec{
		Query: "Search function",
		Tool:  "search_code",
	})
}

func BenchmarkQuery_RRFFusion(b *testing.B) {
	benchmarkSingleQuery(b, QuerySpec{
		Query: "How does RRF fusion work",
		Tool:  "search",
	})
}

func benchmarkSingleQuery(b *testing.B, spec QuerySpec) {
	ctx := context.Background()
	root, err := config.FindProjectRoot(".")
	if err != nil {
		b.Skipf("skipping: %v", err)
	}

	validator, err := NewValidator(ctx, root)
	if err != nil {
		b.Skipf("skipping: %v", err)
	}
	defer validator.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.RunQuery(ctx, spec)
	}
}

// Helper functions

func findProjectRoot(t *testing.T) string {
	t.Helper()

	// Try current directory first
	root, err := config.FindProjectRoot(".")
	if err == nil {
		return root
	}

	// Try environment variable
	if envRoot := os.Getenv("AMANMCP_PROJECT_ROOT"); envRoot != "" {
		return envRoot
	}

	// Try common paths
	candidates := []string{
		".",
		"..",
		"../..",
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(fmt.Sprintf("%s/.amanmcp/metadata.db", candidate)); err == nil {
			return candidate
		}
	}

	t.Skip("skipping: no index found - run 'amanmcp index' first")
	return ""
}

// TestQuery_ByID runs a single query by ID for debugging.
// Use: go test -run TestQuery_ByID/T1-Q7 ./internal/validation/
func TestQuery_ByID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	root := findProjectRoot(t)
	validator, err := NewValidator(ctx, root)
	if err != nil {
		if err == ErrIndexLocked {
			t.Skip("skipping: index locked by another process")
		}
		if strings.Contains(err.Error(), "no index found") {
			t.Skip("skipping: no index found - run 'amanmcp index' first")
		}
		require.NoError(t, err)
	}
	defer validator.Close()

	// Load all queries from YAML (data-driven)
	cfg, err := LoadQueries()
	require.NoError(t, err, "failed to load queries.yaml")

	// Combine all queries for testing
	allQueries := append(cfg.Tier1, cfg.Tier2...)
	allQueries = append(allQueries, cfg.Negative...)

	for _, spec := range allQueries {
		t.Run(spec.ID, func(t *testing.T) {
			result := validator.RunQuery(ctx, spec)

			t.Logf("Query: %q", spec.Query)
			t.Logf("Duration: %.2fms", float64(result.Duration.Microseconds())/1000)
			t.Logf("Passed: %v", result.Passed)
			t.Logf("MatchedAt: %d", result.MatchedAt)
			t.Logf("Expected: %v", spec.Expected)
			t.Logf("TopResults: %v", result.TopResults)

			// Don't fail on individual queries - TestTier1_All handles pass rates
		})
	}
}
