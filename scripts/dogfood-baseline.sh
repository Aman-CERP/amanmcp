#!/bin/bash
# DEPRECATED: Use MCP-based validation instead (avoids BoltDB lock conflicts)
#
#   make validate-tier1   # Run Tier 1 tests (must pass 100%)
#   make validate-tier2   # Run Tier 2 tests (should pass 75%)
#   make validate-all     # Run full validation suite
#
# This script uses CLI commands which require exclusive BoltDB access.
# It will hang if MCP serve or Claude Code has the index open.
# The new MCP-based tests in internal/validation/ use direct MCP calls.
#
# ---
#
# Automated Baseline Query Testing for AmanMCP Dogfooding (DEPRECATED)
#
# Runs baseline queries against AmanMCP and validates results
# Requires: amanmcp binary in PATH, indexed codebase
#
# Usage: ./scripts/dogfood-baseline.sh [--tier1|--tier2|--negative|--all]
#
# Exit codes:
#   0 - All tests passed
#   1 - Tier 1 failures (critical)
#   2 - Tier 2 failures only (acceptable)
#   3 - Negative test failures (critical)

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
RESULTS_FILE="${PROJECT_ROOT}/.aman-pm/validation/baseline-results.json"
STATE_FILE="${PROJECT_ROOT}/.aman-pm/validation/state.json"

# Counters
TIER1_PASSED=0
TIER1_FAILED=0
TIER2_PASSED=0
TIER2_FAILED=0
NEGATIVE_PASSED=0
NEGATIVE_FAILED=0

# Results array for JSON
declare -a RESULTS=()

# Check if amanmcp is available
check_amanmcp() {
    if ! command -v amanmcp &> /dev/null; then
        echo -e "${RED}Error: amanmcp not found in PATH.${NC}"
        echo "Install with: make install-local"
        exit 1
    fi
}

# Check if index exists
check_index() {
    if [[ ! -d "${PROJECT_ROOT}/.amanmcp" ]]; then
        echo -e "${YELLOW}Index not found. Running 'amanmcp index .'...${NC}"
        cd "$PROJECT_ROOT"
        amanmcp index .
    fi
}

# Run a search query and check if expected file is in results
run_query() {
    local tier="$1"
    local query="$2"
    local expected="$3"
    local tool="${4:-search}"
    local extra_args="${5:-}"

    echo -n "  Testing: \"${query}\" → ${expected} ... "

    local cmd="amanmcp ${tool} \"${query}\" --limit 10 --format json ${extra_args}"
    local output
    local exit_code=0

    cd "$PROJECT_ROOT"
    output=$(eval "$cmd" 2>&1) || exit_code=$?

    if [[ $exit_code -ne 0 ]]; then
        echo -e "${RED}FAIL${NC} (command error)"
        RESULTS+=("{\"tier\":\"${tier}\",\"query\":\"${query}\",\"expected\":\"${expected}\",\"status\":\"error\",\"error\":\"command failed\"}")
        return 1
    fi

    # Check if expected file/pattern appears in results
    if echo "$output" | grep -q "$expected"; then
        echo -e "${GREEN}PASS${NC}"
        RESULTS+=("{\"tier\":\"${tier}\",\"query\":\"${query}\",\"expected\":\"${expected}\",\"status\":\"pass\"}")
        return 0
    else
        echo -e "${RED}FAIL${NC} (expected not found)"
        RESULTS+=("{\"tier\":\"${tier}\",\"query\":\"${query}\",\"expected\":\"${expected}\",\"status\":\"fail\",\"output\":\"$(echo "$output" | head -5 | tr '\n' ' ')\"}")
        return 1
    fi
}

# Run negative test (should NOT crash, MAY return empty)
run_negative() {
    local query="$1"
    local description="$2"

    echo -n "  Testing: ${description} ... "

    local cmd="amanmcp search \"${query}\" --limit 5 --format json"
    local output
    local exit_code=0

    cd "$PROJECT_ROOT"
    output=$(eval "$cmd" 2>&1) || exit_code=$?

    # For negative tests, we just check it doesn't crash (exit 0 or empty results OK)
    # Exit code 0 means success, even with empty results
    if [[ $exit_code -eq 0 ]]; then
        echo -e "${GREEN}PASS${NC} (handled gracefully)"
        RESULTS+=("{\"tier\":\"negative\",\"query\":\"${query}\",\"description\":\"${description}\",\"status\":\"pass\"}")
        return 0
    else
        # Some errors are acceptable for edge cases
        if echo "$output" | grep -qE "(no results|empty|not found)"; then
            echo -e "${GREEN}PASS${NC} (expected empty)"
            RESULTS+=("{\"tier\":\"negative\",\"query\":\"${query}\",\"description\":\"${description}\",\"status\":\"pass\"}")
            return 0
        fi
        echo -e "${RED}FAIL${NC} (unexpected error)"
        RESULTS+=("{\"tier\":\"negative\",\"query\":\"${query}\",\"description\":\"${description}\",\"status\":\"fail\",\"error\":\"${output}\"}")
        return 1
    fi
}

# Tier 1 queries (must pass - 12 queries)
run_tier1() {
    echo -e "\n${BOLD}${CYAN}=== Tier 1 Queries (Must Pass) ===${NC}\n"

    # Code location queries
    if run_query "tier1" "Where is the vector store created" "internal/store/hnsw" "search"; then
        ((TIER1_PASSED++))
    else
        ((TIER1_FAILED++))
    fi

    if run_query "tier1" "How does RRF fusion work" "internal/search/fusion" "search"; then
        ((TIER1_PASSED++))
    else
        ((TIER1_FAILED++))
    fi

    if run_query "tier1" "Find the embedder interface" "internal/embed" "search"; then
        ((TIER1_PASSED++))
    else
        ((TIER1_FAILED++))
    fi

    if run_query "tier1" "Where are MCP tools registered" "internal/mcp/server" "search"; then
        ((TIER1_PASSED++))
    else
        ((TIER1_FAILED++))
    fi

    # Documentation queries
    if run_query "tier1" "What are the performance targets" "docs/" "search"; then
        ((TIER1_PASSED++))
    else
        ((TIER1_FAILED++))
    fi

    if run_query "tier1" "How do I add a new language" "docs/" "search"; then
        ((TIER1_PASSED++))
    else
        ((TIER1_FAILED++))
    fi

    # Symbol queries
    if run_query "tier1" "Search function" "internal/search/engine" "search"; then
        ((TIER1_PASSED++))
    else
        ((TIER1_FAILED++))
    fi

    if run_query "tier1" "Index function" "internal/index" "search"; then
        ((TIER1_PASSED++))
    else
        ((TIER1_FAILED++))
    fi

    if run_query "tier1" "Chunk type" "internal/chunk" "search"; then
        ((TIER1_PASSED++))
    else
        ((TIER1_FAILED++))
    fi

    if run_query "tier1" "Config type" "internal/config" "search"; then
        ((TIER1_PASSED++))
    else
        ((TIER1_FAILED++))
    fi

    # Cross-reference queries
    if run_query "tier1" "OllamaEmbedder" "internal/embed/ollama" "search"; then
        ((TIER1_PASSED++))
    else
        ((TIER1_FAILED++))
    fi

    if run_query "tier1" "Config struct fields" "internal/config" "search"; then
        ((TIER1_PASSED++))
    else
        ((TIER1_FAILED++))
    fi

    echo ""
    echo -e "Tier 1 Results: ${GREEN}${TIER1_PASSED} passed${NC}, ${RED}${TIER1_FAILED} failed${NC} (12 total)"
}

# Tier 2 queries (should pass - 4 queries)
run_tier2() {
    echo -e "\n${BOLD}${CYAN}=== Tier 2 Queries (Should Pass) ===${NC}\n"

    if run_query "tier2" "What configuration options exist" "config" "search"; then
        ((TIER2_PASSED++))
    else
        ((TIER2_FAILED++))
    fi

    if run_query "tier2" "Installation instructions" "README" "search"; then
        ((TIER2_PASSED++))
    else
        ((TIER2_FAILED++))
    fi

    if run_query "tier2" "Error codes" "internal/" "search"; then
        ((TIER2_PASSED++))
    else
        ((TIER2_FAILED++))
    fi

    if run_query "tier2" "SearchOptions parameters" "search" "search"; then
        ((TIER2_PASSED++))
    else
        ((TIER2_FAILED++))
    fi

    echo ""
    echo -e "Tier 2 Results: ${GREEN}${TIER2_PASSED} passed${NC}, ${RED}${TIER2_FAILED} failed${NC} (4 total)"
}

# Negative tests (must not crash - 4 tests)
run_negative_tests() {
    echo -e "\n${BOLD}${CYAN}=== Negative Tests (Must Not Crash) ===${NC}\n"

    if run_negative "xyznonexistent123abcdef" "Non-existent symbol"; then
        ((NEGATIVE_PASSED++))
    else
        ((NEGATIVE_FAILED++))
    fi

    if run_negative "func(); DROP TABLE users;" "SQL injection attempt"; then
        ((NEGATIVE_PASSED++))
    else
        ((NEGATIVE_FAILED++))
    fi

    if run_negative "" "Empty query string"; then
        ((NEGATIVE_PASSED++))
    else
        ((NEGATIVE_FAILED++))
    fi

    if run_negative "a" "Single character query"; then
        ((NEGATIVE_PASSED++))
    else
        ((NEGATIVE_FAILED++))
    fi

    echo ""
    echo -e "Negative Results: ${GREEN}${NEGATIVE_PASSED} passed${NC}, ${RED}${NEGATIVE_FAILED} failed${NC} (4 total)"
}

# Save results to JSON
save_results() {
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    mkdir -p "$(dirname "$RESULTS_FILE")"

    cat > "$RESULTS_FILE" << EOF
{
  "timestamp": "${timestamp}",
  "summary": {
    "tier1": {"passed": ${TIER1_PASSED}, "failed": ${TIER1_FAILED}, "total": 12},
    "tier2": {"passed": ${TIER2_PASSED}, "failed": ${TIER2_FAILED}, "total": 4},
    "negative": {"passed": ${NEGATIVE_PASSED}, "failed": ${NEGATIVE_FAILED}, "total": 4}
  },
  "results": [
$(IFS=,; echo "${RESULTS[*]}" | sed 's/},{/},\n    {/g' | sed 's/^/    /')
  ]
}
EOF

    echo -e "\n${CYAN}Results saved to: ${RESULTS_FILE}${NC}"

    # Update state.json if it exists
    if [[ -f "$STATE_FILE" ]]; then
        jq --argjson t1 "$TIER1_PASSED" \
           --argjson t2 "$TIER2_PASSED" \
           --argjson neg "$NEGATIVE_PASSED" \
           --arg file "$RESULTS_FILE" \
           '.phases.phase_2.tier1_passed = $t1 | .phases.phase_2.tier2_passed = $t2 | .phases.phase_2.negative_passed = $neg | .phases.phase_2.results_file = $file' \
           "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
    fi
}

# Print final summary
print_summary() {
    echo -e "\n${BOLD}${CYAN}=== Final Summary ===${NC}\n"

    local total_passed=$((TIER1_PASSED + TIER2_PASSED + NEGATIVE_PASSED))
    local total_failed=$((TIER1_FAILED + TIER2_FAILED + NEGATIVE_FAILED))
    local total=$((total_passed + total_failed))

    echo "Total: ${total_passed}/${total} tests passed"
    echo ""

    # Exit criteria check
    local tier1_rate=$((TIER1_PASSED * 100 / 12))
    local tier2_rate=$((TIER2_PASSED * 100 / 4))

    echo -e "${BOLD}Exit Criteria:${NC}"
    echo -n "  Tier 1 (100% required): ${tier1_rate}% "
    if [[ $TIER1_FAILED -eq 0 ]]; then
        echo -e "${GREEN}PASS${NC}"
    else
        echo -e "${RED}FAIL${NC}"
    fi

    echo -n "  Tier 2 (75% required):  ${tier2_rate}% "
    if [[ $TIER2_PASSED -ge 3 ]]; then
        echo -e "${GREEN}PASS${NC}"
    else
        echo -e "${YELLOW}WARN${NC}"
    fi

    echo -n "  Negative (100%):        "
    if [[ $NEGATIVE_FAILED -eq 0 ]]; then
        echo -e "${GREEN}PASS${NC}"
    else
        echo -e "${RED}FAIL${NC}"
    fi

    echo ""

    # Overall result
    if [[ $TIER1_FAILED -eq 0 && $NEGATIVE_FAILED -eq 0 ]]; then
        if [[ $TIER2_PASSED -ge 3 ]]; then
            echo -e "${GREEN}${BOLD}OVERALL: PASS${NC}"
            return 0
        else
            echo -e "${YELLOW}${BOLD}OVERALL: PARTIAL (Tier 2 below 75%)${NC}"
            return 2
        fi
    elif [[ $TIER1_FAILED -gt 0 ]]; then
        echo -e "${RED}${BOLD}OVERALL: FAIL (Tier 1 failures)${NC}"
        return 1
    else
        echo -e "${RED}${BOLD}OVERALL: FAIL (Negative test failures)${NC}"
        return 3
    fi
}

# Show help
show_help() {
    echo -e "${BOLD}${CYAN}AmanMCP Baseline Query Testing${NC}"
    echo ""
    echo "Usage: ./scripts/dogfood-baseline.sh [options]"
    echo ""
    echo "Options:"
    echo "  --all       Run all tests (default)"
    echo "  --tier1     Run Tier 1 tests only (12 must-pass)"
    echo "  --tier2     Run Tier 2 tests only (4 should-pass)"
    echo "  --negative  Run negative tests only (4 must-not-crash)"
    echo "  --help      Show this help"
    echo ""
    echo "Exit codes:"
    echo "  0 - All tests passed"
    echo "  1 - Tier 1 failures (critical)"
    echo "  2 - Tier 2 failures only (acceptable)"
    echo "  3 - Negative test failures (critical)"
}

# Main
main() {
    local run_tier1=false
    local run_tier2=false
    local run_negative=false

    # Parse arguments
    if [[ $# -eq 0 ]]; then
        run_tier1=true
        run_tier2=true
        run_negative=true
    else
        case "$1" in
            "--all")
                run_tier1=true
                run_tier2=true
                run_negative=true
                ;;
            "--tier1")
                run_tier1=true
                ;;
            "--tier2")
                run_tier2=true
                ;;
            "--negative")
                run_negative=true
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
    fi

    echo -e "${BOLD}${CYAN}=== AmanMCP Baseline Query Testing ===${NC}"
    echo ""

    check_amanmcp
    check_index

    # Run selected tests
    [[ "$run_tier1" == true ]] && run_tier1
    [[ "$run_tier2" == true ]] && run_tier2
    [[ "$run_negative" == true ]] && run_negative_tests

    save_results
    print_summary
}

main "$@"
