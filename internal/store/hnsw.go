package store

import (
	"bufio"
	"context"
	"encoding/gob"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/coder/hnsw"
)

// HNSWStore implements VectorStore using coder/hnsw pure Go HNSW implementation.
// This replaces USearchStore to eliminate CGO dependencies.
type HNSWStore struct {
	mu     sync.RWMutex
	graph  *hnsw.Graph[uint64]
	config VectorStoreConfig

	// ID mapping (string <-> uint64)
	idMap   map[string]uint64 // string ID -> internal key
	keyMap  map[uint64]string // internal key -> string ID
	nextKey uint64            // next available key

	closed bool
}

// hnswMetadata stores ID mappings for persistence.
type hnswMetadata struct {
	IDMap   map[string]uint64
	NextKey uint64
	Config  VectorStoreConfig
}

// NewHNSWStore creates a new HNSW-based vector store.
func NewHNSWStore(cfg VectorStoreConfig) (*HNSWStore, error) {
	// Apply defaults
	if cfg.Metric == "" {
		cfg.Metric = "cos"
	}
	if cfg.M == 0 {
		cfg.M = 16 // coder/hnsw default recommendation
	}
	if cfg.EfSearch == 0 {
		cfg.EfSearch = 20 // coder/hnsw default
	}

	// Create HNSW graph
	graph := hnsw.NewGraph[uint64]()

	// Set distance function
	switch cfg.Metric {
	case "cos":
		graph.Distance = hnsw.CosineDistance
	case "l2":
		graph.Distance = hnsw.EuclideanDistance
	default:
		graph.Distance = hnsw.CosineDistance
	}

	// Set HNSW parameters
	graph.M = cfg.M
	graph.EfSearch = cfg.EfSearch
	graph.Ml = 0.25 // default level generation factor (1/ln(M))

	return &HNSWStore{
		graph:   graph,
		config:  cfg,
		idMap:   make(map[string]uint64),
		keyMap:  make(map[uint64]string),
		nextKey: 0,
	}, nil
}

// Add inserts vectors with their IDs.
// If an ID already exists, it will be updated (delete + add).
func (s *HNSWStore) Add(ctx context.Context, ids []string, vectors [][]float32) error {
	if len(ids) == 0 {
		return nil
	}

	if len(ids) != len(vectors) {
		return fmt.Errorf("ids and vectors length mismatch: %d vs %d", len(ids), len(vectors))
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("store is closed")
	}

	// Validate dimensions
	for _, v := range vectors {
		if len(v) != s.config.Dimensions {
			return ErrDimensionMismatch{
				Expected: s.config.Dimensions,
				Got:      len(v),
			}
		}
	}

	// Add vectors
	for i, id := range ids {
		// If ID exists, use lazy deletion (just update mappings, don't remove from graph)
		// This avoids a bug in coder/hnsw where deleting the last node breaks the graph
		if existingKey, exists := s.idMap[id]; exists {
			// Don't call s.graph.Delete() - use lazy deletion
			delete(s.keyMap, existingKey) // orphan the old key
			delete(s.idMap, id)
		}

		key := s.nextKey
		s.nextKey++

		// Normalize vector for cosine similarity
		vec := make([]float32, len(vectors[i]))
		copy(vec, vectors[i])
		if s.config.Metric == "cos" {
			normalizeVectorInPlace(vec)
		}

		// Create node and add to graph
		node := hnsw.MakeNode(key, vec)
		s.graph.Add(node)

		s.idMap[id] = key
		s.keyMap[key] = id
	}

	return nil
}

// Search finds k nearest neighbors to query vector.
func (s *HNSWStore) Search(ctx context.Context, query []float32, k int) ([]*VectorResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, fmt.Errorf("store is closed")
	}

	if len(query) != s.config.Dimensions {
		return nil, ErrDimensionMismatch{
			Expected: s.config.Dimensions,
			Got:      len(query),
		}
	}

	// Handle empty graph
	if s.graph.Len() == 0 {
		return []*VectorResult{}, nil
	}

	// Normalize query for cosine similarity
	normalizedQuery := make([]float32, len(query))
	copy(normalizedQuery, query)
	if s.config.Metric == "cos" {
		normalizeVectorInPlace(normalizedQuery)
	}

	// Search
	nodes := s.graph.Search(normalizedQuery, k)

	// Convert results
	results := make([]*VectorResult, 0, len(nodes))
	for _, node := range nodes {
		id, exists := s.keyMap[node.Key]
		if !exists {
			// Skip entries without valid ID mapping (shouldn't happen normally)
			continue
		}

		// Calculate distance
		distance := s.graph.Distance(normalizedQuery, node.Value)
		score := distanceToScore(distance, s.config.Metric)

		results = append(results, &VectorResult{
			ID:       id,
			Distance: distance,
			Score:    score,
		})
	}

	return results, nil
}

// Delete removes vectors by ID.
// Uses lazy deletion to avoid coder/hnsw issues with deleting last node.
func (s *HNSWStore) Delete(ctx context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("store is closed")
	}

	for _, id := range ids {
		if key, exists := s.idMap[id]; exists {
			// Use lazy deletion - just remove from mappings
			// The node remains in the graph but won't appear in results
			// This avoids issues with coder/hnsw when deleting nodes
			delete(s.keyMap, key)
			delete(s.idMap, id)
		}
	}

	return nil
}

// AllIDs returns all vector IDs in the store.
// Used for consistency checking between stores.
func (s *HNSWStore) AllIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil
	}

	ids := make([]string, 0, len(s.idMap))
	for id := range s.idMap {
		ids = append(ids, id)
	}
	return ids
}

// Contains checks if ID exists.
func (s *HNSWStore) Contains(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return false
	}

	_, exists := s.idMap[id]
	return exists
}

// Count returns number of vectors.
func (s *HNSWStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return 0
	}

	return len(s.idMap)
}

// HNSWStats contains HNSW store statistics including orphan count.
// Used by background compaction to determine when cleanup is needed.
type HNSWStats struct {
	ValidIDs   int // Number of valid ID mappings (active vectors)
	GraphNodes int // Total nodes in HNSW graph (includes orphans)
	Orphans    int // GraphNodes - ValidIDs (lazy-deleted nodes)
}

// Stats returns HNSW store statistics for compaction decisions.
// Orphans are nodes that remain in the graph after lazy deletion.
func (s *HNSWStore) Stats() HNSWStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return HNSWStats{}
	}

	validIDs := len(s.idMap)
	graphNodes := s.graph.Len()

	return HNSWStats{
		ValidIDs:   validIDs,
		GraphNodes: graphNodes,
		Orphans:    graphNodes - validIDs,
	}
}

// Save persists the index to disk.
// Uses atomic save (temp file + rename).
func (s *HNSWStore) Save(path string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return fmt.Errorf("store is closed")
	}

	// Create directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Save HNSW graph to temp file
	tmpIndexPath := path + ".tmp"
	file, err := os.Create(tmpIndexPath)
	if err != nil {
		return fmt.Errorf("failed to create index file: %w", err)
	}

	if err := s.graph.Export(file); err != nil {
		file.Close()
		os.Remove(tmpIndexPath)
		return fmt.Errorf("failed to export graph: %w", err)
	}

	if err := file.Close(); err != nil {
		os.Remove(tmpIndexPath)
		return fmt.Errorf("failed to close index file: %w", err)
	}

	// Rename to final path (atomic on most filesystems)
	if err := os.Rename(tmpIndexPath, path); err != nil {
		os.Remove(tmpIndexPath)
		return fmt.Errorf("failed to rename index file: %w", err)
	}

	// Save ID mappings
	metaPath := path + ".meta"
	if err := s.saveMetadata(metaPath); err != nil {
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	return nil
}

// saveMetadata saves ID mappings to a gob file.
func (s *HNSWStore) saveMetadata(path string) error {
	tmpPath := path + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp metadata file: %w", err)
	}

	meta := hnswMetadata{
		IDMap:   s.idMap,
		NextKey: s.nextKey,
		Config:  s.config,
	}

	encoder := gob.NewEncoder(file)
	if err := encoder.Encode(meta); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			slog.Warn("failed to close temp file during cleanup", slog.String("error", closeErr.Error()))
		}
		os.Remove(tmpPath)
		return fmt.Errorf("encode metadata: %w", err)
	}

	if err := file.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close metadata file: %w", err)
	}

	return os.Rename(tmpPath, path)
}

// Load loads the index from disk.
func (s *HNSWStore) Load(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("store is closed")
	}

	// Load ID mappings first to get config
	metaPath := path + ".meta"
	if err := s.loadMetadata(metaPath); err != nil {
		return fmt.Errorf("failed to load metadata: %w", err)
	}

	// Load HNSW graph
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open index file: %w", err)
	}
	defer file.Close()

	// Use bufio.Reader because coder/hnsw Import requires io.ByteReader
	reader := bufio.NewReader(file)
	if err := s.graph.Import(reader); err != nil {
		return fmt.Errorf("failed to import graph: %w", err)
	}

	return nil
}

// loadMetadata loads ID mappings from a gob file.
func (s *HNSWStore) loadMetadata(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open metadata file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.Warn("failed to close metadata file", slog.String("error", err.Error()))
		}
	}()

	var meta hnswMetadata

	decoder := gob.NewDecoder(file)
	if err := decoder.Decode(&meta); err != nil {
		return fmt.Errorf("decode hnsw metadata: %w", err)
	}

	// Rebuild mappings
	s.idMap = meta.IDMap
	s.keyMap = make(map[uint64]string)
	s.nextKey = meta.NextKey
	s.config = meta.Config

	for id, key := range s.idMap {
		s.keyMap[key] = id
	}

	return nil
}

// Close releases resources.
func (s *HNSWStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true
	// coder/hnsw Graph doesn't need explicit cleanup
	s.graph = nil

	return nil
}

// ReadHNSWStoreDimensions reads the dimensions from an existing HNSW store's metadata.
// Returns 0 if the metadata file doesn't exist (fresh start).
// The path should be the vector store path (e.g., "vectors.hnsw"), not the meta file path.
func ReadHNSWStoreDimensions(vectorPath string) (int, error) {
	metaPath := vectorPath + ".meta"

	file, err := os.Open(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // Fresh start
		}
		return 0, fmt.Errorf("failed to open hnsw metadata: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.Warn("failed to close hnsw metadata file", slog.String("error", err.Error()))
		}
	}()

	var meta hnswMetadata
	decoder := gob.NewDecoder(file)
	if err := decoder.Decode(&meta); err != nil {
		return 0, fmt.Errorf("failed to decode hnsw metadata: %w", err)
	}

	return meta.Config.Dimensions, nil
}

// Verify interface implementation
var _ VectorStore = (*HNSWStore)(nil)

// normalizeVectorInPlace normalizes a vector to unit length in place.
func normalizeVectorInPlace(v []float32) {
	var sumSquares float64
	for _, val := range v {
		sumSquares += float64(val) * float64(val)
	}
	if sumSquares == 0 {
		return
	}
	invMagnitude := float32(1.0 / math.Sqrt(sumSquares))
	for i := range v {
		v[i] *= invMagnitude
	}
}

// distanceToScore converts a distance value to a similarity score.
// For cosine distance: score = 1 - distance (distance ranges 0-2)
// For L2 distance: score = 1 / (1 + distance)
func distanceToScore(distance float32, metric string) float32 {
	switch metric {
	case "cos":
		// Cosine distance ranges from 0 (identical) to 2 (opposite)
		// Convert to similarity score 0-1
		return 1.0 - distance/2.0
	case "l2":
		// L2 distance ranges from 0 to infinity
		// Convert to similarity score 0-1
		return 1.0 / (1.0 + distance)
	default:
		return 1.0 - distance/2.0
	}
}
