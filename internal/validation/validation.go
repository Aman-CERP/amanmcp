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
	"errors"
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

// ExpectedResult defines graded evidence for a query result.
type ExpectedResult struct {
	Path      string `yaml:"path"`                 // Indexed file path or prefix
	Symbol    string `yaml:"symbol,omitempty"`     // Optional symbol expected within the path
	StartLine int    `yaml:"start_line,omitempty"` // Optional source line expectation
	EndLine   int    `yaml:"end_line,omitempty"`   // Optional source line expectation
	Page      int    `yaml:"page,omitempty"`       // Optional exact PDF page expectation
	PageStart int    `yaml:"page_start,omitempty"` // Optional PDF page range start
	PageEnd   int    `yaml:"page_end,omitempty"`   // Optional PDF page range end
	Grade     int    `yaml:"grade"`                // Relevance grade: 0, 1, 2, or 3
	Rationale string `yaml:"rationale,omitempty"`  // Why this result earns the grade
}

// QuerySpec defines a test query with expected results.
type QuerySpec struct {
	ID              string            `yaml:"id"`                 // e.g., "T1-Q7"
	Name            string            `yaml:"name"`               // Human-readable name
	Query           string            `yaml:"query"`              // The search query
	Tool            string            `yaml:"tool"`               // "search", "search_code", or "search_docs"
	Profile         string            `yaml:"profile,omitempty"`  // Optional retrieval profile override
	Scope           []string          `yaml:"scope,omitempty"`    // Optional path scope prefixes
	Mode            string            `yaml:"mode,omitempty"`     // Optional search mode, e.g. decisions
	Class           string            `yaml:"class"`              // F37 query class
	Job             string            `yaml:"job"`                // Product job this query validates
	Expected        []string          `yaml:"expected"`           // Legacy path/prefix compatibility, mapped to grade-3 evidence
	ExpectedResults []ExpectedResult  `yaml:"expected_results"`   // F37 graded expected evidence
	Metadata        map[string]string `yaml:"metadata,omitempty"` // Query metadata, e.g. content_type: pdf
	Holdout         bool              `yaml:"holdout"`            // Excluded from normal tuning when true
	Source          string            `yaml:"source"`             // Query provenance
	Notes           string            `yaml:"notes"`              // Optional explanation for maintainers
	Tier            int               `yaml:"-"`                  // Set programmatically based on section
}

// QueryConfig holds all validation queries loaded from YAML.
type QueryConfig struct {
	Tier1    []QuerySpec `yaml:"tier1"`
	Tier2    []QuerySpec `yaml:"tier2"`
	Negative []QuerySpec `yaml:"negative"`
	Graded   []QuerySpec `yaml:"graded"`
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

		cfg, err := parseQueryConfig(data)
		if err != nil {
			queriesErr = fmt.Errorf("failed to parse queries YAML: %w", err)
			return
		}
		queriesData = cfg
	})

	return queriesData, queriesErr
}

func parseQueryConfig(data []byte) (*QueryConfig, error) {
	if err := validateRawQueryYAML(data); err != nil {
		return nil, err
	}

	var cfg QueryConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Set tier values programmatically.
	for i := range cfg.Tier1 {
		cfg.Tier1[i].Tier = 1
	}
	for i := range cfg.Tier2 {
		cfg.Tier2[i].Tier = 2
	}
	for i := range cfg.Negative {
		cfg.Negative[i].Tier = 0
	}
	for i := range cfg.Graded {
		cfg.Graded[i].Tier = 3
	}

	normalizeLegacyExpected(&cfg)
	if err := validateQueryConfig(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ResetQueries clears the cached queries (for testing).
func ResetQueries() {
	queriesOnce = sync.Once{}
	queriesData = nil
	queriesErr = nil
}

var (
	allowedQueryTools = map[string]struct{}{
		"search":      {},
		"search_code": {},
		"search_docs": {},
	}
	allowedQueryClasses = map[string]struct{}{
		"exact_identifier":        {},
		"path_lookup":             {},
		"quoted_string":           {},
		"config_error":            {},
		"natural_language_intent": {},
		"caller_callee":           {},
		"impact_analysis":         {},
		"docs_to_code":            {},
		"test_to_implementation":  {},
		"adr_to_code":             {},
		"cross_file_subsystem":    {},
		"negative_adversarial":    {},
	}
	allowedQueryJobs = map[string]struct{}{
		"code":            {},
		"project_memory":  {},
		"decision_lookup": {},
		"pm_inspection":   {},
		"exact_lookup":    {},
		"general":         {},
	}
)

func validateRawQueryYAML(data []byte) error {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return err
	}
	if len(root.Content) == 0 {
		return fmt.Errorf("query corpus is empty")
	}

	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return fmt.Errorf("query corpus root must be a mapping")
	}

	for i := 0; i < len(doc.Content); i += 2 {
		section := doc.Content[i].Value
		if section != "tier1" && section != "tier2" && section != "negative" && section != "graded" {
			continue
		}
		seq := doc.Content[i+1]
		if seq.Kind != yaml.SequenceNode {
			return fmt.Errorf("%s must be a sequence", section)
		}
		for _, item := range seq.Content {
			if item.Kind != yaml.MappingNode {
				return fmt.Errorf("%s entry must be a mapping", section)
			}
			if err := validateRawQueryNode(section, item); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateRawQueryNode(section string, node *yaml.Node) error {
	id := nodeFieldValue(node, "id")
	if id == "" {
		id = "<missing id>"
	}

	holdout := nodeField(node, "holdout")
	if holdout == nil {
		return fmt.Errorf("%s: holdout is required", id)
	}
	if holdout.Kind != yaml.ScalarNode || holdout.Tag != "!!bool" {
		return fmt.Errorf("%s: holdout must be a boolean", id)
	}

	required := []string{"id", "name", "query", "tool", "class", "job", "source"}
	for _, field := range required {
		if nodeFieldValue(node, field) == "" {
			if section == "negative" && field == "query" {
				continue
			}
			return fmt.Errorf("%s: %s is required", id, field)
		}
	}
	return nil
}

func nodeField(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func nodeFieldValue(node *yaml.Node, key string) string {
	value := nodeField(node, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}
	return strings.TrimSpace(value.Value)
}

func normalizeLegacyExpected(cfg *QueryConfig) {
	for _, specs := range [][]QuerySpec{cfg.Tier1, cfg.Tier2, cfg.Negative, cfg.Graded} {
		for i := range specs {
			if len(specs[i].ExpectedResults) > 0 || len(specs[i].Expected) == 0 {
				continue
			}
			for _, path := range specs[i].Expected {
				specs[i].ExpectedResults = append(specs[i].ExpectedResults, ExpectedResult{
					Path:      path,
					Grade:     3,
					Rationale: "legacy expected compatibility mapping",
				})
			}
		}
	}
}

func validateQueryConfig(cfg *QueryConfig) error {
	seenIDs := make(map[string]struct{})
	for _, spec := range allQuerySpecs(cfg) {
		if _, exists := seenIDs[spec.ID]; exists {
			return fmt.Errorf("%s: duplicate query id", spec.ID)
		}
		seenIDs[spec.ID] = struct{}{}

		if _, ok := allowedQueryTools[spec.Tool]; !ok {
			return fmt.Errorf("%s: unsupported tool %q", spec.ID, spec.Tool)
		}
		if _, err := search.ParseProfile(spec.Profile); err != nil {
			return fmt.Errorf("%s: invalid profile: %w", spec.ID, err)
		}
		if _, err := search.ParseMode(spec.Mode); err != nil {
			return fmt.Errorf("%s: invalid mode: %w", spec.ID, err)
		}
		for _, scope := range spec.Scope {
			if strings.TrimSpace(scope) == "" {
				return fmt.Errorf("%s: scope entries must be non-empty", spec.ID)
			}
		}
		if _, ok := allowedQueryClasses[spec.Class]; !ok {
			return fmt.Errorf("%s: unsupported class %q", spec.ID, spec.Class)
		}
		if _, ok := allowedQueryJobs[spec.Job]; !ok {
			return fmt.Errorf("%s: unsupported job %q", spec.ID, spec.Job)
		}

		for _, expected := range spec.ExpectedResults {
			if expected.Grade < 0 || expected.Grade > 3 {
				return fmt.Errorf("%s: invalid expected result grade %d", spec.ID, expected.Grade)
			}
			if strings.TrimSpace(expected.Path) == "" {
				return fmt.Errorf("%s: expected result path is required", spec.ID)
			}
			if expected.Page < 0 || expected.PageStart < 0 || expected.PageEnd < 0 {
				return fmt.Errorf("%s: expected result page values must be non-negative", spec.ID)
			}
			if expected.PageStart > 0 && expected.PageEnd > 0 && expected.PageEnd < expected.PageStart {
				return fmt.Errorf("%s: expected result page_end must be greater than or equal to page_start", spec.ID)
			}
		}

		if spec.Class != "negative_adversarial" && len(spec.ExpectedResults) == 0 {
			return fmt.Errorf("%s: graded non-negative query requires expected evidence", spec.ID)
		}
	}
	return nil
}

func allQuerySpecs(cfg *QueryConfig) []QuerySpec {
	total := len(cfg.Tier1) + len(cfg.Tier2) + len(cfg.Negative) + len(cfg.Graded)
	all := make([]QuerySpec, 0, total)
	all = append(all, cfg.Tier1...)
	all = append(all, cfg.Tier2...)
	all = append(all, cfg.Negative...)
	all = append(all, cfg.Graded...)
	return all
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
	Timestamp   time.Time    `json:"timestamp"`
	Tier1       []TestResult `json:"tier1"`
	Tier2       []TestResult `json:"tier2"`
	Negative    []TestResult `json:"negative"`
	Tier1Pass   int          `json:"tier1_pass"`
	Tier1Total  int          `json:"tier1_total"`
	Tier2Pass   int          `json:"tier2_pass"`
	Tier2Total  int          `json:"tier2_total"`
	NegPass     int          `json:"negative_pass"`
	NegTotal    int          `json:"negative_total"`
	Embedder    string       `json:"embedder"`
	IndexChunks int          `json:"index_chunks"`
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
	engine   search.SearchEngine
	embedder embed.Embedder
	config   *config.Config
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

	provider, model := validationEmbedderSelection(ctx, cfg, metadata)
	embedder, err := embed.NewEmbedder(ctx, provider, model)
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
	engineConfig.MetadataRules = cfg.SearchMetadataRules()
	engineConfig.ProfileRules = cfg.SearchProfileRules()
	if cfg.Search.BM25Weight > 0 || cfg.Search.SemanticWeight > 0 {
		engineConfig.DefaultWeights = search.Weights{
			BM25:     cfg.Search.BM25Weight,
			Semantic: cfg.Search.SemanticWeight,
		}
	}
	engineConfig.RerankerPolicy = search.RerankerPolicy(cfg.Search.Reranker.Policy)

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
		engine:   engine,
		embedder: embedder,
		config:   cfg,
	}, nil
}

// StructuredSearchResult captures the structured MCP search output and byte cost.
type StructuredSearchResult struct {
	Results       []mcp.SearchResultOutput
	ResponseBytes int
}

// RunStructuredQuery executes a query through the same search options used by MCP
// typed handlers and returns the structured output shape they expose.
func (v *Validator) RunStructuredQuery(ctx context.Context, spec QuerySpec) (StructuredSearchResult, error) {
	if strings.TrimSpace(spec.Query) == "" {
		if spec.Class == "negative_adversarial" || spec.Tier == 0 {
			return StructuredSearchResult{Results: []mcp.SearchResultOutput{}, ResponseBytes: len(`{"results":[]}`)}, nil
		}
		return StructuredSearchResult{}, fmt.Errorf("query parameter is required")
	}

	opts := search.SearchOptions{Limit: 10}
	switch spec.Tool {
	case "search":
	case "search_code":
		opts.Filter = "code"
		opts.Profile = search.ProfileCode
	case "search_docs":
		opts.Filter = "docs"
		opts.Profile = search.ProfileProjectMemory
	default:
		return StructuredSearchResult{}, fmt.Errorf("unsupported search tool %q", spec.Tool)
	}

	if spec.Profile != "" {
		profile, err := search.ParseProfile(spec.Profile)
		if err != nil {
			return StructuredSearchResult{}, err
		}
		opts.Profile = profile
	}
	if spec.Mode != "" {
		mode, err := search.ParseMode(spec.Mode)
		if err != nil {
			return StructuredSearchResult{}, err
		}
		opts.Mode = mode
	}
	opts.Scopes = append([]string(nil), spec.Scope...)
	opts.Scopes = v.defaultDecisionScopes(opts)

	results, err := v.engine.Search(ctx, spec.Query, opts)
	if err != nil {
		return StructuredSearchResult{}, err
	}
	output := v.server.BuildSearchOutput(spec.Tool, spec.Query, opts, results, nil)
	data, err := json.Marshal(output)
	if err != nil {
		return StructuredSearchResult{}, fmt.Errorf("failed to encode structured search output: %w", err)
	}
	return StructuredSearchResult{Results: output.Results, ResponseBytes: len(data)}, nil
}

func (v *Validator) defaultDecisionScopes(opts search.SearchOptions) []string {
	if len(opts.Scopes) > 0 {
		return opts.Scopes
	}
	if opts.Mode != search.SearchModeDecisions && opts.Mode != search.SearchModeDecisionHistory {
		return opts.Scopes
	}

	rules := search.DefaultMetadataRules()
	if v.config != nil {
		rules = v.config.SearchMetadataRules()
	}
	return search.DecisionScopePrefixes(rules)
}

func validationEmbedderSelection(ctx context.Context, cfg *config.Config, metadata *store.SQLiteStore) (embed.ProviderType, string) {
	provider := embed.ParseProvider(cfg.Embeddings.Provider)
	model := cfg.Embeddings.Model

	indexModel, err := metadata.GetState(ctx, store.StateKeyIndexModel)
	if err != nil || indexModel == "" {
		return provider, model
	}

	switch store.InferBackendFromModel(indexModel) {
	case "static":
		return embed.ProviderStatic, indexModel
	case "mlx":
		return embed.ProviderMLX, indexModel
	case "ollama":
		return embed.ProviderOllama, indexModel
	default:
		return provider, model
	}
}

// Close releases resources.
func (v *Validator) Close() error {
	var errs []error
	if v.engine != nil {
		if err := v.engine.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if v.embedder != nil {
		if err := v.embedder.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed to close validator: %w", errors.Join(errs...))
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
	if spec.Profile != "" {
		args["profile"] = spec.Profile
	}
	if len(spec.Scope) > 0 {
		scope := make([]interface{}, 0, len(spec.Scope))
		for _, value := range spec.Scope {
			scope = append(scope, value)
		}
		args["scope"] = scope
	}
	if spec.Mode != "" {
		args["mode"] = spec.Mode
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
		Timestamp: time.Now(),
		Embedder:  v.embedder.ModelName(),
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
		} else if strings.HasPrefix(strings.TrimSpace(line), "### ") {
			// Markdown docs format: ### 1. README.md (score: 1.00)
			fields := strings.Fields(strings.TrimSpace(line))
			if len(fields) >= 3 {
				if path, ok := normalizeResultPath(fields[2]); ok {
					paths = append(paths, path)
				}
			}
		} else if strings.Contains(line, ".go:") || strings.Contains(line, ".md:") {
			// Markdown format: **internal/search/engine.go:42-78**
			// Or: `internal/search/engine.go:42`
			for _, part := range strings.Fields(line) {
				if path, ok := normalizeResultPath(part); ok {
					paths = append(paths, path)
				}
			}
		}
	}

	return paths
}

func normalizeResultPath(value string) (string, bool) {
	value = strings.Trim(value, "*`[]()#")
	if idx := strings.Index(value, ":"); idx > 0 {
		value = value[:idx]
	}
	if value == "" {
		return "", false
	}
	switch {
	case strings.HasSuffix(value, ".go"),
		strings.HasSuffix(value, ".md"),
		strings.HasSuffix(value, ".yaml"),
		strings.HasSuffix(value, ".yml"),
		strings.HasSuffix(value, ".json"):
		return value, true
	default:
		return "", false
	}
}

// checkExpected verifies if any expected file appears in results.
func checkExpected(results []string, expected []string) (bool, int) {
	for i, path := range results {
		for _, exp := range expected {
			if matchesExpectedPath(path, exp) {
				return true, i
			}
		}
	}
	return false, -1
}

func matchesExpectedPath(got, want string) bool {
	got = strings.Trim(strings.TrimSpace(got), "/")
	want = strings.Trim(strings.TrimSpace(want), "/")
	if got == "" || want == "" {
		return false
	}
	return got == want || strings.HasPrefix(got, want+"/")
}
