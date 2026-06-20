package eval

import "time"

const (
	DefaultCorpusPath = "internal/validation/testdata/queries.yaml"
	DefaultOutDir     = ".aman-pm/validation/search-eval"

	GraphRecommendationKeep  = "keep"
	GraphRecommendationTune  = "tune"
	GraphRecommendationDefer = "defer"
	GraphRecommendationKill  = "kill"
)

type Corpus struct {
	Queries []Query `json:"queries" yaml:"queries"`
}

type Query struct {
	ID              string            `json:"id" yaml:"id"`
	Name            string            `json:"name" yaml:"name"`
	Query           string            `json:"query" yaml:"query"`
	Tool            string            `json:"tool" yaml:"tool"`
	Profile         string            `json:"profile,omitempty" yaml:"profile,omitempty"`
	Scope           []string          `json:"scope,omitempty" yaml:"scope,omitempty"`
	Mode            string            `json:"mode,omitempty" yaml:"mode,omitempty"`
	Tier            string            `json:"tier,omitempty" yaml:"tier,omitempty"`
	Class           string            `json:"class" yaml:"class"`
	Job             string            `json:"job" yaml:"job"`
	Expected        []string          `json:"expected,omitempty" yaml:"expected,omitempty"`
	ExpectedResults []ExpectedResult  `json:"expected_results" yaml:"expected_results"`
	Metadata        map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Holdout         bool              `json:"holdout" yaml:"holdout"`
	Source          string            `json:"source" yaml:"source"`
	Notes           string            `json:"notes,omitempty" yaml:"notes,omitempty"`
}

type ExpectedResult struct {
	Path      string `json:"path" yaml:"path"`
	Symbol    string `json:"symbol,omitempty" yaml:"symbol,omitempty"`
	StartLine int    `json:"start_line,omitempty" yaml:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty" yaml:"end_line,omitempty"`
	Page      int    `json:"page,omitempty" yaml:"page,omitempty"`
	PageStart int    `json:"page_start,omitempty" yaml:"page_start,omitempty"`
	PageEnd   int    `json:"page_end,omitempty" yaml:"page_end,omitempty"`
	Grade     int    `json:"grade" yaml:"grade"`
	Rationale string `json:"rationale,omitempty" yaml:"rationale,omitempty"`
}

type SearchResult struct {
	Path        string `json:"path"`
	Symbol      string `json:"symbol,omitempty"`
	Text        string `json:"text,omitempty"`
	ResultID    string `json:"result_id,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	PageNumber  string `json:"page_number,omitempty"`
	PageStart   string `json:"page_start,omitempty"`
	PageEnd     string `json:"page_end,omitempty"`
}

type SearchResponse struct {
	Results       []SearchResult
	ResponseBytes int
}

type QueryResult struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Query           string            `json:"query"`
	Tool            string            `json:"tool"`
	Class           string            `json:"class"`
	Job             string            `json:"job"`
	Profile         string            `json:"profile"`
	SourceClass     string            `json:"source_class"`
	Language        string            `json:"language"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	Holdout         bool              `json:"holdout"`
	ExpectedResults []ExpectedResult  `json:"expected_results"`
	TopResults      []SearchResult    `json:"top_results"`
	MatchedGrade    int               `json:"matched_grade"`
	FirstUsefulRank int               `json:"first_useful_rank"`
	LatencyMs       int64             `json:"latency_ms"`
	TokenEstimate   TokenEstimate     `json:"token_estimate"`
	Passed          bool              `json:"passed"`
	FailureReason   string            `json:"failure_reason,omitempty"`
	Error           string            `json:"error,omitempty"`
}

type TokenEstimate struct {
	Method          string  `json:"method"`
	QueryTokens     int     `json:"query_tokens"`
	ResponseBytes   int     `json:"response_bytes"`
	ResultTokens    int     `json:"result_tokens"`
	TokensPerResult float64 `json:"tokens_per_result"`
	TotalTokens     int     `json:"total_tokens"`
}

type Options struct {
	CorpusPath             string
	Subset                 string
	Output                 string
	OutDir                 string
	BaselinePath           string
	TokenBaselinePath      string
	FailOnRegression       bool
	IncludeHoldout         bool
	SaveBaseline           bool
	ForceOverwriteBaseline bool
	TokenBudgetEnabled     bool
	Command                string
}

type Selection struct {
	Subset         string
	IncludeHoldout bool
}

type RunMetadata struct {
	Timestamp      time.Time  `json:"timestamp"`
	GitSHA         string     `json:"git_sha,omitempty"`
	Command        string     `json:"command"`
	CorpusPath     string     `json:"corpus_path"`
	Subset         string     `json:"subset"`
	IncludeHoldout bool       `json:"include_holdout"`
	Output         string     `json:"output"`
	Embedder       string     `json:"embedder,omitempty"`
	BM25Backend    string     `json:"bm25_backend,omitempty"`
	IndexChunks    int        `json:"index_chunks,omitempty"`
	Tolerances     Tolerances `json:"tolerances"`
}

type Tolerances struct {
	MinPassRateDelta              float64                       `json:"min_pass_rate_delta"`
	MinRecallAt10Delta            float64                       `json:"min_recall_at_10_delta"`
	MaxP95LatencyMsDelta          int64                         `json:"max_p95_latency_ms_delta"`
	MaxTokenMeanIncreaseRatio     float64                       `json:"max_token_mean_increase_ratio"`
	MaxTokenP95IncreaseRatio      float64                       `json:"max_token_p95_increase_ratio"`
	MaxTokenClassIncreaseRatio    float64                       `json:"max_token_class_increase_ratio"`
	MaxTokenClassP95IncreaseRatio float64                       `json:"max_token_class_p95_increase_ratio"`
	MaxTokenQueryIncreaseRatio    float64                       `json:"max_token_query_increase_ratio"`
	DimensionRegression           map[string]DimensionTolerance `json:"dimension_regression,omitempty"`
}

type DimensionTolerance struct {
	MinPassRateDelta   float64 `json:"min_pass_rate_delta"`
	MinRecallAt10Delta float64 `json:"min_recall_at_10_delta"`
}

type Summary struct {
	QueryCount                   int     `json:"query_count"`
	PassCount                    int     `json:"pass_count"`
	FailCount                    int     `json:"fail_count"`
	PassRate                     float64 `json:"pass_rate"`
	ZeroResultRate               float64 `json:"zero_result_rate"`
	P50LatencyMs                 int64   `json:"p50_latency_ms"`
	P95LatencyMs                 int64   `json:"p95_latency_ms"`
	MemoryPeakBytes              uint64  `json:"memory_peak_bytes,omitempty"`
	TokensPerResultMean          float64 `json:"tokens_per_result_mean"`
	TokensPerResultP95           float64 `json:"tokens_per_result_p95"`
	ZeroResultResponseTokensMean float64 `json:"zero_result_response_tokens_mean,omitempty"`
}

type Metrics struct {
	PassRate                    float64 `json:"pass_rate"`
	RecallAt5                   float64 `json:"recall_at_5"`
	RecallAt10                  float64 `json:"recall_at_10"`
	MRRAt10                     float64 `json:"mrr_at_10"`
	NDCGAt10                    float64 `json:"ndcg_at_10"`
	FirstUsefulResultRankMean   float64 `json:"first_useful_result_rank_mean"`
	TestPollutionRate           float64 `json:"test_pollution_rate"`
	ExactLookupPassRate         float64 `json:"exact_lookup_pass_rate"`
	NegativeAdversarialPassRate float64 `json:"negative_adversarial_pass_rate"`
	PDFPassRate                 float64 `json:"pdf_pass_rate"`
	PDFRecallAt10               float64 `json:"pdf_recall_at_10"`
}

type BaselineComparison struct {
	BaselinePath             string   `json:"baseline_path,omitempty"`
	TokenBaselinePath        string   `json:"token_baseline_path,omitempty"`
	Compared                 bool     `json:"compared"`
	TokenBudgetCompared      bool     `json:"token_budget_compared"`
	Regressed                bool     `json:"regressed"`
	PassRateDelta            float64  `json:"pass_rate_delta"`
	RecallAt10Delta          float64  `json:"recall_at_10_delta"`
	MRRAt10Delta             float64  `json:"mrr_at_10_delta"`
	NDCGAt10Delta            float64  `json:"ndcg_at_10_delta"`
	P95LatencyMsDelta        int64    `json:"p95_latency_ms_delta"`
	TokensPerResultMeanDelta float64  `json:"tokens_per_result_mean_delta"`
	TokensPerResultP95Delta  float64  `json:"tokens_per_result_p95_delta"`
	RegressionReasons        []string `json:"regression_reasons,omitempty"`
}

type DimensionRegression struct {
	Dimension     string  `json:"dimension"`
	Group         string  `json:"group"`
	Metric        string  `json:"metric"`
	BaselineValue float64 `json:"baseline_value"`
	CurrentValue  float64 `json:"current_value"`
	Delta         float64 `json:"delta"`
	Tolerance     float64 `json:"tolerance"`
	Regressed     bool    `json:"regressed"`
}

type ClassGroups struct {
	GraphHeavy  []string `json:"graph_heavy"`
	ExactLookup []string `json:"exact_lookup"`
	Ordinary    []string `json:"ordinary"`
}

type GraphEvalGate struct {
	Required                 bool                      `json:"required"`
	Compared                 bool                      `json:"compared"`
	Passed                   bool                      `json:"passed"`
	Recommendation           string                    `json:"recommendation"`
	RecommendationTarget     string                    `json:"recommendation_target"`
	EvaluationScope          string                    `json:"evaluation_scope"`
	MeasuredTool             string                    `json:"measured_tool"`
	GraphToolMeasured        bool                      `json:"graph_tool_measured"`
	Reasons                  []string                  `json:"reasons,omitempty"`
	BaselineSource           string                    `json:"baseline_source,omitempty"`
	TargetRecallAt10Delta    float64                   `json:"target_recall_at_10_delta"`
	KillRecallAt10Delta      float64                   `json:"kill_recall_at_10_delta"`
	LowBaselineThreshold     float64                   `json:"low_baseline_threshold"`
	LowBaselineAbsoluteFloor float64                   `json:"low_baseline_absolute_floor"`
	CurrentQueryCount        int                       `json:"current_query_count"`
	BaselineQueryCount       int                       `json:"baseline_query_count,omitempty"`
	TokenMetrics             GraphTokenMetrics         `json:"token_metrics"`
	Classes                  map[string]GraphClassGate `json:"classes,omitempty"`
	Failures                 []GraphEvalGateFailure    `json:"failures,omitempty"`
}

type GraphClassGate struct {
	QueryCount               int      `json:"query_count"`
	BaselineRecallAt10Floor  float64  `json:"baseline_recall_at_10_floor"`
	CurrentRecallAt10        float64  `json:"current_recall_at_10"`
	RecallAt10Delta          float64  `json:"recall_at_10_delta"`
	TargetRecallAt10Delta    float64  `json:"target_recall_at_10_delta"`
	KillRecallAt10Delta      float64  `json:"kill_recall_at_10_delta"`
	LowBaselineAbsoluteFloor float64  `json:"low_baseline_absolute_floor"`
	Passed                   bool     `json:"passed"`
	Recommendation           string   `json:"recommendation"`
	Reasons                  []string `json:"reasons,omitempty"`
}

type GraphTokenMetrics struct {
	Count               int     `json:"count"`
	MeanTokensPerResult float64 `json:"mean_tokens_per_result"`
	P95TokensPerResult  float64 `json:"p95_tokens_per_result"`
	ZeroResultCount     int     `json:"zero_result_count"`
}

type GraphEvalGateFailure struct {
	Class                   string  `json:"class"`
	Reason                  string  `json:"reason"`
	BaselineRecallAt10Floor float64 `json:"baseline_recall_at_10_floor"`
	CurrentRecallAt10       float64 `json:"current_recall_at_10"`
	RecallAt10Delta         float64 `json:"recall_at_10_delta"`
}

type ExactLookupGate struct {
	Required           bool                       `json:"required"`
	Compared           bool                       `json:"compared"`
	Passed             bool                       `json:"passed"`
	BaselineQueryCount int                        `json:"baseline_query_count"`
	CurrentQueryCount  int                        `json:"current_query_count"`
	Failures           []ExactLookupGateFailure   `json:"failures,omitempty"`
	Classes            map[string]ExactGateMetric `json:"classes,omitempty"`
}

type ExactGateMetric struct {
	QueryCount int     `json:"query_count"`
	PassRate   float64 `json:"pass_rate"`
}

type ExactLookupGateFailure struct {
	QueryID          string `json:"query_id"`
	Reason           string `json:"reason"`
	BaselineRank     int    `json:"baseline_rank,omitempty"`
	CurrentRank      int    `json:"current_rank,omitempty"`
	BaselinePath     string `json:"baseline_path,omitempty"`
	CurrentPath      string `json:"current_path,omitempty"`
	BaselineResultID string `json:"baseline_result_id,omitempty"`
	CurrentResultID  string `json:"current_result_id,omitempty"`
}

type OutputPaths struct {
	JSON     string `json:"json,omitempty"`
	Markdown string `json:"markdown,omitempty"`
}

type Report struct {
	Run                  RunMetadata           `json:"run"`
	Summary              Summary               `json:"summary"`
	Metrics              Metrics               `json:"metrics"`
	ClassGroups          ClassGroups           `json:"class_groups"`
	ByClass              map[string]Metrics    `json:"by_class"`
	ByJob                map[string]Metrics    `json:"by_job"`
	ByProfile            map[string]Metrics    `json:"by_profile"`
	BySourceClass        map[string]Metrics    `json:"by_source_class"`
	ByLanguage           map[string]Metrics    `json:"by_language"`
	DimensionRegressions []DimensionRegression `json:"dimension_regressions,omitempty"`
	Queries              []QueryResult         `json:"queries"`
	GraphEvalGate        GraphEvalGate         `json:"graph_eval_gate"`
	ExactLookupGate      ExactLookupGate       `json:"exact_lookup_gate"`
	BaselineComparison   BaselineComparison    `json:"baseline_comparison"`
	OutputPaths          OutputPaths           `json:"output_paths,omitempty"`
}
