#!/bin/bash
#
# verify-ssot-consistency.sh - Verify SSOT (Single Source of Truth) consistency
#
# This script validates that all SSOT files agree on project state.
# It detects drift between feature index, maintenance index, context.md,
# and validation guides.
#
# Usage: ./scripts/verify-ssot-consistency.sh [--quiet]
#
# Exit codes:
#   0 - All SSOT files are consistent
#   1 - Inconsistency detected (prints details and fix suggestions)
#
# Integration:
#   - Runs as part of 'make ci-check'
#   - Can run at session start to detect corruption
#   - Can run in CI to catch drift before merge
#
# Philosophy:
#   Documentation explains *why*. Automation enforces *what*.
#   This script enforces SSOT consistency without requiring humans to verify manually.

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Options
QUIET=false
if [[ "$1" == "--quiet" ]]; then
    QUIET=true
fi

# Counters
PASSED=0
FAILED=0

# Helper functions
pass() {
    ((PASSED++)) || true
    if [[ "$QUIET" == "false" ]]; then
        echo -e "${GREEN}✓${NC} $1"
    fi
}

fail() {
    ((FAILED++)) || true
    echo -e "${RED}✗${NC} $1"
}

info() {
    if [[ "$QUIET" == "false" ]]; then
        echo -e "${BLUE}ℹ${NC} $1"
    fi
}

header() {
    if [[ "$QUIET" == "false" ]]; then
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo " $1"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    fi
}

# Navigate to project root
cd "$(dirname "$0")/.."

header "SSOT Consistency Verification"

# ============================================================================
# Check 1: Feature index internal consistency
# ============================================================================
info "Checking feature count consistency..."

# Count feature rows with "Validated" status (both bold and plain)
# Exclude legend row by requiring feature ID (F##) in the row
FEATURE_VALIDATED=$(grep -E '^\| \*?\*?F[0-9]' docs/specs/features/index.md 2>/dev/null | \
                    grep -c -E '\| \*?\*?Validated\*?\*? \|' || echo 0)

# Get "**Completed:** 31" value from header
INDEX_COMPLETED=$(grep '^\*\*Completed:\*\*' docs/specs/features/index.md 2>/dev/null | \
                  sed 's/.*\*\*Completed:\*\* //' | tr -d '[:space:]' || echo 0)

if [[ "$FEATURE_VALIDATED" -eq "$INDEX_COMPLETED" ]]; then
    pass "Feature index internal consistency: $FEATURE_VALIDATED validated = $INDEX_COMPLETED completed"
else
    fail "Feature index inconsistency: $FEATURE_VALIDATED validated rows != $INDEX_COMPLETED in header"
    echo "  Fix: Update '**Completed:**' header in docs/specs/features/index.md to $FEATURE_VALIDATED"
fi

# ============================================================================
# Check 2: Maintenance index internal consistency
# ============================================================================
info "Checking maintenance index consistency..."

# Count "| **Resolved** |" rows
MAINT_RESOLVED=$(grep -c '| \*\*Resolved\*\* |' docs/maintenance/index.md 2>/dev/null || echo 0)

# Get "**Total Items:** 18" value from header
MAINT_TOTAL=$(grep '^\*\*Total Items:\*\*' docs/maintenance/index.md 2>/dev/null | \
              sed 's/.*\*\*Total Items:\*\* //' | sed 's/ .*//' | tr -d '[:space:]' || echo 0)

if [[ "$MAINT_RESOLVED" -eq "$MAINT_TOTAL" ]]; then
    pass "Maintenance index consistency: $MAINT_RESOLVED resolved = $MAINT_TOTAL total"
else
    fail "Maintenance index inconsistency: $MAINT_RESOLVED resolved rows != $MAINT_TOTAL total"
    echo "  Fix: Resolve remaining items or update '**Total Items:**' header"
fi

# ============================================================================
# Check 3: Version consistency across files
# ============================================================================
info "Checking version consistency..."

if [[ -f "VERSION" ]]; then
    VERSION=$(cat VERSION | tr -d '[:space:]')

    # Check context.md has version
    if grep -q "$VERSION" docs/context.md 2>/dev/null; then
        pass "Version $VERSION found in context.md"
    else
        fail "Version $VERSION not found in context.md"
        echo "  Fix: Update 'Version:' in docs/context.md to $VERSION"
    fi

    # Check CLAUDE.md has version
    if grep -q "$VERSION" CLAUDE.md 2>/dev/null; then
        pass "Version $VERSION found in CLAUDE.md"
    else
        fail "Version $VERSION not found in CLAUDE.md"
        echo "  Fix: Update 'Version:' in CLAUDE.md to $VERSION"
    fi
else
    fail "VERSION file not found"
fi

# ============================================================================
# Check 4: Feature/maintenance status matches context.md state description
# ============================================================================
info "Checking project state description..."

# If all features validated, context should reflect this
if [[ "$FEATURE_VALIDATED" -gt 0 ]] && [[ "$FEATURE_VALIDATED" -eq "$INDEX_COMPLETED" ]]; then
    if grep -q "All.*features validated" docs/context.md 2>/dev/null || \
       grep -q "31/31" docs/context.md 2>/dev/null || \
       grep -q "$FEATURE_VALIDATED features" docs/context.md 2>/dev/null; then
        pass "Context.md correctly reports features validated"
    else
        fail "Context.md doesn't reflect all features validated"
        echo "  Fix: Update docs/context.md to show all $FEATURE_VALIDATED features validated"
    fi
fi

# If all maintenance resolved, context should reflect this
if [[ "$MAINT_RESOLVED" -gt 0 ]] && [[ "$MAINT_RESOLVED" -eq "$MAINT_TOTAL" ]]; then
    if grep -q "All.*maintenance.*resolved" docs/context.md 2>/dev/null || \
       grep -qi "maintenance complete" docs/context.md 2>/dev/null || \
       grep -q "$MAINT_RESOLVED.*resolved" docs/context.md 2>/dev/null; then
        pass "Context.md correctly reports maintenance complete"
    else
        fail "Context.md doesn't reflect maintenance complete"
        echo "  Fix: Update docs/context.md to show all $MAINT_RESOLVED maintenance items resolved"
    fi
fi

# ============================================================================
# Check 5: Dogfooding state consistency
# ============================================================================
info "Checking dogfooding state..."

# If BUG-018 exists and is not resolved, dogfooding should be blocked
if [[ -f "docs/bugs/BUG-018-cli-search-hang.md" ]]; then
    BUG_STATUS=$(grep 'Status:' docs/bugs/BUG-018-cli-search-hang.md 2>/dev/null | \
                 head -1 | sed 's/.*Status://' | tr -d '[:space:]*' || echo "Unknown")

    if [[ "$BUG_STATUS" != "Resolved" ]]; then
        # Dogfooding should be marked as blocked in context.md
        if grep -qi "blocked\|BUG-018" docs/context.md 2>/dev/null; then
            pass "Dogfooding correctly marked as blocked (BUG-018 unresolved)"
        else
            fail "BUG-018 unresolved but dogfooding not marked blocked in context.md"
            echo "  Fix: Update docs/context.md status to show dogfooding blocked by BUG-018"
        fi
    else
        pass "BUG-018 is resolved"
    fi
else
    pass "No blocking bugs found"
fi

# ============================================================================
# Summary
# ============================================================================
header "Summary"

echo ""
echo -e "${GREEN}Passed:${NC} $PASSED"
echo -e "${RED}Failed:${NC} $FAILED"
echo ""

if [[ $FAILED -gt 0 ]]; then
    echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${RED} SSOT CONSISTENCY FAILED${NC}"
    echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo "The SSOT files have drifted out of sync."
    echo "Follow the fix instructions above to restore consistency."
    exit 1
else
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${GREEN} SSOT CONSISTENCY VERIFIED${NC}"
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    exit 0
fi
