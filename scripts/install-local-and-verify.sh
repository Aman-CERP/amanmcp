#!/usr/bin/env bash
# Rebuild, install, and verify the local AmanMCP binary version.

set -euo pipefail

MODE="strict"
BINARY_PATH="${AMANMCP_INSTALL_BINARY_PATH:-$HOME/.local/bin/amanmcp}"
VERSION_FILE="VERSION"
SKIP_INSTALL=false

usage() {
    cat <<'EOF'
Usage: ./scripts/install-local-and-verify.sh [OPTIONS]

Options:
  --mode advisory|strict   Advisory logs failures and exits 0; strict fails.
  --binary-path PATH       Installed binary path (default: ~/.local/bin/amanmcp)
  --version-file PATH      VERSION file path (default: VERSION)
  --skip-install           Verify the binary path without rebuilding/installing
  -h, --help               Show this help
EOF
}

fail_usage() {
    echo "install-local-and-verify: $1" >&2
    exit 2
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --mode)
            MODE="${2:-}"
            shift 2
            ;;
        --binary-path)
            BINARY_PATH="${2:-}"
            shift 2
            ;;
        --version-file)
            VERSION_FILE="${2:-}"
            shift 2
            ;;
        --skip-install)
            SKIP_INSTALL=true
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            fail_usage "unknown option: $1"
            ;;
    esac
done

if [[ "$MODE" != "advisory" && "$MODE" != "strict" ]]; then
    fail_usage "--mode must be advisory or strict"
fi

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || fail_usage "not inside a git repository"
cd "$repo_root"

finish() {
    local status="$1"
    local message="$2"

    echo "install-local-and-verify:"
    echo "  mode: ${MODE}"
    echo "  repo_version: ${REPO_VERSION:-unknown}"
    echo "  installed_binary: ${BINARY_PATH}"
    echo "  installed_version: ${INSTALLED_VERSION:-unknown}"
    echo "  final_status: ${status}"
    echo "  message: ${message}"

    if [[ "$status" == "pass" ]]; then
        exit 0
    fi

    echo "  remediation: make build && make install-local"

    if [[ "$MODE" == "advisory" ]]; then
        echo "  final_exit_status: 0 (advisory)"
        exit 0
    fi

    echo "  final_exit_status: 1 (strict)"
    exit 1
}

install_binary() {
    local install_dir

    install_dir="$(dirname "$BINARY_PATH")"

    echo "Building amanmcp from current checkout..."
    make build

    echo "Installing amanmcp to ${BINARY_PATH}..."
    mkdir -p "$install_dir"
    cp bin/amanmcp "$BINARY_PATH"
    chmod +x "$BINARY_PATH"

    if [[ "$(uname -s)" == "Darwin" ]]; then
        xattr -cr "$BINARY_PATH" 2>/dev/null || true
        codesign --force --deep --sign - "$BINARY_PATH" 2>/dev/null || true
    fi
}

read_installed_version() {
    local output

    if output="$("$BINARY_PATH" version --short 2>/dev/null)"; then
        printf '%s\n' "$output" | head -n 1
        return
    fi

    output="$("$BINARY_PATH" version 2>/dev/null | head -n 1 || true)"
    printf '%s\n' "$output" | awk '{print $2}'
}

if [[ ! -f "$VERSION_FILE" ]]; then
    finish "fail" "VERSION file not found: ${VERSION_FILE}"
fi

REPO_VERSION="$(tr -d '[:space:]' < "$VERSION_FILE")"
if [[ -z "$REPO_VERSION" ]]; then
    finish "fail" "VERSION file is empty: ${VERSION_FILE}"
fi

if [[ "$SKIP_INSTALL" == "false" ]]; then
    if ! install_binary; then
        finish "fail" "build or install failed"
    fi
fi

if [[ ! -x "$BINARY_PATH" ]]; then
    finish "fail" "installed binary is missing or not executable"
fi

INSTALLED_VERSION="$(read_installed_version)"
INSTALLED_VERSION="$(printf '%s' "$INSTALLED_VERSION" | tr -d '[:space:]')"

if [[ -z "$INSTALLED_VERSION" ]]; then
    finish "fail" "could not read installed binary version"
fi

if [[ "$INSTALLED_VERSION" != "$REPO_VERSION" ]]; then
    finish "fail" "installed binary version does not match VERSION"
fi

finish "pass" "installed binary version matches VERSION"
