package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/graph"
)

const maxGraphStatusExtractorSamples = 10

type GraphOptions struct {
	CorpusPath                             string
	Subset                                 string
	Output                                 string
	OutDir                                 string
	FailOnRegression                       bool
	Command                                string
	BlockingDegradationThreshold           float64
	BlockingDegradationThresholdConfigured bool
	// ModeThresholds carries the per-mode relevance floors enforced by the gate
	// (TASK-GRA12/13/14). The cmd layer loads these from config; the runner
	// defaults them when unset so direct callers still gate consistently.
	ModeThresholds config.GraphEvalModeThresholds
}

type DirectGraphClient interface {
	SnapshotGraph(context.Context) (*graph.StatusSnapshot, error)
	QueryGraph(context.Context, GraphQuery) (DirectGraphToolOutput, error)
}

type DirectGraphToolOutput = graph.QueryToolOutput

type DirectGraphRunner struct {
	client DirectGraphClient
}

func NewDirectGraphRunner(client DirectGraphClient) *DirectGraphRunner {
	return &DirectGraphRunner{client: client}
}

func (r *DirectGraphRunner) Run(ctx context.Context, opts GraphOptions) (*DirectGraphEvalReport, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("direct graph client is required")
	}
	opts = normalizeGraphOptions(opts)
	if err := validateGraphOutput(opts.Output); err != nil {
		return nil, err
	}
	if err := validateGraphBlockingThreshold(opts.BlockingDegradationThreshold); err != nil {
		return nil, err
	}
	if closer, ok := r.client.(interface{ Close() error }); ok {
		defer func() { _ = closer.Close() }()
	}

	corpus, err := LoadGraphCorpus(opts.CorpusPath)
	if err != nil {
		return nil, err
	}
	queries, err := SelectGraphQueries(corpus.Queries, GraphSelection{Subset: opts.Subset})
	if err != nil {
		return nil, err
	}
	if len(queries) == 0 {
		return nil, fmt.Errorf("graph subset %q selected no queries", opts.Subset)
	}
	if preparer, ok := r.client.(preparer); ok {
		if err := preparer.Prepare(ctx); err != nil {
			return nil, err
		}
	}

	statusSnapshots := make([]GraphStatusSnapshotBrief, 0, 2)
	before, err := r.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	statusSnapshots = append(statusSnapshots, before)

	results := make([]DirectGraphQueryResult, 0, len(queries))
	for _, query := range queries {
		results = append(results, r.runQuery(ctx, query))
	}

	after, err := r.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(before, after) {
		statusSnapshots = append(statusSnapshots, after)
	}

	report := buildDirectGraphReport(opts, results, statusSnapshots)
	if err := writeDirectGraphReport(opts, report); err != nil {
		return report, err
	}
	if opts.FailOnRegression {
		// (1) Measurement truth and (2) blocking degradation are preserved from
		// TASK-GRA11 and run first: an unmeasured or unhealthy graph must fail
		// before any relevance judgement.
		if !report.GraphToolMeasured {
			return report, fmt.Errorf("direct graph eval gate failed: %s", report.UnmeasuredReason)
		}
		if report.Summary.DegradationBlockingRate > opts.BlockingDegradationThreshold {
			return report, fmt.Errorf("direct graph eval gate failed: blocking degradation rate %.2f exceeds %.2f",
				report.Summary.DegradationBlockingRate,
				opts.BlockingDegradationThreshold,
			)
		}
		// (3) Quality-class contract failures (transport error, disallowed
		// status, missing warnings) are hard failures regardless of aggregates.
		if failures := qualityContractFailures(results); len(failures) > 0 {
			return report, fmt.Errorf("direct graph eval gate failed: quality contract failures: %s",
				strings.Join(failures, "; "))
		}
		// (4) Per-mode relevance thresholds (TASK-GRA12/13/14). Degraded/gap
		// cases are excluded from these aggregates.
		if failures := graphModeThresholdFailures(report, opts.ModeThresholds); len(failures) > 0 {
			return report, fmt.Errorf("direct graph eval gate failed: relevance thresholds not met: %s",
				strings.Join(failures, "; "))
		}
	}
	return report, nil
}

func (r *DirectGraphRunner) snapshot(ctx context.Context) (GraphStatusSnapshotBrief, error) {
	snapshot, err := r.client.SnapshotGraph(ctx)
	if err != nil {
		return GraphStatusSnapshotBrief{}, fmt.Errorf("read graph status snapshot: %w", err)
	}
	return briefGraphStatusSnapshot(snapshot), nil
}

func (r *DirectGraphRunner) runQuery(ctx context.Context, query GraphQuery) DirectGraphQueryResult {
	start := time.Now()
	output, err := r.client.QueryGraph(ctx, query)
	latency := time.Since(start)
	result := DirectGraphQueryResult{
		ID:               query.ID,
		Name:             query.Name,
		Mode:             query.Mode,
		Query:            query.Query,
		ExpectationClass: graphExpectationClassOrDefault(query.ExpectationClass),
		Status:           output.Status,
		Available:        output.Available,
		Degraded:         output.Degraded,
		Warnings:         append([]graph.StatusWarning(nil), output.Warnings...),
		WarningCodes:     graphWarningCodes(output.Warnings),
		Results:          append([]graph.QueryResult(nil), output.Results...),
		ResultCount:      len(output.Results),
		LatencyMs:        latency.Milliseconds(),
	}
	if err != nil {
		result.Error = err.Error()
		result.FailureReason = err.Error()
	}
	// Degradation labels (TASK-GRA16) are computed for every path — including
	// transport errors and non-servable statuses — so empty/unavailable/failed/
	// invalid cases are categorized even though they never reach relevance
	// scoring. BlockingDegradation is then derived from the matrix-blocking subset.
	result.DegradationLabels = graphDegradationLabels(graphDegradationInput{
		Status:         output.Status,
		WarningCodes:   result.WarningCodes,
		StaleResults:   anyStaleGraphResult(output.Results),
		Language:       query.Metadata["language"],
		InvalidParams:  err != nil && isGraphInvalidParamsError(err),
		TransportError: err != nil,
	})
	result.BlockingDegradationLabels = graphBlockingDegradationLabels(result.DegradationLabels, output.Status, result.ResultCount, query)
	result.BlockingDegradation = len(result.BlockingDegradationLabels) > 0
	if err != nil {
		return result
	}
	if isBlockingGraphStatus(query.Degradation.BlockingStatuses, output.Status) {
		result.FailureReason = fmt.Sprintf("blocking graph status %s", output.Status)
		return result
	}
	if !isAllowedGraphStatus(query.Degradation.AllowedStatuses, output.Status) {
		result.FailureReason = fmt.Sprintf("disallowed graph status %s", output.Status)
		return result
	}
	if missing := missingGraphWarningCodes(query.Degradation.ExpectedWarningCodes, result.WarningCodes); len(missing) > 0 {
		result.FailureReason = fmt.Sprintf("missing expected warning codes: %s", strings.Join(graphWarningCodeStrings(missing), ", "))
		return result
	}
	// Guard the precision dedup invariant before scoring: if results sharing a
	// per-mode identity disagree on relevance, precision@K is order-dependent and
	// must not be trusted. Treat it as a contract failure (Scored stays false, so
	// the case is excluded from quality aggregates) rather than emitting a
	// silently-misattributed precision number (DEBT-037 finding #2).
	if ambiguous := graphPrecisionIdentityAmbiguity(output.Results, query.Expected, query.AcceptedAlternatives, query.Mode); ambiguous != "" {
		result.FailureReason = fmt.Sprintf("precision identity ambiguity: results sharing identity %q disagree on relevance, so precision@K is order-dependent; make the matcher discriminate on an identity field (source_path/node_kind/relation/role, plus node_id for impact_analysis) or relax it to match the whole bucket (DEBT-037)", ambiguous)
		return result
	}
	result.MatchedEvidenceCount = matchedGraphEvidenceCount(query.Expected, output.Results)
	// Rank-aware metrics are additive: MatchedEvidenceCount/Passed keep their
	// original "matched anywhere in results" contract (GRA11), while the @k
	// metrics measure relevance within the fixed rank window (GRA12/13/14).
	metrics := scoreGraphCase(query.Expected, query.AcceptedAlternatives, output.Results, query.Mode)
	result.Scored = true
	result.MatchedPositions = metrics.MatchedPositions
	result.WindowSize = metrics.WindowSize
	result.UniqueResultCount = metrics.UniqueResultCount
	result.ExpectedRecallAt10 = metrics.ExpectedRecallAt10
	result.PrecisionAt3 = metrics.PrecisionAt3
	result.PrecisionAt5 = metrics.PrecisionAt5
	result.PrecisionAt10 = metrics.PrecisionAt10
	result.HitAt3 = metrics.HitAt3
	result.HitAt10 = metrics.HitAt10
	result.Passed = result.MatchedEvidenceCount == len(query.Expected)
	if !result.Passed {
		result.FailureReason = fmt.Sprintf("matched %d/%d expected evidence", result.MatchedEvidenceCount, len(query.Expected))
	}
	return result
}

type SQLiteDirectGraphClient struct {
	dataDir   string
	projectID string
	repo      graph.Repository
	service   *graph.QueryService
}

func NewSQLiteDirectGraphClient(dataDir, projectID string) *SQLiteDirectGraphClient {
	return &SQLiteDirectGraphClient{dataDir: dataDir, projectID: projectID}
}

func (c *SQLiteDirectGraphClient) Prepare(context.Context) error {
	if c == nil {
		return fmt.Errorf("direct graph client is required")
	}
	if strings.TrimSpace(c.dataDir) == "" {
		return fmt.Errorf("graph data directory is required")
	}
	if strings.TrimSpace(c.projectID) == "" {
		return fmt.Errorf("project id is required")
	}
	if c.repo != nil {
		return nil
	}
	repo, err := graph.OpenSQLiteRepository(filepath.Join(c.dataDir, "graph.db"))
	if err != nil {
		return fmt.Errorf("open graph repository: %w", err)
	}
	c.repo = repo
	c.service = graph.NewQueryService(repo, graph.QueryServiceOptions{})
	return nil
}

func (c *SQLiteDirectGraphClient) SnapshotGraph(ctx context.Context) (*graph.StatusSnapshot, error) {
	if err := c.Prepare(ctx); err != nil {
		return nil, err
	}
	snapshot, err := c.repo.Snapshot(ctx, graph.StatusOptions{
		ProjectID: c.projectID,
		Now:       time.Now().UTC(),
	})
	if err != nil {
		return nil, fmt.Errorf("read graph status: %w", err)
	}
	return snapshot, nil
}

func (c *SQLiteDirectGraphClient) QueryGraph(ctx context.Context, query GraphQuery) (DirectGraphToolOutput, error) {
	if err := c.Prepare(ctx); err != nil {
		return DirectGraphToolOutput{}, err
	}
	response, err := c.service.Query(ctx, graph.QueryRequest{
		ProjectID:    c.projectID,
		Mode:         query.Mode,
		Query:        query.Query,
		Limit:        query.Limit,
		IncludeStale: query.IncludeStale,
	})
	if err != nil {
		return DirectGraphToolOutput{}, err
	}
	return directGraphToolOutput(response), nil
}

func (c *SQLiteDirectGraphClient) Close() error {
	if c == nil || c.repo == nil {
		return nil
	}
	return c.repo.Close()
}

func directGraphToolOutput(response graph.QueryResponse) DirectGraphToolOutput {
	return graph.NewQueryToolOutput(response)
}

func normalizeGraphOptions(opts GraphOptions) GraphOptions {
	if opts.CorpusPath == "" {
		opts.CorpusPath = DefaultGraphCorpusPath
	}
	if opts.Subset == "" {
		opts.Subset = GraphSubsetFull
	}
	if opts.Output == "" {
		opts.Output = "both"
	}
	if opts.OutDir == "" {
		opts.OutDir = DefaultGraphOutDir
	}
	if opts.Command == "" {
		opts.Command = "amanmcp eval graph"
	}
	if opts.BlockingDegradationThreshold == 0 && !opts.BlockingDegradationThresholdConfigured {
		opts.BlockingDegradationThreshold = config.DefaultEvalGraphBlockingDegradationThreshold
	}
	if opts.ModeThresholds == (config.GraphEvalModeThresholds{}) {
		opts.ModeThresholds = defaultGraphModeThresholds()
	}
	return opts
}

// defaultGraphModeThresholds returns the built-in per-mode relevance floors,
// delegating to config so a direct runner caller that omits ModeThresholds
// enforces the same gate as the configured CLI path (single source of truth).
func defaultGraphModeThresholds() config.GraphEvalModeThresholds {
	return config.DefaultGraphEvalModeThresholds()
}

func validateGraphOutput(output string) error {
	if output != "json" && output != "markdown" && output != "both" {
		return fmt.Errorf("unsupported output %q", output)
	}
	return nil
}

func validateGraphBlockingThreshold(threshold float64) error {
	if threshold <= 0 || threshold > 1 {
		return fmt.Errorf("blocking degradation threshold must be greater than 0 and at most 1, got %.2f", threshold)
	}
	return nil
}

func buildDirectGraphReport(
	opts GraphOptions,
	results []DirectGraphQueryResult,
	statusSnapshots []GraphStatusSnapshotBrief,
) *DirectGraphEvalReport {
	summary := summarizeDirectGraph(results)
	report := &DirectGraphEvalReport{
		SchemaVersion:   GraphCorpusSchemaVersion,
		ReportType:      DirectGraphReportType,
		MeasuredTool:    DirectGraphMeasuredTool,
		EvaluationScope: DirectGraphEvaluationScope,
		Run: DirectGraphRunMetadata{
			Timestamp:  time.Now().UTC(),
			GitSHA:     gitSHA(),
			Command:    opts.Command,
			CorpusPath: opts.CorpusPath,
			Subset:     opts.Subset,
			Output:     opts.Output,
		},
		Queries:         results,
		StatusSnapshots: statusSnapshots,
	}
	report.Summary = summary
	report.ModeCounts = directGraphModeCounts(results)
	report.ByMode = summarizeDirectGraphByMode(results)
	for mode, modeSummary := range report.ByMode {
		met, _ := evaluateGraphModeThreshold(mode, modeSummary, graphModeThreshold(mode, opts.ModeThresholds))
		modeSummary.ThresholdMet = met
		report.ByMode[mode] = modeSummary
	}
	report.Degradation = summarizeDirectGraphDegradation(results)
	// Derive measurement truth from direct graph.query execution evidence, not
	// from how many cases passed: a relevance miss on a healthy graph is still a
	// measurement of the tool. See TASK-GRA11.
	report.UnmeasuredReason = directGraphMeasurementReason(report)
	report.GraphToolMeasured = report.UnmeasuredReason == ""
	return report
}

// directGraphMeasurementReason returns a non-empty reason when the report does
// not constitute honest direct graph.query measurement. It rejects search-only
// fallbacks (wrong measured_tool/scope), empty case selection, and runs where
// graph.query never produced servable output. An empty string means the run is
// a valid, evidence-backed graph.query measurement.
func directGraphMeasurementReason(report *DirectGraphEvalReport) string {
	if report.MeasuredTool != DirectGraphMeasuredTool {
		return fmt.Sprintf("measured_tool is %q, not %q (search-only fallback)", report.MeasuredTool, DirectGraphMeasuredTool)
	}
	if report.EvaluationScope != DirectGraphEvaluationScope {
		return fmt.Sprintf("evaluation_scope is %q, not %q", report.EvaluationScope, DirectGraphEvaluationScope)
	}
	if report.Summary.QueryCount == 0 {
		return "no graph-tool cases were selected"
	}
	if report.Summary.MeasuredQueryCount == 0 {
		return "graph.query produced no servable output (tool not measured)"
	}
	return ""
}

func summarizeDirectGraph(results []DirectGraphQueryResult) DirectGraphSummary {
	summary := DirectGraphSummary{QueryCount: len(results)}
	latencies := make([]int64, 0, len(results))
	zeroResults := 0
	blocking := 0
	for _, result := range results {
		if directGraphResultMeasured(result) {
			summary.MeasuredQueryCount++
		}
		if result.Passed {
			summary.PassCount++
		}
		if result.ResultCount == 0 {
			zeroResults++
		}
		if result.BlockingDegradation {
			blocking++
		}
		latencies = append(latencies, result.LatencyMs)
	}
	summary.FailCount = summary.QueryCount - summary.PassCount
	if summary.QueryCount > 0 {
		summary.PassRate = float64(summary.PassCount) / float64(summary.QueryCount)
		summary.ZeroResultRate = float64(zeroResults) / float64(summary.QueryCount)
		summary.DegradationBlockingRate = float64(blocking) / float64(summary.QueryCount)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	summary.P50LatencyMs = percentile(latencies, 0.50)
	summary.P95LatencyMs = percentile(latencies, 0.95)
	return summary
}

// directGraphResultMeasured reports whether a query produced a servable
// graph.query answer. A transport error (no envelope) or a non-servable status
// (unavailable, incompatible, empty, or failed) is infrastructure degradation,
// not a measurement of tool quality. It uses graph.QueryServable — the same
// servability predicate the production query service uses to short-circuit — so
// eval measurement accounting cannot drift from real query behavior. Note this
// is stricter than result.Available, which stays true for a `failed` build.
func directGraphResultMeasured(result DirectGraphQueryResult) bool {
	return result.Error == "" && graph.QueryServable(result.Status)
}

func directGraphModeCounts(results []DirectGraphQueryResult) map[string]int {
	counts := make(map[string]int)
	for _, result := range results {
		counts[result.Mode]++
	}
	return counts
}

func summarizeDirectGraphByMode(results []DirectGraphQueryResult) map[string]DirectGraphModeSummary {
	groups := make(map[string][]DirectGraphQueryResult)
	for _, result := range results {
		groups[result.Mode] = append(groups[result.Mode], result)
	}
	out := make(map[string]DirectGraphModeSummary, len(groups))
	for mode, group := range groups {
		summary := DirectGraphModeSummary{QueryCount: len(group)}
		var recallSum, precision3Sum, precision5Sum, precision10Sum float64
		for _, result := range group {
			if directGraphResultMeasured(result) {
				summary.MeasuredCount++
			}
			if result.Passed {
				summary.PassCount++
			}
			summary.ResultCount += result.ResultCount
			if result.ResultCount == 0 {
				summary.ZeroResultCount++
			}
			// Relevance quality is scored only over measured, quality-class,
			// non-blocking cases: a degraded/gap probe or an unmeasured (empty/
			// unavailable) run is not a relevance signal.
			if !directGraphResultQualityScored(result) {
				continue
			}
			summary.QualityCount++
			recallSum += result.ExpectedRecallAt10
			precision3Sum += result.PrecisionAt3
			precision5Sum += result.PrecisionAt5
			precision10Sum += result.PrecisionAt10
			if result.HitAt3 {
				summary.HitRateAt3++
			}
			if result.HitAt10 {
				summary.HitRateAt10++
			}
		}
		summary.FailCount = summary.QueryCount - summary.PassCount
		if summary.QueryCount > 0 {
			summary.PassRate = float64(summary.PassCount) / float64(summary.QueryCount)
			summary.ZeroResultRate = float64(summary.ZeroResultCount) / float64(summary.QueryCount)
		}
		if summary.QualityCount > 0 {
			quality := float64(summary.QualityCount)
			summary.ExpectedRecallAt10 = recallSum / quality
			summary.PrecisionAt3 = precision3Sum / quality
			summary.PrecisionAt5 = precision5Sum / quality
			summary.PrecisionAt10 = precision10Sum / quality
			summary.HitRateAt3 /= quality
			summary.HitRateAt10 /= quality
		}
		out[mode] = summary
	}
	return out
}

// directGraphResultQualityScored reports whether a case contributes to the
// per-mode relevance aggregates: it must have reached relevance scoring (Scored,
// which already implies a servable, allowed answer), be labeled quality-class,
// and carry no unexpected (blocking) degradation. This keeps graph health,
// contract failures, and infrastructure degradation separate from relevance
// quality (TASK-GRA16: quality metrics exclude unexpected degradation).
func directGraphResultQualityScored(result DirectGraphQueryResult) bool {
	return result.Scored &&
		graphExpectationClassOrDefault(result.ExpectationClass) == GraphExpectationClassQuality &&
		!result.BlockingDegradation
}

// graphQualityExclusionReason returns "" when a case is included in the quality
// aggregates, or a short reason when it is excluded — so the report can document
// exactly which cases the quality pass rate did not cover.
func graphQualityExclusionReason(result DirectGraphQueryResult) string {
	if directGraphResultQualityScored(result) {
		return ""
	}
	if !result.Scored {
		if !directGraphResultMeasured(result) {
			if len(result.DegradationLabels) > 0 {
				return "unmeasured: " + joinGraphDegradationLabels(result.DegradationLabels)
			}
			return "unmeasured"
		}
		return "contract_failure"
	}
	if graphExpectationClassOrDefault(result.ExpectationClass) != GraphExpectationClassQuality {
		return "non_quality_class: " + graphExpectationClassOrDefault(result.ExpectationClass)
	}
	if result.BlockingDegradation {
		return "unexpected_degradation: " + joinGraphDegradationLabels(result.BlockingDegradationLabels)
	}
	return "excluded"
}

// anyStaleGraphResult reports whether any returned evidence row is a stale edge,
// the result-level signal for the stale_edges degradation label.
func anyStaleGraphResult(results []graph.QueryResult) bool {
	for _, result := range results {
		if result.Stale {
			return true
		}
	}
	return false
}

func joinGraphDegradationLabels(labels []GraphDegradationLabel) string {
	parts := make([]string, 0, len(labels))
	for _, label := range labels {
		parts = append(parts, string(label))
	}
	return strings.Join(parts, ", ")
}

// isQualityContractFailure reports whether a quality-class case failed for a
// non-relevance reason: a transport error, or a measured answer that did not
// reach relevance scoring (disallowed status, missing expected warnings). These
// are hard failures regardless of aggregate metrics. Blocking degradation is
// excluded because it is owned by the blocking-degradation-rate gate.
func isQualityContractFailure(result DirectGraphQueryResult) bool {
	if graphExpectationClassOrDefault(result.ExpectationClass) != GraphExpectationClassQuality {
		return false
	}
	if result.BlockingDegradation {
		return false
	}
	if result.Error != "" {
		return true
	}
	return directGraphResultMeasured(result) && !result.Scored
}

// graphModeThreshold returns the relevance floors configured for one query mode.
func graphModeThreshold(mode string, thresholds config.GraphEvalModeThresholds) config.GraphEvalModeThreshold {
	switch mode {
	case graph.QueryModeFindReferences:
		return thresholds.FindReferences
	case graph.QueryModeExplainSymbol:
		return thresholds.ExplainSymbol
	case graph.QueryModeImpactAnalysis:
		return thresholds.ImpactAnalysis
	default:
		return config.GraphEvalModeThreshold{}
	}
}

// evaluateGraphModeThreshold reports whether a mode met its configured relevance
// floors. A mode with no configured floor is trivially met. A mode with a
// configured floor but no quality-scored cases is NOT met (it cannot certify
// relevance), guarding against a vacuous pass. Returns the failure reasons for
// the gate.
func evaluateGraphModeThreshold(mode string, summary DirectGraphModeSummary, floor config.GraphEvalModeThreshold) (bool, []string) {
	checks := []struct {
		name  string
		floor float64
		value float64
	}{
		{"expected_recall_at_10", floor.MinExpectedRecallAt10, summary.ExpectedRecallAt10},
		{"precision_at_10", floor.MinPrecisionAt10, summary.PrecisionAt10},
		{"hit_rate_at_3", floor.MinHitRateAt3, summary.HitRateAt3},
		{"hit_rate_at_10", floor.MinHitRateAt10, summary.HitRateAt10},
	}
	applicable := false
	for _, c := range checks {
		if c.floor > 0 {
			applicable = true
			break
		}
	}
	if !applicable {
		return true, nil
	}
	if summary.QualityCount == 0 {
		return false, []string{fmt.Sprintf("%s has no quality-scored cases to evaluate relevance thresholds", mode)}
	}
	met := true
	var reasons []string
	for _, c := range checks {
		if c.floor > 0 && c.value < c.floor {
			met = false
			reasons = append(reasons, fmt.Sprintf("%s %s %.2f < %.2f", mode, c.name, c.value, c.floor))
		}
	}
	return met, reasons
}

// graphModeThresholdFailures collects per-mode relevance-threshold failure
// reasons across all evaluated modes, in deterministic mode order.
func graphModeThresholdFailures(report *DirectGraphEvalReport, thresholds config.GraphEvalModeThresholds) []string {
	var reasons []string
	for _, mode := range sortedGraphModes(report.ByMode) {
		floor := graphModeThreshold(mode, thresholds)
		if _, modeReasons := evaluateGraphModeThreshold(mode, report.ByMode[mode], floor); len(modeReasons) > 0 {
			reasons = append(reasons, modeReasons...)
		}
	}
	return reasons
}

// qualityContractFailures collects non-relevance hard failures (transport error,
// disallowed status, missing expected warnings) for quality-class cases. These
// fail the gate regardless of aggregate relevance because they are graph/contract
// failures, not relevance misses.
func qualityContractFailures(results []DirectGraphQueryResult) []string {
	var reasons []string
	for _, result := range results {
		if !isQualityContractFailure(result) {
			continue
		}
		detail := result.FailureReason
		if detail == "" {
			detail = result.Error
		}
		if detail == "" {
			detail = "contract failure"
		}
		reasons = append(reasons, fmt.Sprintf("case %s (%s): %s", result.ID, result.Mode, detail))
	}
	return reasons
}

func summarizeDirectGraphDegradation(results []DirectGraphQueryResult) DirectGraphDegradationSummary {
	summary := DirectGraphDegradationSummary{
		ByStatus:        make(map[graph.GraphStatus]int),
		WarningCounts:   make(map[graph.WarningCode]int),
		ByLabel:         make(map[GraphDegradationLabel]int),
		BlockingByLabel: make(map[GraphDegradationLabel]int),
	}
	presentLabels := make(map[GraphDegradationLabel]struct{})
	for _, result := range results {
		if result.Status != "" && result.Status != graph.GraphStatusFresh {
			summary.ByStatus[result.Status]++
		}
		if result.BlockingDegradation {
			summary.BlockingCount++
		}
		if len(result.DegradationLabels) > 0 {
			summary.OverallCount++
		}
		for _, code := range result.WarningCodes {
			summary.WarningCounts[code]++
		}
		for _, label := range result.DegradationLabels {
			summary.ByLabel[label]++
			presentLabels[label] = struct{}{}
		}
		for _, label := range result.BlockingDegradationLabels {
			summary.BlockingByLabel[label]++
		}
		if reason := graphQualityExclusionReason(result); reason != "" {
			summary.QualityExcluded = append(summary.QualityExcluded, GraphQualityExclusion{
				ID:     result.ID,
				Mode:   result.Mode,
				Reason: reason,
			})
		}
	}
	if total := len(results); total > 0 {
		summary.BlockingRate = float64(summary.BlockingCount) / float64(total)
		summary.OverallRate = float64(summary.OverallCount) / float64(total)
	}
	// Emit the recoverability matrix only for labels observed in this run, in
	// canonical order, so reviewers see remediation for exactly what occurred.
	for _, label := range allGraphDegradationLabels() {
		if _, ok := presentLabels[label]; ok {
			summary.Recoverability = append(summary.Recoverability, graphLabelRecoverability(label))
		}
	}
	return summary
}

func writeDirectGraphReport(opts GraphOptions, report *DirectGraphEvalReport) error {
	if err := validateGraphOutput(opts.Output); err != nil {
		return err
	}
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return fmt.Errorf("failed to create graph eval output directory %s: %w", opts.OutDir, err)
	}
	if opts.Output == "json" || opts.Output == "both" {
		report.OutputPaths.JSON = filepath.Join(opts.OutDir, "latest.json")
	}
	if opts.Output == "markdown" || opts.Output == "both" {
		report.OutputPaths.Markdown = filepath.Join(opts.OutDir, "latest.md")
	}
	if report.OutputPaths.JSON != "" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to encode graph eval JSON report: %w", err)
		}
		if err := os.WriteFile(report.OutputPaths.JSON, append(data, '\n'), 0o644); err != nil {
			return fmt.Errorf("failed to write graph eval JSON report %s: %w", report.OutputPaths.JSON, err)
		}
	}
	if report.OutputPaths.Markdown != "" {
		markdown := directGraphMarkdownReport(report)
		if err := os.WriteFile(report.OutputPaths.Markdown, []byte(markdown), 0o644); err != nil {
			return fmt.Errorf("failed to write graph eval Markdown report %s: %w", report.OutputPaths.Markdown, err)
		}
	}
	return nil
}

func directGraphMarkdownReport(report *DirectGraphEvalReport) string {
	var b strings.Builder
	b.WriteString("# Direct Graph Eval Report\n\n")
	b.WriteString("## Summary\n\n")
	fmt.Fprintf(&b, "- Measured tool: `%s`\n", report.MeasuredTool)
	fmt.Fprintf(&b, "- Evaluation scope: `%s`\n", report.EvaluationScope)
	fmt.Fprintf(&b, "- Graph tool measured: %t\n", report.GraphToolMeasured)
	if report.UnmeasuredReason != "" {
		fmt.Fprintf(&b, "- Unmeasured reason: %s\n", report.UnmeasuredReason)
	}
	fmt.Fprintf(&b, "- Queries: %d\n", report.Summary.QueryCount)
	fmt.Fprintf(&b, "- Measured queries: %d\n", report.Summary.MeasuredQueryCount)
	fmt.Fprintf(&b, "- Passed: %d\n", report.Summary.PassCount)
	fmt.Fprintf(&b, "- Failed: %d\n", report.Summary.FailCount)
	fmt.Fprintf(&b, "- Pass rate: %.2f\n", report.Summary.PassRate)
	fmt.Fprintf(&b, "- Zero-result rate: %.2f\n", report.Summary.ZeroResultRate)
	// Blocking degradation rate is rendered once, in the dedicated Degradation
	// section below (with its case count), rather than duplicated here (DEBT-037
	// finding #6). The gate reads report.Summary.DegradationBlockingRate directly.
	fmt.Fprintf(&b, "- p95 latency: %d ms\n\n", report.Summary.P95LatencyMs)

	b.WriteString("## Modes\n\n")
	b.WriteString("| Mode | Queries | Measured | Quality | recall@10 | precision@3 | precision@5 | precision@10 | hit@3 | hit@10 | Threshold met |\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|:--:|\n")
	for _, mode := range sortedGraphModes(report.ByMode) {
		summary := report.ByMode[mode]
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f | %t |\n",
			mode,
			summary.QueryCount,
			summary.MeasuredCount,
			summary.QualityCount,
			summary.ExpectedRecallAt10,
			summary.PrecisionAt3,
			summary.PrecisionAt5,
			summary.PrecisionAt10,
			summary.HitRateAt3,
			summary.HitRateAt10,
			summary.ThresholdMet,
		)
	}
	b.WriteString("\n")

	b.WriteString("## Degradation\n\n")
	fmt.Fprintf(&b, "- Blocking degradation rate: %.2f (%d cases)\n", report.Degradation.BlockingRate, report.Degradation.BlockingCount)
	fmt.Fprintf(&b, "- Overall degradation rate: %.2f (%d cases)\n", report.Degradation.OverallRate, report.Degradation.OverallCount)
	for _, status := range sortedGraphStatuses(report.Degradation.ByStatus) {
		fmt.Fprintf(&b, "- status %s: %d\n", status, report.Degradation.ByStatus[status])
	}
	for _, code := range sortedGraphWarningCodes(report.Degradation.WarningCounts) {
		fmt.Fprintf(&b, "- warning %s: %d\n", code, report.Degradation.WarningCounts[code])
	}
	b.WriteString("\n")

	if len(report.Degradation.ByLabel) > 0 {
		b.WriteString("### Degradation labels\n\n")
		b.WriteString("| Label | Total | Counts toward blocking rate | Recoverability | Remediation |\n")
		b.WriteString("|---|---:|---:|---|---|\n")
		for _, label := range sortedGraphDegradationLabelCounts(report.Degradation.ByLabel) {
			info := graphLabelRecoverability(label)
			fmt.Fprintf(&b, "| %s | %d | %d | %s | %s |\n",
				escapeMarkdownCell(string(label)),
				report.Degradation.ByLabel[label],
				report.Degradation.BlockingByLabel[label],
				escapeMarkdownCell(info.Recoverability),
				escapeMarkdownCell(info.Remediation),
			)
		}
		b.WriteString("\n")
	}

	if len(report.Degradation.QualityExcluded) > 0 {
		b.WriteString("### Cases excluded from quality metrics\n\n")
		b.WriteString("| ID | Mode | Reason |\n")
		b.WriteString("|---|---|---|\n")
		for _, excluded := range report.Degradation.QualityExcluded {
			fmt.Fprintf(&b, "| %s | %s | %s |\n",
				escapeMarkdownCell(excluded.ID),
				escapeMarkdownCell(excluded.Mode),
				escapeMarkdownCell(excluded.Reason),
			)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Queries\n\n")
	b.WriteString("| ID | Mode | Class | Status | Results | Unique | Matched | recall@10 | p@3 | p@5 | p@10 | hit@3 | hit@10 | Passed | Failure |\n")
	b.WriteString("|---|---|---|---|---:|---:|---:|---:|---:|---:|---:|:--:|:--:|:--:|---|\n")
	for _, result := range report.Queries {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %d | %d | %d | %.2f | %.2f | %.2f | %.2f | %t | %t | %t | %s |\n",
			escapeMarkdownCell(result.ID),
			escapeMarkdownCell(result.Mode),
			escapeMarkdownCell(graphExpectationClassOrDefault(result.ExpectationClass)),
			escapeMarkdownCell(string(result.Status)),
			result.ResultCount,
			result.UniqueResultCount,
			result.MatchedEvidenceCount,
			result.ExpectedRecallAt10,
			result.PrecisionAt3,
			result.PrecisionAt5,
			result.PrecisionAt10,
			result.HitAt3,
			result.HitAt10,
			result.Passed,
			escapeMarkdownCell(result.FailureReason),
		)
	}
	return b.String()
}

func matchedGraphEvidenceCount(expected []GraphExpectedEvidence, results []graph.QueryResult) int {
	matched := 0
	for _, want := range expected {
		for _, result := range results {
			if matchesGraphEvidence(want, result) {
				matched++
				break
			}
		}
	}
	return matched
}

// directGraphRankWindow is the fixed top-k window for direct graph eval rank
// metrics. It is deliberately independent of a case's query limit (which may be
// up to maxGraphEvalLimit): "precision@10" must always measure the first ten
// results, never a wider window a case happens to request.
const directGraphRankWindow = 10

// graphCaseMetrics holds rank-aware per-case scores for one graph eval query.
// Recall, hit ranks, and the MatchedPositions/WindowSize diagnostics are derived
// from a single matcher pass over the already-sorted, already-truncated raw
// results (the GRA12/13/14 contract). The Precision@K family is computed over
// the deduplicated result list (GRA15) so duplicate-looking evidence cannot
// inflate precision. ExpectedRecallAt10 is set-recall over a case's expected
// rows; this is intentionally distinct from search-eval's RecallAt10
// (runner.go), which is first-hit recall over queries.
type graphCaseMetrics struct {
	// ExpectedRecallAt10 is the fraction of expected evidence rows matched by
	// some raw result within the rank window.
	ExpectedRecallAt10 float64
	// PrecisionAt3/5/10 are the fraction of the first K *unique* results
	// (deduped by graphResultIdentity) that are relevant — relevance meaning a
	// match against an expected row OR an accepted alternative. The denominator
	// is min(K, UniqueResultCount), so a case that legitimately returns fewer
	// than K distinct rows is scored over its actual returned breadth and an
	// empty result scores 0.
	PrecisionAt3  float64
	PrecisionAt5  float64
	PrecisionAt10 float64
	// HitAt3 reports whether any expected row matched within the top three raw
	// results (the explain_symbol definition/context-hit contract).
	HitAt3 bool
	// HitAt10 reports whether any expected row matched within the raw rank
	// window (the impact_analysis direct-impact-hit contract).
	HitAt10 bool
	// MatchedExpected is the number of expected rows matched within the raw
	// window; it agrees with matchedGraphEvidenceCount when all matches fall
	// inside it.
	MatchedExpected int
	// MatchedPositions is the number of raw windowed result slots that match at
	// least one expected row; surfaced alongside UniqueResultCount so reviewers
	// can spot duplicate inflation (high MatchedPositions, low precision).
	MatchedPositions int
	// WindowSize is the effective raw rank window, min(directGraphRankWindow, len(results)).
	WindowSize int
	// UniqueResultCount is the number of distinct result identities returned
	// (the precision@K denominator ceiling).
	UniqueResultCount int
}

// scoreGraphCase computes rank-aware metrics for one graph eval case. It reuses
// matchesGraphEvidence so the matching contract is single-sourced, and it must
// not re-sort: result ordering is owned by graph.QueryService.Query. expected
// drives recall/hits; expected ∪ alternatives drives precision relevance.
func scoreGraphCase(expected, alternatives []GraphExpectedEvidence, results []graph.QueryResult, mode string) graphCaseMetrics {
	window := min(directGraphRankWindow, len(results))

	bestRank := make([]int, len(expected))
	for j := range bestRank {
		bestRank[j] = -1
	}
	matchedPositions := 0
	for i := 0; i < window; i++ {
		positionMatched := false
		for j, want := range expected {
			if matchesGraphEvidence(want, results[i]) {
				positionMatched = true
				if bestRank[j] == -1 {
					bestRank[j] = i
				}
			}
		}
		if positionMatched {
			matchedPositions++
		}
	}

	metrics := graphCaseMetrics{
		WindowSize:       window,
		MatchedPositions: matchedPositions,
	}
	for _, rank := range bestRank {
		if rank < 0 {
			continue
		}
		metrics.MatchedExpected++
		metrics.HitAt10 = true
		if rank < 3 {
			metrics.HitAt3 = true
		}
	}
	if len(expected) > 0 {
		metrics.ExpectedRecallAt10 = float64(metrics.MatchedExpected) / float64(len(expected))
	}

	// Precision@K is deduped over the SAME raw rank window recall/hits use
	// (results[:window]), so the two metric families can never disagree about
	// whether a relevant row exists: a result beyond the raw window is invisible
	// to both. Within that window, identical result identities collapse so a broad
	// row returned ten ways is one relevant result, not ten.
	unique := dedupeGraphResults(results[:window], mode)
	metrics.UniqueResultCount = len(unique)
	metrics.PrecisionAt3 = precisionAtK(unique, expected, alternatives, 3)
	metrics.PrecisionAt5 = precisionAtK(unique, expected, alternatives, 5)
	metrics.PrecisionAt10 = precisionAtK(unique, expected, alternatives, directGraphRankWindow)
	return metrics
}

// graphResultIdentity is the per-mode stable dedup key for a graph result. Its
// base is source_path|node_kind|relation|role (TASK-GRA15) — exactly the stable
// matcher fields. node_id, confidence, and graph_path are excluded from the base:
// node_id is index-volatile (the corpus never matches on it), and graph_path
// embeds per-result node IDs (graph.query builds it as [seed.id, edge.kind,
// related.id], query.go) — including either would leak that volatility into the
// key AND defeat dedup for the dominant find_references shape (many chunks of one
// file share source_path/kind/relation/role but each carry a distinct chunk node
// id), which is exactly the duplicate-looking evidence the ticket says to collapse.
// \x00 separates fields so no realistic field value (filesystem paths, edge kinds,
// role enums) can forge a key collision.
//
// PER-MODE RULE (DEBT-037 finding #1): impact_analysis results all share the
// seed's source_path/role/relation and differ only by target node id. With the
// base-only key they collapse to one bucket, so precision@K would measure
// relevant-evidence-bucket fraction, not distinct-target fraction. For
// impact_analysis we therefore append node_id, keeping distinct targets distinct;
// find_references and explain_symbol keep the base key so their chunk rows of one
// file still collapse. impact_analysis is gated on hit@10 today, but a precision
// floor for it is now safe to add (its precision is meaningful under this key).
func graphResultIdentity(result graph.QueryResult, mode string) string {
	fields := []string{
		filepath.ToSlash(strings.TrimSpace(result.SourcePath)),
		string(result.NodeKind),
		string(result.Relation),
		strings.TrimSpace(result.Role),
	}
	if mode == graph.QueryModeImpactAnalysis {
		fields = append(fields, strings.TrimSpace(result.NodeID))
	}
	return strings.Join(fields, "\x00")
}

// graphPrecisionIdentityAmbiguity returns the first per-mode identity in the
// scored window whose members disagree on relevance (some relevant, some not).
// Such a bucket makes precision@K order-dependent: dedup keeps the first-seen
// member, so a relevant row hidden behind a non-relevant twin (or vice versa)
// would silently change the score. It returns "" when every bucket is
// relevance-uniform, which is the invariant the precision dedup relies on
// (DEBT-037 finding #2).
//
// This is validated against real returned results, NOT by statically rejecting
// matchers: the corpus legitimately and pervasively matches on fields outside the
// identity (confidence_label, evidence_method, graph_path_contains), and those
// matchers are safe precisely because relevance is uniform within each identity
// bucket on the real graph. The guard fails fast only if that assumption breaks.
func graphPrecisionIdentityAmbiguity(results []graph.QueryResult, expected, alternatives []GraphExpectedEvidence, mode string) string {
	window := min(directGraphRankWindow, len(results))
	firstRelevance := make(map[string]bool, window)
	seen := make(map[string]bool, window)
	for i := 0; i < window; i++ {
		id := graphResultIdentity(results[i], mode)
		relevant := graphResultRelevant(results[i], expected, alternatives)
		if seen[id] {
			if firstRelevance[id] != relevant {
				return id
			}
			continue
		}
		seen[id] = true
		firstRelevance[id] = relevant
	}
	return ""
}

// dedupeGraphResults collapses results sharing a graphResultIdentity (per mode),
// preserving first-seen (rank) order so precision@K windows score the
// highest-ranked representative of each distinct evidence row.
func dedupeGraphResults(results []graph.QueryResult, mode string) []graph.QueryResult {
	seen := make(map[string]struct{}, len(results))
	unique := make([]graph.QueryResult, 0, len(results))
	for _, result := range results {
		id := graphResultIdentity(result, mode)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, result)
	}
	return unique
}

// precisionAtK is the fraction of the first K unique results that are relevant.
// The denominator is min(k, len(unique)); an empty result list scores 0.
func precisionAtK(unique []graph.QueryResult, expected, alternatives []GraphExpectedEvidence, k int) float64 {
	window := min(k, len(unique))
	if window == 0 {
		return 0
	}
	relevant := 0
	for i := 0; i < window; i++ {
		if graphResultRelevant(unique[i], expected, alternatives) {
			relevant++
		}
	}
	return float64(relevant) / float64(window)
}

// graphResultRelevant reports whether a result matches any expected row or any
// accepted alternative. A result matching several matchers is relevant exactly
// once (boolean), so expected/alternative overlap is never double-counted.
func graphResultRelevant(result graph.QueryResult, expected, alternatives []GraphExpectedEvidence) bool {
	for _, want := range expected {
		if matchesGraphEvidence(want, result) {
			return true
		}
	}
	for _, alt := range alternatives {
		if matchesGraphEvidence(alt, result) {
			return true
		}
	}
	return false
}

func matchesGraphEvidence(want GraphExpectedEvidence, result graph.QueryResult) bool {
	if want.NodeID != "" && result.NodeID != want.NodeID {
		return false
	}
	if want.NodeKind != "" && result.NodeKind != want.NodeKind {
		return false
	}
	if want.SourcePath != "" && !matchesGraphSourcePath(want.SourcePath, result.SourcePath) {
		return false
	}
	if want.Role != "" && result.Role != want.Role {
		return false
	}
	if want.Relation != "" && result.Relation != want.Relation {
		return false
	}
	if want.ConfidenceLabel != "" && result.ConfidenceLabel != want.ConfidenceLabel {
		return false
	}
	if want.EvidenceMethod != "" && result.EvidenceMethod != want.EvidenceMethod {
		return false
	}
	for _, token := range want.GraphPathContains {
		if !graphPathContains(result.GraphPath, token) {
			return false
		}
	}
	return true
}

func matchesGraphSourcePath(want, got string) bool {
	want = filepath.ToSlash(strings.TrimSpace(want))
	got = filepath.ToSlash(strings.TrimSpace(got))
	if got == want {
		return true
	}
	if strings.HasSuffix(want, "/") {
		return strings.HasPrefix(got, want)
	}
	return false
}

func graphPathContains(path []string, token string) bool {
	token = strings.TrimSpace(token)
	for _, segment := range path {
		if strings.TrimSpace(segment) == token {
			return true
		}
	}
	return false
}

func isBlockingGraphStatus(blocking []graph.GraphStatus, status graph.GraphStatus) bool {
	for _, candidate := range blocking {
		if candidate == status {
			return true
		}
	}
	return false
}

func isAllowedGraphStatus(allowed []graph.GraphStatus, status graph.GraphStatus) bool {
	for _, candidate := range allowed {
		if candidate == status {
			return true
		}
	}
	return false
}

func missingGraphWarningCodes(expected, actual []graph.WarningCode) []graph.WarningCode {
	if len(expected) == 0 {
		return nil
	}
	actualSet := make(map[graph.WarningCode]struct{}, len(actual))
	for _, code := range actual {
		actualSet[code] = struct{}{}
	}
	missing := make([]graph.WarningCode, 0)
	for _, code := range expected {
		if _, ok := actualSet[code]; !ok {
			missing = append(missing, code)
		}
	}
	return missing
}

func graphWarningCodeStrings(codes []graph.WarningCode) []string {
	out := make([]string, 0, len(codes))
	for _, code := range codes {
		out = append(out, string(code))
	}
	sort.Strings(out)
	return out
}

func graphWarningCodes(warnings []graph.StatusWarning) []graph.WarningCode {
	codes := make([]graph.WarningCode, 0, len(warnings))
	for _, warning := range warnings {
		codes = append(codes, warning.Code)
	}
	return codes
}

func briefGraphStatusSnapshot(snapshot *graph.StatusSnapshot) GraphStatusSnapshotBrief {
	if snapshot == nil {
		return GraphStatusSnapshotBrief{}
	}
	return GraphStatusSnapshotBrief{
		Available:             snapshot.Available,
		SchemaVersion:         snapshot.SchemaVersion,
		Status:                snapshot.Status,
		Freshness:             snapshot.Freshness.State,
		NodeCount:             snapshot.Nodes.Total,
		EdgeCount:             snapshot.Edges.Total,
		ActiveEdgeCount:       snapshot.ActiveEdges.Total,
		StaleEdgeCount:        snapshot.StaleEdges.Total,
		WarningCodes:          graphWarningCodes(snapshot.Warnings),
		Warnings:              append([]graph.StatusWarning(nil), snapshot.Warnings...),
		ExtractorStatusCounts: graphExtractorStatusCounts(snapshot.Extractors),
		Extractors:            graphExtractorSamples(snapshot.Extractors),
		Confidence:            cloneStringIntMap(snapshot.Confidence),
	}
}

func graphExtractorStatusCounts(extractors []graph.ExtractorSummary) map[graph.ExtractorStatus]int {
	counts := make(map[graph.ExtractorStatus]int)
	for _, extractor := range extractors {
		counts[extractor.Status]++
	}
	return counts
}

func graphExtractorSamples(extractors []graph.ExtractorSummary) []graph.ExtractorSummary {
	samples := make([]graph.ExtractorSummary, 0, maxGraphStatusExtractorSamples)
	for _, extractor := range extractors {
		if extractor.Status == graph.ExtractorStatusSuccess {
			continue
		}
		samples = append(samples, extractor)
		if len(samples) == maxGraphStatusExtractorSamples {
			break
		}
	}
	return samples
}

func sortedGraphModes(summaries map[string]DirectGraphModeSummary) []string {
	modes := make([]string, 0, len(summaries))
	for mode := range summaries {
		modes = append(modes, mode)
	}
	sort.Strings(modes)
	return modes
}

func sortedGraphStatuses(counts map[graph.GraphStatus]int) []graph.GraphStatus {
	statuses := make([]graph.GraphStatus, 0, len(counts))
	for status := range counts {
		statuses = append(statuses, status)
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i] < statuses[j] })
	return statuses
}

func sortedGraphDegradationLabelCounts(counts map[GraphDegradationLabel]int) []GraphDegradationLabel {
	labels := make([]GraphDegradationLabel, 0, len(counts))
	for label := range counts {
		labels = append(labels, label)
	}
	sortGraphDegradationLabels(labels)
	return labels
}

func sortedGraphWarningCodes(counts map[graph.WarningCode]int) []graph.WarningCode {
	codes := make([]graph.WarningCode, 0, len(counts))
	for code := range counts {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	return codes
}

func escapeMarkdownCell(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.ReplaceAll(value, "|", "\\|")
}

func cloneStringIntMap(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
