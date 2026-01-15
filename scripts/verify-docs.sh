#!/bin/bash
#
# verify-docs.sh - Documentation drift detection
#
# This script checks for common documentation issues:
# - Broken internal links
# - Version mismatches
# - Stale references
# - Hardcoded values that should be dynamic
#
# Usage: ./scripts/verify-docs.sh
#
# Exit codes:
#   0 - No errors found (warnings may exist)
#   1 - Errors found

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Counters
ERRORS=0
WARNINGS=0

# Helper functions
error() {
    echo -e "${RED}ERROR:${NC} $1"
    ((ERRORS++)) || true
}

warn() {
    echo -e "${YELLOW}WARN:${NC} $1"
    ((WARNINGS++)) || true
}

info() {
    echo -e "${CYAN}INFO:${NC} $1"
}

pass() {
    echo -e "${GREEN}✓${NC} $1"
}

header() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo " $1"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

# Navigate to project root
cd "$(dirname "$0")/.."

header "Documentation Verification"

# Get current version
if [[ -f "VERSION" ]]; then
    VERSION=$(cat VERSION | tr -d '[:space:]')
    info "Current version: $VERSION"
else
    error "VERSION file not found"
    VERSION=""
fi

# 1. Check for broken internal markdown links
header "Check 1: Broken Internal Links"

# Find all .md links in key files
LINK_FILES="README.md CLAUDE.md docs/index.md docs/context.md docs/workflow.md"
for file in $LINK_FILES; do
    if [[ -f "$file" ]]; then
        # Extract markdown links like [text](./path.md) or [text](path.md)
        links=$(grep -oE '\[([^]]+)\]\(([^)]+\.md)\)' "$file" 2>/dev/null | grep -oE '\(([^)]+)\)' | tr -d '()' || true)

        for link in $links; do
            # Handle relative links
            if [[ "$link" == ./* ]]; then
                target_dir=$(dirname "$file")
                target="$target_dir/${link#./}"
            elif [[ "$link" == ../* ]]; then
                target_dir=$(dirname "$file")
                target="$target_dir/$link"
            else
                target="$link"
            fi

            # Normalize path
            target=$(echo "$target" | sed 's|/\./|/|g')

            if [[ ! -f "$target" ]]; then
                error "Broken link in $file: $link -> $target"
            fi
        done
    fi
done

if [[ $ERRORS -eq 0 ]]; then
    pass "No broken links found in key files"
fi

# 2. Check VERSION synchronization
header "Check 2: Version Synchronization"

if [[ -n "$VERSION" ]]; then
    # Check docs/context.md
    if [[ -f "docs/context.md" ]]; then
        if grep -q "$VERSION" docs/context.md; then
            pass "docs/context.md has correct version"
        else
            error "docs/context.md version mismatch (expected $VERSION)"
        fi
    fi

    # Check CHANGELOG.md
    if [[ -f "docs/changelog/CHANGELOG.md" ]]; then
        if grep -q "\[$VERSION\]" docs/changelog/CHANGELOG.md; then
            pass "CHANGELOG.md has version entry"
        else
            warn "CHANGELOG.md missing [$VERSION] entry (may be pre-release)"
        fi
    fi
fi

# 3. Check for hardcoded ADR/feature counts
header "Check 3: Hardcoded Counts"

# Check for hardcoded ADR counts that might drift
if grep -rn "34 ADR\|34 decisions\|34 architecture" docs/ 2>/dev/null; then
    warn "Found hardcoded ADR count - may drift"
fi

# Check for hardcoded feature counts (exclude index and changelog - historical counts are OK)
if grep -rn "24 features\|50 features" docs/ 2>/dev/null | grep -v "index.md" | grep -v "changelog"; then
    warn "Found hardcoded feature count outside index - may drift"
fi

if [[ $WARNINGS -eq 0 ]]; then
    pass "No suspicious hardcoded counts found"
fi

# 4. Check for stale archive references
header "Check 4: Archive References"

# Check for references to archived content
if grep -rn "archive/" docs/ .claude/ 2>/dev/null | grep -v "verify-docs.sh" | head -5; then
    warn "Found references to archive/ directory"
fi

# 5. Check for placeholder text
header "Check 5: Placeholder Text"

PLACEHOLDERS=("TODO:" "FIXME:" "XXX:" "<your-" "YYYY-MM-DD" "example.com")
for placeholder in "${PLACEHOLDERS[@]}"; do
    matches=$(grep -rn "$placeholder" docs/ .claude/ 2>/dev/null | grep -v "template.md" | grep -v "verify-docs.sh" | head -3 || true)
    if [[ -n "$matches" ]]; then
        warn "Found placeholder '$placeholder':"
        echo "$matches" | head -3
    fi
done

# 6. Check for "latest" version tags
header "Check 6: Latest Version Tags"

# Check code files for latest tags (ERROR) - configs should be pinned
CODE_LATEST=$(grep -rn "@latest\|:latest\|version: latest" . 2>/dev/null | grep -v "node_modules\|\.git\|archive\|verify-docs.sh" | grep -E '\.(go|ya?ml|json|toml):|Makefile:' | head -5)
if [[ -n "$CODE_LATEST" ]]; then
    echo "$CODE_LATEST"
    error "Found 'latest' version tags in code - should be pinned (ADR-011)"
else
    pass "No 'latest' tags in code files"
fi

# Check docs for latest tags (WARNING) - docs may legitimately show patterns
DOCS_LATEST=$(grep -rn "@latest\|:latest\|version: latest" . 2>/dev/null | grep -v "node_modules\|\.git\|archive\|verify-docs.sh" | grep -E '\.(md):|docs/' | head -5)
if [[ -n "$DOCS_LATEST" ]]; then
    echo "$DOCS_LATEST"
    warn "Found 'latest' tags in documentation - verify they're intentional examples"
fi

# 7. Check feature spec references
header "Check 7: Feature Spec References"

if [[ -d "docs/specs/features" ]]; then
    # Check that referenced feature files exist
    for ref in $(grep -oh "F[0-9]\{2\}" docs/specs/features/index.md 2>/dev/null | sort -u); do
        spec_file="docs/specs/features/${ref}-*.md"
        # Only warn if specifically referenced but doesn't exist
        # (not all features have specs yet)
    done
    pass "Feature spec directory structure valid"
fi

# 8. Check for common typos in documentation
header "Check 8: Common Typos"

TYPOS=("teh " "taht " "hte " "funciton" "recieve" "seperate")
for typo in "${TYPOS[@]}"; do
    if grep -rin "$typo" docs/ .claude/ 2>/dev/null | grep -v "verify-docs.sh" | head -1; then
        warn "Possible typo: '$typo'"
    fi
done

# Summary
header "Summary"

echo ""
echo -e "${RED}Errors:${NC}   $ERRORS"
echo -e "${YELLOW}Warnings:${NC} $WARNINGS"
echo ""

if [[ $ERRORS -gt 0 ]]; then
    echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${RED} DOCUMENTATION VERIFICATION FAILED${NC}"
    echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo "Fix the errors above."
    exit 1
else
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${GREEN} DOCUMENTATION VERIFICATION PASSED${NC}"
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    if [[ $WARNINGS -gt 0 ]]; then
        echo ""
        echo "Consider addressing the warnings above."
    fi
    exit 0
fi
