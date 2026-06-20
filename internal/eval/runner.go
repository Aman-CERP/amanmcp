package eval

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Searcher interface {
	Search(context.Context, Query) (SearchResponse, error)
}

type preparer interface {
	Prepare(context.Context) error
}

type Runner struct {
	searcher Searcher
}

func NewRunner(searcher Searcher) *Runner {
	return &Runner{searcher: searcher}
}

func (r *Runner) Run(ctx context.Context, opts Options) (*Report, error) {
	opts = normalizeOptions(opts)
	if closer, ok := r.searcher.(interface{ Close() error }); ok {
		defer func() { _ = closer.Close() }()
	}
	corpus, err := LoadCorpus(opts.CorpusPath)
	if err != nil {
		return nil, err
	}
	queries, err := SelectQueries(corpus.Queries, Selection{Subset: opts.Subset, IncludeHoldout: opts.IncludeHoldout})
	if err != nil {
		return nil, err
	}
	if len(queries) == 0 {
		return nil, fmt.Errorf("subset %q selected no queries", opts.Subset)
	}
	if preparer, ok := r.searcher.(preparer); ok {
		if err := preparer.Prepare(ctx); err != nil {
			return nil, err
		}
	}

	results := make([]QueryResult, 0, len(queries))
	memoryPeakBytes := sampleMemoryBytes()
	for _, query := range queries {
		results = append(results, r.runQuery(ctx, query))
		memoryPeakBytes = max(memoryPeakBytes, sampleMemoryBytes())
	}

	report := buildReport(opts, results)
	report.Summary.MemoryPeakBytes = memoryPeakBytes
	compareErr := compareBaseline(opts, report)
	if err := writeReport(opts, report); err != nil {
		return report, err
	}
	if compareErr != nil {
		return report, compareErr
	}
	if opts.FailOnRegression && report.BaselineComparison.Regressed {
		return report, fmt.Errorf("eval regression detected: %s", strings.Join(report.BaselineComparison.RegressionReasons, "; "))
	}
	return report, nil
}

func normalizeOptions(opts Options) Options {
	if opts.CorpusPath == "" {
		opts.CorpusPath = DefaultCorpusPath
	}
	if opts.Subset == "" {
		opts.Subset = "full"
	}
	if opts.Output == "" {
		opts.Output = "both"
	}
	if opts.OutDir == "" {
		opts.OutDir = DefaultOutDir
	}
	if opts.Command == "" {
		opts.Command = "amanmcp eval search"
	}
	opts.TokenBudgetEnabled = true
	return opts
}

func SelectQueries(queries []Query, selection Selection) ([]Query, error) {
	subset := selection.Subset
	if subset == "" {
		subset = "full"
	}

	switch {
	case subset == "full":
		return filterQueries(queries, selection.IncludeHoldout, func(Query) bool { return true }), nil
	case subset == "holdout":
		return filterQueries(queries, true, func(query Query) bool { return query.Holdout }), nil
	case subset == "quick":
		return selectQuickQueries(queries), nil
	case subset == "graph":
		return filterQueries(queries, selection.IncludeHoldout, func(query Query) bool {
			return isGraphHeavyClass(query.Class) || isExactRegressionQuery(query)
		}), nil
	case strings.HasPrefix(subset, "class:"):
		class := strings.TrimPrefix(subset, "class:")
		if !allowedClasses[class] {
			return nil, fmt.Errorf("unsupported subset class %q", class)
		}
		return filterQueries(queries, selection.IncludeHoldout, func(query Query) bool { return query.Class == class }), nil
	case strings.HasPrefix(subset, "job:"):
		job := strings.TrimPrefix(subset, "job:")
		if !allowedJobs[job] {
			return nil, fmt.Errorf("unsupported subset job %q", job)
		}
		return filterQueries(queries, selection.IncludeHoldout, func(query Query) bool { return query.Job == job }), nil
	default:
		return nil, fmt.Errorf("unsupported subset %q", subset)
	}
}

func filterQueries(queries []Query, includeHoldout bool, keep func(Query) bool) []Query {
	selected := make([]Query, 0, len(queries))
	for _, query := range queries {
		if query.Holdout && !includeHoldout {
			continue
		}
		if keep(query) {
			selected = append(selected, query)
		}
	}
	return selected
}

func selectQuickQueries(queries []Query) []Query {
	requiredClasses := []string{
		"exact_identifier",
		"path_lookup",
		"quoted_string",
		"natural_language_intent",
		"docs_to_code",
		"negative_adversarial",
		"config_error",
	}
	selected := make([]Query, 0, 15)
	seen := make(map[string]bool)
	for _, class := range requiredClasses {
		for _, query := range queries {
			if query.Holdout || query.Class != class || seen[query.ID] {
				continue
			}
			selected = append(selected, query)
			seen[query.ID] = true
			break
		}
	}
	for _, query := range queries {
		if query.Holdout || seen[query.ID] || !isPDFQuery(query.Metadata) {
			continue
		}
		selected = append(selected, query)
		seen[query.ID] = true
	}
	for _, query := range queries {
		if len(selected) >= 15 {
			break
		}
		if query.Holdout || seen[query.ID] {
			continue
		}
		selected = append(selected, query)
		seen[query.ID] = true
	}
	return selected
}

func (r *Runner) runQuery(ctx context.Context, query Query) QueryResult {
	start := time.Now()
	response, err := r.searcher.Search(ctx, query)
	latency := time.Since(start)
	results := response.Results
	qr := QueryResult{
		ID:              query.ID,
		Name:            query.Name,
		Query:           query.Query,
		Tool:            query.Tool,
		Class:           query.Class,
		Job:             query.Job,
		Profile:         queryProfile(query),
		SourceClass:     querySourceClass(query),
		Language:        queryLanguage(query),
		Metadata:        cloneStringMap(query.Metadata),
		Holdout:         query.Holdout,
		ExpectedResults: query.ExpectedResults,
		TopResults:      results,
		FirstUsefulRank: -1,
		LatencyMs:       latency.Milliseconds(),
		TokenEstimate:   estimateTokens(query, response),
	}
	if err != nil {
		qr.Error = err.Error()
		qr.FailureReason = err.Error()
		return qr
	}
	qr.Passed, qr.FirstUsefulRank, qr.MatchedGrade = scoreQuery(query, results)
	if !qr.Passed {
		qr.FailureReason = failureReason(query, results)
	}
	return qr
}

func scoreQuery(query Query, results []SearchResult) (bool, int, int) {
	if query.Class == "negative_adversarial" {
		for rank, result := range results {
			for _, want := range query.ExpectedResults {
				if want.Grade != 0 {
					continue
				}
				if matchesPath(result.Path, want.Path) {
					return false, rank + 1, 0
				}
			}
		}
		return true, -1, 0
	}
	for rank, result := range results {
		grade := matchedGrade(query.ExpectedResults, result)
		if grade >= 2 {
			return true, rank + 1, grade
		}
	}
	return false, -1, 0
}

func matchedGrade(expected []ExpectedResult, result SearchResult) int {
	best := 0
	for _, want := range expected {
		if !matchesPath(result.Path, want.Path) {
			continue
		}
		if !matchesSymbol(want.Symbol, result.Symbol) {
			continue
		}
		if !matchesPageExpectation(want, result) {
			continue
		}
		if want.Grade > best {
			best = want.Grade
		}
	}
	return best
}

func matchesSymbol(expected, actual string) bool {
	if expected == "" {
		return true
	}
	if actual == expected {
		return true
	}
	suffix, ok := strings.CutPrefix(actual, expected+"_part")
	if !ok || suffix == "" {
		return false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func matchesPageExpectation(want ExpectedResult, result SearchResult) bool {
	if want.Page == 0 && want.PageStart == 0 && want.PageEnd == 0 {
		return true
	}
	resultStart, resultEnd := resultPageRange(result)
	if resultStart == 0 && resultEnd == 0 {
		return false
	}
	if want.Page > 0 {
		return want.Page >= resultStart && want.Page <= resultEnd
	}
	wantStart := want.PageStart
	if wantStart == 0 {
		wantStart = want.PageEnd
	}
	wantEnd := want.PageEnd
	if wantEnd == 0 {
		wantEnd = wantStart
	}
	return wantStart >= resultStart && wantEnd <= resultEnd
}

func resultPageRange(result SearchResult) (int, int) {
	start := parsePositiveInt(result.PageStart)
	end := parsePositiveInt(result.PageEnd)
	if start == 0 && end == 0 {
		page := parsePositiveInt(result.PageNumber)
		return page, page
	}
	if start == 0 {
		start = end
	}
	if end == 0 {
		end = start
	}
	return start, end
}

func parsePositiveInt(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}

func matchesPath(got, want string) bool {
	got = strings.Trim(strings.TrimSpace(got), "/")
	want = strings.Trim(strings.TrimSpace(want), "/")
	if got == "" || want == "" {
		return false
	}
	return got == want || strings.HasPrefix(got, want+"/")
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func isPDFQuery(metadata map[string]string) bool {
	return strings.EqualFold(strings.TrimSpace(metadata["content_type"]), "pdf")
}

func queryProfile(query Query) string {
	if profile := strings.TrimSpace(query.Profile); profile != "" {
		return profile
	}
	return "default"
}

func querySourceClass(query Query) string {
	if sourceClass := normalizedMetadataValue(query.Metadata, "source_class"); sourceClass != "" {
		return sourceClass
	}
	if contentType := normalizedMetadataValue(query.Metadata, "content_type"); contentType != "" {
		if contentType == "pdf" {
			return "pdf"
		}
		if contentType == "markdown" || contentType == "md" {
			return "docs"
		}
	}
	return sourceClassForPath(primaryExpectedPath(query.ExpectedResults))
}

func queryLanguage(query Query) string {
	if language := normalizedMetadataValue(query.Metadata, "language"); language != "" {
		return language
	}
	if contentType := normalizedMetadataValue(query.Metadata, "content_type"); contentType != "" {
		switch contentType {
		case "pdf":
			return "pdf"
		case "markdown", "md":
			return "markdown"
		}
	}
	return languageForPath(primaryExpectedPath(query.ExpectedResults))
}

func normalizedMetadataValue(metadata map[string]string, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value := strings.ToLower(strings.TrimSpace(metadata[key]))
	return strings.ReplaceAll(value, "_", "-")
}

func primaryExpectedPath(expected []ExpectedResult) string {
	for _, result := range expected {
		if result.Grade > 0 && strings.TrimSpace(result.Path) != "" {
			return result.Path
		}
	}
	for _, result := range expected {
		if strings.TrimSpace(result.Path) != "" {
			return result.Path
		}
	}
	return ""
}

func resultProfile(result QueryResult) string {
	if profile := strings.TrimSpace(result.Profile); profile != "" {
		return profile
	}
	return "default"
}

func resultSourceClass(result QueryResult) string {
	if sourceClass := strings.TrimSpace(result.SourceClass); sourceClass != "" {
		return sourceClass
	}
	if sourceClass := normalizedMetadataValue(result.Metadata, "source_class"); sourceClass != "" {
		return sourceClass
	}
	if isPDFQuery(result.Metadata) {
		return "pdf"
	}
	return sourceClassForPath(primaryExpectedPath(result.ExpectedResults))
}

func resultLanguage(result QueryResult) string {
	if language := strings.TrimSpace(result.Language); language != "" {
		return language
	}
	if language := normalizedMetadataValue(result.Metadata, "language"); language != "" {
		return language
	}
	if isPDFQuery(result.Metadata) {
		return "pdf"
	}
	return languageForPath(primaryExpectedPath(result.ExpectedResults))
}

func sourceClassForPath(path string) string {
	clean := strings.Trim(strings.ToLower(strings.TrimSpace(path)), "/")
	if clean == "" {
		return "unknown"
	}
	base := filepath.Base(clean)
	ext := filepath.Ext(base)
	switch {
	case strings.HasPrefix(clean, ".aman-pm/decisions/") || strings.HasPrefix(base, "adr-"):
		return "adr"
	case strings.HasPrefix(clean, ".aman-pm/"):
		return "pm"
	case isTestPath(clean):
		return "test"
	case ext == ".pdf":
		return "pdf"
	case isConfigPath(clean):
		return "config"
	case isCodePath(clean):
		return "code"
	case isMarkdownPath(clean):
		return "docs"
	default:
		return "unknown"
	}
}

func languageForPath(path string) string {
	clean := strings.Trim(strings.ToLower(strings.TrimSpace(path)), "/")
	if clean == "" {
		return "unknown"
	}
	base := filepath.Base(clean)
	ext := filepath.Ext(base)
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "ts"
	case ".js", ".jsx":
		return "js"
	case ".md", ".mdx":
		return "markdown"
	case ".pdf":
		return "pdf"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".toml":
		return "toml"
	case ".sh", ".bash", ".zsh":
		return "shell"
	case ".env":
		return "env"
	case ".properties":
		return "properties"
	default:
		if base == ".env" || strings.HasSuffix(base, ".env") {
			return "env"
		}
		return "unknown"
	}
}

func isTestPath(path string) bool {
	base := filepath.Base(path)
	return strings.Contains(path, "/testdata/") ||
		strings.Contains(path, "/tests/") ||
		strings.Contains(path, "/__tests__/") ||
		strings.HasSuffix(base, "_test.go") ||
		strings.HasPrefix(base, "test_") ||
		strings.Contains(base, ".test.") ||
		strings.Contains(base, ".spec.")
}

func isCodePath(path string) bool {
	switch filepath.Ext(filepath.Base(path)) {
	case ".go", ".py", ".ts", ".tsx", ".js", ".jsx", ".java", ".rb", ".rs", ".c", ".cc", ".cpp", ".h", ".hpp", ".cs", ".swift", ".php", ".sh":
		return true
	default:
		return false
	}
}

func isMarkdownPath(path string) bool {
	switch filepath.Ext(filepath.Base(path)) {
	case ".md", ".mdx", ".rst", ".txt":
		return true
	default:
		return false
	}
}

func isConfigPath(path string) bool {
	base := filepath.Base(path)
	switch filepath.Ext(base) {
	case ".yaml", ".yml", ".json", ".toml", ".properties":
		return true
	default:
		return base == ".env" ||
			strings.HasSuffix(base, ".env") ||
			base == ".amanmcp.yaml" ||
			base == "go.mod" ||
			base == "go.sum"
	}
}

func failureReason(query Query, results []SearchResult) string {
	if len(results) == 0 {
		return "no results"
	}
	if query.Class == "negative_adversarial" {
		return "negative query returned prohibited result"
	}
	return "expected result not found in top 10"
}

func estimateTokens(query Query, response SearchResponse) TokenEstimate {
	queryTokens := estimateUTF8Tokens(query.Query)
	responseBytes := response.ResponseBytes
	if responseBytes == 0 {
		responseBytes = compactJSONLen(response.Results)
	}
	resultTokens := estimateTokensFromBytes(responseBytes)
	perResult := 0.0
	if len(response.Results) > 0 {
		perResult = float64(resultTokens) / float64(len(response.Results))
	}
	return TokenEstimate{
		Method:          "utf8-json-bytes-v1",
		QueryTokens:     queryTokens,
		ResponseBytes:   responseBytes,
		ResultTokens:    resultTokens,
		TokensPerResult: perResult,
		TotalTokens:     queryTokens + resultTokens,
	}
}

func estimateUTF8Tokens(s string) int {
	return estimateTokensFromBytes(len([]byte(s)))
}

func compactJSONLen(v any) int {
	data, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(data)
}

func estimateTokensFromBytes(n int) int {
	if n <= 0 {
		return 0
	}
	return int(math.Ceil(float64(n) / 4.0))
}

func buildReport(opts Options, results []QueryResult) *Report {
	report := &Report{
		Run: RunMetadata{
			Timestamp:      time.Now().UTC(),
			GitSHA:         gitSHA(),
			Command:        opts.Command,
			CorpusPath:     opts.CorpusPath,
			Subset:         opts.Subset,
			IncludeHoldout: opts.IncludeHoldout,
			Output:         opts.Output,
			Tolerances:     loadTolerances(),
		},
		Queries: results,
	}
	report.Summary = summarize(results)
	report.Metrics = calculateMetrics(results)
	report.ClassGroups = evalClassGroups()
	report.ByClass = groupMetrics(results, func(result QueryResult) string { return result.Class })
	report.ByJob = groupMetrics(results, func(result QueryResult) string { return result.Job })
	report.ByProfile = groupMetrics(results, resultProfile)
	report.BySourceClass = groupMetrics(results, resultSourceClass)
	report.ByLanguage = groupMetrics(results, resultLanguage)
	report.ExactLookupGate = currentExactLookupGate(results)
	report.GraphEvalGate = buildGraphEvalGate(results, report.ExactLookupGate, "")
	report.BaselineComparison = BaselineComparison{}
	return report
}

func defaultTolerances() Tolerances {
	return Tolerances{
		MinPassRateDelta:              -0.0001,
		MinRecallAt10Delta:            -0.0001,
		MaxP95LatencyMsDelta:          250,
		MaxTokenMeanIncreaseRatio:     0.10,
		MaxTokenP95IncreaseRatio:      0.10,
		MaxTokenClassIncreaseRatio:    0.15,
		MaxTokenClassP95IncreaseRatio: 0.15,
		MaxTokenQueryIncreaseRatio:    0.10,
		DimensionRegression:           defaultDimensionTolerances(),
	}
}

func defaultDimensionTolerances() map[string]DimensionTolerance {
	return map[string]DimensionTolerance{
		"class":        {MinPassRateDelta: -0.0001, MinRecallAt10Delta: -0.0001},
		"job":          {MinPassRateDelta: -0.0001, MinRecallAt10Delta: -0.0001},
		"profile":      {MinPassRateDelta: -0.0001, MinRecallAt10Delta: -0.0001},
		"source_class": {MinPassRateDelta: -0.0001, MinRecallAt10Delta: -0.0001},
		"language":     {MinPassRateDelta: -0.0001, MinRecallAt10Delta: -0.0001},
	}
}

func loadTolerances() Tolerances {
	tolerances := defaultTolerances()
	data, err := os.ReadFile(".aman-pm/rules.yaml")
	if err != nil {
		return tolerances
	}
	var rules struct {
		Validation struct {
			SearchEval struct {
				Tolerances struct {
					MinPassRateDelta              *float64 `yaml:"min_pass_rate_delta"`
					MinRecallAt10Delta            *float64 `yaml:"min_recall_at_10_delta"`
					MaxP95LatencyMsDelta          *int64   `yaml:"max_p95_latency_ms_delta"`
					MaxTokenMeanIncreaseRatio     *float64 `yaml:"max_token_mean_increase_ratio"`
					MaxTokenP95IncreaseRatio      *float64 `yaml:"max_token_p95_increase_ratio"`
					MaxTokenClassIncreaseRatio    *float64 `yaml:"max_token_class_increase_ratio"`
					MaxTokenClassP95IncreaseRatio *float64 `yaml:"max_token_class_p95_increase_ratio"`
					MaxTokenQueryIncreaseRatio    *float64 `yaml:"max_token_query_increase_ratio"`
				} `yaml:"tolerances"`
				DimensionRegression map[string]struct {
					MinPassRateDelta   *float64 `yaml:"min_pass_rate_delta"`
					MinRecallAt10Delta *float64 `yaml:"min_recall_at_10_delta"`
				} `yaml:"dimension_regression"`
			} `yaml:"search_eval"`
		} `yaml:"validation"`
	}
	if err := yaml.Unmarshal(data, &rules); err != nil {
		return tolerances
	}
	cfg := rules.Validation.SearchEval.Tolerances
	if cfg.MinPassRateDelta != nil {
		tolerances.MinPassRateDelta = *cfg.MinPassRateDelta
	}
	if cfg.MinRecallAt10Delta != nil {
		tolerances.MinRecallAt10Delta = *cfg.MinRecallAt10Delta
	}
	if cfg.MaxP95LatencyMsDelta != nil {
		tolerances.MaxP95LatencyMsDelta = *cfg.MaxP95LatencyMsDelta
	}
	if cfg.MaxTokenMeanIncreaseRatio != nil {
		tolerances.MaxTokenMeanIncreaseRatio = *cfg.MaxTokenMeanIncreaseRatio
	}
	if cfg.MaxTokenP95IncreaseRatio != nil {
		tolerances.MaxTokenP95IncreaseRatio = *cfg.MaxTokenP95IncreaseRatio
	}
	if cfg.MaxTokenClassIncreaseRatio != nil {
		tolerances.MaxTokenClassIncreaseRatio = *cfg.MaxTokenClassIncreaseRatio
	}
	if cfg.MaxTokenClassP95IncreaseRatio != nil {
		tolerances.MaxTokenClassP95IncreaseRatio = *cfg.MaxTokenClassP95IncreaseRatio
	}
	if cfg.MaxTokenQueryIncreaseRatio != nil {
		tolerances.MaxTokenQueryIncreaseRatio = *cfg.MaxTokenQueryIncreaseRatio
	}
	for dimension, override := range rules.Validation.SearchEval.DimensionRegression {
		current := dimensionTolerance(tolerances, dimension)
		if override.MinPassRateDelta != nil {
			current.MinPassRateDelta = *override.MinPassRateDelta
		}
		if override.MinRecallAt10Delta != nil {
			current.MinRecallAt10Delta = *override.MinRecallAt10Delta
		}
		tolerances.DimensionRegression[dimension] = current
	}
	return tolerances
}

func summarize(results []QueryResult) Summary {
	summary := Summary{QueryCount: len(results)}
	latencies := make([]int64, 0, len(results))
	zeroResults := 0
	tokenTotals := 0.0
	tokenSamples := make([]float64, 0, len(results))
	zeroResultTokens := 0.0
	for _, result := range results {
		if result.Passed {
			summary.PassCount++
		}
		if len(result.TopResults) == 0 {
			zeroResults++
			zeroResultTokens += float64(result.TokenEstimate.ResultTokens)
		} else {
			tokenTotals += result.TokenEstimate.TokensPerResult
			tokenSamples = append(tokenSamples, result.TokenEstimate.TokensPerResult)
		}
		latencies = append(latencies, result.LatencyMs)
	}
	summary.FailCount = summary.QueryCount - summary.PassCount
	if summary.QueryCount > 0 {
		summary.PassRate = float64(summary.PassCount) / float64(summary.QueryCount)
		summary.ZeroResultRate = float64(zeroResults) / float64(summary.QueryCount)
		if nonZeroResults := summary.QueryCount - zeroResults; nonZeroResults > 0 {
			summary.TokensPerResultMean = tokenTotals / float64(nonZeroResults)
		}
		if zeroResults > 0 {
			summary.ZeroResultResponseTokensMean = zeroResultTokens / float64(zeroResults)
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	summary.P50LatencyMs = percentile(latencies, 0.50)
	summary.P95LatencyMs = percentile(latencies, 0.95)
	sort.Float64s(tokenSamples)
	summary.TokensPerResultP95 = percentileFloat(tokenSamples, 0.95)
	return summary
}

func sampleMemoryBytes() uint64 {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return mem.HeapAlloc
}

func calculateMetrics(results []QueryResult) Metrics {
	var metrics Metrics
	if len(results) == 0 {
		return metrics
	}

	recall5 := 0
	recall10 := 0
	reciprocalRank := 0.0
	ndcg := 0.0
	firstUsefulTotal := 0
	firstUsefulCount := 0
	testPollution := 0
	exactTotal := 0
	exactPass := 0
	negativeTotal := 0
	negativePass := 0
	pdfTotal := 0
	pdfPass := 0
	pdfRecall10 := 0
	passCount := 0

	for _, result := range results {
		if result.Passed {
			passCount++
		}
		if result.FirstUsefulRank > 0 && result.FirstUsefulRank <= 5 {
			recall5++
		}
		if result.FirstUsefulRank > 0 && result.FirstUsefulRank <= 10 {
			recall10++
			reciprocalRank += 1 / float64(result.FirstUsefulRank)
		}
		if result.FirstUsefulRank > 0 {
			firstUsefulTotal += result.FirstUsefulRank
			firstUsefulCount++
			ndcg += ndcgAtK(result, 10)
		}
		if hasTestPollution(result.TopResults) {
			testPollution++
		}
		if result.Class == "exact_identifier" || result.Job == "exact_lookup" {
			exactTotal++
			if result.Passed {
				exactPass++
			}
		}
		if result.Class == "negative_adversarial" {
			negativeTotal++
			if result.Passed {
				negativePass++
			}
		}
		if isPDFQuery(result.Metadata) {
			pdfTotal++
			if result.Passed {
				pdfPass++
			}
			if result.FirstUsefulRank > 0 && result.FirstUsefulRank <= 10 {
				pdfRecall10++
			}
		}
	}

	total := float64(len(results))
	metrics.PassRate = float64(passCount) / total
	metrics.RecallAt5 = float64(recall5) / total
	metrics.RecallAt10 = float64(recall10) / total
	metrics.MRRAt10 = reciprocalRank / total
	metrics.NDCGAt10 = ndcg / total
	metrics.TestPollutionRate = float64(testPollution) / total
	if firstUsefulCount > 0 {
		metrics.FirstUsefulResultRankMean = float64(firstUsefulTotal) / float64(firstUsefulCount)
	}
	if exactTotal > 0 {
		metrics.ExactLookupPassRate = float64(exactPass) / float64(exactTotal)
	}
	if negativeTotal > 0 {
		metrics.NegativeAdversarialPassRate = float64(negativePass) / float64(negativeTotal)
	}
	if pdfTotal > 0 {
		metrics.PDFPassRate = float64(pdfPass) / float64(pdfTotal)
		metrics.PDFRecallAt10 = float64(pdfRecall10) / float64(pdfTotal)
	}
	return metrics
}

func groupMetrics(results []QueryResult, key func(QueryResult) string) map[string]Metrics {
	groups := make(map[string][]QueryResult)
	for _, result := range results {
		groups[key(result)] = append(groups[key(result)], result)
	}
	out := make(map[string]Metrics, len(groups))
	for name, group := range groups {
		out[name] = calculateMetrics(group)
	}
	return out
}

func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(len(sorted))*p)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func percentileFloat(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(len(sorted))*p)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func dcg(grade, rank int) float64 {
	return (math.Pow(2, float64(grade)) - 1) / math.Log2(float64(rank)+1)
}

func ndcgAtK(result QueryResult, k int) float64 {
	if k <= 0 || len(result.TopResults) == 0 || len(result.ExpectedResults) == 0 {
		return 0
	}
	limit := k
	if len(result.TopResults) < limit {
		limit = len(result.TopResults)
	}
	actualDCG := 0.0
	usedExpected := make(map[int]bool, len(result.ExpectedResults))
	for i := 0; i < limit; i++ {
		grade, expectedIndex := matchedUnusedGrade(result.ExpectedResults, result.TopResults[i], usedExpected)
		if grade > 0 {
			actualDCG += dcg(grade, i+1)
			usedExpected[expectedIndex] = true
		}
	}
	if actualDCG == 0 {
		return 0
	}

	idealGrades := make([]int, 0, len(result.ExpectedResults))
	for _, expected := range result.ExpectedResults {
		if expected.Grade > 0 {
			idealGrades = append(idealGrades, expected.Grade)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(idealGrades)))
	if len(idealGrades) > k {
		idealGrades = idealGrades[:k]
	}
	idealDCG := 0.0
	for i, grade := range idealGrades {
		idealDCG += dcg(grade, i+1)
	}
	if idealDCG == 0 {
		return 0
	}
	return actualDCG / idealDCG
}

func matchedUnusedGrade(expected []ExpectedResult, result SearchResult, used map[int]bool) (int, int) {
	best := 0
	bestIndex := -1
	for i, want := range expected {
		if used[i] {
			continue
		}
		if !matchesPath(result.Path, want.Path) {
			continue
		}
		if !matchesSymbol(want.Symbol, result.Symbol) {
			continue
		}
		if !matchesPageExpectation(want, result) {
			continue
		}
		if want.Grade > best {
			best = want.Grade
			bestIndex = i
		}
	}
	return best, bestIndex
}

func hasTestPollution(results []SearchResult) bool {
	for _, result := range results {
		if strings.Contains(result.Path, "_test.go") || strings.Contains(result.Path, "testdata/") {
			return true
		}
	}
	return false
}

func compareBaseline(opts Options, report *Report) error {
	if opts.SaveBaseline {
		return nil
	}
	var compareErrs []error
	baselinePath := opts.BaselinePath
	if baselinePath == "" {
		candidate := filepath.Join(opts.OutDir, "baseline.json")
		if _, err := os.Stat(candidate); err == nil {
			baselinePath = candidate
		}
	}
	if baselinePath != "" {
		if err := compareQualityBaseline(baselinePath, report); err != nil {
			compareErrs = append(compareErrs, err)
		}
	} else if opts.FailOnRegression && report.ExactLookupGate.Required {
		report.ExactLookupGate.Passed = false
		report.BaselineComparison.RegressionReasons = append(report.BaselineComparison.RegressionReasons, "exact lookup baseline missing")
	}

	if opts.TokenBudgetEnabled {
		tokenBaselinePath := opts.TokenBaselinePath
		if tokenBaselinePath == "" {
			candidate := filepath.Join(opts.OutDir, "tokens-baseline.json")
			if _, err := os.Stat(candidate); err == nil {
				tokenBaselinePath = candidate
			}
		}
		if tokenBaselinePath != "" {
			if err := compareTokenBaseline(tokenBaselinePath, report); err != nil {
				compareErrs = append(compareErrs, err)
			}
		}
	}

	refreshGraphEvalGate(report)
	report.BaselineComparison.Regressed = len(report.BaselineComparison.RegressionReasons) > 0
	if len(compareErrs) > 0 {
		return fmt.Errorf("baseline comparison failed: %w", compareErrs[0])
	}
	return nil
}

func refreshGraphEvalGate(report *Report) {
	baselineSource := graphBaselineSource
	if report.BaselineComparison.BaselinePath != "" {
		baselineSource = report.BaselineComparison.BaselinePath
	}
	report.GraphEvalGate = buildGraphEvalGate(report.Queries, report.ExactLookupGate, baselineSource)
	if report.GraphEvalGate.Required && !report.GraphEvalGate.Passed {
		report.BaselineComparison.RegressionReasons = appendUniqueString(report.BaselineComparison.RegressionReasons, "graph eval gate failed")
	}
}

func compareQualityBaseline(baselinePath string, report *Report) error {
	data, err := os.ReadFile(baselinePath)
	if err != nil {
		report.BaselineComparison.RegressionReasons = append(report.BaselineComparison.RegressionReasons, "baseline read failed")
		return fmt.Errorf("failed to read baseline %s: %w", baselinePath, err)
	}
	var baseline Report
	if err := json.Unmarshal(data, &baseline); err != nil {
		report.BaselineComparison.BaselinePath = baselinePath
		report.BaselineComparison.RegressionReasons = append(report.BaselineComparison.RegressionReasons, "baseline parse failed")
		return fmt.Errorf("failed to parse baseline %s: %w", baselinePath, err)
	}
	comparison := &report.BaselineComparison
	comparison.BaselinePath = baselinePath
	comparison.Compared = true
	comparison.PassRateDelta = report.Summary.PassRate - baseline.Summary.PassRate
	comparison.RecallAt10Delta = report.Metrics.RecallAt10 - baseline.Metrics.RecallAt10
	comparison.MRRAt10Delta = report.Metrics.MRRAt10 - baseline.Metrics.MRRAt10
	comparison.NDCGAt10Delta = report.Metrics.NDCGAt10 - baseline.Metrics.NDCGAt10
	comparison.P95LatencyMsDelta = report.Summary.P95LatencyMs - baseline.Summary.P95LatencyMs
	comparison.TokensPerResultMeanDelta = report.Summary.TokensPerResultMean - baseline.Summary.TokensPerResultMean
	comparison.TokensPerResultP95Delta = report.Summary.TokensPerResultP95 - baseline.Summary.TokensPerResultP95

	if comparison.PassRateDelta < report.Run.Tolerances.MinPassRateDelta {
		comparison.RegressionReasons = append(comparison.RegressionReasons, "pass rate decreased")
	}
	if comparison.RecallAt10Delta < report.Run.Tolerances.MinRecallAt10Delta {
		comparison.RegressionReasons = append(comparison.RegressionReasons, "recall@10 decreased")
	}
	if baseline.Summary.P95LatencyMs > 0 && comparison.P95LatencyMsDelta > report.Run.Tolerances.MaxP95LatencyMsDelta {
		comparison.RegressionReasons = append(comparison.RegressionReasons, "p95 latency increased")
	}
	compareExactLookupGate(&baseline, report)
	compareDimensionBaselines(&baseline, report)
	return nil
}

func compareDimensionBaselines(baseline, report *Report) {
	specs := []struct {
		name     string
		current  map[string]Metrics
		baseline map[string]Metrics
	}{
		{name: "class", current: metricsByClass(report), baseline: metricsByClass(baseline)},
		{name: "job", current: metricsByJob(report), baseline: metricsByJob(baseline)},
		{name: "profile", current: metricsByProfile(report), baseline: metricsByProfile(baseline)},
		{name: "source_class", current: metricsBySourceClass(report), baseline: metricsBySourceClass(baseline)},
		{name: "language", current: metricsByLanguage(report), baseline: metricsByLanguage(baseline)},
	}

	for _, spec := range specs {
		tolerance := dimensionTolerance(report.Run.Tolerances, spec.name)
		for group, currentMetrics := range spec.current {
			baselineMetrics, ok := spec.baseline[group]
			if !ok {
				continue
			}
			report.DimensionRegressions = append(report.DimensionRegressions,
				dimensionRegressionCheck(spec.name, group, "pass_rate", baselineMetrics.PassRate, currentMetrics.PassRate, tolerance.MinPassRateDelta),
				dimensionRegressionCheck(spec.name, group, "recall_at_10", baselineMetrics.RecallAt10, currentMetrics.RecallAt10, tolerance.MinRecallAt10Delta),
			)
		}
	}

	for _, regression := range report.DimensionRegressions {
		if !regression.Regressed {
			continue
		}
		report.BaselineComparison.RegressionReasons = appendUniqueString(
			report.BaselineComparison.RegressionReasons,
			fmt.Sprintf("dimension regression: %s %s %s decreased", regression.Dimension, regression.Group, metricDisplayName(regression.Metric)),
		)
	}
}

func dimensionRegressionCheck(dimension, group, metric string, baselineValue, currentValue, tolerance float64) DimensionRegression {
	delta := currentValue - baselineValue
	return DimensionRegression{
		Dimension:     dimension,
		Group:         group,
		Metric:        metric,
		BaselineValue: baselineValue,
		CurrentValue:  currentValue,
		Delta:         delta,
		Tolerance:     tolerance,
		Regressed:     delta < tolerance,
	}
}

func dimensionTolerance(tolerances Tolerances, dimension string) DimensionTolerance {
	if tolerances.DimensionRegression != nil {
		if tolerance, ok := tolerances.DimensionRegression[dimension]; ok {
			return tolerance
		}
	}
	return DimensionTolerance{MinPassRateDelta: tolerances.MinPassRateDelta, MinRecallAt10Delta: tolerances.MinRecallAt10Delta}
}

func metricDisplayName(metric string) string {
	switch metric {
	case "pass_rate":
		return "pass rate"
	case "recall_at_10":
		return "recall@10"
	default:
		return metric
	}
}

func metricsByClass(report *Report) map[string]Metrics {
	if len(report.Queries) > 0 {
		return groupMetrics(report.Queries, func(result QueryResult) string { return result.Class })
	}
	if len(report.ByClass) > 0 {
		return report.ByClass
	}
	return nil
}

func metricsByJob(report *Report) map[string]Metrics {
	if len(report.Queries) > 0 {
		return groupMetrics(report.Queries, func(result QueryResult) string { return result.Job })
	}
	if len(report.ByJob) > 0 {
		return report.ByJob
	}
	return nil
}

func metricsByProfile(report *Report) map[string]Metrics {
	if len(report.Queries) > 0 {
		return groupMetrics(report.Queries, resultProfile)
	}
	if len(report.ByProfile) > 0 {
		return report.ByProfile
	}
	return nil
}

func metricsBySourceClass(report *Report) map[string]Metrics {
	if len(report.Queries) > 0 {
		return groupMetrics(report.Queries, resultSourceClass)
	}
	if len(report.BySourceClass) > 0 {
		return report.BySourceClass
	}
	return nil
}

func metricsByLanguage(report *Report) map[string]Metrics {
	if len(report.Queries) > 0 {
		return groupMetrics(report.Queries, resultLanguage)
	}
	if len(report.ByLanguage) > 0 {
		return report.ByLanguage
	}
	return nil
}

func currentExactLookupGate(results []QueryResult) ExactLookupGate {
	exactResults := exactLookupResults(results)
	gate := ExactLookupGate{
		Required:          len(exactResults) > 0,
		Passed:            true,
		CurrentQueryCount: len(exactResults),
		Classes:           exactGateMetrics(exactResults),
	}
	for _, result := range exactResults {
		if !result.Passed {
			gate.Passed = false
			break
		}
	}
	return gate
}

func compareExactLookupGate(baseline *Report, report *Report) {
	current := exactLookupResults(report.Queries)
	baselineResults := exactLookupResults(baseline.Queries)
	report.ExactLookupGate.Required = len(current) > 0
	report.ExactLookupGate.Compared = true
	report.ExactLookupGate.CurrentQueryCount = len(current)
	report.ExactLookupGate.BaselineQueryCount = len(baselineResults)
	report.ExactLookupGate.Classes = exactGateMetrics(current)
	report.ExactLookupGate.Passed = true

	if len(current) == 0 {
		return
	}
	if len(baselineResults) == 0 {
		report.ExactLookupGate.Passed = false
		report.ExactLookupGate.Failures = append(report.ExactLookupGate.Failures, ExactLookupGateFailure{
			Reason: "baseline contains no exact lookup queries",
		})
		report.BaselineComparison.RegressionReasons = append(report.BaselineComparison.RegressionReasons, "exact lookup baseline missing")
		return
	}

	baselineByID := make(map[string]QueryResult, len(baselineResults))
	for _, result := range baselineResults {
		baselineByID[result.ID] = result
	}

	for _, currentResult := range current {
		baselineResult, ok := baselineByID[currentResult.ID]
		if !ok {
			continue
		}
		report.ExactLookupGate.Failures = append(report.ExactLookupGate.Failures, exactGateFailures(baselineResult, currentResult)...)
	}

	if len(report.ExactLookupGate.Failures) > 0 {
		report.ExactLookupGate.Passed = false
		report.BaselineComparison.RegressionReasons = append(report.BaselineComparison.RegressionReasons, "exact lookup gate failed")
	}
}

func exactLookupResults(results []QueryResult) []QueryResult {
	out := make([]QueryResult, 0, len(results))
	for _, result := range results {
		if isExactLookupResult(result) {
			out = append(out, result)
		}
	}
	return out
}

func isExactLookupResult(result QueryResult) bool {
	return result.Job == "exact_lookup" ||
		result.Class == "path_lookup" ||
		result.Class == "quoted_string"
}

func exactGateMetrics(results []QueryResult) map[string]ExactGateMetric {
	if len(results) == 0 {
		return nil
	}
	type counts struct {
		total  int
		passed int
	}
	byClass := make(map[string]counts)
	for _, result := range results {
		c := byClass[result.Class]
		c.total++
		if result.Passed {
			c.passed++
		}
		byClass[result.Class] = c
	}
	metrics := make(map[string]ExactGateMetric, len(byClass))
	for class, count := range byClass {
		passRate := 0.0
		if count.total > 0 {
			passRate = float64(count.passed) / float64(count.total)
		}
		metrics[class] = ExactGateMetric{QueryCount: count.total, PassRate: passRate}
	}
	return metrics
}

type exactMatchSnapshot struct {
	rank     int
	path     string
	resultID string
}

func exactMatch(result QueryResult) (exactMatchSnapshot, bool) {
	bestGrade := 0
	var best exactMatchSnapshot
	for rank, item := range result.TopResults {
		grade := matchedGrade(result.ExpectedResults, item)
		if grade < 2 {
			continue
		}
		if grade > bestGrade || (grade == bestGrade && (best.rank == 0 || rank+1 < best.rank)) {
			bestGrade = grade
			best = exactMatchSnapshot{
				rank:     rank + 1,
				path:     strings.Trim(strings.TrimSpace(item.Path), "/"),
				resultID: item.ResultID,
			}
		}
	}
	if bestGrade == 0 {
		return exactMatchSnapshot{}, false
	}
	return best, true
}

func exactGateFailures(baseline, current QueryResult) []ExactLookupGateFailure {
	base, baseOK := exactMatch(baseline)
	if !baseOK {
		return []ExactLookupGateFailure{{
			QueryID: baseline.ID,
			Reason:  "baseline exact hit missing",
		}}
	}
	if base.resultID == "" {
		return []ExactLookupGateFailure{{
			QueryID:      baseline.ID,
			Reason:       "baseline result id missing",
			BaselineRank: base.rank,
			BaselinePath: base.path,
		}}
	}

	got, gotOK := exactMatch(current)
	if !gotOK {
		return []ExactLookupGateFailure{{
			QueryID:          current.ID,
			Reason:           "current exact hit missing",
			BaselineRank:     base.rank,
			BaselinePath:     base.path,
			BaselineResultID: base.resultID,
		}}
	}

	var failures []ExactLookupGateFailure
	if got.rank > base.rank {
		failures = append(failures, ExactLookupGateFailure{
			QueryID:      current.ID,
			Reason:       "exact hit rank demoted",
			BaselineRank: base.rank,
			CurrentRank:  got.rank,
			BaselinePath: base.path,
			CurrentPath:  got.path,
		})
	}
	if got.path != base.path {
		failures = append(failures, ExactLookupGateFailure{
			QueryID:      current.ID,
			Reason:       "exact hit source path changed",
			BaselineRank: base.rank,
			CurrentRank:  got.rank,
			BaselinePath: base.path,
			CurrentPath:  got.path,
		})
	}
	if got.resultID == "" {
		failures = append(failures, ExactLookupGateFailure{
			QueryID:      current.ID,
			Reason:       "current result id missing",
			BaselineRank: base.rank,
			CurrentRank:  got.rank,
			BaselinePath: base.path,
			CurrentPath:  got.path,
		})
	}
	return failures
}

func compareTokenBaseline(path string, report *Report) error {
	data, err := os.ReadFile(path)
	if err != nil {
		report.BaselineComparison.RegressionReasons = append(report.BaselineComparison.RegressionReasons, "token baseline read failed")
		return fmt.Errorf("failed to read token baseline %s: %w", path, err)
	}
	var baseline tokenBaseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		report.BaselineComparison.TokenBaselinePath = path
		report.BaselineComparison.RegressionReasons = append(report.BaselineComparison.RegressionReasons, "token baseline parse failed")
		return fmt.Errorf("failed to parse token baseline %s: %w", path, err)
	}

	comparison := &report.BaselineComparison
	comparison.TokenBaselinePath = path
	comparison.TokenBudgetCompared = true
	currentOverall := tokenStats(report.Queries)
	comparison.TokensPerResultMeanDelta = currentOverall.MeanTokensPerResult - baseline.Overall.MeanTokensPerResult
	comparison.TokensPerResultP95Delta = currentOverall.P95TokensPerResult - baseline.Overall.P95TokensPerResult

	reasons := tokenRegressionReasons(report, baseline)
	comparison.RegressionReasons = append(comparison.RegressionReasons, reasons...)
	return nil
}

func writeReport(opts Options, report *Report) error {
	if opts.Output != "json" && opts.Output != "markdown" && opts.Output != "both" {
		return fmt.Errorf("unsupported output %q", opts.Output)
	}
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", opts.OutDir, err)
	}
	if opts.Output == "json" || opts.Output == "both" {
		report.OutputPaths.JSON = filepath.Join(opts.OutDir, "latest.json")
	}
	if opts.Output == "markdown" || opts.Output == "both" {
		report.OutputPaths.Markdown = filepath.Join(opts.OutDir, "latest.md")
	}
	if opts.Output == "json" || opts.Output == "both" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to encode JSON report: %w", err)
		}
		if err := os.WriteFile(report.OutputPaths.JSON, append(data, '\n'), 0o644); err != nil {
			return fmt.Errorf("failed to write JSON report %s: %w", report.OutputPaths.JSON, err)
		}
		if opts.SaveBaseline {
			baselinePath := filepath.Join(opts.OutDir, "baseline.json")
			if err := refuseBaselineOverwrite(baselinePath, opts); err != nil {
				return err
			}
			if err := os.WriteFile(baselinePath, append(data, '\n'), 0o644); err != nil {
				return fmt.Errorf("failed to write JSON baseline %s: %w", baselinePath, err)
			}
		}
	}
	if opts.Output == "markdown" || opts.Output == "both" {
		markdown := markdownReport(report)
		if err := os.WriteFile(report.OutputPaths.Markdown, []byte(markdown), 0o644); err != nil {
			return fmt.Errorf("failed to write Markdown report %s: %w", report.OutputPaths.Markdown, err)
		}
		if opts.SaveBaseline {
			baselinePath := filepath.Join(opts.OutDir, "baseline.md")
			if err := refuseBaselineOverwrite(baselinePath, opts); err != nil {
				return err
			}
			if err := os.WriteFile(baselinePath, []byte(markdown), 0o644); err != nil {
				return fmt.Errorf("failed to write Markdown baseline %s: %w", baselinePath, err)
			}
		}
	}
	if opts.SaveBaseline {
		if err := writeTokenBaseline(opts, report); err != nil {
			return err
		}
	}
	return nil
}

func markdownReport(report *Report) string {
	var b strings.Builder
	b.WriteString("# Search Eval Report\n\n")
	b.WriteString("## Summary\n\n")
	fmt.Fprintf(&b, "- Queries: %d\n", report.Summary.QueryCount)
	fmt.Fprintf(&b, "- Passed: %d\n", report.Summary.PassCount)
	fmt.Fprintf(&b, "- Failed: %d\n", report.Summary.FailCount)
	fmt.Fprintf(&b, "- Pass rate: %.2f\n", report.Summary.PassRate)
	fmt.Fprintf(&b, "- Recall@10: %.2f\n", report.Metrics.RecallAt10)
	fmt.Fprintf(&b, "- MRR@10: %.2f\n", report.Metrics.MRRAt10)
	fmt.Fprintf(&b, "- nDCG@10: %.2f\n", report.Metrics.NDCGAt10)
	fmt.Fprintf(&b, "- Negative adversarial pass rate: %.2f\n", report.Metrics.NegativeAdversarialPassRate)
	fmt.Fprintf(&b, "- PDF pass rate: %.2f\n", report.Metrics.PDFPassRate)
	fmt.Fprintf(&b, "- PDF recall@10: %.2f\n", report.Metrics.PDFRecallAt10)
	fmt.Fprintf(&b, "- p95 latency: %d ms\n", report.Summary.P95LatencyMs)
	fmt.Fprintf(&b, "- Tokens/result mean: %.2f\n", report.Summary.TokensPerResultMean)
	fmt.Fprintf(&b, "- Tokens/result p95: %.2f\n\n", report.Summary.TokensPerResultP95)

	b.WriteString("## Baseline Comparison\n\n")
	if report.BaselineComparison.Compared || report.BaselineComparison.TokenBudgetCompared {
		fmt.Fprintf(&b, "- Baseline: `%s`\n", report.BaselineComparison.BaselinePath)
		fmt.Fprintf(&b, "- Token baseline: `%s`\n", report.BaselineComparison.TokenBaselinePath)
		fmt.Fprintf(&b, "- Regressed: %t\n", report.BaselineComparison.Regressed)
		fmt.Fprintf(&b, "- Pass rate delta: %.2f\n", report.BaselineComparison.PassRateDelta)
		fmt.Fprintf(&b, "- Recall@10 delta: %.2f\n", report.BaselineComparison.RecallAt10Delta)
		fmt.Fprintf(&b, "- Tokens/result mean delta: %.2f\n", report.BaselineComparison.TokensPerResultMeanDelta)
		fmt.Fprintf(&b, "- Tokens/result p95 delta: %.2f\n", report.BaselineComparison.TokensPerResultP95Delta)
		if len(report.BaselineComparison.RegressionReasons) > 0 {
			fmt.Fprintf(&b, "- Reasons: %s\n", strings.Join(report.BaselineComparison.RegressionReasons, "; "))
		}
		b.WriteString("\n")
	} else {
		b.WriteString("- No baseline comparison.\n\n")
	}

	b.WriteString("## Dimension Regressions\n\n")
	if len(report.DimensionRegressions) == 0 {
		b.WriteString("- No dimension baseline comparison.\n\n")
	} else {
		b.WriteString("| Dimension | Group | Metric | Baseline | Current | Delta | Tolerance | Regressed |\n")
		b.WriteString("|---|---|---|---:|---:|---:|---:|---:|\n")
		for _, regression := range report.DimensionRegressions {
			fmt.Fprintf(&b, "| %s | %s | %s | %.2f | %.2f | %.2f | %.4f | %t |\n",
				regression.Dimension,
				escapeTable(regression.Group),
				regression.Metric,
				regression.BaselineValue,
				regression.CurrentValue,
				regression.Delta,
				regression.Tolerance,
				regression.Regressed,
			)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Graph Eval Gate\n\n")
	fmt.Fprintf(&b, "- Required: %t\n", report.GraphEvalGate.Required)
	fmt.Fprintf(&b, "- Compared: %t\n", report.GraphEvalGate.Compared)
	fmt.Fprintf(&b, "- Passed: %t\n", report.GraphEvalGate.Passed)
	fmt.Fprintf(&b, "- Recommendation: %s\n", report.GraphEvalGate.Recommendation)
	fmt.Fprintf(&b, "- Recommendation target: `%s`\n", report.GraphEvalGate.RecommendationTarget)
	fmt.Fprintf(&b, "- Evaluation scope: `%s`\n", report.GraphEvalGate.EvaluationScope)
	fmt.Fprintf(&b, "- Measured tool: `%s`\n", report.GraphEvalGate.MeasuredTool)
	fmt.Fprintf(&b, "- Graph tool measured: %t\n", report.GraphEvalGate.GraphToolMeasured)
	fmt.Fprintf(&b, "- Baseline source: `%s`\n", report.GraphEvalGate.BaselineSource)
	fmt.Fprintf(&b, "- Target recall@10 lift: %.2f\n", report.GraphEvalGate.TargetRecallAt10Delta)
	fmt.Fprintf(&b, "- Kill/defer recall@10 lift threshold: %.2f\n", report.GraphEvalGate.KillRecallAt10Delta)
	fmt.Fprintf(&b, "- Low-baseline absolute floor: %.2f when baseline <= %.2f\n", report.GraphEvalGate.LowBaselineAbsoluteFloor, report.GraphEvalGate.LowBaselineThreshold)
	fmt.Fprintf(&b, "- Graph queries: %d\n", report.GraphEvalGate.CurrentQueryCount)
	fmt.Fprintf(&b, "- Graph tokens/result mean: %.2f\n", report.GraphEvalGate.TokenMetrics.MeanTokensPerResult)
	fmt.Fprintf(&b, "- Graph tokens/result p95: %.2f\n", report.GraphEvalGate.TokenMetrics.P95TokensPerResult)
	if len(report.GraphEvalGate.Reasons) > 0 {
		fmt.Fprintf(&b, "- Reasons: %s\n", strings.Join(report.GraphEvalGate.Reasons, "; "))
	}
	if len(report.GraphEvalGate.Classes) > 0 {
		b.WriteString("\n| Class | Queries | Baseline recall@10 floor | Current recall@10 | Lift | Recommendation | Reasons |\n")
		b.WriteString("|---|---:|---:|---:|---:|---|---|\n")
		names := make([]string, 0, len(report.GraphEvalGate.Classes))
		for name := range report.GraphEvalGate.Classes {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			classGate := report.GraphEvalGate.Classes[name]
			fmt.Fprintf(&b, "| %s | %d | %.2f | %.2f | %.2f | %s | %s |\n",
				name,
				classGate.QueryCount,
				classGate.BaselineRecallAt10Floor,
				classGate.CurrentRecallAt10,
				classGate.RecallAt10Delta,
				classGate.Recommendation,
				escapeTable(strings.Join(classGate.Reasons, "; ")),
			)
		}
	}
	b.WriteString("\n")

	b.WriteString("## Exact Lookup Gate\n\n")
	fmt.Fprintf(&b, "- Required: %t\n", report.ExactLookupGate.Required)
	fmt.Fprintf(&b, "- Compared: %t\n", report.ExactLookupGate.Compared)
	fmt.Fprintf(&b, "- Passed: %t\n", report.ExactLookupGate.Passed)
	fmt.Fprintf(&b, "- Baseline queries: %d\n", report.ExactLookupGate.BaselineQueryCount)
	fmt.Fprintf(&b, "- Current queries: %d\n", report.ExactLookupGate.CurrentQueryCount)
	if len(report.ExactLookupGate.Failures) > 0 {
		b.WriteString("\n| Query | Reason | Baseline rank | Current rank | Baseline path | Current path |\n")
		b.WriteString("|---|---|---:|---:|---|---|\n")
		for _, failure := range report.ExactLookupGate.Failures {
			fmt.Fprintf(&b, "| %s | %s | %d | %d | %s | %s |\n",
				failure.QueryID,
				escapeTable(failure.Reason),
				failure.BaselineRank,
				failure.CurrentRank,
				escapeTable(failure.BaselinePath),
				escapeTable(failure.CurrentPath),
			)
		}
	}
	b.WriteString("\n")

	b.WriteString("## By Class\n\n")
	writeMetricTable(&b, report.ByClass)
	b.WriteString("## By Job\n\n")
	writeMetricTable(&b, report.ByJob)
	b.WriteString("## By Profile\n\n")
	writeMetricTable(&b, report.ByProfile)
	b.WriteString("## By Source Class\n\n")
	writeMetricTable(&b, report.BySourceClass)
	b.WriteString("## By Language\n\n")
	writeMetricTable(&b, report.ByLanguage)

	b.WriteString("## Query Results\n\n")
	b.WriteString("| Query | Class | Job | Passed | First Useful | Top Results | Failure |\n")
	b.WriteString("|---|---|---|---:|---:|---|---|\n")
	for _, query := range report.Queries {
		fmt.Fprintf(&b, "| %s | %s | %s | %t | %d | %s | %s |\n",
			query.ID,
			query.Class,
			query.Job,
			query.Passed,
			query.FirstUsefulRank,
			escapeTable(topResultPaths(query.TopResults)),
			escapeTable(query.FailureReason),
		)
	}
	return b.String()
}

func writeMetricTable(b *strings.Builder, metrics map[string]Metrics) {
	b.WriteString("| Group | Pass Rate | Recall@10 | MRR@10 | nDCG@10 | Exact Pass | Negative Pass | PDF Pass | PDF Recall@10 | Test Pollution |\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	names := make([]string, 0, len(metrics))
	for name := range metrics {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		metric := metrics[name]
		fmt.Fprintf(b, "| %s | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f |\n",
			name,
			metric.PassRate,
			metric.RecallAt10,
			metric.MRRAt10,
			metric.NDCGAt10,
			metric.ExactLookupPassRate,
			metric.NegativeAdversarialPassRate,
			metric.PDFPassRate,
			metric.PDFRecallAt10,
			metric.TestPollutionRate,
		)
	}
	b.WriteString("\n")
}

func topResultPaths(results []SearchResult) string {
	paths := make([]string, 0, len(results))
	for _, result := range results {
		paths = append(paths, result.Path)
	}
	if len(paths) > 5 {
		paths = paths[:5]
	}
	return strings.Join(paths, "<br>")
}

func escapeTable(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

type tokenBaseline struct {
	SchemaVersion  int                         `json:"schema_version"`
	GeneratedAt    string                      `json:"generated_at,omitempty"`
	GitCommit      string                      `json:"git_commit,omitempty"`
	EvalCommand    string                      `json:"eval_command,omitempty"`
	CorpusPath     string                      `json:"corpus_path,omitempty"`
	CorpusVersion  string                      `json:"corpus_version,omitempty"`
	ResultLimit    int                         `json:"result_limit,omitempty"`
	ConfigProfile  string                      `json:"config_profile,omitempty"`
	EmbedderMode   string                      `json:"embedder_mode,omitempty"`
	TokenEstimator string                      `json:"token_estimator,omitempty"`
	BudgetContract tokenBudgetContract         `json:"budget_contract,omitempty"`
	Overall        tokenBudgetStats            `json:"overall"`
	Tools          map[string]tokenBudgetStats `json:"tools"`
	QueryClasses   map[string]tokenBudgetStats `json:"query_classes"`
	Queries        []tokenBaselineQuery        `json:"queries,omitempty"`
	Outliers       []tokenBaselineQuery        `json:"outliers,omitempty"`
}

type tokenBudgetContract struct {
	CompactOutputMeanAndP95Budget string `json:"compact_output_mean_and_p95_budget"`
	QueryClassReviewThreshold     string `json:"query_class_review_threshold"`
	VerboseExplainMode            string `json:"verbose_explain_mode"`
	ZeroResultCost                string `json:"zero_result_cost"`
}

type tokenBudgetStats struct {
	Count                 int     `json:"count"`
	ZeroResultCount       int     `json:"zero_result_count"`
	MeanTokensPerResult   float64 `json:"mean_tokens_per_result"`
	MedianTokensPerResult float64 `json:"median_tokens_per_result"`
	P95TokensPerResult    float64 `json:"p95_tokens_per_result"`
	MaxTokensPerResult    float64 `json:"max_tokens_per_result"`
	MeanResponseTokens    float64 `json:"mean_response_tokens"`
	P95ResponseTokens     int     `json:"p95_response_tokens"`
	MaxResponseTokens     int     `json:"max_response_tokens"`
	MeanResponseBytes     float64 `json:"mean_response_bytes"`
	P95ResponseBytes      int     `json:"p95_response_bytes"`
	MaxResponseBytes      int     `json:"max_response_bytes"`
}

type tokenBaselineQuery struct {
	ID              string  `json:"id"`
	Name            string  `json:"name,omitempty"`
	Tool            string  `json:"tool"`
	Class           string  `json:"class"`
	Job             string  `json:"job,omitempty"`
	Holdout         bool    `json:"holdout,omitempty"`
	ResultCount     int     `json:"result_count"`
	ResponseBytes   int     `json:"response_bytes"`
	ResponseTokens  int     `json:"response_tokens"`
	TokensPerResult float64 `json:"tokens_per_result"`
}

func writeTokenBaseline(opts Options, report *Report) error {
	baseline := buildTokenBaseline(opts, report)
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode token baseline: %w", err)
	}
	jsonPath := filepath.Join(opts.OutDir, "tokens-baseline.json")
	if err := refuseBaselineOverwrite(jsonPath, opts); err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("failed to write token baseline %s: %w", jsonPath, err)
	}
	mdPath := filepath.Join(opts.OutDir, "tokens-baseline.md")
	if err := refuseBaselineOverwrite(mdPath, opts); err != nil {
		return err
	}
	if err := os.WriteFile(mdPath, []byte(markdownTokenBaseline(baseline)), 0o644); err != nil {
		return fmt.Errorf("failed to write token baseline summary %s: %w", mdPath, err)
	}
	return nil
}

func refuseBaselineOverwrite(path string, opts Options) error {
	if opts.ForceOverwriteBaseline {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("refusing to overwrite existing baseline %s without --force-overwrite-baseline", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to inspect baseline path %s: %w", path, err)
	}
	return nil
}

func buildTokenBaseline(opts Options, report *Report) tokenBaseline {
	queries := make([]tokenBaselineQuery, 0, len(report.Queries))
	for _, result := range report.Queries {
		queries = append(queries, tokenBaselineQuery{
			ID:              result.ID,
			Name:            result.Name,
			Tool:            result.Tool,
			Class:           result.Class,
			Job:             result.Job,
			Holdout:         result.Holdout,
			ResultCount:     len(result.TopResults),
			ResponseBytes:   result.TokenEstimate.ResponseBytes,
			ResponseTokens:  result.TokenEstimate.ResultTokens,
			TokensPerResult: result.TokenEstimate.TokensPerResult,
		})
	}
	outliers := append([]tokenBaselineQuery(nil), queries...)
	sort.Slice(outliers, func(i, j int) bool {
		return outliers[i].TokensPerResult > outliers[j].TokensPerResult
	})
	if len(outliers) > 10 {
		outliers = outliers[:10]
	}

	return tokenBaseline{
		SchemaVersion:  1,
		GeneratedAt:    report.Run.Timestamp.Format(time.RFC3339Nano),
		GitCommit:      report.Run.GitSHA,
		EvalCommand:    report.Run.Command,
		CorpusPath:     report.Run.CorpusPath,
		CorpusVersion:  corpusVersion(opts.CorpusPath),
		ResultLimit:    10,
		ConfigProfile:  "local-index",
		EmbedderMode:   report.Run.Embedder,
		TokenEstimator: "utf8-json-bytes-v1",
		BudgetContract: tokenBudgetContract{
			CompactOutputMeanAndP95Budget: "+10%",
			QueryClassReviewThreshold:     "+15%",
			VerboseExplainMode:            "excluded from compact-output budget",
			ZeroResultCost:                "reported separately and excluded from tokens-per-result denominator when result_count is zero",
		},
		Overall:      tokenStats(report.Queries),
		Tools:        tokenStatsBy(report.Queries, func(result QueryResult) string { return result.Tool }),
		QueryClasses: tokenStatsBy(report.Queries, func(result QueryResult) string { return result.Class }),
		Queries:      queries,
		Outliers:     outliers,
	}
}

func markdownTokenBaseline(baseline tokenBaseline) string {
	var b strings.Builder
	b.WriteString("# Tokens-Per-Result Baseline\n\n")
	fmt.Fprintf(&b, "- Generated: %s\n", baseline.GeneratedAt)
	fmt.Fprintf(&b, "- Git: %s\n", baseline.GitCommit)
	fmt.Fprintf(&b, "- Command: `%s`\n", baseline.EvalCommand)
	fmt.Fprintf(&b, "- Corpus: `%s` (%s)\n", baseline.CorpusPath, baseline.CorpusVersion)
	fmt.Fprintf(&b, "- Estimator: `%s`\n", baseline.TokenEstimator)
	fmt.Fprintf(&b, "- Queries: %d\n", baseline.Overall.Count)
	fmt.Fprintf(&b, "- Mean tokens/result: %.2f\n", baseline.Overall.MeanTokensPerResult)
	fmt.Fprintf(&b, "- p95 tokens/result: %.2f\n", baseline.Overall.P95TokensPerResult)
	fmt.Fprintf(&b, "- Zero-result count: %d\n\n", baseline.Overall.ZeroResultCount)
	b.WriteString("## Budget Contract\n\n")
	b.WriteString("- Later compact-output changes must stay within +10% of baseline mean and p95 tokens/result by tool and query class.\n")
	b.WriteString("- Any query class above +15% needs explicit accepted quality evidence.\n")
	b.WriteString("- Verbose/explain metadata remains opt-in and outside this compact-output budget.\n\n")
	b.WriteString("## Top Outliers\n\n")
	b.WriteString("| Query | Tool | Class | Results | Response bytes | Response tokens | Tokens/result |\n")
	b.WriteString("|---|---|---|---:|---:|---:|---:|\n")
	for _, outlier := range baseline.Outliers {
		fmt.Fprintf(&b, "| %s | %s | %s | %d | %d | %d | %.2f |\n",
			outlier.ID,
			outlier.Tool,
			outlier.Class,
			outlier.ResultCount,
			outlier.ResponseBytes,
			outlier.ResponseTokens,
			outlier.TokensPerResult,
		)
	}
	return b.String()
}

func tokenRegressionReasons(report *Report, baseline tokenBaseline) []string {
	var reasons []string
	tolerances := report.Run.Tolerances
	currentQueries := make(map[string]tokenBaselineQuery, len(report.Queries))
	for _, result := range report.Queries {
		currentQueries[result.ID] = tokenBaselineQuery{
			ID:              result.ID,
			Tool:            result.Tool,
			Class:           result.Class,
			ResultCount:     len(result.TopResults),
			TokensPerResult: result.TokenEstimate.TokensPerResult,
		}
	}
	for _, baselineQuery := range baseline.Queries {
		current, ok := currentQueries[baselineQuery.ID]
		if !ok || current.ResultCount == 0 || baselineQuery.ResultCount == 0 || baselineQuery.TokensPerResult == 0 {
			continue
		}
		limit := baselineQuery.TokensPerResult * (1 + tolerances.MaxTokenQueryIncreaseRatio)
		if current.TokensPerResult > limit {
			reasons = append(reasons, fmt.Sprintf("query %s tokens/result increased", current.ID))
		}
	}

	currentTools, baselineTools := commonTokenStatsBy(report, baseline, func(result QueryResult) string { return result.Tool })
	for tool, baselineStats := range baselineTools {
		current, ok := currentTools[tool]
		if !ok {
			continue
		}
		reasons = append(reasons, tokenStatsRegressionReasons("tool "+tool, current, baselineStats, tolerances.MaxTokenMeanIncreaseRatio, tolerances.MaxTokenP95IncreaseRatio)...)
	}

	currentClasses, baselineClasses := commonTokenStatsBy(report, baseline, func(result QueryResult) string { return result.Class })
	for class, baselineStats := range baselineClasses {
		current, ok := currentClasses[class]
		if !ok {
			continue
		}
		reasons = append(reasons, tokenStatsRegressionReasons("class "+class, current, baselineStats, tolerances.MaxTokenClassIncreaseRatio, tolerances.MaxTokenClassP95IncreaseRatio)...)
	}
	for i, reason := range reasons {
		reasons[i] = "token budget " + reason
	}
	return reasons
}

func commonTokenStatsBy(report *Report, baseline tokenBaseline, key func(QueryResult) string) (map[string]tokenBudgetStats, map[string]tokenBudgetStats) {
	currentByID := make(map[string]QueryResult, len(report.Queries))
	for _, result := range report.Queries {
		currentByID[result.ID] = result
	}

	currentCommon := make([]QueryResult, 0, len(baseline.Queries))
	baselineCommon := make([]QueryResult, 0, len(baseline.Queries))
	for _, baselineQuery := range baseline.Queries {
		current, ok := currentByID[baselineQuery.ID]
		if !ok || len(current.TopResults) == 0 || baselineQuery.ResultCount == 0 || baselineQuery.TokensPerResult == 0 {
			continue
		}
		currentCommon = append(currentCommon, current)
		baselineCommon = append(baselineCommon, QueryResult{
			ID:            baselineQuery.ID,
			Tool:          baselineQuery.Tool,
			Class:         baselineQuery.Class,
			TokenEstimate: TokenEstimate{TokensPerResult: baselineQuery.TokensPerResult},
			TopResults:    make([]SearchResult, baselineQuery.ResultCount),
		})
	}

	return tokenStatsBy(currentCommon, key), tokenStatsBy(baselineCommon, key)
}

func tokenStatsRegressionReasons(label string, current, baseline tokenBudgetStats, meanRatio, p95Ratio float64) []string {
	var reasons []string
	if baseline.MeanTokensPerResult > 0 && current.MeanTokensPerResult > baseline.MeanTokensPerResult*(1+meanRatio) {
		reasons = append(reasons, label+" mean tokens/result increased")
	}
	if baseline.P95TokensPerResult > 0 && current.P95TokensPerResult > baseline.P95TokensPerResult*(1+p95Ratio) {
		reasons = append(reasons, label+" p95 tokens/result increased")
	}
	return reasons
}

func tokenStatsBy(results []QueryResult, key func(QueryResult) string) map[string]tokenBudgetStats {
	groups := make(map[string][]QueryResult)
	for _, result := range results {
		groups[key(result)] = append(groups[key(result)], result)
	}
	out := make(map[string]tokenBudgetStats, len(groups))
	for name, group := range groups {
		out[name] = tokenStats(group)
	}
	return out
}

func tokenStats(results []QueryResult) tokenBudgetStats {
	stats := tokenBudgetStats{Count: len(results)}
	perResult := make([]float64, 0, len(results))
	responseTokens := make([]int64, 0, len(results))
	responseBytes := make([]int64, 0, len(results))
	responseTokenTotal := 0
	responseByteTotal := 0
	for _, result := range results {
		if len(result.TopResults) == 0 {
			stats.ZeroResultCount++
		} else {
			perResult = append(perResult, result.TokenEstimate.TokensPerResult)
		}
		responseTokens = append(responseTokens, int64(result.TokenEstimate.ResultTokens))
		responseBytes = append(responseBytes, int64(result.TokenEstimate.ResponseBytes))
		responseTokenTotal += result.TokenEstimate.ResultTokens
		responseByteTotal += result.TokenEstimate.ResponseBytes
	}
	sort.Float64s(perResult)
	sort.Slice(responseTokens, func(i, j int) bool { return responseTokens[i] < responseTokens[j] })
	sort.Slice(responseBytes, func(i, j int) bool { return responseBytes[i] < responseBytes[j] })
	if len(perResult) > 0 {
		total := 0.0
		for _, value := range perResult {
			total += value
		}
		stats.MeanTokensPerResult = total / float64(len(perResult))
		stats.MedianTokensPerResult = percentileFloat(perResult, 0.50)
		stats.P95TokensPerResult = percentileFloat(perResult, 0.95)
		stats.MaxTokensPerResult = perResult[len(perResult)-1]
	}
	if len(results) > 0 {
		stats.MeanResponseTokens = float64(responseTokenTotal) / float64(len(results))
		stats.MeanResponseBytes = float64(responseByteTotal) / float64(len(results))
		stats.P95ResponseTokens = int(percentile(responseTokens, 0.95))
		stats.P95ResponseBytes = int(percentile(responseBytes, 0.95))
		stats.MaxResponseTokens = int(responseTokens[len(responseTokens)-1])
		stats.MaxResponseBytes = int(responseBytes[len(responseBytes)-1])
	}
	return stats
}

func corpusVersion(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum)
}

func gitSHA() string {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
