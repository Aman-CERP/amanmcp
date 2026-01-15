// Package search provides hybrid search functionality combining BM25 and semantic search.
// Results are fused using Reciprocal Rank Fusion (RRF).
package search

import (
	"sort"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

// DefaultRRFConstant is the standard RRF smoothing parameter.
// k=60 is empirically validated across domains (used by Azure AI Search, OpenSearch, etc.).
const DefaultRRFConstant = 60

// FusedResult represents a single result after RRF fusion.
type FusedResult struct {
	ChunkID      string   // Chunk identifier
	RRFScore     float64  // Combined RRF score (normalized 0-1)
	BM25Score    float64  // Original BM25 score (preserved)
	BM25Rank     int      // Position in BM25 list (1-indexed, 0 if absent)
	VecScore     float64  // Original vector similarity score (preserved)
	VecRank      int      // Position in vector list (1-indexed, 0 if absent)
	InBothLists  bool     // Document appeared in both result lists
	MatchedTerms []string // BM25 matched terms (for highlighting)
}

// RRFFusion combines BM25 and vector search results using
// Reciprocal Rank Fusion algorithm.
//
// Algorithm: RRF_score(d) = Σ weight_i / (k + rank_i)
//
// Where:
//   - k = smoothing constant (default: 60)
//   - rank_i = position in ranked list i (1-indexed)
//   - weight_i = weight for search source i
type RRFFusion struct {
	K int // RRF smoothing constant (default: 60)
}

// NewRRFFusion creates a new RRF fusion instance with default k=60.
func NewRRFFusion() *RRFFusion {
	return &RRFFusion{K: DefaultRRFConstant}
}

// NewRRFFusionWithK creates a new RRF fusion with custom k value.
// If k <= 0, defaults to 60.
func NewRRFFusionWithK(k int) *RRFFusion {
	if k <= 0 {
		k = DefaultRRFConstant
	}
	return &RRFFusion{K: k}
}

// Fuse combines BM25 and vector results using Reciprocal Rank Fusion.
//
// Documents appearing in only one list use missing_rank = max(len(bm25), len(vec)) + 1
// for the missing source's contribution.
//
// Results are sorted by: RRFScore (desc) → InBothLists (true first) → BM25Score (desc) → ChunkID (asc)
func (f *RRFFusion) Fuse(
	bm25 []*store.BM25Result,
	vec []*store.VectorResult,
	weights Weights,
) []*FusedResult {
	// Return empty slice, not nil, for consistent API behavior (DEBT-012)
	if len(bm25) == 0 && len(vec) == 0 {
		return []*FusedResult{}
	}

	// Build result map with RRF scores
	capacity := len(bm25) + len(vec)
	scores := make(map[string]*FusedResult, capacity)

	// Process BM25 results (1-indexed ranks)
	for rank, r := range bm25 {
		result := f.getOrCreate(scores, r.DocID)
		result.BM25Score = r.Score
		result.BM25Rank = rank + 1
		result.MatchedTerms = r.MatchedTerms
		result.RRFScore += weights.BM25 / float64(f.K+rank+1)
	}

	// Process vector results (1-indexed ranks)
	for rank, r := range vec {
		result := f.getOrCreate(scores, r.ID)
		result.VecScore = float64(r.Score)
		result.VecRank = rank + 1
		result.RRFScore += weights.Semantic / float64(f.K+rank+1)

		// Mark if in both lists
		if result.BM25Rank > 0 {
			result.InBothLists = true
		}
	}

	// Handle documents in only one list (use missing_rank)
	missingRank := f.calculateMissingRank(len(bm25), len(vec))
	for _, r := range scores {
		if r.BM25Rank == 0 && r.VecRank > 0 {
			// Document only in vector results - add BM25 contribution at missing_rank
			r.RRFScore += weights.BM25 / float64(f.K+missingRank)
		}
		if r.VecRank == 0 && r.BM25Rank > 0 {
			// Document only in BM25 results - add semantic contribution at missing_rank
			r.RRFScore += weights.Semantic / float64(f.K+missingRank)
		}
	}

	// Convert to sorted slice
	results := f.toSortedSlice(scores)

	// Normalize scores to 0-1 range
	f.normalize(results)

	return results
}

// getOrCreate returns existing result or creates new one.
func (f *RRFFusion) getOrCreate(m map[string]*FusedResult, id string) *FusedResult {
	if r, ok := m[id]; ok {
		return r
	}
	r := &FusedResult{ChunkID: id}
	m[id] = r
	return r
}

// calculateMissingRank returns rank for documents not in a list.
// Uses max(len1, len2) + 1 to penalize missing documents appropriately.
func (f *RRFFusion) calculateMissingRank(bm25Len, vecLen int) int {
	if bm25Len > vecLen {
		return bm25Len + 1
	}
	return vecLen + 1
}

// toSortedSlice converts map to slice and sorts by RRF score with tie-breaking.
func (f *RRFFusion) toSortedSlice(m map[string]*FusedResult) []*FusedResult {
	results := make([]*FusedResult, 0, len(m))
	for _, r := range m {
		results = append(results, r)
	}

	sort.Slice(results, func(i, j int) bool {
		return f.compare(results[i], results[j])
	})

	return results
}

// compare implements deterministic comparison for sorting.
// Returns true if a should rank before b.
//
// Priority:
//  1. Higher RRF score
//  2. In both lists (true before false)
//  3. Higher BM25 score (exact match indicator)
//  4. Lexicographically smaller ChunkID (deterministic)
func (f *RRFFusion) compare(a, b *FusedResult) bool {
	// Primary: Higher RRF score ranks first
	if a.RRFScore != b.RRFScore {
		return a.RRFScore > b.RRFScore
	}

	// Tie-break 1: Prefer documents in both lists
	if a.InBothLists != b.InBothLists {
		return a.InBothLists
	}

	// Tie-break 2: Prefer higher BM25 score (exact match indicator)
	if a.BM25Score != b.BM25Score {
		return a.BM25Score > b.BM25Score
	}

	// Tie-break 3: Lexicographic by ChunkID (deterministic)
	return a.ChunkID < b.ChunkID
}

// normalize scales all RRF scores to 0-1 range.
// Uses the maximum score as the reference (becomes 1.0).
func (f *RRFFusion) normalize(results []*FusedResult) {
	if len(results) == 0 {
		return
	}

	// Results are sorted, first has max score
	maxScore := results[0].RRFScore
	if maxScore == 0 {
		return
	}

	for _, r := range results {
		r.RRFScore = r.RRFScore / maxScore
	}
}
