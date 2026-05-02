# Homebrew Setup Guide

**For Maintainers:** Step-by-step guide to set up and maintain the AmanMCP Homebrew tap.

---

## Overview

AmanMCP is distributed via Homebrew using a custom tap. This guide covers:

1. Creating the tap repository (one-time setup)
2. Configuring GitHub secrets
3. How automated releases work
4. Manual release process
5. Testing casks locally
6. Troubleshooting

### Installation Workflow

```mermaid
flowchart TD
    Start([User Wants to Install]) --> Tap[brew tap Aman-CERP/amanmcp]
    Tap --> Install[brew install --cask amanmcp]
    Install --> Verify[amanmcp version]
    Verify --> Success{Version<br/>Displays?}
    Success -->|Yes| Init[amanmcp init in project]
    Success -->|No| Troubleshoot[Check Troubleshooting]
    Init --> Ollama{Install<br/>Ollama?}
    Ollama -->|Yes| OllamaInstall[brew install ollama<br/>ollama pull qwen3-embedding]
    Ollama -->|No| Done([Ready to Use])
    OllamaInstall --> Done
    Troubleshoot --> CheckArch[Check Architecture]
    CheckArch --> Reinstall[brew reinstall --cask amanmcp]
    Reinstall --> Verify

    style Start fill:#e1f5ff,stroke:#01579b,stroke-width:2px
    style Done fill:#c8e6c9,stroke:#2e7d32,stroke-width:2px
    style Success fill:#fff9c4,stroke:#f57f17,stroke-width:2px
    style Ollama fill:#fff9c4,stroke:#f57f17,stroke-width:2px
    style Troubleshoot fill:#ffccbc,stroke:#d84315,stroke-width:2px
```

---

## Part 1: One-Time Setup (Create Tap Repository)

### Step 1: Create the Repository

Create a new GitHub repository:

- **Name:** `homebrew-amanmcp` (must start with `homebrew-`)
- **Visibility:** Public
- **Initialize:** with README

```bash
# Clone the new repository
git clone https://github.com/YOUR_ORG/homebrew-amanmcp.git
cd homebrew-amanmcp
```

### Step 2: Create Directory Structure

```bash
mkdir -p Casks
```

### Step 3: Create Initial Cask

Create `Casks/amanmcp.rb` with a placeholder:

```ruby
# Casks/amanmcp.rb
# This file is auto-updated by GoReleaser on each release.
# Manual edits will be overwritten.

cask "amanmcp" do
  version "0.0.0"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000"

  url "https://github.com/Aman-CERP/amanmcp/releases/download/v#{version}/amanmcp_#{version}_darwin_arm64.tar.gz"
  name "AmanMCP"
  desc "Local-first RAG MCP server for AI coding assistants"
  homepage "https://github.com/Aman-CERP/amanmcp"
  license "Apache-2.0"

  binary "amanmcp"

  caveats <<~EOS
    To get started:
      cd your-project
      amanmcp init

    For best search quality, install Ollama:
      brew install ollama && ollama serve
      ollama pull qwen3-embedding:0.6b
  EOS
end
```

### Step 4: Push to GitHub

```bash
git add .
git commit -m "Initial tap setup"
git push origin main
```

---

## Part 2: Configure GitHub Secrets

For automated cask updates, you need a Personal Access Token (PAT).

### Step 1: Generate PAT

1. Go to GitHub → Settings → Developer settings → Personal access tokens → Tokens (classic)
2. Click "Generate new token (classic)"
3. Set expiration (recommend: 1 year)
4. Select scopes:
   - `repo` (full control of private repositories)
   - `workflow` (update GitHub Action workflows)
5. Generate and copy the token

### Step 2: Add Secret to Main Repository

1. Go to `amanmcp` repository (main project, not the tap)
2. Settings → Secrets and variables → Actions
3. Click "New repository secret"
4. Name: `HOMEBREW_TAP_GITHUB_TOKEN`
5. Value: paste your PAT
6. Click "Add secret"

---

## Part 3: How Automated Releases Work

When you push a version tag, everything happens automatically:

```
┌──────────────────────────────────────────────────────────────┐
│ Push tag: git push origin v1.0.0                              │
│                         │                                     │
│                         ▼                                     │
│ ┌─────────────────────────────────────────────────────────┐  │
│ │ GitHub Actions: .github/workflows/release.yml           │  │
│ │                                                          │  │
│ │ 1. Checkout code                                         │  │
│ │ 2. Setup Go + dependencies                               │  │
│ │ 3. Run tests                                             │  │
│ │ 4. Run GoReleaser                                        │  │
│ └─────────────────────────────────────────────────────────┘  │
│                         │                                     │
│         ┌───────────────┼───────────────┐                    │
│         ▼               ▼               ▼                    │
│   Build binary     Create release   Update cask              │
│   (darwin arm64)   (GitHub)         (homebrew-amanmcp)       │
│                                                               │
└──────────────────────────────────────────────────────────────┘
```

### What GoReleaser Does

1. Builds the currently supported `darwin/arm64` binary
2. Generates SHA256 checksums
3. Creates GitHub release with assets
4. Auto-updates the cask in `homebrew-amanmcp` tap

### What Users See

After release, users can:

```bash
# First install
brew tap Aman-CERP/amanmcp
brew install --cask amanmcp

# Upgrade existing
brew upgrade --cask amanmcp
```

---

## Part 4: Manual Release Process

### Using the Release Script

```bash
# Navigate to project root
cd /path/to/amanmcp

# Run release script with version
./scripts/release.sh v1.0.0
```

### What the Script Does

1. **Validates version format** - Must be `vX.Y.Z` or `vX.Y.Z-suffix`
2. **Checks for uncommitted changes** - Fails if working tree is dirty
3. **Warns if not on main branch** - Prompts for confirmation
4. **Checks if tag exists** - Fails if tag already exists
5. **Updates VERSION file** - Writes version without `v` prefix
6. **Runs CI checks** - `make ci-check` must pass
7. **Commits VERSION change** - With conventional commit message
8. **Creates annotated tag** - With release notes
9. **Pushes to origin** - Triggers GitHub Actions

### Version Format Examples

| Version | Type | Description |
|---------|------|-------------|
| `v1.0.0` | Stable | First stable release |
| `v1.0.1` | Patch | Bug fix release |
| `v1.1.0` | Minor | New features, backwards compatible |
| `v2.0.0` | Major | Breaking changes |
| `v1.0.0-alpha.1` | Pre-release | Alpha testing |
| `v1.0.0-beta.1` | Pre-release | Beta testing |
| `v1.0.0-rc.1` | Pre-release | Release candidate |

---

## Part 5: Testing Formula Locally

### Test Before Release

```bash
# Build binary locally
make build

# Test version command works
./bin/amanmcp version

# Test with custom ldflags (simulating release build)
go build -ldflags "-X github.com/Aman-CERP/amanmcp/pkg/version.Version=1.0.0-test" \
    -o bin/amanmcp ./cmd/amanmcp
./bin/amanmcp version
```

### Test Cask Locally

```bash
# Create a local cask file for testing
cat > /tmp/amanmcp.rb << 'EOF'
cask "amanmcp" do
  version "test"
  sha256 "YOUR_SHA256"

  url "file:///path/to/your/amanmcp_test.tar.gz"
  name "AmanMCP"
  desc "Local-first RAG MCP server"
  homepage "https://github.com/Aman-CERP/amanmcp"
  binary "amanmcp"
end
EOF

# Install from local cask
brew install --cask /tmp/amanmcp.rb

# Test it works
amanmcp version
```

### Test Published Tap

```bash
# Tap the repository
brew tap Aman-CERP/amanmcp

# Check cask
brew info --cask amanmcp

# Install
brew install --cask amanmcp

# Verify
amanmcp version
which amanmcp
```

---

## Part 6: Troubleshooting

### Troubleshooting Decision Tree

```mermaid
flowchart TD
    Issue([Homebrew Issue]) --> Type{What's<br/>Wrong?}

    Type -->|Installation Fails| Install
    Type -->|Cask Not Updated| Cask
    Type -->|Binary Won't Run| Binary
    Type -->|Wrong Architecture| Arch

    Install[Installation Error] --> Cache{Checksum<br/>Mismatch?}
    Cache -->|Yes| ClearCache[brew cleanup amanmcp]
    Cache -->|No| Reinstall[brew reinstall --cask amanmcp]
    ClearCache --> RetryInstall[brew install --cask amanmcp]
    Reinstall --> CheckLogs[brew install --cask --verbose --debug amanmcp]
    RetryInstall --> Fixed1{Fixed?}
    CheckLogs --> Fixed1
    Fixed1 -->|Yes| Success([Problem Solved])
    Fixed1 -->|No| FileIssue([File GitHub Issue])

    Cask[Cask Not Updated] --> CheckToken{Token Set?}
    CheckToken -->|No| AddToken[Add HOMEBREW_TAP_GITHUB_TOKEN]
    CheckToken -->|Yes| CheckPerms{Correct<br/>Permissions?}
    AddToken --> CheckPerms
    CheckPerms -->|No| UpdatePerms[Add repo + workflow scopes]
    CheckPerms -->|Yes| CheckLogs2[Review GoReleaser Logs]
    UpdatePerms --> RerunRelease[Re-run Release Workflow]
    CheckLogs2 --> RerunRelease
    RerunRelease --> Success

    Binary[Binary Won't Run] --> Gatekeeper{Gatekeeper<br/>Blocking?}
    Gatekeeper -->|Yes| RemoveQuarantine[xattr -d com.apple.quarantine<br/>/usr/local/bin/amanmcp]
    Gatekeeper -->|No| CheckExec{Executable<br/>Permissions?}
    RemoveQuarantine --> TestRun[amanmcp version]
    CheckExec -->|No| FixPerms[chmod +x /usr/local/bin/amanmcp]
    CheckExec -->|Yes| ReinstallBinary[brew reinstall --cask amanmcp]
    FixPerms --> TestRun
    ReinstallBinary --> TestRun
    TestRun --> Success

    Arch[Wrong Architecture] --> CheckSys[uname -m<br/>Check System Arch]
    CheckSys --> CheckBin[file /usr/local/bin/amanmcp<br/>Check Binary Arch]
    CheckBin --> Mismatch{Architectures<br/>Match?}
    Mismatch -->|No| ForceReinstall[brew uninstall --cask amanmcp<br/>brew install --cask amanmcp]
    Mismatch -->|Yes| OtherIssue[Check Other Issues]
    ForceReinstall --> Success
    OtherIssue --> FileIssue

    style Issue fill:#e1f5ff,stroke:#01579b,stroke-width:2px
    style Success fill:#c8e6c9,stroke:#2e7d32,stroke-width:2px
    style FileIssue fill:#ffccbc,stroke:#d84315,stroke-width:2px
    style Type fill:#fff9c4,stroke:#f57f17,stroke-width:2px
    style Cache fill:#fff9c4,stroke:#f57f17,stroke-width:2px
    style CheckToken fill:#fff9c4,stroke:#f57f17,stroke-width:2px
    style CheckPerms fill:#fff9c4,stroke:#f57f17,stroke-width:2px
    style Gatekeeper fill:#fff9c4,stroke:#f57f17,stroke-width:2px
    style CheckExec fill:#fff9c4,stroke:#f57f17,stroke-width:2px
    style Mismatch fill:#fff9c4,stroke:#f57f17,stroke-width:2px
    style Fixed1 fill:#fff9c4,stroke:#f57f17,stroke-width:2px
```

### Cask Update Failed

**Symptom:** Release workflow succeeds but cask not updated.

**Check:**
1. Is `HOMEBREW_TAP_GITHUB_TOKEN` set?
   ```bash
   # In GitHub: repo → Settings → Secrets → Actions
   ```
2. Does PAT have correct permissions? (repo + workflow scopes)
3. Check GoReleaser logs in GitHub Actions

### Checksum Mismatch

**Symptom:** `Error: SHA256 mismatch`

**Cause:** Cask points to old binary but checksum was updated, or download corrupted.

**Solution:**
```bash
# Clear cache and retry
brew cleanup amanmcp
brew install --cask amanmcp
```

### Binary Not Signed

**Symptom:** macOS Gatekeeper blocks execution.

**Solution for users:**
```bash
# Allow the binary
xattr -d com.apple.quarantine /usr/local/bin/amanmcp
```

### Wrong Architecture

**Symptom:** Binary doesn't run (wrong arch for your Mac).

**Check:**
```bash
# Check your architecture
uname -m
# arm64 = Apple Silicon
# x86_64 = Intel

# Check binary architecture
file /usr/local/bin/amanmcp
```

**Solution:** Reinstall - Homebrew auto-selects correct architecture.

---

## Quick Reference

| Task | Command |
|------|---------|
| Release new version | `./scripts/release.sh v1.0.0` |
| Check release status | Visit GitHub Actions tab |
| Test cask locally | `brew install --cask ./Casks/amanmcp.rb` |
| Update tap | `brew tap --force-auto-update Aman-CERP/amanmcp` |
| Reinstall | `brew reinstall --cask amanmcp` |
| Check logs | `brew install --cask amanmcp --verbose --debug` |

---

## Related Files

| File | Purpose |
|------|---------|
| `.goreleaser.yaml` | GoReleaser configuration |
| `.github/workflows/release.yml` | Release workflow |
| `scripts/release.sh` | Release helper script |
| `pkg/version/version.go` | Version embedding |

---

*Last updated: 2026-01-05*
