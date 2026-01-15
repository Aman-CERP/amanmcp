#!/bin/bash
# Documentation Drift Detection for AmanMCP
#
# Cross-references code and documentation to detect drift
# Uses pattern matching and file comparison to find inconsistencies
#
# Usage: ./scripts/drift-check.sh [--area embedder|config|errors|cli|all]
#
# Exit codes:
#   0 - Drift < 5% (acceptable)
#   1 - Drift >= 5% (action needed)

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'
BOLD='\033[1m'

# Find project root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPORT_FILE="${PROJECT_ROOT}/.aman-pm/validation/drift-report.md"
STATE_FILE="${PROJECT_ROOT}/.aman-pm/validation/state.json"

# Counters
TOTAL_CHECKS=0
DRIFT_ITEMS=0
declare -a DRIFT_DETAILS=()

# Check a drift area
check_drift() {
    local area="$1"
    local description="$2"
    local code_pattern="$3"
    local doc_pattern="$4"
    local code_file="$5"
    local doc_file="$6"

    ((TOTAL_CHECKS++))

    echo -n "  Checking: ${description}... "

    local code_matches=0
    local doc_matches=0

    # Count code occurrences
    if [[ -f "${PROJECT_ROOT}/${code_file}" ]]; then
        code_matches=$(grep -c "$code_pattern" "${PROJECT_ROOT}/${code_file}" 2>/dev/null || echo "0")
    elif [[ -d "${PROJECT_ROOT}/${code_file}" ]]; then
        code_matches=$(grep -r "$code_pattern" "${PROJECT_ROOT}/${code_file}" 2>/dev/null | wc -l | tr -d ' ')
    fi

    # Count doc occurrences
    if [[ -f "${PROJECT_ROOT}/${doc_file}" ]]; then
        doc_matches=$(grep -ci "$doc_pattern" "${PROJECT_ROOT}/${doc_file}" 2>/dev/null || echo "0")
    elif [[ -d "${PROJECT_ROOT}/${doc_file}" ]]; then
        doc_matches=$(grep -ri "$doc_pattern" "${PROJECT_ROOT}/${doc_file}" 2>/dev/null | wc -l | tr -d ' ')
    fi

    if [[ "$code_matches" -gt 0 && "$doc_matches" -eq 0 ]]; then
        echo -e "${RED}DRIFT${NC} (code has ${code_matches} refs, docs have 0)"
        ((DRIFT_ITEMS++))
        DRIFT_DETAILS+=("| ${area} | ${description} | Code: ${code_matches}, Docs: 0 | Update docs |")
        return 1
    elif [[ "$code_matches" -eq 0 && "$doc_matches" -gt 0 ]]; then
        echo -e "${YELLOW}STALE${NC} (docs reference removed code)"
        ((DRIFT_ITEMS++))
        DRIFT_DETAILS+=("| ${area} | ${description} | Code: 0, Docs: ${doc_matches} | Remove from docs |")
        return 1
    else
        echo -e "${GREEN}OK${NC} (code: ${code_matches}, docs: ${doc_matches})"
        return 0
    fi
}

# Check embedder drift
check_embedder() {
    echo -e "\n${BOLD}${CYAN}=== Embedder Documentation Drift ===${NC}\n"

    # Check OllamaEmbedder is documented (current default embedder as of v0.1.42)
    check_drift "embedder" "OllamaEmbedder in ADR-023" \
        "OllamaEmbedder" "ollama" \
        "internal/embed/ollama.go" "docs/decisions/ADR-023-ollama-http-api-embedder.md" || true

    # Check Static768Embedder fallback is documented
    check_drift "embedder" "Static768Embedder fallback" \
        "Static768Embedder\|StaticEmbedder768" "static.*768\|fallback" \
        "internal/embed/static768.go" "docs/decisions" || true
}

# Check config drift
check_config() {
    echo -e "\n${BOLD}${CYAN}=== Configuration Documentation Drift ===${NC}\n"

    # Count Config struct fields vs documented options
    local config_fields
    config_fields=$(grep -E "^\s+\w+\s+\w+" "${PROJECT_ROOT}/internal/config/config.go" 2>/dev/null | wc -l | tr -d ' ')

    echo -n "  Config struct fields: ${config_fields} ... "

    # Check if README documents configuration
    local readme_config_lines
    readme_config_lines=$(grep -ci "config\|option\|setting" "${PROJECT_ROOT}/README.md" 2>/dev/null || echo "0")

    if [[ "$readme_config_lines" -lt 5 ]]; then
        echo -e "${YELLOW}SPARSE${NC} (README has few config references)"
        ((TOTAL_CHECKS++))
        ((DRIFT_ITEMS++))
        DRIFT_DETAILS+=("| config | Config documentation sparse | ${config_fields} fields, ${readme_config_lines} README refs | Add config section |")
    else
        echo -e "${GREEN}OK${NC}"
        ((TOTAL_CHECKS++))
    fi

    # Check specific config options
    check_drift "config" "EmbedderProvider option" \
        "EmbedderProvider" "embedder.*provider\|provider.*embedder" \
        "internal/config/config.go" "docs/" || true

    check_drift "config" "VectorDimensions option" \
        "VectorDimensions\|Dimensions" "dimension" \
        "internal/config/config.go" "docs/" || true

    check_drift "config" "BatchSize option" \
        "BatchSize" "batch" \
        "internal/config/config.go" "docs/" || true
}

# Check error handling drift
check_errors() {
    echo -e "\n${BOLD}${CYAN}=== Error Handling Documentation Drift ===${NC}\n"

    # Count error wrappings in code
    local error_wraps
    error_wraps=$(grep -r "fmt.Errorf.*%w" "${PROJECT_ROOT}/internal" 2>/dev/null | wc -l | tr -d ' ')

    echo -n "  Error wrappings in code: ${error_wraps} ... "
    ((TOTAL_CHECKS++))

    # Check if error handling is documented
    local error_docs
    error_docs=$(grep -ri "error\|err" "${PROJECT_ROOT}/docs/guides" 2>/dev/null | wc -l | tr -d ' ')

    if [[ "$error_docs" -lt 10 ]]; then
        echo -e "${YELLOW}SPARSE${NC} (limited error documentation)"
        ((DRIFT_ITEMS++))
        DRIFT_DETAILS+=("| errors | Error handling docs sparse | ${error_wraps} wraps, ${error_docs} doc lines | Document error patterns |")
    else
        echo -e "${GREEN}OK${NC}"
    fi

    # Check specific error types
    check_drift "errors" "ErrNotIndexed" \
        "ErrNotIndexed" "not.*indexed\|index.*required" \
        "internal/" "docs/" || true

    check_drift "errors" "ErrNoResults" \
        "ErrNoResults\|no.*results" "no.*results\|empty.*results" \
        "internal/search" "docs/" || true
}

# Check CLI drift
check_cli() {
    echo -e "\n${BOLD}${CYAN}=== CLI Documentation Drift ===${NC}\n"

    # Find cobra commands in code
    local commands
    commands=$(grep -r "cobra.Command" "${PROJECT_ROOT}/cmd" 2>/dev/null | wc -l | tr -d ' ')

    echo -n "  CLI commands in code: ${commands} ... "
    ((TOTAL_CHECKS++))

    # Check README documents CLI usage
    local cli_docs
    cli_docs=$(grep -ci "amanmcp\|command\|--\|flag" "${PROJECT_ROOT}/README.md" 2>/dev/null || echo "0")

    if [[ "$cli_docs" -lt 10 ]]; then
        echo -e "${YELLOW}SPARSE${NC} (limited CLI documentation)"
        ((DRIFT_ITEMS++))
        DRIFT_DETAILS+=("| cli | CLI docs sparse | ${commands} commands, ${cli_docs} doc refs | Document CLI usage |")
    else
        echo -e "${GREEN}OK${NC}"
    fi

    # Check specific commands
    check_drift "cli" "index command" \
        "\"index\"" "amanmcp index" \
        "cmd/amanmcp" "README.md" || true

    check_drift "cli" "serve command" \
        "\"serve\"" "amanmcp serve" \
        "cmd/amanmcp" "README.md" || true

    check_drift "cli" "search command" \
        "\"search\"" "amanmcp search" \
        "cmd/amanmcp" "README.md" || true
}

# Generate drift report
generate_report() {
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    local drift_percentage=0
    if [[ $TOTAL_CHECKS -gt 0 ]]; then
        drift_percentage=$((DRIFT_ITEMS * 100 / TOTAL_CHECKS))
    fi

    mkdir -p "$(dirname "$REPORT_FILE")"

    cat > "$REPORT_FILE" << EOF
# Documentation Drift Report

**Generated:** ${timestamp}
**Checks Run:** ${TOTAL_CHECKS}
**Drift Items:** ${DRIFT_ITEMS}
**Drift Percentage:** ${drift_percentage}%

## Summary

EOF

    if [[ $drift_percentage -lt 5 ]]; then
        echo "Status: **PASS** - Drift is within acceptable limits (<5%)" >> "$REPORT_FILE"
    else
        echo "Status: **ACTION NEEDED** - Drift exceeds 5% threshold" >> "$REPORT_FILE"
    fi

    if [[ ${#DRIFT_DETAILS[@]} -gt 0 ]]; then
        cat >> "$REPORT_FILE" << EOF

## Drift Items

| Area | Issue | Details | Action |
|------|-------|---------|--------|
EOF
        for item in "${DRIFT_DETAILS[@]}"; do
            echo "$item" >> "$REPORT_FILE"
        done
    else
        echo -e "\nNo drift items found." >> "$REPORT_FILE"
    fi

    cat >> "$REPORT_FILE" << EOF

## Recommendations

1. Review each drift item and determine if code or docs need updating
2. For "STALE" items, remove outdated documentation
3. For "DRIFT" items, add missing documentation
4. Run this check weekly to catch new drift early

---

*Report generated by scripts/drift-check.sh*
EOF

    echo -e "\n${CYAN}Report saved to: ${REPORT_FILE}${NC}"

    # Update state.json if it exists
    if [[ -f "$STATE_FILE" ]]; then
        jq --argjson items "$DRIFT_ITEMS" \
           '.phases.phase_4.drift_items = $items' \
           "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
    fi
}

# Print summary
print_summary() {
    echo -e "\n${BOLD}${CYAN}=== Drift Check Summary ===${NC}\n"

    local drift_percentage=0
    if [[ $TOTAL_CHECKS -gt 0 ]]; then
        drift_percentage=$((DRIFT_ITEMS * 100 / TOTAL_CHECKS))
    fi

    echo "Total Checks: ${TOTAL_CHECKS}"
    echo "Drift Items:  ${DRIFT_ITEMS}"
    echo "Drift Rate:   ${drift_percentage}%"
    echo ""

    if [[ $drift_percentage -lt 5 ]]; then
        echo -e "${GREEN}${BOLD}RESULT: PASS${NC} (drift < 5%)"
        return 0
    else
        echo -e "${RED}${BOLD}RESULT: ACTION NEEDED${NC} (drift >= 5%)"
        return 1
    fi
}

# Show help
show_help() {
    echo -e "${BOLD}${CYAN}AmanMCP Documentation Drift Detection${NC}"
    echo ""
    echo "Usage: ./scripts/drift-check.sh [--area AREA]"
    echo ""
    echo "Options:"
    echo "  --area embedder  Check embedder documentation drift"
    echo "  --area config    Check configuration documentation drift"
    echo "  --area errors    Check error handling documentation drift"
    echo "  --area cli       Check CLI documentation drift"
    echo "  --area all       Check all areas (default)"
    echo "  --help           Show this help"
    echo ""
    echo "Exit codes:"
    echo "  0 - Drift < 5% (acceptable)"
    echo "  1 - Drift >= 5% (action needed)"
}

# Main
main() {
    local area="all"

    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case "$1" in
            "--area")
                area="$2"
                shift 2
                ;;
            "--help"|"-h")
                show_help
                exit 0
                ;;
            *)
                echo -e "${RED}Unknown option: $1${NC}"
                show_help
                exit 1
                ;;
        esac
    done

    echo -e "${BOLD}${CYAN}=== AmanMCP Documentation Drift Detection ===${NC}"

    case "$area" in
        "embedder")
            check_embedder
            ;;
        "config")
            check_config
            ;;
        "errors")
            check_errors
            ;;
        "cli")
            check_cli
            ;;
        "all")
            check_embedder
            check_config
            check_errors
            check_cli
            ;;
        *)
            echo -e "${RED}Unknown area: $area${NC}"
            show_help
            exit 1
            ;;
    esac

    generate_report
    print_summary
}

main "$@"
