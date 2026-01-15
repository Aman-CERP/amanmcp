# AmanMCP Makefile
# Local-first RAG MCP server for developers
#
# Usage:
#   make help          - Show available commands
#   make build         - Build the binary
#   make test          - Run unit tests
#   make ci-check      - Run full CI validation locally

# ============================================================================
# Tool Versions (Single Source of Truth - ADR-011)
# Last reviewed: 2025-12-28
# ============================================================================
GO_VERSION = 1.25.5
GOLANGCI_LINT_VERSION = v2.7.2

# Go tools (use go run for portability - no need to install locally)
# Note: golangci-lint v2.x uses /v2/ in the module path
GOLANGCI_LINT = go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

# Project paths
BINARY_NAME = amanmcp
CMD_PATH = ./cmd/amanmcp

# Version injection (BUG-006 fix)
# Read version from VERSION file, inject via ldflags for local builds
VERSION := $(shell cat VERSION 2>/dev/null || echo "dev")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/Aman-CERP/amanmcp/pkg/version.Version=$(VERSION) \
           -X github.com/Aman-CERP/amanmcp/pkg/version.Commit=$(GIT_COMMIT) \
           -X github.com/Aman-CERP/amanmcp/pkg/version.Date=$(BUILD_DATE)

# CGO is required for tree-sitter code parsing only
# USearch removed in v0.1.38 - now using coder/hnsw (pure Go)

.PHONY: help build build-logs test test-race test-cover test-cover-html lint lint-fix lint-fast ci-check ci-check-quick clean verify-checkpoint verify-docs verify-ssot verify-all install install-user install-local install-local-logs install-local-all uninstall uninstall-local install-mlx start-mlx install-ollama start-ollama stop-ollama switch-backend-mlx switch-backend-ollama verify-install validate validate-tier1 validate-tier2 validate-all

# ============================================================================
# Help
# ============================================================================

help:
	@echo "AmanMCP - Local-first RAG MCP Server"
	@echo ""
	@echo "Build & Install Commands:"
	@echo "  make build              - Build the main binary to bin/"
	@echo "  make build-logs         - Build the log viewer binary to bin/"
	@echo "  make build-all          - Build all binaries"
	@echo "  make install-local      - Install amanmcp to ~/.local/bin (RECOMMENDED)"
	@echo "  make install-local-all  - Install all binaries to ~/.local/bin"
	@echo "  make clean              - Remove build artifacts"
	@echo ""
	@echo "MLX Server (Apple Silicon - Default, ~1.7x faster):"
	@echo "  make install-mlx        - Install MLX server + download model"
	@echo "  make start-mlx          - Start MLX server on port 9659"
	@echo ""
	@echo "Ollama Backend (Default on non-Apple Silicon - Cross-Platform):"
	@echo "  make install-ollama     - Install Ollama + pull default model"
	@echo "  make start-ollama       - Start Ollama server"
	@echo "  make stop-ollama        - Stop Ollama server"
	@echo ""
	@echo "Backend Switching:"
	@echo "  make switch-backend-mlx    - Switch to MLX (Apple Silicon)"
	@echo "  make switch-backend-ollama - Switch to Ollama"
	@echo "  make verify-install        - Verify installation is working"
	@echo ""
	@echo "Testing Commands:"
	@echo "  make test               - Run unit tests"
	@echo "  make test-race          - Run tests with race detector"
	@echo "  make test-cover         - Run tests with coverage report"
	@echo ""
	@echo "Validation Commands (MCP-based dogfooding):"
	@echo "  make validate           - Run Tier 1 validation (alias)"
	@echo "  make validate-tier1     - Run Tier 1 tests (must pass 100%)"
	@echo "  make validate-tier2     - Run Tier 2 tests (should pass 75%)"
	@echo "  make validate-all       - Run full validation suite"
	@echo "  make validate-bench     - Run validation benchmarks"
	@echo ""
	@echo "Quality Commands:"
	@echo "  make lint               - Run golangci-lint"
	@echo "  make ci-check           - Run FULL CI validation locally"
	@echo "  make ci-check-quick     - Run quick CI validation"
	@echo ""
	@echo "Quick Start (Apple Silicon):"
	@echo "  1. make install-local   - Install amanmcp"
	@echo "  2. make install-mlx     - Install MLX server + model"
	@echo "  3. make start-mlx       - Start MLX server"
	@echo "  4. amanmcp index .      - Index (auto-uses MLX)"
	@echo ""
	@echo "Quick Start (Other Platforms):"
	@echo "  1. make install-local   - Install amanmcp"
	@echo "  2. make install-ollama  - Install Ollama + model"
	@echo "  3. amanmcp index .      - Index (uses Ollama)"

# ============================================================================
# Build
# ============================================================================

build:
	@echo "Building $(BINARY_NAME) v$(VERSION)..."
	@go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME) $(CMD_PATH) 2>/dev/null || \
		(echo "Note: cmd/amanmcp not yet created, building all packages..." && go build -ldflags "$(LDFLAGS)" ./...)
	@# BUG-037 Fix: Re-sign on macOS to prevent Gatekeeper SIGKILL when built from sandboxed apps
	@# See: https://github.com/astral-sh/uv/issues/16726 for same issue/fix
	@if [ "$$(uname -s)" = "Darwin" ] && [ -f bin/$(BINARY_NAME) ]; then \
		xattr -cr bin/$(BINARY_NAME) 2>/dev/null || true; \
		codesign --force --deep --sign - bin/$(BINARY_NAME) 2>/dev/null || true; \
	fi

build-logs:
	@echo "Building amanmcp-logs v$(VERSION)..."
	@go build -ldflags "$(LDFLAGS)" -o bin/amanmcp-logs ./cmd/amanmcp-logs

build-all: build build-logs
	@echo "Built all binaries"

clean:
	@rm -rf bin/
	@rm -f coverage.out coverage.html

# ============================================================================
# Testing
# CGO_ENABLED=1 required for tree-sitter code parsing
# ============================================================================

test:
	@echo "Running unit tests..."
	@CGO_ENABLED=1 go test -v ./...

test-race:
	@echo "Running tests with race detector..."
	@CGO_ENABLED=1 go test -race ./...

test-cover:
	@echo "Running tests with coverage..."
	@CGO_ENABLED=1 go test -coverprofile=coverage.out -covermode=atomic ./...
	@echo ""
	@go tool cover -func=coverage.out | grep -E '^total:|ok'

test-cover-html: test-cover
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# ============================================================================
# Validation (MCP-based dogfooding tests)
# Uses MCP server directly - no BoltDB locking issues
# ============================================================================

## Run Tier 1 validation tests (must pass 100%)
validate-tier1:
	@echo "Running Tier 1 validation tests..."
	@CGO_ENABLED=1 go test -v -run TestTier1 ./internal/validation/...

## Run Tier 2 validation tests (should pass 75%)
validate-tier2:
	@echo "Running Tier 2 validation tests..."
	@CGO_ENABLED=1 go test -v -run TestTier2 ./internal/validation/...

## Run full validation suite with summary
validate-all:
	@echo "Running full validation suite..."
	@CGO_ENABLED=1 go test -v -run TestValidation_FullSuite ./internal/validation/...

## Run validation benchmarks
validate-bench:
	@echo "Running validation benchmarks..."
	@CGO_ENABLED=1 go test -bench=. -benchmem -run=^$$ ./internal/validation/...

## Alias for validate-tier1
validate: validate-tier1

# ============================================================================
# Linting
# ============================================================================

lint:
	@echo "Running golangci-lint..."
	@$(GOLANGCI_LINT) run --timeout=5m

lint-fix:
	@echo "Running golangci-lint with auto-fix..."
	@$(GOLANGCI_LINT) run --fix --timeout=5m

lint-fast:
	@echo "Running golangci-lint on changed files..."
	@$(GOLANGCI_LINT) run --new-from-rev=HEAD~1 --timeout=5m

# ============================================================================
# CI Parity Checks
# Ensures local matches GitHub Actions CI exactly
# ============================================================================

ci-check:
	@echo "Running FULL CI parity check..."
	@echo "This runs ALL checks that GitHub Actions runs."
	@echo ""
	@./scripts/ci-parity-check.sh --full

ci-check-quick:
	@echo "Running QUICK CI parity check..."
	@echo "This runs critical checks only (tests + lint)."
	@echo ""
	@./scripts/ci-parity-check.sh --quick

# Fast commit check (no tests, lint-fast only) - used by pre-commit hook
ci-check-commit:
	@./scripts/ci-parity-check.sh --commit

# Pre-push check (alias for ci-check-quick)
ci-check-push: ci-check-quick

# ============================================================================
# Development
# ============================================================================

# Install git hooks
install-hooks:
	@echo "Installing git hooks..."
	@mkdir -p .git/hooks
	@cp scripts/pre-commit.sh .git/hooks/pre-commit 2>/dev/null || \
		echo "Note: scripts/pre-commit.sh not yet created"
	@chmod +x .git/hooks/pre-commit 2>/dev/null || true
	@echo "Git hooks installed"

# Install binary to /usr/local/bin (requires sudo)
install: build
	@echo "Installing $(BINARY_NAME) to /usr/local/bin/..."
	@sudo cp bin/$(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)
	@sudo chmod +x /usr/local/bin/$(BINARY_NAME)
	@# BUG-037 Fix: Clear xattrs AND re-sign on macOS to prevent Gatekeeper SIGKILL
	@# See: https://github.com/astral-sh/uv/issues/16726 for same issue/fix
	@if [ "$$(uname -s)" = "Darwin" ]; then \
		sudo xattr -cr /usr/local/bin/$(BINARY_NAME) 2>/dev/null || true; \
		sudo codesign --force --deep --sign - /usr/local/bin/$(BINARY_NAME) 2>/dev/null || true; \
	fi
	@echo "Installed: /usr/local/bin/$(BINARY_NAME)"
	@# Auto-upgrade user config if it exists
	@if [ -f "$${XDG_CONFIG_HOME:-$(HOME)/.config}/amanmcp/config.yaml" ]; then \
		echo ""; \
		echo "Upgrading user configuration..."; \
		/usr/local/bin/$(BINARY_NAME) config init --force || true; \
	fi
	@echo "Verify: amanmcp --version"

# Install binary to user's Go bin (no sudo required)
install-user: build
	@echo "Installing $(BINARY_NAME) to ~/go/bin/..."
	@mkdir -p $(HOME)/go/bin
	@cp bin/$(BINARY_NAME) $(HOME)/go/bin/$(BINARY_NAME)
	@chmod +x $(HOME)/go/bin/$(BINARY_NAME)
	@# BUG-037 Fix: Clear xattrs AND re-sign on macOS to prevent Gatekeeper SIGKILL
	@# See: https://github.com/astral-sh/uv/issues/16726 for same issue/fix
	@if [ "$$(uname -s)" = "Darwin" ]; then \
		xattr -cr $(HOME)/go/bin/$(BINARY_NAME) 2>/dev/null || true; \
		codesign --force --deep --sign - $(HOME)/go/bin/$(BINARY_NAME) 2>/dev/null || true; \
	fi
	@echo "Installed: $(HOME)/go/bin/$(BINARY_NAME)"
	@# Auto-upgrade user config if it exists
	@if [ -f "$${XDG_CONFIG_HOME:-$(HOME)/.config}/amanmcp/config.yaml" ]; then \
		echo ""; \
		echo "Upgrading user configuration..."; \
		$(HOME)/go/bin/$(BINARY_NAME) config init --force || true; \
	fi
	@echo ""
	@echo "To make this permanent, add to your shell config:"
	@echo ""
	@echo "  For zsh (~/.zshrc):"
	@echo "    export PATH=\"\$$HOME/go/bin:\$$PATH\""
	@echo ""
	@echo "  For bash (~/.bashrc):"
	@echo "    export PATH=\"\$$HOME/go/bin:\$$PATH\""
	@echo ""
	@echo "Then reload: source ~/.zshrc (or ~/.bashrc)"

# Install to ~/.local/bin (XDG standard, no sudo required) - RECOMMENDED
# This is the preferred method for end users
install-local: build
	@echo "Installing $(BINARY_NAME) to ~/.local/bin/..."
	@mkdir -p $(HOME)/.local/bin
	@cp bin/$(BINARY_NAME) $(HOME)/.local/bin/$(BINARY_NAME)
	@chmod +x $(HOME)/.local/bin/$(BINARY_NAME)
	@# BUG-037 Fix: Clear xattrs AND re-sign on macOS to prevent Gatekeeper SIGKILL
	@# See: https://github.com/astral-sh/uv/issues/16726 for same issue/fix
	@if [ "$$(uname -s)" = "Darwin" ]; then \
		xattr -cr $(HOME)/.local/bin/$(BINARY_NAME) 2>/dev/null || true; \
		codesign --force --deep --sign - $(HOME)/.local/bin/$(BINARY_NAME) 2>/dev/null || true; \
	fi
	@echo ""
	@echo "Installed: $(HOME)/.local/bin/$(BINARY_NAME)"
	@# Auto-upgrade user config if it exists
	@if [ -f "$${XDG_CONFIG_HOME:-$(HOME)/.config}/amanmcp/config.yaml" ]; then \
		echo ""; \
		echo "Upgrading user configuration..."; \
		$(HOME)/.local/bin/$(BINARY_NAME) config init --force || true; \
	fi
	@echo ""
	@# Auto-configure PATH in shell config (FEAT-UX3)
	@SHELL_RC=""; \
	if [ "$${SHELL}" = "/bin/zsh" ] || [ "$${SHELL}" = "/usr/bin/zsh" ]; then \
		SHELL_RC="$${HOME}/.zshrc"; \
	elif [ "$${SHELL}" = "/bin/bash" ] || [ "$${SHELL}" = "/usr/bin/bash" ]; then \
		SHELL_RC="$${HOME}/.bashrc"; \
	fi; \
	if [ -n "$$SHELL_RC" ] && [ -f "$$SHELL_RC" ]; then \
		if grep -q '$$HOME/.local/bin' "$$SHELL_RC" 2>/dev/null; then \
			echo "PATH already configured in $$SHELL_RC"; \
		else \
			echo '' >> "$$SHELL_RC"; \
			echo '# Added by AmanMCP installer' >> "$$SHELL_RC"; \
			echo 'export PATH="$$HOME/.local/bin:$$PATH"' >> "$$SHELL_RC"; \
			echo "PATH configured in $$SHELL_RC"; \
		fi; \
		echo ""; \
		echo "Run: source $$SHELL_RC (or restart terminal)"; \
	else \
		echo "Add to your shell config:"; \
		echo "  export PATH=\"\$$HOME/.local/bin:\$$PATH\""; \
	fi

# Install log viewer to ~/.local/bin
install-local-logs: build-logs
	@echo "Installing amanmcp-logs to ~/.local/bin/..."
	@mkdir -p $(HOME)/.local/bin
	@cp bin/amanmcp-logs $(HOME)/.local/bin/amanmcp-logs
	@chmod +x $(HOME)/.local/bin/amanmcp-logs
	@# BUG-037 Fix: Clear xattrs AND re-sign on macOS
	@if [ "$$(uname -s)" = "Darwin" ]; then \
		xattr -cr $(HOME)/.local/bin/amanmcp-logs 2>/dev/null || true; \
		codesign --force --deep --sign - $(HOME)/.local/bin/amanmcp-logs 2>/dev/null || true; \
	fi
	@echo "Installed: $(HOME)/.local/bin/amanmcp-logs"

# Install all binaries to ~/.local/bin
install-local-all: install-local install-local-logs
	@echo "All binaries installed to ~/.local/bin/"

# Uninstall from ~/.local/bin
uninstall-local:
	@echo "Removing $(BINARY_NAME) from ~/.local/bin/..."
	@rm -f $(HOME)/.local/bin/$(BINARY_NAME)
	@echo "Uninstalled"
	@echo ""
	@echo "Note: User data preserved at ~/.amanmcp/"
	@echo "To remove all data: rm -rf ~/.amanmcp"

# Uninstall binary from /usr/local/bin
uninstall:
	@echo "Removing $(BINARY_NAME) from /usr/local/bin/..."
	@sudo rm -f /usr/local/bin/$(BINARY_NAME)
	@echo "Uninstalled"

# Run the server (for development)
run:
	@go run $(CMD_PATH) serve

# Check version consistency across files
check-versions:
	@./scripts/check-version-consistency.sh

# Verify checkpoint completeness before commit
verify-checkpoint:
	@./scripts/verify-checkpoint.sh

# Check for documentation drift
verify-docs:
	@./scripts/verify-docs.sh

# Verify SSOT (Single Source of Truth) consistency
verify-ssot:
	@./scripts/verify-ssot-consistency.sh

# Run all verification checks
verify-all: verify-docs verify-checkpoint verify-ssot
	@echo "All verification checks complete"


# ============================================================================
# Benchmarking (F23 Performance Optimization)
# ============================================================================

.PHONY: bench bench-baseline bench-compare bench-search bench-store

## Run all performance benchmarks
bench:
	@echo "Running performance benchmarks..."
	@CGO_ENABLED=1 go test -bench=. -benchmem -count=3 -run=^$$ ./internal/search/... ./internal/store/... 2>&1 | tee bench.txt
	@echo ""
	@echo "Benchmark results saved to bench.txt"

## Run search engine benchmarks only
bench-search:
	@echo "Running search engine benchmarks..."
	@CGO_ENABLED=1 go test -bench=. -benchmem -count=3 -run=^$$ ./internal/search/...

## Run store benchmarks only
bench-store:
	@echo "Running store benchmarks..."
	@CGO_ENABLED=1 go test -bench=. -benchmem -count=3 -run=^$$ ./internal/store/...

## Create new benchmark baseline (run after significant changes)
bench-baseline:
	@echo "Creating benchmark baseline..."
	@CGO_ENABLED=1 go test -bench=. -benchmem -count=5 -run=^$$ ./internal/search/... ./internal/store/... 2>&1 | tee baseline.txt
	@echo ""
	@echo "Baseline saved to baseline.txt"

## Compare current benchmarks against baseline (fails on >20% regression)
bench-compare: bench
	@echo ""
	@echo "Comparing benchmarks against baseline..."
	@if [ ! -f baseline.txt ]; then \
		echo "Error: baseline.txt not found. Run 'make bench-baseline' first."; \
		exit 1; \
	fi
	@go run scripts/bench-compare.go bench.txt baseline.txt

## Generate test corpus for large-scale benchmarks
bench-corpus:
	@echo "Generating test corpus..."
	@go run scripts/generate-test-corpus.go -files 1000 -output testdata/bench
	@echo "Test corpus generated in testdata/bench/"


# ============================================================================
# MLX Server (Apple Silicon Only)
# ============================================================================

## Install MLX embedding server (Apple Silicon only)
## Sets up Python venv, installs dependencies, and downloads the model
install-mlx:
	@# Check for Apple Silicon
	@if [ "$$(uname -m)" != "arm64" ] || [ "$$(uname -s)" != "Darwin" ]; then \
		echo ""; \
		echo "Error: MLX requires Apple Silicon (M1/M2/M3/M4 Mac)"; \
		echo ""; \
		echo "Use Ollama instead:"; \
		echo "  brew install ollama && ollama serve"; \
		echo "  ollama pull qwen3-embedding:0.6b"; \
		echo ""; \
		exit 1; \
	fi
	@# Check for Python 3
	@if ! command -v python3 >/dev/null 2>&1; then \
		echo "Error: Python 3 is required. Install with: brew install python3"; \
		exit 1; \
	fi
	@echo ""
	@echo "=============================================="
	@echo "  Installing MLX Embedding Server"
	@echo "=============================================="
	@echo ""
	@echo "Step 1/3: Creating Python virtual environment..."
	@cd mlx-server && python3 -m venv .venv
	@echo "Step 2/3: Installing dependencies (this may take a minute)..."
	@cd mlx-server && .venv/bin/pip install --quiet --upgrade pip
	@cd mlx-server && .venv/bin/pip install --quiet -r requirements.txt
	@echo ""
	@echo "Step 3/3: Model download"
	@echo ""
	@read -p "Download Qwen3-0.6B model (~1.5GB)? [Y/n] " answer; \
	if [ "$$answer" != "n" ] && [ "$$answer" != "N" ]; then \
		cd mlx-server && .venv/bin/python download_model.py small; \
	else \
		echo ""; \
		echo "Skipped. Model will download on first use."; \
	fi
	@echo ""
	@echo "=============================================="
	@echo "  MLX Server Ready!"
	@echo "=============================================="
	@echo ""
	@echo "To start the server:"
	@echo "  make start-mlx"
	@echo ""
	@echo "Or manually:"
	@echo "  cd mlx-server && .venv/bin/python server.py"
	@echo ""
	@echo "Then use with amanmcp:"
	@echo "  amanmcp index --backend=mlx ."
	@echo ""

## Start MLX server (after install-mlx)
start-mlx:
	@if [ ! -f mlx-server/.venv/bin/python ]; then \
		echo "Error: MLX server not installed. Run 'make install-mlx' first."; \
		exit 1; \
	fi
	@echo "Starting MLX server on port 9659..."
	@echo "Press Ctrl+C to stop"
	@echo ""
	@cd mlx-server && .venv/bin/python server.py

# ============================================================================
# Backend Management (FEAT-UX3)
# ============================================================================

## Install Ollama and pull default model (cross-platform)
install-ollama:
	@echo "Installing Ollama..."
	@if ! command -v ollama >/dev/null 2>&1; then \
		if [ "$$(uname -s)" = "Darwin" ]; then \
			echo "Installing via Homebrew..."; \
			brew install ollama || { echo "Failed. Install from https://ollama.ai"; exit 1; }; \
		else \
			echo "Please install Ollama from https://ollama.ai"; \
			exit 1; \
		fi; \
	else \
		echo "Ollama already installed"; \
	fi
	@echo ""
	@echo "Starting Ollama..."
	@if [ "$$(uname -s)" = "Darwin" ]; then \
		open -a Ollama 2>/dev/null || (ollama serve &); \
	else \
		ollama serve & \
	fi
	@sleep 3
	@echo "Pulling default model (qwen3-embedding:0.6b)..."
	@ollama pull qwen3-embedding:0.6b
	@echo ""
	@echo "Ollama ready!"
	@echo "  Model: qwen3-embedding:0.6b"
	@echo "  Endpoint: http://localhost:11434"

## Start Ollama server
start-ollama:
	@if pgrep -x ollama >/dev/null 2>&1 || curl -s http://localhost:11434/api/tags >/dev/null 2>&1; then \
		echo "Ollama is already running"; \
	else \
		echo "Starting Ollama..."; \
		if [ "$$(uname -s)" = "Darwin" ]; then \
			open -a Ollama 2>/dev/null || (ollama serve &); \
		else \
			ollama serve & \
		fi; \
		sleep 3; \
		echo "Ollama started"; \
	fi

## Stop Ollama server
stop-ollama:
	@echo "Stopping Ollama..."
	@if [ "$$(uname -s)" = "Darwin" ]; then \
		osascript -e 'quit app "Ollama"' 2>/dev/null || true; \
	fi
	@pkill -x ollama 2>/dev/null || true
	@echo "Ollama stopped"

## Switch to MLX backend
switch-backend-mlx:
	@./scripts/switch-backend.sh mlx

## Switch to Ollama backend
switch-backend-ollama:
	@./scripts/switch-backend.sh ollama

## Verify installation is working
verify-install:
	@echo "=== AmanMCP Installation Verification ==="
	@echo ""
	@echo "1. Checking binary..."
	@if command -v amanmcp >/dev/null 2>&1; then \
		echo "   OK - Found at $$(which amanmcp)"; \
		amanmcp version 2>/dev/null || true; \
	elif [ -f "$(HOME)/.local/bin/amanmcp" ]; then \
		echo "   WARN - Binary exists but not in PATH"; \
		echo "   Run: source ~/.zshrc (or ~/.bashrc)"; \
	else \
		echo "   FAIL - Binary not found"; \
		echo "   Run: make install-local"; \
		exit 1; \
	fi
	@echo ""
	@echo "2. Checking embedder..."
	@if curl -s http://localhost:9659/health >/dev/null 2>&1; then \
		echo "   OK - MLX server running on port 9659"; \
	elif curl -s http://localhost:11434/api/tags >/dev/null 2>&1; then \
		echo "   OK - Ollama running on port 11434"; \
	else \
		echo "   WARN - No embedder running"; \
		echo "   Run: make start-ollama (or make start-mlx)"; \
	fi
	@echo ""
	@echo "=== Verification Complete ==="
