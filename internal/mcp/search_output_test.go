package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/search"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

func TestSDKSearchHandlers_DefaultOutputIncludesCompactQuality(t *testing.T) {
	tests := []struct {
		name string
		call func(*Server) (SearchOutput, error)
	}{
		{
			name: "search",
			call: func(srv *Server) (SearchOutput, error) {
				_, output, err := srv.mcpSearchHandler(context.Background(), nil, SearchInput{
					Query: "authentication middleware",
					Limit: 5,
				})
				return output, err
			},
		},
		{
			name: "search_code",
			call: func(srv *Server) (SearchOutput, error) {
				_, output, err := srv.mcpSearchCodeHandler(context.Background(), nil, SearchCodeInput{
					Query:    "authentication middleware",
					Language: "go",
					Limit:    5,
				})
				return output, err
			},
		},
		{
			name: "search_docs",
			call: func(srv *Server) (SearchOutput, error) {
				_, output, err := srv.mcpSearchDocsHandler(context.Background(), nil, SearchDocsInput{
					Query: "authentication middleware",
					Limit: 5,
				})
				return output, err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := &MockSearchEngine{
				SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
					assert.False(t, opts.Explain, "compact output must not enable verbose explain by default")
					return []*search.SearchResult{
						searchQualityResult("chunk-auth", "internal/auth/handler.go", 10, 40),
					}, nil
				},
				StatsFn: func() *search.EngineStats {
					return &search.EngineStats{
						BM25Stats:   &store.IndexStats{DocumentCount: 12},
						VectorCount: 12,
					}
				},
			}
			srv := newTestServerWithEngine(t, engine)

			output, err := tt.call(srv)

			require.NoError(t, err)
			require.Len(t, output.Results, 1)
			assert.NotEmpty(t, output.Results[0].ResultID)
			assert.Equal(t, "source_code", output.Results[0].SourceClass)
			assert.Equal(t, "active", output.Results[0].Authority)
			assert.Equal(t, "code", output.Results[0].Profile)
			assert.Equal(t, "internal/auth/handler.go", output.Results[0].SourcePath)
			assert.Equal(t, "search_quality.v1", output.SearchQuality.SchemaVersion)
			assert.Equal(t, "hybrid", output.SearchQuality.Mode)
			assert.False(t, output.SearchQuality.Degraded)
			assert.Equal(t, "none", output.SearchQuality.Reason)
			assert.Equal(t, 0.35, output.SearchQuality.Weights.BM25)
			assert.Equal(t, 0.65, output.SearchQuality.Weights.Semantic)
			assert.Equal(t, "MIXED", output.SearchQuality.QueryClass)
			assert.Equal(t, "unavailable", output.SearchQuality.QueryClassConfidenceState)
			assert.Equal(t, "not_configured", output.SearchQuality.Reranker.State)
			assert.Equal(t, "ready", output.SearchQuality.IndexFreshness.State)
			assert.Equal(t, "ast", output.SearchQuality.ChunkTelemetry.State)
			assert.Nil(t, output.SearchExplain)
			assert.Nil(t, output.Results[0].Explain)
		})
	}
}

func TestToSearchResultOutput_ExposesPDFMetadataFlat(t *testing.T) {
	output := ToSearchResultOutput(&search.SearchResult{
		Chunk: &store.Chunk{
			ID:          "pdf-1",
			FilePath:    "docs/spec.pdf",
			Content:     "PDF content",
			ContentType: store.ContentTypePDF,
			Language:    "pdf",
			Metadata: map[string]string{
				"content_type": "pdf",
				"chunker":      "pdf",
				"page_number":  "2",
				"page_start":   "2",
				"page_end":     "3",
			},
		},
		Score: 0.8,
	})

	assert.Equal(t, "pdf", output.ContentType)
	assert.Equal(t, "pdf", output.Chunker)
	assert.Equal(t, "2", output.PageNumber)
	assert.Equal(t, "2", output.PageStart)
	assert.Equal(t, "3", output.PageEnd)
}

func TestToSearchResultOutput_ReportsLanguageSupportTierPerResult(t *testing.T) {
	tests := []struct {
		name  string
		chunk *store.Chunk
		want  string
	}{
		{
			name: "tier 1 parser backed code",
			chunk: &store.Chunk{
				FilePath:    "internal/auth/handler.go",
				Content:     "func AuthMiddleware() {}",
				ContentType: store.ContentTypeCode,
				Language:    "go",
				Metadata:    map[string]string{"chunk_provenance": "ast"},
			},
			want: "tier_1_parser_backed",
		},
		{
			name: "tier 2 detected line fallback code",
			chunk: &store.Chunk{
				FilePath:    "src/lib.rs",
				Content:     "fn main() {}",
				ContentType: store.ContentTypeText,
				Language:    "rust",
				Metadata:    map[string]string{"chunk_provenance": "line_fallback"},
			},
			want: "tier_2_line_fallback",
		},
		{
			name: "tier 3 plain text fallback",
			chunk: &store.Chunk{
				FilePath:    "notes/unknown.txt",
				Content:     "plain text",
				ContentType: store.ContentTypeText,
			},
			want: "tier_3_plain_text",
		},
		{
			name: "document formats are not parser backed code support",
			chunk: &store.Chunk{
				FilePath:    "docs/spec.pdf",
				Content:     "PDF content",
				ContentType: store.ContentTypePDF,
				Language:    "pdf",
				Metadata:    map[string]string{"content_type": "pdf", "chunker": "pdf"},
			},
			want: "tier_3_plain_text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := ToSearchResultOutput(&search.SearchResult{Chunk: tt.chunk})

			assert.Equal(t, tt.want, output.LanguageSupportTier)
		})
	}
}

func TestBuildSearchOutput_DoesNotPutLanguageSupportTierOnSearchQuality(t *testing.T) {
	output := buildSearchOutput(searchOutputBuildContext{}, []*search.SearchResult{
		{
			Chunk: &store.Chunk{
				FilePath:    "internal/auth/handler.go",
				Content:     "func AuthMiddleware() {}",
				ContentType: store.ContentTypeCode,
				Language:    "go",
				Metadata:    map[string]string{"chunk_provenance": "ast"},
			},
		},
	}, nil)

	require.Len(t, output.Results, 1)
	assert.Equal(t, "tier_1_parser_backed", output.Results[0].LanguageSupportTier)

	encoded, err := json.Marshal(output.SearchQuality)
	require.NoError(t, err)

	var searchQualityFields map[string]any
	require.NoError(t, json.Unmarshal(encoded, &searchQualityFields))
	assert.NotContains(t, searchQualityFields, "language_support_tier")
	assert.NotContains(t, searchQualityFields, "language_support_tiers")
}

func TestSDKSearchHandler_ExplainIsOptIn(t *testing.T) {
	var capturedOpts []search.SearchOptions
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			capturedOpts = append(capturedOpts, opts)
			result := searchQualityResult("chunk-auth", "internal/auth/handler.go", 10, 40)
			if opts.Explain {
				result.Explain = &search.ExplainData{
					Query:             query,
					BM25ResultCount:   1,
					VectorResultCount: 1,
					Weights:           search.DefaultWeights(),
					RRFConstant:       search.DefaultRRFConstant,
				}
			}
			return []*search.SearchResult{result}, nil
		},
		StatsFn: func() *search.EngineStats {
			return &search.EngineStats{
				BM25Stats:   &store.IndexStats{DocumentCount: 1},
				VectorCount: 1,
			}
		},
	}
	srv := newTestServerWithEngine(t, engine)

	_, compact, err := srv.mcpSearchHandler(context.Background(), nil, SearchInput{
		Query: "auth",
	})
	require.NoError(t, err)
	require.Nil(t, compact.SearchExplain)
	require.Nil(t, compact.Results[0].Explain)

	_, verbose, err := srv.mcpSearchHandler(context.Background(), nil, SearchInput{
		Query:   "auth",
		Explain: true,
	})
	require.NoError(t, err)

	require.Len(t, capturedOpts, 2)
	assert.False(t, capturedOpts[0].Explain)
	assert.True(t, capturedOpts[1].Explain)
	require.NotNil(t, verbose.SearchExplain)
	assert.Equal(t, 1, verbose.SearchExplain.BM25ResultCount)
	assert.Equal(t, 1, verbose.SearchExplain.VectorResultCount)
	require.NotNil(t, verbose.Results[0].Explain)
	assert.Equal(t, 1, verbose.Results[0].Explain.BM25Rank)
	assert.Equal(t, 1, verbose.Results[0].Explain.VectorRank)
	assert.Equal(t, 0.77, verbose.Results[0].Explain.BM25Score)
	assert.Equal(t, 0.81, verbose.Results[0].Explain.VectorScore)
	assert.Equal(t, 0.92, verbose.Results[0].Explain.RRFScore)
}

func TestSDKSearchDocsHandler_DecisionsModeSetsProjectMemoryDefaults(t *testing.T) {
	var capturedOpts search.SearchOptions
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			capturedOpts = opts
			return []*search.SearchResult{}, nil
		},
	}
	srv := newTestServerWithEngine(t, engine)

	_, _, err := srv.mcpSearchDocsHandler(context.Background(), nil, SearchDocsInput{
		Query: "current ADR decisions",
		Mode:  "decisions",
	})

	require.NoError(t, err)
	assert.Equal(t, "docs", capturedOpts.Filter)
	assert.Equal(t, search.ProfileProjectMemory, capturedOpts.Profile)
	assert.Equal(t, search.SearchModeDecisions, capturedOpts.Mode)
	assert.Contains(t, capturedOpts.Scopes, ".aman-pm/decisions")
	assert.Contains(t, capturedOpts.Scopes, "docs/reference/decisions")
}

func TestSearchResultID_IsStableAndIndependentOfRank(t *testing.T) {
	first := searchQualityResult("chunk-auth", "internal/auth/handler.go", 10, 40)
	second := searchQualityResult("chunk-cache", "internal/cache/cache.go", 50, 70)

	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			return []*search.SearchResult{first, second}, nil
		},
		StatsFn: func() *search.EngineStats {
			return &search.EngineStats{
				BM25Stats:   &store.IndexStats{DocumentCount: 2},
				VectorCount: 2,
			}
		},
	}
	srv := newTestServerWithEngine(t, engine)

	_, initial, err := srv.mcpSearchHandler(context.Background(), nil, SearchInput{Query: "shared query"})
	require.NoError(t, err)
	initialByPath := resultIDsByPath(initial.Results)

	engine.SearchFn = func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
		return []*search.SearchResult{second, first}, nil
	}
	_, reordered, err := srv.mcpSearchHandler(context.Background(), nil, SearchInput{Query: "shared query"})
	require.NoError(t, err)

	assert.Equal(t, initialByPath, resultIDsByPath(reordered.Results))
	assert.NotEqual(t, reordered.Results[0].ResultID, reordered.Results[1].ResultID)
}

func TestSDKSearchHandler_ZeroResultsReturnsStructuredHint(t *testing.T) {
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			return []*search.SearchResult{}, nil
		},
		StatsFn: func() *search.EngineStats {
			return &search.EngineStats{
				BM25Stats:   &store.IndexStats{DocumentCount: 7},
				VectorCount: 7,
			}
		},
	}
	srv := newTestServerWithEngine(t, engine)

	_, output, err := srv.mcpSearchHandler(context.Background(), nil, SearchInput{Query: "missing term"})

	require.NoError(t, err)
	assert.Empty(t, output.Results)
	assert.Equal(t, "hybrid", output.SearchQuality.Mode)
	assert.True(t, output.SearchQuality.Degraded)
	assert.Equal(t, "zero_results", output.SearchQuality.Reason)
	require.Len(t, output.SearchQuality.Warnings, 1)
	assert.Equal(t, "zero_results", output.SearchQuality.Warnings[0].Code)
	assert.Equal(t, "broaden_query_or_check_index_status", output.SearchQuality.Warnings[0].NextAction)
}

func TestSDKSearchHandler_SearchQualityUsesRuntimeClassification(t *testing.T) {
	confidence := 0.82
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			require.NotNil(t, opts.QueryClassification)
			*opts.QueryClassification = search.QueryClassification{
				Type:            search.QueryTypeSemantic,
				Confidence:      &confidence,
				ConfidenceState: search.QueryClassificationConfidenceAvailable,
			}
			return []*search.SearchResult{
				searchQualityResult("chunk-auth", "internal/auth/handler.go", 10, 40),
			}, nil
		},
	}
	srv := newTestServerWithEngine(t, engine)

	_, output, err := srv.mcpSearchHandler(context.Background(), nil, SearchInput{Query: "how does authentication work"})

	require.NoError(t, err)
	assert.Equal(t, "SEMANTIC", output.SearchQuality.QueryClass)
	require.NotNil(t, output.SearchQuality.QueryClassConfidence)
	assert.Equal(t, confidence, *output.SearchQuality.QueryClassConfidence)
	assert.Equal(t, search.QueryClassificationConfidenceAvailable, output.SearchQuality.QueryClassConfidenceState)
}

func TestSDKSearchHandler_SearchQualityUsesRuntimeRerankerState(t *testing.T) {
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			require.NotNil(t, opts.RerankerStatus)
			*opts.RerankerStatus = search.RerankerStatus{State: search.RerankerStateApplied}
			return []*search.SearchResult{
				searchQualityResult("chunk-auth", "internal/auth/handler.go", 10, 40),
			}, nil
		},
	}
	srv := newTestServerWithEngine(t, engine)

	_, output, err := srv.mcpSearchHandler(context.Background(), nil, SearchInput{Query: "authentication"})

	require.NoError(t, err)
	assert.Equal(t, search.RerankerStateApplied, output.SearchQuality.Reranker.State)
	assert.Empty(t, output.SearchQuality.Reranker.SkipReason)
}

func TestSDKSearchHandler_ProfileMismatchReturnsStructuredHint(t *testing.T) {
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			raw := []*search.SearchResult{
				{
					Chunk: &store.Chunk{
						ID:          "review-authority",
						FilePath:    "vend_feedback/gpt/f39-authority.md",
						Content:     "Authority metadata recommendation",
						ContentType: store.ContentTypeMarkdown,
						Language:    "markdown",
					},
					Score: 0.9,
				},
			}
			return search.ApplyFilters(raw, opts), nil
		},
		StatsFn: func() *search.EngineStats {
			return &search.EngineStats{
				BM25Stats:   &store.IndexStats{DocumentCount: 7},
				VectorCount: 7,
			}
		},
	}
	srv := newTestServerWithEngine(t, engine)

	_, output, err := srv.mcpSearchHandler(context.Background(), nil, SearchInput{
		Query:   "authority metadata recommendation",
		Profile: "project-memory",
	})

	require.NoError(t, err)
	assert.Empty(t, output.Results)
	assert.True(t, output.SearchQuality.Degraded)
	assert.Equal(t, "profile_mismatch", output.SearchQuality.Reason)
	require.Len(t, output.ProfileMismatches, 1)
	assert.Equal(t, "vend_feedback/gpt/f39-authority.md", output.ProfileMismatches[0].SourcePath)
	assert.Equal(t, "project-memory", output.ProfileMismatches[0].RequestedProfile)
	assert.Equal(t, "review-corpus", output.ProfileMismatches[0].RequiredProfile)
	assert.Equal(t, "review_corpus", output.ProfileMismatches[0].SourceClass)
	assert.Equal(t, "advisory", output.ProfileMismatches[0].Authority)
	require.Len(t, output.SearchQuality.Warnings, 1)
	assert.Equal(t, "profile_mismatch", output.SearchQuality.Warnings[0].Code)
}

func TestLegacyCallTool_SearchShowsProfileMismatchHint(t *testing.T) {
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			raw := []*search.SearchResult{
				{
					Chunk: &store.Chunk{
						ID:          "review-authority",
						FilePath:    "vend_feedback/gpt/f39-authority.md",
						Content:     "Authority metadata recommendation",
						ContentType: store.ContentTypeMarkdown,
						Language:    "markdown",
					},
					Score: 0.9,
				},
			}
			return search.ApplyFilters(raw, opts), nil
		},
	}
	srv := newTestServerWithEngine(t, engine)

	result, err := srv.CallTool(context.Background(), "search", map[string]any{
		"query":   "authority metadata recommendation",
		"profile": "project-memory",
	})

	require.NoError(t, err)
	text, ok := result.(string)
	require.True(t, ok)
	assert.Contains(t, text, "Profile mismatch")
	assert.Contains(t, text, "review-corpus")
}

func TestLegacyCallTool_SearchStillReturnsMarkdownString(t *testing.T) {
	engine := &MockSearchEngine{
		SearchFn: func(ctx context.Context, query string, opts search.SearchOptions) ([]*search.SearchResult, error) {
			return []*search.SearchResult{
				searchQualityResult("chunk-auth", "internal/auth/handler.go", 10, 40),
			}, nil
		},
	}
	srv := newTestServerWithEngine(t, engine)

	result, err := srv.CallTool(context.Background(), "search", map[string]any{
		"query": "auth",
	})

	require.NoError(t, err)
	text, ok := result.(string)
	require.True(t, ok, "legacy CallTool must keep returning markdown strings, got %T", result)
	assert.Contains(t, text, "## Search Results")
	assert.NotContains(t, text, "search_quality")
	assert.NotContains(t, text, "result_id")
}

func searchQualityResult(id, filePath string, startLine, endLine int) *search.SearchResult {
	return &search.SearchResult{
		Chunk: &store.Chunk{
			ID:        id,
			FilePath:  filePath,
			StartLine: startLine,
			EndLine:   endLine,
			Content:   "func AuthMiddleware() {}",
			Language:  "go",
			Metadata: map[string]string{
				"chunk_provenance": "ast",
			},
			Symbols: []*store.Symbol{
				{Name: "AuthMiddleware", Type: store.SymbolTypeFunction},
			},
		},
		Score:        0.92,
		BM25Score:    0.77,
		VecScore:     0.81,
		BM25Rank:     1,
		VecRank:      1,
		InBothLists:  true,
		MatchedTerms: []string{"auth"},
	}
}

func resultIDsByPath(results []SearchResultOutput) map[string]string {
	ids := make(map[string]string, len(results))
	for _, result := range results {
		ids[result.FilePath] = result.ResultID
	}
	return ids
}
