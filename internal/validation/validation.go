// Package validation provides test infrastructure for dogfooding validation.
// It enables running Tier 1, Tier 2, and Negative tests against real indices
// using the MCP server interface, avoiding CLI/BoltDB locking issues.
//
// Validation queries are data-driven, loaded from testdata/queries.yaml.
// This follows the Unix Philosophy: "Data-driven behavior" - queries can be
// modified without rebuilding the application.
package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/embed"
	"github.com/Aman-CERP/amanmcp/internal/mcp"
	"github.com/Aman-CERP/amanmcp/internal/search"
	"github.com/Aman-CERP/amanmcp/internal/store"
	"gopkg.in/yaml.v3"
)

// QuerySpec defines a test query with expected results.
type QuerySpec struct {
	ID       string   `yaml:"id"`       // e.g., "T1-Q7"
	Name     string   `yaml:"name"`     // Human-readable name
	Query    string   `yaml:"query"`    // The search query
	Tool     string   `yaml:"tool"`     // "search", "search_code", or "search_docs"
	Expected []string `yaml:"expected"` // File paths or prefixes that should appear in results
	Notes    string   `yaml:"notes"`    // Optional explanation for maintainers
	Tier     int      `yaml:"-"`        // Set programmatically based on section
}

// QueryConfig holds all validation queries loaded from YAML.
type QueryConfig struct {
	Tier1    []QuerySpec `yaml:"tier1"`
	Tier2    []QuerySpec `yaml:"tier2"`
	Negative []QuerySpec `yaml:"negative"`
}

var (
	queriesOnce sync.Once
	queriesData *QueryConfig
	queriesErr  error
)

// LoadQueries loads validation queries from the testdata/queries.yaml file.
// Results are cached after first load (singleton pattern).
func LoadQueries() (*QueryConfig, error) {
	queriesOnce.Do(func() {
		// Get the directory of this source file
		_, filename, _, ok := runtime.Caller(0)
		if !ok {
			queriesErr = fmt.Errorf("failed to get current file path")
			return
		}

		// testdata/queries.yaml is relative to this file
		dir := filepath.Dir(filename)
		path := filepath.Join(dir, "testdata", "queries.yaml")

		data, err := os.ReadFile(path)
		if err != nil {
			queriesErr = fmt.Errorf("failed to read queries file %s: %w", path, err)
			return
		}

		var cfg QueryConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			queriesErr = fmt.Errorf("failed to parse queries YAML: %w", err)
			return
		}

		// Set tier values programmatically
		for i := range cfg.Tier1 {
			cfg.Tier1[i].Tier = 1
		}
		for i := range cfg.Tier2 {
			cfg.Tier2[i].Tier = 2
		}
		for i := range cfg.Negative {
			cfg.Negative[i].Tier = 0
		}

		queriesData = &cfg
	})

	return queriesData, queriesErr
}

// ResetQueries clears the cached queries (for testing).
func ResetQueries() {
	queriesOnce = sync.Once{}
	queriesData = nil
	queriesErr = nil
}

// TestResult captures the outcome of a single query test.
type TestResult struct {
	Spec       QuerySpec     `json:"spec"`
	Passed     bool          `json:"passed"`
	Duration   time.Duration `json:"duration_ms"`
	TopResults []string      `json:"top_results"` // File paths returned
	MatchedAt  int           `json:"matched_at"`  // Position of first match (-1 if not found)
	Error      string        `json:"error,omitempty"`
}

// ValidationResult captures results of a full validation run.
type ValidationResult struct {
	Timestamp   time.Time      `json:"timestamp"`
	Tier1       []TestResult   `json:"tier1"`
	Tier2       []TestResult   `json:"tier2"`
	Negative    []TestResult   `json:"negative"`
	Tier1Pass   int            `json:"tier1_pass"`
	Tier1Total  int            `json:"tier1_total"`
	Tier2Pass   int            `json:"tier2_pass"`
	Tier2Total  int            `json:"tier2_total"`
	NegPass     int            `json:"negative_pass"`
	NegTotal    int            `json:"negative_total"`
	Embedder    string         `json:"embedder"`
	IndexChunks int            `json:"index_chunks"`
}

// Tier1Queries returns the standard Tier 1 validation queries.
// Queries are loaded from testdata/queries.yaml - no rebuild required to modify.
func Tier1Queries() []QuerySpec {
	cfg, err := LoadQueries()
	if err != nil {
		// Return empty slice on error - tests will report 0/0
		return nil
	}
	return cfg.Tier1
}

// Tier2Queries returns the Tier 2 validation queries.
// Queries are loaded from testdata/queries.yaml - no rebuild required to modify.
func Tier2Queries() []QuerySpec {
	cfg, err := LoadQueries()
	if err != nil {
		return nil
	}
	return cfg.Tier2
}

// NegativeQueries returns negative test cases that should not crash.
// Queries are loaded from testdata/queries.yaml - no rebuild required to modify.
func NegativeQueries() []QuerySpec {
	cfg, err := LoadQueries()
	if err != nil {
		return nil
	}
	return cfg.Negative
}

// Validator runs validation queries against an MCP server.
type Validator struct {
	server   *mcp.Server
	embedder embed.Embedder
}

// ErrIndexLocked indicates another process has the index locked.
var ErrIndexLocked = fmt.Errorf("index is locked by another process (stop MCP serve or Claude Code first)")

// NewValidator creates a validator for the given project root.
// It initializes the search engine and MCP server using the real index.
//
// Note: For Bleve backend, this requires exclusive access to the BM25 index.
// If another process has the index open, this will return ErrIndexLocked.
// SQLite backend supports concurrent access (BUG-064 fix).
func NewValidator(ctx context.Context, projectRoot string) (*Validator, error) {
	dataDir := filepath.Join(projectRoot, ".amanmcp")

	// Check index exists
	metadataPath := filepath.Join(dataDir, "metadata.db")
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("no index found at %s - run 'amanmcp index' first", dataDir)
	}

	// Load configuration first to determine backend
	cfg, err := config.Load(projectRoot)
	if err != nil {
		cfg = config.NewConfig()
	}

	// Detect existing backend or use config
	bm25BasePath := filepath.Join(dataDir, "bm25")
	backend := cfg.Search.BM25Backend
	if backend == "" {
		// Auto-detect: check which backend exists
		detected := store.DetectBM25Backend(bm25BasePath)
		if detected != "" {
			backend = string(detected)
		} else {
			backend = "sqlite" // Default for new indexes
		}
	}

	// For Bleve backend, check for locks (BoltDB exclusive locking issue)
	if backend == "bleve" {
		lockPath := filepath.Join(dataDir, "bm25.bleve", "index_meta.json")
		if _, err := os.Stat(lockPath); err == nil {
			// Try to open with a timeout to detect locks
			// BoltDB will block indefinitely if locked, so we use a goroutine with timeout
			type result struct {
				bm25 *store.BleveBM25Index
				err  error
			}
			done := make(chan result, 1)
			bm25Path := filepath.Join(dataDir, "bm25.bleve")

			go func() {
				bm25, err := store.NewBleveBM25Index(bm25Path, store.DefaultBM25Config())
				done <- result{bm25, err}
			}()

			select {
			case r := <-done:
				if r.err != nil {
					return nil, fmt.Errorf("failed to open BM25 index: %w", r.err)
				}
				// Successfully opened, continue with this bm25
				return newValidatorWithBM25(ctx, projectRoot, dataDir, r.bm25)
			case <-time.After(5 * time.Second):
				return nil, ErrIndexLocked
			}
		}
	}

	// Initialize stores
	metadata, err := store.NewSQLiteStore(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open metadata: %w", err)
	}

	// Use factory for BM25 backend (SQLite supports concurrent access)
	bm25, err := store.NewBM25IndexWithBackend(bm25BasePath, store.DefaultBM25Config(), backend)
	if err != nil {
		metadata.Close()
		return nil, fmt.Errorf("failed to open BM25 index: %w", err)
	}

	return newValidatorWithStores(ctx, projectRoot, cfg, metadata, bm25)
}

// newValidatorWithBM25 continues validator creation after BM25 is opened.
func newValidatorWithBM25(ctx context.Context, projectRoot, dataDir string, bm25 *store.BleveBM25Index) (*Validator, error) {
	// Load configuration
	cfg, err := config.Load(projectRoot)
	if err != nil {
		cfg = config.NewConfig()
	}

	metadataPath := filepath.Join(dataDir, "metadata.db")
	metadata, err := store.NewSQLiteStore(metadataPath)
	if err != nil {
		bm25.Close()
		return nil, fmt.Errorf("failed to open metadata: %w", err)
	}

	return newValidatorWithStores(ctx, projectRoot, cfg, metadata, bm25)
}

// newValidatorWithStores creates validator with pre-opened stores.
func newValidatorWithStores(ctx context.Context, projectRoot string, cfg *config.Config, metadata *store.SQLiteStore, bm25 store.BM25Index) (*Validator, error) {
	dataDir := filepath.Join(projectRoot, ".amanmcp")

	// Initialize embedder
	embed.SetMLXConfig(embed.MLXServerConfig{
		Endpoint: cfg.Embeddings.MLXEndpoint,
		Model:    cfg.Embeddings.MLXModel,
	})

	provider := embed.ParseProvider(cfg.Embeddings.Provider)
	embedder, err := embed.NewEmbedder(ctx, provider, cfg.Embeddings.Model)
	if err != nil {
		bm25.Close()
		metadata.Close()
		return nil, fmt.Errorf("failed to create embedder: %w", err)
	}

	// Initialize vector store
	vectorPath := filepath.Join(dataDir, "vectors.hnsw")
	dimensions := embedder.Dimensions()
	vectorConfig := store.DefaultVectorStoreConfig(dimensions)
	vector, err := store.NewHNSWStore(vectorConfig)
	if err != nil {
		embedder.Close()
		bm25.Close()
		metadata.Close()
		return nil, fmt.Errorf("failed to create vector store: %w", err)
	}

	// Load vectors
	if _, err := os.Stat(vectorPath); err == nil {
		_ = vector.Load(vectorPath) // Non-fatal, continue with empty vectors if load fails
	}

	// Create search engine
	engineConfig := search.DefaultConfig()
	if cfg.Search.BM25Weight > 0 || cfg.Search.SemanticWeight > 0 {
		engineConfig.DefaultWeights = search.Weights{
			BM25:     cfg.Search.BM25Weight,
			Semantic: cfg.Search.SemanticWeight,
		}
	}

	engine := search.New(bm25, vector, embedder, metadata, engineConfig,
		search.WithMultiQuerySearch(search.NewPatternDecomposer()))

	// Create MCP server
	server, err := mcp.NewServer(engine, metadata, embedder, cfg, projectRoot)
	if err != nil {
		embedder.Close()
		bm25.Close()
		metadata.Close()
		vector.Close()
		return nil, fmt.Errorf("failed to create MCP server: %w", err)
	}

	return &Validator{
		server:   server,
		embedder: embedder,
	}, nil
}

// Close releases resources.
func (v *Validator) Close() error {
	if v.embedder != nil {
		v.embedder.Close()
	}
	return nil
}

// RunQuery executes a single query and returns the result.
func (v *Validator) RunQuery(ctx context.Context, spec QuerySpec) TestResult {
	start := time.Now()
	result := TestResult{
		Spec:      spec,
		MatchedAt: -1,
	}

	// Build tool args
	args := map[string]any{
		"query": spec.Query,
		"limit": 10,
	}

	// Call the appropriate tool
	resp, err := v.server.CallTool(ctx, spec.Tool, args)
	result.Duration = time.Since(start)

	if err != nil {
		// For negative tests, errors are acceptable
		if spec.Tier == 0 {
			result.Passed = true
		} else {
			result.Error = err.Error()
		}
		return result
	}

	// Parse response to extract file paths
	result.TopResults = extractFilePaths(resp)

	// Check if expected files appear in results
	if len(spec.Expected) == 0 {
		// Negative test - just needs to not crash
		result.Passed = true
	} else {
		result.Passed, result.MatchedAt = checkExpected(result.TopResults, spec.Expected)
	}

	return result
}

// RunAll executes all validation queries and returns results.
func (v *Validator) RunAll(ctx context.Context) *ValidationResult {
	result := &ValidationResult{
		Timestamp:  time.Now(),
		Embedder:   v.embedder.ModelName(),
	}

	// Run Tier 1
	for _, spec := range Tier1Queries() {
		tr := v.RunQuery(ctx, spec)
		result.Tier1 = append(result.Tier1, tr)
		result.Tier1Total++
		if tr.Passed {
			result.Tier1Pass++
		}
	}

	// Run Tier 2
	for _, spec := range Tier2Queries() {
		tr := v.RunQuery(ctx, spec)
		result.Tier2 = append(result.Tier2, tr)
		result.Tier2Total++
		if tr.Passed {
			result.Tier2Pass++
		}
	}

	// Run Negative
	for _, spec := range NegativeQueries() {
		tr := v.RunQuery(ctx, spec)
		result.Negative = append(result.Negative, tr)
		result.NegTotal++
		if tr.Passed {
			result.NegPass++
		}
	}

	return result
}

// extractFilePaths extracts file paths from MCP tool response.
func extractFilePaths(resp any) []string {
	var paths []string

	// Response is typically a markdown string
	text, ok := resp.(string)
	if !ok {
		// Try JSON
		if data, err := json.Marshal(resp); err == nil {
			text = string(data)
		}
	}

	// Extract file paths from markdown format
	// Looking for patterns like: internal/search/engine.go:42
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		// Look for file_path in JSON or markdown
		if strings.Contains(line, "file_path") {
			// JSON format
			if idx := strings.Index(line, `"file_path":`); idx >= 0 {
				rest := line[idx+12:]
				if start := strings.Index(rest, `"`); start >= 0 {
					if end := strings.Index(rest[start+1:], `"`); end >= 0 {
						paths = append(paths, rest[start+1:start+1+end])
					}
				}
			}
		} else if strings.Contains(line, ".go:") || strings.Contains(line, ".md:") {
			// Markdown format: **internal/search/engine.go:42-78**
			// Or: `internal/search/engine.go:42`
			for _, part := range strings.Fields(line) {
				part = strings.Trim(part, "*`[]()#")
				if strings.Contains(part, "/") && (strings.Contains(part, ".go") || strings.Contains(part, ".md")) {
					// Remove line numbers
					if idx := strings.Index(part, ":"); idx > 0 {
						part = part[:idx]
					}
					paths = append(paths, part)
				}
			}
		}
	}

	return paths
}

// checkExpected verifies if any expected file appears in results.
func checkExpected(results []string, expected []string) (bool, int) {
	for i, path := range results {
		for _, exp := range expected {
			if strings.HasPrefix(path, exp) || strings.Contains(path, exp) {
				return true, i
			}
		}
	}
	return false, -1
}
