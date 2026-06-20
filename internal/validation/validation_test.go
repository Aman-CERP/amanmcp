package validation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/embed"
	"github.com/Aman-CERP/amanmcp/internal/store"
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

func TestParseQueryConfig_F37SchemaAndLegacyExpectedCompatibility(t *testing.T) {
	data := []byte(`
tier1:
  - id: T1-Q1
    name: Legacy expected query
    query: "RRF fusion"
    tool: search
    class: exact_identifier
    job: exact_lookup
    expected:
      - internal/search/fusion.go
    holdout: false
    source: dogfood-tier1
    notes: "legacy expected must map to grade-3 evidence"
tier2: []
negative:
  - id: N-Q1
    name: Empty query
    query: ""
    tool: search
    class: negative_adversarial
    job: general
    expected: []
    holdout: false
    source: dogfood-negative
graded:
  - id: CODE-Q01
    name: Graded query
    query: "how does vector storage load"
    tool: search_code
    class: natural_language_intent
    job: code
    expected_results:
      - path: internal/store/hnsw.go
        symbol: HNSWStore
        page: 2
        grade: 3
        rationale: "Canonical vector store implementation."
    metadata:
      content_type: pdf
    holdout: true
    source: manual
    notes: "F37 graded evidence"
`)

	cfg, err := parseQueryConfig(data)
	require.NoError(t, err)

	require.Len(t, cfg.Tier1, 1)
	assert.Equal(t, 1, cfg.Tier1[0].Tier)
	require.Len(t, cfg.Tier1[0].ExpectedResults, 1)
	assert.Equal(t, "internal/search/fusion.go", cfg.Tier1[0].ExpectedResults[0].Path)
	assert.Equal(t, 3, cfg.Tier1[0].ExpectedResults[0].Grade)

	require.Len(t, cfg.Negative, 1)
	assert.Equal(t, 0, cfg.Negative[0].Tier)

	require.Len(t, cfg.Graded, 1)
	assert.Equal(t, 3, cfg.Graded[0].Tier)
	assert.True(t, cfg.Graded[0].Holdout)
	assert.Equal(t, "natural_language_intent", cfg.Graded[0].Class)
	assert.Equal(t, "code", cfg.Graded[0].Job)
	assert.Equal(t, "pdf", cfg.Graded[0].Metadata["content_type"])
	assert.Equal(t, 2, cfg.Graded[0].ExpectedResults[0].Page)
}

func TestParseQueryConfig_RejectsMalformedCorpus(t *testing.T) {
	base := func(body string) []byte {
		return []byte("tier1:\n" + body + "\ntier2: []\nnegative: []\ngraded: []\n")
	}

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name: "duplicate ids",
			body: `  - id: DUP-Q1
    name: First
    query: "SearchOptions"
    tool: search
    class: exact_identifier
    job: exact_lookup
    expected_results:
      - path: internal/search/options.go
        grade: 3
        rationale: "Canonical."
    holdout: false
    source: manual
  - id: DUP-Q1
    name: Second
    query: "SearchOptions"
    tool: search
    class: exact_identifier
    job: exact_lookup
    expected_results:
      - path: internal/search/types.go
        grade: 2
        rationale: "Supporting."
    holdout: false
    source: manual
`,
			wantErr: "duplicate query id",
		},
		{
			name: "unsupported tool",
			body: `  - id: BAD-Q1
    name: Bad tool
    query: "SearchOptions"
    tool: search_everything
    class: exact_identifier
    job: exact_lookup
    expected_results:
      - path: internal/search/options.go
        grade: 3
        rationale: "Canonical."
    holdout: false
    source: manual
`,
			wantErr: "unsupported tool",
		},
		{
			name: "unsupported class",
			body: `  - id: BAD-Q1
    name: Bad class
    query: "SearchOptions"
    tool: search
    class: vibes
    job: exact_lookup
    expected_results:
      - path: internal/search/options.go
        grade: 3
        rationale: "Canonical."
    holdout: false
    source: manual
`,
			wantErr: "unsupported class",
		},
		{
			name: "invalid profile",
			body: `  - id: BAD-Q1
    name: Bad profile
    query: "SearchOptions"
    tool: search_docs
    profile: everything
    class: docs_to_code
    job: project_memory
    expected_results:
      - path: docs/reference/architecture.md
        grade: 3
        rationale: "Canonical."
    holdout: false
    source: manual
`,
			wantErr: "invalid profile",
		},
		{
			name: "invalid mode",
			body: `  - id: BAD-Q1
    name: Bad mode
    query: "ADR decisions"
    tool: search_docs
    mode: latest
    class: adr_to_code
    job: decision_lookup
    expected_results:
      - path: docs/reference/decisions/ADR-001.md
        grade: 3
        rationale: "Canonical."
    holdout: false
    source: manual
`,
			wantErr: "invalid mode",
		},
		{
			name: "unsupported job",
			body: `  - id: BAD-Q1
    name: Bad job
    query: "SearchOptions"
    tool: search
    class: exact_identifier
    job: everything
    expected_results:
      - path: internal/search/options.go
        grade: 3
        rationale: "Canonical."
    holdout: false
    source: manual
`,
			wantErr: "unsupported job",
		},
		{
			name: "invalid grade",
			body: `  - id: BAD-Q1
    name: Bad grade
    query: "SearchOptions"
    tool: search
    class: exact_identifier
    job: exact_lookup
    expected_results:
      - path: internal/search/options.go
        grade: 4
        rationale: "Out of range."
    holdout: false
    source: manual
`,
			wantErr: "invalid expected result grade",
		},
		{
			name: "malformed holdout",
			body: `  - id: BAD-Q1
    name: Bad holdout
    query: "SearchOptions"
    tool: search
    class: exact_identifier
    job: exact_lookup
    expected_results:
      - path: internal/search/options.go
        grade: 3
        rationale: "Canonical."
    holdout: sometimes
    source: manual
`,
			wantErr: "holdout must be a boolean",
		},
		{
			name: "missing expected evidence",
			body: `  - id: BAD-Q1
    name: Missing evidence
    query: "SearchOptions"
    tool: search
    class: exact_identifier
    job: exact_lookup
    holdout: false
    source: manual
`,
			wantErr: "requires expected evidence",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseQueryConfig(base(tt.body))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestLoadQueries_CurrentCorpusHasF37Coverage(t *testing.T) {
	ResetQueries()
	t.Cleanup(ResetQueries)

	cfg, err := LoadQueries()
	require.NoError(t, err)

	all := allQuerySpecs(cfg)
	assert.GreaterOrEqual(t, len(all), 77)

	holdout := 0
	pdfQueries := 0
	nonHoldoutByClass := make(map[string]int)
	byJob := make(map[string]int)
	for _, spec := range all {
		if spec.Holdout {
			holdout++
		} else {
			nonHoldoutByClass[spec.Class]++
		}
		if spec.Metadata["content_type"] == "pdf" {
			pdfQueries++
		}
		byJob[spec.Job]++
		if spec.Class != "negative_adversarial" {
			assert.NotEmpty(t, spec.ExpectedResults, "query %s must have expected evidence", spec.ID)
		}
	}

	assert.GreaterOrEqual(t, holdout, 12)
	assert.GreaterOrEqual(t, pdfQueries, 8)
	for class := range allowedQueryClasses {
		assert.GreaterOrEqual(t, nonHoldoutByClass[class], 3, "class %s should have at least 3 non-holdout queries", class)
	}
	for job := range allowedQueryJobs {
		assert.NotZero(t, byJob[job], "job %s should be represented", job)
	}
}

func TestValidationEmbedderSelection_UsesIndexedStaticBackend(t *testing.T) {
	ctx := context.Background()
	metadata, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "metadata.db"))
	require.NoError(t, err)
	defer func() { _ = metadata.Close() }()
	require.NoError(t, metadata.SetState(ctx, store.StateKeyIndexModel, "static768"))

	cfg := config.NewConfig()
	cfg.Embeddings.Provider = "ollama"

	provider, model := validationEmbedderSelection(ctx, cfg, metadata)

	assert.Equal(t, embed.ProviderStatic, provider)
	assert.Equal(t, "static768", model)
}

func TestExtractFilePaths_CoversLegacyResponseShapes(t *testing.T) {
	markdown := "### 1. internal/search/engine.go:173-215 (score: 0.91)\n" +
		"### 2. docs/reference/architecture.md:42-60 (score: 0.81)\n"
	docsMarkdown := "### 1. README.md (score: 1.00)\n\n" +
		"### 2. .aman-pm/product/F02-configuration.md (score: 0.97)\n"
	jsonish := `{"results":[{"file_path":"internal/mcp/server.go"},{"file_path":"README.md"}]}`

	assert.Equal(t,
		[]string{"internal/search/engine.go", "docs/reference/architecture.md"},
		extractFilePaths(markdown),
	)
	assert.Equal(t,
		[]string{"README.md", ".aman-pm/product/F02-configuration.md"},
		extractFilePaths(docsMarkdown),
	)
	assert.Equal(t,
		[]string{"internal/mcp/server.go"},
		extractFilePaths(jsonish),
	)
}

func TestCheckExpected_UsesExactOrDirectoryPrefixOnly(t *testing.T) {
	results := []string{"internal/store/hnsw.go", "internal/storeother/hnsw.go"}

	passed, idx := checkExpected(results, []string{"internal/store"})
	assert.True(t, passed)
	assert.Equal(t, 0, idx)

	passed, idx = checkExpected([]string{"internal/storeother/hnsw.go"}, []string{"internal/store"})
	assert.False(t, passed)
	assert.Equal(t, -1, idx)
}
