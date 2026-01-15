// Package index provides indexing operations including the Runner for reusable indexing logic.
package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/chunk"
	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/embed"
	"github.com/Aman-CERP/amanmcp/internal/scanner"
	"github.com/Aman-CERP/amanmcp/internal/store"
	"github.com/Aman-CERP/amanmcp/internal/ui"
)

// RunnerConfig configures an indexing run.
type RunnerConfig struct {
	// RootDir is the project root directory to index.
	RootDir string

	// DataDir is the .amanmcp data directory (defaults to RootDir/.amanmcp).
	DataDir string

	// Offline uses static embeddings instead of neural embedder.
	Offline bool

	// ResumeFromCheckpoint is the number of chunks already embedded (for resume).
	ResumeFromCheckpoint int

	// CheckpointModel is the embedder model name from checkpoint (for validation).
	CheckpointModel string

	// InterBatchDelay is the cooling delay between embedding batches.
	InterBatchDelay time.Duration
}

// RunnerResult contains the outcome of an indexing operation.
type RunnerResult struct {
	// Files is the number of files indexed.
	Files int

	// Chunks is the number of chunks created.
	Chunks int

	// Duration is the total indexing time.
	Duration time.Duration

	// Errors is the count of fatal errors.
	Errors int

	// Warnings is the count of non-fatal warnings.
	Warnings int

	// Resumed indicates if this was a resumed operation.
	Resumed bool
}

// RunnerDependencies contains the injected dependencies for Runner.
type RunnerDependencies struct {
	// Renderer for progress display (required).
	Renderer ui.Renderer

	// Config is the loaded project configuration (required).
	Config *config.Config

	// Metadata store for chunks and files.
	Metadata store.MetadataStore

	// BM25 index for keyword search.
	BM25 store.BM25Index

	// Vector store for semantic search.
	Vector store.VectorStore

	// Embedder for generating embeddings.
	Embedder embed.Embedder

	// CodeChunker for chunking code files.
	CodeChunker chunk.Chunker

	// MarkdownChunker for chunking markdown files.
	MarkdownChunker chunk.Chunker
}

// Runner executes indexing operations with progress reporting.
// It accepts injected dependencies for testability and reusability.
type Runner struct {
	renderer        ui.Renderer
	config          *config.Config
	metadata        store.MetadataStore
	bm25            store.BM25Index
	vector          store.VectorStore
	embedder        embed.Embedder
	codeChunker     chunk.Chunker
	markdownChunker chunk.Chunker
}

// NewRunner creates a Runner with injected dependencies.
func NewRunner(deps RunnerDependencies) (*Runner, error) {
	if deps.Renderer == nil {
		return nil, fmt.Errorf("renderer is required")
	}
	if deps.Config == nil {
		return nil, fmt.Errorf("config is required")
	}
	if deps.Metadata == nil {
		return nil, fmt.Errorf("metadata store is required")
	}
	if deps.BM25 == nil {
		return nil, fmt.Errorf("BM25 index is required")
	}
	if deps.Vector == nil {
		return nil, fmt.Errorf("vector store is required")
	}
	if deps.Embedder == nil {
		return nil, fmt.Errorf("embedder is required")
	}

	// Use provided chunkers or create defaults
	codeChunker := deps.CodeChunker
	if codeChunker == nil {
		codeChunker = chunk.NewCodeChunker()
	}

	markdownChunker := deps.MarkdownChunker
	if markdownChunker == nil {
		markdownChunker = chunk.NewMarkdownChunker()
	}

	return &Runner{
		renderer:        deps.Renderer,
		config:          deps.Config,
		metadata:        deps.Metadata,
		bm25:            deps.BM25,
		vector:          deps.Vector,
		embedder:        deps.Embedder,
		codeChunker:     codeChunker,
		markdownChunker: markdownChunker,
	}, nil
}

// Closer is an optional interface for chunkers that need cleanup.
type Closer interface {
	Close()
}

// Close releases resources held by the Runner.
func (r *Runner) Close() error {
	// Close chunkers if they implement Closer
	if c, ok := r.codeChunker.(Closer); ok {
		c.Close()
	}
	if c, ok := r.markdownChunker.(Closer); ok {
		c.Close()
	}
	return nil
}

// stageTiming tracks duration for each indexing stage.
type stageTiming struct {
	scan    time.Duration
	chunk   time.Duration
	context time.Duration
	embed   time.Duration
	index   time.Duration
}

// Run executes the full indexing pipeline.
func (r *Runner) Run(ctx context.Context, cfg RunnerConfig) (*RunnerResult, error) {
	startTime := time.Now()
	var errorCount, warnCount int
	var timing stageTiming

	root := cfg.RootDir
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = filepath.Join(root, ".amanmcp")
	}

	// Create project ID
	projectID := hashString(root)
	now := time.Now()

	// Save project metadata first (needed for foreign key constraints)
	project := &store.Project{
		ID:          projectID,
		Name:        filepath.Base(root),
		RootPath:    root,
		ProjectType: string(config.DetectProjectType(root)),
		FileCount:   0,
		ChunkCount:  0,
		IndexedAt:   now,
		Version:     fmt.Sprintf("%d", store.CurrentSchemaVersion),
	}
	if err := r.metadata.SaveProject(ctx, project); err != nil {
		return nil, fmt.Errorf("failed to save project: %w", err)
	}

	// Stage 1: Scan files
	scanStart := time.Now()
	files, err := r.scanFiles(ctx, root)
	if err != nil {
		return nil, err
	}
	timing.scan = time.Since(scanStart)
	warnCount += r.getWarningCount(files)

	if len(files) == 0 {
		return &RunnerResult{
			Files:    0,
			Chunks:   0,
			Duration: time.Since(startTime),
			Warnings: warnCount,
		}, nil
	}

	// Stage 2: Chunk files
	chunkStart := time.Now()
	allChunks, storeFiles, chunkWarns := r.chunkFiles(ctx, files, projectID, now)
	timing.chunk = time.Since(chunkStart)
	warnCount += chunkWarns

	if len(allChunks) == 0 {
		return &RunnerResult{
			Files:    len(files),
			Chunks:   0,
			Duration: time.Since(startTime),
			Warnings: warnCount,
		}, nil
	}

	// Save files and chunks to metadata (enables checkpoint/resume)
	if err := r.metadata.SaveFiles(ctx, storeFiles); err != nil {
		return nil, fmt.Errorf("failed to save files: %w", err)
	}

	storeChunks := make([]*store.Chunk, len(allChunks))
	for i, c := range allChunks {
		storeChunks[i] = convertChunkToStore(c, storeFiles, now)
	}
	if err := r.metadata.SaveChunks(ctx, storeChunks); err != nil {
		return nil, fmt.Errorf("failed to save chunks: %w", err)
	}

	// Stage 3: Contextual enrichment (CR-1)
	if r.config.Contextual.Enabled && cfg.ResumeFromCheckpoint == 0 {
		contextStart := time.Now()
		if err := r.enrichWithContext(ctx, storeChunks); err != nil {
			slog.Warn("contextual enrichment failed, continuing with original content",
				slog.String("error", err.Error()))
		}
		timing.context = time.Since(contextStart)

		// Save enriched chunks back to database
		if err := r.metadata.SaveChunks(ctx, storeChunks); err != nil {
			slog.Warn("failed to save enriched chunks, search will use original content",
				slog.String("error", err.Error()))
		}
	}

	// Stage 4: Generate embeddings
	embedStart := time.Now()
	currentModel := r.embedder.ModelName()
	if err := r.generateEmbeddings(ctx, allChunks, cfg, currentModel); err != nil {
		return nil, err
	}
	timing.embed = time.Since(embedStart)

	// Stage 5: Build indices
	indexStart := time.Now()
	if err := r.buildIndices(ctx, allChunks, dataDir, currentModel); err != nil {
		return nil, err
	}
	timing.index = time.Since(indexStart)

	// Update project stats
	if err := r.metadata.UpdateProjectStats(ctx, projectID, len(storeFiles), len(allChunks)); err != nil {
		return nil, fmt.Errorf("failed to update project stats: %w", err)
	}

	// Clear checkpoint on successful completion
	if err := r.metadata.ClearIndexCheckpoint(ctx); err != nil {
		slog.Warn("failed to clear checkpoint", slog.String("error", err.Error()))
	}

	// Mark index as using content-addressable chunk IDs (BUG-052)
	if err := r.metadata.SetState(ctx, store.StateKeyChunkIDVersion, store.ChunkIDVersionContent); err != nil {
		slog.Warn("failed to save chunk ID version", slog.String("error", err.Error()))
	}

	// BUG-042: Store embedding dimension and model for mismatch detection at search time
	if err := r.storeIndexEmbeddingInfo(ctx); err != nil {
		slog.Warn("failed to store index embedding info", slog.String("error", err.Error()))
	}

	// Save gitignore hash for startup reconciliation (BUG-053)
	gitignoreHash, err := ComputeGitignoreHash(root)
	if err != nil {
		slog.Warn("failed to compute gitignore hash", slog.String("error", err.Error()))
	} else {
		if err := r.metadata.SetState(ctx, GitignoreHashKey, gitignoreHash); err != nil {
			slog.Warn("failed to save gitignore hash", slog.String("error", err.Error()))
		}
	}

	duration := time.Since(startTime)

	// Get embedder info for logging and display
	embedderInfo := embed.GetInfo(ctx, r.embedder)

	// Complete
	r.renderer.Complete(ui.CompletionStats{
		Files:    len(storeFiles),
		Chunks:   len(allChunks),
		Duration: duration,
		Errors:   errorCount,
		Warnings: warnCount,
		Stages: ui.StageTimings{
			Scan:    timing.scan,
			Chunk:   timing.chunk,
			Context: timing.context,
			Embed:   timing.embed,
			Index:   timing.index,
		},
		Embedder: ui.EmbedderInfo{
			Backend:    string(embedderInfo.Provider),
			Model:      embedderInfo.Model,
			Dimensions: embedderInfo.Dimensions,
		},
	})

	// Enhanced logging with stage timings and backend info
	chunksPerSec := 0.0
	if timing.embed.Seconds() > 0 {
		chunksPerSec = float64(len(allChunks)) / timing.embed.Seconds()
	}

	slog.Info("index_complete",
		slog.Int("files", len(storeFiles)),
		slog.Int("chunks", len(allChunks)),
		slog.String("duration_total", duration.String()),
		slog.Int64("duration_total_ms", duration.Milliseconds()),
		slog.Int64("duration_scan_ms", timing.scan.Milliseconds()),
		slog.Int64("duration_chunk_ms", timing.chunk.Milliseconds()),
		slog.Int64("duration_context_ms", timing.context.Milliseconds()),
		slog.Int64("duration_embed_ms", timing.embed.Milliseconds()),
		slog.Int64("duration_index_ms", timing.index.Milliseconds()),
		slog.String("embedder_backend", string(embedderInfo.Provider)),
		slog.String("embedder_model", embedderInfo.Model),
		slog.Int("embedder_dimensions", embedderInfo.Dimensions),
		slog.Float64("chunks_per_sec", chunksPerSec),
		slog.String("path", root))

	return &RunnerResult{
		Files:    len(storeFiles),
		Chunks:   len(allChunks),
		Duration: duration,
		Errors:   errorCount,
		Warnings: warnCount,
		Resumed:  cfg.ResumeFromCheckpoint > 0,
	}, nil
}

// scanFiles scans the project directory for indexable files.
func (r *Runner) scanFiles(ctx context.Context, root string) ([]*scanner.FileInfo, error) {
	r.renderer.UpdateProgress(ui.ProgressEvent{
		Stage:   ui.StageScanning,
		Message: fmt.Sprintf("Scanning %s...", root),
	})
	slog.Info("index_scan_started", slog.String("path", root))

	excludePatterns := append(r.config.Paths.Exclude, "**/.amanmcp/**")
	s, err := scanner.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create scanner: %w", err)
	}

	results, err := s.Scan(ctx, &scanner.ScanOptions{
		RootDir:          root,
		IncludePatterns:  r.config.Paths.Include,
		ExcludePatterns:  excludePatterns,
		RespectGitignore: true,
		Workers:          runtime.NumCPU(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start scanning: %w", err)
	}

	var files []*scanner.FileInfo
	for result := range results {
		if result.Error != nil {
			r.renderer.AddError(ui.ErrorEvent{
				File:   result.File.Path,
				Err:    result.Error,
				IsWarn: true,
			})
			continue
		}
		files = append(files, result.File)
	}

	slog.Info("index_scan_complete",
		slog.Int("files", len(files)))
	return files, nil
}

// getWarningCount returns the number of warnings from scan results (currently 0 since we don't track).
func (r *Runner) getWarningCount(files []*scanner.FileInfo) int {
	return 0 // Warnings are tracked via renderer.AddError
}

// chunkFiles processes files and creates chunks.
func (r *Runner) chunkFiles(ctx context.Context, files []*scanner.FileInfo, projectID string, now time.Time) ([]*chunk.Chunk, []*store.File, int) {
	var allChunks []*chunk.Chunk
	var storeFiles []*store.File
	var warnCount int
	totalFiles := len(files)

	r.renderer.UpdateProgress(ui.ProgressEvent{
		Stage: ui.StageChunking,
		Total: totalFiles,
	})

	for i, file := range files {
		r.renderer.UpdateProgress(ui.ProgressEvent{
			Stage:       ui.StageChunking,
			Current:     i + 1,
			Total:       totalFiles,
			CurrentFile: file.Path,
		})

		// Read file content
		content, err := os.ReadFile(file.AbsPath)
		if err != nil {
			r.renderer.AddError(ui.ErrorEvent{
				File:   file.Path,
				Err:    fmt.Errorf("failed to read: %w", err),
				IsWarn: true,
			})
			warnCount++
			continue
		}

		// Create store file record
		storeFile := &store.File{
			ID:          hashString(file.Path),
			ProjectID:   projectID,
			Path:        file.Path,
			Size:        file.Size,
			ModTime:     file.ModTime,
			ContentHash: hashString(string(content)),
			Language:    file.Language,
			ContentType: string(file.ContentType),
			IndexedAt:   now,
		}
		storeFiles = append(storeFiles, storeFile)

		// Chunk the file based on content type
		input := &chunk.FileInput{
			Path:     file.Path,
			Content:  content,
			Language: file.Language,
		}

		var chunks []*chunk.Chunk
		switch file.ContentType {
		case scanner.ContentTypeCode:
			chunks, err = r.codeChunker.Chunk(ctx, input)
		case scanner.ContentTypeMarkdown:
			chunks, err = r.markdownChunker.Chunk(ctx, input)
		default:
			continue
		}

		if err != nil {
			r.renderer.AddError(ui.ErrorEvent{
				File:   file.Path,
				Err:    fmt.Errorf("failed to chunk: %w", err),
				IsWarn: true,
			})
			warnCount++
			continue
		}

		allChunks = append(allChunks, chunks...)
	}

	slog.Info("index_chunking_complete", slog.Int("chunks", len(allChunks)), slog.Int("files", len(storeFiles)))
	return allChunks, storeFiles, warnCount
}

// enrichWithContext adds LLM-generated context to chunks (CR-1 Contextual Retrieval).
func (r *Runner) enrichWithContext(ctx context.Context, chunks []*store.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	r.renderer.UpdateProgress(ui.ProgressEvent{
		Stage:   ui.StageContextual,
		Message: "Generating contextual descriptions...",
		Total:   len(chunks),
	})

	// Create context generator based on config
	var gen ContextGenerator
	if r.config.Contextual.FallbackOnly {
		gen = NewPatternContextGenerator(r.config)
		slog.Info("contextual_using_pattern_fallback",
			slog.Bool("code_chunks", r.config.Contextual.CodeChunks))
	} else {
		llmGen, err := NewLLMContextGenerator(ContextGeneratorConfig{
			OllamaHost: r.config.Embeddings.OllamaHost,
			Model:      r.config.Contextual.Model,
			Timeout:    r.config.Contextual.Timeout,
			BatchSize:  r.config.Contextual.BatchSize,
		})
		if err != nil || !llmGen.Available(ctx) {
			slog.Info("contextual_llm_unavailable_using_pattern",
				slog.String("model", r.config.Contextual.Model),
				slog.Bool("code_chunks", r.config.Contextual.CodeChunks))
			gen = NewPatternContextGenerator(r.config)
		} else {
			gen = NewHybridContextGenerator(llmGen, r.config)
			slog.Info("contextual_using_llm",
				slog.String("model", r.config.Contextual.Model),
				slog.Bool("code_chunks", r.config.Contextual.CodeChunks))
		}
	}
	defer func() { _ = gen.Close() }()

	// Group chunks by file for prompt caching optimization
	chunksByFile := GroupChunksByFile(chunks)
	processed := 0

	for filePath, fileChunks := range chunksByFile {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		docContext := ExtractDocumentContext(fileChunks)
		contexts, err := gen.GenerateBatch(ctx, fileChunks, docContext)
		if err != nil {
			slog.Debug("contextual_batch_failed",
				slog.String("file", filePath),
				slog.String("error", err.Error()))
			continue
		}

		for i, c := range fileChunks {
			if i < len(contexts) && contexts[i] != "" {
				EnrichChunkWithContext(c, contexts[i])
			}
		}

		processed += len(fileChunks)
		r.renderer.UpdateProgress(ui.ProgressEvent{
			Stage:   ui.StageContextual,
			Current: processed,
			Total:   len(chunks),
		})
	}

	slog.Info("contextual_enrichment_complete",
		slog.Int("chunks", len(chunks)),
		slog.String("generator", gen.ModelName()))

	return nil
}

// generateEmbeddings creates embeddings for all chunks with checkpointing.
func (r *Runner) generateEmbeddings(ctx context.Context, chunks []*chunk.Chunk, cfg RunnerConfig, currentModel string) error {
	const embeddingBatchSize = 32

	// Validate embedder model matches checkpoint (BUG-053)
	if cfg.ResumeFromCheckpoint > 0 && cfg.CheckpointModel != "" && cfg.CheckpointModel != currentModel {
		return fmt.Errorf("embedder mismatch on resume: checkpoint used '%s', but current embedder is '%s'. "+
			"Use --force to rebuild the index from scratch, or ensure the original embedder is available",
			cfg.CheckpointModel, currentModel)
	}

	startFromChunk := 0
	if cfg.ResumeFromCheckpoint > 0 && cfg.ResumeFromCheckpoint < len(chunks) {
		startFromChunk = cfg.ResumeFromCheckpoint
		r.embedder.SetBatchIndex(startFromChunk / embeddingBatchSize)
		slog.Info("resume_embedding",
			slog.Int("skip_chunks", startFromChunk),
			slog.Int("total_chunks", len(chunks)),
			slog.Int("batch_index", startFromChunk/embeddingBatchSize))
	}

	// Save checkpoint: starting/resuming embedding
	if err := r.metadata.SaveIndexCheckpoint(ctx, "embedding", len(chunks), startFromChunk, currentModel); err != nil {
		slog.Warn("failed to save checkpoint", slog.String("error", err.Error()))
	}

	r.renderer.UpdateProgress(ui.ProgressEvent{
		Stage:   ui.StageEmbedding,
		Current: startFromChunk,
		Total:   len(chunks),
	})

	modelName := r.embedder.ModelName()
	embeddedCount := startFromChunk

	for batchStart := startFromChunk; batchStart < len(chunks); batchStart += embeddingBatchSize {
		select {
		case <-ctx.Done():
			slog.Info("index_interrupted",
				slog.Int("embedded", embeddedCount),
				slog.Int("total", len(chunks)))
			return fmt.Errorf("indexing interrupted at %d/%d chunks: %w", embeddedCount, len(chunks), ctx.Err())
		default:
		}

		batchEnd := batchStart + embeddingBatchSize
		if batchEnd > len(chunks) {
			batchEnd = len(chunks)
		}
		batchChunks := chunks[batchStart:batchEnd]

		batchContents := make([]string, len(batchChunks))
		batchIDs := make([]string, len(batchChunks))
		for i, c := range batchChunks {
			batchContents[i] = c.Content
			batchIDs[i] = c.ID
		}

		// Mark final batch for timeout boost (BUG-050)
		if batchEnd >= len(chunks) {
			r.embedder.SetFinalBatch(true)
		}

		batchEmbeddings, err := r.embedder.EmbedBatch(ctx, batchContents)
		if err != nil {
			return fmt.Errorf("failed to generate embeddings for batch %d-%d: %w", batchStart, batchEnd, err)
		}

		if err := r.metadata.SaveChunkEmbeddings(ctx, batchIDs, batchEmbeddings, modelName); err != nil {
			return fmt.Errorf("failed to save embeddings: %w", err)
		}

		embeddedCount += len(batchChunks)

		if err := r.metadata.SaveIndexCheckpoint(ctx, "embedding", len(chunks), embeddedCount, currentModel); err != nil {
			slog.Warn("failed to save checkpoint", slog.String("error", err.Error()))
		}

		r.renderer.UpdateProgress(ui.ProgressEvent{
			Stage:   ui.StageEmbedding,
			Current: embeddedCount,
			Total:   len(chunks),
		})

		// Inter-batch cooling delay (thermal management)
		if cfg.InterBatchDelay > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(cfg.InterBatchDelay):
			}
		}
	}

	return nil
}

// buildIndices creates BM25 and vector indices from chunks.
func (r *Runner) buildIndices(ctx context.Context, chunks []*chunk.Chunk, dataDir string, currentModel string) error {
	r.renderer.UpdateProgress(ui.ProgressEvent{
		Stage:   ui.StageIndexing,
		Message: "Building search indices...",
	})

	// Save checkpoint: embedding complete, starting indexing
	if err := r.metadata.SaveIndexCheckpoint(ctx, "indexing", len(chunks), len(chunks), currentModel); err != nil {
		slog.Warn("failed to save checkpoint", slog.String("error", err.Error()))
	}

	// Index in BM25
	docs := make([]*store.Document, len(chunks))
	for i, c := range chunks {
		docs[i] = &store.Document{
			ID:      c.ID,
			Content: c.Content,
		}
	}
	if err := r.bm25.Index(ctx, docs); err != nil {
		return fmt.Errorf("failed to index in BM25: %w", err)
	}

	// Load all embeddings from SQLite and add to vector store
	allEmbeddings, err := r.metadata.GetAllEmbeddings(ctx)
	if err != nil {
		return fmt.Errorf("failed to load embeddings: %w", err)
	}

	// Build vectors in chunk order, regenerating missing if needed (BUG-052)
	ids := make([]string, len(chunks))
	embeddings := make([][]float32, len(chunks))
	var missingChunks []*chunk.Chunk
	var missingIndices []int

	for i, c := range chunks {
		ids[i] = c.ID
		if emb, ok := allEmbeddings[c.ID]; ok {
			embeddings[i] = emb
		} else {
			missingChunks = append(missingChunks, c)
			missingIndices = append(missingIndices, i)
		}
	}

	// Regenerate missing embeddings if any
	if len(missingChunks) > 0 {
		slog.Warn("regenerating missing embeddings",
			slog.Int("count", len(missingChunks)),
			slog.String("first_chunk", missingChunks[0].ID))

		missingContents := make([]string, len(missingChunks))
		missingIDs := make([]string, len(missingChunks))
		for i, c := range missingChunks {
			missingContents[i] = c.Content
			missingIDs[i] = c.ID
		}

		regenerated, err := r.embedder.EmbedBatch(ctx, missingContents)
		if err != nil {
			return fmt.Errorf("failed to regenerate %d missing embeddings: %w", len(missingChunks), err)
		}

		modelName := r.embedder.ModelName()
		if err := r.metadata.SaveChunkEmbeddings(ctx, missingIDs, regenerated, modelName); err != nil {
			slog.Warn("failed to save regenerated embeddings", slog.String("error", err.Error()))
		}

		for i, idx := range missingIndices {
			embeddings[idx] = regenerated[i]
		}

		slog.Info("regenerated missing embeddings", slog.Int("count", len(missingChunks)))
	}

	if err := r.vector.Add(ctx, ids, embeddings); err != nil {
		return fmt.Errorf("failed to add to vector store: %w", err)
	}

	// Save indices to disk
	// Note: BM25 index manages its own path; the path arg is for interface compatibility
	bm25Path := filepath.Join(dataDir, "bm25")
	if err := r.bm25.Save(bm25Path); err != nil {
		return fmt.Errorf("failed to save BM25 index: %w", err)
	}

	vectorPath := filepath.Join(dataDir, "vectors.hnsw")
	if err := r.vector.Save(vectorPath); err != nil {
		return fmt.Errorf("failed to save vector store: %w", err)
	}

	return nil
}

// storeIndexEmbeddingInfo saves the current embedder's dimension and model to metadata.
// BUG-042: This enables detection of dimension mismatch when embedder changes at search time.
// Without this, searching with a different embedder produces incorrect results silently.
func (r *Runner) storeIndexEmbeddingInfo(ctx context.Context) error {
	dim := fmt.Sprintf("%d", r.embedder.Dimensions())
	model := r.embedder.ModelName()

	if err := r.metadata.SetState(ctx, store.StateKeyIndexDimension, dim); err != nil {
		return fmt.Errorf("failed to store index dimension: %w", err)
	}
	if err := r.metadata.SetState(ctx, store.StateKeyIndexModel, model); err != nil {
		return fmt.Errorf("failed to store index model: %w", err)
	}

	slog.Info("index_embedding_info_stored",
		slog.String("model", model),
		slog.Int("dimensions", r.embedder.Dimensions()))

	return nil
}

// hashString returns SHA256 hash of a string (first 16 chars).
func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:16]
}

// convertChunkToStore converts a chunk.Chunk to store.Chunk.
func convertChunkToStore(c *chunk.Chunk, files []*store.File, now time.Time) *store.Chunk {
	var fileID string
	for _, f := range files {
		if f.Path == c.FilePath {
			fileID = f.ID
			break
		}
	}

	var symbols []*store.Symbol
	for _, s := range c.Symbols {
		symbols = append(symbols, &store.Symbol{
			Name:       s.Name,
			Type:       store.SymbolType(s.Type),
			StartLine:  s.StartLine,
			EndLine:    s.EndLine,
			Signature:  s.Signature,
			DocComment: s.DocComment,
		})
	}

	return &store.Chunk{
		ID:          c.ID,
		FileID:      fileID,
		FilePath:    c.FilePath,
		Content:     c.Content,
		RawContent:  c.RawContent,
		Context:     c.Context,
		ContentType: store.ContentType(c.ContentType),
		Language:    c.Language,
		StartLine:   c.StartLine,
		EndLine:     c.EndLine,
		Symbols:     symbols,
		Metadata:    c.Metadata,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}
