package eval

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/graph"
	"github.com/stretchr/testify/assert"
)

// TestGraphDegradationLabels_MapsStatusAndWarnings proves the eval-owned
// degradation taxonomy (TASK-GRA16) maps cleanly to current graph statuses and
// warning codes without inventing conflicting product states.
func TestGraphDegradationLabels_MapsStatusAndWarnings(t *testing.T) {
	tests := []struct {
		name string
		in   graphDegradationInput
		want []GraphDegradationLabel
	}{
		{
			name: "fresh healthy graph has no degradation",
			in:   graphDegradationInput{Status: graph.GraphStatusFresh, Language: "go"},
			want: nil,
		},
		{
			name: "empty status maps to empty_graph",
			in:   graphDegradationInput{Status: graph.GraphStatusEmpty},
			want: []GraphDegradationLabel{DegradationEmptyGraph},
		},
		{
			// Corrupt/missing graph.db surfaces as an unavailable provider state,
			// NOT as empty_graph (TASK-GRA16 AC).
			name: "unavailable status maps to unavailable_graph not empty_graph",
			in:   graphDegradationInput{Status: graph.GraphStatusUnavailable},
			want: []GraphDegradationLabel{DegradationUnavailableGraph},
		},
		{
			name: "incompatible status maps to incompatible_graph",
			in:   graphDegradationInput{Status: graph.GraphStatusIncompatible},
			want: []GraphDegradationLabel{DegradationIncompatibleGraph},
		},
		{
			name: "failed status maps to failed_graph",
			in:   graphDegradationInput{Status: graph.GraphStatusFailed},
			want: []GraphDegradationLabel{DegradationFailedGraph},
		},
		{
			name: "stale status maps to stale_graph",
			in:   graphDegradationInput{Status: graph.GraphStatusStale},
			want: []GraphDegradationLabel{DegradationStaleGraph},
		},
		{
			name: "partial status maps to partial_graph",
			in:   graphDegradationInput{Status: graph.GraphStatusPartial},
			want: []GraphDegradationLabel{DegradationPartialGraph},
		},
		{
			name: "graph_stale warning on a fresh status still maps to stale_graph",
			in:   graphDegradationInput{Status: graph.GraphStatusFresh, WarningCodes: []graph.WarningCode{graph.WarningGraphStale}},
			want: []GraphDegradationLabel{DegradationStaleGraph},
		},
		{
			name: "extractor_partial and extractor_failed map to partial_graph",
			in: graphDegradationInput{
				Status:       graph.GraphStatusFresh,
				WarningCodes: []graph.WarningCode{graph.WarningExtractorPartial, graph.WarningExtractorFailed},
			},
			want: []GraphDegradationLabel{DegradationPartialGraph},
		},
		{
			name: "build_failed warning maps to failed_graph",
			in:   graphDegradationInput{Status: graph.GraphStatusFresh, WarningCodes: []graph.WarningCode{graph.WarningBuildFailed}},
			want: []GraphDegradationLabel{DegradationFailedGraph},
		},
		{
			name: "graph_stale_edges warning maps to stale_edges",
			in:   graphDegradationInput{Status: graph.GraphStatusFresh, WarningCodes: []graph.WarningCode{graph.WarningGraphStaleEdges}},
			want: []GraphDegradationLabel{DegradationStaleEdges},
		},
		{
			name: "stale results without a warning still map to stale_edges",
			in:   graphDegradationInput{Status: graph.GraphStatusFresh, StaleResults: true},
			want: []GraphDegradationLabel{DegradationStaleEdges},
		},
		{
			name: "graph_results_truncated warning maps to result_truncated",
			in:   graphDegradationInput{Status: graph.GraphStatusFresh, WarningCodes: []graph.WarningCode{graph.WarningCode("graph_results_truncated")}},
			want: []GraphDegradationLabel{DegradationResultTruncated},
		},
		{
			name: "traversal_budget_exhausted uses structured budget_reason",
			in: graphDegradationInput{
				Status:       graph.GraphStatusFresh,
				WarningCodes: []graph.WarningCode{graph.WarningTraversalBudgetExhausted},
				Warnings: []graph.StatusWarning{{
					Code:         graph.WarningTraversalBudgetExhausted,
					BudgetReason: graph.TraversalBudgetResults,
					BudgetLimit:  10,
				}},
			},
			want: []GraphDegradationLabel{DegradationResultTruncated, DegradationTraversalBudgetExhausted},
		},
		{
			name: "results budget reason is detected when another budget warning precedes it",
			in: graphDegradationInput{
				Status:       graph.GraphStatusFresh,
				WarningCodes: []graph.WarningCode{graph.WarningTraversalBudgetExhausted},
				Warnings: []graph.StatusWarning{
					{
						Code:         graph.WarningTraversalBudgetExhausted,
						BudgetReason: graph.TraversalBudgetPerEdgeKind,
						BudgetLimit:  3,
					},
					{
						Code:         graph.WarningTraversalBudgetExhausted,
						BudgetReason: graph.TraversalBudgetResults,
						BudgetLimit:  5,
					},
				},
			},
			want: []GraphDegradationLabel{DegradationResultTruncated, DegradationTraversalBudgetExhausted},
		},
		{
			name: "unsupported corpus language maps to unsupported_language",
			in:   graphDegradationInput{Status: graph.GraphStatusFresh, Language: "rust"},
			want: []GraphDegradationLabel{DegradationUnsupportedLanguage},
		},
		{
			name: "unsupported_language product warning maps to unsupported_language",
			in: graphDegradationInput{
				Status:       graph.GraphStatusFresh,
				WarningCodes: []graph.WarningCode{graph.WarningUnsupportedLanguage},
			},
			want: []GraphDegradationLabel{DegradationUnsupportedLanguage},
		},
		{
			name: "supported languages do not map to unsupported_language",
			in:   graphDegradationInput{Status: graph.GraphStatusFresh, Language: "typescript"},
			want: nil,
		},
		{
			name: "invalid params maps to invalid_params",
			in:   graphDegradationInput{InvalidParams: true},
			want: []GraphDegradationLabel{DegradationInvalidParams},
		},
		{
			// A corrupt/missing-DB open/list failure returns a zero-value envelope
			// (empty status) via a transport error. It must appear in the by-label
			// breakdown as unavailable_graph, never empty_graph (DEBT-037 finding #5).
			name: "transport error with empty status maps to unavailable_graph",
			in:   graphDegradationInput{TransportError: true},
			want: []GraphDegradationLabel{DegradationUnavailableGraph},
		},
		{
			// An input-validation rejection is also a transport error with empty
			// status, but it is an authoring failure — it stays invalid_params and is
			// NOT relabeled as an infrastructure failure.
			name: "invalid-params transport error stays invalid_params not unavailable_graph",
			in:   graphDegradationInput{TransportError: true, InvalidParams: true},
			want: []GraphDegradationLabel{DegradationInvalidParams},
		},
		{
			name: "multiple conditions yield a sorted, de-duplicated label set",
			in: graphDegradationInput{
				Status:       graph.GraphStatusPartial,
				WarningCodes: []graph.WarningCode{graph.WarningCode("graph_results_truncated"), graph.WarningGraphStaleEdges},
				Language:     "rust",
			},
			want: []GraphDegradationLabel{
				DegradationPartialGraph,
				DegradationResultTruncated,
				DegradationStaleEdges,
				DegradationUnsupportedLanguage,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := graphDegradationLabels(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestGraphBlockingDegradationLabels_FollowsRecoverabilityMatrix proves which
// labels count toward blocking_degradation_rate by default and how expectation,
// stale-edge opt-in, and per-query blocking statuses neutralize them
// (TASK-GRA16 recoverability matrix).
func TestGraphBlockingDegradationLabels_FollowsRecoverabilityMatrix(t *testing.T) {
	tests := []struct {
		name        string
		labels      []GraphDegradationLabel
		status      graph.GraphStatus
		resultCount int
		query       GraphQuery
		warnings    []graph.StatusWarning
		want        []GraphDegradationLabel
	}{
		{
			name:   "empty_graph blocks by default",
			labels: []GraphDegradationLabel{DegradationEmptyGraph},
			status: graph.GraphStatusEmpty,
			query:  GraphQuery{Degradation: GraphDegradationExpectation{BlockingStatuses: defaultBlockingGraphStatuses}},
			want:   []GraphDegradationLabel{DegradationEmptyGraph},
		},
		{
			name:   "empty_graph does not block when it is the expected fixture state",
			labels: []GraphDegradationLabel{DegradationEmptyGraph},
			status: graph.GraphStatusEmpty,
			query: GraphQuery{Degradation: GraphDegradationExpectation{
				BlockingStatuses: defaultBlockingGraphStatuses,
				ExpectedLabels:   []GraphDegradationLabel{DegradationEmptyGraph},
			}},
			want: nil,
		},
		{
			name:   "stale_graph never blocks by default",
			labels: []GraphDegradationLabel{DegradationStaleGraph},
			status: graph.GraphStatusStale,
			query:  GraphQuery{Degradation: GraphDegradationExpectation{BlockingStatuses: defaultBlockingGraphStatuses}},
			want:   nil,
		},
		{
			name:   "partial_graph does not block when partial is allowed substrate",
			labels: []GraphDegradationLabel{DegradationPartialGraph},
			status: graph.GraphStatusPartial,
			query:  GraphQuery{Degradation: GraphDegradationExpectation{BlockingStatuses: defaultBlockingGraphStatuses}},
			want:   nil,
		},
		{
			name:   "partial_graph blocks when a case opts partial into blocking_statuses",
			labels: []GraphDegradationLabel{DegradationPartialGraph},
			status: graph.GraphStatusPartial,
			query:  GraphQuery{Degradation: GraphDegradationExpectation{BlockingStatuses: []graph.GraphStatus{graph.GraphStatusPartial}}},
			want:   []GraphDegradationLabel{DegradationPartialGraph},
		},
		{
			name:        "result_truncated blocks when it cuts below the scored rank window",
			labels:      []GraphDegradationLabel{DegradationResultTruncated},
			status:      graph.GraphStatusFresh,
			resultCount: 5, // fewer than directGraphRankWindow (10): truncation shortened the window
			query:       GraphQuery{Degradation: GraphDegradationExpectation{BlockingStatuses: defaultBlockingGraphStatuses}},
			want:        []GraphDegradationLabel{DegradationResultTruncated},
		},
		{
			name:        "result_truncated does not block when a full rank window was returned",
			labels:      []GraphDegradationLabel{DegradationResultTruncated},
			status:      graph.GraphStatusFresh,
			resultCount: 10, // full window: truncation beyond it does not affect precision@K/hit@K
			query:       GraphQuery{Degradation: GraphDegradationExpectation{BlockingStatuses: defaultBlockingGraphStatuses}},
			want:        nil,
		},
		{
			name:        "result_truncated does not block when expected",
			labels:      []GraphDegradationLabel{DegradationResultTruncated},
			status:      graph.GraphStatusFresh,
			resultCount: 3,
			query: GraphQuery{Degradation: GraphDegradationExpectation{
				BlockingStatuses: defaultBlockingGraphStatuses,
				ExpectedLabels:   []GraphDegradationLabel{DegradationResultTruncated},
			}},
			want: nil,
		},
		{
			name:   "stale_edges does not block when explicitly opted in via include_stale",
			labels: []GraphDegradationLabel{DegradationStaleEdges},
			status: graph.GraphStatusFresh,
			query:  GraphQuery{IncludeStale: true, Degradation: GraphDegradationExpectation{BlockingStatuses: defaultBlockingGraphStatuses}},
			want:   nil,
		},
		{
			name:   "stale_edges blocks when unexpected and not opted in",
			labels: []GraphDegradationLabel{DegradationStaleEdges},
			status: graph.GraphStatusFresh,
			query:  GraphQuery{Degradation: GraphDegradationExpectation{BlockingStatuses: defaultBlockingGraphStatuses}},
			want:   []GraphDegradationLabel{DegradationStaleEdges},
		},
		{
			name:   "unsupported_language blocks when mislabeled as supported",
			labels: []GraphDegradationLabel{DegradationUnsupportedLanguage},
			status: graph.GraphStatusFresh,
			query:  GraphQuery{Degradation: GraphDegradationExpectation{BlockingStatuses: defaultBlockingGraphStatuses}},
			want:   []GraphDegradationLabel{DegradationUnsupportedLanguage},
		},
		{
			name:   "unsupported_language does not block when the case is marked unsupported",
			labels: []GraphDegradationLabel{DegradationUnsupportedLanguage},
			status: graph.GraphStatusFresh,
			query: GraphQuery{Degradation: GraphDegradationExpectation{
				BlockingStatuses: defaultBlockingGraphStatuses,
				ExpectedLabels:   []GraphDegradationLabel{DegradationUnsupportedLanguage},
			}},
			want: nil,
		},
		{
			name:   "invalid_params blocks and is classified separately",
			labels: []GraphDegradationLabel{DegradationInvalidParams},
			status: "",
			query:  GraphQuery{Degradation: GraphDegradationExpectation{BlockingStatuses: defaultBlockingGraphStatuses}},
			want:   []GraphDegradationLabel{DegradationInvalidParams},
		},
		{
			name:        "traversal_budget_exhausted blocks when results reason follows another budget warning",
			labels:      []GraphDegradationLabel{DegradationTraversalBudgetExhausted},
			status:      graph.GraphStatusFresh,
			resultCount: 5,
			query:       GraphQuery{Degradation: GraphDegradationExpectation{BlockingStatuses: defaultBlockingGraphStatuses}},
			warnings: []graph.StatusWarning{
				{
					Code:         graph.WarningTraversalBudgetExhausted,
					BudgetReason: graph.TraversalBudgetPerEdgeKind,
					BudgetLimit:  3,
				},
				{
					Code:         graph.WarningTraversalBudgetExhausted,
					BudgetReason: graph.TraversalBudgetResults,
					BudgetLimit:  5,
				},
			},
			want: []GraphDegradationLabel{DegradationTraversalBudgetExhausted},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := graphBlockingDegradationLabels(tt.labels, tt.status, tt.resultCount, tt.query, tt.warnings)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestIsGraphInvalidParamsError distinguishes graph.query input-validation
// rejections (corpus/config authoring failures) from infrastructure errors via
// the typed graph.ErrInvalidQueryParams sentinel (DEBT-037 finding #3), so only
// the former are classified as invalid_params degradation and the classification
// cannot drift when product message text is reworded.
func TestIsGraphInvalidParamsError(t *testing.T) {
	// Errors wrapping the sentinel (what graph.validateQueryRequest now emits)
	// classify as invalid params regardless of the human-readable prefix.
	invalid := []error{
		fmt.Errorf("project_id is required: %w", graph.ErrInvalidQueryParams),
		fmt.Errorf("query is required: %w", graph.ErrInvalidQueryParams),
		fmt.Errorf(`unsupported graph query mode "callers": %w`, graph.ErrInvalidQueryParams),
		// A future reword of the human-readable text must still classify correctly.
		fmt.Errorf("graph query params rejected (reworded message): %w", graph.ErrInvalidQueryParams),
		graph.ErrInvalidQueryParams,
	}
	for _, err := range invalid {
		assert.True(t, isGraphInvalidParamsError(err), "should classify %q as invalid params", err)
	}

	// Infrastructure errors do not wrap the sentinel even if their text happens to
	// resemble a validation phrase.
	notInvalid := []error{
		errors.New("open graph repository: database is locked"),
		errors.New("read graph status: context deadline exceeded"),
		fmt.Errorf("list graph edges: %w", errors.New("disk I/O error")),
		errors.New("query is required"), // bare text, no sentinel: must NOT match
	}
	for _, err := range notInvalid {
		assert.False(t, isGraphInvalidParamsError(err), "should not classify %q as invalid params", err)
	}
	assert.False(t, isGraphInvalidParamsError(nil))
}

// TestGraphLabelRecoverability_CoversEveryLabel proves every required label has
// a recoverability classification and remediation string, so the report's
// recoverability matrix can never reference an unclassified label.
func TestGraphLabelRecoverability_CoversEveryLabel(t *testing.T) {
	for _, label := range allGraphDegradationLabels() {
		info := graphLabelRecoverability(label)
		assert.Equal(t, label, info.Label)
		assert.NotEmpty(t, info.Recoverability, "label %s missing recoverability", label)
		assert.NotEmpty(t, info.Remediation, "label %s missing remediation", label)
	}
}
