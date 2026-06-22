package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPreferResult_DeterministicTieBreakOnRelation(t *testing.T) {
	low := QueryResult{
		NodeID: "node:symbol:project:pkg.go#A:1", Confidence: 0.9,
		Relation: EdgeKindDocMentionsFile, SourcePath: "docs/a.md", EvidenceMethod: "doc_link",
		Path: GraphPath{Explanation: "docs/a.md -> pkg.go"},
	}
	high := low
	high.Relation = EdgeKindDocMentionsPath
	high.EvidenceMethod = "doc_path"

	assert.True(t, preferResult(low, high))
	assert.False(t, preferResult(high, low))
}

func TestDeduplicateResultsByTarget_StableRegardlessOfInputOrder(t *testing.T) {
	makeResult := func(relation EdgeKind, method string) QueryResult {
		return QueryResult{
			NodeID: "node:file:project:pkg.go", Confidence: 0.9,
			Relation: relation, SourcePath: "docs/a.md", EvidenceMethod: method,
			Path: GraphPath{Explanation: "docs/a.md -> pkg.go via " + string(relation)},
		}
	}
	forward := deduplicateResultsByTarget([]QueryResult{
		makeResult(EdgeKindDocMentionsPath, "doc_path"),
		makeResult(EdgeKindDocMentionsFile, "doc_link"),
	})
	reverse := deduplicateResultsByTarget([]QueryResult{
		makeResult(EdgeKindDocMentionsFile, "doc_link"),
		makeResult(EdgeKindDocMentionsPath, "doc_path"),
	})
	requireLen := func(t *testing.T, results []QueryResult) {
		t.Helper()
		if assert.Len(t, results, 1) {
			assert.Equal(t, EdgeKindDocMentionsFile, results[0].Relation)
			assert.Equal(t, 1, results[0].AdditionalPathsCount)
			assert.ElementsMatch(t, []EdgeKind{EdgeKindDocMentionsPath}, results[0].AdditionalRelations)
		}
	}
	requireLen(t, forward)
	requireLen(t, reverse)
	assert.Equal(t, forward[0].Relation, reverse[0].Relation)
	assert.Equal(t, forward[0].AdditionalRelations, reverse[0].AdditionalRelations)
}

func TestRecordSuppressedRelation_SkipsRepresentativeKind(t *testing.T) {
	entry := &targetDedupBucket{otherRelations: map[EdgeKind]struct{}{}}
	recordSuppressedRelation(entry, EdgeKindDocMentionsFile, EdgeKindDocMentionsFile)
	assert.Empty(t, entry.otherRelations)
	recordSuppressedRelation(entry, EdgeKindDocMentionsPath, EdgeKindDocMentionsFile)
	assert.Contains(t, entry.otherRelations, EdgeKindDocMentionsPath)
}