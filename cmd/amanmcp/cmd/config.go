package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Aman-CERP/amanmcp/configs"
	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/output"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage user configuration",
		Long: `Manage the user/global configuration file.

User configuration contains machine-specific settings that apply to ALL projects
on this machine, such as:
  - Ollama host and embedding model
  - Thermal management settings (Apple Silicon)
  - Default log level
  - Performance tuning

Configuration precedence (lowest to highest):
  1. Hardcoded defaults
  2. User config (~/.config/amanmcp/config.yaml)
  3. Project config (.amanmcp.yaml)
  4. Environment variables (AMANMCP_*)`,
		Example: `  # Create user config from template
  amanmcp config init

  # Show effective configuration (merged from all sources)
  amanmcp config show

  # Print user config file path
  amanmcp config path`,
	}

	cmd.AddCommand(newConfigInitCmd())
	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigPathCmd())

	return cmd
}

func newConfigInitCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create user configuration file",
		Long: `Create the user/global configuration file from a template.

The configuration file is created at ~/.config/amanmcp/config.yaml
(or $XDG_CONFIG_HOME/amanmcp/config.yaml if XDG_CONFIG_HOME is set).

This file contains machine-specific settings like:
  - Ollama host and embedding model
  - Thermal management for Apple Silicon
  - Performance tuning`,
		Example: `  # Create user config
  amanmcp config init

  # Overwrite existing config
  amanmcp config init --force`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigInit(cmd, force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing configuration")

	return cmd
}

func newConfigShowCmd() *cobra.Command {
	var (
		jsonOutput bool
		source     string
	)

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show effective configuration",
		Long: `Show the effective configuration after merging all sources.

By default, shows the merged configuration from:
  1. Hardcoded defaults
  2. User config (~/.config/amanmcp/config.yaml)
  3. Project config (.amanmcp.yaml)
  4. Environment variables`,
		Example: `  # Show merged configuration
  amanmcp config show

  # Show as JSON
  amanmcp config show --json

  # Show only user config
  amanmcp config show --source user`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigShow(cmd, jsonOutput, source)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	cmd.Flags().StringVar(&source, "source", "merged", "Config source: merged, user, project, defaults")

	return cmd
}

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print user config file path",
		Long:  `Print the path to the user configuration file.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), config.GetUserConfigPath())
			return nil
		},
	}
}

func runConfigInit(cmd *cobra.Command, force bool) error {
	out := output.New(cmd.OutOrStdout())

	configPath := config.GetUserConfigPath()
	configDir := config.GetUserConfigDir()

	// Check if file exists
	if config.UserConfigExists() {
		if !force {
			out.Warning("User configuration already exists")
			out.Statusf("📁", "Location: %s", configPath)
			out.Newline()
			out.Status("💡", "Use --force to upgrade with new defaults (preserves your settings)")
			return nil
		}

		// Force mode: backup, merge new defaults, write
		return runConfigUpgrade(cmd, out, configPath)
	}

	// Create directory if needed
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", configDir, err)
	}

	// Write template
	if err := os.WriteFile(configPath, []byte(configs.UserConfigTemplate), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	out.Success("Created user configuration")
	out.Statusf("📁", "Location: %s", configPath)
	out.Newline()
	out.Status("📋", "Next steps:")
	out.Status("", "  1. Edit the file to customize settings")
	out.Status("", "  2. Uncomment thermal settings if needed (Apple Silicon)")
	out.Status("", "  3. Run 'amanmcp config show' to verify")

	return nil
}

// runConfigUpgrade performs backup + merge for existing config
func runConfigUpgrade(cmd *cobra.Command, out *output.Writer, configPath string) error {
	// Step 1: Create backup
	backupPath, err := config.BackupUserConfig()
	if err != nil {
		return fmt.Errorf("failed to backup config: %w", err)
	}

	// Step 2: Load existing config
	existingCfg, err := config.LoadUserConfig()
	if err != nil {
		return fmt.Errorf("failed to load existing config: %w", err)
	}
	if existingCfg == nil {
		// Should not happen since we checked UserConfigExists
		return fmt.Errorf("config file disappeared during upgrade")
	}

	// Step 3: Merge new defaults
	newFields := existingCfg.MergeNewDefaults()

	// Step 4: Write updated config
	if err := existingCfg.WriteYAML(configPath); err != nil {
		return fmt.Errorf("failed to write upgraded config: %w", err)
	}

	// Step 5: Inform user
	out.Success("Configuration upgraded")
	out.Statusf("📁", "Location: %s", configPath)
	out.Statusf("💾", "Backup: %s", backupPath)
	out.Newline()

	if len(newFields) > 0 {
		out.Status("✨", "New options added with defaults:")
		for _, field := range newFields {
			out.Statusf("", "  - %s", field)
		}
	} else {
		out.Status("✓", "Your configuration is already up to date")
	}

	out.Newline()
	out.Status("💡", "Your existing settings have been preserved")

	return nil
}

func runConfigShow(cmd *cobra.Command, jsonOutput bool, source string) error {
	out := output.New(cmd.OutOrStdout())

	var cfg *config.Config
	var sourceDesc string

	switch source {
	case "merged":
		// Get current directory for project config
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		root, err := config.FindProjectRoot(cwd)
		if err != nil {
			root = cwd
		}

		cfg, err = config.Load(root)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		sourceDesc = "merged (defaults + user + project + env)"

	case "user":
		configPath := config.GetUserConfigPath()
		if !config.UserConfigExists() {
			out.Warning("No user configuration file found")
			out.Statusf("📁", "Expected at: %s", configPath)
			out.Status("💡", "Run 'amanmcp config init' to create one")
			return nil
		}

		cfg = config.NewConfig()
		data, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("failed to read user config: %w", err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return fmt.Errorf("failed to parse user config: %w", err)
		}
		sourceDesc = fmt.Sprintf("user (%s)", configPath)

	case "project":
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		root, err := config.FindProjectRoot(cwd)
		if err != nil {
			root = cwd
		}

		// Check for project config
		yamlPath := filepath.Join(root, ".amanmcp.yaml")
		ymlPath := filepath.Join(root, ".amanmcp.yml")

		var configPath string
		if _, err := os.Stat(yamlPath); err == nil {
			configPath = yamlPath
		} else if _, err := os.Stat(ymlPath); err == nil {
			configPath = ymlPath
		} else {
			out.Warning("No project configuration file found")
			out.Statusf("📁", "Expected at: %s", yamlPath)
			out.Status("💡", "Run 'amanmcp init' to create one")
			return nil
		}

		cfg = config.NewConfig()
		data, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("failed to read project config: %w", err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return fmt.Errorf("failed to parse project config: %w", err)
		}
		sourceDesc = fmt.Sprintf("project (%s)", configPath)

	case "defaults":
		cfg = config.NewConfig()
		sourceDesc = "defaults (hardcoded)"

	default:
		return fmt.Errorf("invalid source: %s (use: merged, user, project, defaults)", source)
	}

	// Output configuration
	if jsonOutput {
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal config: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
	} else {
		out.Statusf("📋", "Configuration source: %s", sourceDesc)
		out.Newline()

		data, err := yaml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("failed to marshal config: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
	}

	return nil
}
