#!/usr/bin/env bash
#
# Version Consistency Check Script for AmanMCP
#
# Purpose: Ensures tool versions match across Makefile, CI, and scripts.
# Implements ADR-011: Version Pinning Strategy
#
# Usage:
#   ./scripts/check-version-consistency.sh
#
# Exit Codes:
#   0 - All versions consistent
#   1 - Version mismatch detected

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

ERRORS=0

log_check() {
    echo -e "${YELLOW}▶ Checking: $1${NC}"
}

log_success() {
    echo -e "${GREEN}  ✓ $1${NC}"
}

log_error() {
    echo -e "${RED}  ✗ $1${NC}"
    ERRORS=$((ERRORS + 1))
}

log_info() {
    echo -e "    $1"
}

# Get version from Makefile (source of truth)
get_makefile_version() {
    local var_name="$1"
    grep "^${var_name}" Makefile 2>/dev/null | head -1 | sed 's/.*= *//' | tr -d ' '
}

# Check for "latest" tags
check_no_latest() {
    log_check "No 'latest' version tags"

    local files_to_check=(
        "Makefile"
        "scripts/ci-parity-check.sh"
        ".github/workflows/*.yml"
    )

    local found_latest=false

    for pattern in "${files_to_check[@]}"; do
        # shellcheck disable=SC2086
        for file in $pattern; do
            if [[ -f "$file" ]]; then
                # Check for @latest (but allow govulncheck@latest as exception)
                if grep -n '@latest' "$file" 2>/dev/null | grep -v 'govulncheck@latest' | grep -v '^#'; then
                    log_error "Found '@latest' in $file (violates ADR-011)"
                    found_latest=true
                fi
            fi
        done
    done

    if [[ "$found_latest" == "false" ]]; then
        log_success "No prohibited 'latest' tags found"
    fi
}

# Check Go version consistency
check_go_version() {
    log_check "Go version consistency"

    local makefile_go=$(get_makefile_version "GO_VERSION")

    if [[ -z "$makefile_go" ]]; then
        log_info "GO_VERSION not found in Makefile (will be added)"
        return
    fi

    log_info "Makefile: $makefile_go"

    # Check go.mod
    if [[ -f "go.mod" ]]; then
        local gomod_version=$(grep "^go " go.mod | awk '{print $2}')
        log_info "go.mod: $gomod_version"

        # go.mod might have shorter version (1.25 vs 1.25.5)
        if [[ ! "$makefile_go" =~ ^"$gomod_version" ]]; then
            log_error "go.mod version ($gomod_version) doesn't match Makefile ($makefile_go)"
        fi
    fi

    # Check CI workflow
    if [[ -f ".github/workflows/ci.yml" ]]; then
        local ci_go=$(grep -E "go-version:" .github/workflows/ci.yml | head -1 | sed 's/.*: *//' | tr -d "'" | tr -d '"')
        log_info "CI workflow: $ci_go"

        if [[ "$ci_go" != "$makefile_go" ]]; then
            log_error "CI workflow Go version ($ci_go) doesn't match Makefile ($makefile_go)"
        fi
    fi

    if [[ $ERRORS -eq 0 ]]; then
        log_success "Go versions are consistent"
    fi
}

# Check golangci-lint version
check_lint_version() {
    log_check "golangci-lint version consistency"

    local makefile_lint=$(get_makefile_version "GOLANGCI_LINT_VERSION")

    if [[ -z "$makefile_lint" ]]; then
        log_info "GOLANGCI_LINT_VERSION not found in Makefile (will be added)"
        return
    fi

    log_info "Makefile: $makefile_lint"

    # Check CI workflow
    if [[ -f ".github/workflows/ci.yml" ]]; then
        local ci_lint=$(grep -E "golangci-lint.*version:" .github/workflows/ci.yml | head -1 | sed 's/.*version: *//' | tr -d "'" | tr -d '"')
        if [[ -n "$ci_lint" ]]; then
            log_info "CI workflow: $ci_lint"

            if [[ "$ci_lint" != "$makefile_lint" ]]; then
                log_error "CI golangci-lint version ($ci_lint) doesn't match Makefile ($makefile_lint)"
            fi
        fi
    fi

    # Check ci-parity-check.sh
    if [[ -f "scripts/ci-parity-check.sh" ]]; then
        local script_lint=$(grep "GOLANGCI_LINT_VERSION=" scripts/ci-parity-check.sh | head -1 | sed 's/.*="//' | tr -d '"')
        if [[ -n "$script_lint" ]]; then
            log_info "ci-parity-check.sh: $script_lint"

            if [[ "$script_lint" != "$makefile_lint" ]]; then
                log_error "ci-parity-check.sh lint version ($script_lint) doesn't match Makefile ($makefile_lint)"
            fi
        fi
    fi

    if [[ $ERRORS -eq 0 ]]; then
        log_success "golangci-lint versions are consistent"
    fi
}

# Check product version consistency (VERSION file vs README badge)
check_product_version() {
    log_check "Product version consistency"

    if [[ ! -f "VERSION" ]]; then
        log_error "VERSION file not found"
        return
    fi

    local version_file=$(cat VERSION | tr -d '[:space:]')
    log_info "VERSION file: $version_file"

    # Check README.md badge
    if [[ -f "README.md" ]]; then
        local readme_version=$(grep -oE 'version-[0-9]+\.[0-9]+\.[0-9]+' README.md | head -1 | sed 's/version-//')
        if [[ -n "$readme_version" ]]; then
            log_info "README badge: $readme_version"

            if [[ "$readme_version" != "$version_file" ]]; then
                log_error "README badge ($readme_version) doesn't match VERSION file ($version_file)"
                log_info "Fix: Update README.md badge or run release script"
            fi
        else
            log_info "README badge: not found (optional)"
        fi
    fi

    # Check if changelog exists for this version
    local changelog_file="docs/changelog/0.${version_file#0.}/v${version_file}.md"
    # Handle version like 0.2.6 -> docs/changelog/0.2/v0.2.6.md
    local major_minor=$(echo "$version_file" | grep -oE '^[0-9]+\.[0-9]+')
    changelog_file="docs/changelog/${major_minor}/v${version_file}.md"

    if [[ -f "$changelog_file" ]]; then
        log_info "Changelog: $changelog_file exists"
    else
        log_info "Changelog: $changelog_file not found (created during release)"
    fi

    if [[ $ERRORS -eq 0 ]]; then
        log_success "Product version is consistent"
    fi
}

# Check for Go 1.25+ compatibility with golangci-lint v2.x
check_go_lint_compatibility() {
    log_check "Go and golangci-lint compatibility"

    local makefile_go=$(get_makefile_version "GO_VERSION")
    local makefile_lint=$(get_makefile_version "GOLANGCI_LINT_VERSION")

    if [[ -z "$makefile_go" ]] || [[ -z "$makefile_lint" ]]; then
        log_info "Skipping compatibility check (versions not defined yet)"
        return
    fi

    # Extract major.minor from Go version
    local go_major_minor=$(echo "$makefile_go" | grep -oE '^[0-9]+\.[0-9]+')

    # Check if Go 1.25+ and lint v2.x
    if [[ "$(printf '%s\n' "1.25" "$go_major_minor" | sort -V | head -n1)" == "1.25" ]]; then
        # Go 1.25+
        if [[ "$makefile_lint" =~ ^v1\. ]]; then
            log_error "Go $makefile_go requires golangci-lint v2.x, but found $makefile_lint"
            log_info "See: https://github.com/golangci/golangci-lint/issues/5257"
        else
            log_success "Go $makefile_go is compatible with golangci-lint $makefile_lint"
        fi
    else
        log_success "Go $makefile_go compatibility check passed"
    fi
}

# Main execution
main() {
    echo ""
    echo "========================================"
    echo "  Version Consistency Check"
    echo "  (ADR-011: Version Pinning Strategy)"
    echo "========================================"
    echo ""

    check_no_latest
    echo ""
    check_go_version
    echo ""
    check_lint_version
    echo ""
    check_go_lint_compatibility
    echo ""
    check_product_version
    echo ""

    if [[ $ERRORS -gt 0 ]]; then
        echo "========================================"
        echo -e "${RED}✗ FAILED: $ERRORS version inconsistencies found${NC}"
        echo "========================================"
        echo ""
        echo "Fix:"
        echo "  1. Update Makefile with correct versions (source of truth)"
        echo "  2. Update other files to match Makefile"
        echo "  3. Re-run this script"
        echo ""
        exit 1
    else
        echo "========================================"
        echo -e "${GREEN}✓ All versions are consistent${NC}"
        echo "========================================"
        echo ""
        exit 0
    fi
}

main "$@"
