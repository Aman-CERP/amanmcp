package eval

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadCorpus_FailsMalformedCorpus(t *testing.T) {
	path := writeTempCorpus(t, `
queries:
  - id: Q1
    name: missing evidence
    query: "where is search"
    tool: search
    class: exact_identifier
    job: code
    holdout: false
    source: manual
`)

	_, err := LoadCorpus(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected evidence")
}

func TestLoadCorpus_FailsDuplicateIDs(t *testing.T) {
	path := writeTempCorpus(t, `
queries:
  - id: Q1
    name: first
    query: "alpha"
    tool: search
    class: exact_identifier
    job: code
    expected_results:
      - path: internal/search/engine.go
        grade: 3
        rationale: owner
    holdout: false
    source: manual
  - id: Q1
    name: second
    query: "beta"
    tool: search
    class: exact_identifier
    job: code
    expected_results:
      - path: internal/search/types.go
        grade: 3
        rationale: owner
    holdout: false
    source: manual
`)

	_, err := LoadCorpus(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate query id")
}

func TestLoadCorpus_IncludesGradedSection(t *testing.T) {
	path := writeTempCorpus(t, `
tier1: []
tier2: []
negative: []
graded:
  - id: G1
    name: graded query
    query: "vector storage"
    tool: search
    class: natural_language_intent
    job: code
    expected_results:
      - path: internal/store/hnsw.go
        grade: 3
        rationale: owner
    holdout: false
    source: manual
`)

	corpus, err := LoadCorpus(path)

	require.NoError(t, err)
	require.Len(t, corpus.Queries, 1)
	assert.Equal(t, "G1", corpus.Queries[0].ID)
	assert.Equal(t, "graded", corpus.Queries[0].Tier)
}

func TestSelectQueries_Subsets(t *testing.T) {
	corpus := Corpus{Queries: []Query{
		query("Q1", "exact_identifier", "code", false),
		query("Q2", "docs_to_code", "project_memory", true),
		query("Q3", "natural_language_intent", "code", false),
		query("Q4", "quoted_string", "exact_lookup", false),
	}}

	tests := []struct {
		name           string
		subset         string
		includeHoldout bool
		wantIDs        []string
	}{
		{name: "full excludes holdout", subset: "full", wantIDs: []string{"Q1", "Q3", "Q4"}},
		{name: "full includes holdout when requested", subset: "full", includeHoldout: true, wantIDs: []string{"Q1", "Q2", "Q3", "Q4"}},
		{name: "holdout only", subset: "holdout", wantIDs: []string{"Q2"}},
		{name: "class filter", subset: "class:exact_identifier", wantIDs: []string{"Q1"}},
		{name: "quoted string class filter", subset: "class:quoted_string", wantIDs: []string{"Q4"}},
		{name: "job filter", subset: "job:project_memory", wantIDs: []string{}},
		{name: "job filter with holdout", subset: "job:project_memory", includeHoldout: true, wantIDs: []string{"Q2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SelectQueries(corpus.Queries, Selection{
				Subset:         tt.subset,
				IncludeHoldout: tt.includeHoldout,
			})

			require.NoError(t, err)
			assert.Equal(t, tt.wantIDs, queryIDs(got))
		})
	}
}

func TestSelectQueries_GraphGateSubsetExcludesHoldoutAndIncludesExactRegression(t *testing.T) {
	corpus := Corpus{Queries: []Query{
		query("GRAPH-Q1", "caller_callee", "code", false),
		query("GRAPH-HOLDOUT", "impact_analysis", "code", true),
		query("EXACT-Q1", "exact_identifier", "exact_lookup", false),
		query("PATH-Q1", "path_lookup", "exact_lookup", false),
		query("QUOTE-Q1", "quoted_string", "exact_lookup", false),
		query("NL-Q1", "natural_language_intent", "code", false),
	}}

	got, err := SelectQueries(corpus.Queries, Selection{Subset: "graph"})

	require.NoError(t, err)
	assert.Equal(t, []string{"GRAPH-Q1", "EXACT-Q1", "PATH-Q1", "QUOTE-Q1"}, queryIDs(got))

	got, err = SelectQueries(corpus.Queries, Selection{Subset: "graph", IncludeHoldout: true})

	require.NoError(t, err)
	assert.Equal(t, []string{"GRAPH-Q1", "GRAPH-HOLDOUT", "EXACT-Q1", "PATH-Q1", "QUOTE-Q1"}, queryIDs(got))
}

func TestRunner_ExactLookupGateFailsClosedWithoutQualityBaseline(t *testing.T) {
	corpusPath := writeTempCorpus(t, `
queries:
  - id: EXACT-Q1
    name: exact owner
    query: "SearchOptions"
    tool: search_code
    class: exact_identifier
    job: exact_lookup
    expected_results:
      - path: internal/search/types.go
        symbol: SearchOptions
        grade: 3
        rationale: owner
    holdout: false
    source: test
`)
	outDir := t.TempDir()
	runner := NewRunner(fakeSearcher{results: []SearchResult{
		{Path: "internal/search/types.go", Symbol: "SearchOptions", ResultID: "sr1_exact"},
	}})

	report, err := runner.Run(context.Background(), Options{
		CorpusPath:       corpusPath,
		Subset:           "full",
		Output:           "json",
		OutDir:           outDir,
		FailOnRegression: true,
	})

	require.Error(t, err)
	require.NotNil(t, report)
	assert.True(t, report.BaselineComparison.Regressed)
	assert.True(t, report.ExactLookupGate.Required)
	assert.False(t, report.ExactLookupGate.Compared)
	assert.Contains(t, report.BaselineComparison.RegressionReasons, "exact lookup baseline missing")
}

func TestRunner_ExactLookupGateDetectsRankPathAndResultIDRegression(t *testing.T) {
	corpusPath := writeTempCorpus(t, `
queries:
  - id: EXACT-Q1
    name: exact owner
    query: "SearchOptions"
    tool: search_code
    class: exact_identifier
    job: exact_lookup
    expected_results:
      - path: internal/search/types.go
        symbol: SearchOptions
        grade: 3
        rationale: owner
    holdout: false
    source: test
  - id: PATH-Q1
    name: path owner
    query: "internal/mcp/server.go"
    tool: search
    class: path_lookup
    job: exact_lookup
    expected_results:
      - path: internal/mcp/server.go
        grade: 3
        rationale: direct path
    holdout: false
    source: test
  - id: QUOTE-Q1
    name: quoted owner
    query: "\"query parameter is required\""
    tool: search_code
    class: quoted_string
    job: exact_lookup
    expected_results:
      - path: internal/validation/validation.go
        grade: 3
        rationale: owns the literal
    holdout: false
    source: test
`)
	outDir := t.TempDir()
	baselinePath := filepath.Join(outDir, "baseline.json")
	baseline := Report{
		Queries: []QueryResult{
			exactQueryResult("EXACT-Q1", "exact_identifier", "internal/search/types.go", "SearchOptions", "sr1_exact", 1),
			exactQueryResult("PATH-Q1", "path_lookup", "internal/mcp/server.go", "", "sr1_path", 1),
			exactQueryResult("QUOTE-Q1", "quoted_string", "internal/validation/validation.go", "", "sr1_quote", 1),
		},
	}
	baseline.Summary = summarize(baseline.Queries)
	baseline.Metrics = calculateMetrics(baseline.Queries)
	baselineData, err := json.Marshal(baseline)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(baselinePath, baselineData, 0o644))

	runner := NewRunner(sequenceSearcher{responses: map[string]SearchResponse{
		"EXACT-Q1": {Results: []SearchResult{
			{Path: "internal/other.go", ResultID: "sr1_other"},
			{Path: "internal/search/types.go", Symbol: "SearchOptions", ResultID: "sr1_exact_changed"},
		}},
		"PATH-Q1": {Results: []SearchResult{
			{Path: "internal/mcp/tools.go", ResultID: "sr1_wrong_path"},
		}},
		"QUOTE-Q1": {Results: []SearchResult{
			{Path: "internal/validation/validation.go", ResultID: "sr1_quote_changed"},
		}},
	}})

	report, err := runner.Run(context.Background(), Options{
		CorpusPath:       corpusPath,
		Subset:           "full",
		Output:           "json",
		OutDir:           outDir,
		BaselinePath:     baselinePath,
		FailOnRegression: true,
	})

	require.Error(t, err)
	require.NotNil(t, report)
	assert.True(t, report.ExactLookupGate.Compared)
	assert.False(t, report.ExactLookupGate.Passed)
	assert.Len(t, report.ExactLookupGate.Failures, 4)
	assert.Contains(t, report.BaselineComparison.RegressionReasons, "exact lookup gate failed")
	assert.Equal(t, 1.0, report.ExactLookupGate.Classes["quoted_string"].PassRate)
}

func TestRunner_GeneratesJSONAndMarkdownReports(t *testing.T) {
	corpusPath := writeTempCorpus(t, `
queries:
  - id: Q1
    name: search owner
    query: "search engine"
    tool: search
    class: exact_identifier
    job: code
    expected_results:
      - path: internal/search/engine.go
        grade: 3
        rationale: owner
    holdout: false
    source: manual
`)
	outDir := t.TempDir()
	runner := NewRunner(fakeSearcher{results: []SearchResult{{Path: "internal/search/engine.go", Text: "search engine owner"}}})

	report, err := runner.Run(context.Background(), Options{
		CorpusPath: corpusPath,
		Subset:     "full",
		Output:     "both",
		OutDir:     outDir,
	})

	require.NoError(t, err)
	assert.Equal(t, 1, report.Summary.QueryCount)
	assert.Equal(t, 1, report.Summary.PassCount)
	assert.FileExists(t, filepath.Join(outDir, "latest.json"))
	assert.FileExists(t, filepath.Join(outDir, "latest.md"))

	data, err := os.ReadFile(filepath.Join(outDir, "latest.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), `"by_class"`)
	assert.Contains(t, string(data), `"baseline_comparison"`)

	md, err := os.ReadFile(filepath.Join(outDir, "latest.md"))
	require.NoError(t, err)
	assert.Contains(t, string(md), "# Search Eval Report")
	assert.Contains(t, string(md), "Q1")
}

func TestRunner_PrepareFailureFailsBeforeReports(t *testing.T) {
	corpusPath := writeTempCorpus(t, `
queries:
  - id: Q1
    name: search owner
    query: "search engine"
    tool: search
    class: exact_identifier
    job: code
    expected_results:
      - path: internal/search/engine.go
        grade: 3
        rationale: owner
    holdout: false
    source: manual
`)
	outDir := t.TempDir()
	searcher := &prepareFailingSearcher{err: assert.AnError}
	runner := NewRunner(searcher)

	report, err := runner.Run(context.Background(), Options{
		CorpusPath: corpusPath,
		Subset:     "full",
		Output:     "both",
		OutDir:     outDir,
	})

	require.ErrorIs(t, err, assert.AnError)
	assert.Nil(t, report)
	assert.Equal(t, 0, searcher.searchCalls)
	assert.NoFileExists(t, filepath.Join(outDir, "latest.json"))
	assert.NoFileExists(t, filepath.Join(outDir, "latest.md"))
}

func TestRunner_BaselineComparisonDetectsRegression(t *testing.T) {
	corpusPath := writeTempCorpus(t, `
queries:
  - id: Q1
    name: search owner
    query: "search engine"
    tool: search
    class: exact_identifier
    job: code
    expected_results:
      - path: internal/search/engine.go
        grade: 3
        rationale: owner
    holdout: false
    source: manual
`)
	outDir := t.TempDir()
	baselinePath := filepath.Join(outDir, "baseline.json")
	baseline := Report{
		Summary: Summary{QueryCount: 1, PassCount: 1, PassRate: 1},
		Metrics: Metrics{RecallAt5: 1, RecallAt10: 1, MRRAt10: 1, NDCGAt10: 1},
	}
	baselineData, err := json.Marshal(baseline)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(baselinePath, baselineData, 0o644))

	runner := NewRunner(fakeSearcher{results: []SearchResult{{Path: "internal/other.go"}}})
	report, err := runner.Run(context.Background(), Options{
		CorpusPath:       corpusPath,
		Subset:           "full",
		Output:           "json",
		OutDir:           outDir,
		BaselinePath:     baselinePath,
		FailOnRegression: true,
	})

	require.Error(t, err)
	require.NotNil(t, report)
	assert.True(t, report.BaselineComparison.Regressed)
	assert.Contains(t, err.Error(), "regression")
}

func TestRunner_GraphEvalGatePassesAndReportsFields(t *testing.T) {
	corpusPath := writeTempCorpus(t, `
queries:
  - id: CALL-Q1
    name: caller callee owner
    query: "who calls search options"
    tool: search_code
    class: caller_callee
    job: code
    expected_results:
      - path: internal/search/options.go
        grade: 3
        rationale: graph owner
    holdout: false
    source: test
  - id: IMPACT-Q1
    name: impact owner
    query: "impact of changing search options"
    tool: search_code
    class: impact_analysis
    job: code
    expected_results:
      - path: internal/search/options.go
        grade: 3
        rationale: graph owner
    holdout: false
    source: test
  - id: TEST-Q1
    name: test owner
    query: "tests for search options"
    tool: search_code
    class: test_to_implementation
    job: code
    expected_results:
      - path: internal/search/options_test.go
        grade: 3
        rationale: graph owner
    holdout: false
    source: test
  - id: ADR-Q1
    name: adr owner
    query: "decision for search options"
    tool: search_docs
    class: adr_to_code
    job: decision_lookup
    expected_results:
      - path: internal/search/options.go
        grade: 3
        rationale: graph owner
    holdout: false
    source: test
  - id: CROSS-Q1
    name: subsystem owner
    query: "cross file search subsystem"
    tool: search_code
    class: cross_file_subsystem
    job: code
    expected_results:
      - path: internal/search/engine.go
        grade: 3
        rationale: graph owner
    holdout: false
    source: test
  - id: EXACT-Q1
    name: exact owner
    query: "SearchOptions"
    tool: search_code
    class: exact_identifier
    job: exact_lookup
    expected_results:
      - path: internal/search/types.go
        symbol: SearchOptions
        grade: 3
        rationale: exact owner
    holdout: false
    source: test
`)
	outDir := t.TempDir()
	baselinePath := filepath.Join(outDir, "baseline.json")
	baseline := Report{
		Queries: []QueryResult{
			exactQueryResult("EXACT-Q1", "exact_identifier", "internal/search/types.go", "SearchOptions", "sr-exact", 1),
		},
	}
	baseline.Summary = summarize(baseline.Queries)
	baseline.Metrics = calculateMetrics(baseline.Queries)
	baselineData, err := json.Marshal(baseline)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(baselinePath, baselineData, 0o644))

	runner := NewRunner(sequenceSearcher{responses: map[string]SearchResponse{
		"CALL-Q1":   {Results: []SearchResult{{Path: "internal/search/options.go"}}},
		"IMPACT-Q1": {Results: []SearchResult{{Path: "internal/search/options.go"}}},
		"TEST-Q1":   {Results: []SearchResult{{Path: "internal/search/options_test.go"}}},
		"ADR-Q1":    {Results: []SearchResult{{Path: "internal/search/options.go"}}},
		"CROSS-Q1":  {Results: []SearchResult{{Path: "internal/search/engine.go"}}},
		"EXACT-Q1":  {Results: []SearchResult{{Path: "internal/search/types.go", Symbol: "SearchOptions", ResultID: "sr-exact"}}},
	}})

	report, err := runner.Run(context.Background(), Options{
		CorpusPath:   corpusPath,
		Subset:       "graph",
		Output:       "both",
		OutDir:       outDir,
		BaselinePath: baselinePath,
	})

	require.NoError(t, err)
	require.NotNil(t, report)
	assert.True(t, report.GraphEvalGate.Compared)
	assert.True(t, report.GraphEvalGate.Passed)
	assert.Equal(t, GraphRecommendationKeep, report.GraphEvalGate.Recommendation)
	assert.Equal(t, 5, report.GraphEvalGate.CurrentQueryCount)
	assert.Equal(t, 5, report.GraphEvalGate.TokenMetrics.Count)
	assert.Equal(t, 0.25, report.GraphEvalGate.Classes["caller_callee"].BaselineRecallAt10Floor)
	assert.Equal(t, 0.0, report.GraphEvalGate.Classes["impact_analysis"].BaselineRecallAt10Floor)
	assert.Equal(t, 0.20, report.GraphEvalGate.Classes["impact_analysis"].LowBaselineAbsoluteFloor)
	assert.Equal(t, GraphRecommendationKeep, report.GraphEvalGate.Classes["cross_file_subsystem"].Recommendation)
	assert.Equal(t, []string{"caller_callee", "impact_analysis", "test_to_implementation", "adr_to_code", "cross_file_subsystem"}, report.ClassGroups.GraphHeavy)
	assert.Equal(t, []string{"exact_identifier", "path_lookup", "quoted_string"}, report.ClassGroups.ExactLookup)
	assert.Contains(t, report.ClassGroups.Ordinary, "natural_language_intent")
	assert.True(t, report.ExactLookupGate.Compared)
	assert.True(t, report.ExactLookupGate.Passed)

	data, err := os.ReadFile(filepath.Join(outDir, "latest.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), `"graph_eval_gate"`)
	assert.Contains(t, string(data), `"baseline_recall_at_10_floor"`)
	assert.Contains(t, string(data), `"recommendation": "keep"`)

	md, err := os.ReadFile(filepath.Join(outDir, "latest.md"))
	require.NoError(t, err)
	assert.Contains(t, string(md), "## Graph Eval Gate")
	assert.Contains(t, string(md), "Recommendation: keep")
	assert.Contains(t, string(md), "caller_callee")
}

func TestBuildReport_GraphEvalGateKillsBelowTenPointLift(t *testing.T) {
	report := buildReport(Options{Subset: "graph"}, []QueryResult{
		graphQueryResult("CROSS-Q1", "cross_file_subsystem", false),
	})

	assert.False(t, report.GraphEvalGate.Passed)
	assert.Equal(t, GraphRecommendationKill, report.GraphEvalGate.Recommendation)
	assert.Equal(t, "default_graph_augmented_search", report.GraphEvalGate.RecommendationTarget)
	assert.Equal(t, "search_engine_graph_heavy_classes", report.GraphEvalGate.EvaluationScope)
	assert.Equal(t, "search_engine", report.GraphEvalGate.MeasuredTool)
	assert.False(t, report.GraphEvalGate.GraphToolMeasured)
	assert.Contains(t, report.GraphEvalGate.Reasons, "graph.query tool quality is not measured by this gate")
	assert.Equal(t, GraphRecommendationKill, report.GraphEvalGate.Classes["cross_file_subsystem"].Recommendation)
	require.NotEmpty(t, report.GraphEvalGate.Failures)
	assert.Contains(t, report.GraphEvalGate.Failures[0].Reason, "below 10pp")
}

func TestBuildReport_GraphEvalGateFailsLowBaselineAbsoluteFloor(t *testing.T) {
	results := []QueryResult{
		graphQueryResult("IMPACT-Q1", "impact_analysis", true),
		graphQueryResult("IMPACT-Q2", "impact_analysis", false),
		graphQueryResult("IMPACT-Q3", "impact_analysis", false),
		graphQueryResult("IMPACT-Q4", "impact_analysis", false),
		graphQueryResult("IMPACT-Q5", "impact_analysis", false),
		graphQueryResult("IMPACT-Q6", "impact_analysis", false),
	}

	report := buildReport(Options{Subset: "graph"}, results)

	assert.False(t, report.GraphEvalGate.Passed)
	assert.Equal(t, GraphRecommendationDefer, report.GraphEvalGate.Recommendation)
	classGate := report.GraphEvalGate.Classes["impact_analysis"]
	assert.InDelta(t, 1.0/6.0, classGate.CurrentRecallAt10, 0.0001)
	assert.InDelta(t, 1.0/6.0, classGate.RecallAt10Delta, 0.0001)
	assert.Equal(t, 0.20, classGate.LowBaselineAbsoluteFloor)
	assert.Contains(t, classGate.Reasons, "current recall@10 below low-baseline absolute floor")
}

func TestRunner_GraphEvalGateKillsOnExactLookupRegression(t *testing.T) {
	corpusPath := writeTempCorpus(t, `
queries:
  - id: CALL-Q1
    name: caller callee owner
    query: "who calls search options"
    tool: search_code
    class: caller_callee
    job: code
    expected_results:
      - path: internal/search/options.go
        grade: 3
        rationale: graph owner
    holdout: false
    source: test
  - id: EXACT-Q1
    name: exact owner
    query: "SearchOptions"
    tool: search_code
    class: exact_identifier
    job: exact_lookup
    expected_results:
      - path: internal/search/types.go
        symbol: SearchOptions
        grade: 3
        rationale: exact owner
    holdout: false
    source: test
`)
	outDir := t.TempDir()
	baselinePath := filepath.Join(outDir, "baseline.json")
	baseline := Report{
		Queries: []QueryResult{
			exactQueryResult("EXACT-Q1", "exact_identifier", "internal/search/types.go", "SearchOptions", "sr-exact", 1),
		},
	}
	baseline.Summary = summarize(baseline.Queries)
	baseline.Metrics = calculateMetrics(baseline.Queries)
	baselineData, err := json.Marshal(baseline)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(baselinePath, baselineData, 0o644))

	runner := NewRunner(sequenceSearcher{responses: map[string]SearchResponse{
		"CALL-Q1":  {Results: []SearchResult{{Path: "internal/search/options.go"}}},
		"EXACT-Q1": {Results: []SearchResult{{Path: "internal/search/types.go", Symbol: "SearchOptions", ResultID: "sr-changed"}}},
	}})

	report, err := runner.Run(context.Background(), Options{
		CorpusPath:       corpusPath,
		Subset:           "graph",
		Output:           "json",
		OutDir:           outDir,
		BaselinePath:     baselinePath,
		FailOnRegression: true,
	})

	require.Error(t, err)
	require.NotNil(t, report)
	assert.False(t, report.ExactLookupGate.Passed)
	assert.False(t, report.GraphEvalGate.Passed)
	assert.Equal(t, GraphRecommendationKill, report.GraphEvalGate.Recommendation)
	assert.Contains(t, report.GraphEvalGate.Reasons, "exact lookup gate failed")
	assert.Contains(t, report.BaselineComparison.RegressionReasons, "exact lookup gate failed")
	assert.Contains(t, report.BaselineComparison.RegressionReasons, "graph eval gate failed")
}

func TestRunner_TokenBaselineComparisonDetectsRegression(t *testing.T) {
	corpusPath := writeTempCorpus(t, `
queries:
  - id: Q1
    name: search owner
    query: "search engine"
    tool: search
    class: exact_identifier
    job: code
    expected_results:
      - path: internal/search/engine.go
        grade: 3
        rationale: owner
    holdout: false
    source: manual
`)
	outDir := t.TempDir()
	tokenBaselinePath := filepath.Join(outDir, "tokens-baseline.json")
	require.NoError(t, os.WriteFile(tokenBaselinePath, []byte(`{
  "schema_version": 1,
  "tools": {
    "search": {
      "count": 1,
      "mean_tokens_per_result": 10,
      "p95_tokens_per_result": 10
    }
  },
  "query_classes": {
    "exact_identifier": {
      "count": 1,
      "mean_tokens_per_result": 10,
      "p95_tokens_per_result": 10
    }
  },
  "queries": [
    {
      "id": "Q1",
      "tool": "search",
      "class": "exact_identifier",
      "result_count": 1,
      "tokens_per_result": 10
    }
  ]
}`), 0o644))

	runner := NewRunner(fakeSearcher{response: SearchResponse{
		Results:       []SearchResult{{Path: "internal/search/engine.go"}},
		ResponseBytes: 400,
	}})
	report, err := runner.Run(context.Background(), Options{
		CorpusPath:         corpusPath,
		Subset:             "full",
		Output:             "json",
		OutDir:             outDir,
		TokenBaselinePath:  tokenBaselinePath,
		FailOnRegression:   true,
		TokenBudgetEnabled: true,
	})

	require.Error(t, err)
	require.NotNil(t, report)
	assert.True(t, report.BaselineComparison.Regressed)
	assert.Contains(t, err.Error(), "token budget")
}

func TestRunner_WritesReportWhenBaselineIsMalformed(t *testing.T) {
	corpusPath := writeTempCorpus(t, `
queries:
  - id: Q1
    name: search owner
    query: "search engine"
    tool: search
    class: exact_identifier
    job: code
    expected_results:
      - path: internal/search/engine.go
        grade: 3
        rationale: owner
    holdout: false
    source: manual
`)
	outDir := t.TempDir()
	baselinePath := filepath.Join(outDir, "baseline.json")
	require.NoError(t, os.WriteFile(baselinePath, []byte(`{"summary":`), 0o644))

	runner := NewRunner(fakeSearcher{response: SearchResponse{
		Results: []SearchResult{{Path: "internal/search/engine.go"}},
	}})
	report, err := runner.Run(context.Background(), Options{
		CorpusPath:   corpusPath,
		Subset:       "full",
		Output:       "both",
		OutDir:       outDir,
		BaselinePath: baselinePath,
	})

	require.Error(t, err)
	require.NotNil(t, report)
	assert.FileExists(t, filepath.Join(outDir, "latest.json"))
	assert.FileExists(t, filepath.Join(outDir, "latest.md"))
	assert.Contains(t, report.BaselineComparison.RegressionReasons, "baseline parse failed")
}

func TestRunner_WritesReportWhenTokenBaselineIsMalformed(t *testing.T) {
	corpusPath := writeTempCorpus(t, `
queries:
  - id: Q1
    name: search owner
    query: "search engine"
    tool: search
    class: exact_identifier
    job: code
    expected_results:
      - path: internal/search/engine.go
        grade: 3
        rationale: owner
    holdout: false
    source: manual
`)
	outDir := t.TempDir()
	tokenBaselinePath := filepath.Join(outDir, "tokens-baseline.json")
	require.NoError(t, os.WriteFile(tokenBaselinePath, []byte(`{"queries":`), 0o644))

	runner := NewRunner(fakeSearcher{response: SearchResponse{
		Results: []SearchResult{{Path: "internal/search/engine.go"}},
	}})
	report, err := runner.Run(context.Background(), Options{
		CorpusPath:        corpusPath,
		Subset:            "full",
		Output:            "both",
		OutDir:            outDir,
		TokenBaselinePath: tokenBaselinePath,
	})

	require.Error(t, err)
	require.NotNil(t, report)
	assert.FileExists(t, filepath.Join(outDir, "latest.json"))
	assert.FileExists(t, filepath.Join(outDir, "latest.md"))
	assert.Contains(t, report.BaselineComparison.RegressionReasons, "token baseline parse failed")
}

func TestRunner_RefusesToOverwriteExistingBaselineWithoutForce(t *testing.T) {
	corpusPath := writeTempCorpus(t, `
queries:
  - id: Q1
    name: search owner
    query: "search engine"
    tool: search
    class: exact_identifier
    job: code
    expected_results:
      - path: internal/search/engine.go
        grade: 3
        rationale: owner
    holdout: false
    source: manual
`)
	outDir := t.TempDir()
	baselinePath := filepath.Join(outDir, "baseline.json")
	require.NoError(t, os.WriteFile(baselinePath, []byte(`existing`), 0o644))

	runner := NewRunner(fakeSearcher{response: SearchResponse{
		Results: []SearchResult{{Path: "internal/search/engine.go"}},
	}})
	report, err := runner.Run(context.Background(), Options{
		CorpusPath:   corpusPath,
		Subset:       "full",
		Output:       "json",
		OutDir:       outDir,
		SaveBaseline: true,
	})

	require.Error(t, err)
	require.NotNil(t, report)
	assert.Contains(t, err.Error(), "refusing to overwrite existing baseline")
	assert.Equal(t, "existing", mustReadFile(t, baselinePath))
}

func TestEstimateTokens_UsesCompactJSONByteEstimator(t *testing.T) {
	results := []SearchResult{{
		Path:   "internal/search/engine.go",
		Symbol: "Engine.Search",
		Text:   "search results",
	}}
	resultBytes, err := json.Marshal(results)
	require.NoError(t, err)

	got := estimateTokens(Query{Query: "abcd1234"}, SearchResponse{Results: results})

	assert.Equal(t, "utf8-json-bytes-v1", got.Method)
	assert.Equal(t, 2, got.QueryTokens)
	assert.Equal(t, len(resultBytes), got.ResponseBytes)
	assert.Equal(t, int(math.Ceil(float64(len(resultBytes))/4.0)), got.ResultTokens)
	assert.Equal(t, got.QueryTokens+got.ResultTokens, got.TotalTokens)
}

func TestScoreQuery_NegativeAdversarialGradeZeroTrap(t *testing.T) {
	query := Query{
		Class: "negative_adversarial",
		ExpectedResults: []ExpectedResult{{
			Path:  "archive/",
			Grade: 0,
		}},
	}

	passed, rank, grade := scoreQuery(query, []SearchResult{{Path: "internal/search/engine.go"}})
	assert.True(t, passed)
	assert.Equal(t, -1, rank)
	assert.Equal(t, 0, grade)

	passed, rank, grade = scoreQuery(query, []SearchResult{{Path: "archive/old.go"}})
	assert.False(t, passed)
	assert.Equal(t, 1, rank)
	assert.Equal(t, 0, grade)
}

func TestMatchesPath_ExactOrDirectoryPrefixOnly(t *testing.T) {
	assert.True(t, matchesPath("internal/store/hnsw.go", "internal/store/hnsw.go"))
	assert.True(t, matchesPath("internal/store/hnsw.go", "internal/store"))
	assert.True(t, matchesPath("internal/store/hnsw.go", "internal/store/"))
	assert.False(t, matchesPath("internal/storeother/hnsw.go", "internal/store"))
	assert.False(t, matchesPath("foo/internal/store/hnsw.go", "internal/store"))
}

func TestMatchedGrade_RequiresExpectedSymbolWhenSpecified(t *testing.T) {
	expected := []ExpectedResult{{
		Path:   "internal/store/hnsw.go",
		Symbol: "HNSWStore",
		Grade:  3,
	}}

	assert.Equal(t, 0, matchedGrade(expected, SearchResult{Path: "internal/store/hnsw.go"}))
	assert.Equal(t, 0, matchedGrade(expected, SearchResult{Path: "internal/store/hnsw.go", Symbol: "Other"}))
	assert.Equal(t, 3, matchedGrade(expected, SearchResult{Path: "internal/store/hnsw.go", Symbol: "HNSWStore"}))
}

func TestCalculateMetrics_UsesTrueNDCGAt10(t *testing.T) {
	results := []QueryResult{{
		ExpectedResults: []ExpectedResult{
			{Path: "b.go", Grade: 3},
			{Path: "a.go", Grade: 2},
		},
		TopResults: []SearchResult{
			{Path: "a.go"},
			{Path: "b.go"},
		},
		FirstUsefulRank: 1,
		MatchedGrade:    2,
	}}

	got := calculateMetrics(results)
	want := (dcg(2, 1) + dcg(3, 2)) / (dcg(3, 1) + dcg(2, 2))
	assert.InDelta(t, want, got.NDCGAt10, 0.0001)
}

func TestCalculateMetrics_DoesNotDoubleCountDuplicateResultsForNDCG(t *testing.T) {
	results := []QueryResult{{
		ExpectedResults: []ExpectedResult{
			{Path: "a.go", Grade: 3},
		},
		TopResults: []SearchResult{
			{Path: "a.go"},
			{Path: "a.go"},
		},
		FirstUsefulRank: 1,
		MatchedGrade:    3,
	}}

	got := calculateMetrics(results)
	assert.Equal(t, 1.0, got.NDCGAt10)
}

func TestSummarize_ExcludesZeroResultRowsFromTokensPerResultMean(t *testing.T) {
	results := []QueryResult{
		{
			TopResults:    []SearchResult{{Path: "a.go"}},
			TokenEstimate: TokenEstimate{TokensPerResult: 10, ResultTokens: 10},
		},
		{
			TopResults:    nil,
			TokenEstimate: TokenEstimate{TokensPerResult: 0, ResultTokens: 4},
		},
	}

	got := summarize(results)
	assert.Equal(t, 10.0, got.TokensPerResultMean)
	assert.Equal(t, 10.0, got.TokensPerResultP95)
	assert.Equal(t, 4.0, got.ZeroResultResponseTokensMean)
}

type fakeSearcher struct {
	results  []SearchResult
	response SearchResponse
	err      error
}

func (f fakeSearcher) Search(context.Context, Query) (SearchResponse, error) {
	if f.response.Results != nil || f.response.ResponseBytes > 0 {
		return f.response, f.err
	}
	return SearchResponse{Results: f.results}, f.err
}

type sequenceSearcher struct {
	responses map[string]SearchResponse
	err       error
}

func (s sequenceSearcher) Search(_ context.Context, query Query) (SearchResponse, error) {
	if response, ok := s.responses[query.ID]; ok {
		return response, s.err
	}
	return SearchResponse{}, s.err
}

type prepareFailingSearcher struct {
	err         error
	searchCalls int
}

func (f *prepareFailingSearcher) Prepare(context.Context) error {
	return f.err
}

func (f *prepareFailingSearcher) Search(context.Context, Query) (SearchResponse, error) {
	f.searchCalls++
	return SearchResponse{}, nil
}

func writeTempCorpus(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "queries.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func query(id, class, job string, holdout bool) Query {
	return Query{
		ID:      id,
		Name:    id,
		Query:   id,
		Tool:    "search",
		Class:   class,
		Job:     job,
		Holdout: holdout,
		Source:  "test",
		ExpectedResults: []ExpectedResult{{
			Path:      "internal/search/engine.go",
			Grade:     3,
			Rationale: "owner",
		}},
	}
}

func queryIDs(queries []Query) []string {
	ids := make([]string, 0, len(queries))
	for _, q := range queries {
		ids = append(ids, q.ID)
	}
	return ids
}

func exactQueryResult(id, class, path, symbol, resultID string, rank int) QueryResult {
	results := make([]SearchResult, rank)
	results[rank-1] = SearchResult{Path: path, Symbol: symbol, ResultID: resultID}
	query := Query{
		ID:    id,
		Class: class,
		Job:   "exact_lookup",
		ExpectedResults: []ExpectedResult{{
			Path:   path,
			Symbol: symbol,
			Grade:  3,
		}},
	}
	passed, firstRank, grade := scoreQuery(query, results)
	return QueryResult{
		ID:              id,
		Class:           class,
		Job:             "exact_lookup",
		ExpectedResults: query.ExpectedResults,
		TopResults:      results,
		Passed:          passed,
		FirstUsefulRank: firstRank,
		MatchedGrade:    grade,
	}
}

func graphQueryResult(id, class string, passed bool) QueryResult {
	expectedPath := "internal/search/options.go"
	results := []SearchResult{{Path: "internal/other.go"}}
	firstRank := -1
	matchedGrade := 0
	if passed {
		results = []SearchResult{{Path: expectedPath}}
		firstRank = 1
		matchedGrade = 3
	}
	return QueryResult{
		ID:    id,
		Class: class,
		Job:   "code",
		ExpectedResults: []ExpectedResult{{
			Path:  expectedPath,
			Grade: 3,
		}},
		TopResults:      results,
		Passed:          passed,
		FirstUsefulRank: firstRank,
		MatchedGrade:    matchedGrade,
		TokenEstimate:   TokenEstimate{TokensPerResult: 10, ResultTokens: 10},
	}
}
