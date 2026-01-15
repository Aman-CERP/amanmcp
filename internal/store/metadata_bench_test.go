package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// =============================================================================
// F23 Performance Benchmarks - Metadata Store
// =============================================================================
// Targets:
// - GetChunk: < 1ms per call
// - GetChunks (batch): < 10ms for 100 chunks
// - SaveChunks: > 1000 chunks/sec
// - Symbol queries: < 5ms
// =============================================================================

// BenchmarkSQLiteStore_GetChunk benchmarks single chunk retrieval.
func BenchmarkSQLiteStore_GetChunk(b *testing.B) {
	store, cleanup := setupBenchmarkMetadataStore(b, 1000)
	defer cleanup()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		chunkID := fmt.Sprintf("chunk-%d", i%1000)
		_, err := store.GetChunk(ctx, chunkID)
		if err != nil {
			b.Fatalf("GetChunk failed: %v", err)
		}
	}
}

// BenchmarkSQLiteStore_GetChunk_Sequential benchmarks N sequential GetChunk calls.
// This establishes the baseline for comparison with batch retrieval.
func BenchmarkSQLiteStore_GetChunk_Sequential(b *testing.B) {
	counts := []int{10, 20, 50, 100}

	for _, count := range counts {
		b.Run(fmt.Sprintf("count_%d", count), func(b *testing.B) {
			store, cleanup := setupBenchmarkMetadataStore(b, 1000)
			defer cleanup()

			ctx := context.Background()
			ids := make([]string, count)
			for i := 0; i < count; i++ {
				ids[i] = fmt.Sprintf("chunk-%d", i)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				// Simulate N individual GetChunk calls (current behavior)
				for _, id := range ids {
					_, err := store.GetChunk(ctx, id)
					if err != nil {
						b.Fatalf("GetChunk failed: %v", err)
					}
				}
			}

			// Report operations per second
			b.ReportMetric(float64(count*b.N)/b.Elapsed().Seconds(), "chunks/sec")
		})
	}
}

// BenchmarkSQLiteStore_GetChunksByFile benchmarks file-based chunk retrieval.
func BenchmarkSQLiteStore_GetChunksByFile(b *testing.B) {
	store, cleanup := setupBenchmarkMetadataStore(b, 1000)
	defer cleanup()

	ctx := context.Background()
	fileID := "file-0" // This file has chunks

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := store.GetChunksByFile(ctx, fileID)
		if err != nil {
			b.Fatalf("GetChunksByFile failed: %v", err)
		}
	}
}

// BenchmarkSQLiteStore_SaveChunks benchmarks batch chunk insertion.
func BenchmarkSQLiteStore_SaveChunks(b *testing.B) {
	batchSizes := []int{10, 50, 100, 500, 1000}

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("batch_%d", batchSize), func(b *testing.B) {
			store, cleanup := setupBenchmarkMetadataStore(b, 0) // Start empty
			defer cleanup()

			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				chunks := generateBenchmarkChunksForStore(batchSize, i)
				err := store.SaveChunks(ctx, chunks)
				if err != nil {
					b.Fatalf("SaveChunks failed: %v", err)
				}
			}

			// Report chunks per second
			b.ReportMetric(float64(batchSize*b.N)/b.Elapsed().Seconds(), "chunks/sec")
		})
	}
}

// BenchmarkSQLiteStore_GetChunks_Batch benchmarks batch chunk retrieval.
// This is the optimized path - compare with GetChunk_Sequential.
func BenchmarkSQLiteStore_GetChunks_Batch(b *testing.B) {
	counts := []int{10, 20, 50, 100}

	for _, count := range counts {
		b.Run(fmt.Sprintf("count_%d", count), func(b *testing.B) {
			store, cleanup := setupBenchmarkMetadataStore(b, 1000)
			defer cleanup()

			ctx := context.Background()
			ids := make([]string, count)
			for i := 0; i < count; i++ {
				ids[i] = fmt.Sprintf("chunk-%d", i)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, err := store.GetChunks(ctx, ids)
				if err != nil {
					b.Fatalf("GetChunks failed: %v", err)
				}
			}

			// Report chunks per second
			b.ReportMetric(float64(count*b.N)/b.Elapsed().Seconds(), "chunks/sec")
		})
	}
}

// BenchmarkSQLiteStore_SearchSymbols benchmarks symbol search.
func BenchmarkSQLiteStore_SearchSymbols(b *testing.B) {
	store, cleanup := setupBenchmarkMetadataStore(b, 1000)
	defer cleanup()

	ctx := context.Background()
	queries := []string{"Handler", "Process", "Service", "Manager", "Controller"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		query := queries[i%len(queries)]
		_, err := store.SearchSymbols(ctx, query, 20)
		if err != nil {
			b.Fatalf("SearchSymbols failed: %v", err)
		}
	}
}

// BenchmarkSQLiteStore_ListFiles benchmarks paginated file listing.
func BenchmarkSQLiteStore_ListFiles(b *testing.B) {
	store, cleanup := setupBenchmarkMetadataStore(b, 1000)
	defer cleanup()

	ctx := context.Background()
	projectID := "bench-project"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _, err := store.ListFiles(ctx, projectID, "", 100)
		if err != nil {
			b.Fatalf("ListFiles failed: %v", err)
		}
	}
}

// BenchmarkSQLiteStore_Concurrent benchmarks concurrent read/write access.
func BenchmarkSQLiteStore_Concurrent(b *testing.B) {
	store, cleanup := setupBenchmarkMetadataStore(b, 1000)
	defer cleanup()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			chunkID := fmt.Sprintf("chunk-%d", i%1000)
			_, err := store.GetChunk(ctx, chunkID)
			if err != nil {
				b.Fatalf("GetChunk failed: %v", err)
			}
			i++
		}
	})
}

// =============================================================================
// Benchmark Helpers
// =============================================================================

// setupBenchmarkMetadataStore creates a SQLite store with pre-populated data.
func setupBenchmarkMetadataStore(b *testing.B, numChunks int) (*SQLiteStore, func()) {
	b.Helper()

	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "bench-metadata-*")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "metadata.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		b.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()

	// Create project
	project := &Project{
		ID:          "bench-project",
		Name:        "Benchmark Project",
		RootPath:    "/tmp/benchmark",
		ProjectType: "go",
		ChunkCount:  numChunks,
		FileCount:   numChunks / 10,
		IndexedAt:   time.Now(),
		Version:     "1.0.0",
	}
	if err := store.SaveProject(ctx, project); err != nil {
		_ = store.Close()
		_ = os.RemoveAll(tmpDir)
		b.Fatalf("failed to save project: %v", err)
	}

	// Create files
	numFiles := numChunks / 10
	if numFiles < 1 {
		numFiles = 1
	}
	files := make([]*File, numFiles)
	for i := 0; i < numFiles; i++ {
		files[i] = &File{
			ID:          fmt.Sprintf("file-%d", i),
			ProjectID:   "bench-project",
			Path:        fmt.Sprintf("internal/service/service%d.go", i),
			Size:        1000 + int64(i*100),
			ModTime:     time.Now(),
			ContentHash: fmt.Sprintf("hash-%d", i),
			Language:    "go",
			ContentType: "code",
			IndexedAt:   time.Now(),
		}
	}
	if err := store.SaveFiles(ctx, files); err != nil {
		_ = store.Close()
		_ = os.RemoveAll(tmpDir)
		b.Fatalf("failed to save files: %v", err)
	}

	// Create chunks in batches
	if numChunks > 0 {
		chunks := generateBenchmarkChunksForStore(numChunks, 0)
		// Assign files to chunks
		for i, c := range chunks {
			c.FileID = fmt.Sprintf("file-%d", i%numFiles)
		}
		if err := store.SaveChunks(ctx, chunks); err != nil {
			_ = store.Close()
			_ = os.RemoveAll(tmpDir)
			b.Fatalf("failed to save chunks: %v", err)
		}
	}

	return store, func() {
		_ = store.Close()
		_ = os.RemoveAll(tmpDir)
	}
}

// generateBenchmarkChunksForStore creates chunks for metadata store benchmarks.
func generateBenchmarkChunksForStore(n int, iteration int) []*Chunk {
	chunks := make([]*Chunk, n)
	now := time.Now()

	symbolTypes := []SymbolType{SymbolTypeFunction, SymbolTypeMethod, SymbolTypeClass}
	symbolNames := []string{"Handler", "Process", "Service", "Manager", "Controller"}

	for i := 0; i < n; i++ {
		symbolType := symbolTypes[i%len(symbolTypes)]
		symbolName := fmt.Sprintf("%s%d", symbolNames[i%len(symbolNames)], i)

		chunks[i] = &Chunk{
			ID:          fmt.Sprintf("chunk-%d", iteration*n+i),
			FileID:      fmt.Sprintf("file-%d", i%100),
			FilePath:    fmt.Sprintf("internal/service/service%d.go", i%100),
			Content:     generateStoreContent(800 + i%400),
			RawContent:  generateStoreContent(400 + i%200),
			Context:     "package service\n\nimport \"context\"",
			ContentType: ContentTypeCode,
			Language:    "go",
			StartLine:   (i % 50) * 20,
			EndLine:     (i%50)*20 + 20,
			Symbols: []*Symbol{
				{
					Name:      symbolName,
					Type:      symbolType,
					StartLine: (i % 50) * 20,
					EndLine:   (i%50)*20 + 20,
					Signature: fmt.Sprintf("func %s(ctx context.Context) error", symbolName),
				},
			},
			CreatedAt: now,
			UpdatedAt: now,
		}
	}
	return chunks
}

// generateStoreContent creates content of specified size.
func generateStoreContent(size int) string {
	template := `func processRequest(ctx context.Context, req *Request) (*Response, error) {
	handler, err := getHandler(req.Type)
	if err != nil {
		return nil, err
	}
	return handler.Execute(ctx, req), nil
}
`
	content := ""
	for len(content) < size {
		content += template
	}
	if len(content) > size {
		content = content[:size]
	}
	return content
}
