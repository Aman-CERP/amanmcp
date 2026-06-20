package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/chunk"
	"github.com/Aman-CERP/amanmcp/internal/gitignore"
	"github.com/Aman-CERP/amanmcp/internal/graph"
	"github.com/Aman-CERP/amanmcp/internal/language"
	"github.com/Aman-CERP/amanmcp/internal/scanner"
	"github.com/Aman-CERP/amanmcp/internal/search"
	"github.com/Aman-CERP/amanmcp/internal/secrets"
	"github.com/Aman-CERP/amanmcp/internal/store"
	"github.com/Aman-CERP/amanmcp/internal/watcher"
)

// DefaultMaxFileSize is the default maximum file size to index (100MB).
// Files larger than this are skipped to prevent memory exhaustion (BUG-002).
const DefaultMaxFileSize int64 = 100 * 1024 * 1024

// CoordinatorConfig contains configuration for the Coordinator.
type CoordinatorConfig struct {
	// ProjectID is the unique identifier for this project.
	ProjectID string

	// RootPath is the absolute path to the project root.
	RootPath string

	// DataDir is the path to the .amanmcp directory.
	DataDir string

	// Engine is the search engine for indexing and deletion.
	Engine *search.Engine

	// Metadata is the metadata store for file/chunk tracking.
	Metadata store.MetadataStore

	// GraphRepository is the optional disposable graph overlay store. When set,
	// coordinator updates keep graph edges aligned with successful index writes.
	GraphRepository graph.Repository

	// CodeChunker handles code files.
	CodeChunker chunk.Chunker

	// MDChunker handles markdown files.
	MDChunker chunk.Chunker

	// PDFChunker handles PDF document files.
	PDFChunker chunk.Chunker

	// Scanner is used for gitignore reconciliation (optional).
	// When set, enables automatic index updates on .gitignore changes.
	Scanner *scanner.Scanner

	// LanguageRegistry resolves language detection and content type.
	// Nil uses the built-in default registry.
	LanguageRegistry *language.Registry

	// SecretScanner gates content before chunking, embedding, BM25, and vector indexing.
	// Nil uses the default pre-index policy.
	SecretScanner *secrets.Scanner

	// ExcludePatterns are patterns to exclude from scanning (from config).
	// These are used during reconciliation to match initial indexing behavior.
	ExcludePatterns []string

	// MaxFileSize is the maximum file size to index in bytes (optional).
	// Files larger than this are skipped with a warning.
	// Defaults to DefaultMaxFileSize (100MB) if zero.
	MaxFileSize int64

	// GraphStalePurgeAfter controls stale-edge retention for refresh
	// maintenance. Defaults to graph.DefaultStalePurgeAfter when zero.
	GraphStalePurgeAfter time.Duration
}

// Coordinator handles incremental index updates based on file events.
type Coordinator struct {
	config CoordinatorConfig
	mu     sync.Mutex

	graphKnownSourcesLoaded bool
	graphKnownSourcesCache  []graph.SourceFile
}

// NewCoordinator creates a new index coordinator.
func NewCoordinator(config CoordinatorConfig) *Coordinator {
	if config.LanguageRegistry == nil {
		config.LanguageRegistry = language.DefaultRegistry()
	}
	if config.SecretScanner == nil {
		config.SecretScanner = secrets.NewScanner(secrets.DefaultPolicy())
	}
	if config.PDFChunker == nil {
		config.PDFChunker = chunk.NewPDFChunker()
	}
	return &Coordinator{
		config: config,
	}
}

// maxFileSize returns the effective maximum file size (uses default if not configured).
func (c *Coordinator) maxFileSize() int64 {
	if c.config.MaxFileSize > 0 {
		return c.config.MaxFileSize
	}
	return DefaultMaxFileSize
}

// HandleEvents processes a batch of file events.
func (c *Coordinator) HandleEvents(ctx context.Context, events []watcher.FileEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var processed int
	for _, event := range events {
		if err := c.handleEvent(ctx, event); err != nil {
			// Log warning but continue processing other events (graceful degradation)
			slog.Warn("failed to process file event",
				slog.String("path", event.Path),
				slog.String("operation", event.Operation.String()),
				slog.String("error", err.Error()))
			continue
		}
		processed++
	}

	// Update project stats after processing events (refresh indexed_at timestamp)
	if processed > 0 {
		if err := c.config.Metadata.RefreshProjectStats(ctx, c.config.ProjectID); err != nil {
			slog.Warn("failed to refresh project stats", slog.String("error", err.Error()))
		}
	}

	return nil
}

// handleEvent processes a single file event.
func (c *Coordinator) handleEvent(ctx context.Context, event watcher.FileEvent) error {
	slog.Debug("processing file event",
		slog.String("path", event.Path),
		slog.String("operation", event.Operation.String()),
		slog.Bool("is_dir", event.IsDir))

	// Skip directories
	if event.IsDir {
		return nil
	}

	switch event.Operation {
	case watcher.OpCreate, watcher.OpModify:
		return c.indexFile(ctx, event.Path)
	case watcher.OpDelete:
		return c.removeFile(ctx, event.Path)
	case watcher.OpRename:
		if event.OldPath != "" {
			if err := c.removeFile(ctx, event.OldPath); err != nil {
				return fmt.Errorf("failed to remove renamed source %s: %w", event.OldPath, err)
			}
			if event.Path == "" {
				return nil
			}
			return c.indexFile(ctx, event.Path)
		}
		if event.Path == "" {
			return nil
		}
		if _, err := os.Lstat(filepath.Join(c.config.RootPath, event.Path)); err != nil {
			if os.IsNotExist(err) {
				return c.removeFile(ctx, event.Path)
			}
			return fmt.Errorf("failed to stat renamed file: %w", err)
		}
		return c.indexFile(ctx, event.Path)
	case watcher.OpGitignoreChange:
		return c.handleGitignoreChange(ctx, event.Path)
	case watcher.OpConfigChange:
		return c.handleConfigChange(ctx)
	default:
		return nil
	}
}

// indexFile indexes or re-indexes a file.
func (c *Coordinator) indexFile(ctx context.Context, relPath string) error {
	absPath := filepath.Join(c.config.RootPath, relPath)

	// Use Lstat to detect symlinks without following them (BUG-005)
	info, err := os.Lstat(absPath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Skip symlinks to prevent security issues and infinite loops (BUG-005)
	if info.Mode()&os.ModeSymlink != 0 {
		slog.Debug("skipping symlink", slog.String("path", relPath))
		return nil
	}

	// Check file size before reading to prevent memory exhaustion (BUG-002)
	maxSize := c.maxFileSize()
	if info.Size() > maxSize {
		slog.Warn("skipping oversized file",
			slog.String("path", relPath),
			slog.Int64("size", info.Size()),
			slog.Int64("max", maxSize))
		return nil // Skip gracefully, don't error
	}

	// Read file content
	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Detect language and content type
	detectedLanguage := scanner.DetectLanguageWithRegistry(relPath, c.config.LanguageRegistry)
	contentType := scanner.DetectContentTypeWithRegistry(detectedLanguage, c.config.LanguageRegistry)

	// Skip binary files except first-class binary document types with chunkers.
	if contentType != scanner.ContentTypePDF && isBinaryContent(content) {
		return nil
	}

	// Skip plain text. Config files are recorded as graph-only metadata below;
	// they do not produce BM25/vector chunks.
	if !isIndexableContentType(contentType) {
		return nil
	}

	var secretResult secrets.Result
	if contentType != scanner.ContentTypePDF {
		secretResult = c.config.SecretScanner.GuardContent(secrets.ContentInput{
			Path:    relPath,
			Content: content,
			Source:  secrets.SourceIndex,
		})
		logSecretWarnings(secretResult.Warnings)
		if secretResult.Blocked {
			return nil
		}
		content = secretResult.Content
	}

	if contentType == scanner.ContentTypeConfig {
		return c.indexConfigFile(ctx, relPath, info, detectedLanguage, contentType, content)
	}

	// Select the appropriate chunker
	var chunker chunk.Chunker
	switch contentType {
	case scanner.ContentTypeCode:
		chunker = c.config.CodeChunker
	case scanner.ContentTypeMarkdown:
		chunker = c.config.MDChunker
	case scanner.ContentTypePDF:
		chunker = c.config.PDFChunker
	default:
		// Skip files without a chunker
		return nil
	}

	// Chunk the file
	fileInput := &chunk.FileInput{
		Path:     relPath,
		Content:  content,
		Language: detectedLanguage,
	}

	chunks, err := chunker.Chunk(ctx, fileInput)
	if err != nil {
		return fmt.Errorf("failed to chunk file: %w", err)
	}

	if len(chunks) == 0 {
		if contentType == scanner.ContentTypePDF {
			slog.Warn("pdf_content_unindexable",
				slog.String("file", relPath),
				slog.String("reason", "ocr_scanned_encrypted_or_malformed_not_supported"))
		}
		if err := c.removeIndexedFile(ctx, relPath); err != nil {
			return err
		}
		c.removeGraphKnownSource(relPath)
		if err := c.replaceGraphSourceWithEmptyEdges(ctx, relPath, false); err != nil {
			c.recordGraphUpdateFailure(ctx, "graph_incremental_source_prune_failed", relPath, err)
		}
		return nil
	}
	if contentType == scanner.ContentTypePDF {
		var warnings []secrets.Warning
		chunks, warnings = guardExtractedPDFChunks(chunks, c.config.SecretScanner, relPath)
		logSecretWarnings(warnings)
		if len(chunks) == 0 {
			if err := c.removeIndexedFile(ctx, relPath); err != nil {
				return err
			}
			c.removeGraphKnownSource(relPath)
			if err := c.replaceGraphSourceWithEmptyEdges(ctx, relPath, false); err != nil {
				c.recordGraphUpdateFailure(ctx, "graph_incremental_source_prune_failed", relPath, err)
			}
			return nil
		}
	} else {
		annotateSecretScan(chunks, secretResult)
	}

	fileID := generateFileID(c.config.ProjectID, relPath)

	// Save file record FIRST (chunks have foreign key to files)
	// Note: reusing 'info' from the size check above
	file := &store.File{
		ID:          fileID,
		ProjectID:   c.config.ProjectID,
		Path:        relPath,
		Size:        info.Size(),
		ModTime:     info.ModTime(),
		ContentHash: hashContent(content),
		Language:    detectedLanguage,
		ContentType: string(contentType),
	}

	// Remove existing chunks only after the replacement content has successfully
	// chunked. This preserves the last good graph/search state on chunker failure.
	if err := c.removeIndexedFile(ctx, relPath); err != nil {
		return err
	}

	if err := c.config.Metadata.SaveFiles(ctx, []*store.File{file}); err != nil {
		return fmt.Errorf("failed to save file record: %w", err)
	}

	// Convert to store.Chunk format
	storeChunks := make([]*store.Chunk, len(chunks))
	for i, ch := range chunks {
		symbols := make([]*store.Symbol, 0, len(ch.Symbols))
		for _, sym := range ch.Symbols {
			symbols = append(symbols, &store.Symbol{
				Name:       sym.Name,
				Type:       store.SymbolType(sym.Type),
				StartLine:  sym.StartLine,
				EndLine:    sym.EndLine,
				Signature:  sym.Signature,
				DocComment: sym.DocComment,
			})
		}
		storeChunks[i] = &store.Chunk{
			ID:          ch.ID,
			FileID:      fileID,
			FilePath:    relPath,
			Content:     ch.Content,
			RawContent:  ch.RawContent,
			Context:     ch.Context,
			ContentType: store.ContentType(ch.ContentType),
			Language:    ch.Language,
			StartLine:   ch.StartLine,
			EndLine:     ch.EndLine,
			Symbols:     symbols,
			Metadata:    ch.Metadata,
		}
	}

	// Index the chunks (engine handles embeddings and saves to metadata)
	if err := c.config.Engine.Index(ctx, storeChunks); err != nil {
		return fmt.Errorf("failed to index chunks: %w", err)
	}
	if err := c.updateGraphSource(ctx, relPath, detectedLanguage, contentType, content, chunks); err != nil {
		c.recordGraphUpdateFailure(ctx, "graph_incremental_update_failed", relPath, err)
	}

	return nil
}

func (c *Coordinator) indexConfigFile(ctx context.Context, relPath string, info fs.FileInfo, language string, contentType scanner.ContentType, content []byte) error {
	fileID := generateFileID(c.config.ProjectID, relPath)
	file := &store.File{
		ID:          fileID,
		ProjectID:   c.config.ProjectID,
		Path:        relPath,
		Size:        info.Size(),
		ModTime:     info.ModTime(),
		ContentHash: hashContent(content),
		Language:    language,
		ContentType: string(contentType),
	}

	if err := c.removeIndexedFile(ctx, relPath); err != nil {
		return err
	}
	if err := c.config.Metadata.SaveFiles(ctx, []*store.File{file}); err != nil {
		return fmt.Errorf("failed to save config file record: %w", err)
	}
	if err := c.updateGraphSource(ctx, relPath, language, contentType, content, nil); err != nil {
		c.recordGraphUpdateFailure(ctx, "graph_incremental_config_update_failed", relPath, err)
	}
	return nil
}

// removeFile removes a file's chunks from the index.
func (c *Coordinator) removeFile(ctx context.Context, relPath string) error {
	if err := c.removeIndexedFile(ctx, relPath); err != nil {
		return err
	}
	c.removeGraphKnownSource(relPath)
	if err := c.replaceGraphSourceWithEmptyEdges(ctx, relPath, true); err != nil {
		c.recordGraphUpdateFailure(ctx, "graph_incremental_delete_failed", relPath, err)
	}
	return nil
}

func (c *Coordinator) removeIndexedFile(ctx context.Context, relPath string) error {
	fileID := generateFileID(c.config.ProjectID, relPath)

	// Get existing chunks for this file
	chunks, err := c.config.Metadata.GetChunksByFile(ctx, fileID)
	if err != nil {
		// File might not exist in index
		return nil
	}

	if len(chunks) == 0 {
		// No chunks, but file record might exist - still try to delete it
		// BUG-026 fix: Log error instead of discarding silently
		if err := c.config.Metadata.DeleteFile(ctx, fileID); err != nil {
			slog.Warn("failed to delete orphan file record",
				slog.String("file_id", fileID),
				slog.String("path", relPath),
				slog.String("error", err.Error()))
		}
		return nil
	}

	// Collect chunk IDs
	chunkIDs := make([]string, len(chunks))
	for i, ch := range chunks {
		chunkIDs[i] = ch.ID
	}

	// Delete from search indices
	if err := c.config.Engine.Delete(ctx, chunkIDs); err != nil {
		return fmt.Errorf("failed to delete from index: %w", err)
	}

	// Delete file record from metadata (this cascades to chunks via ON DELETE CASCADE)
	if err := c.config.Metadata.DeleteFile(ctx, fileID); err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	return nil
}

func (c *Coordinator) updateGraphSource(ctx context.Context, relPath, language string, contentType scanner.ContentType, content []byte, chunks []*chunk.Chunk) error {
	if c.config.GraphRepository == nil {
		return nil
	}
	source, ok := graphSourceFromChunkedFile(&scanner.FileInfo{
		Path:        relPath,
		Language:    language,
		ContentType: contentType,
	}, content, chunks)
	if !ok {
		return nil
	}
	knownSources, err := c.graphKnownSources(ctx)
	if err != nil {
		return err
	}
	c.upsertGraphKnownSource(source)
	summary, err := graph.UpdateCheapEdgesWithSummary(ctx, c.config.GraphRepository, c.config.ProjectID, []graph.SourceFile{source}, graph.CheapExtractorOptions{
		KnownSources: knownSources,
	})
	if err != nil {
		return err
	}
	status := graph.GraphStatusFresh
	message := "incremental graph update"
	if summary.HadErrors || summary.HadWarnings {
		status = graph.GraphStatusPartial
		message = "incremental graph update completed with warnings or errors"
	}
	return c.recordGraphBuildWithSourceVersion(ctx, status, message, summary.SourceVersion)
}

func (c *Coordinator) graphKnownSources(ctx context.Context) ([]graph.SourceFile, error) {
	if c.config.Metadata == nil {
		return nil, nil
	}
	if c.graphKnownSourcesLoaded {
		return cloneGraphSources(c.graphKnownSourcesCache), nil
	}
	sources, err := c.loadGraphKnownSources(ctx)
	if err != nil {
		return nil, err
	}
	c.setGraphKnownSourcesFromSources(sources)
	return cloneGraphSources(c.graphKnownSourcesCache), nil
}

func (c *Coordinator) loadGraphKnownSources(ctx context.Context) ([]graph.SourceFile, error) {
	var sources []graph.SourceFile
	cursor := ""
	for {
		files, nextCursor, err := c.config.Metadata.ListFiles(ctx, c.config.ProjectID, cursor, 500)
		if err != nil {
			return nil, fmt.Errorf("list indexed files for graph path context: %w", err)
		}
		for _, file := range files {
			if file == nil {
				continue
			}
			contentType, ok := graphContentTypeFromString(file.ContentType)
			if !ok {
				continue
			}
			var content []byte
			if contentType == graph.SourceContentTypeConfig {
				content, err = readIndexedSourceFile(c.config.RootPath, file.Path)
				if err != nil {
					if errors.Is(err, os.ErrNotExist) {
						slog.Warn("graph_known_source_missing",
							slog.String("project_id", c.config.ProjectID),
							slog.String("file", file.Path),
							slog.String("action", "skip"))
						continue
					}
					return nil, err
				}
				guarded, blocked := guardGraphSourceContent(c.config.SecretScanner, file.Path, store.ContentType(file.ContentType), content)
				if blocked {
					continue
				}
				content = guarded
			}
			chunks, err := c.config.Metadata.GetChunksByFile(ctx, file.ID)
			if err != nil {
				return nil, fmt.Errorf("load graph chunks for known source %s: %w", file.Path, err)
			}
			source, ok := graphSourceFromStoreFile(file, content, chunks)
			if ok {
				sources = append(sources, source)
			}
		}
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	return sources, nil
}

func (c *Coordinator) setGraphKnownSourcesFromSources(sources []graph.SourceFile) {
	c.graphKnownSourcesCache = cloneGraphSources(sources)
	c.graphKnownSourcesLoaded = true
}

func (c *Coordinator) upsertGraphKnownSource(source graph.SourceFile) {
	if !c.graphKnownSourcesLoaded || source.Path == "" {
		return
	}
	normalized := normalizeGraphSourceForCache(source)
	for i, cached := range c.graphKnownSourcesCache {
		if cached.Path == normalized.Path {
			c.graphKnownSourcesCache[i] = normalized
			return
		}
	}
	c.graphKnownSourcesCache = append(c.graphKnownSourcesCache, normalized)
}

func (c *Coordinator) removeGraphKnownSource(relPath string) {
	if !c.graphKnownSourcesLoaded || relPath == "" {
		return
	}
	normalized := filepath.ToSlash(relPath)
	for i, source := range c.graphKnownSourcesCache {
		if source.Path == normalized {
			c.graphKnownSourcesCache = append(c.graphKnownSourcesCache[:i], c.graphKnownSourcesCache[i+1:]...)
			return
		}
	}
}

func cloneGraphSources(sources []graph.SourceFile) []graph.SourceFile {
	if len(sources) == 0 {
		return nil
	}
	cloned := make([]graph.SourceFile, 0, len(sources))
	for _, source := range sources {
		cloned = append(cloned, normalizeGraphSourceForCache(source))
	}
	return cloned
}

func normalizeGraphSourceForCache(source graph.SourceFile) graph.SourceFile {
	normalized := graph.SourceFile{
		Path:        filepath.ToSlash(source.Path),
		Language:    source.Language,
		ContentType: source.ContentType,
		Content:     append([]byte(nil), source.Content...),
	}
	if len(source.Chunks) > 0 {
		normalized.Chunks = make([]graph.SourceChunk, 0, len(source.Chunks))
		for _, chunk := range source.Chunks {
			normalized.Chunks = append(normalized.Chunks, cloneGraphSourceChunk(chunk))
		}
	}
	return normalized
}

func cloneGraphSourceChunk(chunk graph.SourceChunk) graph.SourceChunk {
	cloned := chunk
	cloned.FilePath = filepath.ToSlash(chunk.FilePath)
	if len(chunk.Symbols) > 0 {
		cloned.Symbols = append([]graph.SourceSymbol(nil), chunk.Symbols...)
	}
	return cloned
}

func (c *Coordinator) replaceGraphSourceWithEmptyEdges(ctx context.Context, relPath string, markInboundStale bool) error {
	if c.config.GraphRepository == nil {
		return nil
	}
	normalized := filepath.ToSlash(relPath)
	now := time.Now().UTC()
	if err := c.config.GraphRepository.ReplaceEdges(ctx, graph.EdgeReplacement{
		ProjectID:  c.config.ProjectID,
		Extractor:  graph.ExtractorCheap,
		SourcePath: normalized,
		Run: graph.ExtractorRun{
			Status:      graph.ExtractorStatusSuccess,
			StartedAt:   now,
			CompletedAt: now,
		},
	}); err != nil {
		return err
	}
	if markInboundStale {
		if err := c.config.GraphRepository.MarkEdgesToSourceStale(ctx, c.config.ProjectID, normalized); err != nil {
			return err
		}
		return c.recordGraphBuild(ctx, graph.GraphStatusFresh, "incremental graph delete")
	}
	return nil
}

func (c *Coordinator) recordGraphBuild(ctx context.Context, status graph.GraphStatus, message string) error {
	return c.recordGraphBuildWithSourceVersion(ctx, status, message, "")
}

func (c *Coordinator) recordGraphBuildWithSourceVersion(ctx context.Context, status graph.GraphStatus, message, sourceVersion string) error {
	if c.config.GraphRepository == nil {
		return nil
	}
	now := time.Now().UTC()
	if err := c.config.GraphRepository.RecordBuild(ctx, graph.BuildMetadata{
		ProjectID:     c.config.ProjectID,
		Kind:          graph.BuildKindIncremental,
		Status:        status,
		StartedAt:     now,
		CompletedAt:   now,
		SourceVersion: sourceVersion,
		Message:       message,
	}); err != nil {
		return fmt.Errorf("record graph build metadata: %w", err)
	}
	return nil
}

func (c *Coordinator) recordGraphUpdateFailure(ctx context.Context, event, relPath string, err error) {
	message := fmt.Sprintf("%s for %s: %v", event, relPath, err)
	if recordErr := c.recordGraphBuild(ctx, graph.GraphStatusPartial, message); recordErr != nil {
		message = fmt.Sprintf("%s; failed to record partial graph state: %v", message, recordErr)
	}
	slog.Warn(event,
		slog.String("project_id", c.config.ProjectID),
		slog.String("path", relPath),
		slog.String("message", message),
		slog.String("error", err.Error()))
}

// reconcileType represents the strategy for gitignore reconciliation.
type reconcileType int

const (
	reconcileFull reconcileType = iota
	reconcileSubtree
	reconcilePatternDiff
)

// reconcileStrategy contains the determined reconciliation approach.
type reconcileStrategy struct {
	Type            reconcileType
	Scope           string   // for subtree (directory path)
	AddedPatterns   []string // for pattern diff
	RemovedPatterns []string // for pattern diff (triggers full scan)
}

// stateGitignoreContent is the state key for storing root .gitignore content.
const stateGitignoreContent = "gitignore_content"

// handleGitignoreChange reconciles the index when .gitignore changes at runtime.
// BUG-028: Smart reconciliation strategy based on change type:
// - Nested .gitignore: Subtree scan only
// - Root .gitignore + patterns ADDED: No scan, just filter indexed files
// - Root .gitignore + patterns REMOVED: Full scan (rare case)
func (c *Coordinator) handleGitignoreChange(ctx context.Context, gitignorePath string) error {
	if c.config.Scanner == nil {
		slog.Warn("gitignore change detected but scanner not configured, skipping reconciliation")
		return nil
	}

	// BUG-022: Invalidate scanner's gitignore cache before reconciliation
	c.config.Scanner.InvalidateGitignoreCache()
	slog.Debug("invalidated scanner gitignore cache", "trigger", gitignorePath)

	// Determine reconciliation strategy
	strategy := c.determineReconciliationStrategy(ctx, gitignorePath)

	var err error
	switch strategy.Type {
	case reconcileSubtree:
		slog.Info("gitignore change - subtree reconciliation",
			slog.String("path", gitignorePath),
			slog.String("scope", strategy.Scope))
		err = c.reconcileGitignoreSubtree(ctx, strategy.Scope)

	case reconcilePatternDiff:
		slog.Info("gitignore change - pattern diff reconciliation",
			slog.String("path", gitignorePath),
			slog.Int("added", len(strategy.AddedPatterns)),
			slog.Int("removed", len(strategy.RemovedPatterns)))
		err = c.reconcileGitignorePatternDiff(ctx, strategy.AddedPatterns)

	default: // reconcileFull
		slog.Info("gitignore change - full reconciliation",
			slog.String("path", gitignorePath),
			slog.String("reason", "patterns removed or no cached content"))
		err = c.reconcileGitignoreInternal(ctx)
	}

	if err != nil {
		return err
	}

	// Update the cached hash after successful reconciliation
	newHash, hashErr := ComputeGitignoreHash(c.config.RootPath)
	if hashErr != nil {
		slog.Warn("failed to compute new gitignore hash", slog.String("error", hashErr.Error()))
		return nil
	}

	if setErr := c.config.Metadata.SetState(ctx, GitignoreHashKey, newHash); setErr != nil {
		slog.Warn("failed to save gitignore hash", slog.String("error", setErr.Error()))
	}

	return nil
}

// determineReconciliationStrategy analyzes the gitignore change and returns the optimal strategy.
func (c *Coordinator) determineReconciliationStrategy(ctx context.Context, gitignorePath string) reconcileStrategy {
	// Get relative path from project root
	relPath, err := filepath.Rel(c.config.RootPath, gitignorePath)
	if err != nil {
		slog.Debug("failed to get relative path, using full reconciliation", slog.String("error", err.Error()))
		return reconcileStrategy{Type: reconcileFull}
	}

	dir := filepath.Dir(relPath)

	// Case 1: Nested .gitignore - subtree scan only
	if dir != "." && dir != "" {
		return reconcileStrategy{Type: reconcileSubtree, Scope: dir}
	}

	// Case 2: Root .gitignore - try pattern diff
	oldContent, err := c.config.Metadata.GetState(ctx, stateGitignoreContent)
	if err != nil || oldContent == "" {
		// No previous content cached, must do full scan
		// But save current content for next time
		newContent, _ := os.ReadFile(gitignorePath)
		if len(newContent) > 0 {
			_ = c.config.Metadata.SetState(ctx, stateGitignoreContent, string(newContent))
		}
		return reconcileStrategy{Type: reconcileFull}
	}

	newContent, err := os.ReadFile(gitignorePath)
	if err != nil {
		// File deleted or unreadable, must do full scan
		// Clear cached content
		_ = c.config.Metadata.SetState(ctx, stateGitignoreContent, "")
		return reconcileStrategy{Type: reconcileFull}
	}

	added, removed := gitignore.DiffPatterns(oldContent, string(newContent))

	// Update cached content for next diff
	_ = c.config.Metadata.SetState(ctx, stateGitignoreContent, string(newContent))

	// Case 2a: Only added patterns - no scan needed!
	if len(added) > 0 && len(removed) == 0 {
		slog.Debug("root gitignore: only patterns added, using pattern diff",
			slog.Int("added_count", len(added)))
		return reconcileStrategy{
			Type:          reconcilePatternDiff,
			AddedPatterns: added,
		}
	}

	// Case 2b: Patterns removed - need full scan to find newly-unignored files
	if len(removed) > 0 {
		slog.Debug("root gitignore: patterns removed, requiring full scan",
			slog.Int("removed_count", len(removed)),
			slog.Int("added_count", len(added)))
		return reconcileStrategy{
			Type:            reconcileFull,
			AddedPatterns:   added,
			RemovedPatterns: removed,
		}
	}

	// Case 2c: No actual pattern change (only comments/whitespace)
	slog.Debug("root gitignore: no pattern changes detected")
	return reconcileStrategy{Type: reconcilePatternDiff, AddedPatterns: nil}
}

// reconcileGitignorePatternDiff handles root .gitignore with only ADDED patterns.
// No filesystem scan needed - just filter indexed files against new patterns.
func (c *Coordinator) reconcileGitignorePatternDiff(ctx context.Context, addedPatterns []string) error {
	if len(addedPatterns) == 0 {
		slog.Debug("gitignore pattern diff: no patterns to process")
		return nil
	}

	// Get all indexed files
	indexedPaths, err := c.config.Metadata.GetFilePathsByProject(ctx, c.config.ProjectID)
	if err != nil {
		return fmt.Errorf("failed to list indexed files: %w", err)
	}

	var toRemove []string
	for _, path := range indexedPaths {
		if gitignore.MatchesAnyPattern(path, addedPatterns) {
			toRemove = append(toRemove, path)
		}
	}

	// Remove files matching new ignore patterns
	for _, path := range toRemove {
		if err := c.removeFile(ctx, path); err != nil {
			slog.Warn("failed to remove newly-ignored file",
				slog.String("path", path),
				slog.String("error", err.Error()))
		}
	}

	slog.Info("pattern diff reconciliation complete",
		slog.Int("patterns_added", len(addedPatterns)),
		slog.Int("files_removed", len(toRemove)))

	return nil
}

// reconcileGitignoreSubtree reconciles only files under a specific subtree.
// Used when a nested .gitignore changes - no need to scan entire project.
func (c *Coordinator) reconcileGitignoreSubtree(ctx context.Context, subtreePath string) error {
	// Step 1: Get indexed files under subtree
	indexedPaths, err := c.config.Metadata.ListFilePathsUnder(ctx, c.config.ProjectID, subtreePath)
	if err != nil {
		return fmt.Errorf("failed to list indexed files under %s: %w", subtreePath, err)
	}
	indexedSet := make(map[string]bool, len(indexedPaths))
	for _, p := range indexedPaths {
		indexedSet[p] = true
	}
	slog.Debug("indexed files in subtree", slog.Int("count", len(indexedPaths)), slog.String("subtree", subtreePath))

	// Step 2: Scan only the subtree with fresh gitignore rules
	resultChan, err := c.config.Scanner.ScanSubtree(ctx, &scanner.ScanOptions{
		RootDir:          c.config.RootPath,
		RespectGitignore: true,
		LanguageRegistry: c.config.LanguageRegistry,
	}, subtreePath)
	if err != nil {
		return fmt.Errorf("failed to scan subtree %s: %w", subtreePath, err)
	}

	// Step 3: Build "should be indexed" set
	shouldBeIndexed := make(map[string]bool)
	for result := range resultChan {
		if result.Error != nil {
			slog.Warn("scan error in subtree",
				slog.String("path", result.File.Path),
				slog.String("error", result.Error.Error()))
			continue
		}
		if result.File == nil {
			continue
		}
		contentType := scanner.DetectContentTypeWithRegistry(result.File.Language, c.config.LanguageRegistry)
		if isIndexableContentType(contentType) {
			shouldBeIndexed[result.File.Path] = true
		}
	}
	slog.Debug("current files in subtree", slog.Int("count", len(shouldBeIndexed)), slog.String("subtree", subtreePath))

	// Step 4: Find files to remove (indexed but now ignored)
	var toRemove []string
	for path := range indexedSet {
		if !shouldBeIndexed[path] {
			toRemove = append(toRemove, path)
		}
	}

	// Step 5: Find files to add (should be indexed but not yet)
	var toAdd []string
	for path := range shouldBeIndexed {
		if !indexedSet[path] {
			toAdd = append(toAdd, path)
		}
	}

	// Step 6: Process changes
	for _, path := range toRemove {
		if err := c.removeFile(ctx, path); err != nil {
			slog.Warn("failed to remove file during subtree reconciliation",
				slog.String("path", path),
				slog.String("error", err.Error()))
		}
	}

	for _, path := range toAdd {
		if err := c.indexFile(ctx, path); err != nil {
			slog.Warn("failed to index file during subtree reconciliation",
				slog.String("path", path),
				slog.String("error", err.Error()))
		}
	}

	slog.Info("subtree reconciliation complete",
		slog.String("subtree", subtreePath),
		slog.Int("removed", len(toRemove)),
		slog.Int("added", len(toAdd)))

	return nil
}

// handleConfigChange handles .amanmcp.yaml configuration file changes.
// BUG-027 fix: Detect config changes and trigger reconciliation.
// Note: Full hot-reload of config requires server restart. This triggers
// a reconciliation to pick up files that should now be ignored based on
// the scanner's exclude patterns (which need restart to fully update).
func (c *Coordinator) handleConfigChange(ctx context.Context) error {
	slog.Info("configuration file changed",
		slog.String("note", "restart server for full config reload"))

	// Trigger a gitignore-style reconciliation to handle any files
	// that should now be excluded/included based on pattern changes.
	// The scanner's exclude patterns are set at startup, so this is
	// a partial solution - full reload requires restart.
	if c.config.Scanner == nil {
		slog.Warn("config change detected but scanner not configured, skipping reconciliation")
		return nil
	}

	// Invalidate cache and reconcile
	c.config.Scanner.InvalidateGitignoreCache()
	return c.reconcileGitignoreInternal(ctx)
}

// generateFileID creates a deterministic file ID.
func generateFileID(projectID, path string) string {
	input := fmt.Sprintf("%s:%s", projectID, path)
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:])[:16]
}

// hashContent creates a hash of file content.
func hashContent(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

// isBinaryContent checks if content appears to be binary.
func isBinaryContent(content []byte) bool {
	// Check first 512 bytes for null bytes
	checkLen := 512
	if len(content) < checkLen {
		checkLen = len(content)
	}

	for i := 0; i < checkLen; i++ {
		if content[i] == 0 {
			return true
		}
	}

	return false
}

func isIndexableContentType(contentType scanner.ContentType) bool {
	return contentType == scanner.ContentTypeCode ||
		contentType == scanner.ContentTypeMarkdown ||
		contentType == scanner.ContentTypePDF ||
		contentType == scanner.ContentTypeConfig
}

// GitignoreHashKey is the state key for storing the gitignore hash.
// Exported for use by index command to save hash after completion.
const GitignoreHashKey = "gitignore_hash"

// ComputeGitignoreHash computes a SHA256 hash of all .gitignore files in the project.
// The hash is deterministic: files are sorted by path and each contributes "path:content".
// Exported for use by index command to save hash after completion.
func ComputeGitignoreHash(rootPath string) (string, error) {
	var gitignorePaths []string

	// Walk the directory tree to find all .gitignore files
	err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}
		if d.IsDir() {
			// Skip hidden directories (except root) and common large directories
			name := d.Name()
			if name != "." && (name[0] == '.' || name == "node_modules" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == ".gitignore" {
			gitignorePaths = append(gitignorePaths, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to walk directory: %w", err)
	}

	// Sort for deterministic ordering
	sort.Strings(gitignorePaths)

	// Build hash input from all gitignore files
	h := sha256.New()
	for _, path := range gitignorePaths {
		content, err := os.ReadFile(path)
		if err != nil {
			continue // Skip unreadable files
		}
		relPath, _ := filepath.Rel(rootPath, path)
		// Write "path:content" for each file
		h.Write([]byte(relPath))
		h.Write([]byte(":"))
		h.Write(content)
		h.Write([]byte("\n"))
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// ReconcileOnStartup checks if .gitignore files have changed since last run
// and reconciles the index if needed. This handles changes made while the server was stopped.
func (c *Coordinator) ReconcileOnStartup(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.config.Scanner == nil {
		slog.Debug("startup reconciliation skipped: scanner not configured")
		return nil
	}

	// Get cached hash from last run
	cachedHash, err := c.config.Metadata.GetState(ctx, GitignoreHashKey)
	if err != nil {
		slog.Warn("failed to get cached gitignore hash", slog.String("error", err.Error()))
		// Continue anyway - treat as first run
	}

	// Compute current hash
	currentHash, err := ComputeGitignoreHash(c.config.RootPath)
	if err != nil {
		slog.Warn("failed to compute gitignore hash", slog.String("error", err.Error()))
		return nil // Non-fatal, skip reconciliation
	}

	// Compare hashes
	if cachedHash == currentHash && cachedHash != "" {
		slog.Debug("gitignore unchanged since last run, skipping startup reconciliation")
		return nil
	}

	slog.Info("gitignore changed since last run, reconciling index")

	// Run reconciliation (reuse existing method, but we're already holding the lock)
	// We need to call the internal logic directly
	if err := c.reconcileGitignoreInternal(ctx); err != nil {
		return fmt.Errorf("startup reconciliation failed: %w", err)
	}

	// Save new hash
	if err := c.config.Metadata.SetState(ctx, GitignoreHashKey, currentHash); err != nil {
		slog.Warn("failed to save gitignore hash", slog.String("error", err.Error()))
		// Non-fatal
	}

	return nil
}

// reconcileGitignoreInternal is the internal reconciliation logic without locking.
// It's called by both handleGitignoreChange (via runtime events) and ReconcileOnStartup.
func (c *Coordinator) reconcileGitignoreInternal(ctx context.Context) error {
	if c.config.Scanner == nil {
		return nil
	}

	slog.Debug("reconciling index after gitignore change")

	// Get all indexed file paths
	indexedPaths, err := c.config.Metadata.GetFilePathsByProject(ctx, c.config.ProjectID)
	if err != nil {
		return fmt.Errorf("failed to get indexed files: %w", err)
	}

	// Build a set of indexed paths for quick lookup
	indexedSet := make(map[string]bool, len(indexedPaths))
	for _, p := range indexedPaths {
		indexedSet[p] = true
	}

	// Scan filesystem with current gitignore rules and exclude patterns
	resultChan, err := c.config.Scanner.Scan(ctx, &scanner.ScanOptions{
		RootDir:          c.config.RootPath,
		RespectGitignore: true,
		ExcludePatterns:  c.config.ExcludePatterns,
		LanguageRegistry: c.config.LanguageRegistry,
	})
	if err != nil {
		return fmt.Errorf("failed to scan for gitignore reconciliation: %w", err)
	}

	// Build a set of files that should be indexed from the channel
	shouldBeIndexed := make(map[string]bool)
	for result := range resultChan {
		if result.Error != nil {
			slog.Debug("scan error during gitignore reconciliation",
				slog.String("error", result.Error.Error()))
			continue
		}
		if result.File == nil {
			continue
		}
		// Only consider code and markdown files (matching indexFile logic)
		contentType := scanner.DetectContentTypeWithRegistry(result.File.Language, c.config.LanguageRegistry)
		if isIndexableContentType(contentType) {
			shouldBeIndexed[result.File.Path] = true
		}
	}

	// Find files to remove (indexed but now ignored)
	var toRemove []string
	for path := range indexedSet {
		if !shouldBeIndexed[path] {
			toRemove = append(toRemove, path)
		}
	}

	// Find files to add (should be indexed but aren't)
	var toAdd []string
	for path := range shouldBeIndexed {
		if !indexedSet[path] {
			toAdd = append(toAdd, path)
		}
	}

	// Remove newly-ignored files
	for _, path := range toRemove {
		if err := c.removeFile(ctx, path); err != nil {
			slog.Warn("failed to remove file during gitignore sync",
				slog.String("path", path),
				slog.String("error", err.Error()))
		}
	}

	// Add newly-unignored files
	for _, path := range toAdd {
		if err := c.indexFile(ctx, path); err != nil {
			slog.Warn("failed to index file during gitignore sync",
				slog.String("path", path),
				slog.String("error", err.Error()))
		}
	}

	// Log summary
	if len(toRemove) > 0 || len(toAdd) > 0 {
		slog.Info("gitignore sync completed",
			slog.Int("removed", len(toRemove)),
			slog.Int("added", len(toAdd)))
	} else {
		slog.Debug("gitignore sync: no changes needed")
	}

	return nil
}

// ChangeType represents the type of file change detected during reconciliation.
type ChangeType int

const (
	// ChangeTypeAdded indicates a new file that needs indexing.
	ChangeTypeAdded ChangeType = iota
	// ChangeTypeModified indicates a file that was modified and needs re-indexing.
	ChangeTypeModified
	// ChangeTypeDeleted indicates a file that was deleted and needs removal from index.
	ChangeTypeDeleted
)

// FileChange represents a detected file change during startup reconciliation.
type FileChange struct {
	Path string
	Type ChangeType
}

// ReconcileFilesOnStartup detects and reconciles file changes that occurred
// while the server was stopped. This handles:
// - New files that need to be indexed
// - Modified files that need re-indexing
// - Deleted files that need chunk removal
//
// BUG-036: This ensures the index stays in sync even when files change offline.
func (c *Coordinator) ReconcileFilesOnStartup(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.config.Scanner == nil {
		slog.Debug("file reconciliation skipped: scanner not configured")
		return nil
	}

	slog.Debug("starting file reconciliation check")

	// Step 1: Get all indexed files from metadata (with mtime/size)
	indexedFiles, err := c.config.Metadata.GetFilesForReconciliation(ctx, c.config.ProjectID)
	if err != nil {
		return fmt.Errorf("failed to get indexed files: %w", err)
	}

	if len(indexedFiles) == 0 {
		slog.Debug("no indexed files found, skipping file reconciliation")
		return nil
	}

	// Step 2: Scan current filesystem
	currentFiles, err := c.scanCurrentFiles(ctx)
	if err != nil {
		return fmt.Errorf("failed to scan filesystem: %w", err)
	}

	// Step 3: Detect changes
	changes := c.detectFileChanges(indexedFiles, currentFiles)

	if len(changes) == 0 {
		slog.Debug("no file changes detected since last index")
		return nil
	}

	// Count changes by type
	var added, modified, deleted int
	for _, ch := range changes {
		switch ch.Type {
		case ChangeTypeAdded:
			added++
		case ChangeTypeModified:
			modified++
		case ChangeTypeDeleted:
			deleted++
		}
	}

	slog.Info("file changes detected, reconciling",
		slog.Int("added", added),
		slog.Int("modified", modified),
		slog.Int("deleted", deleted))

	// Step 4: Apply changes
	if err := c.applyFileChanges(ctx, changes); err != nil {
		return fmt.Errorf("failed to apply file changes: %w", err)
	}

	slog.Info("file reconciliation completed",
		slog.Int("total_changes", len(changes)))

	return nil
}

// ReconcileGraphOnStartup verifies the graph overlay and rebuilds it from the
// committed metadata index when it is empty, stale, partial, failed, or missing
// build metadata. It shares the coordinator lock with watcher events so rebuilds
// and incremental updates cannot race.
func (c *Coordinator) ReconcileGraphOnStartup(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.config.GraphRepository == nil {
		slog.Debug("graph startup reconciliation skipped: graph repository not configured")
		return nil
	}

	snapshot, err := c.config.GraphRepository.Snapshot(ctx, graph.StatusOptions{
		ProjectID: c.config.ProjectID,
		Now:       time.Now().UTC(),
	})
	if err != nil {
		wrapped := fmt.Errorf("inspect graph status on startup: %w", err)
		c.recordGraphUpdateFailure(ctx, "graph_startup_reconciliation_failed", "", wrapped)
		return wrapped
	}
	if err := c.purgeStaleGraphEdgesLocked(ctx, "graph_startup_reconciliation"); err != nil {
		return err
	}
	if !graphSnapshotNeedsRebuild(snapshot) {
		slog.Debug("graph startup reconciliation skipped",
			slog.String("project_id", c.config.ProjectID),
			slog.String("status", string(snapshot.Status)))
		return nil
	}

	return c.rebuildGraphFromMetadataLocked(ctx, "graph_startup_reconciliation", string(snapshot.Status))
}

// RefreshGraph refreshes the graph overlay from committed metadata when the
// current graph is missing, empty, stale, partial, or failed. It is meant for
// background maintenance cycles and shares the coordinator lock with watcher
// events so rebuilds and incremental updates remain ordered.
func (c *Coordinator) RefreshGraph(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.config.GraphRepository == nil {
		return nil
	}
	snapshot, err := c.config.GraphRepository.Snapshot(ctx, graph.StatusOptions{
		ProjectID: c.config.ProjectID,
		Now:       time.Now().UTC(),
	})
	if err != nil {
		wrapped := fmt.Errorf("inspect graph status before refresh: %w", err)
		c.recordGraphUpdateFailure(ctx, "graph_refresh_failed", "", wrapped)
		return wrapped
	}
	if err := c.purgeStaleGraphEdgesLocked(ctx, "graph_refresh"); err != nil {
		return err
	}
	if !graphSnapshotNeedsRebuild(snapshot) {
		slog.Debug("graph refresh skipped",
			slog.String("project_id", c.config.ProjectID),
			slog.String("status", string(snapshot.Status)))
		return nil
	}
	return c.rebuildGraphFromMetadataLocked(ctx, "graph_refresh", "scheduled")
}

func (c *Coordinator) purgeStaleGraphEdgesLocked(ctx context.Context, event string) error {
	if c.config.GraphRepository == nil {
		return nil
	}
	retention := c.config.GraphStalePurgeAfter
	if retention <= 0 {
		retention = graph.DefaultStalePurgeAfter
	}
	olderThan := time.Now().UTC().Add(-retention)
	purged, err := c.config.GraphRepository.PurgeStaleEdges(ctx, c.config.ProjectID, olderThan)
	if err != nil {
		wrapped := fmt.Errorf("purge stale graph edges older than %s: %w", retention, err)
		c.recordGraphUpdateFailure(ctx, event+"_stale_purge_failed", "", wrapped)
		return wrapped
	}
	if purged > 0 {
		slog.Info(event+"_stale_purge_complete",
			slog.String("project_id", c.config.ProjectID),
			slog.Int("purged_edges", purged),
			slog.Duration("retention", retention))
	}
	return nil
}

func graphSnapshotNeedsRebuild(snapshot *graph.StatusSnapshot) bool {
	if snapshot == nil || !snapshot.Available {
		return true
	}
	if snapshot.Status != graph.GraphStatusFresh {
		return true
	}
	return snapshot.Nodes.Total == 0 && snapshot.Edges.Total == 0
}

func (c *Coordinator) rebuildGraphFromMetadataLocked(ctx context.Context, event, reason string) error {
	if c.config.GraphRepository == nil {
		return nil
	}

	started := time.Now().UTC()
	slog.Info(event+"_begin",
		slog.String("project_id", c.config.ProjectID),
		slog.String("reason", reason))

	sources, err := BuildGraphSourcesFromMetadata(ctx, MetadataGraphSourceConfig{
		RootDir:       c.config.RootPath,
		ProjectID:     c.config.ProjectID,
		Metadata:      c.config.Metadata,
		SecretScanner: c.config.SecretScanner,
	})
	if err != nil {
		wrapped := fmt.Errorf("build graph sources from metadata: %w", err)
		c.recordGraphUpdateFailure(ctx, event+"_failed", "", wrapped)
		return wrapped
	}
	if err := c.config.GraphRepository.Reset(ctx); err != nil {
		wrapped := fmt.Errorf("reset graph overlay: %w", err)
		c.recordGraphUpdateFailure(ctx, event+"_failed", "", wrapped)
		return wrapped
	}
	if err := graph.IndexCheapEdges(ctx, c.config.GraphRepository, c.config.ProjectID, sources, graph.CheapExtractorOptions{}); err != nil {
		wrapped := fmt.Errorf("rebuild graph overlay: %w", err)
		c.recordGraphUpdateFailure(ctx, event+"_failed", "", wrapped)
		return wrapped
	}
	c.setGraphKnownSourcesFromSources(sources)

	slog.Info(event+"_complete",
		slog.String("project_id", c.config.ProjectID),
		slog.Int("files", len(sources)),
		slog.Duration("elapsed", time.Since(started)))
	return nil
}

// scanCurrentFiles performs a filesystem scan and returns map[path] -> FileInfo.
func (c *Coordinator) scanCurrentFiles(ctx context.Context) (map[string]*scanner.FileInfo, error) {
	resultChan, err := c.config.Scanner.Scan(ctx, &scanner.ScanOptions{
		RootDir:          c.config.RootPath,
		RespectGitignore: true,
		ExcludePatterns:  c.config.ExcludePatterns,
		LanguageRegistry: c.config.LanguageRegistry,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start scan: %w", err)
	}

	current := make(map[string]*scanner.FileInfo)
	for result := range resultChan {
		if result.Error != nil {
			slog.Debug("scan error during file reconciliation",
				slog.String("error", result.Error.Error()))
			continue
		}
		if result.File == nil {
			continue
		}
		// Only consider indexable content types (matching indexFile logic)
		contentType := scanner.DetectContentTypeWithRegistry(result.File.Language, c.config.LanguageRegistry)
		if isIndexableContentType(contentType) {
			current[result.File.Path] = result.File
		}
	}
	return current, nil
}

// detectFileChanges compares indexed vs current files and returns changes.
func (c *Coordinator) detectFileChanges(indexed map[string]*store.File, current map[string]*scanner.FileInfo) []FileChange {
	var changes []FileChange

	// Check for deleted and modified files
	for path, indexedFile := range indexed {
		currentFile, exists := current[path]
		if !exists {
			// File was deleted
			changes = append(changes, FileChange{
				Path: path,
				Type: ChangeTypeDeleted,
			})
		} else {
			// Check if modified (mtime or size changed)
			// Note: We truncate both to second precision since filesystem mtime
			// resolution varies and SQLite stores with second precision
			indexedMtime := indexedFile.ModTime.Truncate(1e9)
			currentMtime := currentFile.ModTime.Truncate(1e9)
			if !currentMtime.Equal(indexedMtime) || currentFile.Size != indexedFile.Size {
				changes = append(changes, FileChange{
					Path: path,
					Type: ChangeTypeModified,
				})
			}
		}
	}

	// Check for new files
	for path := range current {
		if _, exists := indexed[path]; !exists {
			changes = append(changes, FileChange{
				Path: path,
				Type: ChangeTypeAdded,
			})
		}
	}

	// Sort changes for deterministic processing: deletions first, then modifications, then additions
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Type != changes[j].Type {
			return changes[i].Type > changes[j].Type // Deleted (2) > Modified (1) > Added (0)
		}
		return changes[i].Path < changes[j].Path
	})

	return changes
}

// applyFileChanges processes the detected changes.
// BUG-037: Checks context before each operation to handle graceful shutdown.
func (c *Coordinator) applyFileChanges(ctx context.Context, changes []FileChange) error {
	var deleted, modified, added int

	for i, change := range changes {
		// BUG-037: Check for shutdown before each file operation.
		// This prevents "database is closed" errors when server shuts down
		// while reconciliation is still running in a background goroutine.
		select {
		case <-ctx.Done():
			slog.Debug("file reconciliation interrupted by shutdown",
				slog.Int("processed", i),
				slog.Int("remaining", len(changes)-i))
			return nil // Graceful shutdown, not an error
		default:
		}

		switch change.Type {
		case ChangeTypeDeleted:
			if err := c.removeFile(ctx, change.Path); err != nil {
				slog.Warn("failed to remove deleted file from index",
					slog.String("path", change.Path),
					slog.String("error", err.Error()))
			} else {
				deleted++
			}
		case ChangeTypeModified:
			if err := c.indexFile(ctx, change.Path); err != nil {
				slog.Warn("failed to re-index modified file",
					slog.String("path", change.Path),
					slog.String("error", err.Error()))
			} else {
				modified++
			}
		case ChangeTypeAdded:
			if err := c.indexFile(ctx, change.Path); err != nil {
				slog.Warn("failed to index new file",
					slog.String("path", change.Path),
					slog.String("error", err.Error()))
			} else {
				added++
			}
		}
	}

	slog.Debug("file reconciliation applied",
		slog.Int("deleted", deleted),
		slog.Int("modified", modified),
		slog.Int("added", added))

	return nil
}
