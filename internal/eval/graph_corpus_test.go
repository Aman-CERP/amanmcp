package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadGraphCorpus_ParsesAndDefaultsDirectGraphSchema(t *testing.T) {
	path := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-FR-Q01
    name: query service references
    mode: find_references
    query: internal/graph/query.go
    subject_type: path
    limit: 10
    include_stale: false
    subsets: [quick, full, mode:find_references]
    holdout: false
    source: manual
    expected:
      - source_path: internal/graph/query.go
        node_kind: symbol
        role: related
        relation: file_defines_symbol
        confidence_label: high
        evidence_method: tree_sitter
        graph_path_contains: [file_defines_symbol]
        rationale: query service should expose defining symbol evidence
    metadata:
      language: go
`)

	corpus, err := LoadGraphCorpus(path)

	require.NoError(t, err)
	require.Len(t, corpus.Queries, 1)
	got := corpus.Queries[0]
	assert.Equal(t, GraphCorpusSchemaVersion, corpus.SchemaVersion)
	assert.Equal(t, graph.QueryModeFindReferences, got.Mode)
	assert.Equal(t, graph.SubjectTypePath, got.SubjectType)
	assert.Equal(t, []string{GraphSubsetQuick, GraphSubsetFull, "mode:find_references"}, got.Subsets)
	assert.Equal(t, []graph.GraphStatus{graph.GraphStatusFresh, graph.GraphStatusStale, graph.GraphStatusPartial}, got.Degradation.AllowedStatuses)
	assert.Equal(t, []graph.GraphStatus{graph.GraphStatusUnavailable, graph.GraphStatusIncompatible, graph.GraphStatusEmpty, graph.GraphStatusFailed}, got.Degradation.BlockingStatuses)
	assert.Equal(t, "go", got.Metadata["language"])
	assert.Equal(t, graph.NodeKindSymbol, got.Expected[0].NodeKind)
	assert.Equal(t, graph.EdgeKindFileDefinesSymbol, got.Expected[0].Relation)
	assert.Equal(t, graph.ConfidenceHigh, got.Expected[0].ConfidenceLabel)
	// A case that omits expectation_class defaults to the quality class so it is
	// scored against the per-mode relevance thresholds (TASK-GRA12/13/14).
	assert.Equal(t, GraphExpectationClassQuality, got.ExpectationClass)
}

func TestLoadGraphCorpus_ParsesExplicitExpectationClass(t *testing.T) {
	path := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-GAP-Q01
    name: reverse caller traversal not yet represented
    mode: impact_analysis
    query: internal/graph/query.go
    subsets: [full, mode:impact_analysis]
    holdout: false
    source: manual
    expectation_class: gap
    expected:
      - source_path: internal/graph/query.go
        relation: symbol_has_chunk
        rationale: downstream chunk evidence exists today; caller edges do not
`)

	corpus, err := LoadGraphCorpus(path)

	require.NoError(t, err)
	require.Len(t, corpus.Queries, 1)
	assert.Equal(t, GraphExpectationClassGap, corpus.Queries[0].ExpectationClass)
}

func TestLoadGraphCorpus_RejectsInvalidDirectGraphContracts(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name: "unsupported mode",
			body: `
schema_version: 1
queries:
  - id: GRA-BAD-Q01
    name: bad mode
    mode: callers
    query: internal/graph/query.go
    subsets: [quick]
    holdout: false
    source: manual
    expected:
      - source_path: internal/graph/query.go
        rationale: has a matcher
`,
			wantErr: `unsupported graph mode "callers"`,
		},
		{
			name: "unsafe query",
			body: `
schema_version: 1
queries:
  - id: GRA-BAD-Q02
    name: unsafe query
    mode: find_references
    query: ../secret.go
    subsets: [quick]
    holdout: false
    source: manual
    expected:
      - source_path: internal/graph/query.go
        rationale: has a matcher
`,
			wantErr: "query must be project-relative and safe",
		},
		{
			name: "unsupported subject type",
			body: `
schema_version: 1
queries:
  - id: GRA-BAD-Q02A
    name: bad subject type
    mode: find_references
    query: internal/graph/query.go
    subject_type: fuzzy
    subsets: [quick]
    holdout: false
    source: manual
    expected:
      - source_path: internal/graph/query.go
        rationale: has a matcher
`,
			wantErr: `unsupported subject_type "fuzzy"`,
		},
		{
			name: "trailing parent segment",
			body: `
schema_version: 1
queries:
  - id: GRA-BAD-Q02B
    name: unsafe trailing parent
    mode: find_references
    query: internal/graph/..
    subsets: [quick]
    holdout: false
    source: manual
    expected:
      - source_path: internal/graph/query.go
        rationale: has a matcher
`,
			wantErr: "query must be project-relative and safe",
		},
		{
			name: "no expected matcher",
			body: `
schema_version: 1
queries:
  - id: GRA-BAD-Q03
    name: no matcher
    mode: explain_symbol
    query: QueryService
    subsets: [quick]
    holdout: false
    source: manual
    expected:
      - rationale: rationale alone is not a matcher
`,
			wantErr: "expected evidence without a matcher",
		},
		{
			name: "holdout subset unsupported",
			body: `
schema_version: 1
queries:
  - id: GRA-BAD-Q04
    name: holdout subset
    mode: impact_analysis
    query: internal/graph/query.go
    subsets: [holdout]
    holdout: true
    source: manual
    expected:
      - source_path: internal/graph/query.go
        rationale: has a matcher
`,
			wantErr: `unsupported graph subset "holdout"`,
		},
		{
			name: "invalid status",
			body: `
schema_version: 1
queries:
  - id: GRA-BAD-Q05
    name: invented status
    mode: find_references
    query: internal/graph/query.go
    subsets: [full]
    holdout: false
    source: manual
    expected:
      - source_path: internal/graph/query.go
        rationale: has a matcher
    degradation:
      allowed_statuses: [fresh, empty_graph]
`,
			wantErr: `unsupported graph status "empty_graph"`,
		},
		{
			name: "search corpus fields are rejected",
			body: `
schema_version: 1
queries:
  - id: GRA-BAD-Q06
    name: search-shaped row
    mode: find_references
    query: internal/graph/query.go
    tool: search
    subsets: [quick]
    holdout: false
    source: manual
    expected_results:
      - path: internal/graph/query.go
        grade: 3
        rationale: search corpus evidence should not parse here
`,
			wantErr: "field tool not found",
		},
		{
			name: "limit above direct graph maximum",
			body: `
schema_version: 1
queries:
  - id: GRA-BAD-Q06B
    name: high limit
    mode: find_references
    query: internal/graph/query.go
    limit: 51
    subsets: [quick]
    holdout: false
    source: manual
    expected:
      - source_path: internal/graph/query.go
        rationale: graph query limit must stay bounded
`,
			wantErr: "limit must be between 0 and 50",
		},
		{
			name: "nul query is rejected",
			body: `
schema_version: 1
queries:
  - id: GRA-BAD-Q06C
    name: nul query
    mode: find_references
    query: "internal/graph/query.go\0bad"
    subsets: [quick]
    holdout: false
    source: manual
    expected:
      - source_path: internal/graph/query.go
        rationale: graph query text must be safe
`,
			wantErr: "query contains unsafe NUL byte",
		},
		{
			name: "missing holdout is rejected",
			body: `
schema_version: 1
queries:
  - id: GRA-BAD-Q07
    name: missing holdout
    mode: find_references
    query: internal/graph/query.go
    subsets: [quick]
    source: manual
    expected:
      - source_path: internal/graph/query.go
        rationale: holdout must be explicit
`,
			wantErr: "missing holdout",
		},
		{
			name: "duplicate expected evidence identity",
			body: `
schema_version: 1
queries:
  - id: GRA-BAD-Q08
    name: duplicate expected evidence
    mode: find_references
    query: internal/graph/query.go
    subsets: [quick]
    holdout: false
    source: manual
    expected:
      - source_path: internal/graph/query.go
        node_kind: symbol
        rationale: first duplicate
      - source_path: internal/graph/query.go
        node_kind: symbol
        rationale: second duplicate
`,
			wantErr: "duplicate expected evidence",
		},
		{
			name: "unsupported expectation class",
			body: `
schema_version: 1
queries:
  - id: GRA-BAD-Q09B
    name: bogus expectation class
    mode: find_references
    query: internal/graph/query.go
    subsets: [quick]
    holdout: false
    source: manual
    expectation_class: speculative
    expected:
      - source_path: internal/graph/query.go
        rationale: expectation_class must be a known class
`,
			wantErr: `unsupported expectation_class "speculative"`,
		},
		{
			name: "unsupported direct graph role",
			body: `
schema_version: 1
queries:
  - id: GRA-BAD-Q09
    name: unsupported role
    mode: find_references
    query: internal/graph/query.go
    subsets: [quick]
    holdout: false
    source: manual
    expected:
      - source_path: internal/graph/query.go
        role: caller
        rationale: role must match graph.query output vocabulary
`,
			wantErr: `unsupported role "caller"`,
		},
		{
			name: "missing source is rejected",
			body: `
schema_version: 1
queries:
  - id: GRA-BAD-Q10
    name: missing source
    mode: find_references
    query: internal/graph/query.go
    subsets: [quick]
    holdout: false
    expected:
      - source_path: internal/graph/query.go
        rationale: source must be explicit
`,
			wantErr: "missing source",
		},
		{
			name: "duplicate id after trimming",
			body: `
schema_version: 1
queries:
  - id: GRA-DUP-Q01
    name: duplicate one
    mode: find_references
    query: internal/graph/query.go
    subsets: [quick]
    holdout: false
    source: manual
    expected:
      - source_path: internal/graph/query.go
        rationale: first row
  - id: " GRA-DUP-Q01 "
    name: duplicate two
    mode: explain_symbol
    query: QueryService
    subsets: [quick]
    holdout: false
    source: manual
    expected:
      - node_kind: symbol
        rationale: second row
`,
			wantErr: `duplicate graph query id "GRA-DUP-Q01"`,
		},
		{
			name: "accepted alternative without a matcher",
			body: `
schema_version: 1
queries:
  - id: GRA-ALT-Q01
    name: alternative missing matcher
    mode: find_references
    query: internal/graph/query.go
    subsets: [quick]
    holdout: false
    source: manual
    expected:
      - source_path: internal/graph/query.go
        rationale: required row
    accepted_alternatives:
      - rationale: rationale alone is not a matcher
`,
			wantErr: "accepted alternative: expected evidence without a matcher",
		},
		{
			name: "accepted alternative duplicates expected evidence",
			body: `
schema_version: 1
queries:
  - id: GRA-ALT-Q02
    name: alternative duplicates expected
    mode: find_references
    query: internal/graph/query.go
    subsets: [quick]
    holdout: false
    source: manual
    expected:
      - source_path: internal/graph/query.go
        rationale: required row
    accepted_alternatives:
      - source_path: internal/graph/query.go
        rationale: same identity as expected
`,
			wantErr: "accepted alternative duplicates expected evidence",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadGraphCorpus(writeTempGraphCorpus(t, tt.body))

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestLoadGraphCorpus_ParsesAcceptedAlternatives proves the optional
// accepted_alternatives block (TASK-GRA15) parses with the same matcher
// discipline as expected rows and is carried onto the loaded GraphQuery.
func TestLoadGraphCorpus_ParsesAcceptedAlternatives(t *testing.T) {
	path := writeTempGraphCorpus(t, `
schema_version: 1
queries:
  - id: GRA-ALT-OK
    name: case with an accepted alternative
    mode: find_references
    query: internal/graph/query.go
    subsets: [quick]
    holdout: false
    source: manual
    expected:
      - source_path: internal/graph/query.go
        rationale: required row
    accepted_alternatives:
      - source_path: internal/graph/query_output.go
        rationale: a sibling file is an acceptable alternative reference
`)

	corpus, err := LoadGraphCorpus(path)
	require.NoError(t, err)
	require.Len(t, corpus.Queries, 1)
	q := corpus.Queries[0]
	require.Len(t, q.AcceptedAlternatives, 1)
	assert.Equal(t, "internal/graph/query_output.go", q.AcceptedAlternatives[0].SourcePath)
}

// TestRealGraphCorpus_LoadsAndMeetsTicketCounts validates the committed corpus
// (internal/validation/testdata/graph-queries.yaml) in CI without a graph.db:
// it must parse, and every mode must carry at least 15 non-holdout cases and at
// least 4 quick-subset cases (TASK-GRA12/13/14 acceptance criteria).
func TestRealGraphCorpus_LoadsAndMeetsTicketCounts(t *testing.T) {
	// The test runs with CWD = this package directory; the corpus lives two
	// levels up under internal/validation/testdata.
	corpus, err := LoadGraphCorpus(filepath.Join("..", "validation", "testdata", "graph-queries.yaml"))
	require.NoError(t, err, "committed graph eval corpus must parse and validate")

	modes := []string{
		graph.QueryModeFindReferences,
		graph.QueryModeExplainSymbol,
		graph.QueryModeImpactAnalysis,
	}
	nonHoldout := map[string]int{}
	quick := map[string]int{}
	classes := map[string]int{}
	for _, q := range corpus.Queries {
		classes[q.ExpectationClass]++
		if q.Holdout {
			continue
		}
		nonHoldout[q.Mode]++
		if hasGraphSubset(q, GraphSubsetQuick) {
			quick[q.Mode]++
		}
	}

	for _, mode := range modes {
		assert.GreaterOrEqualf(t, nonHoldout[mode], 15,
			"mode %s must have >=15 non-holdout cases, got %d", mode, nonHoldout[mode])
		assert.GreaterOrEqualf(t, quick[mode], 4,
			"mode %s must have >=4 quick cases, got %d", mode, quick[mode])
	}
	// Degraded/gap probe cases must exist so their separate reporting path is
	// exercised by the committed corpus.
	assert.Positive(t, classes[GraphExpectationClassDegraded], "expected at least one degraded-class case")
	assert.Positive(t, classes[GraphExpectationClassGap], "expected at least one gap-class case")
	assert.GreaterOrEqual(t, classes[GraphExpectationClassNegativeAdversarial], 8,
		"TASK-GRA26 requires >=8 negative_adversarial cases")
}

func TestSelectGraphQueries_SubsetsExcludeHoldoutUntilExplicitlyOwned(t *testing.T) {
	corpus := GraphCorpus{SchemaVersion: GraphCorpusSchemaVersion, Queries: []GraphQuery{
		graphQuery("GRA-FR-Q01", graph.QueryModeFindReferences, false, GraphSubsetQuick, GraphSubsetFull, "mode:find_references"),
		graphQuery("GRA-ES-Q01", graph.QueryModeExplainSymbol, false, GraphSubsetFull, "mode:explain_symbol"),
		graphQuery("GRA-IA-Q01", graph.QueryModeImpactAnalysis, true, GraphSubsetQuick, GraphSubsetFull, "mode:impact_analysis"),
	}}

	got, err := SelectGraphQueries(corpus.Queries, GraphSelection{Subset: GraphSubsetQuick})
	require.NoError(t, err)
	assert.Equal(t, []string{"GRA-FR-Q01"}, graphQueryIDs(got))

	got, err = SelectGraphQueries(corpus.Queries, GraphSelection{Subset: GraphSubsetFull})
	require.NoError(t, err)
	assert.Equal(t, []string{"GRA-FR-Q01", "GRA-ES-Q01"}, graphQueryIDs(got))

	got, err = SelectGraphQueries(corpus.Queries, GraphSelection{Subset: "mode:find_references"})
	require.NoError(t, err)
	assert.Equal(t, []string{"GRA-FR-Q01"}, graphQueryIDs(got))

	_, err = SelectGraphQueries(corpus.Queries, GraphSelection{Subset: "holdout"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unsupported graph subset "holdout"`)
}

func TestDirectGraphEvalReportSchema_IsSeparateFromSearchGraphGate(t *testing.T) {
	report := DirectGraphEvalReport{
		SchemaVersion:     DirectGraphReportSchemaVersion,
		ReportType:        DirectGraphReportType,
		MeasuredTool:      DirectGraphMeasuredTool,
		EvaluationScope:   DirectGraphEvaluationScope,
		GraphToolMeasured: true,
		Run: DirectGraphRunMetadata{
			Command:    "amanmcp eval graph --subset quick --output both",
			CorpusPath: DefaultGraphCorpusPath,
			Subset:     GraphSubsetQuick,
			Output:     "both",
		},
		Summary: DirectGraphSummary{
			QueryCount:         1,
			MeasuredQueryCount: 1,
			PassCount:          1,
			ZeroResultRate:     0,
			P50LatencyMs:       7,
			P95LatencyMs:       7,
		},
		ModeCounts: map[string]int{
			graph.QueryModeFindReferences: 1,
		},
		ByMode: map[string]DirectGraphModeSummary{
			graph.QueryModeFindReferences: {
				QueryCount: 1,
			},
		},
		Degradation: DirectGraphDegradationSummary{
			ByStatus: map[graph.GraphStatus]int{
				graph.GraphStatusFresh: 1,
			},
		},
		Queries: []DirectGraphQueryResult{{
			ID:                   "GRA-FR-Q01",
			Mode:                 graph.QueryModeFindReferences,
			Query:                "internal/graph/query.go",
			Status:               graph.GraphStatusFresh,
			Available:            true,
			ResultCount:          1,
			MatchedEvidenceCount: 1,
			LatencyMs:            7,
			Passed:               true,
			Results: []graph.QueryResult{{
				NodeID:          "symbol:QueryService",
				NodeKind:        graph.NodeKindSymbol,
				SourcePath:      "internal/graph/query.go",
				Role:            "related",
				Relation:        graph.EdgeKindFileDefinesSymbol,
				ConfidenceLabel: graph.ConfidenceHigh,
				EvidenceMethod:  "tree_sitter",
				Path:            singleHopGraphPath("file:internal/graph/query.go", string(graph.EdgeKindFileDefinesSymbol), "symbol:QueryService"),
			}},
			Warnings: []graph.StatusWarning{{
				Code:    graph.WarningGraphStale,
				Message: "graph is stale",
			}},
		}},
		StatusSnapshots: []GraphStatusSnapshotBrief{{
			Available:     true,
			SchemaVersion: graph.SchemaVersion,
			Status:        graph.GraphStatusFresh,
			Freshness:     graph.FreshnessFresh,
			Extractors: []graph.ExtractorSummary{{
				Name:      "tree_sitter",
				Status:    graph.ExtractorStatusSuccess,
				NodeCount: 1,
				EdgeCount: 1,
			}},
			Confidence: map[string]int{
				string(graph.ConfidenceHigh): 1,
			},
			Warnings: []graph.StatusWarning{{
				Code:    graph.WarningGraphStale,
				Message: "graph is stale",
			}},
		}},
	}

	data, err := json.Marshal(report)

	require.NoError(t, err)
	assert.Contains(t, string(data), `"report_type":"direct_graph_eval"`)
	assert.Contains(t, string(data), `"measured_tool":"graph.query"`)
	assert.Contains(t, string(data), `"evaluation_scope":"direct_graph_query_modes"`)
	assert.Contains(t, string(data), `"graph_tool_measured":true`)
	assert.Contains(t, string(data), `"measured_query_count":1`)
	assert.Contains(t, string(data), `"schema_version":2`)
	assert.Contains(t, string(data), `"summary"`)
	assert.Contains(t, string(data), `"by_mode"`)
	assert.Contains(t, string(data), `"degradation"`)
	assert.Contains(t, string(data), `"results"`)
	assert.Contains(t, string(data), `"node_id":"symbol:QueryService"`)
	assert.Contains(t, string(data), `"node_kind":"symbol"`)
	assert.Contains(t, string(data), `"source_path":"internal/graph/query.go"`)
	assert.Contains(t, string(data), `"role":"related"`)
	assert.Contains(t, string(data), `"relation":"file_defines_symbol"`)
	assert.Contains(t, string(data), `"confidence_label":"high"`)
	assert.Contains(t, string(data), `"evidence_method":"tree_sitter"`)
	assert.Contains(t, string(data), `"path":{"from":`)
	assert.Contains(t, string(data), `"extractors"`)
	assert.Contains(t, string(data), `"confidence"`)
	assert.Contains(t, string(data), `"warnings"`)
	// Rank-aware relevance schema is carried by the direct graph report (GRA12/13/14).
	assert.Contains(t, string(data), `"expected_recall_at_10"`)
	assert.Contains(t, string(data), `"precision_at_10"`)
	assert.Contains(t, string(data), `"hit_rate_at_3"`)
	assert.Contains(t, string(data), `"hit_rate_at_10"`)
	assert.Contains(t, string(data), `"quality_count"`)
	assert.Contains(t, string(data), `"threshold_met"`)
	assert.Contains(t, string(data), `"scored"`)
	assert.NotContains(t, string(data), "search_engine_graph_heavy_classes")
}

func writeTempGraphCorpus(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "graph-queries.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

func graphQuery(id string, mode string, holdout bool, subsets ...string) GraphQuery {
	return GraphQuery{
		ID:      id,
		Name:    id,
		Mode:    mode,
		Query:   "internal/graph/query.go",
		Subsets: subsets,
		Holdout: holdout,
		Source:  "test",
		Expected: []GraphExpectedEvidence{{
			SourcePath: "internal/graph/query.go",
			Rationale:  "test expectation",
		}},
	}
}

func graphQueryIDs(queries []GraphQuery) []string {
	ids := make([]string, 0, len(queries))
	for _, query := range queries {
		ids = append(ids, query.ID)
	}
	return ids
}
