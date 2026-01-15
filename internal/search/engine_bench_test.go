package search

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

// =============================================================================
// F23 Performance Benchmarks - Search Engine at Scale
// =============================================================================
// Targets:
// - P50 < 20ms (10K), < 50ms (50K), < 100ms (100K)
// - P95 < 50ms (10K), < 100ms (50K), < 200ms (100K)
// - P99 < 100ms (10K), < 200ms (50K), < 300ms (100K)
// =============================================================================

// BenchmarkEngineSearch_Scale runs search benchmarks at various scales.
func BenchmarkEngineSearch_Scale(b *testing.B) {
	scales := []int{100, 1000, 10000, 50000}

	for _, scale := range scales {
		b.Run(fmt.Sprintf("scale_%d", scale), func(b *testing.B) {
			engine, cleanup := setupScaleBenchmarkEngine(b, scale)
			defer cleanup()

			ctx := context.Background()
			queries := generateBenchQueries(10)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				query := queries[i%len(queries)]
				_, err := engine.Search(ctx, query, SearchOptions{Limit: 20})
				if err != nil {
					b.Fatalf("search failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkEngineSearch_Parallel tests concurrent search performance.
func BenchmarkEngineSearch_Parallel(b *testing.B) {
	engine, cleanup := setupScaleBenchmarkEngine(b, 10000)
	defer cleanup()

	ctx := context.Background()
	queries := generateBenchQueries(100)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			query := queries[i%len(queries)]
			_, err := engine.Search(ctx, query, SearchOptions{Limit: 20})
			if err != nil {
				b.Fatalf("search failed: %v", err)
			}
			i++
		}
	})
}

// BenchmarkEngine_EnrichResults benchmarks result enrichment (critical path).
func BenchmarkEngine_EnrichResults(b *testing.B) {
	resultCounts := []int{10, 20, 50, 100}

	for _, count := range resultCounts {
		b.Run(fmt.Sprintf("results_%d", count), func(b *testing.B) {
			engine, cleanup := setupScaleBenchmarkEngineWithChunks(b, count*10)
			defer cleanup()

			// Create fused results to enrich
			fused := make([]*fusedResult, count)
			for i := 0; i < count; i++ {
				fused[i] = &fusedResult{
					chunkID:      fmt.Sprintf("chunk-%d", i),
					rrfScore:     0.5 + float64(i)*0.01,
					bm25Score:    0.3,
					vecScore:     0.7,
					inBothLists:  true,
					matchedTerms: []string{"function", "handler", "process"},
				}
			}

			ctx := context.Background()
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, err := engine.enrichResults(ctx, fused)
				if err != nil {
					b.Fatalf("enrich failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkEngine_CalculateHighlights benchmarks highlight calculation.
func BenchmarkEngine_CalculateHighlights(b *testing.B) {
	engine, cleanup := setupScaleBenchmarkEngine(b, 100)
	defer cleanup()

	contentSizes := []int{500, 1000, 2000, 5000}
	terms := []string{"function", "handler", "error", "context", "result"}

	for _, size := range contentSizes {
		b.Run(fmt.Sprintf("content_%d_chars", size), func(b *testing.B) {
			content := generateBenchContent(size)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_ = engine.calculateHighlights(content, terms)
			}
		})
	}
}

// BenchmarkEngineIndex_Throughput benchmarks indexing throughput.
func BenchmarkEngineIndex_Throughput(b *testing.B) {
	chunkCounts := []int{10, 50, 100, 500}

	for _, count := range chunkCounts {
		b.Run(fmt.Sprintf("chunks_%d", count), func(b *testing.B) {
			engine, cleanup := setupScaleBenchmarkEngine(b, 0) // Start empty
			defer cleanup()

			chunks := generateBenchChunks(count)
			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				err := engine.Index(ctx, chunks)
				if err != nil {
					b.Fatalf("index failed: %v", err)
				}
			}

			// Report custom metric: chunks/sec
			b.ReportMetric(float64(count*b.N)/b.Elapsed().Seconds(), "chunks/sec")
		})
	}
}

// BenchmarkEngineMemory_Scale measures memory usage at scale.
func BenchmarkEngineMemory_Scale(b *testing.B) {
	scales := []int{1000, 5000, 10000}

	for _, scale := range scales {
		b.Run(fmt.Sprintf("scale_%d", scale), func(b *testing.B) {
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				engine, cleanup := setupScaleBenchmarkEngine(b, scale)
				cleanup()
				_ = engine
			}
		})
	}
}

// =============================================================================
// Benchmark Helpers
// =============================================================================

// setupScaleBenchmarkEngine creates an engine with mock stores pre-populated with data.
func setupScaleBenchmarkEngine(b *testing.B, numChunks int) (*Engine, func()) {
	b.Helper()

	// Create mock stores with pre-populated data
	bm25Results := generateBenchBM25Results(numChunks)
	vecResults := generateBenchVectorResults(numChunks)

	bm25 := &MockBM25Index{
		SearchFn: func(_ context.Context, _ string, limit int) ([]*store.BM25Result, error) {
			if limit > len(bm25Results) {
				limit = len(bm25Results)
			}
			return bm25Results[:limit], nil
		},
		StatsFn: func() *store.IndexStats {
			return &store.IndexStats{DocumentCount: numChunks}
		},
	}

	vec := &MockVectorStore{
		SearchFn: func(_ context.Context, _ []float32, k int) ([]*store.VectorResult, error) {
			if k > len(vecResults) {
				k = len(vecResults)
			}
			return vecResults[:k], nil
		},
		CountFn: func() int { return numChunks },
	}

	embedder := &MockEmbedder{
		EmbedFn: func(_ context.Context, _ string) ([]float32, error) {
			return make([]float32, 768), nil
		},
		DimensionsFn: func() int { return 768 },
	}

	metadata := NewMockMetadataStore()
	// Pre-populate chunks
	for i := 0; i < numChunks; i++ {
		metadata.chunks[fmt.Sprintf("chunk-%d", i)] = &store.Chunk{
			ID:          fmt.Sprintf("chunk-%d", i),
			FilePath:    fmt.Sprintf("file-%d.go", i%100),
			Content:     fmt.Sprintf("func handler%d() { /* implementation */ }", i),
			ContentType: store.ContentTypeCode,
			Language:    "go",
			StartLine:   i * 10,
			EndLine:     i*10 + 10,
		}
	}

	engine := New(bm25, vec, embedder, metadata, DefaultConfig())

	return engine, func() {
		_ = engine.Close()
	}
}

// setupScaleBenchmarkEngineWithChunks creates an engine with actual chunks in metadata.
func setupScaleBenchmarkEngineWithChunks(b *testing.B, numChunks int) (*Engine, func()) {
	b.Helper()

	bm25 := &MockBM25Index{
		SearchFn: func(_ context.Context, _ string, _ int) ([]*store.BM25Result, error) {
			return nil, nil
		},
	}

	vec := &MockVectorStore{
		SearchFn: func(_ context.Context, _ []float32, _ int) ([]*store.VectorResult, error) {
			return nil, nil
		},
	}

	embedder := &MockEmbedder{
		EmbedFn: func(_ context.Context, _ string) ([]float32, error) {
			return make([]float32, 768), nil
		},
	}

	metadata := NewMockMetadataStore()
	// Pre-populate chunks with realistic content
	for i := 0; i < numChunks; i++ {
		metadata.chunks[fmt.Sprintf("chunk-%d", i)] = &store.Chunk{
			ID:          fmt.Sprintf("chunk-%d", i),
			FilePath:    fmt.Sprintf("internal/handler/handler%d.go", i),
			Content:     generateBenchContent(1000 + rand.Intn(1000)),
			ContentType: store.ContentTypeCode,
			Language:    "go",
			StartLine:   1,
			EndLine:     50,
		}
	}

	engine := New(bm25, vec, embedder, metadata, DefaultConfig())

	return engine, func() {
		_ = engine.Close()
	}
}

// generateBenchBM25Results creates mock BM25 search results.
func generateBenchBM25Results(n int) []*store.BM25Result {
	results := make([]*store.BM25Result, benchMin(n, 100))
	for i := range results {
		results[i] = &store.BM25Result{
			DocID:        fmt.Sprintf("chunk-%d", i),
			Score:        10.0 - float64(i)*0.1,
			MatchedTerms: []string{"function", "handler"},
		}
	}
	return results
}

// generateBenchVectorResults creates mock vector search results.
func generateBenchVectorResults(n int) []*store.VectorResult {
	results := make([]*store.VectorResult, benchMin(n, 100))
	for i := range results {
		results[i] = &store.VectorResult{
			ID:       fmt.Sprintf("chunk-%d", i),
			Distance: float32(i) * 0.01,
			Score:    1.0 - float32(i)*0.01,
		}
	}
	return results
}

// generateBenchQueries creates a set of realistic queries for benchmarking.
func generateBenchQueries(n int) []string {
	baseQueries := []string{
		"authentication middleware",
		"database connection pool",
		"error handling patterns",
		"API endpoint handler",
		"configuration management",
		"HTTP request processing",
		"context cancellation",
		"goroutine synchronization",
		"file parsing function",
		"cache invalidation strategy",
	}

	queries := make([]string, n)
	for i := 0; i < n; i++ {
		queries[i] = baseQueries[i%len(baseQueries)]
	}
	return queries
}

// generateBenchChunks creates chunks for indexing benchmarks.
func generateBenchChunks(n int) []*store.Chunk {
	chunks := make([]*store.Chunk, n)
	for i := 0; i < n; i++ {
		chunks[i] = &store.Chunk{
			ID:          fmt.Sprintf("bench-chunk-%d-%d", time.Now().UnixNano(), i),
			FilePath:    fmt.Sprintf("internal/service/service%d.go", i),
			Content:     generateBenchContent(800 + rand.Intn(400)),
			ContentType: store.ContentTypeCode,
			Language:    "go",
			StartLine:   1,
			EndLine:     30,
		}
	}
	return chunks
}

// generateBenchContent creates realistic code-like content of specified size.
func generateBenchContent(size int) string {
	template := `func processRequest(ctx context.Context, req *Request) (*Response, error) {
	if err := validateRequest(req); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	handler, err := getHandler(req.Type)
	if err != nil {
		return nil, fmt.Errorf("handler not found: %w", err)
	}

	result, err := handler.Execute(ctx, req.Payload)
	if err != nil {
		return nil, fmt.Errorf("execution failed: %w", err)
	}

	return &Response{
		Status: "success",
		Data:   result,
	}, nil
}
`
	// Repeat and truncate to desired size
	content := ""
	for len(content) < size {
		content += template
	}
	return content[:size]
}

func benchMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
