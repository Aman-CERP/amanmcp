#!/usr/bin/env bash
# AmanMCP Universal Install Script
# Usage: curl -sSL https://raw.githubusercontent.com/amanmcp/amanmcp/main/scripts/install.sh | sh
#
# This script:
# 1. Detects your OS and architecture
# 2. Downloads the latest release from GitHub
# 3. Installs to ~/.local/bin (user-scope, no sudo required)

set -euo pipefail

# Configuration
GITHUB_REPO="amanmcp/amanmcp"
INSTALL_DIR="${HOME}/.local/bin"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Helper functions
info() { echo -e "${BLUE}→${NC} $1"; }
success() { echo -e "${GREEN}✓${NC} $1"; }
warn() { echo -e "${YELLOW}!${NC} $1"; }
error() { echo -e "${RED}✗${NC} $1"; exit 1; }

# Banner
echo ""
echo -e "${GREEN}╔═══════════════════════════════════════╗${NC}"
echo -e "${GREEN}║        AmanMCP Installer              ║${NC}"
echo -e "${GREEN}║   Local-first RAG MCP Server          ║${NC}"
echo -e "${GREEN}╚═══════════════════════════════════════╝${NC}"
echo ""

# Detect OS
detect_os() {
    local os
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$os" in
        darwin) echo "darwin" ;;
        linux) echo "linux" ;;
        mingw*|msys*|cygwin*) error "Windows is not yet supported. Please use WSL2." ;;
        *) error "Unsupported operating system: $os" ;;
    esac
}

# Detect architecture
detect_arch() {
    local arch
    arch=$(uname -m)
    case "$arch" in
        x86_64|amd64) echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *) error "Unsupported architecture: $arch" ;;
    esac
}

# Check for required commands
check_dependencies() {
    for cmd in curl tar; do
        if ! command -v "$cmd" &> /dev/null; then
            error "Required command not found: $cmd"
        fi
    done
}

# Get latest version from GitHub API
get_latest_version() {
    local version
    version=$(curl -sS "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" 2>/dev/null | grep '"tag_name"' | cut -d '"' -f4)
    if [[ -z "$version" ]]; then
        error "Failed to fetch latest version from GitHub. Check your internet connection."
    fi
    echo "$version"
}

# Download and extract release
download_release() {
    local version=$1
    local os=$2
    local arch=$3
    local tmp_dir

    # Create temp directory
    tmp_dir=$(mktemp -d)
    trap "rm -rf $tmp_dir" EXIT

    # Construct download URL
    # GoReleaser naming: amanmcp_0.1.37_darwin_arm64.tar.gz
    local version_no_v="${version#v}"
    local tarball="amanmcp_${version_no_v}_${os}_${arch}.tar.gz"
    local url="https://github.com/${GITHUB_REPO}/releases/download/${version}/${tarball}"

    info "Downloading $tarball..."
    if ! curl -sL "$url" -o "$tmp_dir/amanmcp.tar.gz"; then
        error "Failed to download release. URL: $url"
    fi

    info "Extracting..."
    if ! tar -xzf "$tmp_dir/amanmcp.tar.gz" -C "$tmp_dir"; then
        error "Failed to extract archive"
    fi

    # Create installation directory
    mkdir -p "$INSTALL_DIR"

    # Install binary - handle both flat and nested structures
    if [[ -f "$tmp_dir/amanmcp" ]]; then
        cp "$tmp_dir/amanmcp" "$INSTALL_DIR/"
        chmod +x "$INSTALL_DIR/amanmcp"
        success "Installed binary to $INSTALL_DIR/amanmcp"
    else
        # Try finding binary in extracted directory
        local binary
        binary=$(find "$tmp_dir" -name "amanmcp" -type f 2>/dev/null | head -1)
        if [[ -n "$binary" && -f "$binary" ]]; then
            cp "$binary" "$INSTALL_DIR/"
            chmod +x "$INSTALL_DIR/amanmcp"
            success "Installed binary to $INSTALL_DIR/amanmcp"
        else
            error "Binary not found in archive"
        fi
    fi

    # BUG-037 Fix: Clear extended attributes to prevent macOS Gatekeeper SIGKILL
    # macOS Ventura+ adds com.apple.provenance xattr to files created by sandboxed apps
    # This combined with ad-hoc signing can cause Gatekeeper to reject the binary
    if [[ "$(uname -s)" == "Darwin" ]]; then
        xattr -cr "$INSTALL_DIR/amanmcp" 2>/dev/null || true
    fi
}

# Check and configure PATH (auto-configure silently)
configure_path() {
    if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
        local shell_config=""
        local shell_name=""

        # Detect shell config file
        if [[ -n "${ZSH_VERSION:-}" ]] || [[ "$SHELL" == *"zsh"* ]]; then
            shell_config="$HOME/.zshrc"
            shell_name="zsh"
        elif [[ -n "${BASH_VERSION:-}" ]] || [[ "$SHELL" == *"bash"* ]]; then
            shell_config="$HOME/.bashrc"
            shell_name="bash"
        fi

        if [[ -n "$shell_config" ]]; then
            # Check if already in shell config
            if ! grep -q 'export PATH="\$HOME/.local/bin:\$PATH"' "$shell_config" 2>/dev/null; then
                echo '' >> "$shell_config"
                echo '# Added by AmanMCP installer' >> "$shell_config"
                echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$shell_config"
                success "PATH updated in $shell_config"
                echo ""
                echo -e "  Run: ${BLUE}source $shell_config${NC} (or restart your terminal)"
                echo ""
            else
                success "PATH already configured in $shell_config"
            fi
        else
            warn "Could not detect shell config file"
            echo ""
            echo "Add this to your shell profile manually:"
            echo -e "  ${BLUE}export PATH=\"\$HOME/.local/bin:\$PATH\"${NC}"
            echo ""
        fi
    else
        success "PATH already includes ~/.local/bin"
    fi
}

# Verify installation
verify_install() {
    if "$INSTALL_DIR/amanmcp" version &>/dev/null; then
        success "Installation verified!"
        echo ""
        "$INSTALL_DIR/amanmcp" version
        return 0
    else
        warn "Installation complete but verification failed"
        echo "You may need to configure your PATH and library path first."
        return 1
    fi
}

# Upgrade user configuration if it exists
upgrade_config() {
    local user_config="${XDG_CONFIG_HOME:-$HOME/.config}/amanmcp/config.yaml"

    if [ -f "$user_config" ]; then
        info "Upgrading user configuration..."
        if "$INSTALL_DIR/amanmcp" config init --force; then
            success "Configuration upgraded (backup created)"
        else
            warn "Configuration upgrade failed, but installation succeeded"
            echo "  Run 'amanmcp config init --force' manually to upgrade"
        fi
    fi
}

# Print quick start guide
print_quickstart() {
    echo ""
    echo -e "${GREEN}╔═══════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║           Quick Start                 ║${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════╝${NC}"
    echo ""
    echo "  Initialize AmanMCP in your project:"
    echo ""
    echo -e "     ${BLUE}cd your-project${NC}"
    echo -e "     ${BLUE}amanmcp init${NC}"
    echo ""
    echo "  This will:"
    echo "    • Configure Claude Code MCP integration"
    echo "    • Index your project with a progress bar"
    echo "    • Verify embedder availability"
    echo ""
    echo "  Then restart Claude Code to activate!"
    echo ""
    echo "Documentation: https://github.com/${GITHUB_REPO}"
    echo ""
}

# Main installation flow
main() {
    info "Checking dependencies..."
    check_dependencies

    info "Detecting platform..."
    local os arch
    os=$(detect_os)
    arch=$(detect_arch)
    success "Detected: $os/$arch"

    info "Fetching latest version..."
    local version
    version=$(get_latest_version)
    success "Latest version: $version"

    download_release "$version" "$os" "$arch"

    echo ""
    configure_path

    verify_install || true
    upgrade_config
    print_quickstart
}

# Run main
main
