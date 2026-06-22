package index

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/chunk"
	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/graph"
	"github.com/Aman-CERP/amanmcp/internal/store"
	"github.com/Aman-CERP/amanmcp/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockRenderer implements ui.Renderer for testing.
type MockRenderer struct {
	StartCalled       bool
	StopCalled        bool
	CompleteCalled    bool
	ProgressEvents    []ui.ProgressEvent
	ErrorEvents       []ui.ErrorEvent
	CompletionStats   ui.CompletionStats
	StartError        error
	ShouldFailOnStart bool
}

func (m *MockRenderer) Start(ctx context.Context) error {
	m.StartCalled = true
	if m.ShouldFailOnStart {
		return m.StartError
	}
	return nil
}

func (m *MockRenderer) UpdateProgress(event ui.ProgressEvent) {
	m.ProgressEvents = append(m.ProgressEvents, event)
}

func (m *MockRenderer) AddError(event ui.ErrorEvent) {
	m.ErrorEvents = append(m.ErrorEvents, event)
}

func (m *MockRenderer) Complete(stats ui.CompletionStats) {
	m.CompleteCalled = true
	m.CompletionStats = stats
}

func (m *MockRenderer) Stop() error {
	m.StopCalled = true
	return nil
}

// MockMetadataStore implements store.MetadataStore for testing.
type MockMetadataStore struct {
	SaveProjectCalled     bool
	SaveFilesCalled       bool
	SaveChunksCalled      bool
	SaveEmbeddingsCalled  bool
	UpdateStatsCalled     bool
	ClearCheckpointCalled bool
	SetStateCalled        bool

	ProjectSaved    *store.Project
	FilesSaved      []*store.File
	ChunksSaved     []*store.Chunk
	EmbeddingsSaved map[string][]float32

	AllEmbeddings    map[string][]float32
	CheckpointToLoad *store.IndexCheckpoint

	// BUG-042: Track SetState calls for dimension/model verification
	StateValues map[string]string

	SaveProjectError      error
	SaveFilesError        error
	SaveChunksError       error
	SaveEmbeddingsError   error
	GetAllEmbeddingsError error
}

func (m *MockMetadataStore) SaveProject(ctx context.Context, project *store.Project) error {
	m.SaveProjectCalled = true
	m.ProjectSaved = project
	return m.SaveProjectError
}

func (m *MockMetadataStore) GetProject(ctx context.Context, id string) (*store.Project, error) {
	return nil, nil
}

func (m *MockMetadataStore) UpdateProjectStats(ctx context.Context, id string, fileCount, chunkCount int) error {
	m.UpdateStatsCalled = true
	return nil
}

func (m *MockMetadataStore) RefreshProjectStats(ctx context.Context, id string) error {
	return nil
}

func (m *MockMetadataStore) SaveFiles(ctx context.Context, files []*store.File) error {
	m.SaveFilesCalled = true
	m.FilesSaved = files
	return m.SaveFilesError
}

func (m *MockMetadataStore) GetFileByPath(ctx context.Context, projectID, path string) (*store.File, error) {
	return nil, nil
}

func (m *MockMetadataStore) GetChangedFiles(ctx context.Context, projectID string, since time.Time) ([]*store.File, error) {
	return nil, nil
}

func (m *MockMetadataStore) ListFiles(ctx context.Context, projectID string, cursor string, limit int) ([]*store.File, string, error) {
	return nil, "", nil
}

func (m *MockMetadataStore) GetFilePathsByProject(ctx context.Context, projectID string) ([]string, error) {
	return nil, nil
}

func (m *MockMetadataStore) GetFilesForReconciliation(ctx context.Context, projectID string) (map[string]*store.File, error) {
	return nil, nil
}

func (m *MockMetadataStore) ListFilePathsUnder(ctx context.Context, projectID, dirPrefix string) ([]string, error) {
	return nil, nil
}

func (m *MockMetadataStore) DeleteFile(ctx context.Context, fileID string) error {
	return nil
}

func (m *MockMetadataStore) DeleteFilesByProject(ctx context.Context, projectID string) error {
	return nil
}

func (m *MockMetadataStore) SaveChunks(ctx context.Context, chunks []*store.Chunk) error {
	m.SaveChunksCalled = true
	m.ChunksSaved = chunks
	return m.SaveChunksError
}

func (m *MockMetadataStore) GetChunk(ctx context.Context, id string) (*store.Chunk, error) {
	return nil, nil
}

func (m *MockMetadataStore) GetChunks(ctx context.Context, ids []string) ([]*store.Chunk, error) {
	return nil, nil
}

func (m *MockMetadataStore) GetChunksByFile(ctx context.Context, fileID string) ([]*store.Chunk, error) {
	return nil, nil
}

func (m *MockMetadataStore) GetChunksByPath(ctx context.Context, path string, limit int) ([]*store.Chunk, error) {
	return nil, nil
}

func (m *MockMetadataStore) GetChunksBySymbol(ctx context.Context, name string, limit int) ([]*store.Chunk, error) {
	return nil, nil
}

func (m *MockMetadataStore) DeleteChunks(ctx context.Context, ids []string) error {
	return nil
}

func (m *MockMetadataStore) DeleteChunksByFile(ctx context.Context, fileID string) error {
	return nil
}

func (m *MockMetadataStore) SearchSymbols(ctx context.Context, name string, limit int) ([]*store.Symbol, error) {
	return nil, nil
}

func (m *MockMetadataStore) GetState(ctx context.Context, key string) (string, error) {
	return "", nil
}

func (m *MockMetadataStore) SetState(ctx context.Context, key, value string) error {
	m.SetStateCalled = true
	// BUG-042: Track state values for verification
	if m.StateValues == nil {
		m.StateValues = make(map[string]string)
	}
	m.StateValues[key] = value
	return nil
}

func (m *MockMetadataStore) SaveChunkEmbeddings(ctx context.Context, chunkIDs []string, embeddings [][]float32, model string) error {
	m.SaveEmbeddingsCalled = true
	if m.EmbeddingsSaved == nil {
		m.EmbeddingsSaved = make(map[string][]float32)
	}
	for i, id := range chunkIDs {
		m.EmbeddingsSaved[id] = embeddings[i]
	}
	return m.SaveEmbeddingsError
}

func (m *MockMetadataStore) GetAllEmbeddings(ctx context.Context) (map[string][]float32, error) {
	if m.GetAllEmbeddingsError != nil {
		return nil, m.GetAllEmbeddingsError
	}
	if m.AllEmbeddings != nil {
		return m.AllEmbeddings, nil
	}
	// Return saved embeddings
	return m.EmbeddingsSaved, nil
}

func (m *MockMetadataStore) GetEmbeddingStats(ctx context.Context) (withEmbedding, withoutEmbedding int, err error) {
	return 0, 0, nil
}

func (m *MockMetadataStore) SaveIndexCheckpoint(ctx context.Context, stage string, total, embeddedCount int, embedderModel string) error {
	return nil
}

func (m *MockMetadataStore) LoadIndexCheckpoint(ctx context.Context) (*store.IndexCheckpoint, error) {
	return m.CheckpointToLoad, nil
}

func (m *MockMetadataStore) ClearIndexCheckpoint(ctx context.Context) error {
	m.ClearCheckpointCalled = true
	return nil
}

func (m *MockMetadataStore) Close() error {
	return nil
}

// MockBM25Index implements store.BM25Index for testing.
type MockBM25Index struct {
	IndexCalled bool
	SaveCalled  bool
	Documents   []*store.Document
	DeletedIDs  []string
	IndexError  error
	SaveError   error
}

func (m *MockBM25Index) Index(ctx context.Context, docs []*store.Document) error {
	m.IndexCalled = true
	m.Documents = docs
	return m.IndexError
}

func (m *MockBM25Index) Search(ctx context.Context, query string, limit int) ([]*store.BM25Result, error) {
	return nil, nil
}

func (m *MockBM25Index) Delete(ctx context.Context, docIDs []string) error {
	m.DeletedIDs = append(m.DeletedIDs, docIDs...)
	return nil
}

func (m *MockBM25Index) AllIDs() ([]string, error) {
	ids := make([]string, len(m.Documents))
	for i, doc := range m.Documents {
		ids[i] = doc.ID
	}
	return ids, nil
}

func (m *MockBM25Index) Stats() *store.IndexStats {
	return nil
}

func (m *MockBM25Index) Save(path string) error {
	m.SaveCalled = true
	return m.SaveError
}

func (m *MockBM25Index) Load(path string) error {
	return nil
}

func (m *MockBM25Index) Close() error {
	return nil
}

// MockVectorStore implements store.VectorStore for testing.
type MockVectorStore struct {
	AddCalled  bool
	SaveCalled bool
	IDs        []string
	Vectors    [][]float32
	DeletedIDs []string
	AddError   error
	SaveError  error
}

func (m *MockVectorStore) Add(ctx context.Context, ids []string, vectors [][]float32) error {
	m.AddCalled = true
	m.IDs = ids
	m.Vectors = vectors
	return m.AddError
}

func (m *MockVectorStore) Search(ctx context.Context, query []float32, k int) ([]*store.VectorResult, error) {
	return nil, nil
}

func (m *MockVectorStore) Delete(ctx context.Context, ids []string) error {
	m.DeletedIDs = append(m.DeletedIDs, ids...)
	return nil
}

func (m *MockVectorStore) AllIDs() []string {
	return m.IDs
}

func (m *MockVectorStore) Contains(id string) bool {
	return false
}

func (m *MockVectorStore) Count() int {
	return 0
}

func (m *MockVectorStore) Save(path string) error {
	m.SaveCalled = true
	return m.SaveError
}

func (m *MockVectorStore) Load(path string) error {
	return nil
}

func (m *MockVectorStore) Close() error {
	return nil
}

// MockEmbedder implements embed.Embedder for testing.
type MockEmbedder struct {
	EmbedBatchCalled bool
	BatchTexts       []string
	EmbedBatchError  error
	DimensionsValue  int
	ModelNameValue   string
	Embeddings       [][]float32
}

func (m *MockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return make([]float32, m.DimensionsValue), nil
}

func (m *MockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	m.EmbedBatchCalled = true
	m.BatchTexts = append(m.BatchTexts, texts...)
	if m.EmbedBatchError != nil {
		return nil, m.EmbedBatchError
	}
	// Return pre-set embeddings or generate default ones
	if m.Embeddings != nil && len(m.Embeddings) >= len(texts) {
		return m.Embeddings[:len(texts)], nil
	}
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = make([]float32, m.DimensionsValue)
	}
	return result, nil
}

func (m *MockEmbedder) Dimensions() int {
	if m.DimensionsValue == 0 {
		return 768
	}
	return m.DimensionsValue
}

func (m *MockEmbedder) ModelName() string {
	if m.ModelNameValue == "" {
		return "test-model"
	}
	return m.ModelNameValue
}

func (m *MockEmbedder) Available(ctx context.Context) bool {
	return true
}

func (m *MockEmbedder) Close() error {
	return nil
}

func (m *MockEmbedder) SetBatchIndex(idx int)      {}
func (m *MockEmbedder) SetFinalBatch(isFinal bool) {}

// MockChunker implements chunk.Chunker for testing.
type MockChunker struct {
	Chunks      []*chunk.Chunk
	ChunkError  error
	CloseCalled bool
	Inputs      []*chunk.FileInput
}

func (m *MockChunker) Chunk(ctx context.Context, file *chunk.FileInput) ([]*chunk.Chunk, error) {
	inputCopy := *file
	inputCopy.Content = append([]byte(nil), file.Content...)
	m.Inputs = append(m.Inputs, &inputCopy)

	if m.ChunkError != nil {
		return nil, m.ChunkError
	}
	if m.Chunks != nil {
		return m.Chunks, nil
	}
	// Generate a default chunk
	return []*chunk.Chunk{
		{
			ID:          hashString(file.Path + "0"),
			FilePath:    file.Path,
			Content:     string(file.Content),
			ContentType: chunk.ContentTypeCode,
			Language:    file.Language,
			StartLine:   1,
			EndLine:     10,
		},
	}, nil
}

func (m *MockChunker) SupportedExtensions() []string {
	return []string{".go", ".py", ".js"}
}

func (m *MockChunker) Close() {
	m.CloseCalled = true
}

type MockGraphRepository struct {
	ReplaceEdgesCalled    bool
	RecordBuildCalled     bool
	ResetCalled           bool
	ResetCount            int
	PurgeStaleEdgesCalled bool
	ReplaceEdgesError     error
	RecordBuildError      error
	RecordedBuild         graph.BuildMetadata
	SnapshotResult        *graph.StatusSnapshot
}

func (m *MockGraphRepository) Snapshot(ctx context.Context, opts graph.StatusOptions) (*graph.StatusSnapshot, error) {
	if m.SnapshotResult != nil {
		return m.SnapshotResult, nil
	}
	return &graph.StatusSnapshot{}, nil
}

func (m *MockGraphRepository) UpsertNode(ctx context.Context, node graph.Node) (graph.Node, error) {
	return node, nil
}

func (m *MockGraphRepository) UpsertEdge(ctx context.Context, edge graph.Edge) (graph.Edge, error) {
	return edge, nil
}

func (m *MockGraphRepository) ReplaceEdges(ctx context.Context, replacement graph.EdgeReplacement) error {
	m.ReplaceEdgesCalled = true
	return m.ReplaceEdgesError
}

func (m *MockGraphRepository) MarkEdgesToSourceStale(ctx context.Context, projectID, sourcePath string) error {
	return nil
}

func (m *MockGraphRepository) PurgeStaleEdges(ctx context.Context, projectID string, olderThan time.Time) (int, error) {
	m.PurgeStaleEdgesCalled = true
	return 0, nil
}

func (m *MockGraphRepository) ListNodes(ctx context.Context, query graph.NodeQuery) ([]graph.Node, error) {
	return nil, nil
}

func (m *MockGraphRepository) ListEdges(ctx context.Context, query graph.EdgeQuery) ([]graph.Edge, error) {
	return nil, nil
}

func (m *MockGraphRepository) RecordBuild(ctx context.Context, metadata graph.BuildMetadata) error {
	m.RecordBuildCalled = true
	m.RecordedBuild = metadata
	return m.RecordBuildError
}

func (m *MockGraphRepository) RecordExtractorRun(ctx context.Context, run graph.ExtractorRun) error {
	return nil
}

func (m *MockGraphRepository) Reset(ctx context.Context) error {
	m.ResetCalled = true
	m.ResetCount++
	return nil
}

func (m *MockGraphRepository) Close() error {
	return nil
}

// TestNewRunner tests Runner creation.
func TestNewRunner(t *testing.T) {
	tests := []struct {
		name    string
		deps    RunnerDependencies
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid dependencies",
			deps: RunnerDependencies{
				Renderer:        &MockRenderer{},
				Config:          config.NewConfig(),
				Metadata:        &MockMetadataStore{},
				BM25:            &MockBM25Index{},
				Vector:          &MockVectorStore{},
				Embedder:        &MockEmbedder{},
				CodeChunker:     &MockChunker{},
				MarkdownChunker: &MockChunker{},
			},
			wantErr: false,
		},
		{
			name: "missing renderer",
			deps: RunnerDependencies{
				Config:   config.NewConfig(),
				Metadata: &MockMetadataStore{},
				BM25:     &MockBM25Index{},
				Vector:   &MockVectorStore{},
				Embedder: &MockEmbedder{},
			},
			wantErr: true,
			errMsg:  "renderer is required",
		},
		{
			name: "missing config",
			deps: RunnerDependencies{
				Renderer: &MockRenderer{},
				Metadata: &MockMetadataStore{},
				BM25:     &MockBM25Index{},
				Vector:   &MockVectorStore{},
				Embedder: &MockEmbedder{},
			},
			wantErr: true,
			errMsg:  "config is required",
		},
		{
			name: "missing metadata",
			deps: RunnerDependencies{
				Renderer: &MockRenderer{},
				Config:   config.NewConfig(),
				BM25:     &MockBM25Index{},
				Vector:   &MockVectorStore{},
				Embedder: &MockEmbedder{},
			},
			wantErr: true,
			errMsg:  "metadata store is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner, err := NewRunner(tt.deps)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewRunner() expected error containing %q, got nil", tt.errMsg)
				} else if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("NewRunner() error = %q, want %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("NewRunner() unexpected error: %v", err)
				}
				if runner == nil {
					t.Error("NewRunner() returned nil runner")
				}
			}
		})
	}
}

// TestRunner_Close tests the Close method.
func TestRunner_Close(t *testing.T) {
	codeChunker := &MockChunker{}
	mdChunker := &MockChunker{}

	runner, err := NewRunner(RunnerDependencies{
		Renderer:        &MockRenderer{},
		Config:          config.NewConfig(),
		Metadata:        &MockMetadataStore{},
		BM25:            &MockBM25Index{},
		Vector:          &MockVectorStore{},
		Embedder:        &MockEmbedder{},
		CodeChunker:     codeChunker,
		MarkdownChunker: mdChunker,
	})
	if err != nil {
		t.Fatalf("NewRunner() error: %v", err)
	}

	err = runner.Close()
	if err != nil {
		t.Errorf("Close() error: %v", err)
	}

	if !codeChunker.CloseCalled {
		t.Error("Close() did not close code chunker")
	}
	if !mdChunker.CloseCalled {
		t.Error("Close() did not close markdown chunker")
	}
}

// TestRunnerResult tests the RunnerResult struct.
func TestRunnerResult(t *testing.T) {
	result := &RunnerResult{
		Files:    10,
		Chunks:   100,
		Duration: 5 * time.Second,
		Errors:   0,
		Warnings: 2,
		Resumed:  false,
	}

	if result.Files != 10 {
		t.Errorf("Files = %d, want 10", result.Files)
	}
	if result.Chunks != 100 {
		t.Errorf("Chunks = %d, want 100", result.Chunks)
	}
	if result.Warnings != 2 {
		t.Errorf("Warnings = %d, want 2", result.Warnings)
	}
}

// TestRunnerConfig tests the RunnerConfig struct.
func TestRunnerConfig(t *testing.T) {
	cfg := RunnerConfig{
		RootDir:              "/test/project",
		DataDir:              "/test/project/.amanmcp",
		Offline:              true,
		ResumeFromCheckpoint: 50,
		CheckpointModel:      "test-model",
		InterBatchDelay:      200 * time.Millisecond,
	}

	if cfg.RootDir != "/test/project" {
		t.Errorf("RootDir = %s, want /test/project", cfg.RootDir)
	}
	if cfg.Offline != true {
		t.Error("Offline = false, want true")
	}
	if cfg.ResumeFromCheckpoint != 50 {
		t.Errorf("ResumeFromCheckpoint = %d, want 50", cfg.ResumeFromCheckpoint)
	}
}

// TestHashString tests the hashString function.
func TestHashString(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"test"},
		{"another test"},
		{""},
		{"longer string with special chars !@#$%"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			hash := hashString(tt.input)
			if len(hash) != 16 {
				t.Errorf("hashString(%q) length = %d, want 16", tt.input, len(hash))
			}
			// Same input should produce same hash
			hash2 := hashString(tt.input)
			if hash != hash2 {
				t.Errorf("hashString(%q) not deterministic: %s != %s", tt.input, hash, hash2)
			}
		})
	}
}

// TestConvertChunkToStore tests chunk conversion.
func TestConvertChunkToStore(t *testing.T) {
	now := time.Now()
	files := []*store.File{
		{ID: "file1", Path: "test.go"},
	}

	c := &chunk.Chunk{
		ID:          "chunk1",
		FilePath:    "test.go",
		Content:     "func main() {}",
		RawContent:  "func main() {}",
		ContentType: chunk.ContentTypeCode,
		Language:    "go",
		StartLine:   1,
		EndLine:     5,
		Symbols: []*chunk.Symbol{
			{Name: "main", Type: chunk.SymbolTypeFunction, StartLine: 1, EndLine: 5},
		},
	}

	result := convertChunkToStore(c, files, now)

	if result.ID != "chunk1" {
		t.Errorf("ID = %s, want chunk1", result.ID)
	}
	if result.FileID != "file1" {
		t.Errorf("FileID = %s, want file1", result.FileID)
	}
	if result.FilePath != "test.go" {
		t.Errorf("FilePath = %s, want test.go", result.FilePath)
	}
	if result.Language != "go" {
		t.Errorf("Language = %s, want go", result.Language)
	}
	if len(result.Symbols) != 1 {
		t.Errorf("Symbols count = %d, want 1", len(result.Symbols))
	}
	if result.Symbols[0].Name != "main" {
		t.Errorf("Symbol name = %s, want main", result.Symbols[0].Name)
	}
}

// BUG-042: Test that Runner stores embedding dimension and model metadata
func TestRunner_StoresEmbeddingMetadata(t *testing.T) {
	// Given: A Runner with a mock embedder that has specific dimensions and model
	metadata := &MockMetadataStore{
		AllEmbeddings: make(map[string][]float32),
	}
	embedder := &MockEmbedder{
		DimensionsValue: 768,
		ModelNameValue:  "embeddinggemma:300m",
	}

	runner, err := NewRunner(RunnerDependencies{
		Renderer:        &MockRenderer{},
		Config:          config.NewConfig(),
		Metadata:        metadata,
		BM25:            &MockBM25Index{},
		Vector:          &MockVectorStore{},
		Embedder:        embedder,
		CodeChunker:     &MockChunker{},
		MarkdownChunker: &MockChunker{},
	})
	if err != nil {
		t.Fatalf("NewRunner() error: %v", err)
	}
	defer runner.Close()

	// Create a temp directory with a test file
	tmpDir := t.TempDir()
	testFile := tmpDir + "/test.go"
	if err := writeTestFile(testFile, "package main\nfunc main() {}"); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// When: Running indexing
	ctx := context.Background()
	_, err = runner.Run(ctx, RunnerConfig{
		RootDir: tmpDir,
		DataDir: tmpDir + "/.amanmcp",
	})
	if err != nil {
		t.Fatalf("Runner.Run() error: %v", err)
	}

	// Then: Embedding dimension and model should be stored in state
	if metadata.StateValues == nil {
		t.Fatal("StateValues is nil - SetState was never called")
	}

	// Check dimension is stored
	storedDim, hasDim := metadata.StateValues[store.StateKeyIndexDimension]
	if !hasDim {
		t.Errorf("Expected %s to be stored, but it was not", store.StateKeyIndexDimension)
	} else if storedDim != "768" {
		t.Errorf("StateKeyIndexDimension = %q, want %q", storedDim, "768")
	}

	// Check model is stored
	storedModel, hasModel := metadata.StateValues[store.StateKeyIndexModel]
	if !hasModel {
		t.Errorf("Expected %s to be stored, but it was not", store.StateKeyIndexModel)
	} else if storedModel != "embeddinggemma:300m" {
		t.Errorf("StateKeyIndexModel = %q, want %q", storedModel, "embeddinggemma:300m")
	}
}

func TestRunner_Run_GraphBuildFailureIsWarning(t *testing.T) {
	// Given: a runner whose graph repository fails during edge replacement
	renderer := &MockRenderer{}
	graphRepo := &MockGraphRepository{
		ReplaceEdgesError: fmt.Errorf("graph write failed"),
	}
	metadata := &MockMetadataStore{AllEmbeddings: make(map[string][]float32)}
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{}

	runner, err := NewRunner(RunnerDependencies{
		Renderer:        renderer,
		Config:          config.NewConfig(),
		Metadata:        metadata,
		BM25:            bm25,
		Vector:          vector,
		Embedder:        &MockEmbedder{},
		CodeChunker:     &MockChunker{},
		MarkdownChunker: &MockChunker{},
		GraphRepository: graphRepo,
	})
	require.NoError(t, err)
	defer runner.Close()

	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(tmpDir+"/test.go", []byte("package main\nfunc main() {}"), 0644))

	// When: indexing runs
	result, err := runner.Run(context.Background(), RunnerConfig{
		RootDir: tmpDir,
		DataDir: tmpDir + "/.amanmcp",
	})

	// Then: search indexing still completes, and graph failure is recorded as partial.
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, bm25.IndexCalled)
	assert.True(t, vector.AddCalled)
	assert.True(t, graphRepo.ReplaceEdgesCalled)
	assert.True(t, graphRepo.RecordBuildCalled)
	assert.Equal(t, graph.GraphStatusPartial, graphRepo.RecordedBuild.Status)
	assert.Contains(t, graphRepo.RecordedBuild.Message, "graph write failed")
	assert.Equal(t, 1, result.Warnings)
	require.NotEmpty(t, renderer.ErrorEvents)
	assert.True(t, renderer.ErrorEvents[len(renderer.ErrorEvents)-1].IsWarn)
	assert.Contains(t, renderer.ErrorEvents[len(renderer.ErrorEvents)-1].Err.Error(), "graph build failed")
}

func TestRunner_Run_DoesNotBuildGraphWhenNoChunksAreIndexed(t *testing.T) {
	// Given: a config-only run that produces graph source candidates but no search chunks
	renderer := &MockRenderer{}
	graphRepo := &MockGraphRepository{}
	metadata := &MockMetadataStore{AllEmbeddings: make(map[string][]float32)}
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{}

	runner, err := NewRunner(RunnerDependencies{
		Renderer:        renderer,
		Config:          config.NewConfig(),
		Metadata:        metadata,
		BM25:            bm25,
		Vector:          vector,
		Embedder:        &MockEmbedder{},
		CodeChunker:     &MockChunker{},
		MarkdownChunker: &MockChunker{},
		GraphRepository: graphRepo,
	})
	require.NoError(t, err)
	defer runner.Close()

	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(tmpDir+"/.amanmcp.yaml", []byte("embeddings:\n  provider: static\n"), 0644))

	// When: indexing exits through the no-chunks path
	result, err := runner.Run(context.Background(), RunnerConfig{
		RootDir: tmpDir,
		DataDir: tmpDir + "/.amanmcp",
	})

	// Then: graph is not mutated ahead of search metadata/index commits.
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, bm25.IndexCalled)
	assert.False(t, vector.AddCalled)
	assert.False(t, graphRepo.ResetCalled)
	assert.False(t, graphRepo.ReplaceEdgesCalled)
	assert.False(t, graphRepo.RecordBuildCalled)
}

func TestRunner_Run_DoesNotBuildGraphWhenSearchIndexFails(t *testing.T) {
	// Given: a run where search vector persistence fails
	renderer := &MockRenderer{}
	graphRepo := &MockGraphRepository{}
	metadata := &MockMetadataStore{AllEmbeddings: make(map[string][]float32)}
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{AddError: fmt.Errorf("vector write failed")}

	runner, err := NewRunner(RunnerDependencies{
		Renderer:        renderer,
		Config:          config.NewConfig(),
		Metadata:        metadata,
		BM25:            bm25,
		Vector:          vector,
		Embedder:        &MockEmbedder{},
		CodeChunker:     &MockChunker{},
		MarkdownChunker: &MockChunker{},
		GraphRepository: graphRepo,
	})
	require.NoError(t, err)
	defer runner.Close()

	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(tmpDir+"/test.go", []byte("package main\nfunc main() {}"), 0644))

	// When: indexing fails before search artifacts are fully committed
	result, err := runner.Run(context.Background(), RunnerConfig{
		RootDir: tmpDir,
		DataDir: tmpDir + "/.amanmcp",
	})

	// Then: the disposable graph overlay is left untouched.
	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, bm25.IndexCalled)
	assert.True(t, vector.AddCalled)
	assert.False(t, graphRepo.ResetCalled)
	assert.False(t, graphRepo.ReplaceEdgesCalled)
	assert.False(t, graphRepo.RecordBuildCalled)
}

// TestRunner_Run_ReplacesFileChunksOnReindex is the DEBT-042 contract: re-indexing
// a file must REPLACE that file's chunks in the metadata store, not leave the prior
// generation behind. Edited files get new content-hash chunk IDs, and the full
// index path persisted them without removing the old set — the stale generation is
// what polluted graph rebuild-from-store (the DEBT-041 duplicate symbol nodes) and
// BM25/vector search.
func TestRunner_Run_ReplacesFileChunksOnReindex(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := tmpDir + "/.amanmcp"
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	metadata, err := store.NewSQLiteStore(dataDir + "/metadata.db")
	require.NoError(t, err)
	defer func() { _ = metadata.Close() }()

	chunker := &MockChunker{}
	bm25 := &MockBM25Index{}
	vector := &MockVectorStore{}
	runner, err := NewRunner(RunnerDependencies{
		Renderer:        &MockRenderer{},
		Config:          config.NewConfig(),
		Metadata:        metadata,
		BM25:            bm25,
		Vector:          vector,
		Embedder:        &MockEmbedder{},
		CodeChunker:     chunker,
		MarkdownChunker: &MockChunker{},
		GraphRepository: &MockGraphRepository{},
	})
	require.NoError(t, err)
	defer runner.Close()

	ctx := context.Background()
	fileID := hashString("test.go")

	// Gen A: index test.go with one chunk at line 2.
	require.NoError(t, os.WriteFile(tmpDir+"/test.go", []byte("package main\nfunc A() {}\n"), 0o644))
	chunker.Chunks = []*chunk.Chunk{{
		ID: "genA-1", FilePath: "test.go", Content: "func A() {}",
		ContentType: chunk.ContentTypeCode, Language: "go", StartLine: 2, EndLine: 2,
	}}
	_, err = runner.Run(ctx, RunnerConfig{RootDir: tmpDir, DataDir: dataDir})
	require.NoError(t, err)

	genA, err := metadata.GetChunksByFile(ctx, fileID)
	require.NoError(t, err)
	require.Len(t, genA, 1, "first index stores one chunk")
	require.Equal(t, "genA-1", genA[0].ID)

	// Gen B: edit the file so the symbol shifts down → a new chunk id.
	require.NoError(t, os.WriteFile(tmpDir+"/test.go", []byte("package main\n\n\nfunc A() {}\n"), 0o644))
	chunker.Chunks = []*chunk.Chunk{{
		ID: "genB-1", FilePath: "test.go", Content: "func A() {}",
		ContentType: chunk.ContentTypeCode, Language: "go", StartLine: 4, EndLine: 4,
	}}
	_, err = runner.Run(ctx, RunnerConfig{RootDir: tmpDir, DataDir: dataDir})
	require.NoError(t, err)

	// The store must hold ONLY the current generation, not both.
	after, err := metadata.GetChunksByFile(ctx, fileID)
	require.NoError(t, err)
	ids := make([]string, len(after))
	for i, c := range after {
		ids[i] = c.ID
	}
	assert.Equal(t, []string{"genB-1"}, ids,
		"re-index must replace the file's chunks, not accumulate the stale generation")

	// AC2: the stale chunk's id is removed from BM25 and the vector store, so search
	// can no longer return the prior generation.
	assert.Contains(t, bm25.DeletedIDs, "genA-1", "stale chunk must be deleted from BM25 on re-index")
	assert.Contains(t, vector.DeletedIDs, "genA-1", "stale chunk must be deleted from the vector store on re-index")
	assert.NotContains(t, bm25.DeletedIDs, "genB-1", "the current chunk must not be deleted")
}

func TestRunner_Run_DispatchesPDFsToPDFChunker(t *testing.T) {
	metadata := &MockMetadataStore{AllEmbeddings: make(map[string][]float32)}
	pdfChunker := &MockChunker{
		Chunks: []*chunk.Chunk{
			{
				ID:          "pdf-chunk-1",
				FilePath:    "guide.pdf",
				Content:     "AmanMCP PDF search marker",
				RawContent:  "AmanMCP PDF search marker",
				ContentType: chunk.ContentTypePDF,
				Language:    "pdf",
				Metadata: map[string]string{
					"content_type": "pdf",
					"chunker":      "pdf",
					"page_start":   "1",
					"page_end":     "1",
				},
			},
		},
	}

	runner, err := NewRunner(RunnerDependencies{
		Renderer:        &MockRenderer{},
		Config:          config.NewConfig(),
		Metadata:        metadata,
		BM25:            &MockBM25Index{},
		Vector:          &MockVectorStore{},
		Embedder:        &MockEmbedder{},
		CodeChunker:     &MockChunker{},
		MarkdownChunker: &MockChunker{},
		PDFChunker:      pdfChunker,
	})
	require.NoError(t, err)
	defer runner.Close()

	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(tmpDir+"/guide.pdf", []byte("%PDF-1.7\x00binary-marker\n%%EOF"), 0o644))

	result, err := runner.Run(context.Background(), RunnerConfig{
		RootDir: tmpDir,
		DataDir: tmpDir + "/.amanmcp",
	})
	require.NoError(t, err)

	require.Equal(t, 1, result.Files)
	require.Equal(t, 1, result.Chunks)
	require.Len(t, metadata.FilesSaved, 1)
	assert.Equal(t, "pdf", metadata.FilesSaved[0].Language)
	assert.Equal(t, "pdf", metadata.FilesSaved[0].ContentType)
	require.Len(t, metadata.ChunksSaved, 1)
	assert.Equal(t, store.ContentTypePDF, metadata.ChunksSaved[0].ContentType)
	assert.Equal(t, "pdf", metadata.ChunksSaved[0].Metadata["content_type"])
}

func TestRunner_Run_WarnsWhenPDFProducesNoChunks(t *testing.T) {
	renderer := &MockRenderer{}
	runner, err := NewRunner(RunnerDependencies{
		Renderer:        renderer,
		Config:          config.NewConfig(),
		Metadata:        &MockMetadataStore{AllEmbeddings: make(map[string][]float32)},
		BM25:            &MockBM25Index{},
		Vector:          &MockVectorStore{},
		Embedder:        &MockEmbedder{},
		CodeChunker:     &MockChunker{},
		MarkdownChunker: &MockChunker{},
		PDFChunker:      &MockChunker{Chunks: []*chunk.Chunk{}},
	})
	require.NoError(t, err)
	defer runner.Close()

	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(tmpDir+"/scanned.pdf", []byte("%PDF-1.7\x00binary-marker\n%%EOF"), 0o644))

	result, err := runner.Run(context.Background(), RunnerConfig{
		RootDir: tmpDir,
		DataDir: tmpDir + "/.amanmcp",
	})
	require.NoError(t, err)

	require.Equal(t, 1, result.Files)
	require.Equal(t, 0, result.Chunks)
	require.Equal(t, 1, result.Warnings)
	require.Len(t, renderer.ErrorEvents, 1)
	assert.True(t, renderer.ErrorEvents[0].IsWarn)
	assert.Equal(t, "scanned.pdf", renderer.ErrorEvents[0].File)
	assert.Contains(t, renderer.ErrorEvents[0].Err.Error(), "OCR")
}

func TestRunner_Run_DoesNotRedactPDFBytesBeforeChunking(t *testing.T) {
	rawSecret := "0123456789abcdefghijklmnopqrstuvwxyzABCDEF"
	pdfBytes := []byte("%PDF-1.7\nxref\ntoken = \"" + rawSecret + "\"\n%%EOF")
	pdfChunker := &MockChunker{
		Chunks: []*chunk.Chunk{
			{
				ID:          "pdf-secret-chunk",
				FilePath:    "secret.pdf",
				Content:     "Extracted PDF text token = \"" + rawSecret + "\"",
				RawContent:  "Extracted PDF text token = \"" + rawSecret + "\"",
				ContentType: chunk.ContentTypePDF,
				Language:    "pdf",
				Metadata: map[string]string{
					"content_type": "pdf",
					"chunker":      "pdf",
					"page_number":  "1",
					"page_start":   "1",
					"page_end":     "1",
				},
			},
		},
	}
	metadata := &MockMetadataStore{AllEmbeddings: make(map[string][]float32)}
	embedder := &MockEmbedder{}

	runner, err := NewRunner(RunnerDependencies{
		Renderer:        &MockRenderer{},
		Config:          config.NewConfig(),
		Metadata:        metadata,
		BM25:            &MockBM25Index{},
		Vector:          &MockVectorStore{},
		Embedder:        embedder,
		CodeChunker:     &MockChunker{},
		MarkdownChunker: &MockChunker{},
		PDFChunker:      pdfChunker,
	})
	require.NoError(t, err)
	defer runner.Close()

	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(tmpDir+"/secret.pdf", pdfBytes, 0o644))

	result, err := runner.Run(context.Background(), RunnerConfig{
		RootDir: tmpDir,
		DataDir: tmpDir + "/.amanmcp",
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Files)
	require.Equal(t, 1, result.Chunks)

	require.Len(t, pdfChunker.Inputs, 1)
	assert.Equal(t, pdfBytes, pdfChunker.Inputs[0].Content, "PDF parser input must not be redacted before extraction")
	require.Len(t, metadata.ChunksSaved, 1)
	assert.NotContains(t, metadata.ChunksSaved[0].Content, rawSecret)
	assert.Contains(t, metadata.ChunksSaved[0].Content, "[REDACTED:generic-secret-assignment]")
	assert.Equal(t, "redact", metadata.ChunksSaved[0].Metadata["secret_scan_action"])
	require.NotEmpty(t, embedder.BatchTexts)
	for _, text := range embedder.BatchTexts {
		assert.NotContains(t, text, rawSecret)
	}
}

// writeTestFile creates a file with the given content.
func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
