#!/bin/bash
# AmanMCP Backend Switcher
# Switches between MLX and Ollama backends for embedding generation.
#
# Usage:
#   ./scripts/switch-backend.sh mlx     # Stop Ollama, start MLX server
#   ./scripts/switch-backend.sh ollama  # Stop MLX server, start Ollama
#   ./scripts/switch-backend.sh status  # Show current backend status
#
# Note: Only one backend can run at a time due to 24GB RAM constraint.
# Memory usage: MLX ~3GB, Ollama ~3-6GB depending on model

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
MLX_SERVER_DIR="$PROJECT_ROOT/mlx-server"
MLX_PID_FILE="/tmp/amanmcp-mlx-server.pid"
MLX_LOG_FILE="/tmp/amanmcp-mlx-server.log"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if MLX server is running
is_mlx_running() {
    if [ -f "$MLX_PID_FILE" ]; then
        local pid=$(cat "$MLX_PID_FILE")
        if ps -p "$pid" > /dev/null 2>&1; then
            return 0
        fi
    fi
    # Also check by process name
    pgrep -f "python.*server.py" > /dev/null 2>&1
}

# Check if Ollama is running
is_ollama_running() {
    pgrep -x ollama > /dev/null 2>&1 || \
    curl -s http://localhost:11434/api/tags > /dev/null 2>&1
}

# Stop MLX server
stop_mlx() {
    if is_mlx_running; then
        log_info "Stopping MLX server..."
        if [ -f "$MLX_PID_FILE" ]; then
            kill $(cat "$MLX_PID_FILE") 2>/dev/null || true
            rm -f "$MLX_PID_FILE"
        fi
        # Kill any remaining python server.py processes
        pkill -f "python.*server.py" 2>/dev/null || true
        sleep 2
        log_info "MLX server stopped"
    else
        log_info "MLX server not running"
    fi
}

# Stop Ollama
stop_ollama() {
    if is_ollama_running; then
        log_info "Stopping Ollama..."
        # On macOS, Ollama runs as a service
        if [ "$(uname)" = "Darwin" ]; then
            # Try to quit via AppleScript first (graceful)
            osascript -e 'quit app "Ollama"' 2>/dev/null || true
            sleep 2
            # If still running, kill the process
            if is_ollama_running; then
                pkill -x ollama 2>/dev/null || true
            fi
        else
            # Linux: stop service or kill process
            sudo systemctl stop ollama 2>/dev/null || \
            pkill -x ollama 2>/dev/null || true
        fi
        sleep 2
        log_info "Ollama stopped"
    else
        log_info "Ollama not running"
    fi
}

# Start MLX server
start_mlx() {
    if is_mlx_running; then
        log_warn "MLX server already running"
        return 0
    fi

    if ! [ -d "$MLX_SERVER_DIR" ]; then
        log_error "MLX server directory not found: $MLX_SERVER_DIR"
        exit 1
    fi

    log_info "Starting MLX server..."
    cd "$MLX_SERVER_DIR"

    # Activate venv if exists
    if [ -f ".venv/bin/activate" ]; then
        source .venv/bin/activate
    fi

    # Start server in background
    nohup python server.py > "$MLX_LOG_FILE" 2>&1 &
    echo $! > "$MLX_PID_FILE"

    # Wait for server to be ready
    for i in {1..30}; do
        if curl -s http://localhost:9659/health > /dev/null 2>&1; then
            log_info "MLX server started (PID: $(cat $MLX_PID_FILE))"
            log_info "Log file: $MLX_LOG_FILE"
            return 0
        fi
        sleep 1
    done

    log_error "MLX server failed to start. Check $MLX_LOG_FILE"
    exit 1
}

# Start Ollama
start_ollama() {
    if is_ollama_running; then
        log_warn "Ollama already running"
        return 0
    fi

    log_info "Starting Ollama..."
    if [ "$(uname)" = "Darwin" ]; then
        # On macOS, open the Ollama app
        open -a Ollama 2>/dev/null || {
            log_error "Ollama not installed. Please install from https://ollama.ai"
            exit 1
        }
    else
        # Linux: start service
        sudo systemctl start ollama 2>/dev/null || \
        ollama serve &
    fi

    # Wait for Ollama to be ready
    for i in {1..30}; do
        if curl -s http://localhost:11434/api/tags > /dev/null 2>&1; then
            log_info "Ollama started and ready"
            return 0
        fi
        sleep 1
    done

    log_error "Ollama failed to start within 30 seconds"
    exit 1
}

# Show status
show_status() {
    echo "=== AmanMCP Backend Status ==="
    echo

    # MLX Status
    if is_mlx_running; then
        local mlx_pid=""
        if [ -f "$MLX_PID_FILE" ]; then
            mlx_pid="(PID: $(cat $MLX_PID_FILE))"
        fi
        echo -e "MLX Server:  ${GREEN}RUNNING${NC} $mlx_pid"
        echo "  Endpoint:  http://localhost:9659"
        echo "  Model:     Qwen3-Embedding-0.6B"
        echo "  Dims:      1024"
    else
        echo -e "MLX Server:  ${RED}STOPPED${NC}"
    fi
    echo

    # Ollama Status
    if is_ollama_running; then
        echo -e "Ollama:      ${GREEN}RUNNING${NC}"
        echo "  Endpoint:  http://localhost:11434"
        # List available embedding models
        local models=$(curl -s http://localhost:11434/api/tags 2>/dev/null | grep -o '"name":"[^"]*embedding[^"]*"' | head -3)
        if [ -n "$models" ]; then
            echo "  Models:    $(echo "$models" | sed 's/"name":"//g' | sed 's/"//g' | tr '\n' ', ')"
        fi
    else
        echo -e "Ollama:      ${RED}STOPPED${NC}"
    fi
    echo

    # Memory usage
    echo "=== Memory Usage ==="
    if [ "$(uname)" = "Darwin" ]; then
        vm_stat | head -5
    else
        free -h | head -2
    fi
}

# Switch to MLX
switch_to_mlx() {
    log_info "Switching to MLX backend..."
    stop_ollama
    start_mlx

    echo
    echo "=== Switch Complete ==="
    echo "Backend: MLX (Qwen3-Embedding-0.6B, 1024 dims)"
    echo
    echo "To use with amanmcp:"
    echo "  export AMANMCP_EMBEDDER=mlx"
    echo "  amanmcp index --force ."
}

# Switch to Ollama
switch_to_ollama() {
    log_info "Switching to Ollama backend..."
    stop_mlx
    start_ollama

    echo
    echo "=== Switch Complete ==="
    echo "Backend: Ollama"
    echo
    echo "Available models:"
    echo "  - qwen3-embedding:0.6b (1024 dims, MTEB-Code: 74.57)"
    echo "  - embeddinggemma (768 dims, MTEB-Code: 68.14, MRL support)"
    echo
    echo "To use with amanmcp:"
    echo "  export AMANMCP_EMBEDDER=ollama"
    echo "  export AMANMCP_OLLAMA_MODEL=qwen3-embedding:0.6b  # or embeddinggemma"
    echo "  amanmcp index --force ."
}

# Main
case "${1:-status}" in
    mlx)
        switch_to_mlx
        ;;
    ollama)
        switch_to_ollama
        ;;
    status)
        show_status
        ;;
    stop)
        stop_mlx
        stop_ollama
        log_info "All backends stopped"
        ;;
    *)
        echo "Usage: $0 {mlx|ollama|status|stop}"
        echo
        echo "Commands:"
        echo "  mlx     - Stop Ollama, start MLX server"
        echo "  ollama  - Stop MLX server, start Ollama"
        echo "  status  - Show current backend status"
        echo "  stop    - Stop all backends"
        echo
        echo "Note: Only one backend can run at a time (24GB RAM constraint)"
        exit 1
        ;;
esac
