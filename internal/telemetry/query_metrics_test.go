package telemetry

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// CircularBuffer Tests
// =============================================================================

func TestCircularBuffer_Add_SingleItem(t *testing.T) {
	buf := NewCircularBuffer[string](10)

	buf.Add("query1")

	items := buf.Items()
	assert.Equal(t, 1, len(items))
	assert.Equal(t, "query1", items[0])
}

func TestCircularBuffer_Add_MultipleItems(t *testing.T) {
	buf := NewCircularBuffer[string](10)

	buf.Add("query1")
	buf.Add("query2")
	buf.Add("query3")

	items := buf.Items()
	assert.Equal(t, 3, len(items))
	assert.Equal(t, []string{"query1", "query2", "query3"}, items)
}

func TestCircularBuffer_MaintainsCapacity(t *testing.T) {
	buf := NewCircularBuffer[string](3)

	// Add more items than capacity
	buf.Add("query1")
	buf.Add("query2")
	buf.Add("query3")
	buf.Add("query4") // Should evict query1
	buf.Add("query5") // Should evict query2

	items := buf.Items()
	assert.Equal(t, 3, len(items))
	// Should contain last 3 items (FIFO eviction)
	assert.Equal(t, []string{"query3", "query4", "query5"}, items)
}

func TestCircularBuffer_Size(t *testing.T) {
	buf := NewCircularBuffer[string](5)

	assert.Equal(t, 0, buf.Size())

	buf.Add("a")
	assert.Equal(t, 1, buf.Size())

	buf.Add("b")
	buf.Add("c")
	assert.Equal(t, 3, buf.Size())

	// Exceed capacity
	buf.Add("d")
	buf.Add("e")
	buf.Add("f") // Evicts "a"
	assert.Equal(t, 5, buf.Size()) // Size capped at capacity
}

func TestCircularBuffer_EmptyItems(t *testing.T) {
	buf := NewCircularBuffer[string](10)

	items := buf.Items()
	assert.Equal(t, 0, len(items))
	assert.NotNil(t, items) // Should return empty slice, not nil
}

func TestCircularBuffer_Clear(t *testing.T) {
	buf := NewCircularBuffer[string](10)

	buf.Add("query1")
	buf.Add("query2")
	buf.Clear()

	assert.Equal(t, 0, buf.Size())
	assert.Equal(t, 0, len(buf.Items()))
}

// =============================================================================
// LatencyBucket Tests
// =============================================================================

func TestLatencyToBucket(t *testing.T) {
	tests := []struct {
		latency  time.Duration
		expected LatencyBucket
	}{
		{5 * time.Millisecond, BucketP10},
		{9 * time.Millisecond, BucketP10},
		{10 * time.Millisecond, BucketP50},
		{25 * time.Millisecond, BucketP50},
		{49 * time.Millisecond, BucketP50},
		{50 * time.Millisecond, BucketP100},
		{75 * time.Millisecond, BucketP100},
		{99 * time.Millisecond, BucketP100},
		{100 * time.Millisecond, BucketP500},
		{250 * time.Millisecond, BucketP500},
		{499 * time.Millisecond, BucketP500},
		{500 * time.Millisecond, BucketP1000},
		{1 * time.Second, BucketP1000},
		{5 * time.Second, BucketP1000},
	}

	for _, tt := range tests {
		t.Run(tt.latency.String(), func(t *testing.T) {
			got := LatencyToBucket(tt.latency)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// =============================================================================
// QueryMetrics Tests
// =============================================================================

func TestQueryMetrics_Record_IncrementsCounts(t *testing.T) {
	m := NewQueryMetrics(nil) // nil store = in-memory only
	defer m.Close()

	m.Record(QueryEvent{
		Query:       "find error handler",
		QueryType:   QueryTypeSemantic,
		ResultCount: 5,
		Latency:     25 * time.Millisecond,
		Timestamp:   time.Now(),
	})

	m.Record(QueryEvent{
		Query:       "ErrorHandler",
		QueryType:   QueryTypeLexical,
		ResultCount: 3,
		Latency:     15 * time.Millisecond,
		Timestamp:   time.Now(),
	})

	m.Record(QueryEvent{
		Query:       "error handling pattern",
		QueryType:   QueryTypeSemantic,
		ResultCount: 8,
		Latency:     50 * time.Millisecond,
		Timestamp:   time.Now(),
	})

	snapshot := m.Snapshot()
	assert.Equal(t, int64(2), snapshot.QueryTypeCounts[QueryTypeSemantic])
	assert.Equal(t, int64(1), snapshot.QueryTypeCounts[QueryTypeLexical])
	assert.Equal(t, int64(3), snapshot.TotalQueries)
}

func TestQueryMetrics_Record_TracksTopTerms(t *testing.T) {
	m := NewQueryMetrics(nil)
	defer m.Close()

	// Record queries with repeating terms
	m.Record(QueryEvent{Query: "error handling", QueryType: QueryTypeMixed, ResultCount: 5, Latency: 10 * time.Millisecond})
	m.Record(QueryEvent{Query: "error retry", QueryType: QueryTypeMixed, ResultCount: 3, Latency: 10 * time.Millisecond})
	m.Record(QueryEvent{Query: "error backoff", QueryType: QueryTypeMixed, ResultCount: 2, Latency: 10 * time.Millisecond})
	m.Record(QueryEvent{Query: "retry backoff", QueryType: QueryTypeMixed, ResultCount: 1, Latency: 10 * time.Millisecond})

	snapshot := m.Snapshot()

	// "error" appears 3 times, should be top term
	var errorCount int64
	for _, tc := range snapshot.TopTerms {
		if tc.Term == "error" {
			errorCount = tc.Count
			break
		}
	}
	assert.Equal(t, int64(3), errorCount)
}

func TestQueryMetrics_Record_CapturesZeroResults(t *testing.T) {
	m := NewQueryMetrics(nil)
	defer m.Close()

	m.Record(QueryEvent{Query: "nonexistent function", QueryType: QueryTypeSemantic, ResultCount: 0, Latency: 30 * time.Millisecond})
	m.Record(QueryEvent{Query: "found something", QueryType: QueryTypeSemantic, ResultCount: 5, Latency: 20 * time.Millisecond})
	m.Record(QueryEvent{Query: "another miss", QueryType: QueryTypeLexical, ResultCount: 0, Latency: 15 * time.Millisecond})

	snapshot := m.Snapshot()
	assert.Equal(t, 2, len(snapshot.ZeroResultQueries))
	assert.Contains(t, snapshot.ZeroResultQueries, "nonexistent function")
	assert.Contains(t, snapshot.ZeroResultQueries, "another miss")
}

func TestQueryMetrics_Record_BucketsLatency(t *testing.T) {
	m := NewQueryMetrics(nil)
	defer m.Close()

	// Record with various latencies
	m.Record(QueryEvent{Query: "fast", QueryType: QueryTypeLexical, ResultCount: 1, Latency: 5 * time.Millisecond})
	m.Record(QueryEvent{Query: "medium1", QueryType: QueryTypeLexical, ResultCount: 1, Latency: 25 * time.Millisecond})
	m.Record(QueryEvent{Query: "medium2", QueryType: QueryTypeLexical, ResultCount: 1, Latency: 35 * time.Millisecond})
	m.Record(QueryEvent{Query: "slow", QueryType: QueryTypeLexical, ResultCount: 1, Latency: 200 * time.Millisecond})
	m.Record(QueryEvent{Query: "very slow", QueryType: QueryTypeLexical, ResultCount: 1, Latency: 1 * time.Second})

	snapshot := m.Snapshot()
	assert.Equal(t, int64(1), snapshot.LatencyDistribution[BucketP10])
	assert.Equal(t, int64(2), snapshot.LatencyDistribution[BucketP50])
	assert.Equal(t, int64(1), snapshot.LatencyDistribution[BucketP500])
	assert.Equal(t, int64(1), snapshot.LatencyDistribution[BucketP1000])
}

func TestQueryMetrics_Concurrent_ThreadSafe(t *testing.T) {
	m := NewQueryMetrics(nil)
	defer m.Close()

	var wg sync.WaitGroup
	numGoroutines := 100
	eventsPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				m.Record(QueryEvent{
					Query:       "test query",
					QueryType:   QueryTypeSemantic,
					ResultCount: 5,
					Latency:     20 * time.Millisecond,
					Timestamp:   time.Now(),
				})
			}
		}(i)
	}

	wg.Wait()

	snapshot := m.Snapshot()
	expected := int64(numGoroutines * eventsPerGoroutine)
	assert.Equal(t, expected, snapshot.TotalQueries)
}

func TestQueryMetrics_Snapshot_ReturnsAccurateCounts(t *testing.T) {
	m := NewQueryMetrics(nil)
	defer m.Close()

	// Record specific pattern
	for i := 0; i < 10; i++ {
		m.Record(QueryEvent{Query: "semantic query", QueryType: QueryTypeSemantic, ResultCount: i, Latency: 10 * time.Millisecond})
	}
	for i := 0; i < 5; i++ {
		m.Record(QueryEvent{Query: "lexical query", QueryType: QueryTypeLexical, ResultCount: i, Latency: 10 * time.Millisecond})
	}
	for i := 0; i < 3; i++ {
		m.Record(QueryEvent{Query: "mixed query", QueryType: QueryTypeMixed, ResultCount: i, Latency: 10 * time.Millisecond})
	}

	snapshot := m.Snapshot()

	assert.Equal(t, int64(10), snapshot.QueryTypeCounts[QueryTypeSemantic])
	assert.Equal(t, int64(5), snapshot.QueryTypeCounts[QueryTypeLexical])
	assert.Equal(t, int64(3), snapshot.QueryTypeCounts[QueryTypeMixed])
	assert.Equal(t, int64(18), snapshot.TotalQueries)
}

func TestQueryMetrics_ZeroResultBuffer_MaintainsCapacity(t *testing.T) {
	m := NewQueryMetricsWithConfig(nil, QueryMetricsConfig{
		TopTermsCapacity:    100,
		ZeroResultsCapacity: 5, // Small capacity for testing
		FlushInterval:       0, // Disable auto-flush
	})
	defer m.Close()

	// Record more zero-result queries than capacity
	for i := 0; i < 10; i++ {
		m.Record(QueryEvent{
			Query:       "miss" + string(rune('A'+i)),
			QueryType:   QueryTypeSemantic,
			ResultCount: 0,
			Latency:     10 * time.Millisecond,
		})
	}

	snapshot := m.Snapshot()
	assert.Equal(t, 5, len(snapshot.ZeroResultQueries))
	// Should contain last 5 (FIFO)
	assert.Contains(t, snapshot.ZeroResultQueries, "missF")
	assert.Contains(t, snapshot.ZeroResultQueries, "missJ")
	assert.NotContains(t, snapshot.ZeroResultQueries, "missA")
}

func TestQueryMetrics_TopTerms_LRUEviction(t *testing.T) {
	m := NewQueryMetricsWithConfig(nil, QueryMetricsConfig{
		TopTermsCapacity:    5, // Small capacity for testing
		ZeroResultsCapacity: 100,
		FlushInterval:       0,
	})
	defer m.Close()

	// Record queries with many unique terms
	m.Record(QueryEvent{Query: "alpha beta", QueryType: QueryTypeMixed, ResultCount: 1, Latency: 10 * time.Millisecond})
	m.Record(QueryEvent{Query: "gamma delta", QueryType: QueryTypeMixed, ResultCount: 1, Latency: 10 * time.Millisecond})
	m.Record(QueryEvent{Query: "epsilon zeta", QueryType: QueryTypeMixed, ResultCount: 1, Latency: 10 * time.Millisecond})
	// Now add more - some old terms should be evicted
	m.Record(QueryEvent{Query: "eta theta", QueryType: QueryTypeMixed, ResultCount: 1, Latency: 10 * time.Millisecond})
	m.Record(QueryEvent{Query: "iota kappa", QueryType: QueryTypeMixed, ResultCount: 1, Latency: 10 * time.Millisecond})

	snapshot := m.Snapshot()
	// Should have at most 5 terms
	assert.LessOrEqual(t, len(snapshot.TopTerms), 5)
}

// =============================================================================
// Term Extraction Tests
// =============================================================================

func TestExtractTerms(t *testing.T) {
	tests := []struct {
		query    string
		expected []string
	}{
		{"error handling", []string{"error", "handling"}},
		{"findUser", []string{"finduser"}}, // Lowercased
		{"  spaces  around  ", []string{"spaces", "around"}},
		{"", nil},
		{"a", nil}, // Too short
		{"ab", nil}, // Too short
		{"abc", []string{"abc"}}, // Minimum length 3
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := ExtractTerms(tt.query)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// =============================================================================
// QueryEvent Tests
// =============================================================================

func TestQueryEvent_IsZeroResult(t *testing.T) {
	zeroResult := QueryEvent{Query: "missing", ResultCount: 0}
	hasResults := QueryEvent{Query: "found", ResultCount: 5}

	assert.True(t, zeroResult.IsZeroResult())
	assert.False(t, hasResults.IsZeroResult())
}

// =============================================================================
// QueryMetricsSnapshot Tests
// =============================================================================

func TestQueryMetricsSnapshot_ZeroResultPercentage(t *testing.T) {
	m := NewQueryMetrics(nil)
	defer m.Close()

	// 2 zero-results out of 10 total = 20%
	for i := 0; i < 8; i++ {
		m.Record(QueryEvent{Query: "found", QueryType: QueryTypeMixed, ResultCount: 5, Latency: 10 * time.Millisecond})
	}
	for i := 0; i < 2; i++ {
		m.Record(QueryEvent{Query: "missed", QueryType: QueryTypeMixed, ResultCount: 0, Latency: 10 * time.Millisecond})
	}

	snapshot := m.Snapshot()
	assert.InDelta(t, 20.0, snapshot.ZeroResultPercentage(), 0.01)
}

// =============================================================================
// Integration Tests
// =============================================================================

func TestQueryMetrics_FullLifecycle(t *testing.T) {
	m := NewQueryMetrics(nil)

	// Record various events
	m.Record(QueryEvent{Query: "search function", QueryType: QueryTypeSemantic, ResultCount: 10, Latency: 25 * time.Millisecond})
	m.Record(QueryEvent{Query: "ErrorHandler", QueryType: QueryTypeLexical, ResultCount: 3, Latency: 5 * time.Millisecond})
	m.Record(QueryEvent{Query: "missing pattern", QueryType: QueryTypeMixed, ResultCount: 0, Latency: 100 * time.Millisecond})

	// Get snapshot
	snapshot := m.Snapshot()
	require.NotNil(t, snapshot)
	assert.Equal(t, int64(3), snapshot.TotalQueries)
	assert.Equal(t, 1, len(snapshot.ZeroResultQueries))

	// Close should work without error
	err := m.Close()
	require.NoError(t, err)

	// After close, Record should be no-op (not panic)
	m.Record(QueryEvent{Query: "after close", QueryType: QueryTypeMixed, ResultCount: 1, Latency: 10 * time.Millisecond})
}

// =============================================================================
// Repetition Tracking Tests (SPIKE-004)
// =============================================================================

func TestQueryMetrics_ExactRepetition_DetectsRepeats(t *testing.T) {
	m := NewQueryMetrics(nil)
	defer m.Close()

	// Record same query multiple times
	m.Record(QueryEvent{Query: "search function", QueryType: QueryTypeSemantic, ResultCount: 5, Latency: 10 * time.Millisecond})
	m.Record(QueryEvent{Query: "another query", QueryType: QueryTypeSemantic, ResultCount: 3, Latency: 10 * time.Millisecond})
	m.Record(QueryEvent{Query: "search function", QueryType: QueryTypeSemantic, ResultCount: 5, Latency: 10 * time.Millisecond}) // Repeat
	m.Record(QueryEvent{Query: "search function", QueryType: QueryTypeSemantic, ResultCount: 5, Latency: 10 * time.Millisecond}) // Repeat again

	snapshot := m.Snapshot()
	assert.Equal(t, int64(4), snapshot.TotalQueries)
	assert.Equal(t, int64(2), snapshot.ExactRepeatCount) // 2 repeats of "search function"
	assert.InDelta(t, 0.5, snapshot.ExactRepeatRate, 0.01) // 2/4 = 50%
}

func TestQueryMetrics_ExactRepetition_CaseInsensitive(t *testing.T) {
	m := NewQueryMetrics(nil)
	defer m.Close()

	m.Record(QueryEvent{Query: "Search Function", QueryType: QueryTypeSemantic, ResultCount: 5, Latency: 10 * time.Millisecond})
	m.Record(QueryEvent{Query: "search function", QueryType: QueryTypeSemantic, ResultCount: 5, Latency: 10 * time.Millisecond}) // Same, different case
	m.Record(QueryEvent{Query: "SEARCH FUNCTION", QueryType: QueryTypeSemantic, ResultCount: 5, Latency: 10 * time.Millisecond}) // Same, different case

	snapshot := m.Snapshot()
	assert.Equal(t, int64(3), snapshot.TotalQueries)
	assert.Equal(t, int64(2), snapshot.ExactRepeatCount) // 2 repeats (case-insensitive)
}

func TestQueryMetrics_ExactRepetition_TrimWhitespace(t *testing.T) {
	m := NewQueryMetrics(nil)
	defer m.Close()

	m.Record(QueryEvent{Query: "search function", QueryType: QueryTypeSemantic, ResultCount: 5, Latency: 10 * time.Millisecond})
	m.Record(QueryEvent{Query: "  search function  ", QueryType: QueryTypeSemantic, ResultCount: 5, Latency: 10 * time.Millisecond}) // Same with whitespace

	snapshot := m.Snapshot()
	assert.Equal(t, int64(2), snapshot.TotalQueries)
	assert.Equal(t, int64(1), snapshot.ExactRepeatCount)
}

func TestQueryMetrics_ExactRepetition_UniqueQueryCount(t *testing.T) {
	m := NewQueryMetrics(nil)
	defer m.Close()

	m.Record(QueryEvent{Query: "query a", QueryType: QueryTypeSemantic, ResultCount: 5, Latency: 10 * time.Millisecond})
	m.Record(QueryEvent{Query: "query b", QueryType: QueryTypeSemantic, ResultCount: 5, Latency: 10 * time.Millisecond})
	m.Record(QueryEvent{Query: "query c", QueryType: QueryTypeSemantic, ResultCount: 5, Latency: 10 * time.Millisecond})
	m.Record(QueryEvent{Query: "query a", QueryType: QueryTypeSemantic, ResultCount: 5, Latency: 10 * time.Millisecond}) // Repeat
	m.Record(QueryEvent{Query: "query b", QueryType: QueryTypeSemantic, ResultCount: 5, Latency: 10 * time.Millisecond}) // Repeat

	snapshot := m.Snapshot()
	assert.Equal(t, int64(5), snapshot.TotalQueries)
	assert.Equal(t, int64(3), snapshot.UniqueQueryCount) // 3 unique queries
}

func TestQueryMetrics_SemanticSimilarity_DetectsSimilar(t *testing.T) {
	m := NewQueryMetricsWithConfig(nil, QueryMetricsConfig{
		TopTermsCapacity:         100,
		ZeroResultsCapacity:      100,
		RecentQueriesCapacity:    500,
		RecentEmbeddingsCapacity: 10,
		SimilarityThreshold:      0.95,
	})
	defer m.Close()

	// Create similar embeddings (cosine > 0.95)
	embed1 := []float32{1.0, 0.0, 0.0, 0.0}
	embed2 := []float32{0.99, 0.1, 0.0, 0.0} // Very similar to embed1
	embed3 := []float32{0.0, 1.0, 0.0, 0.0}  // Different direction

	m.RecordQueryEmbedding(embed1)
	m.RecordQueryEmbedding(embed2) // Should detect similarity to embed1
	m.RecordQueryEmbedding(embed3) // Should NOT be similar

	snapshot := m.Snapshot()
	assert.Equal(t, int64(1), snapshot.SimilarQueryCount) // Only embed2 was similar
}

func TestQueryMetrics_SemanticSimilarity_EmptyEmbeddingIgnored(t *testing.T) {
	m := NewQueryMetrics(nil)
	defer m.Close()

	m.RecordQueryEmbedding(nil)
	m.RecordQueryEmbedding([]float32{})

	snapshot := m.Snapshot()
	assert.Equal(t, int64(0), snapshot.SimilarQueryCount)
}

func TestQueryMetrics_SemanticSimilarity_CircularBuffer(t *testing.T) {
	m := NewQueryMetricsWithConfig(nil, QueryMetricsConfig{
		TopTermsCapacity:         100,
		ZeroResultsCapacity:      100,
		RecentQueriesCapacity:    500,
		RecentEmbeddingsCapacity: 3, // Small buffer for testing
		SimilarityThreshold:      0.95,
	})
	defer m.Close()

	// Fill buffer beyond capacity
	m.RecordQueryEmbedding([]float32{1.0, 0.0})
	m.RecordQueryEmbedding([]float32{0.0, 1.0})
	m.RecordQueryEmbedding([]float32{0.0, 0.0, 1.0})
	m.RecordQueryEmbedding([]float32{0.0, 0.0, 0.0, 1.0}) // Should evict first

	// Now add similar to first (which was evicted)
	m.RecordQueryEmbedding([]float32{0.99, 0.01}) // Similar to evicted [1.0, 0.0]

	snapshot := m.Snapshot()
	// Should NOT detect similarity since first embedding was evicted
	assert.Equal(t, int64(0), snapshot.SimilarQueryCount)
}

func TestCosineSimilarity_Identical(t *testing.T) {
	a := []float32{1.0, 0.0, 0.0}
	b := []float32{1.0, 0.0, 0.0}

	similarity := cosineSimilarity(a, b)
	assert.InDelta(t, 1.0, similarity, 0.0001)
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float32{1.0, 0.0, 0.0}
	b := []float32{0.0, 1.0, 0.0}

	similarity := cosineSimilarity(a, b)
	assert.InDelta(t, 0.0, similarity, 0.0001)
}

func TestCosineSimilarity_Similar(t *testing.T) {
	a := []float32{1.0, 0.0, 0.0}
	b := []float32{0.99, 0.1, 0.0}

	similarity := cosineSimilarity(a, b)
	assert.Greater(t, similarity, 0.95) // Should be very similar
}

func TestCosineSimilarity_DifferentLengths(t *testing.T) {
	a := []float32{1.0, 0.0}
	b := []float32{1.0, 0.0, 0.0}

	similarity := cosineSimilarity(a, b)
	assert.Equal(t, 0.0, similarity) // Different lengths should return 0
}

func TestCosineSimilarity_Empty(t *testing.T) {
	similarity := cosineSimilarity(nil, nil)
	assert.Equal(t, 0.0, similarity)

	similarity = cosineSimilarity([]float32{}, []float32{})
	assert.Equal(t, 0.0, similarity)
}

func TestRepetitionSummary_NoQueries(t *testing.T) {
	snapshot := &QueryMetricsSnapshot{
		TotalQueries: 0,
	}
	assert.Equal(t, "No queries recorded", snapshot.RepetitionSummary())
}

func TestRepetitionSummary_WithData(t *testing.T) {
	snapshot := &QueryMetricsSnapshot{
		TotalQueries:     100,
		ExactRepeatRate:  0.15,  // 15%
		SimilarQueryRate: 0.08,  // 8%
		UniqueQueryCount: 85,
	}
	summary := snapshot.RepetitionSummary()
	assert.Contains(t, summary, "exact=")
	assert.Contains(t, summary, "similar=")
	assert.Contains(t, summary, "unique=")
}
