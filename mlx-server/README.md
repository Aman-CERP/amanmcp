# AmanMCP MLX Embedding Server

High-performance text embedding server using MLX on Apple Silicon, bundled with AmanMCP.

**Original Source:** [jakedahn/qwen3-embeddings-mlx](https://github.com/jakedahn/qwen3-embeddings-mlx)
**License:** MIT

## AmanMCP Modifications

This is a modified version for AmanMCP integration:

| Change | Original | AmanMCP |
|--------|----------|---------|
| Default port | 8000 | 9659 |
| Default model | 0.6B | 8B (better quality) |
| Models directory | HuggingFace default | `~/.amanmcp/models/mlx/` |

## Quick Start

### Prerequisites

- macOS with Apple Silicon (M1/M2/M3/M4)
- Python 3.9+
- ~5GB free disk space (for 8B model)

### Installation

```bash
cd mlx-server
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
```

### Start Server

```bash
python server.py
```

First run downloads the model (~4.5GB for 8B).

### Verify

```bash
curl http://localhost:9659/health
```

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/embed` | POST | Single text embedding |
| `/embed_batch` | POST | Batch text embeddings |
| `/health` | GET | Health check |
| `/models` | GET | List available models |
| `/docs` | GET | Interactive API docs |

### Example

```bash
# Single embedding
curl -X POST http://localhost:9659/embed \
  -H "Content-Type: application/json" \
  -d '{"text": "Hello world", "model": "large"}'

# Batch embedding
curl -X POST http://localhost:9659/embed_batch \
  -H "Content-Type: application/json" \
  -d '{"texts": ["Hello", "World"], "model": "large"}'
```

## Models

| Alias | Model | Dimensions | Size |
|-------|-------|------------|------|
| `small` | Qwen3-Embedding-0.6B-4bit | 1024 | ~400MB |
| `medium` | Qwen3-Embedding-4B-4bit | 2560 | ~2.5GB |
| `large` | Qwen3-Embedding-8B-4bit | 4096 | ~4.5GB |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | 9659 | Server port |
| `HOST` | 0.0.0.0 | Bind address |
| `MODEL_NAME` | `mlx-community/Qwen3-Embedding-8B-4bit-DWQ` | Default model |
| `AMANMCP_MLX_MODELS_DIR` | `~/.amanmcp/models/mlx/` | Models storage |
| `LOG_LEVEL` | INFO | Logging level (DEBUG, INFO, WARNING, ERROR) |
| `AMANMCP_LOG_DIR` | `~/.amanmcp/logs/` | Log files directory |

## Logging

The server logs to **both** terminal and file:

| Output | Format | Location |
|--------|--------|----------|
| Terminal | Human-readable, colored | stdout |
| File | JSON (for `amanmcp-logs`) | `~/.amanmcp/logs/mlx-server.log` |

### View Logs

```bash
# View MLX server logs
amanmcp-logs --source mlx

# Follow logs in real-time
amanmcp-logs --source mlx -f

# View all logs (Go + MLX) merged by timestamp
amanmcp-logs --source all

# Filter by level
amanmcp-logs --source mlx --level error
```

### Log Rotation

- **Max file size:** 10 MB
- **Max files:** 5 (total 50 MB)
- **Pattern:** `mlx-server.log`, `mlx-server.log.1`, ..., `mlx-server.log.5`

### Health Check Filtering

Health check requests (`/health`) are **excluded from log files** to reduce noise. They still appear in terminal output for live debugging.

## Auto-Start (LaunchAgent)

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
        <string>/path/to/amanmcp/mlx-server/.venv/bin/python</string>
        <string>/path/to/amanmcp/mlx-server/server.py</string>
    </array>
    <key>WorkingDirectory</key>
    <string>/path/to/amanmcp/mlx-server</string>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/amanmcp-mlx-server.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/amanmcp-mlx-server.err</string>
</dict>
</plist>
```

Enable:
```bash
launchctl load ~/Library/LaunchAgents/com.amanmcp.mlx-server.plist
```

## Performance

| Model | Batch (32 texts) | Throughput |
|-------|------------------|------------|
| small (0.6B) | ~30ms | 44K tok/s |
| medium (4B) | ~43ms | 18K tok/s |
| large (8B) | ~60ms | 11K tok/s |

Compared to Ollama with same model: **~55x faster**
