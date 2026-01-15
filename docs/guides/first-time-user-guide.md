# Getting Started Guide

**Welcome to AmanMCP!** This guide will get you up and running step-by-step.

---

> **USE AT YOUR OWN RISK**
>
> AmanMCP is experimental software in active development. By using this software:
>
> - You acknowledge this is **alpha/beta quality** software
> - You accept **full responsibility** for any issues that may occur
> - You understand the developers are **not liable** for data loss, system issues, or other problems
> - You are encouraged to **review the source code** before use
> - You should **backup your data** before running on important projects
>
> This software is provided "AS IS" without warranty of any kind.

---

## What is AmanMCP?

AmanMCP gives your AI coding assistant (like Claude Code or Cursor) deep knowledge of your codebase.

- **It runs locally** - Your code never leaves your machine
- **Zero configuration** - Just run `amanmcp init` in your project
- **Smart search** - Finds code by meaning, not just keywords

```
You ask Claude: "How does authentication work?"

Without AmanMCP: Claude guesses based on common patterns
With AmanMCP:    Claude searches YOUR code and gives specific answers
```

---

## Why AmanMCP?

| Benefit | Description |
|---------|-------------|
| **100% Local** | Your code never leaves your machine |
| **Zero Config** | Just run `amanmcp init` - no setup files |
| **Fast Search** | Hybrid BM25 + semantic search < 100ms |
| **Code-Aware** | AST-based chunking preserves functions/classes |
| **Privacy First** | No telemetry, no cloud, no tracking |

### Features

- **Hybrid Search** - Combines keyword (BM25) + semantic (vector) search
- **AST Chunking** - Uses tree-sitter for intelligent code splitting
- **Multi-Language** - Go, TypeScript, JavaScript, Python, HTML, CSS
- **Auto-Discovery** - Detects project type and structure automatically
- **Incremental Updates** - Only re-indexes changed files
- **Claude Code Integration** - Native MCP protocol support

---

## Prerequisites

### Tested Hardware

- **Reference Machine:** MacBook Pro M4 with 24GB RAM
- **Minimum:** Apple Silicon Mac with 16GB RAM
- **Note:** Indexing on lower-spec machines will be slower - please be patient!

### System Requirements

| Requirement | Minimum | Recommended |
|-------------|---------|-------------|
| **OS** | macOS 13 (Ventura) | macOS 14+ (Sonoma/Sequoia) |
| **RAM** | 16 GB | 24 GB+ |
| **Disk Space** | 500 MB | 2 GB |
| **Processor** | Apple Silicon | Apple Silicon M3/M4 |

### Before You Start

- [ ] Close unnecessary applications to free up RAM
- [ ] Ensure at least 2GB free disk space (5GB if using MLX 8B model)
- [ ] Have Homebrew installed (recommended)
- [ ] Have a project you want to index
- [ ] (Apple Silicon) Python 3.9+ for MLX server

---

## Installation (Step-by-Step)

Follow these steps in order for the best experience.

### Step 1: Set Up Embedding Backend

Choose the option appropriate for your platform:

#### Option A: MLX Server (Apple Silicon - Default)

MLX is the **default on Apple Silicon** (M1/M2/M3/M4), providing **~1.7x faster** embeddings than Ollama.

```bash
# Navigate to mlx-server directory (bundled with AmanMCP source)
# If you installed via Homebrew, clone the repo first:
git clone https://github.com/Aman-CERP/amanmcp.git
cd amanmcp/mlx-server

# Create virtual environment and install dependencies
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt

# Start the server (keep this terminal open)
python server.py
```

> **First run:** Downloads the embedding model (~4.5GB for 8B). This takes 5-15 minutes once.

**Verify MLX is running:**
```bash
curl http://localhost:9659/health
```

Expected output: `{"status":"healthy","model_status":"ready",...}`

> **Skip to Step 3** if using MLX - you don't need Ollama!

#### Option B: Ollama (Default on Non-Apple Silicon)

Use Ollama if you're not on Apple Silicon. Also serves as automatic fallback when MLX is unavailable.

```bash
# Install via Homebrew
brew install ollama

# Start Ollama (keep this terminal open)
ollama serve
```

In a **new terminal window**, pull the embedding model:

```bash
# Pull the recommended embedding model (~2GB download)
ollama pull qwen3-embedding:8b
```

Verify the model is available:

```bash
ollama list
```

You should see `qwen3-embedding:8b` in the list.

### Step 2: (Optional) Auto-Start MLX Server

If using MLX, set up auto-start so the server runs on login:

Create `~/Library/LaunchAgents/com.amanmcp.mlx-server.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.amanmcp.mlx-server</string>
    <key>ProgramArguments</key>
    <array>
        <string>/path/to/mlx-server/.venv/bin/python</string>
        <string>/path/to/mlx-server/server.py</string>
    </array>
    <key>WorkingDirectory</key>
    <string>/path/to/mlx-server</string>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
```

Enable with: `launchctl load ~/Library/LaunchAgents/com.amanmcp.mlx-server.plist`

### Step 3: Install AmanMCP

Choose one of these installation methods:

**Option A: Homebrew (Recommended)**

```bash
brew tap Aman-CERP/tap
brew install amanmcp
```

**Option B: Install Script**

```bash
curl -sSL https://raw.githubusercontent.com/Aman-CERP/amanmcp/main/scripts/install.sh | sh
source ~/.zshrc  # or restart your terminal
```

**Option C: Build from Source (Developers)**

Requires Go 1.25.5+ and C toolchain:

```bash
git clone https://github.com/Aman-CERP/amanmcp
cd amanmcp
make install-local
source ~/.zshrc
```

**Option D: Manual Download (Air-Gapped Systems)**

For systems without internet access or manual control:

1. **Download** from [GitHub Releases](https://github.com/Aman-CERP/amanmcp/releases)
   - macOS Apple Silicon: `amanmcp_X.X.X_darwin_arm64.tar.gz`
   - macOS Intel: `amanmcp_X.X.X_darwin_amd64.tar.gz`
   - Linux x64: `amanmcp_X.X.X_linux_amd64.tar.gz`
   - Linux ARM: `amanmcp_X.X.X_linux_arm64.tar.gz`

2. **Extract and install** (no sudo required):
```bash
tar -xzf amanmcp_*.tar.gz
mkdir -p ~/.local/bin
cp amanmcp ~/.local/bin/
chmod +x ~/.local/bin/amanmcp
```

3. **Configure PATH** (add to `~/.zshrc` or `~/.bashrc`):
```bash
export PATH="$HOME/.local/bin:$PATH"
```

### Step 4: Verify Installation

```bash
amanmcp version
```

**Expected output:**

```
amanmcp 0.1.43 (commit: f6a73f7, built: 2026-01-05T07:22:29Z, go: go1.25.5)
```

If you see `zsh: killed`, see the [Troubleshooting](#troubleshooting) section below.

### Step 5: Initialize in Your Project

Navigate to your project and run the init command:

```bash
cd /path/to/your/project
amanmcp init
```

**What `amanmcp init` does:**

- Configures Claude Code MCP integration (creates `.mcp.json`)
- Generates `.amanmcp.yaml` configuration template
- Indexes your project (shows progress bar)
- Verifies embedder availability

**Init Command Options:**

| Flag | Purpose |
|------|---------|
| `--force` | Overwrite existing `.mcp.json` configuration |
| `--config-only` | Fix config without reindexing (useful for config fixes) |
| `--offline` | Use static embeddings (no Ollama required) |
| `--global` | Configure for all projects (user scope) |
| `--resume` | Resume interrupted indexing from checkpoint |

**Example output:**

```
$ amanmcp init
AmanMCP v0.1.43 - Initializing...

Project: /Users/you/your-project

Configuring MCP integration...
Added MCP server (project scope)

Indexing project...
[████████████████████░░░░░░░░░░] 67% (568/847) - ETA: 4s
Indexed 847 files in 12.3s

Embedder: OllamaEmbedder

Initialization complete!

Next steps:
   1. Restart Claude Code to activate MCP server
   2. Test with: "Search my codebase for..."
   3. Run 'amanmcp doctor' to verify setup
```

### Step 6: Restart Claude Code

Restart VS Code with Claude Code extension to activate the MCP server.

- Press `Cmd+Shift+P` and select "Developer: Reload Window"
- Or simply close and reopen VS Code

### Step 7: Test It!

In Claude Code, ask a question about your codebase:

> "Search my codebase for authentication"

If AmanMCP is working, you'll get results from YOUR code, not generic answers.

---

## Performance Expectations

| Codebase Size | Indexing Time | Memory Usage |
|---------------|---------------|--------------|
| Small (<1K files) | ~30 seconds | ~200 MB |
| Medium (1K-10K files) | 1-5 minutes | ~500 MB |
| Large (10K+ files) | 5-15 minutes | ~1 GB+ |

### Lower-Spec Machines

If you have less than 24GB RAM:

- **Expect longer indexing times** (2-3x slower than reference machine)
- **Close other applications** before indexing to free up RAM
- **Use `--offline` flag** if Ollama is too resource-intensive:

```bash
amanmcp init --offline
```

- **Be patient** - first index takes longest, subsequent updates are fast

> **Important:** AmanMCP was developed and tested on a MacBook Pro M4 with 24GB RAM. If you're using a machine with less RAM, indexing will be slower and may consume significant system resources. Close other applications before running.

---

## How It Works

```
Your Project                    AmanMCP                      AI Assistant
     │                              │                              │
     │  1. Scans files              │                              │
     │ ◄────────────────────────────│                              │
     │                              │                              │
     │  2. Creates smart chunks     │                              │
     │  (preserves functions,       │                              │
     │   classes, documentation)    │                              │
     │                              │                              │
     │  3. Builds search index      │                              │
     │  (keyword + semantic)        │                              │
     │                              │                              │
     │                              │  4. "Find auth code"         │
     │                              │ ◄─────────────────────────────│
     │                              │                              │
     │                              │  5. Returns relevant code    │
     │                              │ ─────────────────────────────►│
     │                              │                              │
```

---

## Common Commands

| Command | What It Does |
|---------|--------------|
| `amanmcp init` | **Initialize project** (configure MCP + index) |
| `amanmcp init --force` | Reinitialize, overwrite existing config |
| `amanmcp init --config-only` | Fix config without reindexing |
| `amanmcp init --offline` | Initialize without Ollama (uses static embeddings) |
| `amanmcp` | Start the server (indexes first if needed) |
| `amanmcp serve` | Start MCP server only |
| `amanmcp status` | Show index status (files, chunks, size) |
| `amanmcp search "query"` | Search your codebase manually |
| `amanmcp search -t code "func"` | Search only code files |
| `amanmcp doctor` | Check system health and diagnose issues |
| `amanmcp compact` | Optimize vector index |
| `amanmcp daemon start` | Background search server (faster CLI) |
| `amanmcp version` | Show version information |

### Examples

```bash
# Check how many files are indexed
amanmcp status

# Search for something
amanmcp search "user authentication"

# Re-index after major code changes
amanmcp --reindex

# Diagnose problems
amanmcp doctor
```

---

## Troubleshooting

### "zsh: killed" Error (macOS)

**Symptom:** Running `amanmcp version` shows `zsh: killed amanmcp version`

**Cause:** macOS Gatekeeper blocking execution due to extended attributes.

**Fix:**

```bash
# Remove extended attributes
xattr -cr ~/.local/bin/amanmcp

# Verify it works
amanmcp version
```

### Slow Indexing

**Symptoms:** Indexing takes much longer than expected, system becomes sluggish.

**Fixes:**

- Close other applications to free RAM
- Use `--offline` flag for static embeddings (faster, less accurate):

```bash
amanmcp init --offline
```

- Check available disk space
- Run `amanmcp doctor` for diagnostics

### Interrupted Indexing

**Symptom:** Indexing was interrupted (Ctrl+C, error, or system restart) and you want to continue.

**Fix:** Use the `--resume` flag to continue from the last checkpoint:

```bash
# Resume from where you left off
amanmcp init --resume

# Or with the index command
amanmcp index --resume
```

Checkpoints are saved after every 32 chunks, so you won't lose much progress.

### MLX Server Issues (Apple Silicon)

**Symptom:** "MLX health check failed" or falls back to Ollama

**Fixes:**

```bash
# Check if MLX server is running
curl http://localhost:9659/health

# If not running, start it
cd /path/to/mlx-server
source .venv/bin/activate
python server.py

# Check for error logs
tail -f /tmp/amanmcp-mlx-server.err  # If using LaunchAgent
```

**Symptom:** MLX server won't start

```bash
# Check Python version (need 3.9+)
python3 --version

# Verify Apple Silicon
uname -m  # Should show 'arm64'

# Check port 9659 is free
lsof -i :9659

# If something is using the port, kill it or change MLX port
```

**Symptom:** Model download fails

```bash
# Check disk space (need ~5GB for 8B model)
df -h ~

# Clear cache and retry
rm -rf ~/.amanmcp/models/mlx/hub
python server.py
```

### Ollama Connection Issues

**Symptom:** Error connecting to Ollama or "embedder not available"

**Fixes:**

```bash
# Check if Ollama is running
curl http://localhost:11434/api/tags

# If not running, start it
ollama serve

# Verify model is pulled
ollama list
```

### Out of Memory

**Symptom:** System becomes unresponsive, indexing crashes.

**Fixes:**

- Close other applications
- Reduce codebase scope with `.amanmcp.yaml` exclusions:

```yaml
# .amanmcp.yaml in your project root
exclude:
  - node_modules/**
  - dist/**
  - vendor/**
```

- Use `--offline` mode (uses less memory)
- Consider a machine with more RAM for large codebases

### "command not found: amanmcp"

**Cause:** PATH not configured properly.

**Fix:**

```bash
# Add to your shell config
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

---

## Debugging Tools

When troubleshooting, these tools provide visibility into AmanMCP operations.

### View Server Logs

Enable logging and view in real-time:

```bash
# Terminal 1: Start with logging
amanmcp --debug serve

# Terminal 2: Follow logs
amanmcp-logs -f

# View MLX server logs
amanmcp-logs --source mlx -f

# View ALL logs (Go + MLX) merged by timestamp
amanmcp-logs --source all -f
```

**Log viewer options:**

| Flag | Description |
|------|-------------|
| `-f, --follow` | Follow logs in real-time |
| `-n, --lines N` | Show last N lines (default: 50) |
| `--level LEVEL` | Filter: debug, info, warn, error |
| `--filter PATTERN` | Filter by regex pattern |
| `--source SOURCE` | Log source: `go`, `mlx`, or `all` (default: go) |

**Log locations:**

| Source | File |
|--------|------|
| Go server | `~/.amanmcp/logs/server.log` |
| MLX server | `~/.amanmcp/logs/mlx-server.log` |

### Monitor System Performance

For performance debugging, use terminal-based monitors:

```bash
# Terminal 1: amanmcp with logging
amanmcp --debug serve

# Terminal 2: Follow all logs (Go + MLX)
amanmcp-logs --source all -f

# Terminal 3: CPU/Memory (all platforms)
htop

# Terminal 4: Apple Silicon GPU/ANE (macOS only)
asitop
```

**What to watch:**

| Tool | Metrics | Look For |
|------|---------|----------|
| `htop` | CPU, Memory, Processes | High CPU during indexing, memory leaks |
| `asitop` | GPU, ANE, Thermal | GPU usage during embedding generation |
| `amanmcp-logs` | Operations, Errors | Slow queries, embedder failures |

**Install monitoring tools:**

```bash
# htop (cross-platform)
brew install htop

# asitop (macOS Apple Silicon only)
pip3 install asitop
sudo asitop  # Requires sudo for power metrics
```

---

## FAQ

### "How do I update AmanMCP?"

```bash
# If installed via Homebrew
brew upgrade amanmcp

# If installed via install script
curl -sSL https://raw.githubusercontent.com/Aman-CERP/amanmcp/main/scripts/install.sh | sh

# If installed manually - download new version from GitHub releases
```

### "How do I uninstall AmanMCP?"

```bash
# If installed via Homebrew
brew uninstall amanmcp

# If installed via install script
rm ~/.local/bin/amanmcp

# To also remove user data
rm -rf ~/.amanmcp

# To remove project-specific data
rm -rf /path/to/project/.amanmcp
```

### "How do I re-index my project?"

```bash
# Force re-index
amanmcp --reindex

# Or delete the index and run again
rm -rf .amanmcp/
amanmcp init
```

### "Can I use it with multiple projects?"

Yes! Run `amanmcp init` in each project directory. Each project gets its own index.

```bash
# Terminal 1
cd ~/projects/frontend
amanmcp init

# Terminal 2
cd ~/projects/backend
amanmcp init
```

### "Can I use it offline?"

Yes! Use the `--offline` flag to use static embeddings (no Ollama required):

```bash
amanmcp init --offline
```

This provides basic search quality without requiring the Ollama service.

---

## Getting Help

### Self-Diagnosis

```bash
amanmcp doctor
```

This checks:

- System requirements
- File permissions
- Index health
- Embedder status (Ollama connectivity)

### Report Issues

If you're stuck, open an issue:

**GitHub:** https://github.com/Aman-CERP/amanmcp/issues

Include:

- Output of `amanmcp doctor`
- Output of `amanmcp version`
- Your system specs (RAM, macOS version)
- What you expected vs what happened

---

## What's Next?

Now that you're set up:

1. **Ask questions about your code** - Test semantic search with Claude Code
2. **Explore commands** - Run `amanmcp --help` to see all options
3. **Check status** - Run `amanmcp status` to see your index
4. **Read the full documentation** - See [README.md](../../README.md) for advanced features

Happy coding!

---

## Appendix: Directory Structure

After installation, AmanMCP uses this structure:

```
INSTALLATION (user-scope):
~/.local/
└── bin/
    └── amanmcp                     # Binary

USER DATA (global):
~/.amanmcp/
└── sessions/                       # Optional named sessions

PROJECT DATA (per-project):
<project>/.amanmcp/
├── metadata.db                     # SQLite metadata
├── bm25.bleve/                     # BM25 keyword index
└── vectors.hnsw                    # Vector embeddings (coder/hnsw)
```

---

*Last updated: 2026-01-05*
