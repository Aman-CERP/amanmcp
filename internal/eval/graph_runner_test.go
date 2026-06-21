package eval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectGraphRunner_WritesMCPShapedReports(t *testing.T) {
	corpusPath := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-FR-Q01
    name: references query service
    mode: find_references
    query: internal/graph/query.go
    subsets: [quick, full, mode:find_references]
    holdout: false
    source: test
    expected:
      - source_path: internal/graph/query.go
        role: related
        relation: file_defines_symbol
        confidence_label: high
        graph_path_contains: [file_defines_symbol]
        rationale: query service symbol should be referenced by graph evidence
  - id: GRA-ES-Q01
    name: explain query service
    mode: explain_symbol
    query: QueryService
    subsets: [quick, full, mode:explain_symbol]
    holdout: false
    source: test
    expected:
      - node_kind: symbol
        role: symbol_context
        rationale: explain_symbol should return symbol context
  - id: GRA-IA-Q01
    name: impact query service
    mode: impact_analysis
    query: QueryService
    subsets: [quick, full, mode:impact_analysis]
    holdout: false
    source: test
    expected:
      - node_kind: file
        role: downstream
        rationale: impact_analysis should return downstream file context
`)
	outDir := t.TempDir()
	client := &fakeDirectGraphClient{
		snapshot: freshGraphSnapshot(),
		responses: map[string]DirectGraphToolOutput{
			"GRA-FR-Q01": graphOutput(graph.QueryModeFindReferences, "internal/graph/query.go", graph.GraphStatusFresh, graph.QueryResult{
				NodeID:          "symbol:QueryService",
				NodeKind:        graph.NodeKindSymbol,
				SourcePath:      "internal/graph/query.go",
				Role:            "related",
				Relation:        graph.EdgeKindFileDefinesSymbol,
				ConfidenceLabel: graph.ConfidenceHigh,
				EvidenceMethod:  "tree_sitter",
				GraphPath:       []string{"file:internal/graph/query.go", string(graph.EdgeKindFileDefinesSymbol), "symbol:QueryService"},
			}),
			"GRA-ES-Q01": graphOutput(graph.QueryModeExplainSymbol, "QueryService", graph.GraphStatusFresh, graph.QueryResult{
				NodeID:     "symbol:QueryService",
				NodeKind:   graph.NodeKindSymbol,
				Role:       "symbol_context",
				GraphPath:  []string{"symbol:QueryService"},
				SourcePath: "internal/graph/query.go",
			}),
			"GRA-IA-Q01": graphOutput(graph.QueryModeImpactAnalysis, "QueryService", graph.GraphStatusFresh, graph.QueryResult{
				NodeID:    "file:internal/eval/graph_runner.go",
				NodeKind:  graph.NodeKindFile,
				Role:      "downstream",
				GraphPath: []string{"symbol:QueryService", "file:internal/eval/graph_runner.go"},
			}),
		},
	}

	report, err := NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath: corpusPath,
		Subset:     GraphSubsetQuick,
		Output:     "both",
		OutDir:     outDir,
		Command:    "amanmcp eval graph --subset quick --output both",
	})

	require.NoError(t, err)
	require.NotNil(t, report)
	assert.Equal(t, DirectGraphReportType, report.ReportType)
	assert.Equal(t, DirectGraphMeasuredTool, report.MeasuredTool)
	assert.Equal(t, DirectGraphEvaluationScope, report.EvaluationScope)
	assert.True(t, report.GraphToolMeasured)
	assert.Empty(t, report.UnmeasuredReason)
	assert.Equal(t, 3, report.Summary.QueryCount)
	assert.Equal(t, 3, report.Summary.MeasuredQueryCount)
	assert.Equal(t, 3, report.Summary.PassCount)
	assert.Equal(t, 0, report.Summary.FailCount)
	assert.Equal(t, 1.0, report.Summary.PassRate)
	assert.Equal(t, map[string]int{
		graph.QueryModeFindReferences: 1,
		graph.QueryModeExplainSymbol:  1,
		graph.QueryModeImpactAnalysis: 1,
	}, report.ModeCounts)
	require.Len(t, report.Queries, 3)
	assert.True(t, report.Queries[0].Available)
	assert.Equal(t, graph.GraphStatusFresh, report.Queries[0].Status)
	require.Len(t, report.Queries[0].Results, 1)
	assert.Equal(t, "symbol:QueryService", report.Queries[0].Results[0].NodeID)
	// Rank-aware per-case metrics (GRA12/13/14): the single result matches the
	// single expected row at rank 0, so the case is a perfect top-window hit.
	assert.Equal(t, 1, report.Queries[0].WindowSize)
	assert.Equal(t, 1, report.Queries[0].MatchedPositions)
	assert.Equal(t, 1.0, report.Queries[0].ExpectedRecallAt10)
	assert.Equal(t, 1.0, report.Queries[0].PrecisionAt10)
	assert.True(t, report.Queries[0].HitAt3)
	assert.True(t, report.Queries[0].HitAt10)
	require.Len(t, report.StatusSnapshots, 1)
	assert.Equal(t, 1, report.StatusSnapshots[0].ExtractorStatusCounts[graph.ExtractorStatusSuccess])
	assert.NotEmpty(t, report.StatusSnapshots[0].Confidence)
	require.Len(t, client.calls, 3)
	assert.Equal(t, []GraphQuery{
		reportQueryByID(t, corpusPath, "GRA-FR-Q01"),
		reportQueryByID(t, corpusPath, "GRA-ES-Q01"),
		reportQueryByID(t, corpusPath, "GRA-IA-Q01"),
	}, client.calls)
	assert.FileExists(t, filepath.Join(outDir, "latest.json"))
	assert.FileExists(t, filepath.Join(outDir, "latest.md"))

	data, err := os.ReadFile(filepath.Join(outDir, "latest.md"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "# Direct Graph Eval Report")
	assert.Contains(t, string(data), "graph.query")
	assert.Contains(t, string(data), "direct_graph_query_modes")
	// Per-mode relevance metrics and the threshold verdict are surfaced (GRA12/13/14).
	assert.Contains(t, string(data), "recall@10")
	assert.Contains(t, string(data), "precision@10")
	assert.Contains(t, string(data), "hit@3")
	assert.Contains(t, string(data), "hit@10")
	assert.Contains(t, string(data), "Threshold met")
}

func TestDirectGraphRunner_RecordsDegradationAndFailureModes(t *testing.T) {
	corpusPath := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-FRESH
    name: fresh graph
    mode: find_references
    query: fresh.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: fresh.go
        rationale: fresh evidence
  - id: GRA-EMPTY
    name: empty graph
    mode: find_references
    query: empty.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: empty.go
        rationale: empty graph should block
  - id: GRA-UNAVAILABLE
    name: unavailable graph
    mode: find_references
    query: unavailable.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: unavailable.go
        rationale: unavailable graph should block
  - id: GRA-PARTIAL
    name: partial graph
    mode: explain_symbol
    query: Partial
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - node_kind: symbol
        rationale: partial graph still returns evidence
  - id: GRA-STALE
    name: stale graph
    mode: explain_symbol
    query: Stale
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - node_kind: symbol
        rationale: stale graph still returns evidence
  - id: GRA-TRUNCATED
    name: truncated graph
    mode: impact_analysis
    query: Truncated
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - node_kind: file
        rationale: truncated graph still returns evidence
  - id: GRA-INVALID
    name: invalid params
    mode: find_references
    query: invalid.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: invalid.go
        rationale: invalid params should be reported
`)
	client := &fakeDirectGraphClient{
		snapshot: freshGraphSnapshot(),
		responses: map[string]DirectGraphToolOutput{
			"GRA-FRESH": graphOutput(graph.QueryModeFindReferences, "fresh.go", graph.GraphStatusFresh, graph.QueryResult{
				SourcePath: "fresh.go",
			}),
			"GRA-EMPTY":       {Available: false, Status: graph.GraphStatusEmpty, Degraded: true, Mode: graph.QueryModeFindReferences, Query: "empty.go"},
			"GRA-UNAVAILABLE": {Available: false, Status: graph.GraphStatusUnavailable, Degraded: true, Mode: graph.QueryModeFindReferences, Query: "unavailable.go"},
			"GRA-PARTIAL": graphOutput(graph.QueryModeExplainSymbol, "Partial", graph.GraphStatusPartial, graph.QueryResult{
				NodeKind: graph.NodeKindSymbol,
			}),
			"GRA-STALE": graphOutput(graph.QueryModeExplainSymbol, "Stale", graph.GraphStatusStale, graph.QueryResult{
				NodeKind: graph.NodeKindSymbol,
			}),
			"GRA-TRUNCATED": {
				Available: true,
				Status:    graph.GraphStatusFresh,
				Mode:      graph.QueryModeImpactAnalysis,
				Query:     "Truncated",
				Results: []graph.QueryResult{{
					NodeKind: graph.NodeKindFile,
				}},
				Warnings: []graph.StatusWarning{{
					Code:    graph.WarningCode("graph_results_truncated"),
					Message: "graph query reached the configured limit",
				}},
			},
		},
		errs: map[string]error{
			"GRA-INVALID": errors.New("invalid params: query contains unsafe path"),
		},
	}

	report, err := NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath: corpusPath,
		Subset:     GraphSubsetQuick,
		Output:     "json",
		OutDir:     t.TempDir(),
	})

	require.NoError(t, err)
	require.NotNil(t, report)
	assert.Equal(t, 7, report.Summary.QueryCount)
	assert.Equal(t, 3, report.Summary.FailCount)
	// Blocking degradation now counts empty + unavailable (status-derived) AND the
	// truncated quality case: result_truncated counts toward blocking degradation
	// for quality cases per the TASK-GRA16 recoverability matrix.
	assert.InDelta(t, 3.0/7.0, report.Summary.DegradationBlockingRate, 0.001)
	assert.Equal(t, 3, report.Degradation.BlockingCount)
	assert.Equal(t, 1, report.Degradation.ByStatus[graph.GraphStatusEmpty])
	assert.Equal(t, 1, report.Degradation.ByStatus[graph.GraphStatusUnavailable])
	assert.Equal(t, 1, report.Degradation.WarningCounts[graph.WarningCode("graph_results_truncated")])
	assert.False(t, graphQueryResultByID(t, report, "GRA-EMPTY").Passed)
	assert.Contains(t, graphQueryResultByID(t, report, "GRA-EMPTY").FailureReason, "blocking graph status")
	assert.False(t, graphQueryResultByID(t, report, "GRA-INVALID").Passed)
	assert.Contains(t, graphQueryResultByID(t, report, "GRA-INVALID").Error, "invalid params")

	// Degradation labels are recorded per case and feed the blocking subset.
	assert.Equal(t, []GraphDegradationLabel{DegradationEmptyGraph},
		graphQueryResultByID(t, report, "GRA-EMPTY").DegradationLabels)
	assert.Equal(t, []GraphDegradationLabel{DegradationUnavailableGraph},
		graphQueryResultByID(t, report, "GRA-UNAVAILABLE").DegradationLabels)
	truncated := graphQueryResultByID(t, report, "GRA-TRUNCATED")
	assert.Equal(t, []GraphDegradationLabel{DegradationResultTruncated}, truncated.DegradationLabels)
	assert.Equal(t, []GraphDegradationLabel{DegradationResultTruncated}, truncated.BlockingDegradationLabels,
		"an unexpected truncation in a quality case counts toward blocking degradation")
	// stale/partial remain non-blocking servable substrate.
	assert.Empty(t, graphQueryResultByID(t, report, "GRA-PARTIAL").BlockingDegradationLabels)
	assert.Empty(t, graphQueryResultByID(t, report, "GRA-STALE").BlockingDegradationLabels)
	// The truncated case is excluded from quality metrics with a documented reason.
	assert.Equal(t, 1, report.Degradation.BlockingByLabel[DegradationResultTruncated])
	assertGraphQualityExcluded(t, report, "GRA-TRUNCATED", "unexpected_degradation")
}

func assertGraphQualityExcluded(t *testing.T, report *DirectGraphEvalReport, id, reasonContains string) {
	t.Helper()
	for _, excluded := range report.Degradation.QualityExcluded {
		if excluded.ID == id {
			assert.Contains(t, excluded.Reason, reasonContains)
			return
		}
	}
	require.FailNowf(t, "missing quality exclusion", "case %s not listed as excluded from quality", id)
}

func TestDirectGraphRunner_MeasuredFalseWhenNoEvidencePasses(t *testing.T) {
	corpusPath := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-EMPTY
    name: empty graph
    mode: find_references
    query: empty.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: empty.go
        rationale: empty graph should not certify measurement
`)
	client := &fakeDirectGraphClient{
		snapshot: freshGraphSnapshot(),
		responses: map[string]DirectGraphToolOutput{
			"GRA-EMPTY": {Available: false, Status: graph.GraphStatusEmpty, Degraded: true, Mode: graph.QueryModeFindReferences, Query: "empty.go"},
		},
	}

	report, err := NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath: corpusPath,
		Subset:     GraphSubsetQuick,
		Output:     "json",
		OutDir:     t.TempDir(),
	})

	require.NoError(t, err)
	require.NotNil(t, report)
	assert.False(t, report.GraphToolMeasured)
	assert.Equal(t, 0, report.Summary.MeasuredQueryCount)
	assert.NotEmpty(t, report.UnmeasuredReason)
	assert.Equal(t, 0, report.Summary.PassCount)
	assert.Equal(t, 1, report.Summary.FailCount)
}

func TestDirectGraphRunner_AcceptedAlternativeCountsAsRelevantPrecision(t *testing.T) {
	// A result that matches an accepted alternative (not a required expected row)
	// counts as relevant for precision but not for recall, end to end through the
	// runner and into the report (TASK-GRA15).
	corpusPath := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-ALT-E2E
    name: accepted alternative widens precision relevance
    mode: find_references
    query: primary.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: primary.go
        rationale: the required reference
    accepted_alternatives:
      - source_path: alt.go
        rationale: a sibling reference that is acceptable but not required
`)
	client := &fakeDirectGraphClient{
		snapshot: freshGraphSnapshot(),
		responses: map[string]DirectGraphToolOutput{
			"GRA-ALT-E2E": graphOutput(graph.QueryModeFindReferences, "primary.go", graph.GraphStatusFresh,
				graph.QueryResult{SourcePath: "primary.go"}, // matches expected
				graph.QueryResult{SourcePath: "alt.go"},     // matches accepted alternative
				graph.QueryResult{SourcePath: "noise.go"},   // irrelevant
			),
		},
	}

	report, err := NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath: corpusPath,
		Subset:     GraphSubsetQuick,
		Output:     "json",
		OutDir:     t.TempDir(),
	})
	require.NoError(t, err)

	res := graphQueryResultByID(t, report, "GRA-ALT-E2E")
	assert.Equal(t, 3, res.UniqueResultCount)
	// Recall counts expected only (primary.go): 1/1.
	assert.InDelta(t, 1.0, res.ExpectedRecallAt10, 0.0001, "recall over expected only")
	// Precision counts expected + alternative as relevant: 2 of 3 unique.
	assert.InDelta(t, 2.0/3.0, res.PrecisionAt10, 0.0001, "alternative widens precision relevance")
	assert.True(t, res.Passed, "all expected matched => passed")
}

func TestSQLiteDirectGraphClient_CorruptDBIsInitFailureNotEmpty(t *testing.T) {
	// A corrupt graph.db must surface as an init/open failure (with rebuild
	// guidance), never be silently classified as empty_graph (TASK-GRA16 AC:
	// "Corrupt graph DB is classified as unavailable/init failure, not
	// empty_graph"). empty_graph would wrongly read as "just run amanmcp index".
	dataDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "graph.db"),
		[]byte("this is not a valid sqlite database file"), 0o644))

	corpusPath := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-CORRUPT
    name: corrupt db
    mode: find_references
    query: internal/graph/query.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: internal/graph/query.go
        rationale: a corrupt db must not be reported as an empty graph
`)

	client := NewSQLiteDirectGraphClient(dataDir, "proj-corrupt")
	report, err := NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath: corpusPath,
		Subset:     GraphSubsetQuick,
		Output:     "json",
		OutDir:     t.TempDir(),
	})

	// The run fails fast as an init failure rather than producing a report that
	// classifies the corrupt store as empty_graph.
	require.Error(t, err)
	assert.Nil(t, report)
	assert.NotContains(t, err.Error(), string(DegradationEmptyGraph),
		"a corrupt db must not be classified as empty_graph")
	lowered := strings.ToLower(err.Error())
	assert.True(t,
		strings.Contains(lowered, "corrupt") ||
			strings.Contains(lowered, "not a database") ||
			strings.Contains(lowered, "open graph repository"),
		"error should identify an open/init failure, got: %s", err.Error())
}

func TestDirectGraphRunner_InvalidParamsErrorIsBlockingDegradation(t *testing.T) {
	// A graph.query input-validation rejection is classified as invalid_params
	// degradation (corpus/config authoring failure) that counts toward blocking
	// degradation, distinct from a generic transport error (TASK-GRA16).
	corpusPath := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-INVALID-E2E
    name: invalid params rejected by the tool
    mode: find_references
    query: internal/graph/query.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: internal/graph/query.go
        rationale: invalid params should be reported as a corpus/config error
`)
	client := &fakeDirectGraphClient{
		snapshot: freshGraphSnapshot(),
		responses: map[string]DirectGraphToolOutput{
			"GRA-INVALID-E2E": {},
		},
		errs: map[string]error{
			// Mirror the product contract: validateQueryRequest wraps the typed
			// graph.ErrInvalidQueryParams sentinel (DEBT-037 finding #3).
			"GRA-INVALID-E2E": fmt.Errorf("query must be project-relative and safe: %w", graph.ErrInvalidQueryParams),
		},
	}

	report, err := NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath: corpusPath,
		Subset:     GraphSubsetQuick,
		Output:     "json",
		OutDir:     t.TempDir(),
	})
	require.NoError(t, err)

	res := graphQueryResultByID(t, report, "GRA-INVALID-E2E")
	assert.Equal(t, []GraphDegradationLabel{DegradationInvalidParams}, res.DegradationLabels)
	assert.Equal(t, []GraphDegradationLabel{DegradationInvalidParams}, res.BlockingDegradationLabels)
	assert.True(t, res.BlockingDegradation)
	assert.Equal(t, 1, report.Degradation.BlockingByLabel[DegradationInvalidParams])
}

// TestDirectGraphRunner_PrecisionIdentityAmbiguityIsContractFailure proves the
// runtime guard (DEBT-037 finding #2) end-to-end: when returned results share a
// per-mode identity but disagree on relevance, the case fails as a contract
// failure (Scored=false, excluded from quality aggregates) rather than emitting
// an order-dependent precision number.
func TestDirectGraphRunner_PrecisionIdentityAmbiguityIsContractFailure(t *testing.T) {
	corpusPath := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-AMBIG
    name: precision identity ambiguity guard
    mode: find_references
    query: internal/x.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: internal/x.go
        confidence_label: exact
        rationale: only the exact-confidence row is relevant
`)
	client := &fakeDirectGraphClient{
		snapshot: freshGraphSnapshot(),
		responses: map[string]DirectGraphToolOutput{
			// Two results share the find_references identity (same source_path; kind/
			// relation/role empty) but differ on confidence_label, which flips
			// relevance — the order-dependent case the guard must catch.
			"GRA-AMBIG": graphOutput("find_references", "internal/x.go", graph.GraphStatusFresh,
				graph.QueryResult{SourcePath: "internal/x.go", ConfidenceLabel: graph.ConfidenceLow},
				graph.QueryResult{SourcePath: "internal/x.go", ConfidenceLabel: graph.ConfidenceExact},
			),
		},
	}

	report, err := NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath: corpusPath,
		Subset:     GraphSubsetQuick,
		Output:     "json",
		OutDir:     t.TempDir(),
	})
	require.NoError(t, err)

	res := graphQueryResultByID(t, report, "GRA-AMBIG")
	assert.Contains(t, res.FailureReason, "precision identity ambiguity",
		"ambiguous identity bucket must surface as a contract failure")
	assert.False(t, res.Passed, "ambiguous case must not pass")
	assert.False(t, res.Scored, "ambiguous case must be excluded from quality aggregates")
}

// TestDirectGraphMarkdownReport_BlockingRateRenderedOnce proves the Markdown
// report no longer prints the blocking degradation rate twice (DEBT-037 finding
// #6). The Summary and Degradation sections previously rendered the identical
// value; the dedicated Degradation section is the single source, keeping the
// unique "(N cases)" detail.
func TestDirectGraphMarkdownReport_BlockingRateRenderedOnce(t *testing.T) {
	report := &DirectGraphEvalReport{
		Summary:     DirectGraphSummary{DegradationBlockingRate: 0.5},
		Degradation: DirectGraphDegradationSummary{BlockingRate: 0.5, BlockingCount: 2},
	}
	md := directGraphMarkdownReport(report)
	assert.Equal(t, 1, strings.Count(md, "Blocking degradation rate"),
		"the blocking degradation rate must render exactly once")
	assert.Contains(t, md, "Blocking degradation rate: 0.50 (2 cases)",
		"the surviving line keeps the case-count detail")
}

func TestDirectGraphRunner_EnforcesExpectedWarnings(t *testing.T) {
	corpusPath := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-WARN
    name: stale graph warning
    mode: explain_symbol
    query: Stale
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - node_kind: symbol
        rationale: evidence matches but warning contract must also match
    degradation:
      expected_warning_codes: [graph_stale]
`)
	client := &fakeDirectGraphClient{
		snapshot: freshGraphSnapshot(),
		responses: map[string]DirectGraphToolOutput{
			"GRA-WARN": {
				Available: true,
				Status:    graph.GraphStatusStale,
				Degraded:  true,
				Mode:      graph.QueryModeExplainSymbol,
				Query:     "Stale",
				Results: []graph.QueryResult{{
					NodeKind: graph.NodeKindSymbol,
				}},
				Warnings: []graph.StatusWarning{{
					Code:    graph.WarningExtractorFailed,
					Message: "extractor failed",
				}},
			},
		},
	}

	report, err := NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath: corpusPath,
		Subset:     GraphSubsetQuick,
		Output:     "json",
		OutDir:     t.TempDir(),
	})

	require.NoError(t, err)
	result := graphQueryResultByID(t, report, "GRA-WARN")
	assert.False(t, result.Passed)
	assert.Contains(t, result.FailureReason, "missing expected warning codes")
	assert.Contains(t, result.FailureReason, "graph_stale")
}

func TestDirectGraphRunner_EnforcesAllowedStatuses(t *testing.T) {
	corpusPath := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-PARTIAL
    name: partial graph disallowed
    mode: explain_symbol
    query: Partial
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - node_kind: symbol
        rationale: evidence matches but partial status is not allowed for this fixture
    degradation:
      allowed_statuses: [fresh]
`)
	client := &fakeDirectGraphClient{
		snapshot: freshGraphSnapshot(),
		responses: map[string]DirectGraphToolOutput{
			"GRA-PARTIAL": graphOutput(graph.QueryModeExplainSymbol, "Partial", graph.GraphStatusPartial, graph.QueryResult{
				NodeKind: graph.NodeKindSymbol,
			}),
		},
	}

	report, err := NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath: corpusPath,
		Subset:     GraphSubsetQuick,
		Output:     "json",
		OutDir:     t.TempDir(),
	})

	require.NoError(t, err)
	result := graphQueryResultByID(t, report, "GRA-PARTIAL")
	assert.False(t, result.Passed)
	assert.Contains(t, result.FailureReason, "disallowed graph status partial")
}

func TestDirectGraphRunner_UsesPerQueryBlockingStatusesInSummaryAndGate(t *testing.T) {
	corpusPath := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-FRESH
    name: fresh graph
    mode: find_references
    query: fresh.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: fresh.go
        rationale: fresh evidence
  - id: GRA-PARTIAL-BLOCKING
    name: partial graph blocking for this fixture
    mode: explain_symbol
    query: Partial
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - node_kind: symbol
        rationale: this fixture treats partial as blocking
    degradation:
      blocking_statuses: [partial]
`)
	client := &fakeDirectGraphClient{
		snapshot: freshGraphSnapshot(),
		responses: map[string]DirectGraphToolOutput{
			"GRA-FRESH": graphOutput(graph.QueryModeFindReferences, "fresh.go", graph.GraphStatusFresh, graph.QueryResult{
				SourcePath: "fresh.go",
			}),
			"GRA-PARTIAL-BLOCKING": graphOutput(graph.QueryModeExplainSymbol, "Partial", graph.GraphStatusPartial, graph.QueryResult{
				NodeKind: graph.NodeKindSymbol,
			}),
		},
	}

	report, err := NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath:                   corpusPath,
		Subset:                       GraphSubsetQuick,
		Output:                       "json",
		OutDir:                       t.TempDir(),
		BlockingDegradationThreshold: 0.75,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, report.Degradation.BlockingCount)
	assert.InDelta(t, 0.5, report.Summary.DegradationBlockingRate, 0.001)

	report, err = NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath:                   corpusPath,
		Subset:                       GraphSubsetQuick,
		Output:                       "json",
		OutDir:                       t.TempDir(),
		FailOnRegression:             true,
		BlockingDegradationThreshold: 0.25,
	})
	require.Error(t, err)
	require.NotNil(t, report)
	assert.Contains(t, err.Error(), "blocking degradation rate 0.50 exceeds 0.25")
}

func TestDirectGraphRunner_FailsRegressionGateOnBlockingDegradation(t *testing.T) {
	// A servable fresh query keeps the run measured so the gate fails on
	// blocking degradation, not on a missing measurement.
	corpusPath := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-FRESH
    name: fresh measured query
    mode: find_references
    query: fresh.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: fresh.go
        rationale: a servable query keeps the run measured
  - id: GRA-EMPTY
    name: empty graph
    mode: find_references
    query: empty.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: empty.go
        rationale: empty graph should block
`)
	client := &fakeDirectGraphClient{
		snapshot: freshGraphSnapshot(),
		responses: map[string]DirectGraphToolOutput{
			"GRA-FRESH": graphOutput(graph.QueryModeFindReferences, "fresh.go", graph.GraphStatusFresh, graph.QueryResult{
				SourcePath: "fresh.go",
			}),
			"GRA-EMPTY": {Available: false, Status: graph.GraphStatusEmpty, Degraded: true, Mode: graph.QueryModeFindReferences, Query: "empty.go"},
		},
	}

	report, err := NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath:       corpusPath,
		Subset:           GraphSubsetQuick,
		Output:           "json",
		OutDir:           t.TempDir(),
		FailOnRegression: true,
	})

	require.Error(t, err)
	require.NotNil(t, report)
	assert.True(t, report.GraphToolMeasured, "fresh servable query keeps the run measured even though another case blocks")
	assert.Contains(t, err.Error(), "direct graph eval gate failed")
	assert.Contains(t, err.Error(), "blocking degradation")
}

func TestDirectGraphRunner_MeasuredTrueWhenServedButEvidenceFails(t *testing.T) {
	// The honest measurement contract: graph.query was invoked and served a
	// healthy answer, so the tool IS measured even though the evidence missed.
	// This is the core TASK-GRA11 fix versus the old PassCount>0 heuristic.
	corpusPath := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-SERVED-MISS
    name: served but missed evidence
    mode: find_references
    query: served.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: served.go
        rationale: graph.query served a healthy answer but the evidence did not match
`)
	client := &fakeDirectGraphClient{
		snapshot: freshGraphSnapshot(),
		responses: map[string]DirectGraphToolOutput{
			"GRA-SERVED-MISS": graphOutput(graph.QueryModeFindReferences, "served.go", graph.GraphStatusFresh, graph.QueryResult{
				SourcePath: "other.go",
			}),
		},
	}

	report, err := NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath: corpusPath,
		Subset:     GraphSubsetQuick,
		Output:     "json",
		OutDir:     t.TempDir(),
	})

	require.NoError(t, err)
	require.NotNil(t, report)
	assert.True(t, report.GraphToolMeasured)
	assert.Equal(t, 1, report.Summary.MeasuredQueryCount)
	assert.Equal(t, 0, report.Summary.PassCount)
	assert.Equal(t, DirectGraphEvaluationScope, report.EvaluationScope)
	assert.Empty(t, report.UnmeasuredReason)
}

func TestDirectGraphRunner_FailedStatusIsNotMeasured(t *testing.T) {
	// A `failed` graph build keeps the MCP available=true flag but the query
	// service short-circuits on it and serves no real answer, so it must NOT
	// certify graph.query measurement. This guards the QueryAvailable vs
	// QueryServable gap that an adversarial review surfaced.
	corpusPath := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-FAILED
    name: failed build
    mode: find_references
    query: failed.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: failed.go
        rationale: a failed graph build cannot certify measurement
`)
	client := &fakeDirectGraphClient{
		snapshot: freshGraphSnapshot(),
		responses: map[string]DirectGraphToolOutput{
			"GRA-FAILED": {Available: true, Status: graph.GraphStatusFailed, Degraded: true, Mode: graph.QueryModeFindReferences, Query: "failed.go"},
		},
	}

	report, err := NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath: corpusPath,
		Subset:     GraphSubsetQuick,
		Output:     "json",
		OutDir:     t.TempDir(),
	})

	require.NoError(t, err)
	require.NotNil(t, report)
	assert.False(t, report.GraphToolMeasured, "failed build serves nothing; tool is not measured")
	assert.Equal(t, 0, report.Summary.MeasuredQueryCount)
	assert.NotEmpty(t, report.UnmeasuredReason)
}

func TestDirectGraphRunner_UnmeasuredWhenNoServableInvocation(t *testing.T) {
	corpusPath := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-UNAVAILABLE-1
    name: unavailable one
    mode: find_references
    query: one.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: one.go
        rationale: unavailable graph cannot certify measurement
  - id: GRA-UNAVAILABLE-2
    name: unavailable two
    mode: explain_symbol
    query: Two
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - node_kind: symbol
        rationale: unavailable graph cannot certify measurement
`)
	client := &fakeDirectGraphClient{
		snapshot: freshGraphSnapshot(),
		responses: map[string]DirectGraphToolOutput{
			"GRA-UNAVAILABLE-1": {Available: false, Status: graph.GraphStatusUnavailable, Degraded: true, Mode: graph.QueryModeFindReferences, Query: "one.go"},
			"GRA-UNAVAILABLE-2": {Available: false, Status: graph.GraphStatusUnavailable, Degraded: true, Mode: graph.QueryModeExplainSymbol, Query: "Two"},
		},
	}

	report, err := NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath: corpusPath,
		Subset:     GraphSubsetQuick,
		Output:     "json",
		OutDir:     t.TempDir(),
	})

	require.NoError(t, err)
	require.NotNil(t, report)
	assert.False(t, report.GraphToolMeasured)
	assert.Equal(t, 0, report.Summary.MeasuredQueryCount)
	assert.NotEmpty(t, report.UnmeasuredReason)
	assert.Contains(t, report.UnmeasuredReason, "graph.query")
}

func TestDirectGraphRunner_GateFailsWhenUnmeasured(t *testing.T) {
	corpusPath := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-UNAVAILABLE
    name: unavailable graph
    mode: find_references
    query: unavailable.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: unavailable.go
        rationale: an unmeasured run must fail the gate
`)
	client := &fakeDirectGraphClient{
		snapshot: freshGraphSnapshot(),
		responses: map[string]DirectGraphToolOutput{
			"GRA-UNAVAILABLE": {Available: false, Status: graph.GraphStatusUnavailable, Degraded: true, Mode: graph.QueryModeFindReferences, Query: "unavailable.go"},
		},
	}

	report, err := NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath:       corpusPath,
		Subset:           GraphSubsetQuick,
		Output:           "json",
		OutDir:           t.TempDir(),
		FailOnRegression: true,
	})

	require.Error(t, err)
	require.NotNil(t, report)
	assert.False(t, report.GraphToolMeasured)
	assert.Contains(t, err.Error(), "not measured")
}

func TestDirectGraphRunner_FailsWhenNoCasesSelected(t *testing.T) {
	// Corpus has only explain_symbol rows; selecting find_references yields zero cases.
	corpusPath := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-ES-ONLY
    name: explain only
    mode: explain_symbol
    query: Only
    subsets: [full, mode:explain_symbol]
    holdout: false
    source: test
    expected:
      - node_kind: symbol
        rationale: present so the corpus is valid
`)
	client := &fakeDirectGraphClient{snapshot: freshGraphSnapshot()}

	_, err := NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath: corpusPath,
		Subset:     "mode:find_references",
		Output:     "json",
		OutDir:     t.TempDir(),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no queries")
}

func TestDirectGraphMeasurementReason(t *testing.T) {
	tests := []struct {
		name        string
		report      *DirectGraphEvalReport
		wantReason  bool
		wantSubstrs []string
	}{
		{
			name: "healthy direct graph run is measured",
			report: &DirectGraphEvalReport{
				MeasuredTool:    DirectGraphMeasuredTool,
				EvaluationScope: DirectGraphEvaluationScope,
				Summary:         DirectGraphSummary{QueryCount: 3, MeasuredQueryCount: 2},
			},
			wantReason: false,
		},
		{
			name: "zero selected cases",
			report: &DirectGraphEvalReport{
				MeasuredTool:    DirectGraphMeasuredTool,
				EvaluationScope: DirectGraphEvaluationScope,
				Summary:         DirectGraphSummary{QueryCount: 0},
			},
			wantReason:  true,
			wantSubstrs: []string{"no", "case"},
		},
		{
			name: "no servable invocation",
			report: &DirectGraphEvalReport{
				MeasuredTool:    DirectGraphMeasuredTool,
				EvaluationScope: DirectGraphEvaluationScope,
				Summary:         DirectGraphSummary{QueryCount: 3, MeasuredQueryCount: 0},
			},
			wantReason:  true,
			wantSubstrs: []string{"graph.query"},
		},
		{
			name: "search-only fallback tool",
			report: &DirectGraphEvalReport{
				MeasuredTool:    "search_engine",
				EvaluationScope: DirectGraphEvaluationScope,
				Summary:         DirectGraphSummary{QueryCount: 3, MeasuredQueryCount: 3},
			},
			wantReason:  true,
			wantSubstrs: []string{"search_engine"},
		},
		{
			name: "wrong evaluation scope",
			report: &DirectGraphEvalReport{
				MeasuredTool:    DirectGraphMeasuredTool,
				EvaluationScope: "search_engine_graph_heavy_classes",
				Summary:         DirectGraphSummary{QueryCount: 3, MeasuredQueryCount: 3},
			},
			wantReason:  true,
			wantSubstrs: []string{"scope"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := directGraphMeasurementReason(tt.report)
			if tt.wantReason {
				require.NotEmpty(t, reason)
				for _, substr := range tt.wantSubstrs {
					assert.Contains(t, reason, substr)
				}
			} else {
				assert.Empty(t, reason)
			}
		})
	}
}

func TestDirectGraphRunner_FailsRegressionGateOnEvidenceFailure(t *testing.T) {
	corpusPath := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-MISS
    name: missing evidence
    mode: find_references
    query: missing.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: missing.go
        rationale: expected evidence must be matched
`)
	client := &fakeDirectGraphClient{
		snapshot: freshGraphSnapshot(),
		responses: map[string]DirectGraphToolOutput{
			"GRA-MISS": graphOutput(graph.QueryModeFindReferences, "missing.go", graph.GraphStatusFresh, graph.QueryResult{
				SourcePath: "other.go",
			}),
		},
	}

	report, err := NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath:       corpusPath,
		Subset:           GraphSubsetQuick,
		Output:           "json",
		OutDir:           t.TempDir(),
		FailOnRegression: true,
	})

	require.Error(t, err)
	require.NotNil(t, report)
	assert.Equal(t, 1, report.Summary.FailCount)
	// Intent preserved (vs the old all-or-nothing FailCount gate): a measured
	// quality case that misses its evidence now fails the gate via the per-mode
	// relevance threshold rather than a raw failure count.
	assert.Contains(t, err.Error(), "relevance thresholds not met")
	assert.Contains(t, err.Error(), graph.QueryModeFindReferences)
	assert.Contains(t, err.Error(), "expected_recall_at_10")
	assert.False(t, report.ByMode[graph.QueryModeFindReferences].ThresholdMet)
}

func TestDirectGraphRunner_DegradedClassMissDoesNotFailGate(t *testing.T) {
	// A degraded-class case is reported but excluded from the relevance
	// aggregates, so its miss must not fail the gate while a healthy quality
	// case meets its mode threshold.
	corpusPath := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-QUALITY
    name: healthy quality case
    mode: find_references
    query: good.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: good.go
        rationale: quality case matches its evidence
  - id: GRA-DEGRADED
    name: degraded probe expected to miss
    mode: find_references
    query: degraded.go
    subsets: [quick]
    holdout: false
    source: test
    expectation_class: degraded
    expected:
      - source_path: present.go
        rationale: a future relationship not represented today; miss is expected
`)
	client := &fakeDirectGraphClient{
		snapshot: freshGraphSnapshot(),
		responses: map[string]DirectGraphToolOutput{
			"GRA-QUALITY": graphOutput(graph.QueryModeFindReferences, "good.go", graph.GraphStatusFresh, graph.QueryResult{
				SourcePath: "good.go",
			}),
			"GRA-DEGRADED": graphOutput(graph.QueryModeFindReferences, "degraded.go", graph.GraphStatusFresh, graph.QueryResult{
				SourcePath: "unrelated.go",
			}),
		},
	}

	report, err := NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath:       corpusPath,
		Subset:           GraphSubsetQuick,
		Output:           "json",
		OutDir:           t.TempDir(),
		FailOnRegression: true,
	})

	require.NoError(t, err, "degraded-class miss must not fail the relevance gate")
	require.NotNil(t, report)
	assert.Equal(t, 1, report.ByMode[graph.QueryModeFindReferences].QualityCount,
		"only the quality case counts toward the relevance aggregate")
	assert.True(t, report.ByMode[graph.QueryModeFindReferences].ThresholdMet)
}

func TestDirectGraphRunner_QualityContractFailureFailsGate(t *testing.T) {
	// A measured quality case that returns a disallowed status is a contract
	// failure, not a relevance miss: it must hard-fail the gate even though a
	// sibling quality case keeps the run measured and meets its threshold.
	corpusPath := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-OK
    name: healthy quality case
    mode: find_references
    query: good.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: good.go
        rationale: keeps the run measured and meets threshold
  - id: GRA-DISALLOWED
    name: quality case with disallowed status
    mode: find_references
    query: partial.go
    subsets: [quick]
    holdout: false
    source: test
    expected:
      - source_path: partial.go
        rationale: evidence matches but partial status is a contract violation here
    degradation:
      allowed_statuses: [fresh]
`)
	client := &fakeDirectGraphClient{
		snapshot: freshGraphSnapshot(),
		responses: map[string]DirectGraphToolOutput{
			"GRA-OK": graphOutput(graph.QueryModeFindReferences, "good.go", graph.GraphStatusFresh, graph.QueryResult{
				SourcePath: "good.go",
			}),
			"GRA-DISALLOWED": graphOutput(graph.QueryModeFindReferences, "partial.go", graph.GraphStatusPartial, graph.QueryResult{
				SourcePath: "partial.go",
			}),
		},
	}

	report, err := NewDirectGraphRunner(client).Run(context.Background(), GraphOptions{
		CorpusPath:       corpusPath,
		Subset:           GraphSubsetQuick,
		Output:           "json",
		OutDir:           t.TempDir(),
		FailOnRegression: true,
	})

	require.Error(t, err)
	require.NotNil(t, report)
	assert.True(t, report.GraphToolMeasured, "the healthy sibling keeps the run measured")
	assert.Contains(t, err.Error(), "quality contract failures")
	assert.Contains(t, err.Error(), "GRA-DISALLOWED")
	assert.Contains(t, err.Error(), "disallowed graph status")
}

func TestDirectGraphToolOutput_UsesMCPAvailableStatusContract(t *testing.T) {
	tests := []struct {
		status    graph.GraphStatus
		available bool
	}{
		{status: graph.GraphStatusUnavailable, available: false},
		{status: graph.GraphStatusIncompatible, available: false},
		{status: graph.GraphStatusEmpty, available: false},
		{status: graph.GraphStatusFresh, available: true},
		{status: graph.GraphStatusStale, available: true},
		{status: graph.GraphStatusPartial, available: true},
		{status: graph.GraphStatusFailed, available: true},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			output := directGraphToolOutput(graph.QueryResponse{Status: tt.status})
			assert.Equal(t, tt.available, output.Available)
			assert.Equal(t, graph.QueryAvailable(tt.status), output.Available)
		})
	}
}

func TestGraphPathContains_RequiresExactSegment(t *testing.T) {
	path := []string{"node:file:internal/a.go", "symbol_has_chunk", "node:chunk:abc"}

	assert.True(t, graphPathContains(path, "symbol_has_chunk"))
	assert.False(t, graphPathContains(path, "file"))
	assert.False(t, graphPathContains(path, "chunk"))
}

func TestBriefGraphStatusSnapshot_BoundsExtractorPayload(t *testing.T) {
	snapshot := freshGraphSnapshot()
	snapshot.Extractors = nil
	for i := 0; i < 20; i++ {
		snapshot.Extractors = append(snapshot.Extractors, graph.ExtractorSummary{
			Name:       graph.ExtractorCheap,
			SourcePath: "internal/success.go",
			Status:     graph.ExtractorStatusSuccess,
		})
	}
	for i := 0; i < 12; i++ {
		snapshot.Extractors = append(snapshot.Extractors, graph.ExtractorSummary{
			Name:       graph.ExtractorCheap,
			SourcePath: "internal/failed.go",
			Status:     graph.ExtractorStatusFailed,
			Message:    "extractor failed",
		})
	}

	brief := briefGraphStatusSnapshot(snapshot)

	assert.Equal(t, 20, brief.ExtractorStatusCounts[graph.ExtractorStatusSuccess])
	assert.Equal(t, 12, brief.ExtractorStatusCounts[graph.ExtractorStatusFailed])
	assert.Len(t, brief.Extractors, maxGraphStatusExtractorSamples)
	for _, extractor := range brief.Extractors {
		assert.NotEqual(t, graph.ExtractorStatusSuccess, extractor.Status)
	}
}

type fakeDirectGraphClient struct {
	snapshot  *graph.StatusSnapshot
	responses map[string]DirectGraphToolOutput
	errs      map[string]error
	calls     []GraphQuery
}

func (f *fakeDirectGraphClient) SnapshotGraph(context.Context) (*graph.StatusSnapshot, error) {
	return f.snapshot, nil
}

func (f *fakeDirectGraphClient) QueryGraph(_ context.Context, query GraphQuery) (DirectGraphToolOutput, error) {
	f.calls = append(f.calls, query)
	if err := f.errs[query.ID]; err != nil {
		return DirectGraphToolOutput{}, err
	}
	return f.responses[query.ID], nil
}

func freshGraphSnapshot() *graph.StatusSnapshot {
	return &graph.StatusSnapshot{
		Available:     true,
		SchemaVersion: graph.SchemaVersion,
		Status:        graph.GraphStatusFresh,
		Freshness: graph.Freshness{
			State: graph.FreshnessFresh,
		},
		Nodes: graph.CountSummary{Total: 1, ByKind: map[string]int{
			string(graph.NodeKindSymbol): 1,
		}},
		Edges:       graph.CountSummary{Total: 1},
		ActiveEdges: graph.CountSummary{Total: 1},
		Extractors: []graph.ExtractorSummary{{
			Name:      graph.ExtractorCheap,
			Status:    graph.ExtractorStatusSuccess,
			NodeCount: 1,
			EdgeCount: 1,
		}},
		Confidence: map[string]int{
			string(graph.ConfidenceHigh): 1,
		},
	}
}

func graphOutput(mode, query string, status graph.GraphStatus, results ...graph.QueryResult) DirectGraphToolOutput {
	return graph.NewQueryToolOutput(graph.QueryResponse{
		Status:   status,
		Degraded: status != graph.GraphStatusFresh,
		Mode:     mode,
		Query:    query,
		Results:  results,
	})
}

func graphQueryResultByID(t *testing.T, report *DirectGraphEvalReport, id string) DirectGraphQueryResult {
	t.Helper()
	for _, query := range report.Queries {
		if query.ID == id {
			return query
		}
	}
	require.FailNowf(t, "missing graph query result", "id %s not found", id)
	return DirectGraphQueryResult{}
}

func reportQueryByID(t *testing.T, corpusPath, id string) GraphQuery {
	t.Helper()
	corpus, err := LoadGraphCorpus(corpusPath)
	require.NoError(t, err)
	for _, query := range corpus.Queries {
		if query.ID == id {
			return query
		}
	}
	require.FailNowf(t, "missing graph query", "id %s not found", id)
	return GraphQuery{}
}
