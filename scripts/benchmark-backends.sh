#!/bin/bash
# Backend Performance Comparison Benchmark
# Compares MLX and Ollama embedding backends with detailed timing metrics.
#
# Usage:
#   ./scripts/benchmark-backends.sh [project_path] [runs_per_backend]
#
# Prerequisites:
#   - amanmcp binary installed (make install-local)
#   - MLX server directory at ../mlx-server (for MLX backend)
#   - Ollama installed (for Ollama backend)
#
# Output:
#   - Summary table comparing backends
#   - Detailed JSON logs in ~/.amanmcp/logs/server.log

set -e

RUNS="${2:-3}"
PROJECT_PATH="${1:-.}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SWITCH_BACKEND="$SCRIPT_DIR/switch-backend.sh"

echo "==================================================="
echo "  AmanMCP Backend Performance Benchmark"
echo "==================================================="
echo ""
echo "Project:    $PROJECT_PATH"
echo "Runs each:  $RUNS"
echo "Date:       $(date '+%Y-%m-%d %H:%M:%S')"
echo ""

# Resolve absolute path
PROJECT_PATH=$(cd "$PROJECT_PATH" && pwd)
LOG_FILE="$HOME/.amanmcp/logs/server.log"

# Check prerequisites
if ! command -v amanmcp &> /dev/null; then
    echo "ERROR: amanmcp not found. Run 'make install-local' first."
    exit 1
fi

if ! command -v jq &> /dev/null; then
    echo "WARNING: jq not found. Install it for better output parsing."
fi

if [ ! -x "$SWITCH_BACKEND" ]; then
    echo "ERROR: switch-backend.sh not found at $SWITCH_BACKEND"
    exit 1
fi

# Function to extract metrics from log file
extract_metrics() {
    local backend=$1
    local run=$2

    # Get the last index_complete entry
    if command -v jq &> /dev/null; then
        local entry=$(grep "index_complete" "$LOG_FILE" | tail -1)
        echo "$entry" | jq -r '"\(.duration_total) | \(.chunks_per_sec | . * 100 | floor / 100) c/s | embed: \(.duration_embed_ms)ms"' 2>/dev/null || echo "Parse error"
    else
        grep "index_complete" "$LOG_FILE" | tail -1 | grep -o '"duration_total":"[^"]*"' || echo "Parse error"
    fi
}

# Function to run a single indexing iteration
run_index() {
    local backend=$1
    local run=$2

    echo -n "  Run $run: "

    # Clean slate
    rm -rf "$PROJECT_PATH/.amanmcp"

    # Set backend
    export AMANMCP_EMBEDDER="$backend"

    # Run index (suppress output, capture errors)
    if amanmcp index "$PROJECT_PATH" --no-tui > /dev/null 2>&1; then
        extract_metrics "$backend" "$run"
    else
        echo "FAILED"
        return 1
    fi
}

# Initialize counters
mlx_success=0
ollama_success=0

echo "---------------------------------------------------"
echo "Switching to MLX Backend"
echo "---------------------------------------------------"
if "$SWITCH_BACKEND" mlx 2>&1; then
    echo ""
    echo "Testing MLX Backend ($RUNS runs)"
    echo "---------------------------------------------------"

    for i in $(seq 1 $RUNS); do
        if run_index mlx $i; then
            mlx_success=$((mlx_success + 1))
        fi
    done
else
    echo "WARN: MLX server unavailable, skipping MLX tests"
fi

echo ""
echo "---------------------------------------------------"
echo "Switching to Ollama Backend"
echo "---------------------------------------------------"
if "$SWITCH_BACKEND" ollama 2>&1; then
    echo ""
    echo "Testing Ollama Backend ($RUNS runs)"
    echo "---------------------------------------------------"

    for i in $(seq 1 $RUNS); do
        if run_index ollama $i; then
            ollama_success=$((ollama_success + 1))
        fi
    done
else
    echo "ERROR: Ollama unavailable"
fi

echo ""
echo "---------------------------------------------------"
echo "Summary"
echo "---------------------------------------------------"
echo ""
echo "MLX:    $mlx_success/$RUNS successful runs"
echo "Ollama: $ollama_success/$RUNS successful runs"
echo ""

# Show recent index_complete entries from log
if command -v jq &> /dev/null; then
    echo "Recent Results (from log):"
    echo ""
    echo "| Backend | Duration | Chunks/sec | Embed Time |"
    echo "|---------|----------|------------|------------|"

    grep "index_complete" "$LOG_FILE" | tail -$((RUNS * 2)) | while read line; do
        backend=$(echo "$line" | jq -r '.embedder_backend // "unknown"' 2>/dev/null)
        duration=$(echo "$line" | jq -r '.duration_total // "?"' 2>/dev/null)
        cps=$(echo "$line" | jq -r '.chunks_per_sec // 0 | . * 10 | floor / 10' 2>/dev/null)
        embed_ms=$(echo "$line" | jq -r '.duration_embed_ms // 0' 2>/dev/null)
        embed_s=$(echo "scale=1; $embed_ms / 1000" | bc 2>/dev/null || echo "?")
        echo "| $backend | $duration | $cps | ${embed_s}s |"
    done
fi

echo ""
echo "Log file: $LOG_FILE"
echo ""
echo "To see detailed metrics:"
echo "  cat $LOG_FILE | grep index_complete | tail -5 | jq ."
