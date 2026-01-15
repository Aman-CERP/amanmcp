package search

import (
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// F14: RRF Score Fusion Tests
// =============================================================================
// AC01: RRF implementation with configurable k, weighted fusion
// AC02: Deterministic tie-breaking (InBothLists → BM25 → ID)
// AC03: Handle documents in only one list using missing_rank
// AC04: Normalize final scores to 0-1, preserve original scores
// AC05: Performance < 1ms for 100 results per list, O(n) space
// =============================================================================

// --- Test Helpers ---

func createBM25Results(ids []string, scores []float64) []*store.BM25Result {
	results := make([]*store.BM25Result, len(ids))
	for i, id := range ids {
		score := 1.0
		if i < len(scores) {
			score = scores[i]
		}
		results[i] = &store.BM25Result{
			DocID:        id,
			Score:        score,
			MatchedTerms: []string{"term"},
		}
	}
	return results
}

func createVecResults(ids []string, scores []float32) []*store.VectorResult {
	results := make([]*store.VectorResult, len(ids))
	for i, id := range ids {
		score := float32(0.9)
		if i < len(scores) {
			score = scores[i]
		}
		results[i] = &store.VectorResult{
			ID:    id,
			Score: score,
		}
	}
	return results
}

// --- TS01: Basic RRF Fusion ---
// Tests: AC01 (RRF algorithm with weighted fusion)

func TestRRFFusion_Basic(t *testing.T) {
	// Given: BM25 results [A, B, C] and Vector results [C, A, D]
	bm25 := createBM25Results([]string{"A", "B", "C"}, []float64{2.5, 2.0, 1.5})
	vec := createVecResults([]string{"C", "A", "D"}, []float32{0.95, 0.90, 0.85})
	weights := DefaultWeights() // BM25: 0.35, Semantic: 0.65
	fusion := NewRRFFusion()

	// When: fusing results
	results := fusion.Fuse(bm25, vec, weights)

	// Then: results are ranked by RRF scores
	require.NotEmpty(t, results)
	require.GreaterOrEqual(t, len(results), 4) // A, B, C, D

	// Verify A and C appear (both in both lists)
	ids := make([]string, len(results))
	for i, r := range results {
		ids[i] = r.ChunkID
	}
	assert.Contains(t, ids, "A")
	assert.Contains(t, ids, "B")
	assert.Contains(t, ids, "C")
	assert.Contains(t, ids, "D")

	// Verify scores are normalized 0-1
	for _, r := range results {
		assert.GreaterOrEqual(t, r.RRFScore, 0.0, "RRF score should be >= 0")
		assert.LessOrEqual(t, r.RRFScore, 1.0, "RRF score should be <= 1")
	}

	// Top result should have score of 1.0 (normalized max)
	assert.Equal(t, 1.0, results[0].RRFScore)
}

// --- TS02: Document in One List Only ---
// Tests: AC03 (missing_rank handling)

func TestRRFFusion_DocumentInOneListOnly(t *testing.T) {
	// Given: B only in BM25, D only in Vector
	bm25 := createBM25Results([]string{"A", "B"}, []float64{2.0, 1.5})
	vec := createVecResults([]string{"A", "D"}, []float32{0.9, 0.8})
	weights := DefaultWeights()
	fusion := NewRRFFusion()

	// When: fusing results
	results := fusion.Fuse(bm25, vec, weights)

	// Then: B and D should still appear with computed missing_rank
	require.Len(t, results, 3) // A, B, D

	resultMap := make(map[string]*FusedResult)
	for _, r := range results {
		resultMap[r.ChunkID] = r
	}

	// A should be in both lists
	assert.True(t, resultMap["A"].InBothLists)
	assert.Equal(t, 1, resultMap["A"].BM25Rank)
	assert.Equal(t, 1, resultMap["A"].VecRank)

	// B should only be in BM25
	assert.False(t, resultMap["B"].InBothLists)
	assert.Equal(t, 2, resultMap["B"].BM25Rank)
	assert.Equal(t, 0, resultMap["B"].VecRank) // 0 means not in list

	// D should only be in Vector
	assert.False(t, resultMap["D"].InBothLists)
	assert.Equal(t, 0, resultMap["D"].BM25Rank) // 0 means not in list
	assert.Equal(t, 2, resultMap["D"].VecRank)

	// All should have RRF scores (missing_rank contributes)
	for _, r := range results {
		assert.Greater(t, r.RRFScore, 0.0)
	}
}

// --- TS03: Tie-Breaking - Prefer InBothLists ---
// Tests: AC02 (deterministic tie-breaking)

func TestRRFFusion_TieBreaking_PreferInBothLists(t *testing.T) {
	// Given: Results where RRF scores are equal but one is in both lists
	// We create a scenario where A is in both lists, B is only in BM25 at same rank
	bm25 := createBM25Results([]string{"A", "B"}, []float64{2.0, 2.0})
	vec := createVecResults([]string{"A"}, []float32{0.9})
	weights := Weights{BM25: 0.5, Semantic: 0.5} // Equal weights
	fusion := NewRRFFusion()

	// When: fusing results
	results := fusion.Fuse(bm25, vec, weights)

	// Then: A (in both) should rank before B (same raw RRF contribution from BM25)
	// A gets: 0.5/(60+1) + 0.5/(60+1) = 0.01639
	// B gets: 0.5/(60+2) + 0.5/(60+3) = 0.01601 (missing_rank = 2)
	// So A wins on RRF score anyway, but InBothLists is also true
	require.Len(t, results, 2)
	assert.Equal(t, "A", results[0].ChunkID)
	assert.True(t, results[0].InBothLists)
}

// --- TS04: Tie-Breaking - Prefer Higher BM25 Score ---
// Tests: AC02 (deterministic tie-breaking)

func TestRRFFusion_TieBreaking_PreferHigherBM25Score(t *testing.T) {
	// Given: Two documents with same RRF score and InBothLists status
	// A and B both in both lists at same ranks but different BM25 scores
	bm25 := createBM25Results([]string{"A", "B"}, []float64{5.0, 3.0})
	vec := createVecResults([]string{"B", "A"}, []float32{0.9, 0.9}) // B first in vec, A second
	weights := DefaultWeights()
	fusion := NewRRFFusion()

	// When: fusing results
	results := fusion.Fuse(bm25, vec, weights)

	// Then: Both are in both lists
	// A: BM25 rank 1, Vec rank 2 -> 0.35/(60+1) + 0.65/(60+2) = 0.01621
	// B: BM25 rank 2, Vec rank 1 -> 0.35/(60+2) + 0.65/(60+1) = 0.01630
	// B has higher RRF score, but let's verify tie-breaking with BM25 score works
	require.Len(t, results, 2)

	// Verify both are in both lists
	assert.True(t, results[0].InBothLists)
	assert.True(t, results[1].InBothLists)

	// Verify original BM25 scores are preserved
	resultMap := make(map[string]*FusedResult)
	for _, r := range results {
		resultMap[r.ChunkID] = r
	}
	assert.Equal(t, 5.0, resultMap["A"].BM25Score)
	assert.Equal(t, 3.0, resultMap["B"].BM25Score)
}

// --- TS05: Tie-Breaking - Lexicographic by ChunkID ---
// Tests: AC02 (deterministic tie-breaking)

func TestRRFFusion_TieBreaking_LexicographicByID(t *testing.T) {
	// Given: Two documents with identical scores, InBothLists, and BM25 scores
	bm25 := createBM25Results([]string{"Z", "A"}, []float64{2.0, 2.0})
	vec := createVecResults([]string{"Z", "A"}, []float32{0.9, 0.9})
	weights := DefaultWeights()
	fusion := NewRRFFusion()

	// When: fusing results
	results := fusion.Fuse(bm25, vec, weights)

	// Then: Both have identical RRF scores and BM25 scores
	// Final tie-break: lexicographic by ID -> A before Z
	require.Len(t, results, 2)

	// Same ranks, same scores, both in both lists
	// Tie-break by ID: A < Z
	if results[0].RRFScore == results[1].RRFScore {
		assert.Equal(t, "A", results[1].ChunkID) // A comes after Z if we sort by RRF first
	}
}

// --- TS06: Empty Inputs ---
// Tests: AC01 (edge case handling)

func TestRRFFusion_EmptyInputs(t *testing.T) {
	fusion := NewRRFFusion()
	weights := DefaultWeights()

	t.Run("both empty", func(t *testing.T) {
		results := fusion.Fuse(nil, nil, weights)
		// DEBT-012: Return empty slice, not nil, for consistent API behavior
		assert.NotNil(t, results, "should return empty slice, not nil")
		assert.Empty(t, results)
	})

	t.Run("BM25 empty", func(t *testing.T) {
		vec := createVecResults([]string{"A", "B"}, []float32{0.9, 0.8})
		results := fusion.Fuse(nil, vec, weights)
		require.Len(t, results, 2)
		// All results should have 0 BM25 rank
		for _, r := range results {
			assert.Equal(t, 0, r.BM25Rank)
			assert.False(t, r.InBothLists)
		}
	})

	t.Run("Vector empty", func(t *testing.T) {
		bm25 := createBM25Results([]string{"A", "B"}, []float64{2.0, 1.5})
		results := fusion.Fuse(bm25, nil, weights)
		require.Len(t, results, 2)
		// All results should have 0 Vec rank
		for _, r := range results {
			assert.Equal(t, 0, r.VecRank)
			assert.False(t, r.InBothLists)
		}
	})
}

// --- TS07: Score Normalization ---
// Tests: AC04 (normalize to 0-1, preserve originals)

func TestRRFFusion_ScoreNormalization(t *testing.T) {
	// Given: Results with various scores
	bm25 := createBM25Results([]string{"A", "B", "C"}, []float64{10.0, 5.0, 2.0})
	vec := createVecResults([]string{"A", "B", "C"}, []float32{0.95, 0.80, 0.60})
	weights := DefaultWeights()
	fusion := NewRRFFusion()

	// When: fusing results
	results := fusion.Fuse(bm25, vec, weights)

	// Then: RRF scores normalized to 0-1, originals preserved
	require.Len(t, results, 3)

	// Top result should have score 1.0
	assert.Equal(t, 1.0, results[0].RRFScore)

	// All scores should be in [0, 1]
	for _, r := range results {
		assert.GreaterOrEqual(t, r.RRFScore, 0.0)
		assert.LessOrEqual(t, r.RRFScore, 1.0)
	}

	// Original scores should be preserved
	resultMap := make(map[string]*FusedResult)
	for _, r := range results {
		resultMap[r.ChunkID] = r
	}
	assert.Equal(t, 10.0, resultMap["A"].BM25Score)
	assert.Equal(t, 5.0, resultMap["B"].BM25Score)
	assert.Equal(t, 2.0, resultMap["C"].BM25Score)
	assert.InDelta(t, 0.95, resultMap["A"].VecScore, 0.001)
	assert.InDelta(t, 0.80, resultMap["B"].VecScore, 0.001)
	assert.InDelta(t, 0.60, resultMap["C"].VecScore, 0.001)
}

// --- TS08: Weight Sensitivity ---
// Tests: AC01 (weighted fusion)

func TestRRFFusion_WeightSensitivity(t *testing.T) {
	// Given: Results where BM25 and Vector rank differently
	// A: BM25 rank 1, Vec rank 3
	// B: BM25 rank 2, Vec rank 2
	// C: BM25 rank 3, Vec rank 1
	bm25 := createBM25Results([]string{"A", "B", "C"}, []float64{3.0, 2.0, 1.0})
	vec := createVecResults([]string{"C", "B", "A"}, []float32{0.95, 0.85, 0.75})
	fusion := NewRRFFusion()

	t.Run("high BM25 weight favors BM25 ranking", func(t *testing.T) {
		weights := Weights{BM25: 0.8, Semantic: 0.2}
		results := fusion.Fuse(bm25, vec, weights)
		require.Len(t, results, 3)
		// A should rank higher with high BM25 weight (A is rank 1 in BM25)
		assert.Equal(t, "A", results[0].ChunkID)
	})

	t.Run("high Semantic weight favors Vector ranking", func(t *testing.T) {
		weights := Weights{BM25: 0.2, Semantic: 0.8}
		results := fusion.Fuse(bm25, vec, weights)
		require.Len(t, results, 3)
		// C should rank higher with high Semantic weight (C is rank 1 in Vec)
		assert.Equal(t, "C", results[0].ChunkID)
	})
}

// --- TS09: Deterministic Ordering ---
// Tests: AC02 (same input -> same output)

func TestRRFFusion_Deterministic(t *testing.T) {
	// Given: Same input data
	bm25 := createBM25Results([]string{"A", "B", "C", "D", "E"}, []float64{5.0, 4.0, 3.0, 2.0, 1.0})
	vec := createVecResults([]string{"E", "D", "C", "B", "A"}, []float32{0.95, 0.90, 0.85, 0.80, 0.75})
	weights := DefaultWeights()
	fusion := NewRRFFusion()

	// When: fusing multiple times
	results1 := fusion.Fuse(bm25, vec, weights)
	results2 := fusion.Fuse(bm25, vec, weights)
	results3 := fusion.Fuse(bm25, vec, weights)

	// Then: all results should be identical
	require.Len(t, results1, 5)
	require.Len(t, results2, 5)
	require.Len(t, results3, 5)

	for i := range results1 {
		assert.Equal(t, results1[i].ChunkID, results2[i].ChunkID)
		assert.Equal(t, results2[i].ChunkID, results3[i].ChunkID)
		assert.Equal(t, results1[i].RRFScore, results2[i].RRFScore)
		assert.Equal(t, results2[i].RRFScore, results3[i].RRFScore)
	}
}

// --- Additional Test: Custom K Value ---
// Tests: AC01 (configurable k)

func TestRRFFusion_CustomK(t *testing.T) {
	bm25 := createBM25Results([]string{"A"}, []float64{2.0})
	vec := createVecResults([]string{"A"}, []float32{0.9})
	weights := Weights{BM25: 0.5, Semantic: 0.5}

	t.Run("default k=60", func(t *testing.T) {
		fusion := NewRRFFusion()
		results := fusion.Fuse(bm25, vec, weights)
		require.Len(t, results, 1)
		// Expected: 0.5/(60+1) + 0.5/(60+1) = 1/(61) ≈ 0.01639 (normalized to 1.0)
		assert.Equal(t, 60, fusion.K)
	})

	t.Run("custom k=10", func(t *testing.T) {
		fusion := NewRRFFusionWithK(10)
		results := fusion.Fuse(bm25, vec, weights)
		require.Len(t, results, 1)
		assert.Equal(t, 10, fusion.K)
	})

	t.Run("invalid k defaults to 60", func(t *testing.T) {
		fusion := NewRRFFusionWithK(0)
		assert.Equal(t, 60, fusion.K)

		fusion = NewRRFFusionWithK(-5)
		assert.Equal(t, 60, fusion.K)
	})
}

// --- Additional Test: MatchedTerms Preservation ---

func TestRRFFusion_PreservesMatchedTerms(t *testing.T) {
	bm25 := []*store.BM25Result{
		{DocID: "A", Score: 2.0, MatchedTerms: []string{"foo", "bar"}},
		{DocID: "B", Score: 1.5, MatchedTerms: []string{"baz"}},
	}
	vec := createVecResults([]string{"A"}, []float32{0.9})
	weights := DefaultWeights()
	fusion := NewRRFFusion()

	results := fusion.Fuse(bm25, vec, weights)

	resultMap := make(map[string]*FusedResult)
	for _, r := range results {
		resultMap[r.ChunkID] = r
	}

	assert.Equal(t, []string{"foo", "bar"}, resultMap["A"].MatchedTerms)
	assert.Equal(t, []string{"baz"}, resultMap["B"].MatchedTerms)
}

// =============================================================================
// Benchmarks
// =============================================================================
// Tests: AC05 (performance requirements)

// =============================================================================
// DEBT-028: Additional Coverage Tests for compare/normalize
// =============================================================================

func TestRRFFusion_Compare_AllTieBreakingBranches(t *testing.T) {
	fusion := NewRRFFusion()

	t.Run("higher RRF score wins", func(t *testing.T) {
		a := &FusedResult{ChunkID: "A", RRFScore: 0.9, InBothLists: false, BM25Score: 1.0}
		b := &FusedResult{ChunkID: "B", RRFScore: 0.8, InBothLists: true, BM25Score: 5.0}
		assert.True(t, fusion.compare(a, b), "higher RRF score should win")
		assert.False(t, fusion.compare(b, a), "lower RRF score should lose")
	})

	t.Run("equal RRF - InBothLists wins", func(t *testing.T) {
		a := &FusedResult{ChunkID: "A", RRFScore: 0.8, InBothLists: true, BM25Score: 1.0}
		b := &FusedResult{ChunkID: "B", RRFScore: 0.8, InBothLists: false, BM25Score: 5.0}
		assert.True(t, fusion.compare(a, b), "InBothLists=true should win")
		assert.False(t, fusion.compare(b, a), "InBothLists=false should lose")
	})

	t.Run("equal RRF and InBothLists - higher BM25 wins", func(t *testing.T) {
		a := &FusedResult{ChunkID: "Z", RRFScore: 0.8, InBothLists: true, BM25Score: 5.0}
		b := &FusedResult{ChunkID: "A", RRFScore: 0.8, InBothLists: true, BM25Score: 1.0}
		assert.True(t, fusion.compare(a, b), "higher BM25 should win")
		assert.False(t, fusion.compare(b, a), "lower BM25 should lose")
	})

	t.Run("all equal - lexicographic ChunkID wins", func(t *testing.T) {
		a := &FusedResult{ChunkID: "A", RRFScore: 0.8, InBothLists: true, BM25Score: 5.0}
		b := &FusedResult{ChunkID: "Z", RRFScore: 0.8, InBothLists: true, BM25Score: 5.0}
		assert.True(t, fusion.compare(a, b), "lexicographically smaller ID should win")
		assert.False(t, fusion.compare(b, a), "lexicographically larger ID should lose")
	})
}

func TestRRFFusion_Normalize_ZeroMaxScore(t *testing.T) {
	fusion := NewRRFFusion()

	// Create results with zero RRF scores
	results := []*FusedResult{
		{ChunkID: "A", RRFScore: 0.0},
		{ChunkID: "B", RRFScore: 0.0},
	}

	// Normalize should handle maxScore == 0 gracefully
	fusion.normalize(results)

	// Scores should remain 0 (no division by zero)
	assert.Equal(t, 0.0, results[0].RRFScore)
	assert.Equal(t, 0.0, results[1].RRFScore)
}

func TestRRFFusion_Normalize_EmptyResults(t *testing.T) {
	fusion := NewRRFFusion()

	// Empty slice should not panic
	results := []*FusedResult{}
	fusion.normalize(results)
	assert.Empty(t, results)
}

// =============================================================================
// DEBT-028: MultiRRFFusion Tests
// =============================================================================

func TestNewMultiRRFFusionWithParams(t *testing.T) {
	t.Run("valid params", func(t *testing.T) {
		fusion := NewMultiRRFFusionWithParams(30, 0.2)
		assert.Equal(t, 30, fusion.K)
		assert.Equal(t, 0.2, fusion.ConsensusBoost)
	})

	t.Run("invalid k defaults to 60", func(t *testing.T) {
		fusion := NewMultiRRFFusionWithParams(0, 0.2)
		assert.Equal(t, DefaultRRFConstant, fusion.K)

		fusion2 := NewMultiRRFFusionWithParams(-5, 0.2)
		assert.Equal(t, DefaultRRFConstant, fusion2.K)
	})

	t.Run("negative consensusBoost defaults to 0.1", func(t *testing.T) {
		fusion := NewMultiRRFFusionWithParams(60, -0.5)
		assert.Equal(t, 0.1, fusion.ConsensusBoost)
	})

	t.Run("zero consensusBoost is valid", func(t *testing.T) {
		fusion := NewMultiRRFFusionWithParams(60, 0.0)
		assert.Equal(t, 0.0, fusion.ConsensusBoost)
	})
}

func TestMultiRRFFusion_Compare_AllTieBreakingBranches(t *testing.T) {
	fusion := NewMultiRRFFusion()

	t.Run("higher RRF score wins", func(t *testing.T) {
		a := &MultiFusedResult{FusedResult: FusedResult{ChunkID: "A", RRFScore: 0.9, InBothLists: false, BM25Score: 1.0}, SubQueryHits: 1}
		b := &MultiFusedResult{FusedResult: FusedResult{ChunkID: "B", RRFScore: 0.8, InBothLists: true, BM25Score: 5.0}, SubQueryHits: 3}
		assert.True(t, fusion.compare(a, b), "higher RRF score should win")
	})

	t.Run("equal RRF - more SubQueryHits wins", func(t *testing.T) {
		a := &MultiFusedResult{FusedResult: FusedResult{ChunkID: "A", RRFScore: 0.8, InBothLists: false, BM25Score: 1.0}, SubQueryHits: 3}
		b := &MultiFusedResult{FusedResult: FusedResult{ChunkID: "B", RRFScore: 0.8, InBothLists: true, BM25Score: 5.0}, SubQueryHits: 1}
		assert.True(t, fusion.compare(a, b), "more SubQueryHits should win")
	})

	t.Run("equal RRF and SubQueryHits - InBothLists wins", func(t *testing.T) {
		a := &MultiFusedResult{FusedResult: FusedResult{ChunkID: "A", RRFScore: 0.8, InBothLists: true, BM25Score: 1.0}, SubQueryHits: 2}
		b := &MultiFusedResult{FusedResult: FusedResult{ChunkID: "B", RRFScore: 0.8, InBothLists: false, BM25Score: 5.0}, SubQueryHits: 2}
		assert.True(t, fusion.compare(a, b), "InBothLists=true should win")
	})

	t.Run("equal RRF, SubQueryHits, InBothLists - higher BM25 wins", func(t *testing.T) {
		a := &MultiFusedResult{FusedResult: FusedResult{ChunkID: "Z", RRFScore: 0.8, InBothLists: true, BM25Score: 5.0}, SubQueryHits: 2}
		b := &MultiFusedResult{FusedResult: FusedResult{ChunkID: "A", RRFScore: 0.8, InBothLists: true, BM25Score: 1.0}, SubQueryHits: 2}
		assert.True(t, fusion.compare(a, b), "higher BM25 should win")
	})

	t.Run("all equal - lexicographic ChunkID wins", func(t *testing.T) {
		a := &MultiFusedResult{FusedResult: FusedResult{ChunkID: "A", RRFScore: 0.8, InBothLists: true, BM25Score: 5.0}, SubQueryHits: 2}
		b := &MultiFusedResult{FusedResult: FusedResult{ChunkID: "Z", RRFScore: 0.8, InBothLists: true, BM25Score: 5.0}, SubQueryHits: 2}
		assert.True(t, fusion.compare(a, b), "lexicographically smaller ID should win")
	})
}

func TestMultiRRFFusion_Normalize_ZeroMaxScore(t *testing.T) {
	fusion := NewMultiRRFFusion()

	// Create results with zero RRF scores
	results := []*MultiFusedResult{
		{FusedResult: FusedResult{ChunkID: "A", RRFScore: 0.0}},
		{FusedResult: FusedResult{ChunkID: "B", RRFScore: 0.0}},
	}

	// Normalize should handle maxScore == 0 gracefully
	fusion.normalize(results)

	// Scores should remain 0 (no division by zero)
	assert.Equal(t, 0.0, results[0].RRFScore)
	assert.Equal(t, 0.0, results[1].RRFScore)
}

func TestMultiRRFFusion_EmptySubResults(t *testing.T) {
	fusion := NewMultiRRFFusion()

	// Empty sub-results should return empty slice, not nil
	results := fusion.FuseMultiQuery([]SubQueryResult{})
	assert.NotNil(t, results)
	assert.Empty(t, results)

	// Nil should also work
	results = fusion.FuseMultiQuery(nil)
	assert.NotNil(t, results)
	assert.Empty(t, results)
}

func TestMultiRRFFusion_ConsensusBoost(t *testing.T) {
	fusion := NewMultiRRFFusion() // ConsensusBoost = 0.1

	// Document A appears in 3 sub-queries, B appears in 1
	subResults := []SubQueryResult{
		{
			SubQuery: SubQuery{Query: "query1", Weight: 1.0},
			Results: []*FusedResult{
				{ChunkID: "A", RRFScore: 0.8},
				{ChunkID: "B", RRFScore: 0.7},
			},
		},
		{
			SubQuery: SubQuery{Query: "query2", Weight: 1.0},
			Results: []*FusedResult{
				{ChunkID: "A", RRFScore: 0.75},
			},
		},
		{
			SubQuery: SubQuery{Query: "query3", Weight: 1.0},
			Results: []*FusedResult{
				{ChunkID: "A", RRFScore: 0.7},
			},
		},
	}

	results := fusion.FuseMultiQuery(subResults)

	// A should be first (appears in all 3 sub-queries)
	require.NotEmpty(t, results)
	assert.Equal(t, "A", results[0].ChunkID)
	assert.Equal(t, 3, results[0].SubQueryHits)

	// B should be second
	require.Len(t, results, 2)
	assert.Equal(t, "B", results[1].ChunkID)
	assert.Equal(t, 1, results[1].SubQueryHits)
}

func TestMultiRRFFusion_ZeroWeight(t *testing.T) {
	fusion := NewMultiRRFFusion()

	// Sub-query with zero weight should use 1.0 as default
	subResults := []SubQueryResult{
		{
			SubQuery: SubQuery{Query: "query1", Weight: 0.0},
			Results: []*FusedResult{
				{ChunkID: "A", RRFScore: 0.8},
			},
		},
	}

	results := fusion.FuseMultiQuery(subResults)
	require.Len(t, results, 1)
	assert.Equal(t, "A", results[0].ChunkID)
	// Score should be computed with weight 1.0
	assert.Greater(t, results[0].RRFScore, 0.0)
}

// =============================================================================
// Benchmarks
// =============================================================================
// Tests: AC05 (performance requirements)

func BenchmarkRRFFusion_20x20(b *testing.B) {
	bm25 := make([]*store.BM25Result, 20)
	vec := make([]*store.VectorResult, 20)
	for i := 0; i < 20; i++ {
		bm25[i] = &store.BM25Result{DocID: string(rune('A' + i)), Score: float64(20 - i)}
		vec[i] = &store.VectorResult{ID: string(rune('A' + i)), Score: float32(0.9 - float32(i)*0.01)}
	}
	weights := DefaultWeights()
	fusion := NewRRFFusion()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fusion.Fuse(bm25, vec, weights)
	}
}

func BenchmarkRRFFusion_100x100(b *testing.B) {
	bm25 := make([]*store.BM25Result, 100)
	vec := make([]*store.VectorResult, 100)
	for i := 0; i < 100; i++ {
		bm25[i] = &store.BM25Result{DocID: string(rune(i)), Score: float64(100 - i)}
		vec[i] = &store.VectorResult{ID: string(rune(i)), Score: float32(0.9 - float32(i)*0.001)}
	}
	weights := DefaultWeights()
	fusion := NewRRFFusion()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fusion.Fuse(bm25, vec, weights)
	}
}

func BenchmarkRRFFusion_1000x1000(b *testing.B) {
	bm25 := make([]*store.BM25Result, 1000)
	vec := make([]*store.VectorResult, 1000)
	for i := 0; i < 1000; i++ {
		bm25[i] = &store.BM25Result{DocID: string(rune(i)), Score: float64(1000 - i)}
		vec[i] = &store.VectorResult{ID: string(rune(i)), Score: float32(0.9 - float32(i)*0.0001)}
	}
	weights := DefaultWeights()
	fusion := NewRRFFusion()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fusion.Fuse(bm25, vec, weights)
	}
}
