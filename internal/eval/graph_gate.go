package eval

import "sort"

const (
	graphBaselineSource       = "sprint13_f37_static_floors"
	graphTargetRecallAt10Lift = 0.15
	graphKillRecallAt10Lift   = 0.10
	graphLowBaselineThreshold = 0.10
	graphLowBaselineFloor     = 0.20
	graphRecommendationTarget = "default_graph_augmented_search"
	graphEvaluationScope      = "search_engine_graph_heavy_classes"
	graphMeasuredTool         = "search_engine"
	graphToolUnmeasuredReason = "graph.query tool quality is not measured by this gate"
)

var graphHeavyClasses = []string{
	"caller_callee",
	"impact_analysis",
	"test_to_implementation",
	"adr_to_code",
	"cross_file_subsystem",
}

var exactLookupClasses = []string{
	"exact_identifier",
	"path_lookup",
	"quoted_string",
}

var ordinaryClasses = []string{
	"config_error",
	"natural_language_intent",
	"docs_to_code",
	"negative_adversarial",
}

var graphBaselineRecallAt10Floors = map[string]float64{
	"caller_callee":          0.25,
	"impact_analysis":        0.00,
	"test_to_implementation": 0.00,
	"adr_to_code":            0.25,
	"cross_file_subsystem":   0.50,
}

func evalClassGroups() ClassGroups {
	return ClassGroups{
		GraphHeavy:  append([]string(nil), graphHeavyClasses...),
		ExactLookup: append([]string(nil), exactLookupClasses...),
		Ordinary:    append([]string(nil), ordinaryClasses...),
	}
}

func isGraphHeavyClass(class string) bool {
	_, ok := graphBaselineRecallAt10Floors[class]
	return ok
}

func isExactRegressionQuery(query Query) bool {
	return (query.Class == "exact_identifier" && query.Job == "exact_lookup") ||
		query.Class == "path_lookup" ||
		query.Class == "quoted_string"
}

func buildGraphEvalGate(results []QueryResult, exactGate ExactLookupGate, baselineSource string) GraphEvalGate {
	if baselineSource == "" {
		baselineSource = graphBaselineSource
	}
	graphResults := graphQueryResults(results)
	gate := GraphEvalGate{
		Required:                 len(graphResults) > 0,
		Compared:                 len(graphResults) > 0,
		Passed:                   true,
		Recommendation:           GraphRecommendationKeep,
		RecommendationTarget:     graphRecommendationTarget,
		EvaluationScope:          graphEvaluationScope,
		MeasuredTool:             graphMeasuredTool,
		GraphToolMeasured:        false,
		Reasons:                  []string{graphToolUnmeasuredReason},
		BaselineSource:           baselineSource,
		TargetRecallAt10Delta:    graphTargetRecallAt10Lift,
		KillRecallAt10Delta:      graphKillRecallAt10Lift,
		LowBaselineThreshold:     graphLowBaselineThreshold,
		LowBaselineAbsoluteFloor: graphLowBaselineFloor,
		CurrentQueryCount:        len(graphResults),
		TokenMetrics:             graphTokenMetrics(graphResults),
		Classes:                  make(map[string]GraphClassGate),
	}
	if len(graphResults) == 0 {
		gate.Compared = false
		gate.Passed = true
		gate.Recommendation = GraphRecommendationDefer
		gate.Reasons = []string{graphToolUnmeasuredReason, "no graph-heavy queries selected"}
		return gate
	}

	byClass := make(map[string][]QueryResult)
	for _, result := range graphResults {
		byClass[result.Class] = append(byClass[result.Class], result)
	}
	for _, class := range graphHeavyClasses {
		classResults := byClass[class]
		if len(classResults) == 0 {
			continue
		}
		classGate := buildGraphClassGate(class, classResults)
		gate.Classes[class] = classGate
		if !classGate.Passed {
			gate.Passed = false
			for _, reason := range classGate.Reasons {
				gate.Failures = append(gate.Failures, GraphEvalGateFailure{
					Class:                   class,
					Reason:                  reason,
					BaselineRecallAt10Floor: classGate.BaselineRecallAt10Floor,
					CurrentRecallAt10:       classGate.CurrentRecallAt10,
					RecallAt10Delta:         classGate.RecallAt10Delta,
				})
			}
		}
		gate.Recommendation = strongerGraphRecommendation(gate.Recommendation, classGate.Recommendation)
	}

	if exactGate.Required && !exactGate.Passed {
		gate.Passed = false
		gate.Recommendation = GraphRecommendationKill
		gate.Reasons = appendUniqueString(gate.Reasons, "exact lookup gate failed")
	}
	if !gate.Passed {
		for _, reason := range graphGateReasons(gate.Failures) {
			gate.Reasons = appendUniqueString(gate.Reasons, reason)
		}
	}
	return gate
}

func buildGraphClassGate(class string, results []QueryResult) GraphClassGate {
	metrics := calculateMetrics(results)
	baseline := graphBaselineRecallAt10Floors[class]
	delta := metrics.RecallAt10 - baseline
	gate := GraphClassGate{
		QueryCount:               len(results),
		BaselineRecallAt10Floor:  baseline,
		CurrentRecallAt10:        metrics.RecallAt10,
		RecallAt10Delta:          delta,
		TargetRecallAt10Delta:    graphTargetRecallAt10Lift,
		KillRecallAt10Delta:      graphKillRecallAt10Lift,
		LowBaselineAbsoluteFloor: graphLowBaselineFloor,
		Passed:                   true,
		Recommendation:           GraphRecommendationKeep,
	}
	switch {
	case delta < graphKillRecallAt10Lift:
		gate.Passed = false
		gate.Recommendation = GraphRecommendationKill
		gate.Reasons = append(gate.Reasons, "recall@10 lift below 10pp kill threshold")
	case baseline <= graphLowBaselineThreshold && metrics.RecallAt10 < graphLowBaselineFloor:
		gate.Passed = false
		gate.Recommendation = GraphRecommendationDefer
		gate.Reasons = append(gate.Reasons, "current recall@10 below low-baseline absolute floor")
	case delta < graphTargetRecallAt10Lift:
		gate.Passed = false
		gate.Recommendation = GraphRecommendationTune
		gate.Reasons = append(gate.Reasons, "recall@10 lift below 15pp target")
	}
	return gate
}

func graphQueryResults(results []QueryResult) []QueryResult {
	out := make([]QueryResult, 0, len(results))
	for _, result := range results {
		if isGraphHeavyClass(result.Class) {
			out = append(out, result)
		}
	}
	return out
}

func graphTokenMetrics(results []QueryResult) GraphTokenMetrics {
	stats := GraphTokenMetrics{Count: len(results)}
	samples := make([]float64, 0, len(results))
	total := 0.0
	for _, result := range results {
		if len(result.TopResults) == 0 {
			stats.ZeroResultCount++
			continue
		}
		total += result.TokenEstimate.TokensPerResult
		samples = append(samples, result.TokenEstimate.TokensPerResult)
	}
	if len(samples) > 0 {
		stats.MeanTokensPerResult = total / float64(len(samples))
		sort.Float64s(samples)
		stats.P95TokensPerResult = percentileFloat(samples, 0.95)
	}
	return stats
}

func graphGateReasons(failures []GraphEvalGateFailure) []string {
	reasons := make([]string, 0, len(failures))
	for _, failure := range failures {
		reasons = appendUniqueString(reasons, failure.Reason)
	}
	return reasons
}

func strongerGraphRecommendation(current, next string) string {
	if graphRecommendationRank(next) > graphRecommendationRank(current) {
		return next
	}
	return current
}

func graphRecommendationRank(recommendation string) int {
	switch recommendation {
	case GraphRecommendationKill:
		return 4
	case GraphRecommendationDefer:
		return 3
	case GraphRecommendationTune:
		return 2
	case GraphRecommendationKeep:
		return 1
	default:
		return 0
	}
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
