#!/usr/bin/env bash
#
# ADR Validation Script for AmanMCP
#
# Purpose: Ensures ADR index matches filesystem and all references are valid.
# Implements prevention measures from RCA-002.
#
# Usage:
#   ./scripts/validate-adrs.sh
#
# Exit Codes:
#   0 - All ADRs valid
#   1 - Validation failed

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

ERRORS=0
ADR_DIR="docs/decisions"
INDEX_FILE="$ADR_DIR/index.md"

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

# Check that index.md exists
check_index_exists() {
    log_check "Index file exists"

    if [[ ! -f "$INDEX_FILE" ]]; then
        log_error "Index file not found: $INDEX_FILE"
        exit 1
    fi

    log_success "Index file exists"
}

# Check all index entries have corresponding files
check_index_entries_have_files() {
    log_check "All index entries have files"

    local missing=0

    # Extract ADR filenames from index
    while IFS= read -r file; do
        if [[ ! -f "$ADR_DIR/$file" ]]; then
            log_error "Index references missing file: $file"
            missing=$((missing + 1))
        fi
    done < <(grep -oE 'ADR-[0-9]+-[a-z0-9-]+\.md' "$INDEX_FILE" | sort -u)

    if [[ $missing -eq 0 ]]; then
        log_success "All index entries have files"
    fi
}

# Check all ADR files are in index
check_files_in_index() {
    log_check "All ADR files are in index"

    local orphaned=0

    for file in "$ADR_DIR"/ADR-*.md; do
        if [[ -f "$file" ]]; then
            local basename=$(basename "$file")
            if ! grep -q "$basename" "$INDEX_FILE"; then
                log_error "File not in index: $basename"
                orphaned=$((orphaned + 1))
            fi
        fi
    done

    if [[ $orphaned -eq 0 ]]; then
        log_success "All ADR files are in index"
    fi
}

# Check all ADR-XXX references in codebase point to existing ADRs
check_adr_references() {
    log_check "All ADR-XXX references are valid"

    local invalid=0

    # Exclude patterns for historical documentation (RCAs, common-issues document historical bugs)
    local exclude_patterns="postmortems|common-issues"

    # Find all ADR-XXX references (excluding decisions dir and historical docs)
    while IFS= read -r ref; do
        local num=$(echo "$ref" | grep -oE '[0-9]+')
        # Check if an ADR file with this number exists
        if ! ls "$ADR_DIR"/ADR-"${num}"-*.md &>/dev/null 2>&1; then
            # Check if references are only in excluded files
            local active_refs=$(grep -rl "ADR-$num" . --include="*.md" --include="*.sh" --include="Makefile" 2>/dev/null | grep -v "$ADR_DIR" | grep -vE "$exclude_patterns" || true)
            if [[ -n "$active_refs" ]]; then
                log_error "Reference to non-existent ADR-$num"
                echo "$active_refs" | head -3 | while read -r loc; do
                    log_info "  Found in: $loc"
                done
                invalid=$((invalid + 1))
            fi
        fi
    done < <(grep -rohE 'ADR-[0-9]+' . --include="*.md" --include="*.sh" --include="Makefile" 2>/dev/null | grep -v "ADR-XXX" | sort -u)

    if [[ $invalid -eq 0 ]]; then
        log_success "All ADR references are valid"
    fi
}

# Check ADR files have required sections
check_template_compliance() {
    log_check "ADR template compliance"

    local noncompliant=0
    local required_sections=("## Context" "## Decision" "## Rationale" "## Consequences" "## Changelog")

    for file in "$ADR_DIR"/ADR-*.md; do
        if [[ -f "$file" ]]; then
            local basename=$(basename "$file")
            for section in "${required_sections[@]}"; do
                if ! grep -q "^$section" "$file"; then
                    log_error "$basename missing section: $section"
                    noncompliant=$((noncompliant + 1))
                fi
            done
        fi
    done

    if [[ $noncompliant -eq 0 ]]; then
        log_success "All ADRs have required sections"
    fi
}

# Check file count matches
check_counts_match() {
    log_check "File count matches index count"

    local file_count=$(ls "$ADR_DIR"/ADR-*.md 2>/dev/null | wc -l | tr -d ' ')
    local index_count=$(grep -cE '^\| \[ADR-[0-9]+' "$INDEX_FILE" || echo "0")

    log_info "ADR files: $file_count"
    log_info "Index entries: $index_count"

    if [[ "$file_count" -ne "$index_count" ]]; then
        log_error "Count mismatch: $file_count files vs $index_count index entries"
    else
        log_success "Counts match: $file_count ADRs"
    fi
}

# Main execution
main() {
    echo ""
    echo "========================================"
    echo "  ADR Validation"
    echo "  (RCA-002 Prevention)"
    echo "========================================"
    echo ""

    check_index_exists
    echo ""
    check_index_entries_have_files
    echo ""
    check_files_in_index
    echo ""
    check_adr_references
    echo ""
    check_template_compliance
    echo ""
    check_counts_match
    echo ""

    if [[ $ERRORS -gt 0 ]]; then
        echo "========================================"
        echo -e "${RED}✗ FAILED: $ERRORS validation errors${NC}"
        echo "========================================"
        echo ""
        echo "Fix:"
        echo "  1. Create missing ADR files"
        echo "  2. Update index to match files"
        echo "  3. Fix invalid ADR references"
        echo "  4. Add missing template sections"
        echo ""
        exit 1
    else
        echo "========================================"
        echo -e "${GREEN}✓ All ADR validations passed${NC}"
        echo "========================================"
        echo ""
        exit 0
    fi
}

main "$@"
