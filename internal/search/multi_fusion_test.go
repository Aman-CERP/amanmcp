package search

import (
	"testing"
)

// TestMultiRRFFusion tests the multi-query RRF fusion algorithm.
func TestMultiRRFFusion(t *testing.T) {
	f := NewMultiRRFFusion()

	t.Run("empty results returns empty", func(t *testing.T) {
		results := f.FuseMultiQuery(nil)
		if results == nil {
			t.Error("Expected empty slice, got nil")
		}
		if len(results) != 0 {
			t.Errorf("Expected 0 results, got %d", len(results))
		}
	})

	t.Run("single sub-query preserves order", func(t *testing.T) {
		subResults := []SubQueryResult{
			{
				SubQuery: SubQuery{Query: "func Search", Weight: 1.0},
				Results: []*FusedResult{
					{ChunkID: "chunk1", RRFScore: 0.9},
					{ChunkID: "chunk2", RRFScore: 0.8},
					{ChunkID: "chunk3", RRFScore: 0.7},
				},
			},
		}

		results := f.FuseMultiQuery(subResults)

		if len(results) != 3 {
			t.Fatalf("Expected 3 results, got %d", len(results))
		}

		// Order should be preserved
		if results[0].ChunkID != "chunk1" {
			t.Errorf("Expected chunk1 first, got %s", results[0].ChunkID)
		}
	})

	t.Run("documents in multiple sub-queries get boosted", func(t *testing.T) {
		// chunk1 appears in both sub-query results
		// chunk2 appears only in first
		// chunk3 appears only in second
		subResults := []SubQueryResult{
			{
				SubQuery: SubQuery{Query: "func Search", Weight: 1.0},
				Results: []*FusedResult{
					{ChunkID: "chunk1", RRFScore: 0.5},
					{ChunkID: "chunk2", RRFScore: 0.6},
				},
			},
			{
				SubQuery: SubQuery{Query: "Search method", Weight: 1.0},
				Results: []*FusedResult{
					{ChunkID: "chunk1", RRFScore: 0.5},
					{ChunkID: "chunk3", RRFScore: 0.7},
				},
			},
		}

		results := f.FuseMultiQuery(subResults)

		// chunk1 should be first because it appears in both lists
		if len(results) < 1 || results[0].ChunkID != "chunk1" {
			t.Errorf("Expected chunk1 first (appears in both), got %v", results)
		}

		// Verify chunk1 has higher score due to appearing in both
		chunk1Score := results[0].RRFScore
		var chunk2Score, chunk3Score float64
		for _, r := range results {
			if r.ChunkID == "chunk2" {
				chunk2Score = r.RRFScore
			}
			if r.ChunkID == "chunk3" {
				chunk3Score = r.RRFScore
			}
		}

		// chunk1 should have highest score due to consensus boost
		if chunk1Score <= chunk2Score || chunk1Score <= chunk3Score {
			t.Errorf("chunk1 (in both) should have highest score: chunk1=%f, chunk2=%f, chunk3=%f",
				chunk1Score, chunk2Score, chunk3Score)
		}
	})

	t.Run("weights affect scoring", func(t *testing.T) {
		// Two sub-queries with different weights
		subResults := []SubQueryResult{
			{
				SubQuery: SubQuery{Query: "high weight", Weight: 2.0},
				Results: []*FusedResult{
					{ChunkID: "chunk_high", RRFScore: 0.5},
				},
			},
			{
				SubQuery: SubQuery{Query: "low weight", Weight: 0.5},
				Results: []*FusedResult{
					{ChunkID: "chunk_low", RRFScore: 0.5},
				},
			},
		}

		results := f.FuseMultiQuery(subResults)

		var highScore, lowScore float64
		for _, r := range results {
			if r.ChunkID == "chunk_high" {
				highScore = r.RRFScore
			}
			if r.ChunkID == "chunk_low" {
				lowScore = r.RRFScore
			}
		}

		if highScore <= lowScore {
			t.Errorf("Higher weight should produce higher score: high=%f, low=%f",
				highScore, lowScore)
		}
	})

	t.Run("three sub-queries fuse correctly", func(t *testing.T) {
		// Simulate "Search function" decomposition
		subResults := []SubQueryResult{
			{
				SubQuery: SubQuery{Query: "func Search", Weight: 1.2},
				Results: []*FusedResult{
					{ChunkID: "engine.go:Search", RRFScore: 0.9},
					{ChunkID: "test_search.go", RRFScore: 0.8},
				},
			},
			{
				SubQuery: SubQuery{Query: "Search method", Weight: 1.0},
				Results: []*FusedResult{
					{ChunkID: "engine.go:Search", RRFScore: 0.85},
					{ChunkID: "docs/search.md", RRFScore: 0.7},
				},
			},
			{
				SubQuery: SubQuery{Query: "engine.go Search", Weight: 1.1},
				Results: []*FusedResult{
					{ChunkID: "engine.go:Search", RRFScore: 0.95},
					{ChunkID: "engine.go:Other", RRFScore: 0.6},
				},
			},
		}

		results := f.FuseMultiQuery(subResults)

		// engine.go:Search should be first (appears in all 3)
		if len(results) < 1 || results[0].ChunkID != "engine.go:Search" {
			t.Errorf("Expected engine.go:Search first (in all 3 sub-queries), got %v", results)
		}

		// Verify SubQueryHits is tracked
		if results[0].SubQueryHits != 3 {
			t.Errorf("Expected SubQueryHits=3 for engine.go:Search, got %d", results[0].SubQueryHits)
		}
	})

	t.Run("scores are normalized", func(t *testing.T) {
		subResults := []SubQueryResult{
			{
				SubQuery: SubQuery{Query: "test", Weight: 1.0},
				Results: []*FusedResult{
					{ChunkID: "chunk1", RRFScore: 0.9},
					{ChunkID: "chunk2", RRFScore: 0.5},
				},
			},
		}

		results := f.FuseMultiQuery(subResults)

		// First result should have score 1.0 (normalized max)
		if results[0].RRFScore != 1.0 {
			t.Errorf("Expected first result score=1.0 (normalized), got %f", results[0].RRFScore)
		}

		// All scores should be between 0 and 1
		for _, r := range results {
			if r.RRFScore < 0 || r.RRFScore > 1 {
				t.Errorf("Score out of range [0,1]: %f", r.RRFScore)
			}
		}
	})
}

// TestMultiRRFFusionConsensusBoost specifically tests the consensus boosting behavior.
func TestMultiRRFFusionConsensusBoost(t *testing.T) {
	f := NewMultiRRFFusion()

	// Create scenario where consensus matters
	// doc1: appears in 3 sub-queries at rank 3
	// doc2: appears in 1 sub-query at rank 1
	subResults := []SubQueryResult{
		{
			SubQuery: SubQuery{Query: "q1", Weight: 1.0},
			Results: []*FusedResult{
				{ChunkID: "doc2"},
				{ChunkID: "other1"},
				{ChunkID: "doc1"},
			},
		},
		{
			SubQuery: SubQuery{Query: "q2", Weight: 1.0},
			Results: []*FusedResult{
				{ChunkID: "other2"},
				{ChunkID: "other3"},
				{ChunkID: "doc1"},
			},
		},
		{
			SubQuery: SubQuery{Query: "q3", Weight: 1.0},
			Results: []*FusedResult{
				{ChunkID: "other4"},
				{ChunkID: "other5"},
				{ChunkID: "doc1"},
			},
		},
	}

	results := f.FuseMultiQuery(subResults)

	// Find doc1 and doc2
	var doc1Rank, doc2Rank int
	for i, r := range results {
		if r.ChunkID == "doc1" {
			doc1Rank = i + 1
		}
		if r.ChunkID == "doc2" {
			doc2Rank = i + 1
		}
	}

	// doc1 (in all 3) should rank higher than doc2 (in 1 only)
	// due to consensus boost, even though doc2 was rank 1 in one query
	if doc1Rank >= doc2Rank {
		t.Errorf("doc1 (consensus=3) should rank higher than doc2 (consensus=1): doc1 rank=%d, doc2 rank=%d",
			doc1Rank, doc2Rank)
	}
}

// SubQueryResult represents results from a single sub-query.
// Defined here for test compilation, will be moved to multi_fusion.go.
