package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/Aman-CERP/amanmcp/internal/embed"
	"github.com/Aman-CERP/amanmcp/internal/lifecycle"
	"github.com/Aman-CERP/amanmcp/internal/output"
)

func newSetupCmd() *cobra.Command {
	var (
		check   bool
		auto    bool
		offline bool
		verbose bool
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up AmanMCP embedding backend",
		Long: `Set up AmanMCP by checking and configuring the embedding backend.

This command will:
1. Check if Ollama is installed and running
2. Start Ollama if installed but not running
3. Pull the embedding model if needed
4. Validate the setup is working

Use --auto for non-interactive mode (e.g., Homebrew post-install).
Use --offline to configure for BM25-only search (no embeddings).`,
		Example: `  # Interactive setup (starts Ollama, pulls model if needed)
  amanmcp setup

  # Check status only
  amanmcp setup --check

  # Non-interactive setup (for scripts/Homebrew)
  amanmcp setup --auto

  # Configure for offline mode
  amanmcp setup --offline`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			return runSetup(ctx, cmd, check, auto, offline, verbose)
		},
	}

	cmd.Flags().BoolVar(&check, "check", false, "Only check status, don't start or pull")
	cmd.Flags().BoolVar(&auto, "auto", false, "Non-interactive mode (auto-start, auto-pull)")
	cmd.Flags().BoolVar(&offline, "offline", false, "Configure for offline mode (BM25-only)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show verbose output")

	return cmd
}

func runSetup(ctx context.Context, cmd *cobra.Command, checkOnly, auto, offline, verbose bool) error {
	out := output.New(cmd.OutOrStdout())

	out.Status("🔧", "AmanMCP Setup")
	out.Newline()

	// Offline mode configuration
	if offline {
		out.Status("📴", "Configuring offline mode (BM25-only search)")
		out.Newline()
		out.Status("ℹ️ ", "Offline mode uses keyword-based search only")
		out.Status("ℹ️ ", "Semantic search requires Ollama")
		out.Newline()
		out.Success("Offline mode configured. Run 'amanmcp init --offline' to index.")
		return nil
	}

	manager := lifecycle.NewOllamaManager()

	// Get current status
	out.Status("🔍", "Checking Ollama status...")
	status, err := manager.Status(ctx, embed.DefaultOllamaModel)
	if err != nil && verbose {
		out.Warningf("Status check warning: %v", err)
	}

	out.Newline()

	// Display current status
	out.Status("📊", "Embedder Status:")
	if status != nil {
		installedStr := "❌ Not installed"
		if status.Installed {
			installedStr = fmt.Sprintf("✅ Installed (%s)", status.InstalledPath)
		}
		out.Status("", fmt.Sprintf("  Ollama:     %s", installedStr))

		runningStr := "❌ Not running"
		if status.Running {
			runningStr = "✅ Running"
		}
		out.Status("", fmt.Sprintf("  Status:     %s", runningStr))

		modelStr := fmt.Sprintf("❌ Not installed (%s)", embed.DefaultOllamaModel)
		if status.HasModel {
			modelStr = fmt.Sprintf("✅ Available (%s)", embed.DefaultOllamaModel)
		}
		out.Status("", fmt.Sprintf("  Model:      %s", modelStr))
	} else {
		out.Status("", "  Unable to determine status")
	}

	out.Newline()

	// Check-only mode
	if checkOnly {
		if status != nil && status.Running && status.HasModel {
			out.Success("Embedder is ready!")
		} else {
			out.Warning("Embedder not fully configured")
			out.Status("💡", "Run 'amanmcp setup' to configure")
		}
		return nil
	}

	// Not installed - show instructions
	if status != nil && !status.Installed {
		out.Warning("Ollama is not installed")
		out.Newline()

		if auto {
			out.Status("", lifecycle.InstallInstructions())
			return fmt.Errorf("ollama not installed (auto mode cannot install)")
		}

		// Interactive mode
		lifecycle.ShowInstallInstructions(os.Stdout)
		out.Newline()
		out.Status("💡", "After installing, run 'amanmcp setup' again")
		return nil
	}

	// Installed but not running - start it
	if status != nil && !status.Running {
		out.Status("🔄", "Starting Ollama...")

		if err := manager.Start(); err != nil {
			out.Warningf("Failed to start Ollama: %v", err)
			return err
		}

		out.Status("⏳", "Waiting for Ollama to be ready...")
		if err := manager.WaitForReady(ctx, lifecycle.StartupTimeout); err != nil {
			out.Warningf("Ollama failed to start: %v", err)
			return err
		}

		out.Success("Ollama started")
		out.Newline()

		// Re-check status
		status, _ = manager.Status(ctx, embed.DefaultOllamaModel)
	}

	// Running but model missing - pull it
	if status != nil && status.Running && !status.HasModel {
		out.Statusf("📥", "Pulling model %s...", embed.DefaultOllamaModel)
		out.Newline()

		progressFunc := lifecycle.CreatePullProgressFunc(os.Stdout)
		if err := manager.PullModel(ctx, embed.DefaultOllamaModel, progressFunc); err != nil {
			out.Newline()
			out.Warningf("Failed to pull model: %v", err)
			out.Statusf("💡", "Try manually: ollama pull %s", embed.DefaultOllamaModel)
			return err
		}

		out.Newline()
		out.Successf("Model %s installed", embed.DefaultOllamaModel)
		out.Newline()
	}

	// Final verification
	out.Status("🔍", "Verifying setup...")

	// Quick health check via embedder
	embedder, err := embed.NewEmbedder(ctx, embed.ProviderOllama, "")
	if err != nil {
		out.Warningf("Embedder verification failed: %v", err)
		return err
	}
	defer func() { _ = embedder.Close() }()

	info := embed.GetInfo(ctx, embedder)
	out.Newline()
	out.Success("Setup complete!")
	out.Newline()
	out.Status("📊", "Configuration:")
	out.Status("", fmt.Sprintf("  Provider:   %s", info.Provider))
	out.Status("", fmt.Sprintf("  Model:      %s", info.Model))
	out.Status("", fmt.Sprintf("  Dimensions: %d", info.Dimensions))
	out.Newline()
	out.Status("🚀", "Ready! Run 'amanmcp init' to index your project.")

	return nil
}
