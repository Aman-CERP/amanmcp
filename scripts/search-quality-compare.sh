#!/bin/bash
# Search Quality Comparison: amanmcp vs Claude Code tools (ripgrep/fd)
#
# Compares search quality between:
# - amanmcp (semantic + BM25 hybrid search)
# - ripgrep (keyword/regex, proxy for Claude Code Grep tool)
# - fd (file finder, proxy for Claude Code Glob tool)
#
# Usage: ./scripts/search-quality-compare.sh [--content|--files|--all]
#
# Prerequisites:
#   - amanmcp CLI must be working (run 'amanmcp status' to verify)
#   - ripgrep (rg) must be installed
#   - jq must be installed
#   - fd is optional (falls back to find)
#
# Exit codes:
#   0 - Comparison completed successfully

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
QUERIES_FILE="${SCRIPT_DIR}/search-quality-queries.json"
RESULTS_FILE="${PROJECT_ROOT}/.aman-pm/validation/comparison-results.json"
REPORT_FILE="${PROJECT_ROOT}/.aman-pm/validation/comparison-report.md"

# Counters
AMANMCP_WINS=0
RIPGREP_WINS=0
FD_WINS=0
TIES=0
TOTAL_CONTENT=0
TOTAL_FILES=0

# Latency accumulators
AMANMCP_LATENCY_SUM=0
RIPGREP_LATENCY_SUM=0
FD_LATENCY_SUM=0

# Results array
declare -a RESULTS=()

# Check dependencies
check_deps() {
    local missing=()

    if ! command -v amanmcp &> /dev/null; then
        missing+=("amanmcp")
    fi
    if ! command -v rg &> /dev/null; then
        missing+=("ripgrep (rg)")
    fi
    # fd is optional - we can use find as fallback
    if ! command -v fd &> /dev/null && ! command -v fdfind &> /dev/null; then
        echo -e "${YELLOW}Note: fd not found, using 'find' as fallback${NC}"
    fi
    if ! command -v jq &> /dev/null; then
        missing+=("jq")
    fi

    if [[ ${#missing[@]} -gt 0 ]]; then
        echo -e "${RED}Error: Missing dependencies: ${missing[*]}${NC}"
        echo "Install with:"
        echo "  brew install ripgrep jq  # macOS"
        echo "  apt install ripgrep jq  # Ubuntu/Debian"
        exit 1
    fi
}

# Get current timestamp in milliseconds
get_ms() {
    if [[ "$OSTYPE" == "darwin"* ]]; then
        # macOS: use gdate if available, otherwise perl
        if command -v gdate &> /dev/null; then
            gdate +%s%3N
        else
            perl -MTime::HiRes=time -e 'printf "%.0f\n", time * 1000'
        fi
    else
        date +%s%3N
    fi
}

# Run amanmcp search and return results
run_amanmcp() {
    local query="$1"
    local start end duration
    local timeout_secs=10

    start=$(get_ms)
    cd "$PROJECT_ROOT"
    local output
    # Use timeout to prevent hanging on CLI issues
    output=$(timeout "$timeout_secs" amanmcp search "$query" --limit 10 --format json 2>/dev/null) || true
    end=$(get_ms)
    duration=$((end - start))

    echo "$duration|$output"
}

# Run ripgrep search and return results
run_ripgrep() {
    local query="$1"
    local start end duration

    start=$(get_ms)
    cd "$PROJECT_ROOT"
    local output
    # Use -l for file list, search common code files
    output=$(rg -l "$query" --type-add 'code:*.{go,ts,js,py,md,json,yaml,yml}' --type code 2>/dev/null | head -10) || true
    end=$(get_ms)
    duration=$((end - start))

    echo "$duration|$output"
}

# Run fd/find search and return results
run_fd() {
    local query="$1"
    local start end duration

    start=$(get_ms)
    cd "$PROJECT_ROOT"
    local output

    # Try fd first, then fdfind, then fall back to find
    if command -v fd &> /dev/null; then
        output=$(fd -t f "$query" 2>/dev/null | head -10) || true
    elif command -v fdfind &> /dev/null; then
        output=$(fdfind -t f "$query" 2>/dev/null | head -10) || true
    else
        # Fallback to find - convert pattern for find
        # For patterns like "*.go", use find -name
        output=$(find . -type f -name "*$query*" 2>/dev/null | head -10) || true
    fi

    end=$(get_ms)
    duration=$((end - start))

    echo "$duration|$output"
}

# Check if expected result is in output
check_expected() {
    local output="$1"
    local expected="$2"

    if echo "$output" | grep -q "$expected"; then
        return 0
    fi
    return 1
}

# Get rank of expected result (1-indexed, 0 if not found)
get_rank() {
    local output="$1"
    local expected="$2"
    local rank=0
    local line_num=1

    while IFS= read -r line; do
        if echo "$line" | grep -q "$expected"; then
            rank=$line_num
            break
        fi
        ((line_num++))
    done <<< "$output"

    echo "$rank"
}

# Run content search comparison
run_content_comparison() {
    echo -e "\n${BOLD}${CYAN}=== Content Search: amanmcp vs ripgrep ===${NC}\n"

    local queries
    queries=$(jq -r '.content_search.queries[] | @base64' "$QUERIES_FILE")

    for row in $queries; do
        local data
        data=$(echo "$row" | base64 --decode)

        local id query expected qtype
        id=$(echo "$data" | jq -r '.id')
        query=$(echo "$data" | jq -r '.query')
        expected=$(echo "$data" | jq -r '.expected')
        qtype=$(echo "$data" | jq -r '.type')

        echo -n "  [$id] \"$query\" ($qtype) ... "

        # Run both searches
        local amanmcp_result ripgrep_result
        amanmcp_result=$(run_amanmcp "$query")
        ripgrep_result=$(run_ripgrep "$query")

        # Parse results
        local am_latency am_output rg_latency rg_output
        am_latency=$(echo "$amanmcp_result" | cut -d'|' -f1)
        am_output=$(echo "$amanmcp_result" | cut -d'|' -f2-)
        rg_latency=$(echo "$ripgrep_result" | cut -d'|' -f1)
        rg_output=$(echo "$ripgrep_result" | cut -d'|' -f2-)

        # Check if expected found
        local am_found=false rg_found=false
        if check_expected "$am_output" "$expected"; then
            am_found=true
        fi
        if check_expected "$rg_output" "$expected"; then
            rg_found=true
        fi

        # Determine winner
        local winner="tie"
        if [[ "$am_found" == true && "$rg_found" == false ]]; then
            winner="amanmcp"
            ((AMANMCP_WINS++))
            echo -e "${GREEN}amanmcp wins${NC} (${am_latency}ms vs ${rg_latency}ms)"
        elif [[ "$am_found" == false && "$rg_found" == true ]]; then
            winner="ripgrep"
            ((RIPGREP_WINS++))
            echo -e "${YELLOW}ripgrep wins${NC} (${am_latency}ms vs ${rg_latency}ms)"
        elif [[ "$am_found" == true && "$rg_found" == true ]]; then
            winner="tie"
            ((TIES++))
            echo -e "${BLUE}tie${NC} (${am_latency}ms vs ${rg_latency}ms)"
        else
            winner="neither"
            echo -e "${RED}neither found${NC} (${am_latency}ms vs ${rg_latency}ms)"
        fi

        # Accumulate latencies
        AMANMCP_LATENCY_SUM=$((AMANMCP_LATENCY_SUM + am_latency))
        RIPGREP_LATENCY_SUM=$((RIPGREP_LATENCY_SUM + rg_latency))
        ((TOTAL_CONTENT++))

        # Store result
        RESULTS+=("{\"id\":\"$id\",\"query\":\"$query\",\"type\":\"$qtype\",\"expected\":\"$expected\",\"amanmcp\":{\"found\":$am_found,\"latency_ms\":$am_latency},\"ripgrep\":{\"found\":$rg_found,\"latency_ms\":$rg_latency},\"winner\":\"$winner\"}")
    done
}

# Run file discovery comparison
run_file_comparison() {
    echo -e "\n${BOLD}${CYAN}=== File Discovery: amanmcp vs fd ===${NC}\n"

    local queries
    queries=$(jq -r '.file_discovery.queries[] | @base64' "$QUERIES_FILE")

    for row in $queries; do
        local data
        data=$(echo "$row" | base64 --decode)

        local id query expected qtype
        id=$(echo "$data" | jq -r '.id')
        query=$(echo "$data" | jq -r '.query')
        expected=$(echo "$data" | jq -r '.expected')
        qtype=$(echo "$data" | jq -r '.type')

        echo -n "  [$id] \"$query\" ($qtype) ... "

        # For semantic queries, use amanmcp; for glob, use fd
        local amanmcp_result fd_result
        amanmcp_result=$(run_amanmcp "$query")

        # For glob patterns, extract the file pattern for fd
        local fd_query="$query"
        if [[ "$qtype" == "glob" ]]; then
            # Convert glob to fd pattern: **/*.go -> .go
            fd_query=$(echo "$query" | sed 's/.*\///' | sed 's/\*//g')
        fi
        fd_result=$(run_fd "$fd_query")

        # Parse results
        local am_latency am_output fd_latency fd_output
        am_latency=$(echo "$amanmcp_result" | cut -d'|' -f1)
        am_output=$(echo "$amanmcp_result" | cut -d'|' -f2-)
        fd_latency=$(echo "$fd_result" | cut -d'|' -f1)
        fd_output=$(echo "$fd_result" | cut -d'|' -f2-)

        # Check if expected found
        local am_found=false fd_found=false
        if check_expected "$am_output" "$expected"; then
            am_found=true
        fi
        if check_expected "$fd_output" "$expected"; then
            fd_found=true
        fi

        # Determine winner
        local winner="tie"
        if [[ "$am_found" == true && "$fd_found" == false ]]; then
            winner="amanmcp"
            ((AMANMCP_WINS++))
            echo -e "${GREEN}amanmcp wins${NC} (${am_latency}ms vs ${fd_latency}ms)"
        elif [[ "$am_found" == false && "$fd_found" == true ]]; then
            winner="fd"
            ((FD_WINS++))
            echo -e "${YELLOW}fd wins${NC} (${am_latency}ms vs ${fd_latency}ms)"
        elif [[ "$am_found" == true && "$fd_found" == true ]]; then
            winner="tie"
            ((TIES++))
            echo -e "${BLUE}tie${NC} (${am_latency}ms vs ${fd_latency}ms)"
        else
            winner="neither"
            echo -e "${RED}neither found${NC} (${am_latency}ms vs ${fd_latency}ms)"
        fi

        # Accumulate latencies
        AMANMCP_LATENCY_SUM=$((AMANMCP_LATENCY_SUM + am_latency))
        FD_LATENCY_SUM=$((FD_LATENCY_SUM + fd_latency))
        ((TOTAL_FILES++))

        # Store result
        RESULTS+=("{\"id\":\"$id\",\"query\":\"$query\",\"type\":\"$qtype\",\"expected\":\"$expected\",\"amanmcp\":{\"found\":$am_found,\"latency_ms\":$am_latency},\"fd\":{\"found\":$fd_found,\"latency_ms\":$fd_latency},\"winner\":\"$winner\"}")
    done
}

# Save results to JSON
save_json_results() {
    local total=$((TOTAL_CONTENT + TOTAL_FILES))
    local am_avg_latency=0
    local baseline_avg_latency=0

    if [[ $total -gt 0 ]]; then
        am_avg_latency=$((AMANMCP_LATENCY_SUM / total))
        baseline_avg_latency=$(( (RIPGREP_LATENCY_SUM + FD_LATENCY_SUM) / total ))
    fi

    mkdir -p "$(dirname "$RESULTS_FILE")"

    cat > "$RESULTS_FILE" << EOF
{
  "timestamp": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "summary": {
    "total_queries": $total,
    "content_queries": $TOTAL_CONTENT,
    "file_queries": $TOTAL_FILES,
    "amanmcp_wins": $AMANMCP_WINS,
    "ripgrep_wins": $RIPGREP_WINS,
    "fd_wins": $FD_WINS,
    "ties": $TIES,
    "amanmcp_win_rate": $(echo "scale=2; $AMANMCP_WINS * 100 / $total" | bc)
  },
  "latency": {
    "amanmcp_avg_ms": $am_avg_latency,
    "baseline_avg_ms": $baseline_avg_latency,
    "overhead_ms": $((am_avg_latency - baseline_avg_latency))
  },
  "results": [
    $(IFS=,; echo "${RESULTS[*]}")
  ]
}
EOF

    echo -e "\n${GREEN}Results saved to: ${RESULTS_FILE}${NC}"
}

# Generate markdown report
generate_report() {
    local total=$((TOTAL_CONTENT + TOTAL_FILES))
    local win_rate
    win_rate=$(echo "scale=1; $AMANMCP_WINS * 100 / $total" | bc)
    local am_avg=$((AMANMCP_LATENCY_SUM / total))
    local baseline_avg=$(( (RIPGREP_LATENCY_SUM + FD_LATENCY_SUM) / total ))

    mkdir -p "$(dirname "$REPORT_FILE")"

    cat > "$REPORT_FILE" << EOF
# Search Quality Comparison Report

**Generated:** $(date -u +"%Y-%m-%d %H:%M:%S UTC")
**Tool Versions:**
- amanmcp: $(amanmcp version 2>/dev/null || echo "unknown")
- ripgrep: $(rg --version | head -1)
- fd: $(fd --version 2>/dev/null || fdfind --version 2>/dev/null || echo "unknown")

---

## Summary

| Metric | Value |
|--------|-------|
| **Total Queries** | $total |
| **amanmcp Wins** | $AMANMCP_WINS |
| **ripgrep Wins** | $RIPGREP_WINS |
| **fd Wins** | $FD_WINS |
| **Ties** | $TIES |
| **amanmcp Win Rate** | ${win_rate}% |

---

## Latency Comparison

| Tool | Avg Latency |
|------|-------------|
| amanmcp | ${am_avg}ms |
| Baseline (rg/fd) | ${baseline_avg}ms |
| **Overhead** | $((am_avg - baseline_avg))ms |

---

## Success Criteria

| Criterion | Target | Actual | Status |
|-----------|--------|--------|--------|
| amanmcp win rate | >60% | ${win_rate}% | $(if (( $(echo "$win_rate > 60" | bc -l) )); then echo "PASS"; else echo "FAIL"; fi) |
| Latency overhead | <100ms | $((am_avg - baseline_avg))ms | $(if [[ $((am_avg - baseline_avg)) -lt 100 ]]; then echo "PASS"; else echo "FAIL"; fi) |

---

## Interpretation

- **amanmcp excels at**: Semantic queries, natural language questions, concept discovery
- **ripgrep excels at**: Exact string matching, regex patterns, known identifiers
- **fd excels at**: File path patterns, glob matching

The comparison validates that amanmcp provides value for semantic search while traditional tools remain better for exact pattern matching.

---

## Detailed Results

See \`docs/dogfooding/comparison-results.json\` for per-query results.
EOF

    echo -e "${GREEN}Report saved to: ${REPORT_FILE}${NC}"
}

# Print summary
print_summary() {
    local total=$((TOTAL_CONTENT + TOTAL_FILES))

    echo -e "\n${BOLD}${CYAN}=== Summary ===${NC}\n"
    echo -e "  Total queries:   $total"
    echo -e "  amanmcp wins:    ${GREEN}$AMANMCP_WINS${NC}"
    echo -e "  ripgrep wins:    ${YELLOW}$RIPGREP_WINS${NC}"
    echo -e "  fd wins:         ${YELLOW}$FD_WINS${NC}"
    echo -e "  Ties:            ${BLUE}$TIES${NC}"

    if [[ $total -gt 0 ]]; then
        local win_rate
        win_rate=$(echo "scale=1; $AMANMCP_WINS * 100 / $total" | bc)
        echo -e "\n  ${BOLD}amanmcp win rate: ${win_rate}%${NC}"

        local am_avg=$((AMANMCP_LATENCY_SUM / total))
        local baseline_avg=$(( (RIPGREP_LATENCY_SUM + FD_LATENCY_SUM) / total ))
        echo -e "  Avg latency: amanmcp=${am_avg}ms, baseline=${baseline_avg}ms"
    fi
}

# Main
main() {
    echo -e "${BOLD}${CYAN}Search Quality Comparison: amanmcp vs Claude Code Tools${NC}"
    echo -e "================================================================\n"

    check_deps

    if [[ ! -f "$QUERIES_FILE" ]]; then
        echo -e "${RED}Error: Query file not found: ${QUERIES_FILE}${NC}"
        exit 1
    fi

    local mode="${1:-all}"

    case "$mode" in
        --content)
            run_content_comparison
            ;;
        --files)
            run_file_comparison
            ;;
        --all|*)
            run_content_comparison
            run_file_comparison
            ;;
    esac

    print_summary
    save_json_results
    generate_report

    echo -e "\n${GREEN}Comparison complete!${NC}"
}

main "$@"
