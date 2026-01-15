// Package index provides indexing operations including consistency checking.
package index

import (
	"context"
	"log/slog"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

// InconsistencyType categorizes detected issues.
type InconsistencyType int

const (
	// InconsistencyOrphanBM25 indicates a BM25 entry without matching metadata.
	InconsistencyOrphanBM25 InconsistencyType = iota
	// InconsistencyOrphanVector indicates a vector entry without matching metadata.
	InconsistencyOrphanVector
	// InconsistencyMissingBM25 indicates a metadata entry missing from BM25.
	InconsistencyMissingBM25
	// InconsistencyMissingVector indicates a metadata entry missing from vector store.
	InconsistencyMissingVector
)

// String returns a human-readable description of the inconsistency type.
func (t InconsistencyType) String() string {
	switch t {
	case InconsistencyOrphanBM25:
		return "orphan_bm25"
	case InconsistencyOrphanVector:
		return "orphan_vector"
	case InconsistencyMissingBM25:
		return "missing_bm25"
	case InconsistencyMissingVector:
		return "missing_vector"
	default:
		return "unknown"
	}
}

// Inconsistency represents a detected cross-store issue.
type Inconsistency struct {
	Type    InconsistencyType
	ChunkID string
	Details string
}

// CheckResult contains the outcome of a consistency check.
type CheckResult struct {
	// Checked is the number of chunks verified.
	Checked int
	// Inconsistencies contains all detected issues.
	Inconsistencies []Inconsistency
	// Duration is how long the check took.
	Duration time.Duration
}

// ConsistencyChecker validates cross-store consistency.
// It detects orphaned entries (present in BM25/Vector but not in metadata)
// and missing entries (present in metadata but not in BM25/Vector).
type ConsistencyChecker struct {
	metadata store.MetadataStore
	bm25     store.BM25Index
	vector   store.VectorStore
}

// NewConsistencyChecker creates a new checker with the given stores.
func NewConsistencyChecker(metadata store.MetadataStore, bm25 store.BM25Index, vector store.VectorStore) *ConsistencyChecker {
	return &ConsistencyChecker{
		metadata: metadata,
		bm25:     bm25,
		vector:   vector,
	}
}

// Check scans all stores for inconsistencies.
// This is O(n) where n is the total number of entries across all stores.
func (c *ConsistencyChecker) Check(ctx context.Context) (*CheckResult, error) {
	start := time.Now()
	var issues []Inconsistency

	// Get all chunk IDs from metadata (source of truth)
	// We use GetAllEmbeddings which returns chunk IDs that have embeddings
	embeddingsMap, err := c.metadata.GetAllEmbeddings(ctx)
	if err != nil {
		return nil, err
	}

	metadataIDs := make(map[string]bool, len(embeddingsMap))
	for id := range embeddingsMap {
		metadataIDs[id] = true
	}

	// Get all IDs from BM25
	bm25IDs, err := c.bm25.AllIDs()
	if err != nil {
		slog.Warn("failed to get BM25 IDs for consistency check", slog.String("error", err.Error()))
		// Continue with what we have
	}

	// Get all IDs from Vector store
	vectorIDs := c.vector.AllIDs()

	// Find orphans in BM25 (not in metadata)
	for _, id := range bm25IDs {
		if !metadataIDs[id] {
			issues = append(issues, Inconsistency{
				Type:    InconsistencyOrphanBM25,
				ChunkID: id,
				Details: "BM25 entry without matching metadata",
			})
		}
	}

	// Find orphans in Vector (not in metadata)
	for _, id := range vectorIDs {
		if !metadataIDs[id] {
			issues = append(issues, Inconsistency{
				Type:    InconsistencyOrphanVector,
				ChunkID: id,
				Details: "Vector entry without matching metadata",
			})
		}
	}

	// Create sets for efficient lookup
	bm25Set := make(map[string]bool, len(bm25IDs))
	for _, id := range bm25IDs {
		bm25Set[id] = true
	}

	vectorSet := make(map[string]bool, len(vectorIDs))
	for _, id := range vectorIDs {
		vectorSet[id] = true
	}

	// Find missing entries (in metadata but not in BM25/Vector)
	for id := range metadataIDs {
		if !bm25Set[id] {
			issues = append(issues, Inconsistency{
				Type:    InconsistencyMissingBM25,
				ChunkID: id,
				Details: "Metadata entry missing from BM25 index",
			})
		}
		if !vectorSet[id] {
			issues = append(issues, Inconsistency{
				Type:    InconsistencyMissingVector,
				ChunkID: id,
				Details: "Metadata entry missing from vector store",
			})
		}
	}

	return &CheckResult{
		Checked:         len(metadataIDs),
		Inconsistencies: issues,
		Duration:        time.Since(start),
	}, nil
}

// Repair fixes detected inconsistencies.
// - Orphans: Deleted from BM25/Vector (best-effort, matches BUG-023 pattern)
// - Missing: Logged as warning (requires re-index to fix)
func (c *ConsistencyChecker) Repair(ctx context.Context, issues []Inconsistency) error {
	var orphanBM25, orphanVector []string
	var missingCount int

	for _, issue := range issues {
		switch issue.Type {
		case InconsistencyOrphanBM25:
			orphanBM25 = append(orphanBM25, issue.ChunkID)
		case InconsistencyOrphanVector:
			orphanVector = append(orphanVector, issue.ChunkID)
		case InconsistencyMissingBM25, InconsistencyMissingVector:
			missingCount++
		}
	}

	// Delete orphans from BM25 (best-effort)
	if len(orphanBM25) > 0 {
		if err := c.bm25.Delete(ctx, orphanBM25); err != nil {
			slog.Warn("failed to delete orphan BM25 entries",
				slog.Int("count", len(orphanBM25)),
				slog.String("error", err.Error()))
		} else {
			slog.Info("deleted orphan BM25 entries", slog.Int("count", len(orphanBM25)))
		}
	}

	// Delete orphans from Vector store (best-effort)
	if len(orphanVector) > 0 {
		if err := c.vector.Delete(ctx, orphanVector); err != nil {
			slog.Warn("failed to delete orphan vector entries",
				slog.Int("count", len(orphanVector)),
				slog.String("error", err.Error()))
		} else {
			slog.Info("deleted orphan vector entries", slog.Int("count", len(orphanVector)))
		}
	}

	// Log warning for missing entries (requires re-index)
	if missingCount > 0 {
		slog.Warn("index has missing entries, run 'amanmcp index --force' to rebuild",
			slog.Int("missing_count", missingCount))
	}

	return nil
}

// QuickCheck performs a lightweight consistency check.
// It only verifies counts match across stores, not individual IDs.
// Returns true if counts are consistent.
func (c *ConsistencyChecker) QuickCheck(ctx context.Context) (bool, error) {
	// Get metadata count
	embeddingsMap, err := c.metadata.GetAllEmbeddings(ctx)
	if err != nil {
		return false, err
	}
	metadataCount := len(embeddingsMap)

	// Get BM25 count
	bm25Stats := c.bm25.Stats()
	bm25Count := 0
	if bm25Stats != nil {
		bm25Count = bm25Stats.DocumentCount
	}

	// Get Vector count
	vectorCount := c.vector.Count()

	// Counts should match
	consistent := metadataCount == bm25Count && metadataCount == vectorCount

	if !consistent {
		slog.Debug("index counts mismatch",
			slog.Int("metadata", metadataCount),
			slog.Int("bm25", bm25Count),
			slog.Int("vector", vectorCount))
	}

	return consistent, nil
}
