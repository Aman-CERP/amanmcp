// Package search provides hybrid search functionality combining BM25 and semantic search.
package search

import (
	"sort"
)

// SubQueryResult represents results from a single sub-query execution.
// Used by MultiRRFFusion to combine results from multiple sub-queries.
type SubQueryResult struct {
	// SubQuery is the sub-query that produced these results.
	SubQuery SubQuery

	// Results are the search results for this sub-query.
	// These are pre-fused results from the hybrid BM25+vector search.
	Results []*FusedResult
}

// MultiFusedResult extends FusedResult with multi-query fusion metadata.
type MultiFusedResult struct {
	FusedResult

	// SubQueryHits is the number of sub-queries this document appeared in.
	// Higher values indicate consensus across multiple query formulations.
	SubQueryHits int
}

// MultiRRFFusion combines results from multiple sub-queries using
// an extended Reciprocal Rank Fusion algorithm.
//
// FEAT-QI3: This addresses generic query failures by fusing results
// from multiple specific sub-queries. Documents appearing in multiple
// sub-query results get boosted (consensus signal).
//
// Algorithm:
//
//	multi_rrf_score(d) = Σ (sub_weight_i / (k + rank_i)) * (1 + boost * (hits - 1))
//
// Where:
//   - k = smoothing constant (default: 60)
//   - sub_weight_i = weight for sub-query i
//   - rank_i = position in sub-query i results (1-indexed)
//   - hits = number of sub-queries where document appears
//   - boost = consensus boost factor (default: 0.1)
type MultiRRFFusion struct {
	K             int     // RRF smoothing constant (default: 60)
	ConsensusBoost float64 // Boost per additional sub-query hit (default: 0.1)
}

// NewMultiRRFFusion creates a new multi-query RRF fusion with default parameters.
func NewMultiRRFFusion() *MultiRRFFusion {
	return &MultiRRFFusion{
		K:             DefaultRRFConstant, // 60
		ConsensusBoost: 0.1,              // 10% boost per additional hit
	}
}

// NewMultiRRFFusionWithParams creates a multi-query RRF fusion with custom parameters.
func NewMultiRRFFusionWithParams(k int, consensusBoost float64) *MultiRRFFusion {
	if k <= 0 {
		k = DefaultRRFConstant
	}
	if consensusBoost < 0 {
		consensusBoost = 0.1
	}
	return &MultiRRFFusion{
		K:             k,
		ConsensusBoost: consensusBoost,
	}
}

// FuseMultiQuery combines results from multiple sub-queries using RRF.
//
// The algorithm:
// 1. Aggregate RRF scores across all sub-queries (weighted by sub-query weight)
// 2. Track how many sub-queries each document appears in (consensus count)
// 3. Apply consensus boost: documents in multiple sub-queries get boosted
// 4. Sort by final score, with tie-breaking by consensus and original scores
// 5. Normalize scores to 0-1 range
func (f *MultiRRFFusion) FuseMultiQuery(subResults []SubQueryResult) []*MultiFusedResult {
	// Return empty slice, not nil, for consistent API behavior
	if len(subResults) == 0 {
		return []*MultiFusedResult{}
	}

	// Build aggregated score map
	scores := make(map[string]*MultiFusedResult)

	// Process each sub-query's results
	for _, sr := range subResults {
		weight := sr.SubQuery.Weight
		if weight <= 0 {
			weight = 1.0
		}

		for rank, result := range sr.Results {
			// Get or create multi-fused result
			mr := f.getOrCreate(scores, result.ChunkID)

			// Add RRF contribution: weight / (k + rank + 1)
			// rank is 0-indexed, so we add 1 for 1-indexed RRF
			mr.RRFScore += weight / float64(f.K+rank+1)

			// Track sub-query hits
			mr.SubQueryHits++

			// Merge metadata from result (take highest scores)
			if result.BM25Score > mr.BM25Score {
				mr.BM25Score = result.BM25Score
				mr.MatchedTerms = result.MatchedTerms
			}
			if result.VecScore > mr.VecScore {
				mr.VecScore = result.VecScore
			}
			if result.InBothLists {
				mr.InBothLists = true
			}
			if mr.BM25Rank == 0 || result.BM25Rank < mr.BM25Rank {
				mr.BM25Rank = result.BM25Rank
			}
			if mr.VecRank == 0 || result.VecRank < mr.VecRank {
				mr.VecRank = result.VecRank
			}
		}
	}

	// Apply consensus boost: documents in multiple sub-queries get boosted
	for _, mr := range scores {
		if mr.SubQueryHits > 1 {
			// Boost = 1 + (consensusBoost * (hits - 1))
			// e.g., 2 hits = 1.1x, 3 hits = 1.2x
			mr.RRFScore *= (1 + f.ConsensusBoost*float64(mr.SubQueryHits-1))
		}
	}

	// Convert to sorted slice
	results := f.toSortedSlice(scores)

	// Normalize scores to 0-1 range
	f.normalize(results)

	return results
}

// getOrCreate returns existing result or creates new one.
func (f *MultiRRFFusion) getOrCreate(m map[string]*MultiFusedResult, id string) *MultiFusedResult {
	if r, ok := m[id]; ok {
		return r
	}
	r := &MultiFusedResult{
		FusedResult: FusedResult{ChunkID: id},
	}
	m[id] = r
	return r
}

// toSortedSlice converts map to slice and sorts by multi-RRF score with tie-breaking.
func (f *MultiRRFFusion) toSortedSlice(m map[string]*MultiFusedResult) []*MultiFusedResult {
	results := make([]*MultiFusedResult, 0, len(m))
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
//  2. More sub-query hits (consensus)
//  3. In both BM25/vector lists
//  4. Higher BM25 score (exact match indicator)
//  5. Lexicographically smaller ChunkID (deterministic)
func (f *MultiRRFFusion) compare(a, b *MultiFusedResult) bool {
	// Primary: Higher RRF score ranks first
	if a.RRFScore != b.RRFScore {
		return a.RRFScore > b.RRFScore
	}

	// Tie-break 1: More sub-query hits (consensus)
	if a.SubQueryHits != b.SubQueryHits {
		return a.SubQueryHits > b.SubQueryHits
	}

	// Tie-break 2: Prefer documents in both BM25/vector lists
	if a.InBothLists != b.InBothLists {
		return a.InBothLists
	}

	// Tie-break 3: Prefer higher BM25 score (exact match indicator)
	if a.BM25Score != b.BM25Score {
		return a.BM25Score > b.BM25Score
	}

	// Tie-break 4: Lexicographic by ChunkID (deterministic)
	return a.ChunkID < b.ChunkID
}

// normalize scales all RRF scores to 0-1 range.
// Uses the maximum score as the reference (becomes 1.0).
func (f *MultiRRFFusion) normalize(results []*MultiFusedResult) {
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
