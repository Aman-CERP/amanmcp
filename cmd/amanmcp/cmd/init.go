package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Aman-CERP/amanmcp/configs"
	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/embed"
	"github.com/Aman-CERP/amanmcp/internal/lifecycle"
	"github.com/Aman-CERP/amanmcp/internal/output"
	"github.com/Aman-CERP/amanmcp/pkg/version"
)

// MCPServerConfig represents the MCP server configuration in .mcp.json
type MCPServerConfig struct {
	Type    string            `json:"type,omitempty"` // BUG-040: Add type field
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// MCPConfig represents the root .mcp.json structure
type MCPConfig struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

func newInitCmd() *cobra.Command {
	var (
		global     bool
		force      bool
		offline    bool
		configOnly bool
		resume     bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize AmanMCP for a project",
		Long: `Initialize AmanMCP for the current project.

This command:
1. Configures Claude Code MCP integration (via 'claude mcp add' or .mcp.json)
2. Generates .amanmcp.yaml configuration template
3. Indexes the project with a detailed progress bar (unless --config-only)
4. Verifies embedder availability (Ollama or fallback)

After running, restart Claude Code to activate the MCP server.

Use --resume to continue from a previous interrupted indexing operation.`,
		Example: `  # Initialize in current project
  amanmcp init

  # Initialize globally (available in all projects)
  amanmcp init --global

  # Force reinitialize (overwrite existing config)
  amanmcp init --force

  # Fix config only (skip indexing)
  amanmcp init --force --config-only

  # Use offline mode (static embeddings)
  amanmcp init --offline

  # Resume interrupted indexing
  amanmcp init --resume`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			return runInit(ctx, cmd, global, force, offline, configOnly, resume)
		},
	}

	cmd.Flags().BoolVar(&global, "global", false, "Configure for all projects (user scope)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing configuration")
	cmd.Flags().BoolVar(&offline, "offline", false, "Use static embeddings (no Ollama required)")
	cmd.Flags().BoolVar(&configOnly, "config-only", false, "Configure MCP only, skip indexing")
	cmd.Flags().BoolVar(&resume, "resume", false, "Resume from previous checkpoint if available")

	return cmd
}

// Note: Project config template is now embedded from configs/project-config.example.yaml
// via the configs.ProjectConfigTemplate variable. This ensures the template is:
// 1. Visible and editable in the repo
// 2. Available in binary distributions (Homebrew, etc.)

// amanmcpStartMarker is the HTML comment that marks the beginning of the amanmcp guide section
const amanmcpStartMarker = "<!-- amanmcp:start -->"

// amanmcpGuideContent is the usage guide added to CLAUDE.md
const amanmcpGuideContent = `<!-- amanmcp:start -->
## AmanMCP Search (Use by Default)

**amanmcp answers "WHAT implements this?"** - Returns full functions with context
**Grep answers "WHERE does this word appear?"** - Returns line fragments only

### Decision Rule

Ask: *Do I need the implementation or just the location?*

| Need | Tool | Example |
|------|------|---------|
| **Implementation** | ` + "`mcp__amanmcp__search_code`" + ` | "How does retry work?" |
| **Understanding** | ` + "`mcp__amanmcp__search`" + ` | "Find error handling" |
| **Architecture** | ` + "`mcp__amanmcp__search_docs`" + ` | "Design decisions" |
| **Exact text** | Grep | ` + "`func NewClient(`" + ` |
| **File paths** | Glob | ` + "`**/*.test.go`" + ` |

### Workflow: MCP → Read → Edit

` + "```" + `
# 1. Find code (MCP)
mcp__amanmcp__search_code("retry logic")

# 2. Get full context (Read) - use file/line from step 1
Read(file_path, offset: N)

# 3. Edit directly - do NOT use Grep in between
Edit(file_path, old_string, new_string)
` + "```" + `

**Default to amanmcp. Never use Grep as intermediate step after MCP.**
<!-- amanmcp:end -->
`

// hasAmanMCPGuide checks if CLAUDE.md contains the amanmcp guide section
func hasAmanMCPGuide(path string) (bool, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading CLAUDE.md: %w", err)
	}
	return strings.Contains(string(content), amanmcpStartMarker), nil
}

// hasAmanmcpIgnore checks if .amanmcp is already in .gitignore.
// Handles variations: .amanmcp, .amanmcp/, /.amanmcp, /.amanmcp/
func hasAmanmcpIgnore(content string) bool {
	patterns := []string{
		".amanmcp",
		".amanmcp/",
		"/.amanmcp",
		"/.amanmcp/",
	}

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, pattern := range patterns {
			if line == pattern {
				return true
			}
		}
	}
	return false
}

// ensureGitignore adds .amanmcp to .gitignore if not present.
// Returns (true, nil) if added, (false, nil) if already present.
func ensureGitignore(projectRoot string) (bool, error) {
	gitignorePath := filepath.Join(projectRoot, ".gitignore")

	// Check if .gitignore exists and read content
	content, err := os.ReadFile(gitignorePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("reading .gitignore: %w", err)
	}

	// Check if .amanmcp is already ignored
	if hasAmanmcpIgnore(string(content)) {
		return false, nil // Already present
	}

	// Determine line ending (match existing or default to LF)
	lineEnding := "\n"
	if bytes.Contains(content, []byte("\r\n")) {
		lineEnding = "\r\n"
	}

	// Ensure file ends with newline before appending
	if len(content) > 0 && !bytes.HasSuffix(content, []byte("\n")) {
		content = append(content, []byte(lineEnding)...)
	}

	// Append .amanmcp entry with comment
	var entry string
	if len(content) == 0 {
		// For new files, don't add leading newline
		entry = fmt.Sprintf("# AmanMCP index data (auto-generated)%s.amanmcp/%s",
			lineEnding, lineEnding)
	} else {
		entry = fmt.Sprintf("%s# AmanMCP index data (auto-generated)%s.amanmcp/%s",
			lineEnding, lineEnding, lineEnding)
	}

	content = append(content, []byte(entry)...)

	// Write back
	if err := os.WriteFile(gitignorePath, content, 0644); err != nil {
		return false, fmt.Errorf("writing .gitignore: %w", err)
	}

	return true, nil
}

// ensureAmanMCPGuide adds the guide section to CLAUDE.md if not present
// Returns: (added bool, err error)
func ensureAmanMCPGuide(path string) (bool, error) {
	// Check if file exists
	fileExists := true
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		fileExists = false
	}

	if fileExists {
		// Check if guide already exists
		hasGuide, err := hasAmanMCPGuide(path)
		if err != nil {
			return false, err
		}
		if hasGuide {
			return false, nil // Already has guide, skip
		}
		// Append to existing file
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return false, fmt.Errorf("opening CLAUDE.md: %w", err)
		}
		defer f.Close()
		if _, err := f.WriteString("\n\n" + amanmcpGuideContent); err != nil {
			return false, fmt.Errorf("appending to CLAUDE.md: %w", err)
		}
		return true, nil
	}

	// Create new file with guide only
	if err := os.WriteFile(path, []byte(amanmcpGuideContent), 0644); err != nil {
		return false, fmt.Errorf("creating CLAUDE.md: %w", err)
	}
	return true, nil
}

// generateAmanmcpYAML creates a template .amanmcp.yaml if it doesn't exist.
//
// How .amanmcp.yaml auto-generation works:
//
//  1. Template Source: The template is embedded at build time from
//     configs/project-config.example.yaml via Go's embed directive (see configs/embed.go).
//     This ensures the template is available in binary distributions (Homebrew, etc.).
//
//  2. File Priority: Checks for both .amanmcp.yaml and .amanmcp.yml extensions.
//     If either exists, the existing file is preserved (never overwritten).
//
//  3. Content: The template includes commented examples for all configuration options.
//     Users uncomment only what they need. Default values work out of the box.
//
//  4. Configuration Hierarchy (see internal/config/config.go Load()):
//     - Hardcoded defaults (internal/config/config.go NewConfig())
//     - User config (~/.config/amanmcp/config.yaml) - machine-specific settings
//     - Project config (.amanmcp.yaml) - project-specific overrides
//     - Environment variables (AMANMCP_*) - highest precedence
//
//  5. Common Use Cases:
//     - Exclude project-specific paths: paths.exclude: [".aman-pm/**", "archive/**"]
//     - Override search settings: search.max_results: 50
//     - Enable git submodules: submodules.enabled: true
//
// The generated file is optional - AmanMCP works with sensible defaults.
func generateAmanmcpYAML(out *output.Writer, projectRoot string) error {
	yamlPath := filepath.Join(projectRoot, ".amanmcp.yaml")

	// Check if file already exists (don't overwrite user customizations)
	if _, err := os.Stat(yamlPath); err == nil {
		out.Status("ℹ️ ", "Existing .amanmcp.yaml preserved")
		return nil
	}

	// Also check .yml extension (both are valid, user preference)
	ymlPath := filepath.Join(projectRoot, ".amanmcp.yml")
	if _, err := os.Stat(ymlPath); err == nil {
		out.Status("ℹ️ ", "Existing .amanmcp.yml found, skipping template")
		return nil
	}

	// Write template from embedded config (see configs/embed.go for source)
	if err := os.WriteFile(yamlPath, []byte(configs.ProjectConfigTemplate), 0644); err != nil {
		return fmt.Errorf("failed to write .amanmcp.yaml: %w", err)
	}

	out.Statusf("📝", "Created .amanmcp.yaml (optional project configuration)")
	return nil
}

// validateExistingMCPConfig checks if existing .mcp.json has required fields
// BUG-042: Validate config instead of just checking file existence
func validateExistingMCPConfig(mcpPath string) (bool, []string) {
	var warnings []string

	data, err := os.ReadFile(mcpPath)
	if err != nil {
		return false, nil
	}

	var config MCPConfig
	if err := json.Unmarshal(data, &config); err != nil {
		warnings = append(warnings, "Invalid JSON in .mcp.json")
		return false, warnings
	}

	amanmcp, exists := config.MCPServers["amanmcp"]
	if !exists {
		warnings = append(warnings, "AmanMCP not configured in .mcp.json")
		return false, warnings
	}

	// Check required fields
	if amanmcp.Cwd == "" {
		warnings = append(warnings, "Missing 'cwd' field - MCP server may run from wrong directory")
	}
	if amanmcp.Command == "" {
		warnings = append(warnings, "Missing 'command' field")
	}

	return len(warnings) == 0, warnings
}

func runInit(ctx context.Context, cmd *cobra.Command, global, force, offline, configOnly, resume bool) error {
	out := output.New(cmd.OutOrStdout())

	out.Statusf("🚀", "AmanMCP %s - Initializing...", version.Version)
	out.Newline()

	// Find project root
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	root, err := config.FindProjectRoot(cwd)
	if err != nil {
		root = cwd // Use current directory if no project root found
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	out.Statusf("📁", "Project: %s", absRoot)

	// Check if already initialized
	mcpConfigPath := filepath.Join(absRoot, ".mcp.json")

	if !force {
		if _, err := os.Stat(mcpConfigPath); err == nil {
			// BUG-042: Validate existing config instead of just checking existence
			isValid, warnings := validateExistingMCPConfig(mcpConfigPath)
			out.Newline()

			if !isValid && len(warnings) > 0 {
				out.Warning("Existing .mcp.json has configuration issues:")
				for _, w := range warnings {
					out.Statusf("  ⚠️ ", "%s", w)
				}
				out.Newline()
				out.Status("💡", "Use --force to fix these issues")
				return nil
			}

			out.Warning("Project already initialized (.mcp.json exists)")
			out.Status("💡", "Use --force to reinitialize")
			return nil
		}
	}

	// Step 1: Configure MCP
	out.Newline()
	out.Status("⚙️ ", "Configuring MCP integration...")

	mcpConfigured, err := configureMCP(ctx, out, absRoot, global, force)
	if err != nil {
		out.Warningf("MCP configuration failed: %v", err)
		out.Status("💡", "You can manually configure .mcp.json later")
	} else if mcpConfigured {
		if global {
			out.Success("Added MCP server (user scope - all projects)")
		} else {
			out.Success("Added MCP server (project scope)")
		}
	}

	// Step 1.5: Generate .amanmcp.yaml template (optional config)
	if err := generateAmanmcpYAML(out, absRoot); err != nil {
		out.Warningf("Could not create .amanmcp.yaml template: %v", err)
	}

	// Step 1.7: Add CLAUDE.md usage guide
	claudeMDPath := filepath.Join(absRoot, "CLAUDE.md")
	added, err := ensureAmanMCPGuide(claudeMDPath)
	if err != nil {
		out.Warningf("Could not update CLAUDE.md: %v", err)
		// Non-fatal, continue with init
	} else if added {
		out.Success("Added amanmcp usage guide to CLAUDE.md")
	} else {
		out.Status("ℹ️ ", "CLAUDE.md already has amanmcp guide")
	}

	// Step 1.8: Ensure .amanmcp in .gitignore
	added, err = ensureGitignore(absRoot)
	if err != nil {
		out.Warningf("Could not update .gitignore: %v", err)
		// Non-fatal, continue with init
	} else if added {
		out.Status("📝", "Added .amanmcp to .gitignore")
	}
	// Silent when already present (no output)

	// Step 2: Index the project (skip if --config-only)
	if configOnly {
		out.Newline()
		out.Status("⏭️ ", "Skipping indexing (--config-only)")
	} else {
		// Check embedder readiness (unless --offline)
		if !offline {
			out.Newline()
			out.Status("🧠", "Checking embedder availability...")

			shouldUseOffline, err := ensureEmbedderReady(ctx, out)
			if err != nil {
				return fmt.Errorf("embedder check failed: %w", err)
			}
			if shouldUseOffline {
				offline = true
				out.Status("ℹ️ ", "Using offline mode (BM25-only search)")
			}
		}

		out.Newline()
		if resume {
			out.Status("📊", "Resuming indexing from checkpoint...")
		} else {
			out.Status("📊", "Indexing project...")
		}

		startTime := time.Now()
		if err := runIndexWithResume(ctx, cmd, absRoot, offline, false, resume, force); err != nil {
			return fmt.Errorf("indexing failed: %w", err)
		}
		duration := time.Since(startTime)

		out.Newline()
		out.Status("⏱️ ", fmt.Sprintf("Completed in %.1fs", duration.Seconds()))

		// Get embedder info
		embedderType := "OllamaEmbedder"
		if offline {
			embedderType = "Static768 (offline)"
		}
		out.Statusf("🧠", "Embedder: %s", embedderType)
	}

	// Final instructions
	out.Newline()
	if configOnly {
		out.Success("Configuration complete!")
	} else {
		out.Success("Initialization complete!")
	}
	out.Newline()
	out.Status("📋", "Next steps:")
	out.Status("", "  1. Restart Claude Code to activate MCP server")
	out.Status("", "  2. Test with: \"Search my codebase for...\"")
	out.Status("", "  3. Run 'amanmcp doctor' to verify setup")

	// Hint about user config for machine-specific settings
	if !config.UserConfigExists() {
		out.Newline()
		out.Status("💡", "For machine-specific settings (thermal, Ollama host):")
		out.Status("", "   Run 'amanmcp config init' to create user config")
	}

	// Check if .mcp.json was created for manual config info
	if !mcpConfigured {
		out.Newline()
		out.Warning("MCP not auto-configured - manual setup required")
		out.Status("💡", fmt.Sprintf("Add to .mcp.json: %s", mcpConfigPath))
	}

	return nil
}

// configureMCP attempts to configure MCP via claude CLI or falls back to .mcp.json
func configureMCP(ctx context.Context, out *output.Writer, projectRoot string, global, force bool) (bool, error) {
	// First, try using claude CLI
	if claudeConfigured, err := configureViaClaude(ctx, out, projectRoot, global, force); err == nil && claudeConfigured {
		return true, nil
	}

	// Fall back to generating .mcp.json
	return configureViaMCPJSON(ctx, out, projectRoot, force)
}

// configureViaClaude attempts to use 'claude mcp add' command
func configureViaClaude(ctx context.Context, out *output.Writer, projectRoot string, global, _ bool) (bool, error) {
	// BUG-041: claude mcp add doesn't support --cwd flag
	// Only use for global scope where cwd isn't needed (user decides at runtime)
	// For project scope, we need .mcp.json which supports cwd field
	if !global {
		out.Status("ℹ️ ", "Using .mcp.json for project scope (supports cwd)")
		return false, nil
	}

	// Check if claude CLI is available
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		out.Status("ℹ️ ", "Claude CLI not found, using .mcp.json fallback")
		return false, nil
	}

	out.Statusf("🔍", "Found Claude CLI: %s", claudePath)

	// Find amanmcp binary path
	amanmcpPath, err := findAmanmcpBinary()
	if err != nil {
		return false, fmt.Errorf("failed to find amanmcp binary: %w", err)
	}

	// Build command arguments (global scope only)
	args := []string{"mcp", "add", "--transport", "stdio", "--scope", "user"}

	// Add server name and command
	args = append(args, "amanmcp", "--", amanmcpPath, "serve")

	// Execute claude mcp add
	cmd := exec.CommandContext(ctx, claudePath, args...)
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("claude mcp add failed: %w", err)
	}

	return true, nil
}

// configureViaMCPJSON creates or updates .mcp.json in the project root
func configureViaMCPJSON(_ context.Context, out *output.Writer, projectRoot string, force bool) (bool, error) {
	mcpPath := filepath.Join(projectRoot, ".mcp.json")

	// Check if file exists
	var existingConfig MCPConfig
	if data, err := os.ReadFile(mcpPath); err == nil {
		if err := json.Unmarshal(data, &existingConfig); err != nil {
			return false, fmt.Errorf("failed to parse existing .mcp.json: %w", err)
		}

		// Check if amanmcp already configured
		if _, exists := existingConfig.MCPServers["amanmcp"]; exists && !force {
			out.Status("ℹ️ ", "AmanMCP already configured in .mcp.json")
			return true, nil
		}
	} else {
		existingConfig = MCPConfig{
			MCPServers: make(map[string]MCPServerConfig),
		}
	}

	// Find amanmcp binary
	amanmcpPath, err := findAmanmcpBinary()
	if err != nil {
		return false, fmt.Errorf("failed to find amanmcp binary: %w", err)
	}

	// Add amanmcp configuration
	existingConfig.MCPServers["amanmcp"] = MCPServerConfig{
		Type:    "stdio", // BUG-040: Set default type
		Command: amanmcpPath,
		Args:    []string{"serve"},
		Cwd:     projectRoot,
	}

	// Write config
	data, err := json.MarshalIndent(existingConfig, "", "  ")
	if err != nil {
		return false, fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(mcpPath, data, 0644); err != nil {
		return false, fmt.Errorf("failed to write .mcp.json: %w", err)
	}

	out.Statusf("📝", "Created %s", mcpPath)
	return true, nil
}

// findAmanmcpBinary locates the amanmcp binary
func findAmanmcpBinary() (string, error) {
	// First, check if we're running as amanmcp (get our own path)
	execPath, err := os.Executable()
	if err == nil {
		// Resolve symlinks to get the real path
		realPath, err := filepath.EvalSymlinks(execPath)
		if err == nil {
			return realPath, nil
		}
		return execPath, nil
	}

	// Fall back to looking in PATH
	path, err := exec.LookPath("amanmcp")
	if err != nil {
		return "", fmt.Errorf("amanmcp not found in PATH: %w", err)
	}

	return path, nil
}

// ensureEmbedderReady checks and ensures the embedder (Ollama) is ready.
// Returns (useOffline, error) - if useOffline is true, caller should use offline mode.
func ensureEmbedderReady(ctx context.Context, out *output.Writer) (bool, error) {
	manager := lifecycle.NewOllamaManager()

	// Skip auto-start for remote hosts
	if manager.IsRemoteHost() {
		out.Status("ℹ️ ", "Using remote Ollama host: "+manager.Host())
		running, err := manager.IsRunning()
		if err != nil {
			return false, fmt.Errorf("failed to check remote Ollama: %w", err)
		}
		if !running {
			return false, fmt.Errorf("remote Ollama at %s is not responding", manager.Host())
		}
		out.Success("Remote Ollama is available")
		return false, nil
	}

	// Get full status
	status, err := manager.Status(ctx, embed.DefaultOllamaModel)
	if err != nil {
		// If we can't get status, check if running at least
		running, _ := manager.IsRunning()
		if running {
			// Ollama is running, proceed
			out.Success("Ollama is running")
			return false, nil
		}
	}

	// Case 1: Not installed
	if status != nil && !status.Installed {
		return handleOllamaNotInstalled(out)
	}

	// Case 2: Installed but not running - auto-start
	if status != nil && !status.Running {
		out.Status("🔄", "Ollama is installed but not running. Starting...")

		if err := manager.Start(); err != nil {
			out.Warningf("Failed to start Ollama: %v", err)
			return handleOllamaStartFailed(out)
		}

		out.Status("⏳", "Waiting for Ollama to be ready...")
		if err := manager.WaitForReady(ctx, lifecycle.StartupTimeout); err != nil {
			out.Warningf("Ollama failed to start in time: %v", err)
			return handleOllamaStartFailed(out)
		}

		out.Success("Ollama started successfully")

		// Re-check for model
		status, _ = manager.Status(ctx, embed.DefaultOllamaModel)
	}

	// Case 3: Running but model missing - auto-pull
	if status != nil && status.Running && !status.HasModel {
		out.Statusf("📥", "Pulling embedding model %s...", embed.DefaultOllamaModel)

		progressFunc := lifecycle.CreatePullProgressFunc(os.Stdout)
		if err := manager.PullModel(ctx, embed.DefaultOllamaModel, progressFunc); err != nil {
			out.Newline() // After progress bar
			out.Warningf("Failed to pull model: %v", err)
			return handleModelPullFailed(out, embed.DefaultOllamaModel)
		}

		out.Newline() // After progress bar
		out.Successf("Model %s ready", embed.DefaultOllamaModel)
	}

	out.Success("Embedder ready")
	return false, nil
}

// handleOllamaNotInstalled handles the case when Ollama is not installed
func handleOllamaNotInstalled(out *output.Writer) (bool, error) {
	// If not TTY, return error with instructions
	if !lifecycle.IsTTY() {
		out.Newline()
		out.Warning("Ollama is not installed (required for semantic search)")
		out.Newline()
		out.Status("", lifecycle.InstallInstructions())
		out.Newline()
		out.Status("💡", "Use --offline flag to skip semantic search")
		return false, fmt.Errorf("ollama not installed (use --offline for BM25-only search)")
	}

	// Interactive prompt
	choice, err := lifecycle.PromptNoEmbedder(os.Stdout, os.Stdin)
	if err != nil {
		return false, err
	}

	switch choice {
	case lifecycle.ChoiceShowInstall:
		lifecycle.ShowInstallInstructions(os.Stdout)
		out.Newline()
		out.Status("💡", "After installing Ollama, run 'amanmcp init' again")
		return false, fmt.Errorf("installation required")

	case lifecycle.ChoiceOfflineMode:
		return true, nil // Use offline mode

	case lifecycle.ChoiceCancel:
		return false, fmt.Errorf("operation cancelled")

	default:
		return false, fmt.Errorf("invalid choice")
	}
}

// handleOllamaStartFailed handles when Ollama fails to start
func handleOllamaStartFailed(out *output.Writer) (bool, error) {
	if !lifecycle.IsTTY() {
		out.Status("💡", "Use --offline flag for BM25-only search")
		return false, fmt.Errorf("failed to start Ollama (use --offline for BM25-only search)")
	}

	out.Newline()
	out.Status("", "  [1] Try again")
	out.Status("", "  [2] Use offline mode (BM25-only)")
	out.Status("", "  [3] Cancel")
	out.Newline()

	// Simple prompt - reuse the same mechanism
	choice, err := lifecycle.PromptNoEmbedder(os.Stdout, os.Stdin)
	if err != nil {
		return false, err
	}

	switch choice {
	case lifecycle.ChoiceShowInstall:
		// User wants to try again - but we can't restart the flow here
		// Return error to let them run init again
		return false, fmt.Errorf("please run 'amanmcp init' again after starting Ollama manually")

	case lifecycle.ChoiceOfflineMode:
		return true, nil

	default:
		return false, fmt.Errorf("operation cancelled")
	}
}

// handleModelPullFailed handles when model pull fails
func handleModelPullFailed(out *output.Writer, model string) (bool, error) {
	if !lifecycle.IsTTY() {
		out.Statusf("💡", "Pull manually with: ollama pull %s", model)
		out.Status("💡", "Or use --offline flag for BM25-only search")
		return false, fmt.Errorf("failed to pull model (use --offline for BM25-only search)")
	}

	out.Newline()
	out.Statusf("", "  Pull manually: ollama pull %s", model)
	out.Status("", "  Or choose an option:")
	out.Newline()

	choice, err := lifecycle.PromptNoEmbedder(os.Stdout, os.Stdin)
	if err != nil {
		return false, err
	}

	switch choice {
	case lifecycle.ChoiceShowInstall:
		return false, fmt.Errorf("please pull the model manually and run 'amanmcp init' again")

	case lifecycle.ChoiceOfflineMode:
		return true, nil

	default:
		return false, fmt.Errorf("operation cancelled")
	}
}
