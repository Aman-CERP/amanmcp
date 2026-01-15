package index

import (
	"context"
	"testing"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

// MockMetadataForConsistency implements minimal MetadataStore for consistency tests.
type MockMetadataForConsistency struct {
	Embeddings map[string][]float32
}

func (m *MockMetadataForConsistency) SaveProject(ctx context.Context, project *store.Project) error {
	return nil
}
func (m *MockMetadataForConsistency) GetProject(ctx context.Context, id string) (*store.Project, error) {
	return nil, nil
}
func (m *MockMetadataForConsistency) UpdateProjectStats(ctx context.Context, id string, fileCount, chunkCount int) error {
	return nil
}
func (m *MockMetadataForConsistency) RefreshProjectStats(ctx context.Context, id string) error {
	return nil
}
func (m *MockMetadataForConsistency) SaveFiles(ctx context.Context, files []*store.File) error {
	return nil
}
func (m *MockMetadataForConsistency) GetFileByPath(ctx context.Context, projectID, path string) (*store.File, error) {
	return nil, nil
}
func (m *MockMetadataForConsistency) GetChangedFiles(ctx context.Context, projectID string, since time.Time) ([]*store.File, error) {
	return nil, nil
}
func (m *MockMetadataForConsistency) ListFiles(ctx context.Context, projectID string, cursor string, limit int) ([]*store.File, string, error) {
	return nil, "", nil
}
func (m *MockMetadataForConsistency) GetFilePathsByProject(ctx context.Context, projectID string) ([]string, error) {
	return nil, nil
}
func (m *MockMetadataForConsistency) GetFilesForReconciliation(ctx context.Context, projectID string) (map[string]*store.File, error) {
	return nil, nil
}
func (m *MockMetadataForConsistency) ListFilePathsUnder(ctx context.Context, projectID, dirPrefix string) ([]string, error) {
	return nil, nil
}
func (m *MockMetadataForConsistency) DeleteFile(ctx context.Context, fileID string) error {
	return nil
}
func (m *MockMetadataForConsistency) DeleteFilesByProject(ctx context.Context, projectID string) error {
	return nil
}
func (m *MockMetadataForConsistency) SaveChunks(ctx context.Context, chunks []*store.Chunk) error {
	return nil
}
func (m *MockMetadataForConsistency) GetChunk(ctx context.Context, id string) (*store.Chunk, error) {
	return nil, nil
}
func (m *MockMetadataForConsistency) GetChunks(ctx context.Context, ids []string) ([]*store.Chunk, error) {
	return nil, nil
}
func (m *MockMetadataForConsistency) GetChunksByFile(ctx context.Context, fileID string) ([]*store.Chunk, error) {
	return nil, nil
}
func (m *MockMetadataForConsistency) DeleteChunks(ctx context.Context, ids []string) error {
	return nil
}
func (m *MockMetadataForConsistency) DeleteChunksByFile(ctx context.Context, fileID string) error {
	return nil
}
func (m *MockMetadataForConsistency) SearchSymbols(ctx context.Context, name string, limit int) ([]*store.Symbol, error) {
	return nil, nil
}
func (m *MockMetadataForConsistency) GetState(ctx context.Context, key string) (string, error) {
	return "", nil
}
func (m *MockMetadataForConsistency) SetState(ctx context.Context, key, value string) error {
	return nil
}
func (m *MockMetadataForConsistency) SaveChunkEmbeddings(ctx context.Context, chunkIDs []string, embeddings [][]float32, model string) error {
	return nil
}
func (m *MockMetadataForConsistency) GetAllEmbeddings(ctx context.Context) (map[string][]float32, error) {
	return m.Embeddings, nil
}
func (m *MockMetadataForConsistency) GetEmbeddingStats(ctx context.Context) (int, int, error) {
	return len(m.Embeddings), 0, nil
}
func (m *MockMetadataForConsistency) SaveIndexCheckpoint(ctx context.Context, stage string, total, embeddedCount int, embedderModel string) error {
	return nil
}
func (m *MockMetadataForConsistency) LoadIndexCheckpoint(ctx context.Context) (*store.IndexCheckpoint, error) {
	return nil, nil
}
func (m *MockMetadataForConsistency) ClearIndexCheckpoint(ctx context.Context) error {
	return nil
}
func (m *MockMetadataForConsistency) Close() error {
	return nil
}

// MockBM25ForConsistency implements minimal BM25Index for consistency tests.
type MockBM25ForConsistency struct {
	IDs          []string
	DeleteCalled bool
	DeletedIDs   []string
}

func (m *MockBM25ForConsistency) Index(ctx context.Context, docs []*store.Document) error {
	return nil
}
func (m *MockBM25ForConsistency) Search(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
	return nil, nil
}
func (m *MockBM25ForConsistency) Delete(ctx context.Context, docIDs []string) error {
	m.DeleteCalled = true
	m.DeletedIDs = append(m.DeletedIDs, docIDs...)
	return nil
}
func (m *MockBM25ForConsistency) AllIDs() ([]string, error) {
	return m.IDs, nil
}
func (m *MockBM25ForConsistency) Stats() *store.IndexStats {
	return &store.IndexStats{DocumentCount: len(m.IDs)}
}
func (m *MockBM25ForConsistency) Save(path string) error {
	return nil
}
func (m *MockBM25ForConsistency) Load(path string) error {
	return nil
}
func (m *MockBM25ForConsistency) Close() error {
	return nil
}

// MockVectorForConsistency implements minimal VectorStore for consistency tests.
type MockVectorForConsistency struct {
	IDs          []string
	DeleteCalled bool
	DeletedIDs   []string
}

func (m *MockVectorForConsistency) Add(ctx context.Context, ids []string, vectors [][]float32) error {
	return nil
}
func (m *MockVectorForConsistency) Search(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
	return nil, nil
}
func (m *MockVectorForConsistency) Delete(ctx context.Context, ids []string) error {
	m.DeleteCalled = true
	m.DeletedIDs = append(m.DeletedIDs, ids...)
	return nil
}
func (m *MockVectorForConsistency) AllIDs() []string {
	return m.IDs
}
func (m *MockVectorForConsistency) Contains(id string) bool {
	for _, i := range m.IDs {
		if i == id {
			return true
		}
	}
	return false
}
func (m *MockVectorForConsistency) Count() int {
	return len(m.IDs)
}
func (m *MockVectorForConsistency) Save(path string) error {
	return nil
}
func (m *MockVectorForConsistency) Load(path string) error {
	return nil
}
func (m *MockVectorForConsistency) Close() error {
	return nil
}

func TestConsistencyChecker_AllConsistent(t *testing.T) {
	// All stores have the same IDs
	metadata := &MockMetadataForConsistency{
		Embeddings: map[string][]float32{
			"chunk1": {0.1, 0.2},
			"chunk2": {0.3, 0.4},
		},
	}
	bm25 := &MockBM25ForConsistency{IDs: []string{"chunk1", "chunk2"}}
	vector := &MockVectorForConsistency{IDs: []string{"chunk1", "chunk2"}}

	checker := NewConsistencyChecker(metadata, bm25, vector)
	result, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}

	if len(result.Inconsistencies) != 0 {
		t.Errorf("Expected 0 inconsistencies, got %d: %+v", len(result.Inconsistencies), result.Inconsistencies)
	}
	if result.Checked != 2 {
		t.Errorf("Expected 2 checked, got %d", result.Checked)
	}
}

func TestConsistencyChecker_OrphanInBM25(t *testing.T) {
	// BM25 has an extra ID not in metadata
	metadata := &MockMetadataForConsistency{
		Embeddings: map[string][]float32{
			"chunk1": {0.1, 0.2},
		},
	}
	bm25 := &MockBM25ForConsistency{IDs: []string{"chunk1", "orphan_bm25"}}
	vector := &MockVectorForConsistency{IDs: []string{"chunk1"}}

	checker := NewConsistencyChecker(metadata, bm25, vector)
	result, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}

	if len(result.Inconsistencies) != 1 {
		t.Errorf("Expected 1 inconsistency, got %d", len(result.Inconsistencies))
	}
	if result.Inconsistencies[0].Type != InconsistencyOrphanBM25 {
		t.Errorf("Expected OrphanBM25, got %v", result.Inconsistencies[0].Type)
	}
	if result.Inconsistencies[0].ChunkID != "orphan_bm25" {
		t.Errorf("Expected orphan_bm25, got %s", result.Inconsistencies[0].ChunkID)
	}
}

func TestConsistencyChecker_OrphanInVector(t *testing.T) {
	// Vector has an extra ID not in metadata
	metadata := &MockMetadataForConsistency{
		Embeddings: map[string][]float32{
			"chunk1": {0.1, 0.2},
		},
	}
	bm25 := &MockBM25ForConsistency{IDs: []string{"chunk1"}}
	vector := &MockVectorForConsistency{IDs: []string{"chunk1", "orphan_vector"}}

	checker := NewConsistencyChecker(metadata, bm25, vector)
	result, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}

	if len(result.Inconsistencies) != 1 {
		t.Errorf("Expected 1 inconsistency, got %d", len(result.Inconsistencies))
	}
	if result.Inconsistencies[0].Type != InconsistencyOrphanVector {
		t.Errorf("Expected OrphanVector, got %v", result.Inconsistencies[0].Type)
	}
}

func TestConsistencyChecker_MissingFromBM25(t *testing.T) {
	// Metadata has an ID not in BM25
	metadata := &MockMetadataForConsistency{
		Embeddings: map[string][]float32{
			"chunk1":  {0.1, 0.2},
			"missing": {0.3, 0.4},
		},
	}
	bm25 := &MockBM25ForConsistency{IDs: []string{"chunk1"}}
	vector := &MockVectorForConsistency{IDs: []string{"chunk1", "missing"}}

	checker := NewConsistencyChecker(metadata, bm25, vector)
	result, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}

	// Should find missing from BM25
	found := false
	for _, issue := range result.Inconsistencies {
		if issue.Type == InconsistencyMissingBM25 && issue.ChunkID == "missing" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected to find MissingBM25 for 'missing', got %+v", result.Inconsistencies)
	}
}

func TestConsistencyChecker_Repair(t *testing.T) {
	metadata := &MockMetadataForConsistency{Embeddings: map[string][]float32{}}
	bm25 := &MockBM25ForConsistency{}
	vector := &MockVectorForConsistency{}

	checker := NewConsistencyChecker(metadata, bm25, vector)

	issues := []Inconsistency{
		{Type: InconsistencyOrphanBM25, ChunkID: "orphan1"},
		{Type: InconsistencyOrphanBM25, ChunkID: "orphan2"},
		{Type: InconsistencyOrphanVector, ChunkID: "orphan3"},
		{Type: InconsistencyMissingBM25, ChunkID: "missing1"},
	}

	err := checker.Repair(context.Background(), issues)
	if err != nil {
		t.Fatalf("Repair() error: %v", err)
	}

	// Verify BM25 orphans were deleted
	if !bm25.DeleteCalled {
		t.Error("Expected BM25 Delete to be called")
	}
	if len(bm25.DeletedIDs) != 2 {
		t.Errorf("Expected 2 BM25 deletions, got %d", len(bm25.DeletedIDs))
	}

	// Verify Vector orphans were deleted
	if !vector.DeleteCalled {
		t.Error("Expected Vector Delete to be called")
	}
	if len(vector.DeletedIDs) != 1 {
		t.Errorf("Expected 1 Vector deletion, got %d", len(vector.DeletedIDs))
	}
}

func TestConsistencyChecker_QuickCheck(t *testing.T) {
	tests := []struct {
		name           string
		metadataCount  int
		bm25Count      int
		vectorCount    int
		wantConsistent bool
	}{
		{
			name:           "all_consistent",
			metadataCount:  10,
			bm25Count:      10,
			vectorCount:    10,
			wantConsistent: true,
		},
		{
			name:           "bm25_mismatch",
			metadataCount:  10,
			bm25Count:      8,
			vectorCount:    10,
			wantConsistent: false,
		},
		{
			name:           "vector_mismatch",
			metadataCount:  10,
			bm25Count:      10,
			vectorCount:    12,
			wantConsistent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create metadata with specified count
			embeddings := make(map[string][]float32)
			for i := 0; i < tt.metadataCount; i++ {
				embeddings[string(rune('a'+i))] = []float32{0.1}
			}
			metadata := &MockMetadataForConsistency{Embeddings: embeddings}

			// Create BM25 with specified count
			bm25IDs := make([]string, tt.bm25Count)
			for i := 0; i < tt.bm25Count; i++ {
				bm25IDs[i] = string(rune('a' + i))
			}
			bm25 := &MockBM25ForConsistency{IDs: bm25IDs}

			// Create Vector with specified count
			vectorIDs := make([]string, tt.vectorCount)
			for i := 0; i < tt.vectorCount; i++ {
				vectorIDs[i] = string(rune('a' + i))
			}
			vector := &MockVectorForConsistency{IDs: vectorIDs}

			checker := NewConsistencyChecker(metadata, bm25, vector)
			consistent, err := checker.QuickCheck(context.Background())
			if err != nil {
				t.Fatalf("QuickCheck() error: %v", err)
			}

			if consistent != tt.wantConsistent {
				t.Errorf("QuickCheck() = %v, want %v", consistent, tt.wantConsistent)
			}
		})
	}
}

func TestInconsistencyType_String(t *testing.T) {
	tests := []struct {
		t    InconsistencyType
		want string
	}{
		{InconsistencyOrphanBM25, "orphan_bm25"},
		{InconsistencyOrphanVector, "orphan_vector"},
		{InconsistencyMissingBM25, "missing_bm25"},
		{InconsistencyMissingVector, "missing_vector"},
		{InconsistencyType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.t.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
