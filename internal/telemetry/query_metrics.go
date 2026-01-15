// Package telemetry provides query pattern telemetry for search optimization.
// All telemetry data is stored locally - no external reporting.
package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// =============================================================================
// Query Types
// =============================================================================

// QueryType represents the classification of a search query.
type QueryType string

const (
	QueryTypeLexical  QueryType = "lexical"
	QueryTypeSemantic QueryType = "semantic"
	QueryTypeMixed    QueryType = "mixed"
)

// =============================================================================
// Latency Buckets
// =============================================================================

// LatencyBucket represents a latency histogram bucket.
type LatencyBucket string

const (
	BucketP10   LatencyBucket = "p10"   // <10ms
	BucketP50   LatencyBucket = "p50"   // 10-50ms
	BucketP100  LatencyBucket = "p100"  // 50-100ms
	BucketP500  LatencyBucket = "p500"  // 100-500ms
	BucketP1000 LatencyBucket = "p1000" // >=500ms
)

// LatencyToBucket converts a duration to its histogram bucket.
func LatencyToBucket(d time.Duration) LatencyBucket {
	ms := d.Milliseconds()
	switch {
	case ms < 10:
		return BucketP10
	case ms < 50:
		return BucketP50
	case ms < 100:
		return BucketP100
	case ms < 500:
		return BucketP500
	default:
		return BucketP1000
	}
}

// =============================================================================
// Query Event
// =============================================================================

// QueryEvent represents a single search query for telemetry recording.
type QueryEvent struct {
	Query       string
	QueryType   QueryType
	ResultCount int
	Latency     time.Duration
	Timestamp   time.Time
}

// IsZeroResult returns true if this query returned no results.
func (e QueryEvent) IsZeroResult() bool {
	return e.ResultCount == 0
}

// =============================================================================
// Circular Buffer
// =============================================================================

// CircularBuffer is a fixed-capacity FIFO buffer.
type CircularBuffer[T any] struct {
	items    []T
	head     int  // Next write position
	size     int  // Current number of items
	capacity int
	mu       sync.RWMutex
}

// NewCircularBuffer creates a new circular buffer with the given capacity.
func NewCircularBuffer[T any](capacity int) *CircularBuffer[T] {
	if capacity <= 0 {
		capacity = 100
	}
	return &CircularBuffer[T]{
		items:    make([]T, capacity),
		capacity: capacity,
	}
}

// Add adds an item to the buffer. If full, the oldest item is evicted.
func (b *CircularBuffer[T]) Add(item T) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.items[b.head] = item
	b.head = (b.head + 1) % b.capacity

	if b.size < b.capacity {
		b.size++
	}
}

// Items returns all items in the buffer in FIFO order (oldest first).
func (b *CircularBuffer[T]) Items() []T {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.size == 0 {
		return []T{}
	}

	result := make([]T, b.size)
	if b.size < b.capacity {
		// Buffer not full - items start at 0
		copy(result, b.items[:b.size])
	} else {
		// Buffer full - oldest item is at head
		copy(result, b.items[b.head:])
		copy(result[b.capacity-b.head:], b.items[:b.head])
	}
	return result
}

// Size returns the current number of items in the buffer.
func (b *CircularBuffer[T]) Size() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size
}

// Clear removes all items from the buffer.
func (b *CircularBuffer[T]) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.head = 0
	b.size = 0
}

// =============================================================================
// Term Extraction
// =============================================================================

// ExtractTerms extracts searchable terms from a query string.
// Terms are lowercased and filtered to minimum length 3.
func ExtractTerms(query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}

	words := strings.Fields(query)
	var terms []string
	for _, w := range words {
		// Filter short terms
		if len(w) >= 3 {
			terms = append(terms, w)
		}
	}

	if len(terms) == 0 {
		return nil
	}
	return terms
}

// =============================================================================
// Term Count
// =============================================================================

// TermCount represents a term and its frequency count.
type TermCount struct {
	Term  string `json:"term"`
	Count int64  `json:"count"`
}

// =============================================================================
// Query Metrics Snapshot
// =============================================================================

// QueryMetricsSnapshot is an immutable snapshot of query metrics.
type QueryMetricsSnapshot struct {
	QueryTypeCounts     map[QueryType]int64     `json:"query_type_counts"`
	TopTerms            []TermCount             `json:"top_terms"`
	ZeroResultQueries   []string                `json:"zero_result_queries"`
	LatencyDistribution map[LatencyBucket]int64 `json:"latency_distribution"`
	TotalQueries        int64                   `json:"total_queries"`
	ZeroResultCount     int64                   `json:"zero_result_count"`
	Since               time.Time               `json:"since"`

	// Repetition metrics (SPIKE-004)
	ExactRepeatCount    int64   `json:"exact_repeat_count"`
	ExactRepeatRate     float64 `json:"exact_repeat_rate"`
	SimilarQueryCount   int64   `json:"similar_query_count"`
	SimilarQueryRate    float64 `json:"similar_query_rate"`
	UniqueQueryCount    int64   `json:"unique_query_count"`
}

// ZeroResultPercentage returns the percentage of zero-result queries.
func (s *QueryMetricsSnapshot) ZeroResultPercentage() float64 {
	if s.TotalQueries == 0 {
		return 0
	}
	return float64(s.ZeroResultCount) / float64(s.TotalQueries) * 100
}

// RepetitionSummary returns a human-readable summary of repetition metrics.
func (s *QueryMetricsSnapshot) RepetitionSummary() string {
	if s.TotalQueries == 0 {
		return "No queries recorded"
	}
	return "exact=" + formatPercent(s.ExactRepeatRate) +
		", similar=" + formatPercent(s.SimilarQueryRate) +
		", unique=" + formatInt64(s.UniqueQueryCount)
}

func formatPercent(rate float64) string {
	// Simple percentage formatting without fmt package
	percent := int(rate * 1000) // e.g., 0.156 -> 156
	whole := percent / 10       // 15
	frac := percent % 10        // 6
	if frac == 0 {
		return intToStr(whole) + "%"
	}
	return intToStr(whole) + "." + intToStr(frac) + "%"
}

func formatInt64(n int64) string {
	return intToStr(int(n))
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + intToStr(-n)
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// =============================================================================
// Query Metrics Store (Interface)
// =============================================================================

// QueryMetricsStore defines persistence operations for query metrics.
type QueryMetricsStore interface {
	// SaveQueryTypeCounts upserts daily query type counts.
	SaveQueryTypeCounts(date string, counts map[QueryType]int64) error

	// GetQueryTypeCounts retrieves counts for a date range.
	GetQueryTypeCounts(from, to string) (map[QueryType]int64, error)

	// UpsertTermCounts updates term frequency counts.
	UpsertTermCounts(terms map[string]int64) error

	// GetTopTerms retrieves the top N terms by frequency.
	GetTopTerms(limit int) ([]TermCount, error)

	// AddZeroResultQuery adds a query to the circular buffer.
	AddZeroResultQuery(query string, timestamp time.Time) error

	// GetZeroResultQueries retrieves recent zero-result queries.
	GetZeroResultQueries(limit int) ([]string, error)

	// SaveLatencyCounts upserts daily latency histogram counts.
	SaveLatencyCounts(date string, counts map[LatencyBucket]int64) error

	// GetLatencyCounts retrieves latency distribution for a date range.
	GetLatencyCounts(from, to string) (map[LatencyBucket]int64, error)

	// Close releases resources.
	Close() error
}

// =============================================================================
// Query Metrics Configuration
// =============================================================================

// QueryMetricsConfig configures the query metrics collector.
type QueryMetricsConfig struct {
	TopTermsCapacity    int           // Max terms to track (default: 100)
	ZeroResultsCapacity int           // Max zero-result queries to track (default: 100)
	FlushInterval       time.Duration // How often to flush to store (default: 60s, 0 = no auto-flush)

	// Repetition tracking (SPIKE-004)
	RecentQueriesCapacity   int     // Max queries to track for repetition (default: 500)
	RecentEmbeddingsCapacity int    // Max embeddings to sample for similarity (default: 10)
	SimilarityThreshold     float64 // Cosine similarity threshold (default: 0.95)
}

// DefaultQueryMetricsConfig returns sensible defaults.
func DefaultQueryMetricsConfig() QueryMetricsConfig {
	return QueryMetricsConfig{
		TopTermsCapacity:         100,
		ZeroResultsCapacity:      100,
		FlushInterval:            60 * time.Second,
		RecentQueriesCapacity:    500,
		RecentEmbeddingsCapacity: 10,
		SimilarityThreshold:      0.95,
	}
}

// =============================================================================
// Query Metrics
// =============================================================================

// QueryMetrics collects query telemetry for search optimization.
// Thread-safe for concurrent access.
type QueryMetrics struct {
	mu sync.RWMutex

	// In-memory aggregates
	queryTypes      map[QueryType]int64
	topTerms        *lru.Cache[string, int64]
	zeroResults     *CircularBuffer[string]
	latencies       map[LatencyBucket]int64
	totalQueries    int64
	zeroResultCount int64
	startTime       time.Time

	// Repetition tracking (SPIKE-004)
	recentQueries     *lru.Cache[string, struct{}] // LRU of query hashes
	exactRepeatCount  int64                        // Count of exact query repeats
	recentEmbeddings  *CircularBuffer[[]float32]   // Circular buffer of recent embeddings
	similarQueryCount int64                        // Count of semantically similar queries

	// Persistence
	store       QueryMetricsStore
	config      QueryMetricsConfig
	flushTicker *time.Ticker
	stopCh      chan struct{}
	closed      bool
}

// NewQueryMetrics creates a new metrics collector with default configuration.
// If store is nil, metrics are only kept in memory.
func NewQueryMetrics(store QueryMetricsStore) *QueryMetrics {
	return NewQueryMetricsWithConfig(store, DefaultQueryMetricsConfig())
}

// NewQueryMetricsWithConfig creates a new metrics collector with custom configuration.
func NewQueryMetricsWithConfig(store QueryMetricsStore, cfg QueryMetricsConfig) *QueryMetrics {
	if cfg.TopTermsCapacity <= 0 {
		cfg.TopTermsCapacity = 100
	}
	if cfg.ZeroResultsCapacity <= 0 {
		cfg.ZeroResultsCapacity = 100
	}
	if cfg.RecentQueriesCapacity <= 0 {
		cfg.RecentQueriesCapacity = 500
	}
	if cfg.RecentEmbeddingsCapacity <= 0 {
		cfg.RecentEmbeddingsCapacity = 10
	}
	if cfg.SimilarityThreshold <= 0 {
		cfg.SimilarityThreshold = 0.95
	}

	topTerms, _ := lru.New[string, int64](cfg.TopTermsCapacity)
	recentQueries, _ := lru.New[string, struct{}](cfg.RecentQueriesCapacity)

	m := &QueryMetrics{
		queryTypes:       make(map[QueryType]int64),
		topTerms:         topTerms,
		zeroResults:      NewCircularBuffer[string](cfg.ZeroResultsCapacity),
		latencies:        make(map[LatencyBucket]int64),
		startTime:        time.Now(),
		recentQueries:    recentQueries,
		recentEmbeddings: NewCircularBuffer[[]float32](cfg.RecentEmbeddingsCapacity),
		store:            store,
		config:           cfg,
		stopCh:           make(chan struct{}),
	}

	// Start auto-flush if configured
	if cfg.FlushInterval > 0 && store != nil {
		m.flushTicker = time.NewTicker(cfg.FlushInterval)
		go m.flushLoop()
	}

	return m
}

// flushLoop periodically flushes metrics to storage.
func (m *QueryMetrics) flushLoop() {
	for {
		select {
		case <-m.flushTicker.C:
			_ = m.Flush()
		case <-m.stopCh:
			return
		}
	}
}

// Record captures metrics from a search query.
// This method is thread-safe and non-blocking.
func (m *QueryMetrics) Record(event QueryEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return
	}

	// Increment query type count
	m.queryTypes[event.QueryType]++
	m.totalQueries++

	// Track terms
	terms := ExtractTerms(event.Query)
	for _, term := range terms {
		count, _ := m.topTerms.Get(term)
		m.topTerms.Add(term, count+1)
	}

	// Track zero-result queries
	if event.IsZeroResult() {
		m.zeroResults.Add(event.Query)
		m.zeroResultCount++
	}

	// Track latency
	bucket := LatencyToBucket(event.Latency)
	m.latencies[bucket]++

	// Track exact query repetition (SPIKE-004)
	queryHash := hashQuery(event.Query)
	if _, exists := m.recentQueries.Get(queryHash); exists {
		m.exactRepeatCount++
	}
	m.recentQueries.Add(queryHash, struct{}{})
}

// hashQuery creates a normalized hash of the query for repetition detection.
func hashQuery(query string) string {
	normalized := strings.ToLower(strings.TrimSpace(query))
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:16]) // Use first 16 bytes for shorter key
}

// RecordQueryEmbedding records a query embedding for semantic similarity sampling.
// Call this after Record() for queries where embeddings are available.
// This is optional - if not called, only exact repetition is tracked.
func (m *QueryMetrics) RecordQueryEmbedding(embedding []float32) {
	if len(embedding) == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return
	}

	// Check similarity against recent embeddings
	recentEmbeddings := m.recentEmbeddings.Items()
	for _, prev := range recentEmbeddings {
		if cosineSimilarity(embedding, prev) > m.config.SimilarityThreshold {
			m.similarQueryCount++
			break // Count once per query
		}
	}

	// Store embedding for future comparisons (copy to avoid aliasing)
	embeddingCopy := make([]float32, len(embedding))
	copy(embeddingCopy, embedding)
	m.recentEmbeddings.Add(embeddingCopy)
}

// cosineSimilarity computes cosine similarity between two vectors.
// Returns 0 if either vector is empty or has different dimensions.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (sqrt(normA) * sqrt(normB))
}

// sqrt computes square root using Newton's method.
// Avoids importing math package for this single use.
func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x / 2
	for i := 0; i < 10; i++ { // 10 iterations is plenty for convergence
		z = z - (z*z-x)/(2*z)
	}
	return z
}

// Snapshot returns current metrics for reporting.
func (m *QueryMetrics) Snapshot() *QueryMetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Copy query type counts
	typeCounts := make(map[QueryType]int64)
	for k, v := range m.queryTypes {
		typeCounts[k] = v
	}

	// Get top terms sorted by count
	var topTerms []TermCount
	keys := m.topTerms.Keys()
	for _, key := range keys {
		if count, ok := m.topTerms.Peek(key); ok {
			topTerms = append(topTerms, TermCount{Term: key, Count: count})
		}
	}
	// Sort by count descending (simple bubble sort for small list)
	for i := 0; i < len(topTerms); i++ {
		for j := i + 1; j < len(topTerms); j++ {
			if topTerms[j].Count > topTerms[i].Count {
				topTerms[i], topTerms[j] = topTerms[j], topTerms[i]
			}
		}
	}

	// Copy zero-result queries
	zeroResults := m.zeroResults.Items()

	// Copy latency distribution
	latencies := make(map[LatencyBucket]int64)
	for k, v := range m.latencies {
		latencies[k] = v
	}

	// Calculate repetition rates (SPIKE-004)
	var exactRepeatRate, similarQueryRate float64
	uniqueQueryCount := int64(m.recentQueries.Len())
	if m.totalQueries > 0 {
		exactRepeatRate = float64(m.exactRepeatCount) / float64(m.totalQueries)
		similarQueryRate = float64(m.similarQueryCount) / float64(m.totalQueries)
	}

	return &QueryMetricsSnapshot{
		QueryTypeCounts:     typeCounts,
		TopTerms:            topTerms,
		ZeroResultQueries:   zeroResults,
		LatencyDistribution: latencies,
		TotalQueries:        m.totalQueries,
		ZeroResultCount:     m.zeroResultCount,
		Since:               m.startTime,
		// Repetition metrics (SPIKE-004)
		ExactRepeatCount:  m.exactRepeatCount,
		ExactRepeatRate:   exactRepeatRate,
		SimilarQueryCount: m.similarQueryCount,
		SimilarQueryRate:  similarQueryRate,
		UniqueQueryCount:  uniqueQueryCount,
	}
}

// Flush persists in-memory metrics to the store.
// Safe to call even if no store is configured.
func (m *QueryMetrics) Flush() error {
	if m.store == nil {
		return nil
	}

	m.mu.RLock()
	snapshot := m.Snapshot()
	m.mu.RUnlock()

	today := time.Now().Format("2006-01-02")

	// Flush query type counts
	if err := m.store.SaveQueryTypeCounts(today, snapshot.QueryTypeCounts); err != nil {
		return err
	}

	// Flush top terms
	termCounts := make(map[string]int64)
	for _, tc := range snapshot.TopTerms {
		termCounts[tc.Term] = tc.Count
	}
	if err := m.store.UpsertTermCounts(termCounts); err != nil {
		return err
	}

	// Flush latency counts
	if err := m.store.SaveLatencyCounts(today, snapshot.LatencyDistribution); err != nil {
		return err
	}

	return nil
}

// Close flushes and releases resources.
func (m *QueryMetrics) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.mu.Unlock()

	// Stop auto-flush
	if m.flushTicker != nil {
		m.flushTicker.Stop()
		close(m.stopCh)
	}

	// Final flush
	if err := m.Flush(); err != nil {
		return err
	}

	return nil
}
