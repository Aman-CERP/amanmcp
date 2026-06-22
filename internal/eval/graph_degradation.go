package eval

import (
	"errors"
	"sort"
	"strings"

	"github.com/Aman-CERP/amanmcp/internal/graph"
)

// GraphDegradationLabel is an eval-owned degradation category (TASK-GRA16). The
// labels map onto current product graph statuses and warning codes without
// inventing conflicting product states; invalid_params is an eval-side
// classification with no required product warning. unsupported_language can also
// be inferred from corpus metadata or graph.WarningUnsupportedLanguage.
type GraphDegradationLabel string

const (
	DegradationEmptyGraph               GraphDegradationLabel = "empty_graph"
	DegradationStaleGraph               GraphDegradationLabel = "stale_graph"
	DegradationPartialGraph             GraphDegradationLabel = "partial_graph"
	DegradationUnavailableGraph         GraphDegradationLabel = "unavailable_graph"
	DegradationIncompatibleGraph        GraphDegradationLabel = "incompatible_graph"
	DegradationFailedGraph              GraphDegradationLabel = "failed_graph"
	DegradationStaleEdges               GraphDegradationLabel = "stale_edges"
	DegradationUnsupportedLanguage      GraphDegradationLabel = "unsupported_language"
	DegradationResultTruncated          GraphDegradationLabel = "result_truncated"
	DegradationTraversalBudgetExhausted GraphDegradationLabel = "traversal_budget_exhausted"
	DegradationInvalidParams            GraphDegradationLabel = "invalid_params"
)

// graphResultsTruncatedWarning mirrors the product's inline truncation warning
// code (internal/graph/query.go), which is not yet a named constant there.
const graphResultsTruncatedWarning = graph.WarningCode("graph_results_truncated")

// allGraphDegradationLabels returns every label in canonical (sorted) order.
func allGraphDegradationLabels() []GraphDegradationLabel {
	labels := []GraphDegradationLabel{
		DegradationEmptyGraph,
		DegradationStaleGraph,
		DegradationPartialGraph,
		DegradationUnavailableGraph,
		DegradationIncompatibleGraph,
		DegradationFailedGraph,
		DegradationStaleEdges,
		DegradationUnsupportedLanguage,
		DegradationResultTruncated,
		DegradationTraversalBudgetExhausted,
		DegradationInvalidParams,
	}
	sortGraphDegradationLabels(labels)
	return labels
}

// graphEvalSupportedLanguages is the eval-owned set of languages the graph
// extractor meaningfully covers (import edges for go/typescript/javascript/
// python; symbols and chunks for any chunked language). It is intentionally
// generous so unsupported_language only fires for clearly-unsupported corpora;
// a case targeting an unsupported language declares it via expected_labels.
var graphEvalSupportedLanguages = map[string]bool{
	"go":         true,
	"typescript": true,
	"javascript": true,
	"python":     true,
}

// graphDegradationInput is the minimal projection of a query result needed to
// classify degradation, kept free of report types so the mapper is pure and
// unit-testable.
type graphDegradationInput struct {
	Status        graph.GraphStatus
	WarningCodes  []graph.WarningCode
	Warnings      []graph.StatusWarning
	StaleResults  bool
	Language      string
	InvalidParams bool
	// TransportError is true when the graph.query call returned an error rather
	// than an envelope, so Status is the zero value ("") and no status-derived
	// label can be set from the switch above (DEBT-037 finding #5).
	TransportError bool
}

// graphDegradationLabels classifies the degradation present in one graph.query
// result. It returns a sorted, de-duplicated set and nil when the result is a
// healthy fresh answer over a supported language.
func graphDegradationLabels(in graphDegradationInput) []GraphDegradationLabel {
	set := make(map[GraphDegradationLabel]struct{})

	switch in.Status {
	case graph.GraphStatusEmpty:
		set[DegradationEmptyGraph] = struct{}{}
	case graph.GraphStatusStale:
		set[DegradationStaleGraph] = struct{}{}
	case graph.GraphStatusPartial:
		set[DegradationPartialGraph] = struct{}{}
	case graph.GraphStatusUnavailable:
		set[DegradationUnavailableGraph] = struct{}{}
	case graph.GraphStatusIncompatible:
		set[DegradationIncompatibleGraph] = struct{}{}
	case graph.GraphStatusFailed:
		set[DegradationFailedGraph] = struct{}{}
	}

	for _, code := range in.WarningCodes {
		switch code {
		case graph.WarningGraphStale:
			set[DegradationStaleGraph] = struct{}{}
		case graph.WarningExtractorPartial, graph.WarningExtractorFailed:
			set[DegradationPartialGraph] = struct{}{}
		case graph.WarningBuildFailed:
			set[DegradationFailedGraph] = struct{}{}
		case graph.WarningGraphUnavailable:
			set[DegradationUnavailableGraph] = struct{}{}
		case graph.WarningSchemaIncompatible:
			set[DegradationIncompatibleGraph] = struct{}{}
		case graph.WarningGraphStaleEdges:
			set[DegradationStaleEdges] = struct{}{}
		case graphResultsTruncatedWarning:
			set[DegradationResultTruncated] = struct{}{}
		case graph.WarningTraversalBudgetExhausted:
			set[DegradationTraversalBudgetExhausted] = struct{}{}
			if hasTraversalBudgetReason(in.Warnings, graph.WarningTraversalBudgetExhausted, graph.TraversalBudgetResults) {
				set[DegradationResultTruncated] = struct{}{}
			}
		case graph.WarningUnsupportedLanguage:
			set[DegradationUnsupportedLanguage] = struct{}{}
		}
	}

	// Stale edges can surface either as a warning (above) or as stale=true on
	// returned evidence rows; both map to the same eval label.
	if in.StaleResults {
		set[DegradationStaleEdges] = struct{}{}
	}

	if lang := strings.TrimSpace(strings.ToLower(in.Language)); lang != "" && !graphEvalSupportedLanguages[lang] {
		set[DegradationUnsupportedLanguage] = struct{}{}
	}

	if in.InvalidParams {
		set[DegradationInvalidParams] = struct{}{}
	}

	// A transport error returns a zero-value envelope (empty status), so the status
	// switch above contributes nothing. When it is not an invalid-params authoring
	// failure it is an infrastructure failure (corrupt/missing DB, open/list error)
	// — label it unavailable_graph so the by-label degradation breakdown is complete
	// (DEBT-037 finding #5). empty_graph stays reserved for a healthy-but-empty graph
	// (status == empty), which is never this path.
	if in.TransportError && in.Status == "" && !in.InvalidParams {
		set[DegradationUnavailableGraph] = struct{}{}
	}

	if len(set) == 0 {
		return nil
	}
	labels := make([]GraphDegradationLabel, 0, len(set))
	for label := range set {
		labels = append(labels, label)
	}
	sortGraphDegradationLabels(labels)
	return labels
}

// graphLabelIsStatusDerived reports whether a label's blocking decision is owned
// by the per-query allowed/blocking status machinery (TASK-GRA11) rather than
// the recoverability matrix default. Status-derived labels defer to that machinery
// so partial/stale stay non-blocking by default (this repo's real index is
// normally partial) while empty/unavailable/incompatible/failed block by default.
func graphLabelIsStatusDerived(label GraphDegradationLabel) bool {
	switch label {
	case DegradationEmptyGraph, DegradationStaleGraph, DegradationPartialGraph,
		DegradationUnavailableGraph, DegradationIncompatibleGraph, DegradationFailedGraph:
		return true
	default:
		return false
	}
}

// graphBlockingDegradationLabels returns the subset of labels that count toward
// blocking_degradation_rate, applying the recoverability matrix: a label in the
// case's expected_labels never blocks; status-derived labels defer to the
// per-query blocking statuses; stale_edges is non-blocking when opted in via
// include_stale; invalid_params and unsupported_language block by default.
//
// result_truncated counts toward blocking only when truncation actually shortened
// the scored rank window — i.e. resultCount < directGraphRankWindow. Truncation
// beyond a full rank window does not change precision@K/hit@K (those score only
// the top window), and counting it would make the <=10% gate unmeetable on a
// healthy graph, where common symbols legitimately have more than ten references.
// A case can still force-expect truncation via expected_labels.
func graphBlockingDegradationLabels(labels []GraphDegradationLabel, status graph.GraphStatus, resultCount int, query GraphQuery, warnings []graph.StatusWarning) []GraphDegradationLabel {
	if len(labels) == 0 {
		return nil
	}
	expected := graphExpectedLabelSet(query)
	var blocking []GraphDegradationLabel
	for _, label := range labels {
		if expected[label] {
			continue
		}
		switch {
		case graphLabelIsStatusDerived(label):
			if isBlockingGraphStatus(query.Degradation.BlockingStatuses, status) {
				blocking = append(blocking, label)
			}
		case label == DegradationStaleEdges:
			if !query.IncludeStale {
				blocking = append(blocking, label)
			}
		case label == DegradationResultTruncated:
			if resultCount < directGraphRankWindow {
				blocking = append(blocking, label)
			}
		case label == DegradationTraversalBudgetExhausted:
			if hasTraversalBudgetReason(warnings, graph.WarningTraversalBudgetExhausted, graph.TraversalBudgetResults) &&
				resultCount < directGraphRankWindow {
				blocking = append(blocking, label)
			}
		default: // invalid_params, unsupported_language
			blocking = append(blocking, label)
		}
	}
	if len(blocking) == 0 {
		return nil
	}
	sortGraphDegradationLabels(blocking)
	return blocking
}

// graphExpectedLabelSet indexes the labels a case intentionally targets so they
// are excluded from blocking degradation.
func graphExpectedLabelSet(query GraphQuery) map[GraphDegradationLabel]bool {
	if len(query.Degradation.ExpectedLabels) == 0 {
		return nil
	}
	set := make(map[GraphDegradationLabel]bool, len(query.Degradation.ExpectedLabels))
	for _, label := range query.Degradation.ExpectedLabels {
		set[label] = true
	}
	return set
}

// GraphDegradationInfo is the recoverability classification for one label,
// surfaced in reports so operators and release reviewers can tell quality
// regressions from infrastructure regressions (TASK-GRA16 recoverability matrix).
type GraphDegradationInfo struct {
	Label          GraphDegradationLabel `json:"label"`
	Recoverability string                `json:"recoverability"`
	Remediation    string                `json:"remediation"`
}

// graphLabelRecoverability returns the recoverability classification and
// remediation guidance for a label. Every label is covered; an unknown label
// returns a conservative non-recoverable classification rather than an empty one.
func graphLabelRecoverability(label GraphDegradationLabel) GraphDegradationInfo {
	switch label {
	case DegradationEmptyGraph:
		return GraphDegradationInfo{label, "recoverable", "run a normal `amanmcp index` and rerun eval"}
	case DegradationStaleGraph:
		return GraphDegradationInfo{label, "recoverable", "run a normal `amanmcp index` and rerun eval; document rerun in release evidence"}
	case DegradationPartialGraph:
		return GraphDegradationInfo{label, "recoverable after extractor/build fix", "inspect extractor failures, fix the root cause, and reindex"}
	case DegradationUnavailableGraph:
		return GraphDegradationInfo{label, "environment/config dependent", "configure/open the graph repository and rerun eval"}
	case DegradationIncompatibleGraph:
		return GraphDegradationInfo{label, "recoverable by rebuild or migration", "rebuild the derived graph store with the current binary"}
	case DegradationFailedGraph:
		return GraphDegradationInfo{label, "recoverable after build fix", "inspect the build failure, fix the root cause, and reindex"}
	case DegradationStaleEdges:
		return GraphDegradationInfo{label, "recoverable", "reindex or remove the stale-edge opt-in"}
	case DegradationUnsupportedLanguage:
		return GraphDegradationInfo{label, "query targets a language without graph extractor support", "use a supported language subject or add extractor support"}
	case DegradationResultTruncated:
		return GraphDegradationInfo{label, "query/corpus tunable", "lower expected breadth or adjust the limit within the product cap"}
	case DegradationTraversalBudgetExhausted:
		return GraphDegradationInfo{label, "query/corpus tunable", "raise the relevant traversal budget within policy or narrow the query"}
	case DegradationInvalidParams:
		return GraphDegradationInfo{label, "corpus/config authoring failure", "fix the corpus row"}
	default:
		return GraphDegradationInfo{label, "non-recoverable", "investigate; label is unclassified"}
	}
}

// isGraphInvalidParamsError reports whether a graph.query transport error is an
// input-validation rejection (corpus/config authoring failure) rather than an
// infrastructure error. It uses errors.Is against the typed graph.ErrInvalidQueryParams
// sentinel so classification cannot drift from the product's validation message
// text (DEBT-037 finding #3). Infrastructure errors (status/node/edge reads) do
// not wrap that sentinel and so are correctly excluded.
func hasTraversalBudgetReason(warnings []graph.StatusWarning, code graph.WarningCode, reason graph.TraversalBudgetReason) bool {
	for _, warning := range warnings {
		if warning.Code != code {
			continue
		}
		if warning.BudgetReason == reason {
			return true
		}
	}
	return false
}

func isGraphInvalidParamsError(err error) bool {
	return errors.Is(err, graph.ErrInvalidQueryParams)
}

func sortGraphDegradationLabels(labels []GraphDegradationLabel) {
	sort.Slice(labels, func(i, j int) bool { return labels[i] < labels[j] })
}

// isKnownGraphDegradationLabel reports whether a label is part of the eval's
// degradation taxonomy, so corpus authoring typos in expected_labels are
// rejected rather than silently ignored.
func isKnownGraphDegradationLabel(label GraphDegradationLabel) bool {
	for _, known := range allGraphDegradationLabels() {
		if known == label {
			return true
		}
	}
	return false
}
