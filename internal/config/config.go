package config

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ProjectType represents the type of project detected.
type ProjectType string

const (
	ProjectTypeGo      ProjectType = "go"
	ProjectTypeNode    ProjectType = "node"
	ProjectTypePython  ProjectType = "python"
	ProjectTypeUnknown ProjectType = "unknown"
)

// Config represents the complete AmanMCP configuration.
// It mirrors the schema defined in specification.md Section 5.
type Config struct {
	Version     int               `yaml:"version" json:"version"`
	Paths       PathsConfig       `yaml:"paths" json:"paths"`
	Search      SearchConfig      `yaml:"search" json:"search"`
	Embeddings  EmbeddingsConfig  `yaml:"embeddings" json:"embeddings"`
	Contextual  ContextualConfig  `yaml:"contextual" json:"contextual"`
	Performance PerformanceConfig `yaml:"performance" json:"performance"`
	Server      ServerConfig      `yaml:"server" json:"server"`
	Submodules  SubmoduleConfig   `yaml:"submodules" json:"submodules"`
	Sessions    SessionsConfig    `yaml:"sessions" json:"sessions"`
	Compaction  CompactionConfig  `yaml:"compaction" json:"compaction"`
}

// PathsConfig configures which paths to include and exclude.
type PathsConfig struct {
	Include []string `yaml:"include" json:"include"`
	Exclude []string `yaml:"exclude" json:"exclude"`
}

// SearchConfig configures hybrid search parameters.
// Weights and RRF constant are configurable via:
//   1. User config (~/.config/amanmcp/config.yaml) - personal defaults
//   2. Project config (.amanmcp.yaml) - per-repo tuning
//   3. Env vars (AMANMCP_BM25_WEIGHT, AMANMCP_SEMANTIC_WEIGHT, AMANMCP_RRF_CONSTANT) - highest priority
type SearchConfig struct {
	// BM25Weight is the weight for BM25 keyword matching (0.0-1.0).
	// Must sum to 1.0 with SemanticWeight.
	BM25Weight float64 `yaml:"bm25_weight" json:"bm25_weight"`

	// SemanticWeight is the weight for semantic similarity (0.0-1.0).
	// Must sum to 1.0 with BM25Weight.
	SemanticWeight float64 `yaml:"semantic_weight" json:"semantic_weight"`

	// RRFConstant is the RRF fusion smoothing parameter (k).
	// Default: 60 (industry standard used by Azure AI Search, OpenSearch).
	// Higher values reduce the impact of rank differences.
	RRFConstant int `yaml:"rrf_constant" json:"rrf_constant"`

	// BM25Backend selects the BM25 index backend.
	// Options: "sqlite" (default, concurrent access) or "bleve" (legacy, single-process)
	// SQLite FTS5 with WAL mode enables concurrent multi-process access (BUG-064 fix).
	BM25Backend string `yaml:"bm25_backend" json:"bm25_backend"`

	ChunkSize    int `yaml:"chunk_size" json:"chunk_size"`
	ChunkOverlap int `yaml:"chunk_overlap" json:"chunk_overlap"`
	MaxResults   int `yaml:"max_results" json:"max_results"`
}

// EmbeddingsConfig configures the embedding provider.
type EmbeddingsConfig struct {
	Provider             string        `yaml:"provider" json:"provider"`
	Model                string        `yaml:"model" json:"model"`
	Dimensions           int           `yaml:"dimensions" json:"dimensions"`
	BatchSize            int           `yaml:"batch_size" json:"batch_size"`
	ModelDownloadTimeout time.Duration `yaml:"model_download_timeout" json:"model_download_timeout"`

	// MLX settings (opt-in on Apple Silicon via --backend=mlx, ~1.7x faster throughput)
	MLXEndpoint string `yaml:"mlx_endpoint" json:"mlx_endpoint"` // MLX server endpoint (default: http://localhost:9659)
	MLXModel    string `yaml:"mlx_model" json:"mlx_model"`       // MLX model size: small (0.6B), medium (4B), large (8B)

	// Ollama settings (default, cross-platform)
	OllamaHost string `yaml:"ollama_host" json:"ollama_host"` // Ollama API endpoint (default: http://localhost:11434)

	// Thermal management settings for sustained GPU workloads (Apple Silicon)
	// These help prevent timeout failures during long indexing operations
	InterBatchDelay        string  `yaml:"inter_batch_delay" json:"inter_batch_delay"`               // Pause between batches (e.g., "200ms", "0" = disabled)
	TimeoutProgression     float64 `yaml:"timeout_progression" json:"timeout_progression"`           // Timeout multiplier for later batches (1.0-3.0, default: 1.0)
	RetryTimeoutMultiplier float64 `yaml:"retry_timeout_multiplier" json:"retry_timeout_multiplier"` // Timeout multiplier per retry (1.0-2.0, default: 1.0)
}

// PerformanceConfig configures performance tuning options.
type PerformanceConfig struct {
	MaxFiles      int    `yaml:"max_files" json:"max_files"`
	IndexWorkers  int    `yaml:"index_workers" json:"index_workers"`
	WatchDebounce string `yaml:"watch_debounce" json:"watch_debounce"`
	CacheSize     int    `yaml:"cache_size" json:"cache_size"`
	MemoryLimit   string `yaml:"memory_limit" json:"memory_limit"`
	Quantization  string `yaml:"quantization" json:"quantization"`
	SQLiteCacheMB int    `yaml:"sqlite_cache_mb" json:"sqlite_cache_mb"` // SQLite cache size in MB (default: 64)
}

// ServerConfig configures the MCP server.
type ServerConfig struct {
	Transport string `yaml:"transport" json:"transport"`
	Port      int    `yaml:"port" json:"port"`
	LogLevel  string `yaml:"log_level" json:"log_level"`
}

// SubmoduleConfig configures git submodule discovery.
type SubmoduleConfig struct {
	// Enabled enables submodule discovery (default: false, opt-in).
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Recursive enables discovery of nested submodules (default: true).
	Recursive bool `yaml:"recursive" json:"recursive"`
	// Include specifies submodules to include (empty = all).
	Include []string `yaml:"include" json:"include"`
	// Exclude specifies submodules to exclude.
	Exclude []string `yaml:"exclude" json:"exclude"`
}

// SessionsConfig configures session management.
type SessionsConfig struct {
	// StoragePath is the directory where sessions are stored.
	// Defaults to ~/.amanmcp/sessions
	StoragePath string `yaml:"storage_path" json:"storage_path"`
	// AutoSave enables automatic session save on shutdown.
	// Defaults to true.
	AutoSave bool `yaml:"auto_save" json:"auto_save"`
	// MaxSessions is the maximum number of sessions allowed.
	// Defaults to 20.
	MaxSessions int `yaml:"max_sessions" json:"max_sessions"`
}

// CompactionConfig configures automatic background compaction.
// FEAT-AI3: Lazy background compaction for HNSW vector index.
type CompactionConfig struct {
	// Enabled enables automatic background compaction.
	// Default: true
	Enabled bool `yaml:"enabled" json:"enabled"`
	// OrphanThreshold is the orphan ratio that triggers compaction eligibility.
	// When orphans/total > threshold, compaction becomes eligible.
	// Range: 0.0-1.0, Default: 0.2 (20%)
	OrphanThreshold float64 `yaml:"orphan_threshold" json:"orphan_threshold"`
	// MinOrphanCount is the minimum number of orphans before considering compaction.
	// Prevents compaction for small indexes with high ratios.
	// Default: 100
	MinOrphanCount int `yaml:"min_orphan_count" json:"min_orphan_count"`
	// IdleTimeout is how long without searches before the project is considered idle.
	// Compaction only runs during idle periods.
	// Default: "30s"
	IdleTimeout string `yaml:"idle_timeout" json:"idle_timeout"`
	// Cooldown is the minimum time between compactions for the same project.
	// Prevents excessive compaction cycles.
	// Default: "1h"
	Cooldown string `yaml:"cooldown" json:"cooldown"`
}

// ContextualConfig configures CR-1 Contextual Retrieval.
// Uses LLM to generate context for chunks at index time.
// See: https://www.anthropic.com/news/contextual-retrieval
type ContextualConfig struct {
	// Enabled enables contextual retrieval (default: true).
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Model is the Ollama model for context generation (default: qwen3:0.6b).
	Model string `yaml:"model" json:"model"`
	// Timeout is the per-chunk timeout (default: 5s).
	Timeout string `yaml:"timeout" json:"timeout"`
	// BatchSize is chunks per batch for prompt caching (default: 8).
	BatchSize int `yaml:"batch_size" json:"batch_size"`
	// FallbackOnly uses pattern-based fallback only, no LLM (default: false).
	FallbackOnly bool `yaml:"fallback_only" json:"fallback_only"`
	// CodeChunks enables context generation for code chunks (default: false).
	// When false, only markdown/docs get contextual prefixes.
	// RCA-015: Disabling for code improves vector search quality with small models.
	CodeChunks bool `yaml:"code_chunks" json:"code_chunks"`
}

// defaultExcludePatterns are always excluded.
var defaultExcludePatterns = []string{
	"**/node_modules/**",
	"**/.git/**",
	"**/vendor/**",
	"**/__pycache__/**",
	"**/dist/**",
	"**/build/**",
	"**/*.min.js",
	"**/*.min.css",
	"**/package-lock.json",
	"**/yarn.lock",
	"**/pnpm-lock.yaml",
	"**/go.sum",
}

// NewConfig creates a new Config with sensible defaults.
func NewConfig() *Config {
	return &Config{
		Version: 1,
		Paths: PathsConfig{
			Include: []string{},
			Exclude: defaultExcludePatterns,
		},
		Search: SearchConfig{
			// RCA-015: Favor BM25 over semantic search until vector search is fixed
			// Vector search returns wrong results; BM25 works correctly for code
			BM25Weight:     0.65,
			SemanticWeight: 0.35,
			// RRF constant k=60 is industry standard (Azure AI Search, OpenSearch)
			RRFConstant: 60,
			// BM25Backend: SQLite FTS5 is default for concurrent multi-process access (BUG-064 fix)
			BM25Backend:  "sqlite",
			ChunkSize:    1500,
			ChunkOverlap: 200,
			MaxResults:   20,
		},
		Embeddings: EmbeddingsConfig{
			Provider:             "", // Empty triggers auto-detection: MLX (Apple Silicon) → Ollama → Static
			Model:                "qwen3-embedding:8b",
			Dimensions:           0, // Auto-detect from embedder
			BatchSize:            32,
			ModelDownloadTimeout: 10 * time.Minute, // Large models may take time on slow networks
			// MLX settings (used when provider is "mlx" or auto-detected)
			MLXEndpoint: "", // Empty uses default http://localhost:9659
			MLXModel:    "", // Empty uses default "small" (0.6B, 1024 dims) - TASK-MEM1
			// Ollama settings (used when provider is "ollama")
			OllamaHost: "", // Empty uses default http://localhost:11434
			// Thermal management defaults for large codebases (98% of users)
			InterBatchDelay:        "",  // Disabled by default (empty = 0)
			TimeoutProgression:     1.5, // 50% increase per 1000 chunks for thermal adaptation
			RetryTimeoutMultiplier: 1.0, // No multiplier by default
		},
		Performance: PerformanceConfig{
			MaxFiles:      100000,
			IndexWorkers:  runtime.NumCPU(),
			WatchDebounce: "500ms",
			CacheSize:     1000,
			MemoryLimit:   "auto",
			Quantization:  "F16",
			SQLiteCacheMB: 64, // 64MB SQLite cache
		},
		Server: ServerConfig{
			Transport: "stdio",
			Port:      8765,
			LogLevel:  "debug", // Debug by default to aid troubleshooting
		},
		Submodules: SubmoduleConfig{
			Enabled:   false, // Opt-in by default
			Recursive: true,  // Index nested submodules when enabled
			Include:   nil,
			Exclude:   nil,
		},
		Sessions: SessionsConfig{
			StoragePath: defaultSessionsPath(),
			AutoSave:    true,
			MaxSessions: 20,
		},
		Compaction: CompactionConfig{
			Enabled:         true,  // Zero-config: automatic compaction enabled by default
			OrphanThreshold: 0.2,   // Trigger when >20% orphans
			MinOrphanCount:  100,   // Skip small indexes
			IdleTimeout:     "30s", // Wait 30s without searches
			Cooldown:        "1h",  // At most once per hour per project
		},
		Contextual: ContextualConfig{
			Enabled:      true,           // CR-1: Enabled by default for 67% error reduction
			Model:        "qwen3:0.6b",   // Small, fast model (~50ms per chunk)
			Timeout:      "5s",           // Per-chunk timeout
			BatchSize:    8,              // Chunks per batch for prompt caching
			FallbackOnly: false,          // Use LLM when available
			CodeChunks:   false,          // RCA-015: Skip prefixes for code (improves vector search)
		},
	}
}

// defaultSessionsPath returns the default sessions storage path.
func defaultSessionsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback to temp directory
		return filepath.Join(os.TempDir(), ".amanmcp", "sessions")
	}
	return filepath.Join(home, ".amanmcp", "sessions")
}

// GetUserConfigPath returns the path to the user/global configuration file.
// It follows XDG Base Directory specification:
//   - $XDG_CONFIG_HOME/amanmcp/config.yaml (if XDG_CONFIG_HOME is set)
//   - ~/.config/amanmcp/config.yaml (default)
func GetUserConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "amanmcp", "config.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback - should rarely happen
		return filepath.Join(os.TempDir(), ".config", "amanmcp", "config.yaml")
	}
	return filepath.Join(home, ".config", "amanmcp", "config.yaml")
}

// GetUserConfigDir returns the directory containing the user configuration.
func GetUserConfigDir() string {
	return filepath.Dir(GetUserConfigPath())
}

// UserConfigExists returns true if the user configuration file exists.
func UserConfigExists() bool {
	return fileExists(GetUserConfigPath())
}

// loadUserConfig loads the user/global configuration file if it exists.
// Returns nil config and nil error if the file doesn't exist (that's OK).
func loadUserConfig() (*Config, error) {
	configPath := GetUserConfigPath()

	// Check if file exists
	if !fileExists(configPath) {
		return nil, nil // No user config is fine
	}

	// Load the config
	cfg := NewConfig()
	if err := cfg.loadYAML(configPath); err != nil {
		return nil, fmt.Errorf("failed to load user config from %s: %w", configPath, err)
	}

	return cfg, nil
}

// Load loads configuration from the specified directory.
// It applies configuration in order of increasing precedence:
//  1. Hardcoded defaults
//  2. User/global config (~/.config/amanmcp/config.yaml)
//  3. Project config (.amanmcp.yaml in project root)
//  4. Environment variables (AMANMCP_*)
func Load(dir string) (*Config, error) {
	cfg := NewConfig()

	// Step 1: Load user/global config (if exists)
	if userCfg, err := loadUserConfig(); err != nil {
		return nil, fmt.Errorf("failed to load user config: %w", err)
	} else if userCfg != nil {
		cfg.mergeWith(userCfg)
	}

	// Step 2: Load project config (overrides user config)
	if err := cfg.loadFromFile(dir); err != nil {
		return nil, err
	}

	// Step 3: Apply environment variable overrides (highest precedence)
	cfg.applyEnvOverrides()

	// Step 4: Validate the final configuration (DEBT-018)
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// loadFromFile attempts to load configuration from .amanmcp.yaml or .amanmcp.yml.
func (c *Config) loadFromFile(dir string) error {
	// Try .yaml first (takes precedence)
	yamlPath := filepath.Join(dir, ".amanmcp.yaml")
	if _, err := os.Stat(yamlPath); err == nil {
		return c.loadYAML(yamlPath)
	}

	// Try .yml as fallback
	ymlPath := filepath.Join(dir, ".amanmcp.yml")
	if _, err := os.Stat(ymlPath); err == nil {
		return c.loadYAML(ymlPath)
	}

	// No config file is fine - use defaults
	return nil
}

// loadYAML loads and merges configuration from a YAML file.
func (c *Config) loadYAML(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	// Use a temporary struct for parsing to detect type errors
	var parsed Config
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	// Merge parsed values with defaults (only non-zero values)
	c.mergeWith(&parsed)
	return nil
}

// mergeWith merges non-zero values from other into c.
func (c *Config) mergeWith(other *Config) {
	if other.Version != 0 {
		c.Version = other.Version
	}

	// Paths
	if len(other.Paths.Include) > 0 {
		c.Paths.Include = other.Paths.Include
	}
	if len(other.Paths.Exclude) > 0 {
		// Merge with defaults rather than replace
		c.Paths.Exclude = append(c.Paths.Exclude, other.Paths.Exclude...)
	}

	// Search weights and RRF constant
	// Note: 0 is not a practical value for weights, so we only merge non-zero values
	if other.Search.BM25Weight != 0 {
		c.Search.BM25Weight = other.Search.BM25Weight
	}
	if other.Search.SemanticWeight != 0 {
		c.Search.SemanticWeight = other.Search.SemanticWeight
	}
	if other.Search.RRFConstant != 0 {
		c.Search.RRFConstant = other.Search.RRFConstant
	}
	if other.Search.BM25Backend != "" {
		c.Search.BM25Backend = other.Search.BM25Backend
	}
	if other.Search.ChunkSize != 0 {
		c.Search.ChunkSize = other.Search.ChunkSize
	}
	if other.Search.ChunkOverlap != 0 {
		c.Search.ChunkOverlap = other.Search.ChunkOverlap
	}
	if other.Search.MaxResults != 0 {
		c.Search.MaxResults = other.Search.MaxResults
	}

	// Embeddings
	if other.Embeddings.Provider != "" {
		c.Embeddings.Provider = other.Embeddings.Provider
	}
	if other.Embeddings.Model != "" {
		c.Embeddings.Model = other.Embeddings.Model
	}
	if other.Embeddings.Dimensions != 0 {
		c.Embeddings.Dimensions = other.Embeddings.Dimensions
	}
	if other.Embeddings.BatchSize != 0 {
		c.Embeddings.BatchSize = other.Embeddings.BatchSize
	}
	if other.Embeddings.OllamaHost != "" {
		c.Embeddings.OllamaHost = other.Embeddings.OllamaHost
	}
	// Thermal management settings
	if other.Embeddings.InterBatchDelay != "" {
		c.Embeddings.InterBatchDelay = other.Embeddings.InterBatchDelay
	}
	if other.Embeddings.TimeoutProgression != 0 {
		c.Embeddings.TimeoutProgression = other.Embeddings.TimeoutProgression
	}
	if other.Embeddings.RetryTimeoutMultiplier != 0 {
		c.Embeddings.RetryTimeoutMultiplier = other.Embeddings.RetryTimeoutMultiplier
	}

	// Performance
	if other.Performance.MaxFiles != 0 {
		c.Performance.MaxFiles = other.Performance.MaxFiles
	}
	if other.Performance.IndexWorkers != 0 {
		c.Performance.IndexWorkers = other.Performance.IndexWorkers
	}
	if other.Performance.WatchDebounce != "" {
		c.Performance.WatchDebounce = other.Performance.WatchDebounce
	}
	if other.Performance.CacheSize != 0 {
		c.Performance.CacheSize = other.Performance.CacheSize
	}
	if other.Performance.MemoryLimit != "" {
		c.Performance.MemoryLimit = other.Performance.MemoryLimit
	}
	if other.Performance.Quantization != "" {
		c.Performance.Quantization = other.Performance.Quantization
	}
	if other.Performance.SQLiteCacheMB != 0 {
		c.Performance.SQLiteCacheMB = other.Performance.SQLiteCacheMB
	}

	// Server
	if other.Server.Transport != "" {
		c.Server.Transport = other.Server.Transport
	}
	if other.Server.Port != 0 {
		c.Server.Port = other.Server.Port
	}
	if other.Server.LogLevel != "" {
		c.Server.LogLevel = other.Server.LogLevel
	}

	// Submodules
	if other.Submodules.Enabled {
		c.Submodules.Enabled = other.Submodules.Enabled
	}
	// Recursive can be explicitly set to false, so we check if the other config was parsed
	// Since yaml.Unmarshal sets false by default, we need a different approach
	// For now, we always merge if the other has any submodule config set
	if len(other.Submodules.Include) > 0 || len(other.Submodules.Exclude) > 0 || other.Submodules.Enabled {
		c.Submodules.Recursive = other.Submodules.Recursive
	}
	if len(other.Submodules.Include) > 0 {
		c.Submodules.Include = other.Submodules.Include
	}
	if len(other.Submodules.Exclude) > 0 {
		c.Submodules.Exclude = other.Submodules.Exclude
	}

	// Sessions
	if other.Sessions.StoragePath != "" {
		c.Sessions.StoragePath = other.Sessions.StoragePath
	}
	// AutoSave can be explicitly set to false, so only merge if storage path is set
	if other.Sessions.StoragePath != "" {
		c.Sessions.AutoSave = other.Sessions.AutoSave
	}
	if other.Sessions.MaxSessions > 0 {
		c.Sessions.MaxSessions = other.Sessions.MaxSessions
	}

	// Compaction (FEAT-AI3)
	// Enabled is boolean - need to check if any compaction config was set
	if other.Compaction.OrphanThreshold != 0 || other.Compaction.MinOrphanCount != 0 ||
		other.Compaction.IdleTimeout != "" || other.Compaction.Cooldown != "" {
		c.Compaction.Enabled = other.Compaction.Enabled
	}
	if other.Compaction.OrphanThreshold != 0 {
		c.Compaction.OrphanThreshold = other.Compaction.OrphanThreshold
	}
	if other.Compaction.MinOrphanCount != 0 {
		c.Compaction.MinOrphanCount = other.Compaction.MinOrphanCount
	}
	if other.Compaction.IdleTimeout != "" {
		c.Compaction.IdleTimeout = other.Compaction.IdleTimeout
	}
	if other.Compaction.Cooldown != "" {
		c.Compaction.Cooldown = other.Compaction.Cooldown
	}
}

// applyEnvOverrides applies AMANMCP_* environment variable overrides.
func (c *Config) applyEnvOverrides() {
	// Search weights (BUG-RR1 fix: support explicit zero values via env vars)
	if v := os.Getenv("AMANMCP_BM25_WEIGHT"); v != "" {
		if w, err := parseFloat64(v); err == nil && w >= 0 && w <= 1 {
			c.Search.BM25Weight = w
		}
	}
	if v := os.Getenv("AMANMCP_SEMANTIC_WEIGHT"); v != "" {
		if w, err := parseFloat64(v); err == nil && w >= 0 && w <= 1 {
			c.Search.SemanticWeight = w
		}
	}
	// RRF constant env override
	if v := os.Getenv("AMANMCP_RRF_CONSTANT"); v != "" {
		if k, err := strconv.Atoi(v); err == nil && k > 0 {
			c.Search.RRFConstant = k
		}
	}

	if v := os.Getenv("AMANMCP_EMBEDDINGS_PROVIDER"); v != "" {
		c.Embeddings.Provider = v
	}
	// AMANMCP_EMBEDDER is an alias for AMANMCP_EMBEDDINGS_PROVIDER
	if v := os.Getenv("AMANMCP_EMBEDDER"); v != "" {
		c.Embeddings.Provider = v
	}
	if v := os.Getenv("AMANMCP_EMBEDDINGS_MODEL"); v != "" {
		c.Embeddings.Model = v
	}
	if v := os.Getenv("AMANMCP_OLLAMA_HOST"); v != "" {
		c.Embeddings.OllamaHost = v
	}
	if v := os.Getenv("AMANMCP_LOG_LEVEL"); v != "" {
		c.Server.LogLevel = v
	}
	if v := os.Getenv("AMANMCP_TRANSPORT"); v != "" {
		c.Server.Transport = v
	}

	// Compaction env overrides (FEAT-AI3)
	if v := os.Getenv("AMANMCP_COMPACTION_ENABLED"); v != "" {
		c.Compaction.Enabled = strings.ToLower(v) == "true" || v == "1"
	}
	if v := os.Getenv("AMANMCP_COMPACTION_ORPHAN_THRESHOLD"); v != "" {
		if t, err := parseFloat64(v); err == nil && t >= 0 && t <= 1 {
			c.Compaction.OrphanThreshold = t
		}
	}
	if v := os.Getenv("AMANMCP_COMPACTION_IDLE_TIMEOUT"); v != "" {
		c.Compaction.IdleTimeout = v
	}
	if v := os.Getenv("AMANMCP_COMPACTION_COOLDOWN"); v != "" {
		c.Compaction.Cooldown = v
	}
}

// parseFloat64 parses a string to float64, used for config parsing.
func parseFloat64(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%f", &f)
	return f, err
}

// DetectProjectType detects the project type based on marker files.
// Priority: go.mod > package.json > pyproject.toml/requirements.txt
func DetectProjectType(dir string) ProjectType {
	// Check for Go project
	if fileExists(filepath.Join(dir, "go.mod")) {
		return ProjectTypeGo
	}

	// Check for Node.js project
	if fileExists(filepath.Join(dir, "package.json")) {
		return ProjectTypeNode
	}

	// Check for Python project
	if fileExists(filepath.Join(dir, "pyproject.toml")) ||
		fileExists(filepath.Join(dir, "requirements.txt")) {
		return ProjectTypePython
	}

	return ProjectTypeUnknown
}

// FindProjectRoot finds the project root directory.
// It looks for .git directory or .amanmcp.yaml/.yml file by walking up the directory tree.
func FindProjectRoot(startDir string) (string, error) {
	absDir, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	currentDir := absDir
	for {
		// Check for .git directory
		if dirExists(filepath.Join(currentDir, ".git")) {
			return currentDir, nil
		}

		// Check for .amanmcp.yaml or .amanmcp.yml
		if fileExists(filepath.Join(currentDir, ".amanmcp.yaml")) ||
			fileExists(filepath.Join(currentDir, ".amanmcp.yml")) {
			return currentDir, nil
		}

		// Move up one directory
		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir {
			// Reached root, return original directory
			return absDir, nil
		}
		currentDir = parentDir
	}
}

// DiscoverSourceDirs discovers common source directories in the project.
func DiscoverSourceDirs(dir string) []string {
	commonSourceDirs := []string{"src", "lib", "pkg", "internal", "cmd"}
	frameworkDirs := []string{"app", "pages"} // Next.js, etc.

	var found []string

	// Check common source directories
	for _, d := range commonSourceDirs {
		if dirExists(filepath.Join(dir, d)) {
			found = append(found, d)
		}
	}

	// Check for framework-specific directories
	if isNextJS(dir) {
		for _, d := range frameworkDirs {
			if dirExists(filepath.Join(dir, d)) {
				found = append(found, d)
			}
		}
	}

	return found
}

// DiscoverDocsDirs discovers documentation directories in the project.
func DiscoverDocsDirs(dir string) []string {
	commonDocDirs := []string{"docs", "doc"}
	commonDocFiles := []string{"README.md", "readme.md", "README.markdown"}

	var found []string

	// Check common doc directories
	for _, d := range commonDocDirs {
		if dirExists(filepath.Join(dir, d)) {
			found = append(found, d)
		}
	}

	// Check for README files
	for _, f := range commonDocFiles {
		if fileExists(filepath.Join(dir, f)) {
			found = append(found, f)
			break // Only add one README
		}
	}

	return found
}

// isNextJS checks if the project is a Next.js project.
func isNextJS(dir string) bool {
	pkgPath := filepath.Join(dir, "package.json")
	if !fileExists(pkgPath) {
		return false
	}

	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return false
	}

	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false
	}

	_, hasNext := pkg.Dependencies["next"]
	_, hasNextDev := pkg.DevDependencies["next"]
	return hasNext || hasNextDev
}

// fileExists checks if a file exists and is not a directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// dirExists checks if a directory exists.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// String returns a string representation of ProjectType.
func (p ProjectType) String() string {
	return string(p)
}

// IsKnown returns true if the project type is known (not unknown).
func (p ProjectType) IsKnown() bool {
	return p != ProjectTypeUnknown
}

// Validate validates the configuration and returns an error if invalid.
func (c *Config) Validate() error {
	// Validate search weights
	if c.Search.BM25Weight < 0 || c.Search.BM25Weight > 1 {
		return fmt.Errorf("bm25_weight must be between 0 and 1, got %f", c.Search.BM25Weight)
	}
	if c.Search.SemanticWeight < 0 || c.Search.SemanticWeight > 1 {
		return fmt.Errorf("semantic_weight must be between 0 and 1, got %f", c.Search.SemanticWeight)
	}

	// Validate weight sum (DEBT-018)
	sum := c.Search.BM25Weight + c.Search.SemanticWeight
	if math.Abs(sum-1.0) > 0.01 {
		return fmt.Errorf("bm25_weight + semantic_weight must equal 1.0, got %.2f", sum)
	}

	// Validate non-negative values (DEBT-018)
	if c.Search.MaxResults < 0 {
		return fmt.Errorf("max_results must be non-negative, got %d", c.Search.MaxResults)
	}
	if c.Search.ChunkSize < 0 {
		return fmt.Errorf("chunk_size must be non-negative, got %d", c.Search.ChunkSize)
	}

	// Validate provider (yzma removed in v0.1.67, empty string allowed for auto-detection)
	// BUG-060 FIX: Added 'mlx' to valid providers list
	if c.Embeddings.Provider != "" { // Empty string triggers auto-detection
		validProviders := map[string]bool{"llama": true, "static": true, "ollama": true, "mlx": true}
		if !validProviders[strings.ToLower(c.Embeddings.Provider)] {
			return fmt.Errorf("embeddings.provider must be 'llama', 'static', 'ollama', 'mlx', or empty (auto-detect), got %s", c.Embeddings.Provider)
		}
	}

	// Validate transport
	validTransports := map[string]bool{"stdio": true, "sse": true}
	if !validTransports[strings.ToLower(c.Server.Transport)] {
		return fmt.Errorf("server.transport must be 'stdio' or 'sse', got %s", c.Server.Transport)
	}

	// Validate log level
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[strings.ToLower(c.Server.LogLevel)] {
		return fmt.Errorf("server.log_level must be 'debug', 'info', 'warn', or 'error', got %s", c.Server.LogLevel)
	}

	return nil
}

// WriteYAML writes the configuration to a YAML file.
func (c *Config) WriteYAML(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// LoadUserConfig loads the user configuration file.
// Returns nil config and nil error if the file doesn't exist.
func LoadUserConfig() (*Config, error) {
	return loadUserConfig()
}

// MergeNewDefaults adds new default fields while preserving existing values.
// Returns a list of field names that were added with their default values.
func (c *Config) MergeNewDefaults() []string {
	defaults := NewConfig()
	var added []string

	// Search - weights and RRF constant (added in v0.8.2, FEAT-UNIX2)
	// Previously yaml:"-", now configurable. Upgrade adds sensible defaults.
	if c.Search.BM25Weight == 0 {
		c.Search.BM25Weight = defaults.Search.BM25Weight
		added = append(added, "search.bm25_weight")
	}
	if c.Search.SemanticWeight == 0 {
		c.Search.SemanticWeight = defaults.Search.SemanticWeight
		added = append(added, "search.semantic_weight")
	}
	if c.Search.RRFConstant == 0 {
		c.Search.RRFConstant = defaults.Search.RRFConstant
		added = append(added, "search.rrf_constant")
	}

	// Embeddings - thermal management (added in v0.1.56)
	if c.Embeddings.TimeoutProgression == 0 {
		c.Embeddings.TimeoutProgression = defaults.Embeddings.TimeoutProgression
		added = append(added, "embeddings.timeout_progression")
	}
	if c.Embeddings.RetryTimeoutMultiplier == 0 {
		c.Embeddings.RetryTimeoutMultiplier = defaults.Embeddings.RetryTimeoutMultiplier
		added = append(added, "embeddings.retry_timeout_multiplier")
	}
	// InterBatchDelay uses empty string as "disabled", so only set if not present
	// We don't auto-add this since "" is a valid value meaning "disabled"

	// Performance - SQLite cache (added in v0.1.50+)
	if c.Performance.SQLiteCacheMB == 0 {
		c.Performance.SQLiteCacheMB = defaults.Performance.SQLiteCacheMB
		added = append(added, "performance.sqlite_cache_mb")
	}

	// Sessions (added in v0.1.40+)
	if c.Sessions.StoragePath == "" {
		c.Sessions.StoragePath = defaults.Sessions.StoragePath
		added = append(added, "sessions.storage_path")
	}
	if c.Sessions.MaxSessions == 0 {
		c.Sessions.MaxSessions = defaults.Sessions.MaxSessions
		added = append(added, "sessions.max_sessions")
	}
	// auto_save is boolean - can't distinguish "not set" from "set to false"
	// so we don't auto-migrate this field

	return added
}
