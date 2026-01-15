#!/bin/bash
#
# verify-checkpoint.sh - Verify checkpoint completeness before commit
#
# This script validates that all required artifacts exist for a checkpoint.
# Run before committing a version bump.
#
# Usage: ./scripts/verify-checkpoint.sh
#
# Exit codes:
#   0 - All checks passed
#   1 - One or more checks failed

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Counters
PASSED=0
FAILED=0
WARNINGS=0

# Helper functions
pass() {
    echo -e "${GREEN}✓${NC} $1"
    ((PASSED++)) || true
}

fail() {
    echo -e "${RED}✗${NC} $1"
    ((FAILED++)) || true
}

warn() {
    echo -e "${YELLOW}!${NC} $1"
    ((WARNINGS++)) || true
}

header() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo " $1"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

# Navigate to project root
cd "$(dirname "$0")/.."

header "Checkpoint Verification"

# 1. Check VERSION file exists and has content
echo ""
echo "Checking VERSION file..."
if [[ -f "VERSION" ]]; then
    VERSION=$(cat VERSION | tr -d '[:space:]')
    if [[ -n "$VERSION" ]]; then
        pass "VERSION file exists: $VERSION"
    else
        fail "VERSION file is empty"
    fi
else
    fail "VERSION file not found"
    VERSION=""
fi

# 2. Check docs/context.md has matching version
echo ""
echo "Checking docs/context.md..."
if [[ -f "docs/context.md" ]]; then
    if [[ -n "$VERSION" ]]; then
        if grep -q "$VERSION" docs/context.md; then
            pass "docs/context.md contains version $VERSION"
        else
            fail "docs/context.md does not contain version $VERSION"
        fi
    else
        warn "Cannot verify context.md version (VERSION file missing)"
    fi
else
    fail "docs/context.md not found"
fi

# 3. Check changelog directory exists for this version
echo ""
echo "Checking changelog structure..."
if [[ -n "$VERSION" ]]; then
    MAJOR_MINOR=$(echo "$VERSION" | cut -d. -f1,2)
    CHANGELOG_DIR="docs/changelog/v${MAJOR_MINOR}"

    if [[ -d "$CHANGELOG_DIR" ]]; then
        pass "Changelog directory exists: $CHANGELOG_DIR"
    else
        fail "Changelog directory missing: $CHANGELOG_DIR"
    fi

    # 4. Check version-specific changelog file exists
    CHANGELOG_FILE="${CHANGELOG_DIR}/${VERSION}.md"
    if [[ -f "$CHANGELOG_FILE" ]]; then
        pass "Version changelog exists: $CHANGELOG_FILE"
    else
        fail "Version changelog missing: $CHANGELOG_FILE"
    fi
fi

# 5. Check CHANGELOG.md has version entry
echo ""
echo "Checking CHANGELOG.md..."
if [[ -f "docs/changelog/CHANGELOG.md" ]]; then
    if [[ -n "$VERSION" ]]; then
        if grep -q "\[$VERSION\]" docs/changelog/CHANGELOG.md; then
            pass "CHANGELOG.md contains [$VERSION] entry"
        else
            fail "CHANGELOG.md missing [$VERSION] entry"
        fi
    fi
else
    fail "docs/changelog/CHANGELOG.md not found"
fi

# 6. Check unreleased.md is reasonably empty (reset after release)
echo ""
echo "Checking unreleased.md..."
if [[ -f "docs/changelog/unreleased.md" ]]; then
    LINE_COUNT=$(wc -l < docs/changelog/unreleased.md | tr -d ' ')
    if [[ $LINE_COUNT -lt 30 ]]; then
        pass "unreleased.md appears reset ($LINE_COUNT lines)"
    else
        warn "unreleased.md has $LINE_COUNT lines (should be reset after release)"
    fi
else
    fail "docs/changelog/unreleased.md not found"
fi

# 7. Check git status is clean (optional but recommended)
echo ""
echo "Checking git status..."
if command -v git &> /dev/null; then
    UNCOMMITTED=$(git status --porcelain 2>/dev/null | wc -l | tr -d ' ')
    if [[ $UNCOMMITTED -eq 0 ]]; then
        pass "Git working directory is clean"
    else
        warn "Git has $UNCOMMITTED uncommitted changes"
    fi
else
    warn "Git not available, skipping status check"
fi

# 8. Check CI parity (optional)
echo ""
echo "Checking CI parity..."
if [[ -x "scripts/ci-parity-check.sh" ]]; then
    if ./scripts/ci-parity-check.sh --quick > /dev/null 2>&1; then
        pass "CI parity check passed"
    else
        warn "CI parity check failed (run 'make ci-check' for details)"
    fi
else
    warn "CI parity check script not found or not executable"
fi

# Summary
header "Summary"

echo ""
echo -e "${GREEN}Passed:${NC}   $PASSED"
echo -e "${YELLOW}Warnings:${NC} $WARNINGS"
echo -e "${RED}Failed:${NC}   $FAILED"
echo ""

if [[ $FAILED -gt 0 ]]; then
    echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${RED} CHECKPOINT VERIFICATION FAILED${NC}"
    echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo "Fix the failures above before committing."
    exit 1
else
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${GREEN} CHECKPOINT VERIFICATION PASSED${NC}"
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    if [[ $WARNINGS -gt 0 ]]; then
        echo ""
        echo "Consider addressing the warnings above."
    fi
    exit 0
fi
