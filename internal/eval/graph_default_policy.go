package eval

import (
	"fmt"
	"strings"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/config"
)

// GraphDefaultPolicy (TASK-GRA17) is the regression guard that decides whether
// graph-default search augmentation MAY be enabled, based solely on direct
// graph.query eval evidence (TASK-GRA10..GRA16). It is intentionally a pure,
// side-effect-free decision kernel:
//
//   - It does NOT persist state, mutate graph_status, or touch search wiring.
//     No default graph augmentation switch exists yet (see the design note at
//     docs/reference/architecture/graph-default-policy.md); when one is added,
//     it must route its enable decision through EvaluateGraphDefaultPolicy.
//   - It governs DEFAULT augmentation only. It never disables explicit
//     graph.query — the gate state exposes no such control surface.
//   - It is fail-closed: a missing, unmeasured, stale, or failing report blocks
//     default augmentation. Silence is never treated as success.
//
// Future runtime integration that lives in internal/search must extract this
// kernel into a leaf package (or pass a plain input projection) to avoid an
// import cycle, because internal/eval imports internal/search.

// GraphDefaultDecision is the binary policy outcome.
type GraphDefaultDecision string

const (
	// GraphDefaultBlocked means default graph augmentation must not be enabled.
	GraphDefaultBlocked GraphDefaultDecision = "blocked"
	// GraphDefaultAllowed means fresh, passing direct graph eval evidence permits
	// default graph augmentation for the current source version.
	GraphDefaultAllowed GraphDefaultDecision = "allowed"
)

const (
	graphDefaultReasonNoReport      = "no direct graph eval report available"
	graphDefaultReasonNotMeasured   = "direct graph tool not measured (graph_tool_measured=false)"
	graphDefaultReasonStaleSource   = "direct graph eval evidence is stale for the current source version"
	graphDefaultReasonNoCurrentVer  = "current source version not provided; eval freshness cannot be certified"
	graphDefaultReasonModeThreshold = "per-mode relevance threshold not met"
	graphDefaultReasonContract      = "graph contract failures present"
	graphDefaultReasonBlockingDeg   = "blocking degradation rate above threshold"
	graphDefaultReasonInsufficient  = "insufficient consecutive passing runs on distinct source versions"
	graphDefaultReasonPassed        = "direct graph eval passed for the current source version"
)

// GraphDefaultPolicyOptions parameterises a policy evaluation. The zero value is
// usable and conservative: it pins to no source version, applies the built-in
// thresholds, and requires a single fresh passing run.
type GraphDefaultPolicyOptions struct {
	// CurrentSourceVersion is the source version (git SHA / index version) the
	// caller wants to enable default augmentation for. It is required to reach an
	// "allowed" decision: the latest eval must have been run against this exact
	// version. When empty, freshness cannot be certified and the policy defers.
	CurrentSourceVersion string

	// BlockingDegradationThreshold is the TASK-GRA16 ceiling on
	// Summary.DegradationBlockingRate. Values <= 0 use the config default.
	BlockingDegradationThreshold float64

	// Thresholds are the TASK-GRA12/13/14/15 per-mode relevance floors. The zero
	// value uses the built-in defaults.
	Thresholds config.GraphEvalModeThresholds

	// RequiredConsecutivePasses, when > 1, requires that many passing runs on
	// distinct source versions before allowing. Values < 1 mean a single fresh
	// passing run allows. Any failing run resets the counter.
	RequiredConsecutivePasses int

	// Previous is the prior gate state, used for consecutive-run accounting. nil
	// starts the counter from zero.
	Previous *GraphDefaultGateState

	// Now is the evaluation timestamp. The zero value falls back to the report's
	// run timestamp so the function stays deterministic and clock-free.
	Now time.Time
}

// GraphDefaultGateState is the policy outcome and the schema for any future
// persisted gate state. It is JSON-serialisable so a runtime integration can
// store it at a config-owned path (graph.default_policy_state_path) without
// schema drift.
type GraphDefaultGateState struct {
	Decision                 GraphDefaultDecision `json:"decision"`
	AllowDefaultAugmentation bool                 `json:"allow_default_augmentation"`
	Recommendation           string               `json:"recommendation"`
	SourceVersion            string               `json:"source_version,omitempty"`
	MeasuredTool             string               `json:"measured_tool,omitempty"`
	GraphToolMeasured        bool                 `json:"graph_tool_measured"`
	ReportPath               string               `json:"report_path,omitempty"`
	// FailingModes lists the query modes whose relevance floors were unmet.
	FailingModes []string `json:"failing_modes,omitempty"`
	// FailingMetrics is a human-readable detail list mixing two categories:
	// per-mode relevance shortfalls (e.g. "find_references precision_at_10 0.50 <
	// 0.70") and quality-contract failure details (e.g. "case q1 (...): ..."). Use
	// Reasons to tell the categories apart; FailingMetrics is for display only.
	FailingMetrics               []string                      `json:"failing_metrics,omitempty"`
	BlockingDegradationRate      float64                       `json:"blocking_degradation_rate"`
	BlockingDegradationThreshold float64                       `json:"blocking_degradation_threshold"`
	DegradationBreakdown         map[GraphDegradationLabel]int `json:"degradation_breakdown,omitempty"`
	// PassingSourceVersions is the set of distinct source versions that have
	// passed in sequence (reset on any failing run). ConsecutivePasses is its
	// length — the distinct-version pass count used for the re-enable threshold.
	PassingSourceVersions []string  `json:"passing_source_versions,omitempty"`
	ConsecutivePasses     int       `json:"consecutive_passes"`
	EvaluatedAt           time.Time `json:"evaluated_at,omitempty"`
	Reasons               []string  `json:"reasons,omitempty"`
}

// EvaluateGraphDefaultPolicy applies the graph-default regression guard to a
// direct graph eval report and returns the gate state. It is pure: it reads the
// report and options and returns a decision, with no I/O or mutation.
func EvaluateGraphDefaultPolicy(report *DirectGraphEvalReport, opts GraphDefaultPolicyOptions) GraphDefaultGateState {
	blockThreshold := opts.BlockingDegradationThreshold
	if blockThreshold <= 0 {
		blockThreshold = config.DefaultEvalGraphBlockingDegradationThreshold
	}
	thresholds := opts.Thresholds
	if thresholds == (config.GraphEvalModeThresholds{}) {
		thresholds = defaultGraphModeThresholds()
	}

	state := GraphDefaultGateState{
		Decision:                     GraphDefaultBlocked,
		AllowDefaultAugmentation:     false,
		Recommendation:               GraphRecommendationDefer,
		BlockingDegradationThreshold: blockThreshold,
		EvaluatedAt:                  opts.Now,
	}

	// 1. No evidence at all: fail closed.
	if report == nil {
		state.Reasons = []string{graphDefaultReasonNoReport}
		return state
	}

	state.MeasuredTool = report.MeasuredTool
	state.GraphToolMeasured = report.GraphToolMeasured
	state.SourceVersion = report.Run.GitSHA
	state.ReportPath = reportPath(report)
	state.BlockingDegradationRate = report.Summary.DegradationBlockingRate
	state.DegradationBreakdown = copyDegradationBreakdown(report.Degradation.BlockingByLabel)
	if state.EvaluatedAt.IsZero() {
		state.EvaluatedAt = report.Run.Timestamp
	}

	var reasons []string
	recommendation := GraphRecommendationKeep

	// 2. Measurement honesty (TASK-GRA11): an unmeasured tool cannot certify
	// quality. Re-derive the reason instead of trusting the precomputed flag, so a
	// malformed report (flag set but no selected/servable cases) still fails closed.
	measurementReason := directGraphMeasurementReason(report)
	if !report.GraphToolMeasured || measurementReason != "" {
		explain := report.UnmeasuredReason
		if explain == "" {
			explain = measurementReason
		}
		detail := graphDefaultReasonNotMeasured
		if explain != "" {
			detail = fmt.Sprintf("%s: %s", graphDefaultReasonNotMeasured, explain)
		}
		reasons = append(reasons, detail)
		recommendation = strongerGraphRecommendation(recommendation, GraphRecommendationDefer)
	}

	// 3. Source-version freshness: a default-enable decision needs evidence for a
	// known current source version. Without one, freshness is uncertifiable, so the
	// policy defers rather than allowing a possibly-stale healthy report.
	switch {
	case opts.CurrentSourceVersion == "":
		reasons = append(reasons, graphDefaultReasonNoCurrentVer)
		recommendation = strongerGraphRecommendation(recommendation, GraphRecommendationDefer)
	case report.Run.GitSHA != opts.CurrentSourceVersion:
		reasons = append(reasons, fmt.Sprintf("%s: evidence source %q != current %q",
			graphDefaultReasonStaleSource, report.Run.GitSHA, opts.CurrentSourceVersion))
		recommendation = strongerGraphRecommendation(recommendation, GraphRecommendationDefer)
	}

	// 4. Per-mode relevance thresholds (TASK-GRA12/13/14/15).
	if metricFailures := graphModeThresholdFailures(report, thresholds); len(metricFailures) > 0 {
		state.FailingMetrics = append(state.FailingMetrics, metricFailures...)
		state.FailingModes = failingGraphModes(report, thresholds)
		reasons = append(reasons, graphDefaultReasonModeThreshold)
		recommendation = strongerGraphRecommendation(recommendation, GraphRecommendationTune)
	}

	// 5. Hard contract failures (transport error, disallowed status, missing warning).
	if contractFailures := qualityContractFailures(report.Queries); len(contractFailures) > 0 {
		state.FailingMetrics = append(state.FailingMetrics, contractFailures...)
		reasons = append(reasons, graphDefaultReasonContract)
		recommendation = strongerGraphRecommendation(recommendation, GraphRecommendationKill)
	}

	// 6. Blocking degradation rate (TASK-GRA16).
	if report.Summary.DegradationBlockingRate > blockThreshold {
		reasons = append(reasons, fmt.Sprintf("%s: %.2f > %.2f",
			graphDefaultReasonBlockingDeg, report.Summary.DegradationBlockingRate, blockThreshold))
		recommendation = strongerGraphRecommendation(recommendation, GraphRecommendationKill)
	}

	passedThisRun := len(reasons) == 0
	state.PassingSourceVersions = nextPassingVersions(opts.Previous, passedThisRun, report.Run.GitSHA)
	state.ConsecutivePasses = len(state.PassingSourceVersions)

	required := opts.RequiredConsecutivePasses
	if required < 1 {
		required = 1
	}

	if passedThisRun && state.ConsecutivePasses >= required {
		state.Decision = GraphDefaultAllowed
		state.AllowDefaultAugmentation = true
		state.Recommendation = GraphRecommendationKeep
		state.Reasons = []string{graphDefaultReasonPassed}
		return state
	}

	// The run cleared every quality gate but has not yet accumulated enough
	// distinct-version passes: block, but as a recoverable defer rather than a kill.
	if passedThisRun {
		reasons = append(reasons, fmt.Sprintf("%s: %d/%d distinct passing runs",
			graphDefaultReasonInsufficient, state.ConsecutivePasses, required))
		recommendation = strongerGraphRecommendation(recommendation, GraphRecommendationDefer)
	}

	state.Decision = GraphDefaultBlocked
	state.AllowDefaultAugmentation = false
	state.Recommendation = recommendation
	state.Reasons = reasons
	return state
}

// failingGraphModes returns the modes whose configured relevance floors were not
// met, in deterministic order.
func failingGraphModes(report *DirectGraphEvalReport, thresholds config.GraphEvalModeThresholds) []string {
	var modes []string
	for _, mode := range sortedGraphModes(report.ByMode) {
		floor := graphModeThreshold(mode, thresholds)
		if met, _ := evaluateGraphModeThreshold(mode, report.ByMode[mode], floor); !met {
			modes = append(modes, mode)
		}
	}
	return modes
}

// nextPassingVersions accumulates the SET of distinct source versions that have
// passed in sequence. A failing run resets the set to empty; a passing run adds
// its source version only if not already present, so revisiting an earlier
// version (e.g. A -> B -> A) contributes no new distinct evidence. The length of
// the set is the distinct-version pass count used for the re-enable threshold.
func nextPassingVersions(prev *GraphDefaultGateState, passed bool, sourceVersion string) []string {
	if !passed {
		return nil
	}
	var versions []string
	if prev != nil {
		versions = append(versions, prev.PassingSourceVersions...)
	}
	if sourceVersion != "" {
		present := false
		for _, v := range versions {
			if v == sourceVersion {
				present = true
				break
			}
		}
		if !present {
			versions = append(versions, sourceVersion)
		}
	}
	return versions
}

// looksLikeGraphDefaultEnableField reports whether a struct field name or tag
// looks like a switch that enables default graph augmentation. It is an
// order-independent token check (graph AND default AND (enable OR augment)) used
// by the TASK-GRA17 regression tripwire to catch a future unguarded enable path.
func looksLikeGraphDefaultEnableField(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "graph") && strings.Contains(l, "default") &&
		(strings.Contains(l, "augment") || strings.Contains(l, "enabl"))
}

func reportPath(report *DirectGraphEvalReport) string {
	if report.OutputPaths.JSON != "" {
		return report.OutputPaths.JSON
	}
	return report.OutputPaths.Markdown
}

func copyDegradationBreakdown(in map[GraphDegradationLabel]int) map[GraphDegradationLabel]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[GraphDegradationLabel]int, len(in))
	for label, count := range in {
		out[label] = count
	}
	return out
}
