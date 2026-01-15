#!/usr/bin/env bash
#
# Unified Documentation Validation Script for AmanMCP
#
# Purpose: Validates ADRs, RCAs, and Tech-Debt registries for integrity.
# Ensures indexes match filesystem and all cross-references are valid.
# Implements prevention measures from RCA-002.
#
# Usage:
#   ./scripts/validate-docs.sh [--adr-only|--rca-only|--debt-only]
#
# Exit Codes:
#   0 - All validations passed
#   1 - Validation failed

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

ERRORS=0
WARNINGS=0

# Directories
ADR_DIR="docs/decisions"
RCA_DIR=".aman-pm/postmortems"
DEBT_DIR=".aman-pm/backlog/debt"

log_section() {
    echo ""
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

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

log_warning() {
    echo -e "${YELLOW}  ⚠ $1${NC}"
    WARNINGS=$((WARNINGS + 1))
}

log_info() {
    echo -e "    $1"
}

# ============================================================================
# ADR VALIDATION
# ============================================================================

validate_adrs() {
    log_section "ADR Validation"

    local index_file="$ADR_DIR/index.md"

    # Check index exists
    log_check "ADR index file exists"
    if [[ ! -f "$index_file" ]]; then
        log_error "Index file not found: $index_file"
        return
    fi
    log_success "Index file exists"

    # Check all index entries have files
    log_check "All ADR index entries have files"
    local missing=0
    while IFS= read -r file; do
        if [[ ! -f "$ADR_DIR/$file" ]]; then
            log_error "Index references missing file: $file"
            missing=$((missing + 1))
        fi
    done < <(grep -oE 'ADR-[0-9]+-[a-z0-9-]+\.md' "$index_file" | sort -u)
    if [[ $missing -eq 0 ]]; then
        log_success "All index entries have files"
    fi

    # Check all ADR files are in index
    log_check "All ADR files are in index"
    local orphaned=0
    for file in "$ADR_DIR"/ADR-*.md; do
        if [[ -f "$file" ]]; then
            local basename=$(basename "$file")
            if ! grep -q "$basename" "$index_file"; then
                log_error "File not in index: $basename"
                orphaned=$((orphaned + 1))
            fi
        fi
    done
    if [[ $orphaned -eq 0 ]]; then
        log_success "All ADR files are in index"
    fi

    # Check ADR references in codebase
    log_check "All ADR-XXX references are valid"
    local invalid=0
    local exclude_patterns="postmortems|common-issues"
    while IFS= read -r ref; do
        local num=$(echo "$ref" | grep -oE '[0-9]+')
        if ! ls "$ADR_DIR"/ADR-"${num}"-*.md &>/dev/null 2>&1; then
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

    # Check template compliance
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

    # Check counts
    log_check "ADR file count matches index count"
    local file_count=$(ls "$ADR_DIR"/ADR-*.md 2>/dev/null | wc -l | tr -d ' ')
    local index_count=$(grep -cE '^\| \[ADR-[0-9]+' "$index_file" || echo "0")
    log_info "ADR files: $file_count"
    log_info "Index entries: $index_count"
    if [[ "$file_count" -ne "$index_count" ]]; then
        log_error "Count mismatch: $file_count files vs $index_count index entries"
    else
        log_success "Counts match: $file_count ADRs"
    fi
}

# ============================================================================
# RCA VALIDATION
# ============================================================================

validate_rcas() {
    log_section "RCA Validation"

    local index_file="$RCA_DIR/index.md"

    # Check index exists
    log_check "RCA index file exists"
    if [[ ! -f "$index_file" ]]; then
        log_error "Index file not found: $index_file"
        return
    fi
    log_success "Index file exists"

    # Check all index entries have files (only from registry table, not examples)
    log_check "All RCA index entries have files"
    local missing=0
    while IFS= read -r file; do
        if [[ ! -f "$RCA_DIR/$file" ]]; then
            log_error "Index references missing file: $file"
            missing=$((missing + 1))
        fi
    done < <(grep -E '^\| \[RCA-[0-9]+\]' "$index_file" | grep -oE 'RCA-[0-9]+-[a-z0-9-]+\.md' | sort -u)
    if [[ $missing -eq 0 ]]; then
        log_success "All index entries have files"
    fi

    # Check all RCA files are in index
    log_check "All RCA files are in index"
    local orphaned=0
    for file in "$RCA_DIR"/RCA-*.md; do
        if [[ -f "$file" ]]; then
            local basename=$(basename "$file")
            if ! grep -q "$basename" "$index_file"; then
                log_error "File not in index: $basename"
                orphaned=$((orphaned + 1))
            fi
        fi
    done
    if [[ $orphaned -eq 0 ]]; then
        log_success "All RCA files are in index"
    fi

    # Check RCA references in codebase
    log_check "All RCA-XXX references are valid"
    local invalid=0
    while IFS= read -r ref; do
        local num=$(echo "$ref" | grep -oE '[0-9]+')
        if ! ls "$RCA_DIR"/RCA-"${num}"-*.md &>/dev/null 2>&1; then
            local active_refs=$(grep -rl "RCA-$num" . --include="*.md" --include="*.sh" 2>/dev/null | grep -v "$RCA_DIR" || true)
            if [[ -n "$active_refs" ]]; then
                log_error "Reference to non-existent RCA-$num"
                echo "$active_refs" | head -3 | while read -r loc; do
                    log_info "  Found in: $loc"
                done
                invalid=$((invalid + 1))
            fi
        fi
    done < <(grep -rohE 'RCA-[0-9]+' . --include="*.md" --include="*.sh" 2>/dev/null | grep -v "RCA-XXX" | sort -u)
    if [[ $invalid -eq 0 ]]; then
        log_success "All RCA references are valid"
    fi

    # Check counts
    log_check "RCA file count matches index count"
    local file_count=$(ls "$RCA_DIR"/RCA-*.md 2>/dev/null | wc -l | tr -d ' ')
    local index_count=$(grep -cE '^\| \[RCA-[0-9]+' "$index_file" || echo "0")
    log_info "RCA files: $file_count"
    log_info "Index entries: $index_count"
    if [[ "$file_count" -ne "$index_count" ]]; then
        log_error "Count mismatch: $file_count files vs $index_count index entries"
    else
        log_success "Counts match: $file_count RCAs"
    fi
}

# ============================================================================
# TECH-DEBT VALIDATION
# ============================================================================

validate_tech_debt() {
    log_section "Tech-Debt Validation"

    local index_file="$DEBT_DIR/index.md"

    # Check index exists
    log_check "Tech-debt index file exists"
    if [[ ! -f "$index_file" ]]; then
        log_error "Index file not found: $index_file"
        return
    fi
    log_success "Index file exists"

    # Check all index entries have files
    log_check "All tech-debt index entries have files"
    local missing=0
    while IFS= read -r file; do
        if [[ ! -f "$DEBT_DIR/$file" ]]; then
            log_error "Index references missing file: $file"
            missing=$((missing + 1))
        fi
    done < <(grep -oE 'DEBT-[0-9]+-[a-z0-9-]+\.md' "$index_file" | sort -u)
    if [[ $missing -eq 0 ]]; then
        log_success "All index entries have files"
    fi

    # Check all DEBT files are in index
    log_check "All DEBT files are in index"
    local orphaned=0
    for file in "$DEBT_DIR"/DEBT-*.md; do
        if [[ -f "$file" ]]; then
            local basename=$(basename "$file")
            if ! grep -q "$basename" "$index_file"; then
                log_error "File not in index: $basename"
                orphaned=$((orphaned + 1))
            fi
        fi
    done
    if [[ $orphaned -eq 0 ]]; then
        log_success "All DEBT files are in index"
    fi

    # Check counts
    log_check "Tech-debt file count matches index count"
    local file_count=$(ls "$DEBT_DIR"/DEBT-*.md 2>/dev/null | wc -l | tr -d ' ')
    local index_count=$(grep -cE '\[DEBT-[0-9]+\]' "$index_file" || echo "0")
    log_info "DEBT files: $file_count"
    log_info "Index entries: $index_count"
    if [[ "$file_count" -ne "$index_count" ]]; then
        log_error "Count mismatch: $file_count files vs $index_count index entries"
    else
        log_success "Counts match: $file_count tech-debt items"
    fi

    # Warn if old docs/tech-debt.md still exists
    if [[ -f "docs/tech-debt.md" ]]; then
        log_warning "Old docs/tech-debt.md still exists - should be migrated to docs/tech-debt/"
    fi
}

# ============================================================================
# MAIN
# ============================================================================

main() {
    local validate_all=true
    local validate_adr=false
    local validate_rca=false
    local validate_debt=false

    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --adr-only)
                validate_all=false
                validate_adr=true
                shift
                ;;
            --rca-only)
                validate_all=false
                validate_rca=true
                shift
                ;;
            --debt-only)
                validate_all=false
                validate_debt=true
                shift
                ;;
            *)
                echo "Unknown option: $1"
                echo "Usage: $0 [--adr-only|--rca-only|--debt-only]"
                exit 1
                ;;
        esac
    done

    echo ""
    echo "╔════════════════════════════════════════════════════════════════╗"
    echo "║           Documentation Validation (RCA-002 Prevention)        ║"
    echo "╚════════════════════════════════════════════════════════════════╝"

    if $validate_all || $validate_adr; then
        validate_adrs
    fi

    if $validate_all || $validate_rca; then
        validate_rcas
    fi

    if $validate_all || $validate_debt; then
        validate_tech_debt
    fi

    echo ""
    echo "╔════════════════════════════════════════════════════════════════╗"

    if [[ $ERRORS -gt 0 ]]; then
        echo -e "║ ${RED}FAILED: $ERRORS errors, $WARNINGS warnings${NC}"
        echo "╚════════════════════════════════════════════════════════════════╝"
        echo ""
        echo "Fix:"
        echo "  1. Create missing files referenced in indexes"
        echo "  2. Add orphaned files to indexes"
        echo "  3. Fix invalid cross-references"
        echo "  4. Add missing template sections"
        echo ""
        exit 1
    elif [[ $WARNINGS -gt 0 ]]; then
        echo -e "║ ${YELLOW}PASSED WITH WARNINGS: $WARNINGS warnings${NC}"
        echo "╚════════════════════════════════════════════════════════════════╝"
        echo ""
        exit 0
    else
        echo -e "║ ${GREEN}PASSED: All documentation validations passed${NC}"
        echo "╚════════════════════════════════════════════════════════════════╝"
        echo ""
        exit 0
    fi
}

main "$@"
