package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// AC01: Default Configuration Tests
// =============================================================================

func TestNewConfig_ReturnsDefaults(t *testing.T) {
	// Given: no configuration file exists
	cfg := NewConfig()

	// Then: all defaults should be applied
	require.NotNil(t, cfg)

	// Search defaults (RCA-015: BM25 favored until vector search is fixed)
	assert.Equal(t, 0.65, cfg.Search.BM25Weight)
	assert.Equal(t, 0.35, cfg.Search.SemanticWeight)
	assert.Equal(t, 60, cfg.Search.RRFConstant) // Industry standard k=60
	assert.Equal(t, 1500, cfg.Search.ChunkSize)
	assert.Equal(t, 200, cfg.Search.ChunkOverlap)
	assert.Equal(t, 20, cfg.Search.MaxResults)

	// Embeddings defaults (auto-detection: MLX on Apple Silicon → Ollama → Static)
	assert.Equal(t, "", cfg.Embeddings.Provider) // Empty triggers auto-detection
	assert.Equal(t, "qwen3-embedding:8b", cfg.Embeddings.Model)
	assert.Equal(t, 0, cfg.Embeddings.Dimensions) // Auto-detect from embedder
	assert.Equal(t, 32, cfg.Embeddings.BatchSize)
	assert.Equal(t, 10*time.Minute, cfg.Embeddings.ModelDownloadTimeout)
	// MLX defaults (empty = use DefaultMLXConfig)
	assert.Equal(t, "", cfg.Embeddings.MLXEndpoint)
	assert.Equal(t, "", cfg.Embeddings.MLXModel)
	// Ollama defaults (empty = use DefaultOllamaConfig)
	assert.Equal(t, "", cfg.Embeddings.OllamaHost)

	// Performance defaults
	assert.Equal(t, 100000, cfg.Performance.MaxFiles)
	assert.Equal(t, runtime.NumCPU(), cfg.Performance.IndexWorkers)
	assert.Equal(t, "500ms", cfg.Performance.WatchDebounce)
	assert.Equal(t, 1000, cfg.Performance.CacheSize)
	assert.Equal(t, "auto", cfg.Performance.MemoryLimit)
	assert.Equal(t, "F16", cfg.Performance.Quantization)

	// Server defaults
	assert.Equal(t, "stdio", cfg.Server.Transport)
	assert.Equal(t, 8765, cfg.Server.Port)
	assert.Equal(t, "debug", cfg.Server.LogLevel) // Debug by default for troubleshooting

	// Paths defaults
	assert.Contains(t, cfg.Paths.Exclude, "**/node_modules/**")
	assert.Contains(t, cfg.Paths.Exclude, "**/.git/**")
	assert.Contains(t, cfg.Paths.Exclude, "**/vendor/**")

	// Sessions defaults
	assert.NotEmpty(t, cfg.Sessions.StoragePath)
	assert.Contains(t, cfg.Sessions.StoragePath, "sessions")
	assert.True(t, cfg.Sessions.AutoSave)
	assert.Equal(t, 20, cfg.Sessions.MaxSessions)
}

func TestConfig_VersionDefaultsToOne(t *testing.T) {
	cfg := NewConfig()
	assert.Equal(t, 1, cfg.Version)
}

func TestConfig_SearchWeightsSumToOne(t *testing.T) {
	cfg := NewConfig()
	sum := cfg.Search.BM25Weight + cfg.Search.SemanticWeight
	assert.InDelta(t, 1.0, sum, 0.01)
}

// =============================================================================
// AC02: Configuration File Loading Tests
// =============================================================================

func TestLoad_NoConfigFile_ReturnsDefaults(t *testing.T) {
	// Given: a directory with no .amanmcp.yaml
	tmpDir := t.TempDir()

	// When: loading configuration
	cfg, err := Load(tmpDir)

	// Then: defaults are returned without error
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, 0.65, cfg.Search.BM25Weight) // RCA-015: BM25 favored
}

func TestLoad_YamlFile_OverridesDefaults(t *testing.T) {
	// Given: a directory with .amanmcp.yaml
	// Search weights and RRF constant are now configurable via YAML (FEAT-UNIX2)
	tmpDir := t.TempDir()
	configContent := `
version: 1
search:
  bm25_weight: 0.4
  semantic_weight: 0.6
  rrf_constant: 100
  chunk_size: 2000
  max_results: 50
`
	err := os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yaml"), []byte(configContent), 0o644)
	require.NoError(t, err)

	// When: loading configuration
	cfg, err := Load(tmpDir)

	// Then: all overrides are applied
	require.NoError(t, err)
	assert.Equal(t, 0.4, cfg.Search.BM25Weight)
	assert.Equal(t, 0.6, cfg.Search.SemanticWeight)
	assert.Equal(t, 100, cfg.Search.RRFConstant)
	assert.Equal(t, 2000, cfg.Search.ChunkSize)
	assert.Equal(t, 50, cfg.Search.MaxResults)
}

func TestLoad_YmlExtension_IsRecognized(t *testing.T) {
	// Given: a directory with .amanmcp.yml (alternative extension)
	tmpDir := t.TempDir()
	configContent := `
version: 1
embeddings:
  provider: static
`
	err := os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yml"), []byte(configContent), 0o644)
	require.NoError(t, err)

	// When: loading configuration
	cfg, err := Load(tmpDir)

	// Then: .yml file is recognized
	require.NoError(t, err)
	assert.Equal(t, "static", cfg.Embeddings.Provider)
}

func TestLoad_YamlPreferredOverYml(t *testing.T) {
	// Given: both .yaml and .yml exist
	tmpDir := t.TempDir()
	yamlContent := `
version: 1
embeddings:
  provider: ollama
`
	ymlContent := `
version: 1
embeddings:
  provider: static
`
	err := os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yaml"), []byte(yamlContent), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yml"), []byte(ymlContent), 0o644)
	require.NoError(t, err)

	// When: loading configuration
	cfg, err := Load(tmpDir)

	// Then: .yaml takes precedence
	require.NoError(t, err)
	assert.Equal(t, "ollama", cfg.Embeddings.Provider)
}

func TestLoad_InvalidYaml_ReturnsError(t *testing.T) {
	// Given: invalid YAML syntax
	tmpDir := t.TempDir()
	invalidContent := `
version: 1
search:
  bm25_weight: [invalid yaml syntax
`
	err := os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yaml"), []byte(invalidContent), 0o644)
	require.NoError(t, err)

	// When: loading configuration
	cfg, err := Load(tmpDir)

	// Then: error is returned with clear message
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "parse")
}

func TestLoad_InvalidFieldType_ReturnsError(t *testing.T) {
	// Given: wrong type for a YAML-accessible field
	tmpDir := t.TempDir()
	invalidContent := `
version: 1
search:
  chunk_size: "not-a-number"
`
	err := os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yaml"), []byte(invalidContent), 0o644)
	require.NoError(t, err)

	// When: loading configuration
	cfg, err := Load(tmpDir)

	// Then: error is returned
	require.Error(t, err)
	assert.Nil(t, cfg)
}

// =============================================================================
// AC03: Project Type Detection Tests
// =============================================================================

func TestDetectProjectType_GoMod_ReturnsGo(t *testing.T) {
	// Given: directory with go.mod
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test"), 0o644)
	require.NoError(t, err)

	// When: detecting project type
	projectType := DetectProjectType(tmpDir)

	// Then: Go is detected
	assert.Equal(t, ProjectTypeGo, projectType)
}

func TestDetectProjectType_PackageJson_ReturnsNode(t *testing.T) {
	// Given: directory with package.json
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte("{}"), 0o644)
	require.NoError(t, err)

	// When: detecting project type
	projectType := DetectProjectType(tmpDir)

	// Then: Node is detected
	assert.Equal(t, ProjectTypeNode, projectType)
}

func TestDetectProjectType_PyprojectToml_ReturnsPython(t *testing.T) {
	// Given: directory with pyproject.toml
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "pyproject.toml"), []byte("[project]"), 0o644)
	require.NoError(t, err)

	// When: detecting project type
	projectType := DetectProjectType(tmpDir)

	// Then: Python is detected
	assert.Equal(t, ProjectTypePython, projectType)
}

func TestDetectProjectType_RequirementsTxt_ReturnsPython(t *testing.T) {
	// Given: directory with requirements.txt
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "requirements.txt"), []byte("requests==2.0"), 0o644)
	require.NoError(t, err)

	// When: detecting project type
	projectType := DetectProjectType(tmpDir)

	// Then: Python is detected
	assert.Equal(t, ProjectTypePython, projectType)
}

func TestDetectProjectType_NoMarkerFiles_ReturnsUnknown(t *testing.T) {
	// Given: directory with only random files
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "random.txt"), []byte("hello"), 0o644)
	require.NoError(t, err)

	// When: detecting project type
	projectType := DetectProjectType(tmpDir)

	// Then: Unknown is returned
	assert.Equal(t, ProjectTypeUnknown, projectType)
}

func TestDetectProjectType_Priority_GoOverNode(t *testing.T) {
	// Given: directory with both go.mod and package.json
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test"), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte("{}"), 0o644)
	require.NoError(t, err)

	// When: detecting project type
	projectType := DetectProjectType(tmpDir)

	// Then: Go has priority (per spec)
	assert.Equal(t, ProjectTypeGo, projectType)
}

// =============================================================================
// AC04: Directory Auto-Detection Tests
// =============================================================================

func TestFindProjectRoot_GitDirectory_ReturnsGitRoot(t *testing.T) {
	// Given: a nested directory in a git repo
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	nestedDir := filepath.Join(tmpDir, "src", "internal")
	require.NoError(t, os.Mkdir(gitDir, 0o755))
	require.NoError(t, os.MkdirAll(nestedDir, 0o755))

	// When: finding project root from nested directory
	root, err := FindProjectRoot(nestedDir)

	// Then: git root is returned
	require.NoError(t, err)
	assert.Equal(t, tmpDir, root)
}

func TestFindProjectRoot_ConfigFile_ReturnsConfigLocation(t *testing.T) {
	// Given: a directory with .amanmcp.yaml (no git)
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "src", "internal")
	require.NoError(t, os.MkdirAll(nestedDir, 0o755))
	err := os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yaml"), []byte("version: 1"), 0o644)
	require.NoError(t, err)

	// When: finding project root from nested directory
	root, err := FindProjectRoot(nestedDir)

	// Then: config file location is returned
	require.NoError(t, err)
	assert.Equal(t, tmpDir, root)
}

func TestFindProjectRoot_NoMarkers_ReturnsCurrentDir(t *testing.T) {
	// Given: a directory with no markers
	tmpDir := t.TempDir()

	// When: finding project root
	root, err := FindProjectRoot(tmpDir)

	// Then: current directory is returned
	require.NoError(t, err)
	assert.Equal(t, tmpDir, root)
}

func TestDiscoverSourceDirs_FindsCommonDirs(t *testing.T) {
	// Given: a directory with common source directories
	tmpDir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "src"), 0o755))
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "lib"), 0o755))
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "internal"), 0o755))
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "cmd"), 0o755))

	// When: discovering source directories
	dirs := DiscoverSourceDirs(tmpDir)

	// Then: all source directories are found
	assert.Contains(t, dirs, "src")
	assert.Contains(t, dirs, "lib")
	assert.Contains(t, dirs, "internal")
	assert.Contains(t, dirs, "cmd")
}

func TestDiscoverDocsDirs_FindsDocDirectories(t *testing.T) {
	// Given: a directory with documentation
	tmpDir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "docs"), 0o755))
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "doc"), 0o755))
	err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Title"), 0o644)
	require.NoError(t, err)

	// When: discovering documentation directories
	dirs := DiscoverDocsDirs(tmpDir)

	// Then: documentation directories are found
	assert.Contains(t, dirs, "docs")
	assert.Contains(t, dirs, "doc")
	assert.Contains(t, dirs, "README.md")
}

func TestDiscoverSourceDirs_NextJS_FindsAppAndPages(t *testing.T) {
	// Given: a Next.js project
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(`{"dependencies":{"next":"*"}}`), 0o644)
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "app"), 0o755))
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "pages"), 0o755))

	// When: discovering source directories
	dirs := DiscoverSourceDirs(tmpDir)

	// Then: Next.js directories are found
	assert.Contains(t, dirs, "app")
	assert.Contains(t, dirs, "pages")
}

// =============================================================================
// AC05: Environment Variable Override Tests
// =============================================================================

func TestLoad_EnvVarOverridesProvider(t *testing.T) {
	// Given: a config file with llama and env var with static
	tmpDir := t.TempDir()
	configContent := `
version: 1
embeddings:
  provider: llama
`
	err := os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yaml"), []byte(configContent), 0o644)
	require.NoError(t, err)
	t.Setenv("AMANMCP_EMBEDDINGS_PROVIDER", "static")

	// When: loading configuration
	cfg, err := Load(tmpDir)

	// Then: env var takes precedence
	require.NoError(t, err)
	assert.Equal(t, "static", cfg.Embeddings.Provider)
}

func TestLoad_EnvVarOverridesModel(t *testing.T) {
	// Given: env var for model
	tmpDir := t.TempDir()
	t.Setenv("AMANMCP_EMBEDDINGS_MODEL", "all-minilm")

	// When: loading configuration
	cfg, err := Load(tmpDir)

	// Then: env var is applied
	require.NoError(t, err)
	assert.Equal(t, "all-minilm", cfg.Embeddings.Model)
}

func TestLoad_EnvVarOverridesLogLevel(t *testing.T) {
	// Given: env var for log level
	tmpDir := t.TempDir()
	t.Setenv("AMANMCP_LOG_LEVEL", "debug")

	// When: loading configuration
	cfg, err := Load(tmpDir)

	// Then: env var is applied
	require.NoError(t, err)
	assert.Equal(t, "debug", cfg.Server.LogLevel)
}

func TestLoad_EnvVarOverridesTransport(t *testing.T) {
	// Given: env var for transport
	tmpDir := t.TempDir()
	t.Setenv("AMANMCP_TRANSPORT", "sse")

	// When: loading configuration
	cfg, err := Load(tmpDir)

	// Then: env var is applied
	require.NoError(t, err)
	assert.Equal(t, "sse", cfg.Server.Transport)
}

func TestLoad_EnvVarOverridesRRFConstant(t *testing.T) {
	// Given: YAML config with RRF constant and env var override
	tmpDir := t.TempDir()
	configContent := `
version: 1
search:
  rrf_constant: 100
`
	err := os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yaml"), []byte(configContent), 0o644)
	require.NoError(t, err)
	t.Setenv("AMANMCP_RRF_CONSTANT", "80")

	// When: loading configuration
	cfg, err := Load(tmpDir)

	// Then: env var takes precedence over YAML
	require.NoError(t, err)
	assert.Equal(t, 80, cfg.Search.RRFConstant)
}

func TestLoad_EnvVarOverridesSearchWeights(t *testing.T) {
	// Given: YAML config with weights and env var override
	tmpDir := t.TempDir()
	configContent := `
version: 1
search:
  bm25_weight: 0.4
  semantic_weight: 0.6
`
	err := os.WriteFile(filepath.Join(tmpDir, ".amanmcp.yaml"), []byte(configContent), 0o644)
	require.NoError(t, err)
	t.Setenv("AMANMCP_BM25_WEIGHT", "0.5")
	t.Setenv("AMANMCP_SEMANTIC_WEIGHT", "0.5")

	// When: loading configuration
	cfg, err := Load(tmpDir)

	// Then: env vars take precedence over YAML
	require.NoError(t, err)
	assert.Equal(t, 0.5, cfg.Search.BM25Weight)
	assert.Equal(t, 0.5, cfg.Search.SemanticWeight)
}

func TestLoad_EnvVarEmptyString_DoesNotOverride(t *testing.T) {
	// Given: empty env var
	tmpDir := t.TempDir()
	t.Setenv("AMANMCP_EMBEDDINGS_PROVIDER", "")

	// When: loading configuration
	cfg, err := Load(tmpDir)

	// Then: default is kept (empty string = auto-detect)
	require.NoError(t, err)
	assert.Equal(t, "", cfg.Embeddings.Provider) // Empty = auto-detect: MLX -> Ollama -> Static
}

// =============================================================================
// AC06: User/Global Configuration Tests
// =============================================================================

func TestGetUserConfigPath_DefaultsToXDGLocation(t *testing.T) {
	// Given: no XDG_CONFIG_HOME set
	t.Setenv("XDG_CONFIG_HOME", "")

	// When: getting user config path
	path := GetUserConfigPath()

	// Then: defaults to ~/.config/amanmcp/config.yaml
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	expected := filepath.Join(home, ".config", "amanmcp", "config.yaml")
	assert.Equal(t, expected, path)
}

func TestGetUserConfigPath_RespectsXDGConfigHome(t *testing.T) {
	// Given: XDG_CONFIG_HOME is set
	customConfig := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", customConfig)

	// When: getting user config path
	path := GetUserConfigPath()

	// Then: uses XDG_CONFIG_HOME
	expected := filepath.Join(customConfig, "amanmcp", "config.yaml")
	assert.Equal(t, expected, path)
}

func TestGetUserConfigDir_ReturnsParentOfConfigPath(t *testing.T) {
	// When: getting user config directory
	dir := GetUserConfigDir()
	path := GetUserConfigPath()

	// Then: directory is parent of config file
	assert.Equal(t, filepath.Dir(path), dir)
}

func TestUserConfigExists_ReturnsFalseWhenMissing(t *testing.T) {
	// Given: XDG_CONFIG_HOME points to empty directory
	emptyDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", emptyDir)

	// When: checking if user config exists
	exists := UserConfigExists()

	// Then: returns false
	assert.False(t, exists)
}

func TestUserConfigExists_ReturnsTrueWhenPresent(t *testing.T) {
	// Given: user config file exists
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	amanmcpDir := filepath.Join(configDir, "amanmcp")
	require.NoError(t, os.MkdirAll(amanmcpDir, 0o755))
	configPath := filepath.Join(amanmcpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("version: 1"), 0o644))

	// When: checking if user config exists
	exists := UserConfigExists()

	// Then: returns true
	assert.True(t, exists)
}

func TestLoad_UserConfigOverridesDefaults(t *testing.T) {
	// Given: user config with custom Ollama host
	configDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	amanmcpDir := filepath.Join(configDir, "amanmcp")
	require.NoError(t, os.MkdirAll(amanmcpDir, 0o755))
	userConfig := `
version: 1
embeddings:
  ollama_host: http://custom-host:11434
`
	require.NoError(t, os.WriteFile(filepath.Join(amanmcpDir, "config.yaml"), []byte(userConfig), 0o644))

	// When: loading configuration
	cfg, err := Load(projectDir)

	// Then: user config values are applied
	require.NoError(t, err)
	assert.Equal(t, "http://custom-host:11434", cfg.Embeddings.OllamaHost)
}

func TestLoad_ProjectConfigOverridesUserConfig(t *testing.T) {
	// Given: both user and project configs exist
	configDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	// User config
	amanmcpDir := filepath.Join(configDir, "amanmcp")
	require.NoError(t, os.MkdirAll(amanmcpDir, 0o755))
	userConfig := `
version: 1
embeddings:
  provider: ollama
  model: user-model
`
	require.NoError(t, os.WriteFile(filepath.Join(amanmcpDir, "config.yaml"), []byte(userConfig), 0o644))

	// Project config (overrides user)
	projectConfig := `
version: 1
embeddings:
  model: project-model
`
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".amanmcp.yaml"), []byte(projectConfig), 0o644))

	// When: loading configuration
	cfg, err := Load(projectDir)

	// Then: project config takes precedence
	require.NoError(t, err)
	assert.Equal(t, "project-model", cfg.Embeddings.Model)
	// And: user config's provider is still used (not overridden by project)
	assert.Equal(t, "ollama", cfg.Embeddings.Provider)
}

func TestLoad_EnvVarOverridesUserAndProjectConfig(t *testing.T) {
	// Given: all three config sources exist
	configDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("AMANMCP_EMBEDDINGS_MODEL", "env-model")

	// User config
	amanmcpDir := filepath.Join(configDir, "amanmcp")
	require.NoError(t, os.MkdirAll(amanmcpDir, 0o755))
	userConfig := `
version: 1
embeddings:
  model: user-model
`
	require.NoError(t, os.WriteFile(filepath.Join(amanmcpDir, "config.yaml"), []byte(userConfig), 0o644))

	// Project config
	projectConfig := `
version: 1
embeddings:
  model: project-model
`
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".amanmcp.yaml"), []byte(projectConfig), 0o644))

	// When: loading configuration
	cfg, err := Load(projectDir)

	// Then: env var has highest precedence
	require.NoError(t, err)
	assert.Equal(t, "env-model", cfg.Embeddings.Model)
}

func TestLoad_InvalidUserConfig_ReturnsError(t *testing.T) {
	// Given: invalid user config
	configDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	amanmcpDir := filepath.Join(configDir, "amanmcp")
	require.NoError(t, os.MkdirAll(amanmcpDir, 0o755))
	invalidConfig := `
version: 1
embeddings:
  model: [invalid yaml
`
	require.NoError(t, os.WriteFile(filepath.Join(amanmcpDir, "config.yaml"), []byte(invalidConfig), 0o644))

	// When: loading configuration
	cfg, err := Load(projectDir)

	// Then: error is returned
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "user config")
}
