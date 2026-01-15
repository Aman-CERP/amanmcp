#!/usr/bin/env bash
# AmanMCP Uninstall Script
# Usage: curl -sSL https://raw.githubusercontent.com/amanmcp/amanmcp/main/scripts/uninstall.sh | sh
#
# This script removes:
# - The amanmcp binary from ~/.local/bin/
#
# User data is preserved:
# - ~/.amanmcp/ (models, sessions)
# - <projects>/.amanmcp/ (project indexes)

set -euo pipefail

# Configuration
INSTALL_DIR="${HOME}/.local/bin"
DATA_DIR="${HOME}/.amanmcp"

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

echo ""
echo -e "${RED}╔═══════════════════════════════════════╗${NC}"
echo -e "${RED}║      AmanMCP Uninstaller              ║${NC}"
echo -e "${RED}╚═══════════════════════════════════════╝${NC}"
echo ""

# Remove binary
if [[ -f "$INSTALL_DIR/amanmcp" ]]; then
    info "Removing binary..."
    rm -f "$INSTALL_DIR/amanmcp"
    success "Removed $INSTALL_DIR/amanmcp"
else
    warn "Binary not found at $INSTALL_DIR/amanmcp"
fi

echo ""
success "AmanMCP has been uninstalled"
echo ""

# Note about user data
if [[ -d "$DATA_DIR" ]]; then
    warn "User data preserved at $DATA_DIR"
    echo ""
    echo "This directory contains:"
    echo "  - models/    (~500MB embedding models)"
    echo "  - sessions/  (named session data)"
    echo ""
    echo "To remove all user data:"
    echo -e "  ${BLUE}rm -rf ~/.amanmcp${NC}"
    echo ""
fi

echo "Note: Project indexes (.amanmcp/ in each project) are also preserved."
echo "Remove them manually from individual projects if desired."
echo ""
