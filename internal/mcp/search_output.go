package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/Aman-CERP/amanmcp/internal/search"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

const (
	searchQualitySchemaVersion = "search_quality.v1"
	resultIDVersion            = "sr1"
)

type SearchQualityOutput struct {
	SchemaVersion             string                     `json:"schema_version"`
	Mode                      string                     `json:"mode"`
	Degraded                  bool                       `json:"degraded"`
	Reason                    string                     `json:"reason"`
	Weights                   SearchWeightsOutput        `json:"weights"`
	QueryClass                string                     `json:"query_class"`
	QueryClassConfidence      *float64                   `json:"query_class_confidence,omitempty"`
	QueryClassConfidenceState string                     `json:"query_class_confidence_state"`
	Reranker                  SearchRerankerOutput       `json:"reranker"`
	IndexFreshness            SearchIndexFreshnessOutput `json:"index_freshness"`
	ChunkTelemetry            SearchChunkTelemetryOutput `json:"chunk_telemetry"`
	Warnings                  []SearchWarningOutput      `json:"warnings,omitempty"`
}

type SearchWeightsOutput struct {
	BM25     float64 `json:"bm25"`
	Semantic float64 `json:"semantic"`
}

type SearchRerankerOutput struct {
	Policy         string `json:"policy"`
	State          string `json:"state"`
	SkipReason     string `json:"skip_reason,omitempty"`
	CandidateCount int    `json:"candidate_count"`
	RerankedCount  int    `json:"reranked_count"`
	LatencyMS      int64  `json:"latency_ms"`
}

type SearchIndexFreshnessOutput struct {
	State         string `json:"state"`
	DocumentCount int    `json:"document_count"`
	VectorCount   int    `json:"vector_count"`
}

type SearchChunkTelemetryOutput struct {
	State             string  `json:"state"`
	ResultCount       int     `json:"result_count"`
	ASTCount          int     `json:"ast_count,omitempty"`
	LineFallbackCount int     `json:"line_fallback_count,omitempty"`
	UnavailableCount  int     `json:"unavailable_count,omitempty"`
	LineFallbackRate  float64 `json:"line_fallback_rate,omitempty"`
}

type SearchWarningOutput struct {
	Code       string `json:"code"`
	NextAction string `json:"next_action"`
}

type ProfileMismatchOutput struct {
	SourcePath       string `json:"source_path"`
	RequestedProfile string `json:"requested_profile,omitempty"`
	RequiredProfile  string `json:"required_profile"`
	SourceClass      string `json:"source_class"`
	Authority        string `json:"authority"`
	Reason           string `json:"reason"`
	Action           string `json:"action"`
}

type SearchExplainOutput struct {
	BM25ResultCount      int                   `json:"bm25_result_count"`
	VectorResultCount    int                   `json:"vector_result_count"`
	Weights              SearchWeightsOutput   `json:"weights"`
	RRFConstant          int                   `json:"rrf_constant"`
	BM25Only             bool                  `json:"bm25_only,omitempty"`
	DimensionMismatch    bool                  `json:"dimension_mismatch,omitempty"`
	MultiQueryDecomposed bool                  `json:"multi_query_decomposed,omitempty"`
	SubQueries           []string              `json:"sub_queries,omitempty"`
	Reranker             SearchRerankerOutput  `json:"reranker"`
	Warnings             []SearchWarningOutput `json:"warnings,omitempty"`
}

type SearchResultExplainOutput struct {
	BM25Rank    int     `json:"bm25_rank,omitempty"`
	BM25Score   float64 `json:"bm25_score,omitempty"`
	VectorRank  int     `json:"vector_rank,omitempty"`
	VectorScore float64 `json:"vector_score,omitempty"`
	RRFScore    float64 `json:"rrf_score,omitempty"`
	InBothLists bool    `json:"in_both_lists,omitempty"`
}

type searchOutputBuildContext struct {
	ToolName       string
	Query          string
	Options        search.SearchOptions
	Stats          *search.EngineStats
	IndexingStatus string
}

// BuildSearchOutput converts engine results into the structured MCP search response.
func (s *Server) BuildSearchOutput(
	toolName, query string,
	opts search.SearchOptions,
	results []*search.SearchResult,
	profileMismatches []search.ProfileMismatch,
) SearchOutput {
	s.mu.RLock()
	progress := s.indexProgress
	s.mu.RUnlock()

	indexingStatus := ""
	if progress != nil {
		indexingStatus = progress.Snapshot().Status
	}

	ctx := searchOutputBuildContext{
		ToolName:       toolName,
		Query:          query,
		Options:        opts,
		Stats:          s.engine.Stats(),
		IndexingStatus: indexingStatus,
	}

	return buildSearchOutput(ctx, results, profileMismatches)
}

func buildSearchOutput(ctx searchOutputBuildContext, results []*search.SearchResult, profileMismatches []search.ProfileMismatch) SearchOutput {
	validResults := filterValidResults(results)
	quality := buildSearchQuality(ctx, validResults, profileMismatches)

	output := SearchOutput{
		Results:           make([]SearchResultOutput, 0, len(validResults)),
		SearchQuality:     quality,
		ProfileMismatches: toProfileMismatchOutputs(profileMismatches),
	}

	for _, r := range validResults {
		output.Results = append(output.Results, toSearchResultOutput(ctx.ToolName, ctx.Query, r, ctx.Options.Explain))
	}

	if ctx.Options.Explain {
		output.SearchExplain = buildSearchExplain(ctx, validResults, quality)
	}

	return output
}

func buildSearchQuality(ctx searchOutputBuildContext, results []*search.SearchResult, profileMismatches []search.ProfileMismatch) SearchQualityOutput {
	weights := effectiveWeights(ctx.Options, results)
	mode, reason, degraded, warnings := qualityMode(results, ctx)
	indexFreshness := buildIndexFreshness(ctx)
	queryClass, queryConfidence, queryConfidenceState := queryClassificationOutput(ctx.Options, weights)

	if indexFreshness.State == "indexing" && reason == "none" {
		degraded = true
		reason = "indexing"
		warnings = append(warnings, SearchWarningOutput{
			Code:       "indexing",
			NextAction: "retry_after_indexing",
		})
	}

	if len(profileMismatches) > 0 && len(results) > 0 {
		warnings = append(warnings, SearchWarningOutput{
			Code:       "profile_mismatch_partial",
			NextAction: "inspect_profile_mismatches_for_excluded_sources",
		})
	}

	if len(results) == 0 {
		degraded = true
		if len(profileMismatches) > 0 {
			reason = "profile_mismatch"
			warnings = append(warnings, SearchWarningOutput{
				Code:       "profile_mismatch",
				NextAction: "select_required_profile_from_profile_mismatches",
			})
		} else if indexFreshness.State == "empty" {
			reason = "empty_index"
			warnings = append(warnings, SearchWarningOutput{
				Code:       "empty_index",
				NextAction: "run_index",
			})
		} else {
			reason = "zero_results"
			warnings = append(warnings, SearchWarningOutput{
				Code:       "zero_results",
				NextAction: "broaden_query_or_check_index_status",
			})
		}
	}

	return SearchQualityOutput{
		SchemaVersion:             searchQualitySchemaVersion,
		Mode:                      mode,
		Degraded:                  degraded,
		Reason:                    reason,
		Weights:                   weightsOutput(weights),
		QueryClass:                string(queryClass),
		QueryClassConfidence:      queryConfidence,
		QueryClassConfidenceState: queryConfidenceState,
		Reranker:                  rerankerOutput(ctx.Options),
		IndexFreshness:            indexFreshness,
		ChunkTelemetry:            buildChunkTelemetry(results),
		Warnings:                  dedupeWarnings(warnings),
	}
}

func buildSearchExplain(ctx searchOutputBuildContext, results []*search.SearchResult, quality SearchQualityOutput) *SearchExplainOutput {
	explain := firstExplain(results)
	output := &SearchExplainOutput{
		BM25ResultCount:   countBM25Results(results),
		VectorResultCount: countVectorResults(results),
		Weights:           quality.Weights,
		RRFConstant:       search.DefaultRRFConstant,
		Reranker:          quality.Reranker,
		Warnings:          quality.Warnings,
	}

	if explain != nil {
		output.BM25ResultCount = explain.BM25ResultCount
		output.VectorResultCount = explain.VectorResultCount
		output.Weights = weightsOutput(explain.Weights)
		output.RRFConstant = explain.RRFConstant
		output.BM25Only = explain.BM25Only
		output.DimensionMismatch = explain.DimensionMismatch
		output.MultiQueryDecomposed = explain.MultiQueryDecomposed
		output.SubQueries = append([]string(nil), explain.SubQueries...)
	}

	return output
}

func toProfileMismatchOutputs(mismatches []search.ProfileMismatch) []ProfileMismatchOutput {
	if len(mismatches) == 0 {
		return nil
	}

	output := make([]ProfileMismatchOutput, 0, len(mismatches))
	for _, mismatch := range mismatches {
		output = append(output, ProfileMismatchOutput{
			SourcePath:       mismatch.SourcePath,
			RequestedProfile: string(mismatch.RequestedProfile),
			RequiredProfile:  string(mismatch.RequiredProfile),
			SourceClass:      string(mismatch.SourceClass),
			Authority:        string(mismatch.Authority),
			Reason:           mismatch.Reason,
			Action:           mismatch.Action,
		})
	}
	return output
}

func toSearchResultOutput(toolName, query string, r *search.SearchResult, explain bool) SearchResultOutput {
	output := ToSearchResultOutput(r)
	output.ResultID = stableSearchResultID(toolName, query, r)
	if explain {
		output.Explain = &SearchResultExplainOutput{
			BM25Rank:    r.BM25Rank,
			BM25Score:   r.BM25Score,
			VectorRank:  r.VecRank,
			VectorScore: r.VecScore,
			RRFScore:    r.Score,
			InBothLists: r.InBothLists,
		}
	}
	return output
}

func stableSearchResultID(toolName, query string, r *search.SearchResult) string {
	if r == nil || r.Chunk == nil {
		return ""
	}

	chunk := r.Chunk
	symbol := ""
	if len(chunk.Symbols) > 0 && chunk.Symbols[0] != nil {
		symbol = chunk.Symbols[0].Name
	}

	contentHash := sha256.Sum256([]byte(chunk.Content))
	tuple := strings.Join([]string{
		resultIDVersion,
		toolName,
		normalizeResultIDQuery(query),
		stableEvidencePath(chunk.FilePath),
		chunk.ID,
		strconv.Itoa(chunk.StartLine),
		strconv.Itoa(chunk.EndLine),
		symbol,
		hex.EncodeToString(contentHash[:8]),
	}, "\x00")

	sum := sha256.Sum256([]byte(tuple))
	return fmt.Sprintf("%s_%s", resultIDVersion, hex.EncodeToString(sum[:12]))
}

func normalizeResultIDQuery(query string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(query))), " ")
}

func stableEvidencePath(filePath string) string {
	clean := path.Clean(strings.ReplaceAll(filePath, "\\", "/"))
	if clean == "." {
		return ""
	}
	return strings.TrimPrefix(clean, "/")
}

func effectiveWeights(opts search.SearchOptions, results []*search.SearchResult) search.Weights {
	if explain := firstExplain(results); explain != nil {
		return explain.Weights
	}
	if opts.Weights != nil {
		return *opts.Weights
	}
	if opts.BM25Only {
		return search.Weights{BM25: 1, Semantic: 0}
	}
	return search.DefaultWeights()
}

func qualityMode(results []*search.SearchResult, ctx searchOutputBuildContext) (mode string, reason string, degraded bool, warnings []SearchWarningOutput) {
	if explain := firstExplain(results); explain != nil && explain.DimensionMismatch {
		return "bm25_only", "dimension_mismatch", true, []SearchWarningOutput{{
			Code:       "dimension_mismatch",
			NextAction: "reindex_with_current_embedder",
		}}
	}
	if ctx.Options.BM25Only {
		return "bm25_only", "none", false, nil
	}

	hasBM25 := false
	hasVector := false
	for _, r := range results {
		if r.BM25Rank > 0 {
			hasBM25 = true
		}
		if r.VecRank > 0 {
			hasVector = true
		}
	}

	switch {
	case hasBM25 && hasVector:
		return "hybrid", "none", false, nil
	case hasBM25:
		return "bm25_only", "vector_unavailable", true, []SearchWarningOutput{{
			Code:       "vector_unavailable",
			NextAction: "check_embedder_or_reindex",
		}}
	case hasVector:
		return "vector_only", "bm25_unavailable", true, []SearchWarningOutput{{
			Code:       "bm25_unavailable",
			NextAction: "check_keyword_index",
		}}
	default:
		return "hybrid", "none", false, nil
	}
}

func buildIndexFreshness(ctx searchOutputBuildContext) SearchIndexFreshnessOutput {
	output := SearchIndexFreshnessOutput{State: "unavailable"}
	if ctx.Stats != nil {
		if ctx.Stats.BM25Stats != nil {
			output.DocumentCount = ctx.Stats.BM25Stats.DocumentCount
		}
		output.VectorCount = ctx.Stats.VectorCount
		if output.DocumentCount == 0 && output.VectorCount == 0 {
			output.State = "empty"
		} else {
			output.State = "ready"
		}
	}

	if ctx.IndexingStatus == "indexing" {
		output.State = "indexing"
	}

	return output
}

func buildChunkTelemetry(results []*search.SearchResult) SearchChunkTelemetryOutput {
	output := SearchChunkTelemetryOutput{
		State:       "unavailable",
		ResultCount: len(results),
	}

	for _, r := range results {
		source := chunkProvenance(r.Chunk)
		switch source {
		case "ast":
			output.ASTCount++
		case "line_fallback":
			output.LineFallbackCount++
		default:
			output.UnavailableCount++
		}
	}

	available := output.ASTCount + output.LineFallbackCount
	if available == 0 {
		return output
	}

	switch {
	case output.ASTCount > 0 && output.LineFallbackCount > 0:
		output.State = "mixed"
	case output.LineFallbackCount > 0:
		output.State = "line_fallback"
	default:
		output.State = "ast"
	}
	output.LineFallbackRate = float64(output.LineFallbackCount) / float64(available)
	return output
}

func chunkProvenance(chunk *store.Chunk) string {
	if chunk == nil || len(chunk.Metadata) == 0 {
		return ""
	}
	for _, key := range []string{"chunk_provenance", "chunk_method", "chunker"} {
		switch strings.ToLower(chunk.Metadata[key]) {
		case "ast", "tree_sitter", "treesitter":
			return "ast"
		case "line", "lines", "line_fallback", "fallback":
			return "line_fallback"
		}
	}
	return ""
}

func queryClassFromWeights(weights search.Weights) search.QueryType {
	defaultWeights := search.DefaultWeights()
	if weights == defaultWeights {
		return search.QueryTypeMixed
	}
	if weights.BM25 > 0.6 {
		return search.QueryTypeLexical
	}
	if weights.Semantic > 0.6 {
		return search.QueryTypeSemantic
	}
	return search.QueryTypeMixed
}

func queryClassificationOutput(opts search.SearchOptions, weights search.Weights) (search.QueryType, *float64, string) {
	if opts.QueryClassification != nil && opts.QueryClassification.Type != "" {
		state := opts.QueryClassification.ConfidenceState
		if state == "" {
			if opts.QueryClassification.Confidence != nil {
				state = search.QueryClassificationConfidenceAvailable
			} else {
				state = search.QueryClassificationConfidenceNotReported
			}
		}
		return opts.QueryClassification.Type, opts.QueryClassification.Confidence, state
	}
	return queryClassFromWeights(weights), nil, search.QueryClassificationConfidenceUnavailable
}

func rerankerOutput(opts search.SearchOptions) SearchRerankerOutput {
	if opts.RerankerStatus == nil || opts.RerankerStatus.State == "" {
		return SearchRerankerOutput{
			Policy: string(search.RerankerPolicyAuto),
			State:  search.RerankerStateNotConfigured,
		}
	}
	policy := opts.RerankerStatus.Policy
	if policy == "" {
		policy = search.RerankerPolicyAuto
	}
	return SearchRerankerOutput{
		Policy:         string(policy),
		State:          opts.RerankerStatus.State,
		SkipReason:     opts.RerankerStatus.SkipReason,
		CandidateCount: opts.RerankerStatus.CandidateCount,
		RerankedCount:  opts.RerankerStatus.RerankedCount,
		LatencyMS:      opts.RerankerStatus.LatencyMS,
	}
}

func weightsOutput(weights search.Weights) SearchWeightsOutput {
	return SearchWeightsOutput{
		BM25:     weights.BM25,
		Semantic: weights.Semantic,
	}
}

func firstExplain(results []*search.SearchResult) *search.ExplainData {
	for _, r := range results {
		if r != nil && r.Explain != nil {
			return r.Explain
		}
	}
	return nil
}

func countBM25Results(results []*search.SearchResult) int {
	count := 0
	for _, r := range results {
		if r != nil && r.BM25Rank > 0 {
			count++
		}
	}
	return count
}

func countVectorResults(results []*search.SearchResult) int {
	count := 0
	for _, r := range results {
		if r != nil && r.VecRank > 0 {
			count++
		}
	}
	return count
}

func dedupeWarnings(warnings []SearchWarningOutput) []SearchWarningOutput {
	if len(warnings) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(warnings))
	deduped := make([]SearchWarningOutput, 0, len(warnings))
	for _, warning := range warnings {
		key := warning.Code + "\x00" + warning.NextAction
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, warning)
	}
	return deduped
}
