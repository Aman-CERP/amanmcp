package search

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// DEBT-028: NoOpReranker Tests
// =============================================================================

func TestNoOpReranker_Rerank_PreservesOrder(t *testing.T) {
	// Given: NoOpReranker and documents
	reranker := &NoOpReranker{}
	documents := []string{"doc1", "doc2", "doc3"}

	// When: reranking
	results, err := reranker.Rerank(context.Background(), "query", documents, 0)

	// Then: order is preserved with decreasing scores
	require.NoError(t, err)
	require.Len(t, results, 3)

	assert.Equal(t, 0, results[0].Index)
	assert.Equal(t, "doc1", results[0].Document)
	assert.InDelta(t, 1.0, results[0].Score, 0.001)

	assert.Equal(t, 1, results[1].Index)
	assert.Equal(t, "doc2", results[1].Document)
	assert.InDelta(t, 0.99, results[1].Score, 0.001)

	assert.Equal(t, 2, results[2].Index)
	assert.Equal(t, "doc3", results[2].Document)
	assert.InDelta(t, 0.98, results[2].Score, 0.001)
}

func TestNoOpReranker_Rerank_RespectsTopK(t *testing.T) {
	// Given: NoOpReranker and many documents
	reranker := &NoOpReranker{}
	documents := []string{"doc1", "doc2", "doc3", "doc4", "doc5"}

	// When: reranking with topK=3
	results, err := reranker.Rerank(context.Background(), "query", documents, 3)

	// Then: only top 3 returned
	require.NoError(t, err)
	assert.Len(t, results, 3)
	assert.Equal(t, "doc1", results[0].Document)
	assert.Equal(t, "doc2", results[1].Document)
	assert.Equal(t, "doc3", results[2].Document)
}

func TestNoOpReranker_Rerank_TopKZeroReturnsAll(t *testing.T) {
	// Given: NoOpReranker
	reranker := &NoOpReranker{}
	documents := []string{"doc1", "doc2", "doc3"}

	// When: reranking with topK=0
	results, err := reranker.Rerank(context.Background(), "query", documents, 0)

	// Then: all documents returned
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestNoOpReranker_Rerank_TopKGreaterThanDocs(t *testing.T) {
	// Given: NoOpReranker with fewer docs than topK
	reranker := &NoOpReranker{}
	documents := []string{"doc1", "doc2"}

	// When: reranking with topK=10
	results, err := reranker.Rerank(context.Background(), "query", documents, 10)

	// Then: all documents returned (topK > len)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestNoOpReranker_Rerank_EmptyDocuments(t *testing.T) {
	// Given: NoOpReranker with no documents
	reranker := &NoOpReranker{}
	documents := []string{}

	// When: reranking empty list
	results, err := reranker.Rerank(context.Background(), "query", documents, 0)

	// Then: empty results, no error
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestNoOpReranker_Available(t *testing.T) {
	// Given: NoOpReranker
	reranker := &NoOpReranker{}

	// When: checking availability
	available := reranker.Available(context.Background())

	// Then: always available
	assert.True(t, available)
}

func TestNoOpReranker_Close(t *testing.T) {
	// Given: NoOpReranker
	reranker := &NoOpReranker{}

	// When: closing
	err := reranker.Close()

	// Then: no error
	assert.NoError(t, err)
}

func TestNoOpReranker_InterfaceCompliance(t *testing.T) {
	// Verify NoOpReranker implements Reranker interface
	var _ Reranker = (*NoOpReranker)(nil)
}

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkNoOpReranker_Rerank(b *testing.B) {
	reranker := &NoOpReranker{}
	documents := make([]string, 50)
	for i := range documents {
		documents[i] = "document content here"
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = reranker.Rerank(context.Background(), "query", documents, 10)
	}
}
