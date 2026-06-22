package eval

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/graph"
	"gopkg.in/yaml.v3"
)

const (
	// GraphCorpusSchemaVersion versions the INPUT corpus YAML contract
	// (graph-queries.yaml). It is validated on load (ValidateGraphCorpus) and must
	// not change unless the corpus file format changes.
	GraphCorpusSchemaVersion = 1
	// DirectGraphReportSchemaVersion versions the OUTPUT report contract
	// (DirectGraphEvalReport, written to latest.json) independently of the corpus.
	// Bumped to 2 by TASK-GRA21: each results[] entry now carries the structured
	// `path` object (was the flat `graph_path` string array), an incompatible shape
	// change for any consumer keying off this version.
	DirectGraphReportSchemaVersion = 2
	DefaultGraphCorpusPath         = "internal/validation/testdata/graph-queries.yaml"
	DefaultGraphOutDir             = ".aman-pm/validation/graph-eval"

	GraphSubsetQuick = "quick"
	GraphSubsetFull  = "full"

	DirectGraphReportType   = "direct_graph_eval"
	DirectGraphMeasuredTool = "graph.query"
	// DirectGraphEvaluationScope distinguishes the direct graph.query measurement
	// from the search-engine graph-heavy gate (search_engine_graph_heavy_classes).
	DirectGraphEvaluationScope = "direct_graph_query_modes"

	// Expectation classes label what a case is probing. Only quality-class cases
	// are scored against the per-mode relevance thresholds; degraded and gap
	// cases are reported separately so an intentionally-degraded probe or a
	// known future-semantics gap is not counted as a relevance failure
	// (TASK-GRA12/13/14).
	GraphExpectationClassQuality             = "quality"
	GraphExpectationClassDegraded            = "degraded"
	GraphExpectationClassGap                 = "gap"
	GraphExpectationClassNegativeAdversarial = "negative_adversarial"

	maxGraphEvalLimit = 50
)

var defaultAllowedGraphStatuses = []graph.GraphStatus{
	graph.GraphStatusFresh,
	graph.GraphStatusStale,
	graph.GraphStatusPartial,
}

var defaultBlockingGraphStatuses = []graph.GraphStatus{
	graph.GraphStatusUnavailable,
	graph.GraphStatusIncompatible,
	graph.GraphStatusEmpty,
	graph.GraphStatusFailed,
}

var allowedGraphModes = map[string]bool{
	graph.QueryModeFindReferences: true,
	graph.QueryModeExplainSymbol:  true,
	graph.QueryModeImpactAnalysis: true,
}

var allowedGraphNodeKinds = map[graph.NodeKind]bool{
	graph.NodeKindProject:    true,
	graph.NodeKindFile:       true,
	graph.NodeKindTestFile:   true,
	graph.NodeKindDoc:        true,
	graph.NodeKindConfigFile: true,
	graph.NodeKindPackage:    true,
	graph.NodeKindImport:     true,
	graph.NodeKindSymbol:     true,
	graph.NodeKindChunk:      true,
	graph.NodeKindConfigKey:  true,
}

var allowedGraphEdgeKinds = map[graph.EdgeKind]bool{
	graph.EdgeKindProjectContainsFile:      true,
	graph.EdgeKindFileDeclaresPackage:      true,
	graph.EdgeKindFileImports:              true,
	graph.EdgeKindPackageImports:           true,
	graph.EdgeKindFileDefinesSymbol:        true,
	graph.EdgeKindSymbolHasChunk:           true,
	graph.EdgeKindFileDefinesConfigKey:     true,
	graph.EdgeKindTestCoversImplementation: true,
	graph.EdgeKindDocMentionsFile:          true,
	graph.EdgeKindDocMentionsSymbol:        true,
	graph.EdgeKindDocMentionsConfigKey:     true,
	graph.EdgeKindDocMentionsPath:          true,
}

var allowedGraphConfidenceLabels = map[graph.ConfidenceLabel]bool{
	graph.ConfidenceExact:  true,
	graph.ConfidenceHigh:   true,
	graph.ConfidenceMedium: true,
	graph.ConfidenceLow:    true,
}

var allowedGraphStatuses = map[graph.GraphStatus]bool{
	graph.GraphStatusUnavailable:  true,
	graph.GraphStatusIncompatible: true,
	graph.GraphStatusEmpty:        true,
	graph.GraphStatusFresh:        true,
	graph.GraphStatusStale:        true,
	graph.GraphStatusPartial:      true,
	graph.GraphStatusFailed:       true,
}

var allowedGraphWarningCodes = map[graph.WarningCode]bool{
	graph.WarningGraphUnavailable:                true,
	graph.WarningSchemaIncompatible:              true,
	graph.WarningGraphStale:                      true,
	graph.WarningGraphStaleEdges:                 true,
	graph.WarningExtractorFailed:                 true,
	graph.WarningExtractorPartial:                true,
	graph.WarningBuildFailed:                     true,
	graph.WarningCode("graph_results_truncated"): true,
	graph.WarningTraversalBudgetExhausted:        true,
	graph.WarningUnsupportedLanguage:             true,
}

var allowedDirectGraphRoles = map[string]bool{
	"related":                true,
	"symbol_context":         true,
	"downstream":             true,
	"test_or_implementation": true,
	"documented_reference":   true,
}

var allowedGraphExpectationClasses = map[string]bool{
	GraphExpectationClassQuality:             true,
	GraphExpectationClassDegraded:            true,
	GraphExpectationClassGap:                 true,
	GraphExpectationClassNegativeAdversarial: true,
}

// graphExpectationClassOrDefault returns the case's expectation class, treating
// an empty value as quality. LoadGraphCorpus normalizes this at load time; the
// helper also covers GraphQuery values constructed directly (e.g. in tests).
func graphExpectationClassOrDefault(class string) string {
	if c := strings.TrimSpace(class); c != "" {
		return c
	}
	return GraphExpectationClassQuality
}

type rawGraphCorpus struct {
	SchemaVersion int             `yaml:"schema_version"`
	Queries       []rawGraphQuery `yaml:"queries"`
}

type rawGraphQuery struct {
	ID                   string                      `yaml:"id"`
	Name                 string                      `yaml:"name"`
	Mode                 string                      `yaml:"mode"`
	Query                string                      `yaml:"query"`
	SubjectType          string                      `yaml:"subject_type,omitempty"`
	Limit                int                         `yaml:"limit,omitempty"`
	IncludeStale         bool                        `yaml:"include_stale,omitempty"`
	BudgetOverrides      graphTraversalBudgetYAML    `yaml:"budget_overrides,omitempty"`
	Subsets              []string                    `yaml:"subsets"`
	Holdout              *bool                       `yaml:"holdout"`
	Source               string                      `yaml:"source"`
	ExpectationClass     string                      `yaml:"expectation_class,omitempty"`
	Expected             []GraphExpectedEvidence     `yaml:"expected"`
	AcceptedAlternatives []GraphExpectedEvidence     `yaml:"accepted_alternatives,omitempty"`
	Negative             GraphNegativeExpectation    `yaml:"negative,omitempty"`
	Degradation          GraphDegradationExpectation `yaml:"degradation,omitempty"`
	Metadata             map[string]string           `yaml:"metadata,omitempty"`
}

// graphTraversalBudgetYAML mirrors graph.TraversalBudgetOverrides for corpus YAML.
type graphTraversalBudgetYAML struct {
	MaxResults     *int `yaml:"max_results,omitempty"`
	MaxNodes       *int `yaml:"max_nodes,omitempty"`
	MaxPerEdgeKind *int `yaml:"max_per_edge_kind,omitempty"`
	MaxTokens      *int `yaml:"max_tokens,omitempty"`
	MaxDepth       *int `yaml:"max_depth,omitempty"`
}

type GraphCorpus struct {
	SchemaVersion int          `json:"schema_version" yaml:"schema_version"`
	Queries       []GraphQuery `json:"queries" yaml:"queries"`
}

type GraphQuery struct {
	ID                   string                         `json:"id" yaml:"id"`
	Name                 string                         `json:"name" yaml:"name"`
	Mode                 string                         `json:"mode" yaml:"mode"`
	Query                string                         `json:"query" yaml:"query"`
	SubjectType          string                         `json:"subject_type,omitempty" yaml:"subject_type,omitempty"`
	Limit                int                            `json:"limit,omitempty" yaml:"limit,omitempty"`
	IncludeStale         bool                           `json:"include_stale,omitempty" yaml:"include_stale,omitempty"`
	BudgetOverrides      graph.TraversalBudgetOverrides `json:"budget_overrides,omitempty" yaml:"budget_overrides,omitempty"`
	Subsets              []string                       `json:"subsets" yaml:"subsets"`
	Holdout              bool                           `json:"holdout" yaml:"holdout"`
	Source               string                         `json:"source" yaml:"source"`
	ExpectationClass     string                         `json:"expectation_class,omitempty" yaml:"expectation_class,omitempty"`
	Expected             []GraphExpectedEvidence        `json:"expected" yaml:"expected"`
	AcceptedAlternatives []GraphExpectedEvidence        `json:"accepted_alternatives,omitempty" yaml:"accepted_alternatives,omitempty"`
	Negative             GraphNegativeExpectation       `json:"negative,omitempty" yaml:"negative,omitempty"`
	Degradation          GraphDegradationExpectation    `json:"degradation,omitempty" yaml:"degradation,omitempty"`
	Metadata             map[string]string              `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

type GraphExpectedEvidence struct {
	NodeID            string                `json:"node_id,omitempty" yaml:"node_id,omitempty"`
	NodeKind          graph.NodeKind        `json:"node_kind,omitempty" yaml:"node_kind,omitempty"`
	SourcePath        string                `json:"source_path,omitempty" yaml:"source_path,omitempty"`
	Role              string                `json:"role,omitempty" yaml:"role,omitempty"`
	Relation          graph.EdgeKind        `json:"relation,omitempty" yaml:"relation,omitempty"`
	ConfidenceLabel   graph.ConfidenceLabel `json:"confidence_label,omitempty" yaml:"confidence_label,omitempty"`
	EvidenceMethod    string                `json:"evidence_method,omitempty" yaml:"evidence_method,omitempty"`
	GraphPathContains []string              `json:"graph_path_contains,omitempty" yaml:"graph_path_contains,omitempty"`
	Rationale         string                `json:"rationale" yaml:"rationale"`
}

type GraphDegradationExpectation struct {
	AllowedStatuses      []graph.GraphStatus `json:"allowed_statuses,omitempty" yaml:"allowed_statuses,omitempty"`
	BlockingStatuses     []graph.GraphStatus `json:"blocking_statuses,omitempty" yaml:"blocking_statuses,omitempty"`
	ExpectedWarningCodes []graph.WarningCode `json:"expected_warning_codes,omitempty" yaml:"expected_warning_codes,omitempty"`
	// ExpectedLabels names the eval-owned degradation labels a case intentionally
	// targets (TASK-GRA16). A label listed here is excluded from blocking
	// degradation — it is the explicit opt-out for degradation probes (e.g. an
	// empty-graph fixture) and unsupported-language cases.
	ExpectedLabels []GraphDegradationLabel `json:"expected_labels,omitempty" yaml:"expected_labels,omitempty"`
}

type GraphSelection struct {
	Subset string
}

type DirectGraphEvalReport struct {
	SchemaVersion     int                               `json:"schema_version"`
	ReportType        string                            `json:"report_type"`
	MeasuredTool      string                            `json:"measured_tool"`
	EvaluationScope   string                            `json:"evaluation_scope"`
	GraphToolMeasured bool                              `json:"graph_tool_measured"`
	UnmeasuredReason  string                            `json:"unmeasured_reason,omitempty"`
	Run               DirectGraphRunMetadata            `json:"run"`
	Summary           DirectGraphSummary                `json:"summary"`
	ModeCounts        map[string]int                    `json:"mode_counts,omitempty"`
	ByMode            map[string]DirectGraphModeSummary `json:"by_mode,omitempty"`
	Degradation       DirectGraphDegradationSummary     `json:"degradation"`
	Queries           []DirectGraphQueryResult          `json:"queries,omitempty"`
	StatusSnapshots   []GraphStatusSnapshotBrief        `json:"status_snapshots,omitempty"`
	OutputPaths       OutputPaths                       `json:"output_paths,omitempty"`
}

type DirectGraphRunMetadata struct {
	Timestamp  time.Time `json:"timestamp,omitempty"`
	GitSHA     string    `json:"git_sha,omitempty"`
	Command    string    `json:"command"`
	CorpusPath string    `json:"corpus_path"`
	Subset     string    `json:"subset"`
	Output     string    `json:"output"`
}

type DirectGraphSummary struct {
	QueryCount              int     `json:"query_count"`
	MeasuredQueryCount      int     `json:"measured_query_count"`
	PassCount               int     `json:"pass_count"`
	FailCount               int     `json:"fail_count"`
	PassRate                float64 `json:"pass_rate"`
	ZeroResultRate          float64 `json:"zero_result_rate"`
	P50LatencyMs            int64   `json:"p50_latency_ms"`
	P95LatencyMs            int64   `json:"p95_latency_ms"`
	DegradationBlockingRate float64 `json:"degradation_blocking_rate"`
	// Negative-adversarial metrics (TASK-GRA26): measured separately from quality
	// relevance and gated at 100% via a direct threshold in the graph runner.
	NegativeAdversarialCount     int     `json:"negative_adversarial_count"`
	NegativeAdversarialPassCount int     `json:"negative_adversarial_pass_count"`
	NegativeAdversarialPassRate  float64 `json:"negative_adversarial_pass_rate"`
}

type DirectGraphModeSummary struct {
	QueryCount      int     `json:"query_count"`
	MeasuredCount   int     `json:"measured_count"`
	PassCount       int     `json:"pass_count"`
	FailCount       int     `json:"fail_count"`
	ResultCount     int     `json:"result_count"`
	ZeroResultCount int     `json:"zero_result_count"`
	PassRate        float64 `json:"pass_rate"`
	ZeroResultRate  float64 `json:"zero_result_rate"`

	// Per-mode relevance aggregates (TASK-GRA12/13/14), macro-averaged over
	// QualityCount = cases that are measured AND quality-class AND non-blocking.
	// Degraded/gap and unmeasured cases are excluded so graph health and
	// future-semantics gaps are not conflated with relevance quality.
	QualityCount       int     `json:"quality_count"`
	ExpectedRecallAt10 float64 `json:"expected_recall_at_10"`
	PrecisionAt3       float64 `json:"precision_at_3"`
	PrecisionAt5       float64 `json:"precision_at_5"`
	PrecisionAt10      float64 `json:"precision_at_10"`
	HitRateAt3         float64 `json:"hit_rate_at_3"`
	HitRateAt10        float64 `json:"hit_rate_at_10"`
	// ThresholdMet reports whether this mode met its configured per-mode
	// relevance thresholds. It is only meaningful when QualityCount > 0; see
	// evaluateGraphModeThreshold.
	ThresholdMet bool `json:"threshold_met"`
}

type DirectGraphDegradationSummary struct {
	// BlockingCount/BlockingRate count cases with >=1 blocking degradation label
	// per the recoverability matrix (TASK-GRA16); BlockingRate is the gate
	// numerator. OverallCount/OverallRate count cases with any degradation label
	// (reported, not gated).
	BlockingCount   int                           `json:"blocking_count"`
	OverallCount    int                           `json:"overall_count"`
	BlockingRate    float64                       `json:"blocking_rate"`
	OverallRate     float64                       `json:"overall_rate"`
	ByStatus        map[graph.GraphStatus]int     `json:"by_status,omitempty"`
	WarningCounts   map[graph.WarningCode]int     `json:"warning_counts,omitempty"`
	ByLabel         map[GraphDegradationLabel]int `json:"by_label,omitempty"`
	BlockingByLabel map[GraphDegradationLabel]int `json:"blocking_by_label,omitempty"`
	// Recoverability is the matrix for the labels actually observed in this run.
	Recoverability []GraphDegradationInfo `json:"recoverability,omitempty"`
	// QualityExcluded documents which cases were dropped from quality metrics and
	// why (unmeasured, non-quality class, or unexpected degradation).
	QualityExcluded []GraphQualityExclusion `json:"quality_excluded,omitempty"`
}

// GraphQualityExclusion records one case excluded from the per-mode relevance
// quality aggregates and the reason, so reports are honest about which cases the
// quality pass rate did and did not cover (TASK-GRA16).
type GraphQualityExclusion struct {
	ID     string `json:"id"`
	Mode   string `json:"mode"`
	Reason string `json:"reason"`
}

type DirectGraphQueryResult struct {
	ID                   string                `json:"id"`
	Name                 string                `json:"name,omitempty"`
	Mode                 string                `json:"mode"`
	Query                string                `json:"query"`
	Resolution           string                `json:"resolution,omitempty"`
	CandidateCount       int                   `json:"candidate_count,omitempty"`
	Status               graph.GraphStatus     `json:"status"`
	Available            bool                  `json:"available"`
	Degraded             bool                  `json:"degraded"`
	BlockingDegradation  bool                  `json:"blocking_degradation,omitempty"`
	WarningCodes         []graph.WarningCode   `json:"warning_codes,omitempty"`
	Warnings             []graph.StatusWarning `json:"warnings,omitempty"`
	Results              []graph.QueryResult   `json:"results,omitempty"`
	ResultCount          int                   `json:"result_count"`
	MatchedEvidenceCount int                   `json:"matched_evidence_count"`
	// Rank-aware per-case metrics (TASK-GRA12/13/14), computed over the fixed
	// top-directGraphRankWindow raw window. See scoreGraphCase.
	MatchedPositions   int     `json:"matched_positions"`
	WindowSize         int     `json:"window_size"`
	ExpectedRecallAt10 float64 `json:"expected_recall_at_10"`
	HitAt3             bool    `json:"hit_at_3"`
	HitAt10            bool    `json:"hit_at_10"`
	// Deduped precision@K (TASK-GRA15): fraction of the first K *unique* results
	// that are relevant (expected or accepted alternative). UniqueResultCount is
	// the precision denominator ceiling. PrecisionAt10 feeds the find_references
	// per-mode floor.
	UniqueResultCount int     `json:"unique_result_count"`
	PrecisionAt3      float64 `json:"precision_at_3"`
	PrecisionAt5      float64 `json:"precision_at_5"`
	PrecisionAt10     float64 `json:"precision_at_10"`
	// Scored reports whether the case reached relevance scoring (graph served a
	// healthy, allowed answer). It is false for transport errors, blocking
	// degradation, disallowed status, and missing-warning contract failures —
	// which are graph/contract failures, not relevance misses.
	Scored bool `json:"scored"`
	// DegradationLabels are the eval-owned degradation categories observed for
	// this case (TASK-GRA16). BlockingDegradationLabels is the subset that counts
	// toward blocking_degradation_rate after applying the recoverability matrix
	// and the case's expected_labels.
	DegradationLabels         []GraphDegradationLabel `json:"degradation_labels,omitempty"`
	BlockingDegradationLabels []GraphDegradationLabel `json:"blocking_degradation_labels,omitempty"`
	// ExpectationClass mirrors the source case class (quality/degraded/gap) so
	// per-mode aggregation can exclude non-quality cases without re-reading the
	// corpus. Empty is treated as quality.
	ExpectationClass string `json:"expectation_class,omitempty"`

	LatencyMs     int64  `json:"latency_ms"`
	Passed        bool   `json:"passed"`
	FailureReason string `json:"failure_reason,omitempty"`
	Error         string `json:"error,omitempty"`
}

type GraphStatusSnapshotBrief struct {
	Available             bool                          `json:"available"`
	SchemaVersion         int                           `json:"schema_version,omitempty"`
	Status                graph.GraphStatus             `json:"status"`
	Freshness             graph.FreshnessState          `json:"freshness,omitempty"`
	NodeCount             int                           `json:"node_count,omitempty"`
	EdgeCount             int                           `json:"edge_count,omitempty"`
	ActiveEdgeCount       int                           `json:"active_edge_count,omitempty"`
	StaleEdgeCount        int                           `json:"stale_edge_count,omitempty"`
	WarningCodes          []graph.WarningCode           `json:"warning_codes,omitempty"`
	Warnings              []graph.StatusWarning         `json:"warnings,omitempty"`
	ExtractorStatusCounts map[graph.ExtractorStatus]int `json:"extractor_status_counts,omitempty"`
	Extractors            []graph.ExtractorSummary      `json:"extractors,omitempty"`
	Confidence            map[string]int                `json:"confidence,omitempty"`
}

func LoadGraphCorpus(path string) (GraphCorpus, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return GraphCorpus{}, fmt.Errorf("failed to read graph corpus %s: %w", path, err)
	}
	var raw rawGraphCorpus
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil {
		return GraphCorpus{}, fmt.Errorf("failed to parse graph corpus %s: %w", path, err)
	}
	corpus, err := raw.toGraphCorpus()
	if err != nil {
		return GraphCorpus{}, err
	}
	corpus = normalizeGraphCorpus(corpus)
	if err := ValidateGraphCorpus(corpus); err != nil {
		return GraphCorpus{}, err
	}
	return corpus, nil
}

func (raw rawGraphCorpus) toGraphCorpus() (GraphCorpus, error) {
	corpus := GraphCorpus{
		SchemaVersion: raw.SchemaVersion,
		Queries:       make([]GraphQuery, 0, len(raw.Queries)),
	}
	for i, query := range raw.Queries {
		if query.Holdout == nil {
			id := strings.TrimSpace(query.ID)
			if id == "" {
				return GraphCorpus{}, fmt.Errorf("graph query at index %d missing holdout", i)
			}
			return GraphCorpus{}, fmt.Errorf("graph query %s missing holdout", id)
		}
		corpus.Queries = append(corpus.Queries, GraphQuery{
			ID:                   strings.TrimSpace(query.ID),
			Name:                 query.Name,
			Mode:                 query.Mode,
			Query:                query.Query,
			SubjectType:          strings.TrimSpace(query.SubjectType),
			Limit:                query.Limit,
			IncludeStale:         query.IncludeStale,
			BudgetOverrides:      query.BudgetOverrides.toGraphOverrides(),
			Subsets:              query.Subsets,
			Holdout:              *query.Holdout,
			Source:               query.Source,
			ExpectationClass:     strings.TrimSpace(query.ExpectationClass),
			Expected:             query.Expected,
			AcceptedAlternatives: query.AcceptedAlternatives,
			Negative:             query.Negative,
			Degradation:          query.Degradation,
			Metadata:             query.Metadata,
		})
	}
	return corpus, nil
}

func ValidateGraphCorpus(corpus GraphCorpus) error {
	if corpus.SchemaVersion != GraphCorpusSchemaVersion {
		return fmt.Errorf("unsupported graph corpus schema_version %d", corpus.SchemaVersion)
	}
	if len(corpus.Queries) == 0 {
		return fmt.Errorf("graph corpus contains no queries")
	}
	seen := make(map[string]bool, len(corpus.Queries))
	for i, query := range corpus.Queries {
		id := strings.TrimSpace(query.ID)
		if id == "" {
			return fmt.Errorf("graph query at index %d missing id", i)
		}
		if seen[id] {
			return fmt.Errorf("duplicate graph query id %q", id)
		}
		seen[id] = true
		if err := validateGraphQuery(query); err != nil {
			return fmt.Errorf("graph query %s: %w", id, err)
		}
	}
	return nil
}

func SelectGraphQueries(queries []GraphQuery, selection GraphSelection) ([]GraphQuery, error) {
	subset := strings.TrimSpace(selection.Subset)
	if subset == "" {
		subset = GraphSubsetFull
	}
	if err := validateGraphSubset(subset); err != nil {
		return nil, err
	}
	selected := make([]GraphQuery, 0, len(queries))
	for _, query := range queries {
		if query.Holdout {
			continue
		}
		if hasGraphSubset(query, subset) {
			selected = append(selected, query)
		}
	}
	return selected, nil
}

func normalizeGraphCorpus(corpus GraphCorpus) GraphCorpus {
	for i := range corpus.Queries {
		if strings.TrimSpace(corpus.Queries[i].ExpectationClass) == "" {
			corpus.Queries[i].ExpectationClass = GraphExpectationClassQuality
		}
		if len(corpus.Queries[i].Degradation.AllowedStatuses) == 0 {
			corpus.Queries[i].Degradation.AllowedStatuses = append([]graph.GraphStatus(nil), defaultAllowedGraphStatuses...)
		}
		if len(corpus.Queries[i].Degradation.BlockingStatuses) == 0 {
			corpus.Queries[i].Degradation.BlockingStatuses = append([]graph.GraphStatus(nil), defaultBlockingGraphStatuses...)
		}
	}
	return corpus
}

func validateGraphQuery(query GraphQuery) error {
	if strings.TrimSpace(query.Name) == "" {
		return fmt.Errorf("missing name")
	}
	if !allowedGraphModes[query.Mode] {
		return fmt.Errorf("unsupported graph mode %q", query.Mode)
	}
	if strings.TrimSpace(query.Query) == "" {
		return fmt.Errorf("missing query text")
	}
	if err := validateProjectRelativeGraphValue(query.Query); err != nil {
		return err
	}
	if err := validateGraphSubjectType(query.SubjectType); err != nil {
		return err
	}
	if query.Limit < 0 || query.Limit > maxGraphEvalLimit {
		return fmt.Errorf("limit must be between 0 and %d", maxGraphEvalLimit)
	}
	if len(query.Subsets) == 0 {
		return fmt.Errorf("missing subsets")
	}
	for _, subset := range query.Subsets {
		if err := validateGraphSubset(subset); err != nil {
			return err
		}
	}
	if strings.TrimSpace(query.Source) == "" {
		return fmt.Errorf("missing source")
	}
	// An empty class is accepted because normalizeGraphCorpus defaults it to
	// quality; any explicit value must be a known class so a typo cannot
	// silently misclassify a case out of the quality-threshold gate.
	if class := strings.TrimSpace(query.ExpectationClass); class != "" && !allowedGraphExpectationClasses[class] {
		return fmt.Errorf("unsupported expectation_class %q", class)
	}
	class := graphExpectationClassOrDefault(query.ExpectationClass)
	if class == GraphExpectationClassNegativeAdversarial {
		if err := validateGraphNegativeExpectation(query.Negative); err != nil {
			return fmt.Errorf("negative expectation: %w", err)
		}
	} else if len(query.Expected) == 0 {
		return fmt.Errorf("missing expected evidence")
	}
	if class == GraphExpectationClassNegativeAdversarial {
		return validateGraphDegradation(query.Degradation)
	}
	seenExpected := make(map[string]bool, len(query.Expected))
	for _, expected := range query.Expected {
		if err := validateGraphExpectedEvidence(expected); err != nil {
			return err
		}
		key := graphExpectedEvidenceIdentity(expected)
		if seenExpected[key] {
			return fmt.Errorf("duplicate expected evidence %q", key)
		}
		seenExpected[key] = true
	}
	// Accepted alternatives (TASK-GRA15) are optional and use the same matcher
	// discipline as expected rows: they widen what counts as relevant for
	// precision without becoming required for recall. They must not duplicate
	// each other or an expected row, so a single result cannot be double-counted.
	seenAlternative := make(map[string]bool, len(query.AcceptedAlternatives))
	for _, alternative := range query.AcceptedAlternatives {
		if err := validateGraphExpectedEvidence(alternative); err != nil {
			return fmt.Errorf("accepted alternative: %w", err)
		}
		key := graphExpectedEvidenceIdentity(alternative)
		if seenExpected[key] {
			return fmt.Errorf("accepted alternative duplicates expected evidence %q", key)
		}
		if seenAlternative[key] {
			return fmt.Errorf("duplicate accepted alternative %q", key)
		}
		seenAlternative[key] = true
	}
	return validateGraphDegradation(query.Degradation)
}

func validateGraphSubjectType(subjectType string) error {
	switch strings.TrimSpace(subjectType) {
	case "", graph.SubjectTypeAuto, graph.SubjectTypePath, graph.SubjectTypeSymbol, graph.SubjectTypePackage, graph.SubjectTypeResultID:
		return nil
	default:
		return fmt.Errorf("unsupported subject_type %q", subjectType)
	}
}

func validateGraphExpectedEvidence(expected GraphExpectedEvidence) error {
	if strings.TrimSpace(expected.Rationale) == "" {
		return fmt.Errorf("expected evidence missing rationale")
	}
	if !hasGraphExpectedMatcher(expected) {
		return fmt.Errorf("expected evidence without a matcher")
	}
	if expected.SourcePath != "" {
		if err := validateProjectRelativeGraphValue(expected.SourcePath); err != nil {
			return fmt.Errorf("expected source_path: %w", err)
		}
	}
	if expected.Role != "" && !allowedDirectGraphRoles[expected.Role] {
		return fmt.Errorf("unsupported role %q", expected.Role)
	}
	if expected.NodeKind != "" && !allowedGraphNodeKinds[expected.NodeKind] {
		return fmt.Errorf("unsupported node_kind %q", expected.NodeKind)
	}
	if expected.Relation != "" && !allowedGraphEdgeKinds[expected.Relation] {
		return fmt.Errorf("unsupported relation %q", expected.Relation)
	}
	if expected.ConfidenceLabel != "" && !allowedGraphConfidenceLabels[expected.ConfidenceLabel] {
		return fmt.Errorf("unsupported confidence_label %q", expected.ConfidenceLabel)
	}
	for _, token := range expected.GraphPathContains {
		if strings.TrimSpace(token) == "" {
			return fmt.Errorf("graph_path_contains has empty token")
		}
	}
	return nil
}

func validateGraphDegradation(degradation GraphDegradationExpectation) error {
	for _, status := range degradation.AllowedStatuses {
		if !allowedGraphStatuses[status] {
			return fmt.Errorf("unsupported graph status %q", status)
		}
	}
	for _, status := range degradation.BlockingStatuses {
		if !allowedGraphStatuses[status] {
			return fmt.Errorf("unsupported graph status %q", status)
		}
	}
	for _, code := range degradation.ExpectedWarningCodes {
		if !allowedGraphWarningCodes[code] {
			return fmt.Errorf("unsupported graph warning code %q", code)
		}
	}
	for _, label := range degradation.ExpectedLabels {
		if !isKnownGraphDegradationLabel(label) {
			return fmt.Errorf("unsupported degradation label %q", label)
		}
	}
	return nil
}

func hasGraphExpectedMatcher(expected GraphExpectedEvidence) bool {
	return strings.TrimSpace(expected.NodeID) != "" ||
		expected.NodeKind != "" ||
		strings.TrimSpace(expected.SourcePath) != "" ||
		strings.TrimSpace(expected.Role) != "" ||
		expected.Relation != "" ||
		expected.ConfidenceLabel != "" ||
		strings.TrimSpace(expected.EvidenceMethod) != "" ||
		len(expected.GraphPathContains) > 0
}

// graphExpectedEvidenceIdentity is the dedup key for AUTHORED corpus rows: it
// detects an author writing the same expectation twice. It deliberately spans ALL
// eight matcher fields, because two rows that differ in any matcher field are
// distinct expectations.
//
// This is intentionally a DIFFERENT, wider field set than graphResultIdentity
// (graph_runner.go), which keys RETURNED results for the precision denominator and
// covers only the stable, per-mode collapse fields (base source_path|node_kind|
// relation|role, plus node_id for impact_analysis). The two answer different
// questions — "is this authored expectation a duplicate?" vs "do these returned
// rows collapse for precision?" — so the divergence is by design, not drift. The
// runtime guard graphPrecisionIdentityAmbiguity bridges them: it fails fast if a
// matcher relies on a field outside graphResultIdentity in a way that would make
// precision order-dependent (DEBT-037 finding #6).
func graphExpectedEvidenceIdentity(expected GraphExpectedEvidence) string {
	return strings.Join([]string{
		strings.TrimSpace(expected.NodeID),
		string(expected.NodeKind),
		strings.TrimSpace(expected.SourcePath),
		strings.TrimSpace(expected.Role),
		string(expected.Relation),
		string(expected.ConfidenceLabel),
		strings.TrimSpace(expected.EvidenceMethod),
		strings.Join(expected.GraphPathContains, "\x1f"),
	}, "\x00")
}

func validateGraphSubset(subset string) error {
	subset = strings.TrimSpace(subset)
	switch {
	case subset == GraphSubsetQuick || subset == GraphSubsetFull:
		return nil
	case subset == "holdout":
		return fmt.Errorf("unsupported graph subset %q", subset)
	case strings.HasPrefix(subset, "mode:"):
		mode := strings.TrimPrefix(subset, "mode:")
		if !allowedGraphModes[mode] {
			return fmt.Errorf("unsupported graph subset mode %q", mode)
		}
		return nil
	default:
		return fmt.Errorf("unsupported graph subset %q", subset)
	}
}

func hasGraphSubset(query GraphQuery, subset string) bool {
	for _, candidate := range query.Subsets {
		if candidate == subset {
			return true
		}
	}
	return false
}

func (raw graphTraversalBudgetYAML) toGraphOverrides() graph.TraversalBudgetOverrides {
	return graph.TraversalBudgetOverrides{
		MaxResults:     raw.MaxResults,
		MaxNodes:       raw.MaxNodes,
		MaxPerEdgeKind: raw.MaxPerEdgeKind,
		MaxTokens:      raw.MaxTokens,
		MaxDepth:       raw.MaxDepth,
	}
}

func validateProjectRelativeGraphValue(value string) error {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "\x00") {
		return fmt.Errorf("query contains unsafe NUL byte")
	}
	slashed := strings.ReplaceAll(filepath.ToSlash(value), "\\", "/")
	if filepath.IsAbs(value) {
		return fmt.Errorf("query must be project-relative and safe")
	}
	for _, segment := range strings.Split(slashed, "/") {
		if segment == ".." {
			return fmt.Errorf("query must be project-relative and safe")
		}
	}
	return nil
}
