package daemon

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/store"
)

// CompactionManager manages automatic background compaction for all projects.
// FEAT-AI3: Lazy background compaction for HNSW vector index.
//
// Compaction runs automatically when:
// 1. Project becomes idle (no searches for IdleTimeout duration)
// 2. Orphan ratio exceeds threshold (orphans/total > OrphanThreshold)
// 3. Minimum orphan count is met (avoids small index churn)
// 4. Cooldown period has elapsed since last compaction
//
// Compaction is interruptible: any search request cancels ongoing compaction.
type CompactionManager struct {
	config config.CompactionConfig
	daemon *Daemon // Back-reference for store access

	mu       sync.Mutex
	projects map[string]*compactionState

	// Lifecycle
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	stopOnce sync.Once
}

// compactionState tracks compaction eligibility per project.
type compactionState struct {
	rootPath    string
	lastSearch  time.Time // Updated on each search
	lastCompact time.Time // When last compacted

	// Idle detection
	idleTimer *time.Timer // Fires when idle timeout reached

	// Compaction in progress
	compacting bool
	cancelFunc context.CancelFunc // To interrupt compaction
}

// NewCompactionManager creates a new compaction manager.
func NewCompactionManager(daemon *Daemon, cfg config.CompactionConfig) *CompactionManager {
	return &CompactionManager{
		config:   cfg,
		daemon:   daemon,
		projects: make(map[string]*compactionState),
	}
}

// Start initializes the compaction manager.
func (m *CompactionManager) Start(ctx context.Context) {
	m.ctx, m.cancel = context.WithCancel(ctx)
	slog.Debug("compaction manager started",
		slog.Bool("enabled", m.config.Enabled),
		slog.Float64("orphan_threshold", m.config.OrphanThreshold),
		slog.Int("min_orphan_count", m.config.MinOrphanCount))
}

// Stop gracefully shuts down the compaction manager.
// Waits for any in-progress compaction to complete or cancel.
func (m *CompactionManager) Stop() {
	m.stopOnce.Do(func() {
		slog.Debug("compaction manager stopping")

		// Cancel context to signal all goroutines
		if m.cancel != nil {
			m.cancel()
		}

		// Cancel any in-progress compactions
		m.mu.Lock()
		for _, state := range m.projects {
			if state.idleTimer != nil {
				state.idleTimer.Stop()
			}
			if state.cancelFunc != nil {
				state.cancelFunc()
			}
		}
		m.mu.Unlock()

		// Wait for all compaction goroutines to finish
		m.wg.Wait()
		slog.Debug("compaction manager stopped")
	})
}

// OnSearchComplete is called after each search to reset idle timer.
// This enables idle detection for triggering compaction.
func (m *CompactionManager) OnSearchComplete(rootPath string) {
	if !m.config.Enabled {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.projects[rootPath]
	if !ok {
		state = &compactionState{rootPath: rootPath}
		m.projects[rootPath] = state
	}

	state.lastSearch = time.Now()

	// Reset idle timer
	if state.idleTimer != nil {
		state.idleTimer.Stop()
	}

	idleTimeout, err := time.ParseDuration(m.config.IdleTimeout)
	if err != nil {
		idleTimeout = 30 * time.Second // Default
	}

	state.idleTimer = time.AfterFunc(idleTimeout, func() {
		m.onIdle(rootPath)
	})
}

// InterruptCompaction stops ongoing compaction for a project.
// Called when a search request comes in during compaction.
func (m *CompactionManager) InterruptCompaction(rootPath string) {
	if !m.config.Enabled {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.projects[rootPath]
	if !ok || !state.compacting {
		return
	}

	if state.cancelFunc != nil {
		slog.Debug("interrupting compaction for search",
			slog.String("project", rootPath))
		state.cancelFunc()
	}
}

// onIdle is called when a project becomes idle (no searches).
func (m *CompactionManager) onIdle(rootPath string) {
	if !m.shouldCompact(rootPath) {
		return
	}

	m.startCompaction(rootPath)
}

// shouldCompact determines if compaction should run for a project.
func (m *CompactionManager) shouldCompact(rootPath string) bool {
	if !m.config.Enabled {
		return false
	}

	// Check if context is cancelled
	select {
	case <-m.ctx.Done():
		return false
	default:
	}

	m.mu.Lock()
	state, ok := m.projects[rootPath]
	if !ok {
		m.mu.Unlock()
		return false
	}

	// Check if already compacting
	if state.compacting {
		m.mu.Unlock()
		return false
	}

	// Check cooldown
	cooldown, err := time.ParseDuration(m.config.Cooldown)
	if err != nil {
		cooldown = time.Hour // Default
	}

	if time.Since(state.lastCompact) < cooldown {
		m.mu.Unlock()
		slog.Debug("compaction skipped: cooldown active",
			slog.String("project", rootPath),
			slog.Duration("remaining", cooldown-time.Since(state.lastCompact)))
		return false
	}
	m.mu.Unlock()

	// Get project state from daemon
	m.daemon.mu.RLock()
	projectState, ok := m.daemon.projects[rootPath]
	m.daemon.mu.RUnlock()

	if !ok || projectState == nil || projectState.vector == nil {
		return false
	}

	// Check orphan threshold
	orphanCount, totalCount, ratio := m.getOrphanStats(projectState)

	if orphanCount < m.config.MinOrphanCount {
		slog.Debug("compaction skipped: below minimum orphan count",
			slog.String("project", rootPath),
			slog.Int("orphans", orphanCount),
			slog.Int("min_required", m.config.MinOrphanCount))
		return false
	}

	if ratio < m.config.OrphanThreshold {
		slog.Debug("compaction skipped: below threshold",
			slog.String("project", rootPath),
			slog.Float64("ratio", ratio),
			slog.Float64("threshold", m.config.OrphanThreshold))
		return false
	}

	slog.Info("compaction eligible",
		slog.String("project", rootPath),
		slog.Int("orphans", orphanCount),
		slog.Int("total", totalCount),
		slog.Float64("ratio", ratio))

	return true
}

// getOrphanStats returns orphan statistics for a project's vector store.
func (m *CompactionManager) getOrphanStats(state *projectState) (orphanCount, totalCount int, ratio float64) {
	hnsw, ok := state.vector.(*store.HNSWStore)
	if !ok {
		return 0, 0, 0
	}

	stats := hnsw.Stats()
	orphanCount = stats.Orphans
	totalCount = stats.GraphNodes

	if totalCount == 0 {
		return 0, 0, 0
	}

	ratio = float64(orphanCount) / float64(totalCount)
	return orphanCount, totalCount, ratio
}

// startCompaction begins background compaction for a project.
func (m *CompactionManager) startCompaction(rootPath string) {
	m.mu.Lock()
	state := m.projects[rootPath]
	if state == nil || state.compacting {
		m.mu.Unlock()
		return
	}

	state.compacting = true
	ctx, cancel := context.WithCancel(m.ctx)
	state.cancelFunc = cancel
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() {
			m.mu.Lock()
			state.compacting = false
			state.cancelFunc = nil
			m.mu.Unlock()
		}()

		m.runCompaction(ctx, rootPath)
	}()
}

// runCompaction performs the actual compaction work.
func (m *CompactionManager) runCompaction(ctx context.Context, rootPath string) {
	start := time.Now()

	slog.Info("background compaction starting",
		slog.String("project", rootPath))

	// Get project state
	m.daemon.mu.RLock()
	projectState, ok := m.daemon.projects[rootPath]
	m.daemon.mu.RUnlock()

	if !ok || projectState == nil {
		slog.Warn("compaction failed: project not found",
			slog.String("project", rootPath))
		return
	}

	// Load embeddings from SQLite (zero re-embedding)
	embeddings, err := projectState.metadata.GetAllEmbeddings(ctx)
	if err != nil {
		slog.Warn("compaction failed: could not load embeddings",
			slog.String("project", rootPath),
			slog.String("error", err.Error()))
		return
	}

	if len(embeddings) == 0 {
		slog.Debug("compaction skipped: no embeddings",
			slog.String("project", rootPath))
		return
	}

	// Check for interruption
	select {
	case <-ctx.Done():
		slog.Debug("compaction interrupted before rebuild",
			slog.String("project", rootPath))
		return
	default:
	}

	// Determine dimensions from first embedding
	var dims int
	for _, emb := range embeddings {
		dims = len(emb)
		break
	}

	// Create fresh HNSW store
	cfg := store.DefaultVectorStoreConfig(dims)
	newVector, err := store.NewHNSWStore(cfg)
	if err != nil {
		slog.Warn("compaction failed: could not create vector store",
			slog.String("project", rootPath),
			slog.String("error", err.Error()))
		return
	}

	// Batch add with periodic interruption checks
	const batchSize = 1000
	ids := make([]string, 0, batchSize)
	vecs := make([][]float32, 0, batchSize)

	for id, vec := range embeddings {
		ids = append(ids, id)
		vecs = append(vecs, vec)

		if len(ids) >= batchSize {
			select {
			case <-ctx.Done():
				slog.Debug("compaction interrupted during rebuild",
					slog.String("project", rootPath))
				_ = newVector.Close()
				return
			default:
			}

			if err := newVector.Add(ctx, ids, vecs); err != nil {
				slog.Warn("compaction failed: batch add error",
					slog.String("project", rootPath),
					slog.String("error", err.Error()))
				_ = newVector.Close()
				return
			}

			ids = ids[:0]
			vecs = vecs[:0]
		}
	}

	// Add remaining
	if len(ids) > 0 {
		if err := newVector.Add(ctx, ids, vecs); err != nil {
			slog.Warn("compaction failed: final batch add error",
				slog.String("project", rootPath),
				slog.String("error", err.Error()))
			_ = newVector.Close()
			return
		}
	}

	// Check for interruption before save
	select {
	case <-ctx.Done():
		slog.Debug("compaction interrupted before save",
			slog.String("project", rootPath))
		_ = newVector.Close()
		return
	default:
	}

	// Get old stats for logging
	oldHNSW, ok := projectState.vector.(*store.HNSWStore)
	if !ok {
		slog.Warn("compaction failed: unexpected vector store type",
			slog.String("project", rootPath))
		_ = newVector.Close()
		return
	}
	oldStats := oldHNSW.Stats()

	// Save new vector store
	dataDir := filepath.Join(rootPath, ".amanmcp")
	vectorPath := filepath.Join(dataDir, "vectors.hnsw")
	if err := newVector.Save(vectorPath); err != nil {
		slog.Warn("compaction failed: could not save",
			slog.String("project", rootPath),
			slog.String("error", err.Error()))
		_ = newVector.Close()
		return
	}

	// Hot-swap the vector store in project state
	m.daemon.mu.Lock()
	oldVector := projectState.vector
	projectState.vector = newVector
	m.daemon.mu.Unlock()

	// Close old vector store
	_ = oldVector.Close()

	// Update compaction state
	m.mu.Lock()
	if state, ok := m.projects[rootPath]; ok {
		state.lastCompact = time.Now()
	}
	m.mu.Unlock()

	orphansRemoved := oldStats.Orphans
	duration := time.Since(start)

	slog.Info("background compaction complete",
		slog.String("project", rootPath),
		slog.Int("orphans_removed", orphansRemoved),
		slog.Int("vectors", newVector.Count()),
		slog.Duration("duration", duration))
}
