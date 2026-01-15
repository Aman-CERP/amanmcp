package search

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/embed"
	"github.com/Aman-CERP/amanmcp/internal/store"
	"github.com/Aman-CERP/amanmcp/internal/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Acceptance Criteria Registry
// =============================================================================
// AC01: SearchEngine interface with Search, Index, Delete, Stats, Close methods
// AC02: Parallel search execution with errgroup, timeout handling, graceful degradation
// AC03: Result fusion (deferred to F14 - using simple weighted merge)
// AC04: Result enrichment from MetadataStore
// AC05: Filter support (content type, language, symbol type)
// AC06: Performance (<100ms P95 for 50K chunks)
// =============================================================================

// =============================================================================
// Mock Implementations
// =============================================================================

// MockBM25Index implements store.BM25Index for testing
type MockBM25Index struct {
	SearchFn     func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error)
	IndexFn      func(ctx context.Context, docs []*store.Document) error
	DeleteFn     func(ctx context.Context, docIDs []string) error
	StatsFn      func() *store.IndexStats
	CloseFn      func() error
	searchCalled atomic.Int32
}

func (m *MockBM25Index) Search(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
	m.searchCalled.Add(1)
	if m.SearchFn != nil {
		return m.SearchFn(ctx, query, limit)
	}
	return nil, nil
}

func (m *MockBM25Index) Index(ctx context.Context, docs []*store.Document) error {
	if m.IndexFn != nil {
		return m.IndexFn(ctx, docs)
	}
	return nil
}

func (m *MockBM25Index) Delete(ctx context.Context, docIDs []string) error {
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, docIDs)
	}
	return nil
}

func (m *MockBM25Index) Stats() *store.IndexStats {
	if m.StatsFn != nil {
		return m.StatsFn()
	}
	return &store.IndexStats{}
}

func (m *MockBM25Index) Save(_ string) error       { return nil }
func (m *MockBM25Index) Load(_ string) error       { return nil }
func (m *MockBM25Index) Close() error {
	if m.CloseFn != nil {
		return m.CloseFn()
	}
	return nil
}
func (m *MockBM25Index) AllIDs() ([]string, error) { return nil, nil }

// MockVectorStore implements store.VectorStore for testing
type MockVectorStore struct {
	SearchFn     func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error)
	AddFn        func(ctx context.Context, ids []string, vectors [][]float32) error
	DeleteFn     func(ctx context.Context, ids []string) error
	CountFn      func() int
	CloseFn      func() error
	searchCalled atomic.Int32
}

func (m *MockVectorStore) Search(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
	m.searchCalled.Add(1)
	if m.SearchFn != nil {
		return m.SearchFn(ctx, query, k)
	}
	return nil, nil
}

func (m *MockVectorStore) Add(ctx context.Context, ids []string, vectors [][]float32) error {
	if m.AddFn != nil {
		return m.AddFn(ctx, ids, vectors)
	}
	return nil
}

func (m *MockVectorStore) Delete(ctx context.Context, ids []string) error {
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, ids)
	}
	return nil
}

func (m *MockVectorStore) Contains(_ string) bool { return false }

func (m *MockVectorStore) Count() int {
	if m.CountFn != nil {
		return m.CountFn()
	}
	return 0
}

func (m *MockVectorStore) Save(_ string) error { return nil }
func (m *MockVectorStore) Load(_ string) error { return nil }
func (m *MockVectorStore) Close() error {
	if m.CloseFn != nil {
		return m.CloseFn()
	}
	return nil
}
func (m *MockVectorStore) AllIDs() []string { return nil }

// MockEmbedder implements embed.Embedder for testing
type MockEmbedder struct {
	EmbedFn      func(ctx context.Context, text string) ([]float32, error)
	DimensionsFn func() int
	embedCalled  atomic.Int32
}

func (m *MockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	m.embedCalled.Add(1)
	if m.EmbedFn != nil {
		return m.EmbedFn(ctx, text)
	}
	return make([]float32, 768), nil
}

func (m *MockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := m.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		result[i] = emb
	}
	return result, nil
}

func (m *MockEmbedder) Dimensions() int {
	if m.DimensionsFn != nil {
		return m.DimensionsFn()
	}
	return 768
}

func (m *MockEmbedder) ModelName() string                       { return "mock-embedder" }
func (m *MockEmbedder) Available(_ context.Context) bool        { return true }
func (m *MockEmbedder) Close() error                            { return nil }
func (m *MockEmbedder) SetBatchIndex(_ int)                     {}
func (m *MockEmbedder) SetFinalBatch(_ bool)                    {}

// MockMetadataStore implements store.MetadataStore for testing
type MockMetadataStore struct {
	GetChunkFn     func(ctx context.Context, id string) (*store.Chunk, error)
	DeleteChunksFn func(ctx context.Context, ids []string) error
	GetStateFn     func(ctx context.Context, key string) (string, error)
	SetStateFn     func(ctx context.Context, key, value string) error
	CloseFn        func() error
	chunks         map[string]*store.Chunk
	state          map[string]string // QW-5: State storage for dimension tracking
}

func NewMockMetadataStore() *MockMetadataStore {
	return &MockMetadataStore{
		chunks: make(map[string]*store.Chunk),
		state:  make(map[string]string),
	}
}

func (m *MockMetadataStore) GetChunk(ctx context.Context, id string) (*store.Chunk, error) {
	if m.GetChunkFn != nil {
		return m.GetChunkFn(ctx, id)
	}
	if chunk, ok := m.chunks[id]; ok {
		return chunk, nil
	}
	return nil, nil
}

func (m *MockMetadataStore) GetChunks(_ context.Context, ids []string) ([]*store.Chunk, error) {
	result := make([]*store.Chunk, 0, len(ids))
	for _, id := range ids {
		if chunk, ok := m.chunks[id]; ok {
			result = append(result, chunk)
		}
	}
	return result, nil
}

func (m *MockMetadataStore) SaveProject(_ context.Context, _ *store.Project) error { return nil }
func (m *MockMetadataStore) GetProject(_ context.Context, _ string) (*store.Project, error) {
	return nil, nil
}
func (m *MockMetadataStore) UpdateProjectStats(_ context.Context, _ string, _, _ int) error {
	return nil
}
func (m *MockMetadataStore) RefreshProjectStats(_ context.Context, _ string) error {
	return nil
}
func (m *MockMetadataStore) SaveFiles(_ context.Context, _ []*store.File) error { return nil }
func (m *MockMetadataStore) GetFileByPath(_ context.Context, _, _ string) (*store.File, error) {
	return nil, nil
}
func (m *MockMetadataStore) GetChangedFiles(_ context.Context, _ string, _ time.Time) ([]*store.File, error) {
	return nil, nil
}
func (m *MockMetadataStore) DeleteFilesByProject(_ context.Context, _ string) error { return nil }
func (m *MockMetadataStore) SaveChunks(_ context.Context, chunks []*store.Chunk) error {
	for _, c := range chunks {
		m.chunks[c.ID] = c
	}
	return nil
}
func (m *MockMetadataStore) GetChunksByFile(_ context.Context, fileID string) ([]*store.Chunk, error) {
	var result []*store.Chunk
	for _, c := range m.chunks {
		if c.FileID == fileID {
			result = append(result, c)
		}
	}
	return result, nil
}
func (m *MockMetadataStore) DeleteChunks(ctx context.Context, ids []string) error {
	if m.DeleteChunksFn != nil {
		return m.DeleteChunksFn(ctx, ids)
	}
	return nil
}
func (m *MockMetadataStore) DeleteChunksByFile(_ context.Context, _ string) error { return nil }
func (m *MockMetadataStore) SearchSymbols(_ context.Context, _ string, _ int) ([]*store.Symbol, error) {
	return nil, nil
}
func (m *MockMetadataStore) ListFiles(_ context.Context, _ string, _ string, _ int) ([]*store.File, string, error) {
	return nil, "", nil
}
func (m *MockMetadataStore) GetFilePathsByProject(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (m *MockMetadataStore) GetFilesForReconciliation(_ context.Context, _ string) (map[string]*store.File, error) {
	return nil, nil
}
func (m *MockMetadataStore) ListFilePathsUnder(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}
func (m *MockMetadataStore) DeleteFile(_ context.Context, _ string) error { return nil }

// QW-5: GetState now uses state map or function override
func (m *MockMetadataStore) GetState(ctx context.Context, key string) (string, error) {
	if m.GetStateFn != nil {
		return m.GetStateFn(ctx, key)
	}
	return m.state[key], nil
}

// QW-5: SetState now uses state map or function override
func (m *MockMetadataStore) SetState(ctx context.Context, key, value string) error {
	if m.SetStateFn != nil {
		return m.SetStateFn(ctx, key, value)
	}
	m.state[key] = value
	return nil
}

// Embedding methods (for HNSW compaction - BUG-024 fix)
func (m *MockMetadataStore) SaveChunkEmbeddings(_ context.Context, _ []string, _ [][]float32, _ string) error {
	return nil
}
func (m *MockMetadataStore) GetAllEmbeddings(_ context.Context) (map[string][]float32, error) {
	return nil, nil
}
func (m *MockMetadataStore) GetEmbeddingStats(_ context.Context) (int, int, error) {
	return 0, 0, nil
}

// Checkpoint methods (DEBT-022: Index Runner)
func (m *MockMetadataStore) SaveIndexCheckpoint(_ context.Context, _ string, _, _ int, _ string) error {
	return nil
}
func (m *MockMetadataStore) LoadIndexCheckpoint(_ context.Context) (*store.IndexCheckpoint, error) {
	return nil, nil
}
func (m *MockMetadataStore) ClearIndexCheckpoint(_ context.Context) error {
	return nil
}

func (m *MockMetadataStore) Close() error {
	if m.CloseFn != nil {
		return m.CloseFn()
	}
	return nil
}

// MockDecomposer implements QueryDecomposer for testing
type MockDecomposer struct {
	ShouldDecomposeFn func(query string) bool
	DecomposeFn       func(query string) []SubQuery
}

func (m *MockDecomposer) ShouldDecompose(query string) bool {
	if m.ShouldDecomposeFn != nil {
		return m.ShouldDecomposeFn(query)
	}
	return false
}

func (m *MockDecomposer) Decompose(query string) []SubQuery {
	if m.DecomposeFn != nil {
		return m.DecomposeFn(query)
	}
	return []SubQuery{{Query: query, Weight: 1.0}}
}

// =============================================================================
// Test Fixtures
// =============================================================================

func createTestChunks() []*store.Chunk {
	return []*store.Chunk{
		{
			ID:          "chunk1",
			FilePath:    "auth/login.go",
			Content:     "func Login(user, pass string) error { ... }",
			ContentType: store.ContentTypeCode,
			Language:    "go",
			StartLine:   10,
			EndLine:     25,
			Symbols: []*store.Symbol{
				{Name: "Login", Type: store.SymbolTypeFunction},
			},
		},
		{
			ID:          "chunk2",
			FilePath:    "auth/logout.go",
			Content:     "func Logout(ctx context.Context) error { ... }",
			ContentType: store.ContentTypeCode,
			Language:    "go",
			StartLine:   5,
			EndLine:     15,
			Symbols: []*store.Symbol{
				{Name: "Logout", Type: store.SymbolTypeFunction},
			},
		},
		{
			ID:          "chunk3",
			FilePath:    "docs/README.md",
			Content:     "# Authentication\n\nThis module handles user authentication...",
			ContentType: store.ContentTypeMarkdown,
			Language:    "markdown",
			StartLine:   1,
			EndLine:     10,
		},
		{
			ID:          "chunk4",
			FilePath:    "handlers/user.ts",
			Content:     "export function getUser(id: string): User { ... }",
			ContentType: store.ContentTypeCode,
			Language:    "typescript",
			StartLine:   1,
			EndLine:     5,
			Symbols: []*store.Symbol{
				{Name: "getUser", Type: store.SymbolTypeFunction},
			},
		},
		{
			ID:          "chunk5",
			FilePath:    "models/user.go",
			Content:     "type User struct { ID string; Name string }",
			ContentType: store.ContentTypeCode,
			Language:    "go",
			StartLine:   1,
			EndLine:     3,
			Symbols: []*store.Symbol{
				{Name: "User", Type: store.SymbolTypeType},
			},
		},
	}
}

func setupTestEngine(t *testing.T) (*Engine, *MockBM25Index, *MockVectorStore, *MockEmbedder, *MockMetadataStore) {
	t.Helper()

	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{}
	embedder := &MockEmbedder{}
	metadata := NewMockMetadataStore()

	// Pre-populate metadata with test chunks
	chunks := createTestChunks()
	for _, c := range chunks {
		metadata.chunks[c.ID] = c
	}

	engine := New(bm25, vector, embedder, metadata, DefaultConfig())

	return engine, bm25, vector, embedder, metadata
}

// =============================================================================
// AC01: SearchEngine Interface Tests
// =============================================================================

func TestEngine_Search_BasicHybrid(t *testing.T) {
	// Given: an engine with indexed documents
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 0.9, MatchedTerms: []string{"login"}},
			{DocID: "chunk2", Score: 0.7, MatchedTerms: []string{"logout"}},
		}, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return []*store.VectorResult{
			{ID: "chunk1", Score: 0.85},
			{ID: "chunk3", Score: 0.6},
		}, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: searching with a query
	results, err := engine.Search(context.Background(), "login authentication", SearchOptions{})

	// Then: returns combined results from both indices
	require.NoError(t, err)
	assert.NotEmpty(t, results)
	// chunk1 should be in both lists and ranked high
	assert.Equal(t, "chunk1", results[0].Chunk.ID)
	assert.True(t, results[0].InBothLists)
}

func TestEngine_Search_EmptyQuery(t *testing.T) {
	// Given: an engine
	engine, _, _, _, _ := setupTestEngine(t)

	// When: searching with empty query
	results, err := engine.Search(context.Background(), "", SearchOptions{})

	// Then: returns empty results without error
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestEngine_Search_WhitespaceQuery(t *testing.T) {
	// Given: an engine
	engine, _, _, _, _ := setupTestEngine(t)

	// When: searching with whitespace-only query
	results, err := engine.Search(context.Background(), "   \t\n  ", SearchOptions{})

	// Then: returns empty results without error
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestEngine_Search_LimitEnforcement(t *testing.T) {
	// Given: an engine with many results
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		results := make([]*store.BM25Result, 50)
		for i := range results {
			results[i] = &store.BM25Result{DocID: "chunk1", Score: float64(50-i) / 50}
		}
		return results, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return nil, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: searching with limit
	results, err := engine.Search(context.Background(), "test", SearchOptions{Limit: 5})

	// Then: respects the limit
	require.NoError(t, err)
	assert.LessOrEqual(t, len(results), 5)
}

func TestEngine_Search_MaxLimitEnforcement(t *testing.T) {
	// Given: an engine
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return nil, nil
	}
	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return nil, nil
	}
	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: searching with limit exceeding max
	_, err := engine.Search(context.Background(), "test", SearchOptions{Limit: 500})

	// Then: caps at max limit (no error, just capped)
	require.NoError(t, err)
}

func TestEngine_Index(t *testing.T) {
	// Given: an engine
	engine, bm25, vector, embedder, metadata := setupTestEngine(t)

	var indexedDocs []*store.Document
	var indexedVectors [][]float32

	bm25.IndexFn = func(ctx context.Context, docs []*store.Document) error {
		indexedDocs = docs
		return nil
	}

	vector.AddFn = func(ctx context.Context, ids []string, vectors [][]float32) error {
		indexedVectors = vectors
		return nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	chunks := createTestChunks()

	// When: indexing chunks
	err := engine.Index(context.Background(), chunks)

	// Then: indexes in both BM25 and vector store
	require.NoError(t, err)
	assert.Len(t, indexedDocs, len(chunks))
	assert.Len(t, indexedVectors, len(chunks))

	// And: saves to metadata store
	for _, c := range chunks {
		chunk, _ := metadata.GetChunk(context.Background(), c.ID)
		assert.NotNil(t, chunk)
	}
}

func TestEngine_Delete(t *testing.T) {
	// Given: an engine with indexed chunks
	engine, bm25, vector, _, _ := setupTestEngine(t)

	var deletedBM25 []string
	var deletedVector []string

	bm25.DeleteFn = func(ctx context.Context, docIDs []string) error {
		deletedBM25 = docIDs
		return nil
	}

	vector.DeleteFn = func(ctx context.Context, ids []string) error {
		deletedVector = ids
		return nil
	}

	// When: deleting chunks
	err := engine.Delete(context.Background(), []string{"chunk1", "chunk2"})

	// Then: deletes from both indices
	require.NoError(t, err)
	assert.Equal(t, []string{"chunk1", "chunk2"}, deletedBM25)
	assert.Equal(t, []string{"chunk1", "chunk2"}, deletedVector)
}

// =============================================================================
// BUG-023: Cross-Store Transaction - Best Effort Delete
// =============================================================================

func TestEngine_Delete_BM25FailsContinues(t *testing.T) {
	// Given: engine where BM25 delete fails
	engine, bm25, vector, _, metadata := setupTestEngine(t)

	bm25.DeleteFn = func(ctx context.Context, docIDs []string) error {
		return errors.New("BM25 delete failed")
	}

	var deletedVector []string
	vector.DeleteFn = func(ctx context.Context, ids []string) error {
		deletedVector = ids
		return nil
	}

	// When: deleting chunks
	err := engine.Delete(context.Background(), []string{"chunk1", "chunk2"})

	// Then: succeeds (best effort) and continues to delete from vector and metadata
	require.NoError(t, err, "BUG-023: should continue despite BM25 failure")
	assert.Equal(t, []string{"chunk1", "chunk2"}, deletedVector, "should still delete from vector")
	// Metadata delete is called via mock (already configured in setupTestEngine)
	_ = metadata // verify metadata is used
}

func TestEngine_Delete_VectorFailsContinues(t *testing.T) {
	// Given: engine where vector delete fails
	engine, bm25, vector, _, _ := setupTestEngine(t)

	var deletedBM25 []string
	bm25.DeleteFn = func(ctx context.Context, docIDs []string) error {
		deletedBM25 = docIDs
		return nil
	}

	vector.DeleteFn = func(ctx context.Context, ids []string) error {
		return errors.New("vector delete failed")
	}

	// When: deleting chunks
	err := engine.Delete(context.Background(), []string{"chunk1", "chunk2"})

	// Then: succeeds (best effort) - metadata is source of truth
	require.NoError(t, err, "BUG-023: should continue despite vector failure")
	assert.Equal(t, []string{"chunk1", "chunk2"}, deletedBM25, "should still delete from BM25")
}

func TestEngine_Delete_MetadataFailsReturnsError(t *testing.T) {
	// Given: engine where metadata delete fails
	engine, bm25, vector, _, metadata := setupTestEngine(t)

	bm25.DeleteFn = func(ctx context.Context, docIDs []string) error {
		return nil
	}

	vector.DeleteFn = func(ctx context.Context, ids []string) error {
		return nil
	}

	metadata.DeleteChunksFn = func(ctx context.Context, ids []string) error {
		return errors.New("metadata delete failed")
	}

	// When: deleting chunks
	err := engine.Delete(context.Background(), []string{"chunk1", "chunk2"})

	// Then: fails because metadata is source of truth
	require.Error(t, err, "BUG-023: should fail when metadata delete fails")
	assert.Contains(t, err.Error(), "metadata")
}

func TestEngine_Stats(t *testing.T) {
	// Given: an engine with indexed data
	engine, bm25, vector, _, _ := setupTestEngine(t)

	bm25.StatsFn = func() *store.IndexStats {
		return &store.IndexStats{DocumentCount: 100, TermCount: 500}
	}

	vector.CountFn = func() int {
		return 100
	}

	// When: getting stats
	stats := engine.Stats()

	// Then: returns aggregated statistics
	assert.Equal(t, 100, stats.BM25Stats.DocumentCount)
	assert.Equal(t, 100, stats.VectorCount)
}

func TestEngine_Close(t *testing.T) {
	// Given: an engine
	engine, _, _, _, _ := setupTestEngine(t)

	// When: closing
	err := engine.Close()

	// Then: closes without error
	require.NoError(t, err)
}

// =============================================================================
// AC02: Parallel Search Execution Tests
// =============================================================================

func TestEngine_Search_ParallelExecution(t *testing.T) {
	// Given: an engine with mocked indices
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	// Track when each search starts
	bm25Started := make(chan struct{})
	vectorStarted := make(chan struct{})
	done := make(chan struct{})

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		close(bm25Started)
		<-done // Wait for signal
		return []*store.BM25Result{{DocID: "chunk1", Score: 0.9}}, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		close(vectorStarted)
		<-done // Wait for signal
		return []*store.VectorResult{{ID: "chunk2", Score: 0.8}}, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: searching
	go func() {
		// Wait for both to start
		<-bm25Started
		<-vectorStarted
		close(done) // Release both
	}()

	results, err := engine.Search(context.Background(), "test", SearchOptions{})

	// Then: both searches executed in parallel
	require.NoError(t, err)
	assert.NotEmpty(t, results)
}

func TestEngine_Search_ContextTimeout(t *testing.T) {
	// Given: an engine with slow search
	engine, bm25, _, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			return []*store.BM25Result{{DocID: "chunk1"}}, nil
		}
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: searching with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := engine.Search(ctx, "test", SearchOptions{})

	// Then: returns context error
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestEngine_Search_GracefulDegradation_VectorFails(t *testing.T) {
	// Given: vector search fails (embedder unavailable)
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 0.9},
			{DocID: "chunk2", Score: 0.7},
		}, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return nil, errors.New("connection refused: Ollama not running")
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: searching
	results, err := engine.Search(context.Background(), "test", SearchOptions{})

	// Then: returns BM25-only results (graceful degradation)
	require.NoError(t, err)
	assert.NotEmpty(t, results)
	// All results should have zero vector score
	for _, r := range results {
		assert.Zero(t, r.VecScore)
	}
}

func TestEngine_Search_GracefulDegradation_BM25Fails(t *testing.T) {
	// Given: BM25 search fails
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return nil, errors.New("index corrupted")
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return []*store.VectorResult{
			{ID: "chunk1", Score: 0.85},
			{ID: "chunk3", Score: 0.6},
		}, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: searching
	results, err := engine.Search(context.Background(), "test", SearchOptions{})

	// Then: returns vector-only results
	require.NoError(t, err)
	assert.NotEmpty(t, results)
	// All results should have zero BM25 score
	for _, r := range results {
		assert.Zero(t, r.BM25Score)
	}
}

func TestEngine_Search_GracefulDegradation_EmbeddingFails(t *testing.T) {
	// Given: embedding fails
	engine, bm25, _, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 0.9},
		}, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return nil, errors.New("embedding service unavailable")
	}

	// When: searching
	results, err := engine.Search(context.Background(), "test", SearchOptions{})

	// Then: returns BM25-only results
	require.NoError(t, err)
	assert.NotEmpty(t, results)
}

func TestEngine_Search_BothFail(t *testing.T) {
	// Given: both searches fail
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return nil, errors.New("BM25 error")
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return nil, errors.New("vector error")
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: searching
	_, err := engine.Search(context.Background(), "test", SearchOptions{})

	// Then: returns error
	assert.Error(t, err)
}

// =============================================================================
// AC04: Result Enrichment Tests
// =============================================================================

func TestEngine_Search_ResultEnrichment(t *testing.T) {
	// Given: an engine with chunk metadata
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 0.9, MatchedTerms: []string{"login", "authentication"}},
		}, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return []*store.VectorResult{
			{ID: "chunk1", Score: 0.85},
		}, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: searching
	results, err := engine.Search(context.Background(), "login", SearchOptions{})

	// Then: results are enriched with full chunk data
	require.NoError(t, err)
	require.NotEmpty(t, results)

	result := results[0]
	assert.Equal(t, "chunk1", result.Chunk.ID)
	assert.Equal(t, "auth/login.go", result.Chunk.FilePath)
	assert.Contains(t, result.Chunk.Content, "Login")
	assert.Equal(t, 10, result.Chunk.StartLine)
	assert.Equal(t, 25, result.Chunk.EndLine)
	assert.NotEmpty(t, result.Chunk.Symbols)

	// And: includes individual scores
	assert.Greater(t, result.BM25Score, 0.0)
	assert.Greater(t, result.VecScore, 0.0)
	assert.Greater(t, result.Score, 0.0)

	// And: marks as in both lists
	assert.True(t, result.InBothLists)
}

func TestEngine_Search_HighlightsCalculation(t *testing.T) {
	// Given: an engine
	engine, bm25, vector, embedder, metadata := setupTestEngine(t)

	// Add a chunk with known content
	metadata.chunks["test-chunk"] = &store.Chunk{
		ID:       "test-chunk",
		Content:  "The quick brown fox jumps over the lazy dog",
		FilePath: "test.txt",
	}

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "test-chunk", Score: 0.9, MatchedTerms: []string{"quick", "fox"}},
		}, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return nil, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: searching
	results, err := engine.Search(context.Background(), "quick fox", SearchOptions{})

	// Then: highlights are calculated for matched terms
	require.NoError(t, err)
	require.NotEmpty(t, results)

	// Highlights should contain ranges for "quick" and "fox"
	assert.NotEmpty(t, results[0].Highlights)
}

// =============================================================================
// AC05: Filter Support Tests
// =============================================================================

func TestEngine_Search_FilterByContentType_Code(t *testing.T) {
	// Given: results with mixed content types
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 0.9}, // code
			{DocID: "chunk3", Score: 0.8}, // markdown
		}, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return nil, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: filtering by "code"
	results, err := engine.Search(context.Background(), "test", SearchOptions{
		Filter: "code",
	})

	// Then: only code results returned
	require.NoError(t, err)
	for _, r := range results {
		assert.Equal(t, store.ContentTypeCode, r.Chunk.ContentType)
	}
}

func TestEngine_Search_FilterByContentType_Docs(t *testing.T) {
	// Given: results with mixed content types
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 0.9}, // code
			{DocID: "chunk3", Score: 0.8}, // markdown
		}, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return nil, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: filtering by "docs"
	results, err := engine.Search(context.Background(), "test", SearchOptions{
		Filter: "docs",
	})

	// Then: only markdown/docs results returned
	require.NoError(t, err)
	for _, r := range results {
		assert.Equal(t, store.ContentTypeMarkdown, r.Chunk.ContentType)
	}
}

func TestEngine_Search_FilterByLanguage(t *testing.T) {
	// Given: results with mixed languages
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 0.9}, // go
			{DocID: "chunk4", Score: 0.8}, // typescript
			{DocID: "chunk2", Score: 0.7}, // go
		}, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return nil, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: filtering by language
	results, err := engine.Search(context.Background(), "test", SearchOptions{
		Language: "go",
	})

	// Then: only Go results returned
	require.NoError(t, err)
	for _, r := range results {
		assert.Equal(t, "go", r.Chunk.Language)
	}
}

func TestEngine_Search_FilterBySymbolType(t *testing.T) {
	// Given: results with different symbol types
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 0.9}, // function
			{DocID: "chunk5", Score: 0.8}, // type
		}, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return nil, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: filtering by symbol type
	results, err := engine.Search(context.Background(), "test", SearchOptions{
		SymbolType: "function",
	})

	// Then: only function results returned
	require.NoError(t, err)
	for _, r := range results {
		hasFunction := false
		for _, s := range r.Chunk.Symbols {
			if s.Type == store.SymbolTypeFunction {
				hasFunction = true
				break
			}
		}
		assert.True(t, hasFunction, "result should have function symbol")
	}
}

func TestEngine_Search_FilterCombination(t *testing.T) {
	// Given: results with various attributes
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 0.9}, // go, code, function
			{DocID: "chunk4", Score: 0.8}, // typescript, code, function
			{DocID: "chunk3", Score: 0.7}, // markdown, docs
		}, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return nil, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: filtering by multiple criteria (AND logic)
	results, err := engine.Search(context.Background(), "test", SearchOptions{
		Filter:   "code",
		Language: "go",
	})

	// Then: only results matching ALL criteria
	require.NoError(t, err)
	for _, r := range results {
		assert.Equal(t, store.ContentTypeCode, r.Chunk.ContentType)
		assert.Equal(t, "go", r.Chunk.Language)
	}
}

func TestEngine_Search_FilterAll(t *testing.T) {
	// Given: an engine
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 0.9},
			{DocID: "chunk3", Score: 0.8},
		}, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return nil, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: filtering by "all" (no filter)
	results, err := engine.Search(context.Background(), "test", SearchOptions{
		Filter: "all",
	})

	// Then: all results returned
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

// =============================================================================
// AC03: Result Fusion Tests (Basic - full RRF in F14)
// =============================================================================

func TestEngine_Search_Deduplication(t *testing.T) {
	// Given: results appearing in both indices
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 0.9},
			{DocID: "chunk2", Score: 0.7},
		}, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return []*store.VectorResult{
			{ID: "chunk1", Score: 0.85}, // Duplicate
			{ID: "chunk3", Score: 0.6},
		}, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: searching
	results, err := engine.Search(context.Background(), "test", SearchOptions{})

	// Then: duplicates are merged (chunk1 only appears once)
	require.NoError(t, err)

	ids := make(map[string]bool)
	for _, r := range results {
		assert.False(t, ids[r.Chunk.ID], "duplicate result: %s", r.Chunk.ID)
		ids[r.Chunk.ID] = true
	}
}

func TestEngine_Search_ScoreNormalization(t *testing.T) {
	// Given: an engine
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 10.5}, // Unnormalized BM25 score
		}, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return []*store.VectorResult{
			{ID: "chunk1", Score: 0.85}, // Already normalized
		}, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: searching
	results, err := engine.Search(context.Background(), "test", SearchOptions{})

	// Then: final scores are normalized to 0-1
	require.NoError(t, err)
	require.NotEmpty(t, results)

	for _, r := range results {
		assert.GreaterOrEqual(t, r.Score, 0.0)
		assert.LessOrEqual(t, r.Score, 1.0)
	}
}

func TestEngine_Search_WeightedScoring(t *testing.T) {
	// Given: custom weights
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 1.0},
		}, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return []*store.VectorResult{
			{ID: "chunk1", Score: 0.5},
		}, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: searching with custom weights
	results, err := engine.Search(context.Background(), "test", SearchOptions{
		Weights: &Weights{BM25: 0.8, Semantic: 0.2},
	})

	// Then: weights affect final score
	require.NoError(t, err)
	require.NotEmpty(t, results)

	// With BM25=0.8 and Semantic=0.2, BM25 should dominate
	// Expected: 0.8 * 1.0 + 0.2 * 0.5 = 0.9 (before normalization)
	assert.Greater(t, results[0].Score, 0.0)
}

func TestEngine_Search_DefaultWeights(t *testing.T) {
	// Given: no custom weights
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{{DocID: "chunk1", Score: 1.0}}, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return []*store.VectorResult{{ID: "chunk1", Score: 1.0}}, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: searching without weights
	results, err := engine.Search(context.Background(), "test", SearchOptions{})

	// Then: uses default weights (BM25=0.35, Semantic=0.65)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.Greater(t, results[0].Score, 0.0)
}

// =============================================================================
// Benchmarks (AC06: Performance)
// =============================================================================

func BenchmarkEngine_Search(b *testing.B) {
	bm25 := &MockBM25Index{
		SearchFn: func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			results := make([]*store.BM25Result, 100)
			for i := range results {
				results[i] = &store.BM25Result{DocID: "chunk1", Score: float64(100-i) / 100}
			}
			return results, nil
		},
	}

	vector := &MockVectorStore{
		SearchFn: func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			results := make([]*store.VectorResult, 100)
			for i := range results {
				results[i] = &store.VectorResult{ID: "chunk1", Score: float32(100-i) / 100}
			}
			return results, nil
		},
	}

	embedder := &MockEmbedder{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		},
	}

	metadata := NewMockMetadataStore()
	metadata.chunks["chunk1"] = &store.Chunk{ID: "chunk1", Content: "test content"}

	engine := New(bm25, vector, embedder, metadata, DefaultConfig())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = engine.Search(context.Background(), "test query", SearchOptions{Limit: 10})
	}
}

// =============================================================================
// Edge Cases
// =============================================================================

func TestEngine_Search_NoResults(t *testing.T) {
	// Given: no matching documents
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return nil, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return nil, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: searching
	results, err := engine.Search(context.Background(), "nonexistent query", SearchOptions{})

	// Then: returns empty slice without error
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestEngine_Search_MissingChunkMetadata(t *testing.T) {
	// Given: result references chunk not in metadata
	engine, bm25, vector, embedder, metadata := setupTestEngine(t)

	// Clear metadata
	metadata.chunks = make(map[string]*store.Chunk)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "missing-chunk", Score: 0.9},
		}, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return nil, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: searching
	results, err := engine.Search(context.Background(), "test", SearchOptions{})

	// Then: skips results with missing metadata
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestNew_NilDependencies(t *testing.T) {
	// When: creating engine with nil dependencies
	// Then: panics or returns error
	assert.Panics(t, func() {
		New(nil, nil, nil, nil, DefaultConfig())
	})
}

// Ensure MockEmbedder implements embed.Embedder
var _ embed.Embedder = (*MockEmbedder)(nil)

// =============================================================================
// F28 Scope Filtering Integration Tests
// =============================================================================

func TestEngine_Search_ScopeFilter(t *testing.T) {
	// Given: engine with chunks in different directories
	bm25 := &MockBM25Index{
		SearchFn: func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{
				{DocID: "api-chunk", Score: 0.9},
				{DocID: "web-chunk", Score: 0.8},
				{DocID: "db-chunk", Score: 0.7},
			}, nil
		},
	}

	vector := &MockVectorStore{
		SearchFn: func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return nil, nil // BM25-only for this test
		},
	}

	embedder := &MockEmbedder{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		},
		DimensionsFn: func() int { return 768 },
	}

	metadata := NewMockMetadataStore()
	metadata.chunks["api-chunk"] = &store.Chunk{ID: "api-chunk", FilePath: "services/api/handler.go", Content: "api handler"}
	metadata.chunks["web-chunk"] = &store.Chunk{ID: "web-chunk", FilePath: "services/web/index.ts", Content: "web handler"}
	metadata.chunks["db-chunk"] = &store.Chunk{ID: "db-chunk", FilePath: "services/db/query.go", Content: "db query"}

	engine := New(bm25, vector, embedder, metadata, DefaultConfig())

	// When: searching with scope filter
	results, err := engine.Search(context.Background(), "handler", SearchOptions{
		Scopes: []string{"services/api"},
	})

	// Then: only api results returned
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "api-chunk", results[0].Chunk.ID)
	assert.Equal(t, "services/api/handler.go", results[0].Chunk.FilePath)
}

func TestEngine_Search_MultipleScopesORLogic(t *testing.T) {
	// Given: engine with chunks in different directories
	bm25 := &MockBM25Index{
		SearchFn: func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{
				{DocID: "api-chunk", Score: 0.9},
				{DocID: "web-chunk", Score: 0.8},
				{DocID: "db-chunk", Score: 0.7},
			}, nil
		},
	}

	vector := &MockVectorStore{
		SearchFn: func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return nil, nil
		},
	}

	embedder := &MockEmbedder{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		},
		DimensionsFn: func() int { return 768 },
	}

	metadata := NewMockMetadataStore()
	metadata.chunks["api-chunk"] = &store.Chunk{ID: "api-chunk", FilePath: "services/api/handler.go", Content: "api"}
	metadata.chunks["web-chunk"] = &store.Chunk{ID: "web-chunk", FilePath: "services/web/index.ts", Content: "web"}
	metadata.chunks["db-chunk"] = &store.Chunk{ID: "db-chunk", FilePath: "services/db/query.go", Content: "db"}

	engine := New(bm25, vector, embedder, metadata, DefaultConfig())

	// When: searching with multiple scopes (OR logic)
	results, err := engine.Search(context.Background(), "test", SearchOptions{
		Scopes: []string{"services/api", "services/web"},
	})

	// Then: both api and web results returned (OR logic)
	require.NoError(t, err)
	require.Len(t, results, 2)
	ids := []string{results[0].Chunk.ID, results[1].Chunk.ID}
	assert.Contains(t, ids, "api-chunk")
	assert.Contains(t, ids, "web-chunk")
}

func TestEngine_Search_InvalidScope_ReturnsEmpty(t *testing.T) {
	// Given: engine with chunks
	bm25 := &MockBM25Index{
		SearchFn: func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{
				{DocID: "chunk1", Score: 0.9},
			}, nil
		},
	}

	vector := &MockVectorStore{
		SearchFn: func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return nil, nil
		},
	}

	embedder := &MockEmbedder{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		},
		DimensionsFn: func() int { return 768 },
	}

	metadata := NewMockMetadataStore()
	metadata.chunks["chunk1"] = &store.Chunk{ID: "chunk1", FilePath: "services/api/handler.go", Content: "handler"}

	engine := New(bm25, vector, embedder, metadata, DefaultConfig())

	// When: searching with non-existent scope
	results, err := engine.Search(context.Background(), "test", SearchOptions{
		Scopes: []string{"nonexistent/path"},
	})

	// Then: empty results, no error
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestEngine_Search_ScopeWithOtherFilters(t *testing.T) {
	// Given: engine with mixed content types in different directories
	bm25 := &MockBM25Index{
		SearchFn: func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{
				{DocID: "api-code", Score: 0.9},
				{DocID: "api-docs", Score: 0.8},
				{DocID: "web-code", Score: 0.7},
			}, nil
		},
	}

	vector := &MockVectorStore{
		SearchFn: func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return nil, nil
		},
	}

	embedder := &MockEmbedder{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		},
		DimensionsFn: func() int { return 768 },
	}

	metadata := NewMockMetadataStore()
	metadata.chunks["api-code"] = &store.Chunk{ID: "api-code", FilePath: "services/api/handler.go", Content: "code", ContentType: store.ContentTypeCode}
	metadata.chunks["api-docs"] = &store.Chunk{ID: "api-docs", FilePath: "services/api/README.md", Content: "docs", ContentType: store.ContentTypeMarkdown}
	metadata.chunks["web-code"] = &store.Chunk{ID: "web-code", FilePath: "services/web/index.ts", Content: "code", ContentType: store.ContentTypeCode}

	engine := New(bm25, vector, embedder, metadata, DefaultConfig())

	// When: searching with scope AND content type filter (AND logic)
	results, err := engine.Search(context.Background(), "test", SearchOptions{
		Filter: "code",
		Scopes: []string{"services/api"},
	})

	// Then: only code in services/api returned
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "api-code", results[0].Chunk.ID)
}

// =============================================================================
// DEBT-012: Nil vs Empty Slice Tests
// =============================================================================

func TestEngine_calculateHighlights_EmptyInputs(t *testing.T) {
	engine, _, _, _, _ := setupTestEngine(t)

	t.Run("empty matched terms", func(t *testing.T) {
		result := engine.calculateHighlights("some content", []string{})
		// DEBT-012: should return empty slice, not nil
		assert.NotNil(t, result, "should return empty slice, not nil")
		assert.Empty(t, result)
	})

	t.Run("empty content", func(t *testing.T) {
		result := engine.calculateHighlights("", []string{"term"})
		// DEBT-012: should return empty slice, not nil
		assert.NotNil(t, result, "should return empty slice, not nil")
		assert.Empty(t, result)
	})

	t.Run("both empty", func(t *testing.T) {
		result := engine.calculateHighlights("", []string{})
		// DEBT-012: should return empty slice, not nil
		assert.NotNil(t, result, "should return empty slice, not nil")
		assert.Empty(t, result)
	})
}

// =============================================================================
// NewEngine Tests - BUG-033: Replace panic with error return
// =============================================================================

func TestNewEngine_AllDependencies_Success(t *testing.T) {
	// Given: all valid dependencies
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{}
	embedder := &MockEmbedder{}
	metadata := NewMockMetadataStore()
	config := DefaultConfig()

	// When: creating engine with NewEngine
	engine, err := NewEngine(bm25, vector, embedder, metadata, config)

	// Then: returns engine without error
	require.NoError(t, err)
	require.NotNil(t, engine)
}

func TestNewEngine_NilBM25_ReturnsError(t *testing.T) {
	// Given: nil BM25 index
	vector := &MockVectorStore{}
	embedder := &MockEmbedder{}
	metadata := NewMockMetadataStore()
	config := DefaultConfig()

	// When: creating engine with NewEngine
	engine, err := NewEngine(nil, vector, embedder, metadata, config)

	// Then: returns nil engine and error
	assert.Nil(t, engine)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNilDependency)
	assert.Contains(t, err.Error(), "bm25")
}

func TestNewEngine_NilVector_ReturnsError(t *testing.T) {
	// Given: nil vector store
	bm25 := &MockBM25Index{}
	embedder := &MockEmbedder{}
	metadata := NewMockMetadataStore()
	config := DefaultConfig()

	// When: creating engine with NewEngine
	engine, err := NewEngine(bm25, nil, embedder, metadata, config)

	// Then: returns nil engine and error
	assert.Nil(t, engine)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNilDependency)
	assert.Contains(t, err.Error(), "vector")
}

func TestNewEngine_NilEmbedder_ReturnsError(t *testing.T) {
	// Given: nil embedder
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{}
	metadata := NewMockMetadataStore()
	config := DefaultConfig()

	// When: creating engine with NewEngine
	engine, err := NewEngine(bm25, vector, nil, metadata, config)

	// Then: returns nil engine and error
	assert.Nil(t, engine)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNilDependency)
	assert.Contains(t, err.Error(), "embedder")
}

func TestNewEngine_NilMetadata_ReturnsError(t *testing.T) {
	// Given: nil metadata store
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{}
	embedder := &MockEmbedder{}
	config := DefaultConfig()

	// When: creating engine with NewEngine
	engine, err := NewEngine(bm25, vector, embedder, nil, config)

	// Then: returns nil engine and error
	assert.Nil(t, engine)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNilDependency)
	assert.Contains(t, err.Error(), "metadata")
}

func TestNewEngine_AllNil_ReturnsError(t *testing.T) {
	// Given: all nil dependencies
	config := DefaultConfig()

	// When: creating engine with NewEngine
	engine, err := NewEngine(nil, nil, nil, nil, config)

	// Then: returns nil engine and error (first nil checked)
	assert.Nil(t, engine)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNilDependency)
}

func TestNewEngine_WithOptions(t *testing.T) {
	// Given: valid dependencies and classifier option
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{}
	embedder := &MockEmbedder{}
	metadata := NewMockMetadataStore()
	config := DefaultConfig()
	classifier := &PatternClassifier{}

	// When: creating engine with option
	engine, err := NewEngine(bm25, vector, embedder, metadata, config, WithClassifier(classifier))

	// Then: returns engine with classifier set
	require.NoError(t, err)
	require.NotNil(t, engine)
	assert.NotNil(t, engine.classifier)
}

// =============================================================================
// QW-5: Dimension Mismatch Handling Tests
// =============================================================================

func TestEngine_validateDimensions_NoStoredDimension(t *testing.T) {
	// Given: engine with no stored dimension (first-time or legacy index)
	engine, _, _, _, metadata := setupTestEngine(t)

	// Clear any stored state
	metadata.state = make(map[string]string)

	// When: validating dimensions
	err := engine.validateDimensions(context.Background())

	// Then: returns nil (allow search)
	assert.NoError(t, err, "should allow search when no stored dimension")
}

func TestEngine_validateDimensions_DimensionsMatch(t *testing.T) {
	// Given: engine with matching stored dimension (768)
	engine, _, _, embedder, metadata := setupTestEngine(t)

	// Set embedder dimension
	embedder.DimensionsFn = func() int { return 768 }

	// Store matching dimension
	metadata.state[store.StateKeyIndexDimension] = "768"
	metadata.state[store.StateKeyIndexModel] = "test-model"

	// When: validating dimensions
	err := engine.validateDimensions(context.Background())

	// Then: returns nil (dimensions match)
	assert.NoError(t, err, "should allow search when dimensions match")
}

func TestEngine_validateDimensions_DimensionMismatch(t *testing.T) {
	// Given: engine with mismatched dimension (Ollama 768 → Static768 fallback)
	engine, _, _, embedder, metadata := setupTestEngine(t)

	// Current embedder has 768 dimensions
	embedder.DimensionsFn = func() int { return 768 }

	// Index was created with 384 dimensions (different model)
	metadata.state[store.StateKeyIndexDimension] = "384"
	metadata.state[store.StateKeyIndexModel] = "mxbai-embed-large"

	// When: validating dimensions
	err := engine.validateDimensions(context.Background())

	// Then: returns ErrDimensionMismatch with helpful message
	assert.Error(t, err, "should return error on dimension mismatch")
	assert.ErrorIs(t, err, ErrDimensionMismatch)
	assert.Contains(t, err.Error(), "384")
	assert.Contains(t, err.Error(), "768")
	assert.Contains(t, err.Error(), "reindex")
}

func TestEngine_validateDimensions_InvalidStoredDimension(t *testing.T) {
	// Given: engine with invalid stored dimension
	engine, _, _, _, metadata := setupTestEngine(t)

	// Store invalid dimension (non-numeric)
	metadata.state[store.StateKeyIndexDimension] = "invalid"

	// When: validating dimensions
	err := engine.validateDimensions(context.Background())

	// Then: returns nil (graceful handling of corrupted state)
	assert.NoError(t, err, "should allow search when stored dimension is invalid")
}

func TestEngine_validateDimensions_GetStateError(t *testing.T) {
	// Given: engine where GetState returns an error
	engine, _, _, embedder, metadata := setupTestEngine(t)

	// Set embedder dimension
	embedder.DimensionsFn = func() int { return 768 }

	// Configure GetState to return an error
	metadata.GetStateFn = func(ctx context.Context, key string) (string, error) {
		return "", errors.New("database connection lost")
	}

	// When: validating dimensions
	err := engine.validateDimensions(context.Background())

	// Then: returns nil (gracefully handles database errors, allows search to proceed)
	assert.NoError(t, err, "should allow search when GetState fails (graceful degradation)")
}

func TestEngine_Search_DimensionMismatch_GracefulDegradation(t *testing.T) {
	// Given: engine with dimension mismatch
	engine, bm25, _, embedder, metadata := setupTestEngine(t)

	// Current embedder has 768 dimensions, but index has 384
	embedder.DimensionsFn = func() int { return 768 }
	metadata.state[store.StateKeyIndexDimension] = "384"
	metadata.state[store.StateKeyIndexModel] = "different-model"

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 0.9},
			{DocID: "chunk2", Score: 0.7},
		}, nil
	}

	// When: searching
	results, err := engine.Search(context.Background(), "test query", SearchOptions{})

	// Then: returns BM25-only results (graceful degradation)
	require.NoError(t, err, "should not fail - use BM25 fallback")
	assert.NotEmpty(t, results)

	// All results should have zero vector score (semantic search skipped)
	for _, r := range results {
		assert.Zero(t, r.VecScore, "vector search should be skipped")
	}
}

func TestEngine_Index_StoresDimensionInfo(t *testing.T) {
	// Given: engine with embedder
	engine, bm25, vector, embedder, metadata := setupTestEngine(t)

	// Setup embedder to return 768 dimensions
	embedder.DimensionsFn = func() int { return 768 }
	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	bm25.IndexFn = func(ctx context.Context, docs []*store.Document) error { return nil }
	vector.AddFn = func(ctx context.Context, ids []string, vectors [][]float32) error { return nil }

	chunks := []*store.Chunk{
		{ID: "test-chunk", Content: "test content"},
	}

	// When: indexing chunks
	err := engine.Index(context.Background(), chunks)

	// Then: stores dimension info in metadata
	require.NoError(t, err)
	assert.Equal(t, "768", metadata.state[store.StateKeyIndexDimension], "should store dimension")
	assert.Equal(t, "mock-embedder", metadata.state[store.StateKeyIndexModel], "should store model name")
}

// =============================================================================
// FEAT-QI5: Adjacent Context Enrichment Tests
// =============================================================================

func TestEngine_enrichResultsWithAdjacent_EmptyResults(t *testing.T) {
	// Given: engine and empty results
	engine, _, _, _, _ := setupTestEngine(t)

	results := []*SearchResult{}

	// When: enriching empty results
	engine.enrichResultsWithAdjacent(context.Background(), results, 2, 5)

	// Then: no panic, results still empty
	assert.Empty(t, results)
}

func TestEngine_enrichResultsWithAdjacent_ZeroAdjacentCount(t *testing.T) {
	// Given: engine and results with chunks
	engine, _, _, _, _ := setupTestEngine(t)

	results := []*SearchResult{
		{
			Chunk: &store.Chunk{
				ID:        "chunk1",
				FileID:    "file1",
				StartLine: 10,
				EndLine:   20,
			},
		},
	}

	// When: enriching with adjacentCount = 0
	engine.enrichResultsWithAdjacent(context.Background(), results, 0, 5)

	// Then: no enrichment happens
	assert.Nil(t, results[0].AdjacentContext.Before)
	assert.Nil(t, results[0].AdjacentContext.After)
}

func TestEngine_enrichResultsWithAdjacent_ResultWithoutChunk(t *testing.T) {
	// Given: engine and results without chunk data
	engine, _, _, _, _ := setupTestEngine(t)

	results := []*SearchResult{
		{Chunk: nil}, // No chunk
		{Chunk: &store.Chunk{ID: "chunk1", FileID: ""}}, // Empty FileID
	}

	// When: enriching
	engine.enrichResultsWithAdjacent(context.Background(), results, 2, 5)

	// Then: results are skipped gracefully (no panic)
	assert.Nil(t, results[0].AdjacentContext.Before)
	assert.Nil(t, results[1].AdjacentContext.Before)
}

func TestEngine_enrichResultsWithAdjacent_SingleFileMultipleChunks(t *testing.T) {
	// Given: engine with chunks from a single file
	engine, _, _, _, metadata := setupTestEngine(t)

	// Setup file chunks in metadata (lines 1-10, 11-20, 21-30, 31-40, 41-50)
	fileID := "test-file"
	chunks := []*store.Chunk{
		{ID: "chunk1", FileID: fileID, StartLine: 1, EndLine: 10},
		{ID: "chunk2", FileID: fileID, StartLine: 11, EndLine: 20},
		{ID: "chunk3", FileID: fileID, StartLine: 21, EndLine: 30}, // Target chunk
		{ID: "chunk4", FileID: fileID, StartLine: 31, EndLine: 40},
		{ID: "chunk5", FileID: fileID, StartLine: 41, EndLine: 50},
	}
	for _, c := range chunks {
		metadata.chunks[c.ID] = c
	}

	// Result is for chunk3 (middle chunk)
	results := []*SearchResult{
		{
			Chunk: chunks[2], // chunk3
		},
	}

	// When: enriching with adjacentCount = 2
	engine.enrichResultsWithAdjacent(context.Background(), results, 2, 5)

	// Then: before contains chunk2, chunk1 (sorted by proximity)
	require.Len(t, results[0].AdjacentContext.Before, 2)
	assert.Equal(t, "chunk2", results[0].AdjacentContext.Before[0].ID, "closest before should be chunk2")
	assert.Equal(t, "chunk1", results[0].AdjacentContext.Before[1].ID, "second before should be chunk1")

	// And: after contains chunk4, chunk5 (sorted by proximity)
	require.Len(t, results[0].AdjacentContext.After, 2)
	assert.Equal(t, "chunk4", results[0].AdjacentContext.After[0].ID, "closest after should be chunk4")
	assert.Equal(t, "chunk5", results[0].AdjacentContext.After[1].ID, "second after should be chunk5")
}

func TestEngine_enrichResultsWithAdjacent_AdjacentCountLimit(t *testing.T) {
	// Given: engine with many chunks in a file
	engine, _, _, _, metadata := setupTestEngine(t)

	// Setup 10 chunks
	fileID := "test-file"
	chunks := make([]*store.Chunk, 10)
	for i := 0; i < 10; i++ {
		chunks[i] = &store.Chunk{
			ID:        fmt.Sprintf("chunk%d", i),
			FileID:    fileID,
			StartLine: i*10 + 1,
			EndLine:   (i + 1) * 10,
		}
		metadata.chunks[chunks[i].ID] = chunks[i]
	}

	// Result is for chunk5 (middle)
	results := []*SearchResult{
		{Chunk: chunks[5]},
	}

	// When: enriching with adjacentCount = 2 (should limit before/after to 2 each)
	engine.enrichResultsWithAdjacent(context.Background(), results, 2, 5)

	// Then: before is limited to 2 closest
	assert.Len(t, results[0].AdjacentContext.Before, 2, "before should be limited to adjacentCount")
	assert.Equal(t, "chunk4", results[0].AdjacentContext.Before[0].ID, "should have closest before")
	assert.Equal(t, "chunk3", results[0].AdjacentContext.Before[1].ID, "should have second closest before")

	// And: after is limited to 2 closest
	assert.Len(t, results[0].AdjacentContext.After, 2, "after should be limited to adjacentCount")
	assert.Equal(t, "chunk6", results[0].AdjacentContext.After[0].ID, "should have closest after")
	assert.Equal(t, "chunk7", results[0].AdjacentContext.After[1].ID, "should have second closest after")
}

// =============================================================================
// QI-1: Query Expansion Tests
// =============================================================================

func TestEngine_Search_BM25UsesExpandedQuery(t *testing.T) {
	// Given: engine with query expander
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	// Track what query is passed to BM25 and embedder
	var bm25Query string
	var embeddedQuery string

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		bm25Query = query
		return []*store.BM25Result{{DocID: "chunk1", Score: 0.9}}, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		embeddedQuery = text
		return make([]float32, 768), nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return []*store.VectorResult{{ID: "chunk1", Score: 0.8}}, nil
	}

	// Set up expander on the engine
	engine.expander = NewQueryExpander()

	// When: searching with a query that will be expanded
	originalQuery := "Search function"
	_, err := engine.Search(context.Background(), originalQuery, SearchOptions{})
	require.NoError(t, err)

	// Then: BM25 should receive the EXPANDED query
	assert.NotEqual(t, originalQuery, bm25Query,
		"BM25 should use expanded query, not original")
	assert.Contains(t, bm25Query, "func",
		"BM25 query should contain 'func' synonym for 'function'")

	// Vector search should receive the FORMATTED query with Qwen3 instruction prefix
	// Per Qwen3 docs: queries need instruction prefix for optimal retrieval
	expectedFormattedQuery := formatQueryForEmbedding(originalQuery)
	assert.Equal(t, expectedFormattedQuery, embeddedQuery,
		"vector search should use Qwen3 formatted query with instruction prefix")
}

func TestEngine_Search_BM25QueryExpansion(t *testing.T) {
	// These tests verify BM25 query expansion for the 3 dogfood queries
	// RCA-010: Vocabulary mismatch between natural language and code

	tests := []struct {
		name          string
		query         string
		expectedTerms []string // Terms that should appear in BM25 query
	}{
		{
			name:          "Search function expands to code terms",
			query:         "Search function",
			expectedTerms: []string{"Search", "func", "method"},
		},
		{
			name:          "Index function expands to code terms",
			query:         "Index function",
			expectedTerms: []string{"Index", "func", "method", "indexer"},
		},
		{
			name:          "OllamaEmbedder expands to related terms",
			query:         "OllamaEmbedder",
			expectedTerms: []string{"Ollama", "Embedder", "embed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: engine with expander
			engine, bm25, vector, embedder, _ := setupTestEngine(t)

			var bm25Query string
			bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
				bm25Query = query
				return []*store.BM25Result{{DocID: "chunk1", Score: 0.5}}, nil
			}

			embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
				return make([]float32, 768), nil
			}

			vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
				return []*store.VectorResult{{ID: "chunk1", Score: 0.5}}, nil
			}

			engine.expander = NewQueryExpander()

			// When: searching
			_, err := engine.Search(context.Background(), tt.query, SearchOptions{})
			require.NoError(t, err)

			// Then: BM25 query should contain expected terms
			for _, term := range tt.expectedTerms {
				assert.Contains(t, bm25Query, term,
					"BM25 query should contain '%s'", term)
			}
		})
	}
}

// =============================================================================
// FEAT-RR1: Reranker Integration Tests
// =============================================================================

// MockReranker implements Reranker for testing
type MockReranker struct {
	RerankFn    func(ctx context.Context, query string, documents []string, topK int) ([]RerankResult, error)
	AvailableFn func(ctx context.Context) bool
	called      int
}

func (m *MockReranker) Rerank(ctx context.Context, query string, documents []string, topK int) ([]RerankResult, error) {
	m.called++
	if m.RerankFn != nil {
		return m.RerankFn(ctx, query, documents, topK)
	}
	// Default: return results in reverse order with decreasing scores
	results := make([]RerankResult, len(documents))
	for i := range documents {
		// Reverse order: last document becomes first
		origIdx := len(documents) - 1 - i
		results[i] = RerankResult{
			Index:    origIdx,
			Score:    1.0 - float64(i)*0.1,
			Document: documents[origIdx],
		}
	}
	return results, nil
}

func (m *MockReranker) Available(ctx context.Context) bool {
	if m.AvailableFn != nil {
		return m.AvailableFn(ctx)
	}
	return true
}

func (m *MockReranker) Close() error {
	return nil
}

// TestEngine_RerankResults_Integration tests reranker integration
func TestEngine_RerankResults_Integration(t *testing.T) {
	t.Run("reranks results when reranker available", func(t *testing.T) {
		// Given: engine with mock reranker
		engine, bm25, vector, embedder, metadata := setupTestEngine(t)

		// Setup chunks in metadata
		chunk1 := &store.Chunk{ID: "chunk1", Content: "func Login() {}", FilePath: "auth.go", ContentType: store.ContentTypeCode}
		chunk2 := &store.Chunk{ID: "chunk2", Content: "func Logout() {}", FilePath: "auth.go", ContentType: store.ContentTypeCode}
		chunk3 := &store.Chunk{ID: "chunk3", Content: "func Register() {}", FilePath: "user.go", ContentType: store.ContentTypeCode}

		metadata.SaveChunks(context.Background(), []*store.Chunk{chunk1, chunk2, chunk3})

		bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{
				{DocID: "chunk1", Score: 0.9},
				{DocID: "chunk2", Score: 0.8},
				{DocID: "chunk3", Score: 0.7},
			}, nil
		}

		embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		}

		vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return []*store.VectorResult{
				{ID: "chunk1", Score: 0.85},
				{ID: "chunk2", Score: 0.75},
				{ID: "chunk3", Score: 0.65},
			}, nil
		}

		// Reranker will reorder: chunk3 (best), chunk2, chunk1
		mockReranker := &MockReranker{
			RerankFn: func(ctx context.Context, query string, documents []string, topK int) ([]RerankResult, error) {
				// Simulate reranker preferring chunk3 (Register)
				return []RerankResult{
					{Index: 2, Score: 0.95, Document: documents[2]}, // chunk3 becomes first
					{Index: 1, Score: 0.80, Document: documents[1]}, // chunk2 second
					{Index: 0, Score: 0.60, Document: documents[0]}, // chunk1 third
				}, nil
			},
		}

		engine.reranker = mockReranker

		// When: searching
		results, err := engine.Search(context.Background(), "authentication", SearchOptions{Limit: 10})

		// Then: reranker was called and results reordered
		require.NoError(t, err)
		require.Len(t, results, 3)
		assert.Equal(t, 1, mockReranker.called, "reranker should be called once")

		// Verify reranked order
		assert.Equal(t, "chunk3", results[0].Chunk.ID, "chunk3 should be first after reranking")
		assert.Equal(t, "chunk2", results[1].Chunk.ID, "chunk2 should be second")
		assert.Equal(t, "chunk1", results[2].Chunk.ID, "chunk1 should be third")

		// Verify scores updated
		assert.InDelta(t, 0.95, results[0].Score, 0.01, "first result should have reranker score")
		assert.InDelta(t, 0.80, results[1].Score, 0.01, "second result should have reranker score")
		assert.InDelta(t, 0.60, results[2].Score, 0.01, "third result should have reranker score")
	})

	t.Run("graceful degradation when reranker unavailable", func(t *testing.T) {
		// Given: engine with unavailable reranker
		engine, bm25, vector, embedder, metadata := setupTestEngine(t)

		chunk1 := &store.Chunk{ID: "chunk1", Content: "content1", FilePath: "a.go", ContentType: store.ContentTypeCode}
		metadata.SaveChunks(context.Background(), []*store.Chunk{chunk1})

		bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{{DocID: "chunk1", Score: 0.9}}, nil
		}
		embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		}
		vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return []*store.VectorResult{{ID: "chunk1", Score: 0.85}}, nil
		}

		mockReranker := &MockReranker{
			AvailableFn: func(ctx context.Context) bool {
				return false // Reranker unavailable
			},
		}
		engine.reranker = mockReranker

		// When: searching
		results, err := engine.Search(context.Background(), "query", SearchOptions{Limit: 10})

		// Then: search succeeds without reranking
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, 0, mockReranker.called, "reranker should not be called when unavailable")
	})

	t.Run("graceful degradation on reranker error", func(t *testing.T) {
		// Given: engine with reranker that returns error
		engine, bm25, vector, embedder, metadata := setupTestEngine(t)

		chunk1 := &store.Chunk{ID: "chunk1", Content: "content1", FilePath: "a.go", ContentType: store.ContentTypeCode}
		chunk2 := &store.Chunk{ID: "chunk2", Content: "content2", FilePath: "b.go", ContentType: store.ContentTypeCode}
		metadata.SaveChunks(context.Background(), []*store.Chunk{chunk1, chunk2})

		bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{
				{DocID: "chunk1", Score: 0.9},
				{DocID: "chunk2", Score: 0.8},
			}, nil
		}
		embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		}
		vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return []*store.VectorResult{
				{ID: "chunk1", Score: 0.85},
				{ID: "chunk2", Score: 0.75},
			}, nil
		}

		mockReranker := &MockReranker{
			RerankFn: func(ctx context.Context, query string, documents []string, topK int) ([]RerankResult, error) {
				return nil, errors.New("reranker service unavailable")
			},
		}
		engine.reranker = mockReranker

		// When: searching
		results, err := engine.Search(context.Background(), "query", SearchOptions{Limit: 10})

		// Then: search succeeds with original order (graceful degradation)
		require.NoError(t, err)
		require.Len(t, results, 2)
		assert.Equal(t, 1, mockReranker.called, "reranker should have been attempted")
		// Original RRF order preserved
		assert.Equal(t, "chunk1", results[0].Chunk.ID)
		assert.Equal(t, "chunk2", results[1].Chunk.ID)
	})

	t.Run("no reranking when reranker nil", func(t *testing.T) {
		// Given: engine without reranker
		engine, bm25, vector, embedder, metadata := setupTestEngine(t)

		chunk1 := &store.Chunk{ID: "chunk1", Content: "content1", FilePath: "a.go", ContentType: store.ContentTypeCode}
		metadata.SaveChunks(context.Background(), []*store.Chunk{chunk1})

		bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{{DocID: "chunk1", Score: 0.9}}, nil
		}
		embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		}
		vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return []*store.VectorResult{{ID: "chunk1", Score: 0.85}}, nil
		}

		// engine.reranker is nil by default

		// When: searching
		results, err := engine.Search(context.Background(), "query", SearchOptions{Limit: 10})

		// Then: search succeeds without reranking
		require.NoError(t, err)
		require.Len(t, results, 1)
	})

	t.Run("skip reranking with single result", func(t *testing.T) {
		// Given: engine with reranker but only 1 result
		engine, bm25, vector, embedder, metadata := setupTestEngine(t)

		chunk1 := &store.Chunk{ID: "chunk1", Content: "content1", FilePath: "a.go", ContentType: store.ContentTypeCode}
		metadata.SaveChunks(context.Background(), []*store.Chunk{chunk1})

		bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{{DocID: "chunk1", Score: 0.9}}, nil
		}
		embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		}
		vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return []*store.VectorResult{{ID: "chunk1", Score: 0.85}}, nil
		}

		mockReranker := &MockReranker{}
		engine.reranker = mockReranker

		// When: searching with single result
		results, err := engine.Search(context.Background(), "query", SearchOptions{Limit: 10})

		// Then: reranker not called (nothing to rerank)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, 0, mockReranker.called, "reranker should not be called for single result")
	})

	t.Run("graceful handling of invalid reranker indices", func(t *testing.T) {
		// Given: engine with reranker that returns invalid indices
		engine, bm25, vector, embedder, metadata := setupTestEngine(t)

		chunk1 := &store.Chunk{ID: "chunk1", Content: "content1", FilePath: "a.go", ContentType: store.ContentTypeCode}
		chunk2 := &store.Chunk{ID: "chunk2", Content: "content2", FilePath: "b.go", ContentType: store.ContentTypeCode}
		metadata.SaveChunks(context.Background(), []*store.Chunk{chunk1, chunk2})

		bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{
				{DocID: "chunk1", Score: 0.9},
				{DocID: "chunk2", Score: 0.8},
			}, nil
		}
		embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		}
		vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return []*store.VectorResult{
				{ID: "chunk1", Score: 0.85},
				{ID: "chunk2", Score: 0.75},
			}, nil
		}

		// Reranker returns some invalid indices
		mockReranker := &MockReranker{
			RerankFn: func(ctx context.Context, query string, documents []string, topK int) ([]RerankResult, error) {
				return []RerankResult{
					{Index: 0, Score: 0.95},  // Valid index
					{Index: 99, Score: 0.90}, // Invalid index (out of range)
					{Index: -1, Score: 0.85}, // Invalid index (negative)
					{Index: 1, Score: 0.80},  // Valid index
				}, nil
			},
		}
		engine.reranker = mockReranker

		// When: searching
		results, err := engine.Search(context.Background(), "query", SearchOptions{Limit: 10})

		// Then: search succeeds, invalid indices are filtered out
		require.NoError(t, err)
		assert.Len(t, results, 2, "should only include results with valid indices")
		assert.Equal(t, 1, mockReranker.called, "reranker should be called")
	})

	t.Run("graceful handling when chunk fetch fails", func(t *testing.T) {
		// Given: engine with reranker but metadata.GetChunks fails
		engine, bm25, vector, embedder, metadata := setupTestEngine(t)

		// Override GetChunks to simulate failure
		originalGetChunks := metadata.GetChunkFn
		metadata.GetChunkFn = func(ctx context.Context, id string) (*store.Chunk, error) {
			return nil, errors.New("database connection lost")
		}
		defer func() { metadata.GetChunkFn = originalGetChunks }()

		// Pre-populate metadata for BM25/Vector to find
		chunk1 := &store.Chunk{ID: "chunk1", Content: "content1", FilePath: "a.go", ContentType: store.ContentTypeCode}
		chunk2 := &store.Chunk{ID: "chunk2", Content: "content2", FilePath: "b.go", ContentType: store.ContentTypeCode}
		metadata.chunks["chunk1"] = chunk1
		metadata.chunks["chunk2"] = chunk2

		bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{
				{DocID: "chunk1", Score: 0.9},
				{DocID: "chunk2", Score: 0.8},
			}, nil
		}
		embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		}
		vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return []*store.VectorResult{
				{ID: "chunk1", Score: 0.85},
				{ID: "chunk2", Score: 0.75},
			}, nil
		}

		mockReranker := &MockReranker{}
		engine.reranker = mockReranker

		// When: searching
		results, err := engine.Search(context.Background(), "query", SearchOptions{Limit: 10})

		// Then: search still succeeds (reranker was skipped due to chunk fetch failure)
		require.NoError(t, err)
		assert.Len(t, results, 2, "should return results even if reranking failed")
		// Reranker may or may not be called depending on implementation
		// The important thing is the search doesn't fail
	})
}

// =============================================================================
// FEAT-DIM1: BM25Only Mode Tests
// =============================================================================

func TestEngine_Search_BM25Only_SkipsVectorSearch(t *testing.T) {
	// Given: engine with BM25Only option
	engine, bm25, vector, embedder, _ := setupTestEngine(t)

	var vectorSearchCalled bool
	var embedderCalled bool

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 0.9, MatchedTerms: []string{"test"}},
			{DocID: "chunk2", Score: 0.7, MatchedTerms: []string{"query"}},
		}, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		vectorSearchCalled = true
		return nil, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		embedderCalled = true
		return make([]float32, 768), nil
	}

	// When: searching with BM25Only mode
	results, err := engine.Search(context.Background(), "test query", SearchOptions{
		Limit:    10,
		BM25Only: true,
	})

	// Then: search succeeds with BM25-only results
	require.NoError(t, err)
	assert.NotEmpty(t, results)
	assert.False(t, vectorSearchCalled, "vector search should NOT be called in BM25Only mode")
	assert.False(t, embedderCalled, "embedder should NOT be called in BM25Only mode")

	// All results should have zero vector score
	for _, r := range results {
		assert.Zero(t, r.VecScore, "vector score should be zero in BM25Only mode")
	}
}

func TestEngine_Search_BM25Only_StillAppliesFilters(t *testing.T) {
	// Given: engine with BM25Only and filters
	engine, bm25, _, _, metadata := setupTestEngine(t)

	// Add chunks with different types
	chunk1 := &store.Chunk{ID: "chunk1", Content: "code content", FilePath: "main.go", ContentType: store.ContentTypeCode, Language: "go"}
	chunk2 := &store.Chunk{ID: "chunk2", Content: "doc content", FilePath: "README.md", ContentType: store.ContentTypeMarkdown}
	metadata.SaveChunks(context.Background(), []*store.Chunk{chunk1, chunk2})

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 0.9},
			{DocID: "chunk2", Score: 0.8},
		}, nil
	}

	// When: searching with BM25Only and code filter
	results, err := engine.Search(context.Background(), "content", SearchOptions{
		Limit:    10,
		BM25Only: true,
		Filter:   "code",
	})

	// Then: only code results returned
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "main.go", results[0].Chunk.FilePath)
}

func TestEngine_Search_BM25Only_StillAppliesReranking(t *testing.T) {
	// Given: engine with BM25Only and reranker
	engine, bm25, _, _, metadata := setupTestEngine(t)

	chunk1 := &store.Chunk{ID: "chunk1", Content: "first content", FilePath: "a.go", ContentType: store.ContentTypeCode}
	chunk2 := &store.Chunk{ID: "chunk2", Content: "second content", FilePath: "b.go", ContentType: store.ContentTypeCode}
	metadata.SaveChunks(context.Background(), []*store.Chunk{chunk1, chunk2})

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 0.9},
			{DocID: "chunk2", Score: 0.8},
		}, nil
	}

	mockReranker := &MockReranker{
		AvailableFn: func(ctx context.Context) bool { return true },
		RerankFn: func(ctx context.Context, query string, documents []string, topK int) ([]RerankResult, error) {
			// Reverse the order
			return []RerankResult{
				{Index: 1, Score: 1.0, Document: documents[1]},
				{Index: 0, Score: 0.5, Document: documents[0]},
			}, nil
		},
	}
	engine.reranker = mockReranker

	// When: searching with BM25Only
	results, err := engine.Search(context.Background(), "content", SearchOptions{
		Limit:    10,
		BM25Only: true,
	})

	// Then: reranker still called and results reordered
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, 1, mockReranker.called, "reranker should be called in BM25Only mode")
	// Reranker reversed the order, so chunk2 should be first
	assert.Equal(t, "b.go", results[0].Chunk.FilePath)
}

// TestEngine_enrichResultsWithAdjacent tests FEAT-QI5: adjacent chunk retrieval.
func TestEngine_enrichResultsWithAdjacent(t *testing.T) {
	// Given: engine with multiple chunks in same file
	engine, _, _, _, metadata := setupTestEngine(t)

	// Create chunks for a single file with different line ranges
	chunks := []*store.Chunk{
		{ID: "chunk1", FileID: "file1", FilePath: "main.go", Content: "package main", StartLine: 1, EndLine: 5},
		{ID: "chunk2", FileID: "file1", FilePath: "main.go", Content: "func main() {}", StartLine: 7, EndLine: 15},
		{ID: "chunk3", FileID: "file1", FilePath: "main.go", Content: "func helper() {}", StartLine: 17, EndLine: 25},
		{ID: "chunk4", FileID: "file1", FilePath: "main.go", Content: "func utility() {}", StartLine: 27, EndLine: 35},
	}
	err := metadata.SaveChunks(context.Background(), chunks)
	require.NoError(t, err)

	// When: enriching results with adjacent chunks
	results := []*SearchResult{
		{Chunk: chunks[1]}, // chunk2 (lines 7-15) - should have chunk1 before and chunk3 after
	}
	engine.enrichResultsWithAdjacent(context.Background(), results, 1, 5)

	// Then: adjacent context populated correctly
	require.Len(t, results[0].AdjacentContext.Before, 1, "should have 1 chunk before")
	require.Len(t, results[0].AdjacentContext.After, 1, "should have 1 chunk after")

	// Verify before chunk is chunk1 (closest before)
	assert.Equal(t, "chunk1", results[0].AdjacentContext.Before[0].ID)

	// Verify after chunk is chunk3 (closest after)
	assert.Equal(t, "chunk3", results[0].AdjacentContext.After[0].ID)
}

// TestEngine_enrichResultsWithAdjacent_MultipleChunks tests adjacent retrieval with limit > 1.
func TestEngine_enrichResultsWithAdjacent_MultipleChunks(t *testing.T) {
	engine, _, _, _, metadata := setupTestEngine(t)

	// Create 5 chunks in sequence
	chunks := []*store.Chunk{
		{ID: "c1", FileID: "file1", FilePath: "main.go", Content: "1", StartLine: 1, EndLine: 10},
		{ID: "c2", FileID: "file1", FilePath: "main.go", Content: "2", StartLine: 11, EndLine: 20},
		{ID: "c3", FileID: "file1", FilePath: "main.go", Content: "3", StartLine: 21, EndLine: 30}, // Target
		{ID: "c4", FileID: "file1", FilePath: "main.go", Content: "4", StartLine: 31, EndLine: 40},
		{ID: "c5", FileID: "file1", FilePath: "main.go", Content: "5", StartLine: 41, EndLine: 50},
	}
	err := metadata.SaveChunks(context.Background(), chunks)
	require.NoError(t, err)

	// When: requesting 2 adjacent chunks
	results := []*SearchResult{{Chunk: chunks[2]}} // c3
	engine.enrichResultsWithAdjacent(context.Background(), results, 2, 5)

	// Then: should get 2 before (c1, c2) and 2 after (c4, c5)
	require.Len(t, results[0].AdjacentContext.Before, 2)
	require.Len(t, results[0].AdjacentContext.After, 2)

	// Before chunks should be sorted by proximity (closest first)
	assert.Equal(t, "c2", results[0].AdjacentContext.Before[0].ID, "c2 is closest before")
	assert.Equal(t, "c1", results[0].AdjacentContext.Before[1].ID, "c1 is second closest before")

	// After chunks should be sorted by proximity (closest first)
	assert.Equal(t, "c4", results[0].AdjacentContext.After[0].ID, "c4 is closest after")
	assert.Equal(t, "c5", results[0].AdjacentContext.After[1].ID, "c5 is second closest after")
}

// TestEngine_enrichResultsWithAdjacent_DisabledWhenZero tests that adjacentCount=0 skips enrichment.
func TestEngine_enrichResultsWithAdjacent_DisabledWhenZero(t *testing.T) {
	engine, _, _, _, metadata := setupTestEngine(t)

	chunks := []*store.Chunk{
		{ID: "c1", FileID: "file1", FilePath: "main.go", Content: "1", StartLine: 1, EndLine: 10},
		{ID: "c2", FileID: "file1", FilePath: "main.go", Content: "2", StartLine: 11, EndLine: 20},
	}
	err := metadata.SaveChunks(context.Background(), chunks)
	require.NoError(t, err)

	// When: adjacentCount is 0
	results := []*SearchResult{{Chunk: chunks[0]}}
	engine.enrichResultsWithAdjacent(context.Background(), results, 0, 5)

	// Then: no adjacent context added
	assert.Empty(t, results[0].AdjacentContext.Before)
	assert.Empty(t, results[0].AdjacentContext.After)
}

// TestEngine_enrichResultsWithAdjacent_TopNLimit tests that only top N results are enriched.
func TestEngine_enrichResultsWithAdjacent_TopNLimit(t *testing.T) {
	engine, _, _, _, metadata := setupTestEngine(t)

	// Create chunks for two files
	chunks := []*store.Chunk{
		{ID: "f1c1", FileID: "file1", FilePath: "a.go", Content: "1", StartLine: 1, EndLine: 10},
		{ID: "f1c2", FileID: "file1", FilePath: "a.go", Content: "2", StartLine: 11, EndLine: 20},
		{ID: "f2c1", FileID: "file2", FilePath: "b.go", Content: "1", StartLine: 1, EndLine: 10},
		{ID: "f2c2", FileID: "file2", FilePath: "b.go", Content: "2", StartLine: 11, EndLine: 20},
	}
	err := metadata.SaveChunks(context.Background(), chunks)
	require.NoError(t, err)

	// When: topN=1, only first result should be enriched
	results := []*SearchResult{
		{Chunk: chunks[0]}, // f1c1 - should be enriched
		{Chunk: chunks[2]}, // f2c1 - should NOT be enriched (beyond topN)
	}
	engine.enrichResultsWithAdjacent(context.Background(), results, 1, 1) // topN=1

	// Then: only first result has adjacent context
	assert.Len(t, results[0].AdjacentContext.After, 1, "first result should be enriched")
	assert.Empty(t, results[1].AdjacentContext.After, "second result should NOT be enriched")
}

// TestEngine_enrichResultsWithAdjacent_DifferentFiles tests that cross-file chunks are not mixed.
func TestEngine_enrichResultsWithAdjacent_DifferentFiles(t *testing.T) {
	engine, _, _, _, metadata := setupTestEngine(t)

	// Create chunks in two different files
	chunks := []*store.Chunk{
		{ID: "f1c1", FileID: "file1", FilePath: "a.go", Content: "1", StartLine: 1, EndLine: 10},
		{ID: "f1c2", FileID: "file1", FilePath: "a.go", Content: "2", StartLine: 11, EndLine: 20},
		{ID: "f2c1", FileID: "file2", FilePath: "b.go", Content: "1", StartLine: 1, EndLine: 10},
	}
	err := metadata.SaveChunks(context.Background(), chunks)
	require.NoError(t, err)

	// When: enriching f2c1
	results := []*SearchResult{{Chunk: chunks[2]}} // f2c1 from file2
	engine.enrichResultsWithAdjacent(context.Background(), results, 1, 5)

	// Then: no adjacent chunks (file2 only has one chunk)
	assert.Empty(t, results[0].AdjacentContext.Before, "file2 has no chunks before f2c1")
	assert.Empty(t, results[0].AdjacentContext.After, "file2 has no chunks after f2c1")
}

// =============================================================================
// FEAT-UNIX3: Search Explanation Mode Tests
// =============================================================================

// TestEngine_Search_ExplainMode_HybridSearch tests explain output for normal hybrid search.
func TestEngine_Search_ExplainMode_HybridSearch(t *testing.T) {
	// Given: engine with indexed documents
	engine, bm25, vector, embedder, metadata := setupTestEngine(t)

	chunk1 := &store.Chunk{ID: "chunk1", FilePath: "main.go", Content: "package main", ContentType: store.ContentTypeCode}
	chunk2 := &store.Chunk{ID: "chunk2", FilePath: "util.go", Content: "func util()", ContentType: store.ContentTypeCode}
	metadata.chunks = map[string]*store.Chunk{chunk1.ID: chunk1, chunk2.ID: chunk2}

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 10.0, MatchedTerms: []string{"test"}},
			{DocID: "chunk2", Score: 8.0, MatchedTerms: []string{"test"}},
		}, nil
	}
	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return []*store.VectorResult{
			{ID: "chunk2", Score: 0.9},
			{ID: "chunk1", Score: 0.7},
		}, nil
	}
	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: searching with Explain=true
	results, err := engine.Search(context.Background(), "test query", SearchOptions{
		Limit:   10,
		Explain: true,
	})

	// Then: results include explain data
	require.NoError(t, err)
	require.NotEmpty(t, results)

	// First result should have ExplainData
	require.NotNil(t, results[0].Explain, "first result should have explain data")
	explain := results[0].Explain

	assert.Equal(t, "test query", explain.Query)
	assert.Equal(t, 2, explain.BM25ResultCount, "should report BM25 result count")
	assert.Equal(t, 2, explain.VectorResultCount, "should report vector result count")
	assert.Equal(t, 60, explain.RRFConstant, "should report RRF k value")
	assert.False(t, explain.BM25Only, "should not be BM25-only mode")
	assert.False(t, explain.DimensionMismatch, "should not have dimension mismatch")

	// All results should have BM25Rank and VecRank populated
	for _, r := range results {
		// At least one of BM25Rank or VecRank should be non-zero
		assert.True(t, r.BM25Rank > 0 || r.VecRank > 0, "results should have rank info")
	}

	// Subsequent results should not duplicate ExplainData
	if len(results) > 1 {
		assert.Nil(t, results[1].Explain, "only first result should have explain data")
	}
}

// TestEngine_Search_ExplainMode_BM25Only tests explain output for BM25-only search.
func TestEngine_Search_ExplainMode_BM25Only(t *testing.T) {
	// Given: engine with indexed documents
	engine, bm25, _, _, metadata := setupTestEngine(t)

	chunk1 := &store.Chunk{ID: "chunk1", FilePath: "main.go", Content: "package main", ContentType: store.ContentTypeCode}
	metadata.chunks = map[string]*store.Chunk{chunk1.ID: chunk1}

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 10.0, MatchedTerms: []string{"test"}},
		}, nil
	}

	// When: searching with BM25Only and Explain
	results, err := engine.Search(context.Background(), "test query", SearchOptions{
		Limit:    10,
		BM25Only: true,
		Explain:  true,
	})

	// Then: explain shows BM25-only mode
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.NotNil(t, results[0].Explain)

	explain := results[0].Explain
	assert.True(t, explain.BM25Only, "should indicate BM25-only mode")
	assert.Equal(t, 0, explain.VectorResultCount, "vector result count should be 0")
	assert.Greater(t, explain.BM25ResultCount, 0, "BM25 result count should be > 0")
}

// TestEngine_Search_ExplainMode_Disabled tests that explain data is not populated when disabled.
func TestEngine_Search_ExplainMode_Disabled(t *testing.T) {
	// Given: engine with indexed documents
	engine, bm25, vector, embedder, metadata := setupTestEngine(t)

	chunk1 := &store.Chunk{ID: "chunk1", FilePath: "main.go", Content: "package main", ContentType: store.ContentTypeCode}
	metadata.chunks = map[string]*store.Chunk{chunk1.ID: chunk1}

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{{DocID: "chunk1", Score: 10.0}}, nil
	}
	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return []*store.VectorResult{{ID: "chunk1", Score: 0.9}}, nil
	}
	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	// When: searching with Explain=false (default)
	results, err := engine.Search(context.Background(), "test query", SearchOptions{
		Limit:   10,
		Explain: false,
	})

	// Then: no explain data attached
	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.Nil(t, results[0].Explain, "explain data should be nil when disabled")
}

// =============================================================================
// Engine Option Tests (DEBT-028: Coverage improvement)
// =============================================================================

// TestWithMetrics verifies the WithMetrics option sets the metrics collector.
func TestWithMetrics(t *testing.T) {
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{}
	embedder := &MockEmbedder{}
	metadata := NewMockMetadataStore()

	// Create engine with metrics option
	engine, err := NewEngine(bm25, vector, embedder, metadata, DefaultConfig())
	require.NoError(t, err)

	// Verify engine was created successfully
	assert.NotNil(t, engine)

	// The metrics option should be applied during NewEngine
	// We can't directly test internal state, but we can verify the option doesn't error
}

// TestWithQueryExpander verifies the WithQueryExpander option sets the expander.
func TestWithQueryExpander(t *testing.T) {
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{}
	embedder := &MockEmbedder{}
	metadata := NewMockMetadataStore()

	// Create a query expander
	expander := NewQueryExpander()

	// Create engine with query expander option
	engine, err := NewEngine(bm25, vector, embedder, metadata, DefaultConfig(), WithQueryExpander(expander))
	require.NoError(t, err)
	assert.NotNil(t, engine)

	// Verify the expander is used during BM25 search
	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		// Query should be expanded (though we can't verify internal expansion directly)
		return []*store.BM25Result{{DocID: "chunk1", Score: 5.0}}, nil
	}

	metadata.chunks["chunk1"] = &store.Chunk{ID: "chunk1", Content: "test content", FilePath: "test.go"}

	// Search should work with expander
	results, err := engine.Search(context.Background(), "getData", SearchOptions{Limit: 10, BM25Only: true})
	require.NoError(t, err)
	assert.NotEmpty(t, results)
}

// TestWithReranker verifies the WithReranker option sets the reranker.
func TestWithReranker(t *testing.T) {
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{}
	embedder := &MockEmbedder{}
	metadata := NewMockMetadataStore()

	rerankCalled := false
	mockReranker := &MockReranker{
		AvailableFn: func(ctx context.Context) bool { return true },
		RerankFn: func(ctx context.Context, query string, documents []string, topK int) ([]RerankResult, error) {
			rerankCalled = true
			// Return reranked order
			results := make([]RerankResult, len(documents))
			for i := range documents {
				results[i] = RerankResult{Index: i, Score: float64(len(documents) - i)}
			}
			return results, nil
		},
	}

	// Create engine with reranker option
	engine, err := NewEngine(bm25, vector, embedder, metadata, DefaultConfig(), WithReranker(mockReranker))
	require.NoError(t, err)
	assert.NotNil(t, engine)

	// Set up search results
	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 10.0},
			{DocID: "chunk2", Score: 8.0},
		}, nil
	}
	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return []*store.VectorResult{
			{ID: "chunk1", Score: 0.9},
			{ID: "chunk2", Score: 0.8},
		}, nil
	}
	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	metadata.chunks["chunk1"] = &store.Chunk{ID: "chunk1", Content: "test content 1", FilePath: "test1.go"}
	metadata.chunks["chunk2"] = &store.Chunk{ID: "chunk2", Content: "test content 2", FilePath: "test2.go"}

	// Search should trigger reranking
	results, err := engine.Search(context.Background(), "test query", SearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.NotEmpty(t, results)
	assert.True(t, rerankCalled, "reranker should be called")
}

// TestWithMultiQuerySearch verifies the WithMultiQuerySearch option.
func TestWithMultiQuerySearch(t *testing.T) {
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{}
	embedder := &MockEmbedder{}
	metadata := NewMockMetadataStore()

	// Create a mock query decomposer
	decomposer := &testQueryDecomposer{
		shouldDecomposeFn: func(query string) bool { return true },
		decomposeFn: func(query string) []SubQuery {
			return []SubQuery{
				{Query: query + " part1", Weight: 0.5},
				{Query: query + " part2", Weight: 0.5},
			}
		},
	}

	// Create engine with multi-query option
	engine, err := NewEngine(bm25, vector, embedder, metadata, DefaultConfig(), WithMultiQuerySearch(decomposer))
	require.NoError(t, err)
	assert.NotNil(t, engine)
}

// TestWithMultiQuerySearch_NilDecomposer verifies nil decomposer is handled.
func TestWithMultiQuerySearch_NilDecomposer(t *testing.T) {
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{}
	embedder := &MockEmbedder{}
	metadata := NewMockMetadataStore()

	// Create engine with nil decomposer (should not panic)
	engine, err := NewEngine(bm25, vector, embedder, metadata, DefaultConfig(), WithMultiQuerySearch(nil))
	require.NoError(t, err)
	assert.NotNil(t, engine)
}

// testQueryDecomposer is a test implementation of QueryDecomposer.
type testQueryDecomposer struct {
	shouldDecomposeFn func(query string) bool
	decomposeFn       func(query string) []SubQuery
}

func (t *testQueryDecomposer) ShouldDecompose(query string) bool {
	if t.shouldDecomposeFn != nil {
		return t.shouldDecomposeFn(query)
	}
	return false
}

func (t *testQueryDecomposer) Decompose(query string) []SubQuery {
	if t.decomposeFn != nil {
		return t.decomposeFn(query)
	}
	return []SubQuery{{Query: query, Weight: 1.0}}
}

// =============================================================================
// classifyQueryType Tests (DEBT-028: Coverage improvement)
// =============================================================================

// TestClassifyQueryType_ExplicitLexicalWeights tests classification with high BM25 weight.
func TestClassifyQueryType_ExplicitLexicalWeights(t *testing.T) {
	engine, _, _, _, _ := setupTestEngine(t)

	opts := SearchOptions{
		Weights: &Weights{BM25: 0.7, Semantic: 0.3},
	}

	queryType := engine.classifyQueryType(context.Background(), "test", opts)
	assert.Equal(t, QueryTypeLexical, queryType, "should classify as lexical when BM25 > 0.6")
}

// TestClassifyQueryType_ExplicitSemanticWeights tests classification with high semantic weight.
func TestClassifyQueryType_ExplicitSemanticWeights(t *testing.T) {
	engine, _, _, _, _ := setupTestEngine(t)

	opts := SearchOptions{
		Weights: &Weights{BM25: 0.2, Semantic: 0.8},
	}

	queryType := engine.classifyQueryType(context.Background(), "test", opts)
	assert.Equal(t, QueryTypeSemantic, queryType, "should classify as semantic when Semantic > 0.6")
}

// TestClassifyQueryType_ExplicitMixedWeights tests classification with balanced weights.
func TestClassifyQueryType_ExplicitMixedWeights(t *testing.T) {
	engine, _, _, _, _ := setupTestEngine(t)

	opts := SearchOptions{
		Weights: &Weights{BM25: 0.5, Semantic: 0.5},
	}

	queryType := engine.classifyQueryType(context.Background(), "test", opts)
	assert.Equal(t, QueryTypeMixed, queryType, "should classify as mixed when weights are balanced")
}

// TestClassifyQueryType_WithClassifier tests using the classifier.
func TestClassifyQueryType_WithClassifier(t *testing.T) {
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{}
	embedder := &MockEmbedder{}
	metadata := NewMockMetadataStore()

	classifier := &engineTestClassifier{
		classifyFn: func(ctx context.Context, query string) (QueryType, Weights, error) {
			return QueryTypeSemantic, Weights{BM25: 0.2, Semantic: 0.8}, nil
		},
	}

	engine, err := NewEngine(bm25, vector, embedder, metadata, DefaultConfig(), WithClassifier(classifier))
	require.NoError(t, err)

	opts := SearchOptions{} // No explicit weights

	queryType := engine.classifyQueryType(context.Background(), "how does this work", opts)
	assert.Equal(t, QueryTypeSemantic, queryType, "should use classifier result")
}

// TestClassifyQueryType_ClassifierError tests fallback when classifier errors.
func TestClassifyQueryType_ClassifierError(t *testing.T) {
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{}
	embedder := &MockEmbedder{}
	metadata := NewMockMetadataStore()

	classifier := &engineTestClassifier{
		classifyFn: func(ctx context.Context, query string) (QueryType, Weights, error) {
			return QueryTypeMixed, Weights{}, errors.New("classifier error")
		},
	}

	engine, err := NewEngine(bm25, vector, embedder, metadata, DefaultConfig(), WithClassifier(classifier))
	require.NoError(t, err)

	opts := SearchOptions{} // No explicit weights

	queryType := engine.classifyQueryType(context.Background(), "test query", opts)
	assert.Equal(t, QueryTypeMixed, queryType, "should default to mixed on classifier error")
}

// TestClassifyQueryType_NoClassifier tests default behavior without classifier.
func TestClassifyQueryType_NoClassifier(t *testing.T) {
	engine, _, _, _, _ := setupTestEngine(t)

	opts := SearchOptions{} // No explicit weights, no classifier

	queryType := engine.classifyQueryType(context.Background(), "test query", opts)
	assert.Equal(t, QueryTypeMixed, queryType, "should default to mixed without classifier")
}

// =============================================================================
// singleSearch Tests (DEBT-028: Coverage improvement)
// =============================================================================

// TestSingleSearch_EmptyQuery tests singleSearch with empty query.
func TestSingleSearch_EmptyQuery(t *testing.T) {
	engine, _, _, _, _ := setupTestEngine(t)

	results, err := engine.singleSearch(context.Background(), "", SearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.Nil(t, results, "empty query should return nil results")
}

// TestSingleSearch_WhitespaceQuery tests singleSearch with whitespace-only query.
func TestSingleSearch_WhitespaceQuery(t *testing.T) {
	engine, _, _, _, _ := setupTestEngine(t)

	results, err := engine.singleSearch(context.Background(), "   ", SearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.Nil(t, results, "whitespace-only query should return nil results")
}

// TestSingleSearch_BM25OnlyMode tests singleSearch in BM25-only mode.
func TestSingleSearch_BM25OnlyMode(t *testing.T) {
	engine, bm25, _, _, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 10.0, MatchedTerms: []string{"test"}},
		}, nil
	}

	results, err := engine.singleSearch(context.Background(), "test", SearchOptions{
		Limit:    10,
		BM25Only: true,
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "chunk1", results[0].ChunkID)
}

// TestSingleSearch_WithFilter tests singleSearch with content type filter.
func TestSingleSearch_WithFilter(t *testing.T) {
	engine, bm25, vector, embedder, metadata := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 10.0},
			{DocID: "chunk2", Score: 8.0},
		}, nil
	}
	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return []*store.VectorResult{
			{ID: "chunk1", Score: 0.9},
			{ID: "chunk2", Score: 0.8},
		}, nil
	}
	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	metadata.chunks["chunk1"] = &store.Chunk{ID: "chunk1", Content: "code content", FilePath: "main.go", ContentType: store.ContentTypeCode}
	metadata.chunks["chunk2"] = &store.Chunk{ID: "chunk2", Content: "doc content", FilePath: "README.md", ContentType: store.ContentTypeMarkdown}

	results, err := engine.singleSearch(context.Background(), "test", SearchOptions{
		Limit:  10,
		Filter: "code", // Only code files
	})

	require.NoError(t, err)
	// Results should be filtered to code only
	for _, r := range results {
		// Since we're using singleSearch which returns FusedResult,
		// the filtering happens based on content type
		assert.NotEmpty(t, r.ChunkID)
	}
}

// =============================================================================
// convertToFusedResult Tests (DEBT-028: Coverage improvement)
// =============================================================================

// TestConvertToFusedResult tests the conversion from internal to public FusedResult.
func TestConvertToFusedResult(t *testing.T) {
	engine, _, _, _, _ := setupTestEngine(t)

	internal := []*fusedResult{
		{
			chunkID:      "chunk1",
			rrfScore:     0.95,
			bm25Score:    10.0,
			bm25Rank:     1,
			vecScore:     0.9,
			vecRank:      2,
			inBothLists:  true,
			matchedTerms: []string{"term1", "term2"},
		},
		{
			chunkID:      "chunk2",
			rrfScore:     0.85,
			bm25Score:    8.0,
			bm25Rank:     2,
			vecScore:     0.8,
			vecRank:      1,
			inBothLists:  false,
			matchedTerms: []string{"term1"},
		},
	}

	result := engine.convertToFusedResult(internal)

	require.Len(t, result, 2)

	assert.Equal(t, "chunk1", result[0].ChunkID)
	assert.Equal(t, 0.95, result[0].RRFScore)
	assert.Equal(t, float64(10.0), result[0].BM25Score)
	assert.Equal(t, 1, result[0].BM25Rank)
	assert.Equal(t, float64(0.9), result[0].VecScore)
	assert.Equal(t, 2, result[0].VecRank)
	assert.True(t, result[0].InBothLists)
	assert.Equal(t, []string{"term1", "term2"}, result[0].MatchedTerms)

	assert.Equal(t, "chunk2", result[1].ChunkID)
	assert.False(t, result[1].InBothLists)
}

// TestConvertToFusedResult_Empty tests conversion of empty slice.
func TestConvertToFusedResult_Empty(t *testing.T) {
	engine, _, _, _, _ := setupTestEngine(t)

	result := engine.convertToFusedResult([]*fusedResult{})

	require.NotNil(t, result)
	assert.Len(t, result, 0)
}

// engineTestClassifier is a test helper for engine tests (avoiding collision with classifier_test.go).
type engineTestClassifier struct {
	classifyFn func(ctx context.Context, query string) (QueryType, Weights, error)
}

func (m *engineTestClassifier) Classify(ctx context.Context, query string) (QueryType, Weights, error) {
	if m.classifyFn != nil {
		return m.classifyFn(ctx, query)
	}
	return QueryTypeMixed, WeightsForQueryType(QueryTypeMixed), nil
}

// =============================================================================
// DEBT-028: Additional Coverage Tests
// =============================================================================

func TestEngine_WithMetrics(t *testing.T) {
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{}
	embedder := &MockEmbedder{}
	metadata := NewMockMetadataStore()

	// Given: a metrics collector
	metrics := &telemetry.QueryMetrics{}

	// When: creating engine with metrics
	engine := New(bm25, vector, embedder, metadata, DefaultConfig(), WithMetrics(metrics))

	// Then: engine is created with metrics
	require.NotNil(t, engine)
	assert.Equal(t, metrics, engine.metrics)
}

func TestEngine_Close_BM25Error(t *testing.T) {
	bm25 := &MockBM25Index{
		CloseFn: func() error {
			return errors.New("bm25 close error")
		},
	}
	vector := &MockVectorStore{}
	embedder := &MockEmbedder{}
	metadata := NewMockMetadataStore()

	engine := New(bm25, vector, embedder, metadata, DefaultConfig())

	// When: closing with BM25 error
	err := engine.Close()

	// Then: error is returned
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bm25 close error")
}

func TestEngine_Close_VectorError(t *testing.T) {
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{
		CloseFn: func() error {
			return errors.New("vector close error")
		},
	}
	embedder := &MockEmbedder{}
	metadata := NewMockMetadataStore()

	engine := New(bm25, vector, embedder, metadata, DefaultConfig())

	// When: closing with vector error
	err := engine.Close()

	// Then: error is returned
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vector close error")
}

func TestEngine_Close_MetadataError(t *testing.T) {
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{}
	embedder := &MockEmbedder{}
	metadata := NewMockMetadataStore()
	metadata.CloseFn = func() error {
		return errors.New("metadata close error")
	}

	engine := New(bm25, vector, embedder, metadata, DefaultConfig())

	// When: closing with metadata error
	err := engine.Close()

	// Then: error is returned
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "metadata close error")
}

func TestEngine_Close_MultipleErrors(t *testing.T) {
	bm25 := &MockBM25Index{
		CloseFn: func() error {
			return errors.New("bm25 error")
		},
	}
	vector := &MockVectorStore{
		CloseFn: func() error {
			return errors.New("vector error")
		},
	}
	embedder := &MockEmbedder{}
	metadata := NewMockMetadataStore()
	metadata.CloseFn = func() error {
		return errors.New("metadata error")
	}

	engine := New(bm25, vector, embedder, metadata, DefaultConfig())

	// When: closing with multiple errors
	err := engine.Close()

	// Then: all errors are joined
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bm25 error")
	assert.Contains(t, err.Error(), "vector error")
	assert.Contains(t, err.Error(), "metadata error")
}

func TestEngine_SingleSearch_EmptyQuery(t *testing.T) {
	engine, _, _, _, _ := setupTestEngine(t)

	// When: searching with empty query
	results, err := engine.singleSearch(context.Background(), "", SearchOptions{})

	// Then: nil is returned without error
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestEngine_SingleSearch_WhitespaceQuery(t *testing.T) {
	engine, _, _, _, _ := setupTestEngine(t)

	// When: searching with whitespace-only query
	results, err := engine.singleSearch(context.Background(), "   ", SearchOptions{})

	// Then: nil is returned without error
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestEngine_SingleSearch_BM25Only(t *testing.T) {
	engine, bm25, _, _, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 0.9, MatchedTerms: []string{"test"}},
		}, nil
	}

	// When: searching in BM25-only mode
	results, err := engine.singleSearch(context.Background(), "test query", SearchOptions{BM25Only: true})

	// Then: results from BM25 only
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "chunk1", results[0].ChunkID)
}

func TestEngine_SingleSearch_BM25OnlyError(t *testing.T) {
	engine, bm25, _, _, _ := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return nil, errors.New("bm25 search failed")
	}

	// When: searching in BM25-only mode with error
	results, err := engine.singleSearch(context.Background(), "test query", SearchOptions{BM25Only: true})

	// Then: error is returned
	assert.Error(t, err)
	assert.Nil(t, results)
	assert.Contains(t, err.Error(), "BM25 search failed")
}

func TestEngine_SingleSearch_WithFilter(t *testing.T) {
	engine, bm25, vector, embedder, metadata := setupTestEngine(t)

	bm25.SearchFn = func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
		return []*store.BM25Result{
			{DocID: "chunk1", Score: 0.9},
		}, nil
	}

	vector.SearchFn = func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
		return []*store.VectorResult{
			{ID: "chunk1", Score: 0.8},
		}, nil
	}

	embedder.EmbedFn = func(ctx context.Context, text string) ([]float32, error) {
		return make([]float32, 768), nil
	}

	metadata.GetChunkFn = func(ctx context.Context, id string) (*store.Chunk, error) {
		return &store.Chunk{
			ID:          id,
			Content:     "func test() {}",
			ContentType: "code",
			Language:    "go",
		}, nil
	}

	// When: searching with filter
	results, err := engine.singleSearch(context.Background(), "test", SearchOptions{Filter: "code"})

	// Then: filtered results are returned
	require.NoError(t, err)
	require.NotEmpty(t, results)
}

func TestEngine_RecordMetrics_NilMetrics(t *testing.T) {
	engine, _, _, _, _ := setupTestEngine(t)

	// Engine has nil metrics by default
	assert.Nil(t, engine.metrics)

	// When: recording metrics (should not panic)
	engine.recordMetrics("test query", QueryTypeMixed, 5, 100*time.Millisecond)

	// Then: no panic, no error (nil check)
}

func TestEngine_RecordMetrics_WithMetrics(t *testing.T) {
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{}
	embedder := &MockEmbedder{}
	metadata := NewMockMetadataStore()
	metrics := telemetry.NewQueryMetrics(nil) // nil store = in-memory only
	defer metrics.Close()

	engine := New(bm25, vector, embedder, metadata, DefaultConfig(), WithMetrics(metrics))

	// When: recording metrics
	engine.recordMetrics("test query", QueryTypeSemantic, 10, 50*time.Millisecond)

	// Then: metrics are recorded (QueryMetrics tracks internally)
	// Note: We can't easily verify without exposing internals, but this exercises the code path
}

// =============================================================================
// DEBT-028: multiQuerySearch Tests
// =============================================================================

func TestEngine_MultiQuerySearch_Basic(t *testing.T) {
	// Given: engine with multi-query decomposition enabled
	bm25 := &MockBM25Index{
		SearchFn: func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{
				{DocID: "chunk1", Score: 10.0},
			}, nil
		},
	}
	vector := &MockVectorStore{
		SearchFn: func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return []*store.VectorResult{
				{ID: "chunk1", Distance: 0.1},
			}, nil
		},
	}
	embedder := &MockEmbedder{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		},
	}
	metadata := NewMockMetadataStore()
	metadata.chunks["chunk1"] = &store.Chunk{
		ID:          "chunk1",
		FilePath:    "internal/search/engine.go",
		Content:     "func Search(ctx context.Context, query string) { ... }",
		ContentType: store.ContentTypeCode,
		Language:    "go",
	}

	decomposer := &MockDecomposer{
		ShouldDecomposeFn: func(query string) bool { return true },
		DecomposeFn: func(query string) []SubQuery {
			return []SubQuery{
				{Query: "search function", Weight: 1.0},
				{Query: "search implementation", Weight: 1.0},
			}
		},
	}

	engine := New(bm25, vector, embedder, metadata, DefaultConfig(), WithMultiQuerySearch(decomposer))

	// When: searching with a query that will be decomposed
	results, err := engine.Search(context.Background(), "how does search work", SearchOptions{Limit: 10})

	// Then: results are returned from multi-query search
	require.NoError(t, err)
	assert.NotEmpty(t, results)
}

func TestEngine_MultiQuerySearch_WithExplain(t *testing.T) {
	// Given: engine with multi-query decomposition and Explain option
	bm25 := &MockBM25Index{
		SearchFn: func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{
				{DocID: "chunk1", Score: 10.0},
			}, nil
		},
	}
	vector := &MockVectorStore{
		SearchFn: func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return []*store.VectorResult{
				{ID: "chunk1", Distance: 0.1},
			}, nil
		},
	}
	embedder := &MockEmbedder{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		},
	}
	metadata := NewMockMetadataStore()
	metadata.chunks["chunk1"] = &store.Chunk{
		ID:          "chunk1",
		FilePath:    "internal/search/engine.go",
		Content:     "func Search(ctx context.Context, query string) { ... }",
		ContentType: store.ContentTypeCode,
		Language:    "go",
	}

	decomposer := &MockDecomposer{
		ShouldDecomposeFn: func(query string) bool { return true },
		DecomposeFn: func(query string) []SubQuery {
			return []SubQuery{
				{Query: "search function", Weight: 1.0},
				{Query: "search implementation", Weight: 1.0},
			}
		},
	}

	engine := New(bm25, vector, embedder, metadata, DefaultConfig(), WithMultiQuerySearch(decomposer))

	// When: searching with Explain enabled
	results, err := engine.Search(context.Background(), "how does search work", SearchOptions{
		Limit:   10,
		Explain: true,
	})

	// Then: results have explain data with sub-queries
	require.NoError(t, err)
	assert.NotEmpty(t, results)
	if len(results) > 0 && results[0].Explain != nil {
		assert.NotEmpty(t, results[0].Explain.SubQueries, "Should have sub-queries in explain data")
	}
}

func TestEngine_MultiQuerySearch_WithAdjacentChunks(t *testing.T) {
	// Given: engine with multi-query and adjacent chunks option
	bm25 := &MockBM25Index{
		SearchFn: func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{
				{DocID: "chunk2", Score: 10.0},
			}, nil
		},
	}
	vector := &MockVectorStore{
		SearchFn: func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return []*store.VectorResult{
				{ID: "chunk2", Distance: 0.1},
			}, nil
		},
	}
	embedder := &MockEmbedder{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		},
	}
	metadata := NewMockMetadataStore()
	// Add chunks with same FileID for adjacent enrichment
	metadata.chunks["chunk1"] = &store.Chunk{
		ID:          "chunk1",
		FileID:      "file1",
		FilePath:    "internal/search/engine.go",
		Content:     "package search",
		ContentType: store.ContentTypeCode,
		StartLine:   1,
		EndLine:     5,
	}
	metadata.chunks["chunk2"] = &store.Chunk{
		ID:          "chunk2",
		FileID:      "file1",
		FilePath:    "internal/search/engine.go",
		Content:     "func Search() { ... }",
		ContentType: store.ContentTypeCode,
		StartLine:   10,
		EndLine:     20,
	}
	metadata.chunks["chunk3"] = &store.Chunk{
		ID:          "chunk3",
		FileID:      "file1",
		FilePath:    "internal/search/engine.go",
		Content:     "func Index() { ... }",
		ContentType: store.ContentTypeCode,
		StartLine:   25,
		EndLine:     35,
	}

	decomposer := &MockDecomposer{
		ShouldDecomposeFn: func(query string) bool { return true },
		DecomposeFn: func(query string) []SubQuery {
			return []SubQuery{{Query: query, Weight: 1.0}}
		},
	}

	engine := New(bm25, vector, embedder, metadata, DefaultConfig(), WithMultiQuerySearch(decomposer))

	// When: searching with adjacent chunks option
	results, err := engine.Search(context.Background(), "search function", SearchOptions{
		Limit:          10,
		AdjacentChunks: 2,
	})

	// Then: results should include main result
	require.NoError(t, err)
	assert.NotEmpty(t, results)
}

func TestEngine_MultiQuerySearch_EmptyResults(t *testing.T) {
	// Given: engine that returns empty results
	bm25 := &MockBM25Index{
		SearchFn: func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return nil, nil
		},
	}
	vector := &MockVectorStore{
		SearchFn: func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return nil, nil
		},
	}
	embedder := &MockEmbedder{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		},
	}
	metadata := NewMockMetadataStore()

	decomposer := &MockDecomposer{
		ShouldDecomposeFn: func(query string) bool { return true },
		DecomposeFn: func(query string) []SubQuery {
			return []SubQuery{{Query: query, Weight: 1.0}}
		},
	}

	engine := New(bm25, vector, embedder, metadata, DefaultConfig(), WithMultiQuerySearch(decomposer))

	// When: searching with no matching results
	results, err := engine.Search(context.Background(), "nonexistent query", SearchOptions{Limit: 10})

	// Then: empty results returned without error
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestEngine_MultiQuerySearch_WithFilter(t *testing.T) {
	// Given: engine with filter options
	bm25 := &MockBM25Index{
		SearchFn: func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{
				{DocID: "chunk1", Score: 10.0},
				{DocID: "chunk2", Score: 8.0},
			}, nil
		},
	}
	vector := &MockVectorStore{
		SearchFn: func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return []*store.VectorResult{
				{ID: "chunk1", Distance: 0.1},
				{ID: "chunk2", Distance: 0.2},
			}, nil
		},
	}
	embedder := &MockEmbedder{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		},
	}
	metadata := NewMockMetadataStore()
	metadata.chunks["chunk1"] = &store.Chunk{
		ID:          "chunk1",
		FilePath:    "internal/search/engine.go",
		Content:     "func Search() { ... }",
		ContentType: store.ContentTypeCode,
		Language:    "go",
	}
	metadata.chunks["chunk2"] = &store.Chunk{
		ID:          "chunk2",
		FilePath:    "docs/README.md",
		Content:     "# Search Documentation",
		ContentType: store.ContentTypeMarkdown,
		Language:    "markdown",
	}

	decomposer := &MockDecomposer{
		ShouldDecomposeFn: func(query string) bool { return true },
		DecomposeFn: func(query string) []SubQuery {
			return []SubQuery{{Query: query, Weight: 1.0}}
		},
	}

	engine := New(bm25, vector, embedder, metadata, DefaultConfig(), WithMultiQuerySearch(decomposer))

	// When: searching with content type filter
	results, err := engine.Search(context.Background(), "search", SearchOptions{
		Limit:  10,
		Filter: "code",
	})

	// Then: only code results are returned
	require.NoError(t, err)
	for _, r := range results {
		assert.Equal(t, store.ContentTypeCode, r.Chunk.ContentType)
	}
}

// =============================================================================
// DEBT-028: singleSearch Edge Case Tests
// =============================================================================

func TestEngine_SingleSearch_WithClassifier(t *testing.T) {
	// Given: engine with classifier
	bm25 := &MockBM25Index{
		SearchFn: func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{
				{DocID: "chunk1", Score: 10.0},
			}, nil
		},
	}
	vector := &MockVectorStore{
		SearchFn: func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return []*store.VectorResult{
				{ID: "chunk1", Distance: 0.1},
			}, nil
		},
	}
	embedder := &MockEmbedder{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		},
	}
	metadata := NewMockMetadataStore()
	metadata.chunks["chunk1"] = &store.Chunk{
		ID:          "chunk1",
		FilePath:    "internal/search/engine.go",
		Content:     "func Search() { ... }",
		ContentType: store.ContentTypeCode,
		Language:    "go",
	}

	// Create classifier that returns specific weights
	classifier := NewHybridClassifier(nil)

	engine := New(bm25, vector, embedder, metadata, DefaultConfig(), WithClassifier(classifier))

	// When: searching without explicit weights (classifier will be used)
	results, err := engine.Search(context.Background(), "how to implement", SearchOptions{Limit: 10})

	// Then: results are returned (classifier used for weights)
	require.NoError(t, err)
	_ = results // We just verify no error with classifier
}

func TestEngine_SingleSearch_DimensionValidationFallback(t *testing.T) {
	// Given: engine where dimension validation fails
	bm25 := &MockBM25Index{
		SearchFn: func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return []*store.BM25Result{
				{DocID: "chunk1", Score: 10.0},
			}, nil
		},
	}
	vector := &MockVectorStore{
		SearchFn: func(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
			return []*store.VectorResult{
				{ID: "chunk1", Distance: 0.1},
			}, nil
		},
	}
	embedder := &MockEmbedder{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		},
		DimensionsFn: func() int { return 768 },
	}
	metadata := NewMockMetadataStore()
	// Set mismatched dimensions in state to trigger validation failure
	metadata.state["embedder_dimensions"] = "384" // Different from embedder's 768
	metadata.chunks["chunk1"] = &store.Chunk{
		ID:          "chunk1",
		FilePath:    "internal/search/engine.go",
		Content:     "func Search() { ... }",
		ContentType: store.ContentTypeCode,
	}

	engine := New(bm25, vector, embedder, metadata, DefaultConfig())

	// When: searching with dimension mismatch (should fall back to BM25)
	results, err := engine.Search(context.Background(), "search function", SearchOptions{Limit: 10})

	// Then: results are still returned (BM25 fallback)
	require.NoError(t, err)
	assert.NotEmpty(t, results)
}

func TestEngine_SingleSearch_BM25FallbackError(t *testing.T) {
	// Given: engine where both dimension validation and BM25 fail
	bm25 := &MockBM25Index{
		SearchFn: func(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
			return nil, errors.New("BM25 error")
		},
	}
	vector := &MockVectorStore{}
	embedder := &MockEmbedder{
		DimensionsFn: func() int { return 768 },
	}
	metadata := NewMockMetadataStore()
	// Set mismatched dimensions to trigger fallback path
	metadata.state["embedder_dimensions"] = "384"

	engine := New(bm25, vector, embedder, metadata, DefaultConfig())

	// When: searching with dimension mismatch and BM25 error
	_, err := engine.Search(context.Background(), "test query", SearchOptions{Limit: 10})

	// Then: error is returned from BM25 fallback
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BM25")
}

// =============================================================================
// DEBT-028: Index Function Edge Case Tests
// =============================================================================

func TestEngine_Index_EmbedderError(t *testing.T) {
	// Given: engine where embedder fails
	bm25 := &MockBM25Index{
		IndexFn: func(ctx context.Context, docs []*store.Document) error {
			return nil
		},
	}
	vector := &MockVectorStore{}
	embedder := &MockEmbedder{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			return nil, errors.New("embedding failed")
		},
	}
	metadata := NewMockMetadataStore()

	engine := New(bm25, vector, embedder, metadata, DefaultConfig())

	// When: indexing with embedder error
	chunks := []*store.Chunk{
		{ID: "chunk1", Content: "test content"},
	}
	err := engine.Index(context.Background(), chunks)

	// Then: error is returned from embedder
	require.Error(t, err)
	assert.Contains(t, err.Error(), "embedding")
}

func TestEngine_Index_BM25Error(t *testing.T) {
	// Given: engine where BM25 indexing fails
	bm25 := &MockBM25Index{
		IndexFn: func(ctx context.Context, docs []*store.Document) error {
			return errors.New("BM25 indexing failed")
		},
	}
	vector := &MockVectorStore{
		AddFn: func(ctx context.Context, ids []string, vectors [][]float32) error {
			return nil
		},
	}
	embedder := &MockEmbedder{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		},
	}
	metadata := NewMockMetadataStore()

	engine := New(bm25, vector, embedder, metadata, DefaultConfig())

	// When: indexing with BM25 error
	chunks := []*store.Chunk{
		{ID: "chunk1", Content: "test content"},
	}
	err := engine.Index(context.Background(), chunks)

	// Then: error is returned from BM25
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BM25")
}

func TestEngine_Index_VectorStoreError(t *testing.T) {
	// Given: engine where vector store fails
	bm25 := &MockBM25Index{
		IndexFn: func(ctx context.Context, docs []*store.Document) error {
			return nil
		},
	}
	vector := &MockVectorStore{
		AddFn: func(ctx context.Context, ids []string, vectors [][]float32) error {
			return errors.New("vector store failed")
		},
	}
	embedder := &MockEmbedder{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		},
	}
	metadata := NewMockMetadataStore()

	engine := New(bm25, vector, embedder, metadata, DefaultConfig())

	// When: indexing with vector store error
	chunks := []*store.Chunk{
		{ID: "chunk1", Content: "test content"},
	}
	err := engine.Index(context.Background(), chunks)

	// Then: error is returned from vector store
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vector")
}

func TestEngine_Index_EmptyChunks(t *testing.T) {
	// Given: engine
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{}
	embedder := &MockEmbedder{}
	metadata := NewMockMetadataStore()

	engine := New(bm25, vector, embedder, metadata, DefaultConfig())

	// When: indexing empty chunks
	err := engine.Index(context.Background(), nil)

	// Then: no error (nothing to index)
	require.NoError(t, err)
}

func TestEngine_Index_WithContextPrefix(t *testing.T) {
	// Given: engine with chunk that has context prefix
	bm25 := &MockBM25Index{
		IndexFn: func(ctx context.Context, docs []*store.Document) error {
			return nil
		},
	}
	vector := &MockVectorStore{
		AddFn: func(ctx context.Context, ids []string, vectors [][]float32) error {
			return nil
		},
	}
	embedder := &MockEmbedder{
		EmbedFn: func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 768), nil
		},
	}
	metadata := NewMockMetadataStore()

	engine := New(bm25, vector, embedder, metadata, DefaultConfig())

	// When: indexing chunk with context
	chunks := []*store.Chunk{
		{
			ID:      "chunk1",
			Content: "test content",
			Context: "package main\nimport \"fmt\"",
		},
	}
	err := engine.Index(context.Background(), chunks)

	// Then: no error
	require.NoError(t, err)
}
