package eval

import (
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/graph"
)

// fixedPolicyTime is a deterministic timestamp for policy tests (no time.Now()).
var fixedPolicyTime = time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)

// passingDirectGraphReport builds a healthy, fully-measured direct graph eval
// report whose per-mode relevance metrics clear every default floor and whose
// blocking degradation rate is zero. Tests mutate the returned value to drive a
// single failing condition at a time.
func passingDirectGraphReport(sourceVersion string) *DirectGraphEvalReport {
	return &DirectGraphEvalReport{
		SchemaVersion:     GraphCorpusSchemaVersion,
		ReportType:        DirectGraphReportType,
		MeasuredTool:      DirectGraphMeasuredTool,
		EvaluationScope:   DirectGraphEvaluationScope,
		GraphToolMeasured: true,
		Run: DirectGraphRunMetadata{
			Timestamp: fixedPolicyTime,
			GitSHA:    sourceVersion,
			Command:   "amanmcp eval graph --subset quick",
			Output:    "both",
		},
		Summary: DirectGraphSummary{
			QueryCount:              9,
			MeasuredQueryCount:      9,
			PassCount:               9,
			PassRate:                1.0,
			DegradationBlockingRate: 0.0,
		},
		ByMode: map[string]DirectGraphModeSummary{
			graph.QueryModeFindReferences: {
				QueryCount: 3, MeasuredCount: 3, PassCount: 3, QualityCount: 3,
				ExpectedRecallAt10: 0.90, PrecisionAt10: 0.90, ThresholdMet: true,
			},
			graph.QueryModeExplainSymbol: {
				QueryCount: 3, MeasuredCount: 3, PassCount: 3, QualityCount: 3,
				HitRateAt3: 0.90, ThresholdMet: true,
			},
			graph.QueryModeImpactAnalysis: {
				QueryCount: 3, MeasuredCount: 3, PassCount: 3, QualityCount: 3,
				HitRateAt10: 0.80, ThresholdMet: true,
			},
		},
		Degradation: DirectGraphDegradationSummary{
			BlockingRate:    0.0,
			BlockingByLabel: map[GraphDegradationLabel]int{},
		},
		// Healthy, scored quality-class cases so the passing path actually runs
		// qualityContractFailures over real data (not an empty slice).
		Queries: []DirectGraphQueryResult{
			{ID: "fr-1", Mode: graph.QueryModeFindReferences, ExpectationClass: GraphExpectationClassQuality, Status: graph.GraphStatusFresh, Scored: true, Passed: true},
			{ID: "es-1", Mode: graph.QueryModeExplainSymbol, ExpectationClass: GraphExpectationClassQuality, Status: graph.GraphStatusFresh, Scored: true, Passed: true},
			{ID: "ia-1", Mode: graph.QueryModeImpactAnalysis, ExpectationClass: GraphExpectationClassQuality, Status: graph.GraphStatusFresh, Scored: true, Passed: true},
		},
		OutputPaths: OutputPaths{JSON: ".aman-pm/validation/graph-eval/latest.json"},
	}
}

func TestEvaluateGraphDefaultPolicy_NilReportFailsClosed(t *testing.T) {
	state := EvaluateGraphDefaultPolicy(nil, GraphDefaultPolicyOptions{Now: fixedPolicyTime})

	if state.Decision != GraphDefaultBlocked {
		t.Fatalf("nil report must be blocked, got %q", state.Decision)
	}
	if state.AllowDefaultAugmentation {
		t.Fatal("nil report must not allow default augmentation")
	}
	if state.Recommendation != GraphRecommendationDefer {
		t.Fatalf("nil report recommendation = %q, want %q", state.Recommendation, GraphRecommendationDefer)
	}
	if len(state.Reasons) == 0 {
		t.Fatal("blocked state must explain why")
	}
}

func TestEvaluateGraphDefaultPolicy_ZeroValueReportBlocks(t *testing.T) {
	// The zero-value report has GraphToolMeasured=false: a default-augmentation
	// enable attempt with no real evidence must fail closed.
	state := EvaluateGraphDefaultPolicy(&DirectGraphEvalReport{}, GraphDefaultPolicyOptions{Now: fixedPolicyTime})

	if state.AllowDefaultAugmentation || state.Decision != GraphDefaultBlocked {
		t.Fatalf("zero-value report must fail closed, got decision=%q allow=%v", state.Decision, state.AllowDefaultAugmentation)
	}
}

func TestEvaluateGraphDefaultPolicy_UnmeasuredBlocks(t *testing.T) {
	report := passingDirectGraphReport("sha-1")
	report.GraphToolMeasured = false
	report.UnmeasuredReason = "graph.query produced no servable output (tool not measured)"

	state := EvaluateGraphDefaultPolicy(report, GraphDefaultPolicyOptions{
		CurrentSourceVersion: "sha-1",
		Now:                  fixedPolicyTime,
	})

	if state.AllowDefaultAugmentation {
		t.Fatal("an unmeasured graph tool must not enable default augmentation")
	}
	if state.Recommendation != GraphRecommendationDefer {
		t.Fatalf("unmeasured recommendation = %q, want %q", state.Recommendation, GraphRecommendationDefer)
	}
}

func TestEvaluateGraphDefaultPolicy_StaleSourceVersionBlocks(t *testing.T) {
	report := passingDirectGraphReport("sha-old")

	state := EvaluateGraphDefaultPolicy(report, GraphDefaultPolicyOptions{
		CurrentSourceVersion: "sha-current",
		Now:                  fixedPolicyTime,
	})

	if state.AllowDefaultAugmentation {
		t.Fatal("stale eval evidence must not enable default augmentation")
	}
	if state.Recommendation != GraphRecommendationDefer {
		t.Fatalf("stale recommendation = %q, want %q", state.Recommendation, GraphRecommendationDefer)
	}
	if state.SourceVersion != "sha-old" {
		t.Fatalf("state must report the evidence source version, got %q", state.SourceVersion)
	}
}

func TestEvaluateGraphDefaultPolicy_FailingModeThresholdBlocks(t *testing.T) {
	report := passingDirectGraphReport("sha-1")
	// find_references precision@10 below the 0.70 default floor.
	fr := report.ByMode[graph.QueryModeFindReferences]
	fr.PrecisionAt10 = 0.50
	fr.ThresholdMet = false
	report.ByMode[graph.QueryModeFindReferences] = fr

	state := EvaluateGraphDefaultPolicy(report, GraphDefaultPolicyOptions{
		CurrentSourceVersion: "sha-1",
		Now:                  fixedPolicyTime,
	})

	if state.AllowDefaultAugmentation {
		t.Fatal("a failing per-mode relevance threshold must block default augmentation")
	}
	if state.Recommendation != GraphRecommendationTune {
		t.Fatalf("failing-threshold recommendation = %q, want %q", state.Recommendation, GraphRecommendationTune)
	}
	if !contains(state.FailingModes, graph.QueryModeFindReferences) {
		t.Fatalf("failing modes %v must include %q", state.FailingModes, graph.QueryModeFindReferences)
	}
	if len(state.FailingMetrics) == 0 {
		t.Fatal("failing metrics must record the specific metric below floor")
	}
}

func TestEvaluateGraphDefaultPolicy_BlockingDegradationOverThresholdBlocks(t *testing.T) {
	report := passingDirectGraphReport("sha-1")
	report.Summary.DegradationBlockingRate = 0.25
	report.Degradation.BlockingRate = 0.25
	report.Degradation.BlockingByLabel = map[GraphDegradationLabel]int{DegradationUnavailableGraph: 2}

	state := EvaluateGraphDefaultPolicy(report, GraphDefaultPolicyOptions{
		CurrentSourceVersion: "sha-1",
		Now:                  fixedPolicyTime,
	})

	if state.AllowDefaultAugmentation {
		t.Fatal("blocking degradation above threshold must block default augmentation")
	}
	if state.Recommendation != GraphRecommendationKill {
		t.Fatalf("over-threshold degradation recommendation = %q, want %q", state.Recommendation, GraphRecommendationKill)
	}
	if state.BlockingDegradationRate != 0.25 {
		t.Fatalf("state must report blocking degradation rate, got %v", state.BlockingDegradationRate)
	}
	if state.DegradationBreakdown[DegradationUnavailableGraph] != 2 {
		t.Fatalf("state must surface the blocking degradation breakdown, got %v", state.DegradationBreakdown)
	}
}

func TestEvaluateGraphDefaultPolicy_ContractFailureBlocks(t *testing.T) {
	report := passingDirectGraphReport("sha-1")
	// A quality-class case that errored is a contract failure, not a relevance
	// miss. Append it to the otherwise-healthy run so ByMode and Queries stay
	// consistent and a single bad case still blocks.
	report.Queries = append(report.Queries,
		DirectGraphQueryResult{ID: "q1", Mode: graph.QueryModeFindReferences, ExpectationClass: GraphExpectationClassQuality, Error: "graph.query transport error"},
	)

	state := EvaluateGraphDefaultPolicy(report, GraphDefaultPolicyOptions{
		CurrentSourceVersion: "sha-1",
		Now:                  fixedPolicyTime,
	})

	if state.AllowDefaultAugmentation {
		t.Fatal("a graph contract failure must block default augmentation")
	}
	if state.Recommendation != GraphRecommendationKill {
		t.Fatalf("contract-failure recommendation = %q, want %q", state.Recommendation, GraphRecommendationKill)
	}
}

func TestEvaluateGraphDefaultPolicy_FreshPassingAllows(t *testing.T) {
	report := passingDirectGraphReport("sha-current")

	state := EvaluateGraphDefaultPolicy(report, GraphDefaultPolicyOptions{
		CurrentSourceVersion: "sha-current",
		Now:                  fixedPolicyTime,
	})

	if !state.AllowDefaultAugmentation {
		t.Fatalf("fresh passing eval for the current source version must allow, reasons=%v", state.Reasons)
	}
	if state.Decision != GraphDefaultAllowed {
		t.Fatalf("decision = %q, want %q", state.Decision, GraphDefaultAllowed)
	}
	if state.Recommendation != GraphRecommendationKeep {
		t.Fatalf("recommendation = %q, want %q", state.Recommendation, GraphRecommendationKeep)
	}
	if state.ConsecutivePasses != 1 {
		t.Fatalf("consecutive passes = %d, want 1", state.ConsecutivePasses)
	}
	if state.SourceVersion != "sha-current" || !state.GraphToolMeasured {
		t.Fatalf("allowed state must carry source version + measured flag, got %+v", state)
	}
}

func TestEvaluateGraphDefaultPolicy_ConsecutivePassesRequireDistinctSourceVersions(t *testing.T) {
	opts := GraphDefaultPolicyOptions{RequiredConsecutivePasses: 2, Now: fixedPolicyTime}

	// First pass on sha-A: one passing run, not yet enough.
	optsA := opts
	optsA.CurrentSourceVersion = "sha-A"
	first := EvaluateGraphDefaultPolicy(passingDirectGraphReport("sha-A"), optsA)
	if first.AllowDefaultAugmentation {
		t.Fatal("a single passing run must not satisfy a 2-run requirement")
	}
	if first.ConsecutivePasses != 1 {
		t.Fatalf("first consecutive passes = %d, want 1", first.ConsecutivePasses)
	}

	// Second pass on the SAME source version must NOT count as a distinct run.
	optsSame := opts
	optsSame.CurrentSourceVersion = "sha-A"
	optsSame.Previous = &first
	same := EvaluateGraphDefaultPolicy(passingDirectGraphReport("sha-A"), optsSame)
	if same.AllowDefaultAugmentation {
		t.Fatal("a repeated pass on the same source version must not satisfy distinct-run requirement")
	}
	if same.ConsecutivePasses != 1 {
		t.Fatalf("same-source consecutive passes = %d, want 1 (no new distinct run)", same.ConsecutivePasses)
	}

	// Second pass on a DISTINCT source version reaches the threshold.
	optsB := opts
	optsB.CurrentSourceVersion = "sha-B"
	optsB.Previous = &first
	second := EvaluateGraphDefaultPolicy(passingDirectGraphReport("sha-B"), optsB)
	if !second.AllowDefaultAugmentation {
		t.Fatalf("two passing runs on distinct source versions must allow, reasons=%v", second.Reasons)
	}
	if second.ConsecutivePasses != 2 {
		t.Fatalf("distinct-source consecutive passes = %d, want 2", second.ConsecutivePasses)
	}
}

func TestEvaluateGraphDefaultPolicy_FailingRunResetsConsecutiveCounter(t *testing.T) {
	prior := GraphDefaultGateState{
		Decision:                 GraphDefaultAllowed,
		AllowDefaultAugmentation: true,
		SourceVersion:            "sha-A",
		ConsecutivePasses:        2,
	}
	report := passingDirectGraphReport("sha-B")
	report.Summary.DegradationBlockingRate = 0.5 // this run fails

	state := EvaluateGraphDefaultPolicy(report, GraphDefaultPolicyOptions{
		CurrentSourceVersion:      "sha-B",
		RequiredConsecutivePasses: 2,
		Previous:                  &prior,
		Now:                       fixedPolicyTime,
	})

	if state.AllowDefaultAugmentation {
		t.Fatal("a failing run must block regardless of prior consecutive passes")
	}
	if state.ConsecutivePasses != 0 {
		t.Fatalf("a failing run must reset the consecutive counter to 0, got %d", state.ConsecutivePasses)
	}
}

func TestEvaluateGraphDefaultPolicy_ExplicitGraphQueryUnaffected(t *testing.T) {
	// TASK-GRA17 non-goal: the policy governs DEFAULT augmentation only. Even a
	// hard block must not signal disabling explicit graph.query — the gate state
	// exposes no such control surface, only AllowDefaultAugmentation.
	report := passingDirectGraphReport("sha-1")
	report.Summary.DegradationBlockingRate = 0.9
	report.Degradation.BlockingRate = 0.9

	state := EvaluateGraphDefaultPolicy(report, GraphDefaultPolicyOptions{
		CurrentSourceVersion: "sha-1",
		Now:                  fixedPolicyTime,
	})

	if state.AllowDefaultAugmentation {
		t.Fatal("precondition: this scenario must block default augmentation")
	}
	if state.MeasuredTool != DirectGraphMeasuredTool {
		t.Fatalf("blocked state must still report the measured tool %q, got %q", DirectGraphMeasuredTool, state.MeasuredTool)
	}
	// Structural guard: the gate-state type must not grow a field that disables
	// explicit graph.query. Only default-augmentation control is permitted.
	st := reflect.TypeOf(GraphDefaultGateState{})
	for i := 0; i < st.NumField(); i++ {
		name := st.Field(i).Name
		if regexp.MustCompile(`(?i)disable|explicit`).MatchString(name) {
			t.Fatalf("GraphDefaultGateState.%s suggests control over explicit graph.query, which TASK-GRA17 forbids", name)
		}
	}
}

// TestNoUnguardedGraphDefaultEnablePath is the regression tripwire required by
// TASK-GRA17: today there is no config switch that enables default graph
// augmentation, so no path can bypass EvaluateGraphDefaultPolicy. If a future
// change adds such a switch, this test fails and forces the author to wire the
// gate and update docs/reference/architecture/graph-default-policy.md.
func TestNoUnguardedGraphDefaultEnablePath(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var scan func(rt reflect.Type, path string)
	scan = func(rt reflect.Type, path string) {
		if rt.Kind() == reflect.Ptr {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] {
			return
		}
		seen[rt] = true
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if looksLikeGraphDefaultEnableField(f.Name) || looksLikeGraphDefaultEnableField(string(f.Tag)) {
				t.Fatalf("config field %s.%s looks like a default-graph-augmentation switch; "+
					"TASK-GRA17 requires routing it through EvaluateGraphDefaultPolicy and updating "+
					"docs/reference/architecture/graph-default-policy.md", path, f.Name)
			}
			ft := f.Type
			if ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				scan(ft, path+"."+f.Name)
			}
		}
	}
	scan(reflect.TypeOf(config.Config{}), "config.Config")
}

func TestEvaluateGraphDefaultPolicy_MeasuredFlagButMalformedReportBlocks(t *testing.T) {
	// Defense in depth (Codex P1-1): a report can carry graph_tool_measured=true
	// while being structurally unmeasured (no selected/servable cases). The policy
	// must re-derive measurement honesty, not trust the precomputed flag.
	report := passingDirectGraphReport("sha-1")
	report.GraphToolMeasured = true // flag claims measured...
	report.Summary.QueryCount = 0   // ...but nothing was selected/measured
	report.Summary.MeasuredQueryCount = 0

	state := EvaluateGraphDefaultPolicy(report, GraphDefaultPolicyOptions{
		CurrentSourceVersion: "sha-1",
		Now:                  fixedPolicyTime,
	})

	if state.AllowDefaultAugmentation {
		t.Fatal("a structurally unmeasured report must block even if graph_tool_measured=true")
	}
	if state.Recommendation != GraphRecommendationDefer {
		t.Fatalf("malformed-measurement recommendation = %q, want %q", state.Recommendation, GraphRecommendationDefer)
	}
}

func TestEvaluateGraphDefaultPolicy_EmptyCurrentSourceVersionBlocks(t *testing.T) {
	// Codex P1-2: freshness cannot be certified without a current source version,
	// so a healthy report must not reach allowed when CurrentSourceVersion is empty.
	report := passingDirectGraphReport("sha-1")

	state := EvaluateGraphDefaultPolicy(report, GraphDefaultPolicyOptions{
		Now: fixedPolicyTime, // CurrentSourceVersion deliberately empty
	})

	if state.AllowDefaultAugmentation {
		t.Fatal("an unspecified current source version must block (freshness uncertifiable)")
	}
	if state.Recommendation != GraphRecommendationDefer {
		t.Fatalf("empty-source recommendation = %q, want %q", state.Recommendation, GraphRecommendationDefer)
	}
}

func TestEvaluateGraphDefaultPolicy_ConsecutiveRequiresDistinctVersionSet(t *testing.T) {
	// Codex P1-3: distinctness is over the full set of passing versions, not just
	// the adjacent prior one. A -> B -> A must count 2 distinct versions, not 3.
	opts := GraphDefaultPolicyOptions{RequiredConsecutivePasses: 3, Now: fixedPolicyTime}

	optsA := opts
	optsA.CurrentSourceVersion = "sha-A"
	run1 := EvaluateGraphDefaultPolicy(passingDirectGraphReport("sha-A"), optsA)
	if run1.ConsecutivePasses != 1 {
		t.Fatalf("run1 consecutive = %d, want 1", run1.ConsecutivePasses)
	}

	optsB := opts
	optsB.CurrentSourceVersion = "sha-B"
	optsB.Previous = &run1
	run2 := EvaluateGraphDefaultPolicy(passingDirectGraphReport("sha-B"), optsB)
	if run2.ConsecutivePasses != 2 {
		t.Fatalf("run2 consecutive = %d, want 2", run2.ConsecutivePasses)
	}

	// Third run revisits sha-A: no NEW distinct version, so the count must stay 2.
	optsA2 := opts
	optsA2.CurrentSourceVersion = "sha-A"
	optsA2.Previous = &run2
	run3 := EvaluateGraphDefaultPolicy(passingDirectGraphReport("sha-A"), optsA2)
	if run3.ConsecutivePasses != 2 {
		t.Fatalf("A->B->A consecutive = %d, want 2 (only 2 distinct versions)", run3.ConsecutivePasses)
	}
	if run3.AllowDefaultAugmentation {
		t.Fatal("two distinct versions must not satisfy a 3-distinct-version requirement")
	}
}

func TestLooksLikeGraphDefaultEnableField(t *testing.T) {
	catches := []string{
		"GraphDefaultEnabled", "DefaultGraphAugmentation", "GraphAugmentationDefault",
		"DefaultEnableGraph", "EnableDefaultGraphAugmentation",
	}
	for _, name := range catches {
		if !looksLikeGraphDefaultEnableField(name) {
			t.Errorf("expected %q to be flagged as a graph-default-enable field", name)
		}
	}
	safe := []string{
		"GraphEvalModeThresholds", "FindReferences", "MinPrecisionAt10",
		"BlockingDegradationThreshold", "Graph", "DefaultEvalGraphMode",
		"BM25Weight", "SemanticWeight",
	}
	for _, name := range safe {
		if looksLikeGraphDefaultEnableField(name) {
			t.Errorf("did not expect %q to be flagged", name)
		}
	}
}

func contains(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}
