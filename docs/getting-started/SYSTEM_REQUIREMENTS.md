# System Requirements

Quick reference for AmanMCP system requirements.

## Quick Check

Run diagnostics to verify your system:

```bash
amanmcp doctor
```

---

## Hardware Requirements

| Component | Minimum | Recommended | Notes |
|-----------|---------|-------------|-------|
| **RAM** | 16 GB | 24 GB+ | See memory sizing guide below |
| **Disk Space** | 100 MB free | 1 GB+ | Index size depends on codebase |
| **File Descriptors** | 1024 | 10240+ | `ulimit -n` on Unix |

---

## Software Requirements

| Software | Version | Required | Notes |
|----------|---------|----------|-------|
| **Go** | 1.25.5+ | Build only | Not needed at runtime |
| **CGO Toolchain** | Any | Build only | For tree-sitter only |

**Note:** v0.1.38+ uses gollama.cpp (purego) for embeddings - no external dependencies needed.

---

## Platform Support

| Platform | Status | Notes |
|----------|--------|-------|
| **macOS 12+** | Full | Primary development platform |
| **Linux (Ubuntu 20.04+)** | Full | AMD64 and ARM64 |
| **Windows 10/11** | Community | See Windows notes below |

---

## Memory Sizing Guide

Choose vector quantization based on your codebase size:

| Codebase Size | RAM Used (F16) | RAM Used (I8) | Recommended RAM |
|---------------|----------------|---------------|-----------------|
| < 10K files | ~50 MB | ~25 MB | 16 GB |
| 10K-50K files | ~100 MB | ~50 MB | 16 GB |
| 50K-100K files | ~200 MB | ~100 MB | 24 GB |
| 100K-300K files | ~400 MB | ~200 MB | 32 GB |

**Configuration:**
```yaml
# .amanmcp.yaml
vector_store:
  quantization: F16  # Default: good quality/size balance
  # quantization: I8  # Half memory, slight quality loss
  # quantization: F32 # Best quality, 2x memory
```

---

## Disk Space Guide

| Component | Size | Notes |
|-----------|------|-------|
| Binary | ~60 MB | Standalone executable |
| Index (per 10K files) | ~20 MB | BM25 + vectors + metadata |
| Embedding model | ~138 MB | nomic-embed-text-v1.5 (auto-download) |

**Index location:** `.amanmcp/` in your project root

---

## Pre-Flight Checks

AmanMCP validates your system on first run:

| Check | Threshold | Required |
|-------|-----------|----------|
| Disk space | 100 MB free | Yes |
| Memory | 1 GB available | Yes |
| Write permissions | Can create files | Yes |
| File descriptors | 1024 limit | Yes |

**Skip checks:** `amanmcp --skip-check`

---

## Troubleshooting

### "Disk space insufficient"

Ensure at least 100 MB free where your project lives:

```bash
df -h .
```

### "File descriptor limit too low"

Increase the limit (Unix):

```bash
# Temporary (current session)
ulimit -n 10240

# Permanent (add to ~/.bashrc or ~/.zshrc)
ulimit -n 10240
```

### "Embedding model not available"

The embedding model downloads automatically on first use:

```bash
# Force re-download/setup
amanmcp setup

# Use offline mode (static embeddings)
amanmcp --offline
```

### "Write permission denied"

Check directory permissions:

```bash
ls -la .
# Ensure you own the directory or have write access
```

---

## Windows Notes

Windows support is community-contributed. Known considerations:

1. **CGO required** - Install MinGW or MSVC toolchain
2. **Path handling** - Use forward slashes in config
3. **File watching** - Uses polling (no fsnotify on Windows)
4. **Ollama** - Download from https://ollama.com/download/windows

---

## Network Requirements

AmanMCP runs entirely locally. Network is only needed for:

| Feature | Network Required |
|---------|-----------------|
| Indexing | No |
| Search | No |
| Ollama embeddings | Localhost only (port 11434) |
| Model download | Yes (one-time during setup) |

**Airgapped environments:** Use `--offline` mode with static embeddings.

---

## See Also

- [README.md](README.md) - Getting started
- [Architecture](docs/architecture/architecture.md) - System design
- [Requirements](docs/requirements.md) - Full requirements document
