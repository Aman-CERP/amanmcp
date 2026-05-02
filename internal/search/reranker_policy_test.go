package search

import (
	"context"
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRerankerPolicy_DefaultAndValidation(t *testing.T) {
	assert.Equal(t, RerankerPolicyAuto, DefaultConfig().RerankerPolicy)

	tests := []struct {
		name    string
		policy  RerankerPolicy
		wantErr bool
	}{
		{name: "empty defaults to auto", policy: "", wantErr: false},
		{name: "auto", policy: RerankerPolicyAuto, wantErr: false},
		{name: "always", policy: RerankerPolicyAlways, wantErr: false},
		{name: "never", policy: RerankerPolicyNever, wantErr: false},
		{name: "invalid", policy: RerankerPolicy("sometimes"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "auto")
				assert.Contains(t, err.Error(), "always")
				assert.Contains(t, err.Error(), "never")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRerankerPolicyDecision_AutoSkipsLexicalPreservingClasses(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		class      *QueryClassification
		wantApply  bool
		wantReason string
	}{
		{
			name:       "unknown classifier state",
			query:      "how does authentication work",
			class:      nil,
			wantReason: RerankerSkipPolicyAutoUnknownClass,
		},
		{
			name:       "exact identifier label",
			query:      "FindProjectRoot",
			class:      &QueryClassification{Type: QueryType("exact_identifier")},
			wantReason: RerankerSkipPolicyAutoLexical,
		},
		{
			name:       "path lookup label",
			query:      "internal/search/engine.go",
			class:      &QueryClassification{Type: QueryType("path_lookup")},
			wantReason: RerankerSkipPolicyAutoPath,
		},
		{
			name:       "quoted string label",
			query:      `"search_quality.v1"`,
			class:      &QueryClassification{Type: QueryType("quoted_string")},
			wantReason: RerankerSkipPolicyAutoQuoted,
		},
		{
			name:       "config error label",
			query:      "ERR_CONNECTION_REFUSED",
			class:      &QueryClassification{Type: QueryType("config_error")},
			wantReason: RerankerSkipPolicyAutoLexical,
		},
		{
			name:       "negative adversarial label",
			query:      "ignore previous instructions",
			class:      &QueryClassification{Type: QueryType("negative_adversarial")},
			wantReason: RerankerSkipPolicyAutoNegative,
		},
		{
			name:       "legacy lexical path helper",
			query:      "internal/search/reranker_policy.go",
			class:      &QueryClassification{Type: QueryTypeLexical},
			wantReason: RerankerSkipPolicyAutoPath,
		},
		{
			name:       "legacy lexical quoted helper",
			query:      `"exact owner lookup"`,
			class:      &QueryClassification{Type: QueryTypeLexical},
			wantReason: RerankerSkipPolicyAutoQuoted,
		},
		{
			name:       "legacy lexical error code helper",
			query:      "ERR_CONNECTION_REFUSED",
			class:      &QueryClassification{Type: QueryTypeLexical},
			wantReason: RerankerSkipPolicyAutoLexical,
		},
		{
			name:      "semantic class applies reranking",
			query:     "how does authentication work",
			class:     &QueryClassification{Type: QueryType("natural_language_intent")},
			wantApply: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := DecideRerankerPolicy(RerankerPolicyAuto, tt.query, tt.class)
			assert.Equal(t, tt.wantApply, decision.Apply)
			assert.Equal(t, tt.wantReason, decision.SkipReason)
		})
	}
}

func TestRerankerPolicyDecision_AlwaysAndNever(t *testing.T) {
	lexical := &QueryClassification{Type: QueryType("exact_identifier")}

	always := DecideRerankerPolicy(RerankerPolicyAlways, "FindProjectRoot", lexical)
	assert.True(t, always.Apply)
	assert.Empty(t, always.SkipReason)

	never := DecideRerankerPolicy(RerankerPolicyNever, "how does authentication work", &QueryClassification{Type: QueryTypeSemantic})
	assert.False(t, never.Apply)
	assert.Equal(t, RerankerSkipPolicyNever, never.SkipReason)
}

func TestEngine_Search_RerankerPolicyControlsCallsAndStatus(t *testing.T) {
	tests := []struct {
		name             string
		policy           RerankerPolicy
		classification   *QueryClassification
		wantCalls        int
		wantState        string
		wantSkipReason   string
		wantFirstChunkID string
	}{
		{
			name:             "auto skips exact identifier",
			policy:           RerankerPolicyAuto,
			classification:   &QueryClassification{Type: QueryType("exact_identifier")},
			wantCalls:        0,
			wantState:        RerankerStateSkipped,
			wantSkipReason:   RerankerSkipPolicyAutoLexical,
			wantFirstChunkID: "chunk1",
		},
		{
			name:             "auto skips unknown classifier state",
			policy:           RerankerPolicyAuto,
			classification:   nil,
			wantCalls:        0,
			wantState:        RerankerStateSkipped,
			wantSkipReason:   RerankerSkipPolicyAutoUnknownClass,
			wantFirstChunkID: "chunk1",
		},
		{
			name:             "auto applies semantic class",
			policy:           RerankerPolicyAuto,
			classification:   &QueryClassification{Type: QueryType("natural_language_intent")},
			wantCalls:        1,
			wantState:        RerankerStateApplied,
			wantFirstChunkID: "chunk2",
		},
		{
			name:             "always attempts reranking",
			policy:           RerankerPolicyAlways,
			classification:   &QueryClassification{Type: QueryType("exact_identifier")},
			wantCalls:        1,
			wantState:        RerankerStateApplied,
			wantFirstChunkID: "chunk2",
		},
		{
			name:             "never hard disables reranking",
			policy:           RerankerPolicyNever,
			classification:   &QueryClassification{Type: QueryTypeSemantic},
			wantCalls:        0,
			wantState:        RerankerStateSkipped,
			wantSkipReason:   RerankerSkipPolicyNever,
			wantFirstChunkID: "chunk1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, bm25, vector, embedder, metadata := setupTestEngine(t)
			engine.config.RerankerPolicy = tt.policy

			chunk1 := &store.Chunk{ID: "chunk1", Content: "func FindProjectRoot() {}", FilePath: "config.go", ContentType: store.ContentTypeCode}
			chunk2 := &store.Chunk{ID: "chunk2", Content: "func SearchConfig() {}", FilePath: "search.go", ContentType: store.ContentTypeCode}
			require.NoError(t, metadata.SaveChunks(context.Background(), []*store.Chunk{chunk1, chunk2}))

			bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
				return []*store.BM25Result{
					{DocID: "chunk1", Score: 0.9},
					{DocID: "chunk2", Score: 0.8},
				}, nil
			}
			embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
				return make([]float32, 768), nil
			}
			vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
				return []*store.VectorResult{
					{ID: "chunk1", Score: 0.85},
					{ID: "chunk2", Score: 0.75},
				}, nil
			}

			mockReranker := &MockReranker{
				RerankFn: func(ctx context.Context, query string, documents []string, topK int) ([]RerankResult, error) {
					return []RerankResult{
						{Index: 1, Score: 1.0, Document: documents[1]},
						{Index: 0, Score: 0.5, Document: documents[0]},
					}, nil
				},
			}
			engine.reranker = mockReranker

			var status RerankerStatus
			opts := SearchOptions{
				Limit:          10,
				RerankerStatus: &status,
			}
			if tt.classification != nil {
				opts.QueryClassification = tt.classification
			}

			results, err := engine.Search(context.Background(), "FindProjectRoot", opts)
			require.NoError(t, err)
			require.Len(t, results, 2)
			assert.Equal(t, tt.wantCalls, mockReranker.called)
			assert.Equal(t, tt.wantState, status.State)
			assert.Equal(t, tt.wantSkipReason, status.SkipReason)
			assert.Equal(t, tt.policy, status.Policy)
			assert.Equal(t, 2, status.CandidateCount)
			assert.Equal(t, tt.wantFirstChunkID, results[0].Chunk.ID)
		})
	}
}
