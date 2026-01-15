#!/bin/bash
# DEPRECATED: This script is superseded by the AI-native PM system
# Work items are now managed in .aman-pm/backlog/ as individual markdown files
# Use /aman-pm skill commands instead
#
# Roadmap Execution State Machine CLI (LEGACY)
# Manages multi-session execution of strategic roadmap items
#
# Usage: ./scripts/roadmap-exec.sh <command> [args]
#
# Commands:
#   status              Show current execution state
#   start               Start/resume execution from last state
#   item <id> <action>  Manage item state (start, doc-complete, ci-passed, validated)
#   phase <n> complete  Advance to next phase
#   pause [notes]       Pause with optional notes
#   baseline            Run dogfood baseline
#   next                Show next action for current state
#   report              Generate progress report
#   help                Show this help
#
# State file: docs/roadmap/execution-state.json

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color
BOLD='\033[1m'

# Find project root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
STATE_FILE="${PROJECT_ROOT}/archive/legacy/execution-state-v1.json"
DOGFOOD_BASELINE="${PROJECT_ROOT}/scripts/dogfood-baseline.sh"
ROADMAP_FILE="${PROJECT_ROOT}/archive/roadmap/strategic-improvements-2026.md"

# Check dependencies
check_deps() {
    if ! command -v jq &> /dev/null; then
        echo -e "${RED}Error: jq is required but not installed.${NC}"
        echo "Install with: brew install jq (macOS) or apt-get install jq (Linux)"
        exit 1
    fi
}

# Ensure state file exists
ensure_state_file() {
    if [[ ! -f "$STATE_FILE" ]]; then
        echo -e "${RED}Error: State file not found: ${STATE_FILE}${NC}"
        echo "Run 'make' or create the file manually from the plan."
        exit 1
    fi
}

# Get current timestamp
get_timestamp() {
    date -u +"%Y-%m-%dT%H:%M:%SZ"
}

# Get session ID
get_session_id() {
    date +%Y%m%d%H%M%S
}

# Get current phase
get_current_phase() {
    jq -r '.current_phase' "$STATE_FILE"
}

# Get current item
get_current_item() {
    jq -r '.current_item // "none"' "$STATE_FILE"
}

# Get item status
get_item_status() {
    local item_id="$1"
    jq -r ".items[\"$item_id\"].status // \"unknown\"" "$STATE_FILE"
}

# Get item name
get_item_name() {
    local item_id="$1"
    jq -r ".items[\"$item_id\"].name // \"Unknown\"" "$STATE_FILE"
}

# Update state file
update_state() {
    local filter="$1"
    local timestamp
    timestamp=$(get_timestamp)

    jq --arg timestamp "$timestamp" "$filter | .last_updated = \$timestamp" \
        "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
}

# Add session log entry
add_session_log() {
    local item_id="$1"
    local action="$2"
    local notes="$3"
    local timestamp
    timestamp=$(get_timestamp)
    local session_id
    session_id=$(jq -r '.session_id // "unknown"' "$STATE_FILE")

    local log_entry="{\"timestamp\": \"$timestamp\", \"session_id\": \"$session_id\", \"action\": \"$action\", \"notes\": \"$notes\"}"

    jq --arg item "$item_id" --argjson entry "$log_entry" \
        '.items[$item].session_log += [$entry]' \
        "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
}

# Show status
cmd_status() {
    local current_phase
    current_phase=$(get_current_phase)
    local current_item
    current_item=$(get_current_item)
    local last_updated
    last_updated=$(jq -r '.last_updated // "never"' "$STATE_FILE")
    local session_id
    session_id=$(jq -r '.session_id // "none"' "$STATE_FILE")

    echo -e "${BOLD}${CYAN}=== AmanMCP Roadmap Execution ===${NC}"
    echo ""
    echo -e "${BOLD}Session ID:${NC}    ${session_id}"
    echo -e "${BOLD}Last Updated:${NC}  ${last_updated}"
    echo ""

    # Current work
    local phase_name
    phase_name=$(jq -r ".phases[\"$current_phase\"].name" "$STATE_FILE")
    echo -e "${BOLD}Current Phase:${NC} ${GREEN}Phase ${current_phase}${NC} - ${phase_name}"

    if [[ "$current_item" != "none" && "$current_item" != "null" ]]; then
        local item_name
        item_name=$(get_item_name "$current_item")
        local item_status
        item_status=$(get_item_status "$current_item")
        echo -e "${BOLD}Current Item:${NC}  ${GREEN}${current_item}${NC} - ${item_name}"
        echo -e "${BOLD}Item Status:${NC}   ${item_status}"
    fi
    echo ""

    # Progress metrics
    local total
    total=$(jq -r '.metrics.total_items' "$STATE_FILE")
    local completed
    completed=$(jq -r '.metrics.completed_items' "$STATE_FILE")
    local in_progress
    in_progress=$(jq -r '.metrics.in_progress_items' "$STATE_FILE")
    local tier1
    tier1=$(jq -r '.metrics.dogfood_tier1_current' "$STATE_FILE")
    local tier1_target
    tier1_target=$(jq -r '.metrics.dogfood_tier1_target' "$STATE_FILE")

    echo -e "${BOLD}Progress:${NC}"
    echo -e "  Items: ${completed}/${total} complete, ${in_progress} in progress"
    echo -e "  Dogfood Tier 1: ${tier1} / ${tier1_target} target"
    echo ""

    # Phase overview
    echo -e "${BOLD}Phase Overview:${NC}"
    for phase in 0 1 2 3 4 5; do
        local p_status
        p_status=$(jq -r ".phases[\"$phase\"].status" "$STATE_FILE")
        local p_name
        p_name=$(jq -r ".phases[\"$phase\"].name" "$STATE_FILE")
        local p_items
        p_items=$(jq -r ".phases[\"$phase\"].items | length" "$STATE_FILE")

        local status_color="${NC}"
        case "$p_status" in
            "complete") status_color="${GREEN}" ;;
            "in_progress") status_color="${YELLOW}" ;;
            "pending") status_color="${NC}" ;;
        esac

        printf "  Phase %d: %-30s %b%-12s%b (%d items)\n" \
            "$phase" "$p_name" "$status_color" "$p_status" "$NC" "$p_items"
    done
    echo ""

    # Blockers
    local blockers
    blockers=$(jq -r '.blockers | length' "$STATE_FILE")
    if [[ "$blockers" -gt 0 ]]; then
        echo -e "${RED}${BOLD}Blockers:${NC}"
        jq -r '.blockers[]' "$STATE_FILE" | while read -r blocker; do
            echo -e "  - ${blocker}"
        done
        echo ""
    fi

    # Notes
    local notes
    notes=$(jq -r '.notes' "$STATE_FILE")
    if [[ -n "$notes" && "$notes" != "" && "$notes" != "null" ]]; then
        echo -e "${YELLOW}${BOLD}Notes:${NC}"
        echo "$notes" | sed 's/^/  /'
        echo ""
    fi
}

# Start/resume execution
cmd_start() {
    local session_id
    session_id=$(get_session_id)
    local timestamp
    timestamp=$(get_timestamp)

    update_state ".session_id = \"$session_id\""

    echo -e "${GREEN}${BOLD}Session started: ${session_id}${NC}"
    echo ""

    # Show current state
    cmd_status

    # Show last session notes if any
    local current_item
    current_item=$(get_current_item)
    if [[ "$current_item" != "none" && "$current_item" != "null" ]]; then
        local last_log
        last_log=$(jq -r ".items[\"$current_item\"].session_log | last // empty" "$STATE_FILE")
        if [[ -n "$last_log" && "$last_log" != "null" ]]; then
            echo -e "${YELLOW}${BOLD}Last session notes:${NC}"
            echo "$last_log" | jq -r '.notes' | sed 's/^/  /'
            echo ""
        fi
    fi

    cmd_next
}

# Show next action
cmd_next() {
    local current_phase
    current_phase=$(get_current_phase)
    local current_item
    current_item=$(get_current_item)

    echo -e "${BOLD}${CYAN}=== Next Actions ===${NC}"
    echo ""

    if [[ "$current_item" == "none" || "$current_item" == "null" ]]; then
        # No current item - suggest starting one
        local phase_items
        phase_items=$(jq -r ".phases[\"$current_phase\"].items[]" "$STATE_FILE")

        echo "No item in progress. Available items in Phase ${current_phase}:"
        echo ""

        for item_id in $phase_items; do
            local status
            status=$(get_item_status "$item_id")
            local name
            name=$(get_item_name "$item_id")
            local priority
            priority=$(jq -r ".items[\"$item_id\"].priority // \"P?\"" "$STATE_FILE")

            if [[ "$status" == "pending" ]]; then
                echo -e "  ${YELLOW}[ ]${NC} ${item_id}: ${name} (${priority})"
            elif [[ "$status" == "complete" ]]; then
                echo -e "  ${GREEN}[x]${NC} ${item_id}: ${name}"
            else
                echo -e "  ${BLUE}[~]${NC} ${item_id}: ${name} (${status})"
            fi
        done

        echo ""
        echo -e "Start an item: ${CYAN}./scripts/roadmap-exec.sh item <id> start${NC}"
    else
        # Show next steps for current item
        local status
        status=$(get_item_status "$current_item")
        local name
        name=$(get_item_name "$current_item")
        local needs_adr
        needs_adr=$(jq -r ".items[\"$current_item\"].documentation.needs_adr // false" "$STATE_FILE")
        local needs_spec
        needs_spec=$(jq -r ".items[\"$current_item\"].documentation.needs_spec // false" "$STATE_FILE")

        echo -e "Current: ${GREEN}${current_item}${NC} - ${name}"
        echo -e "Status: ${status}"
        echo ""

        case "$status" in
            "pending")
                echo "Start this item:"
                echo -e "  ${CYAN}./scripts/roadmap-exec.sh item ${current_item} start${NC}"
                ;;
            "doc_pending")
                echo "Documentation needed:"
                if [[ "$needs_adr" == "true" ]]; then
                    local adr_id
                    adr_id=$(jq -r ".items[\"$current_item\"].documentation.adr_id" "$STATE_FILE")
                    echo -e "  ${RED}[ ]${NC} Write ADR: docs/decisions/${adr_id}-*.md"
                fi
                if [[ "$needs_spec" == "true" ]]; then
                    local spec_id
                    spec_id=$(jq -r ".items[\"$current_item\"].documentation.spec_id" "$STATE_FILE")
                    echo -e "  ${RED}[ ]${NC} Write Spec: docs/specs/features/${spec_id}-*.md"
                fi
                echo ""
                echo "When done:"
                echo -e "  ${CYAN}./scripts/roadmap-exec.sh item ${current_item} doc-complete${NC}"
                ;;
            "implementing")
                echo "Implementation in progress. Next steps:"
                echo "  1. Follow TDD: Write failing test (RED)"
                echo "  2. Implement minimum code (GREEN)"
                echo "  3. Refactor while green (REFACTOR)"
                echo "  4. Run: make ci-check"
                echo ""
                echo "When CI passes:"
                echo -e "  ${CYAN}./scripts/roadmap-exec.sh item ${current_item} ci-passed${NC}"
                ;;
            "validating")
                echo "Validation in progress. Next steps:"
                echo "  1. Run dogfood baseline: ./scripts/dogfood-baseline.sh --tier1"
                echo "  2. Verify relevant queries pass or improve"
                echo "  3. Add changelog entry: docs/changelog/unreleased.md"
                echo ""
                echo "When validated:"
                echo -e "  ${CYAN}./scripts/roadmap-exec.sh item ${current_item} validated${NC}"
                ;;
            "complete")
                echo "Item complete! Start next item or advance phase."
                ;;
            *)
                echo "Unknown status: ${status}"
                ;;
        esac
    fi
}

# Manage item state
cmd_item() {
    local item_id="$1"
    local action="$2"
    shift 2 || true
    local notes="$*"

    if [[ -z "$item_id" || -z "$action" ]]; then
        echo "Usage: ./scripts/roadmap-exec.sh item <id> <action> [notes]"
        echo ""
        echo "Actions:"
        echo "  start        - Begin work on item"
        echo "  doc-complete - Mark documentation done"
        echo "  ci-passed    - Mark CI check passed"
        echo "  validated    - Mark item complete"
        exit 1
    fi

    # Check item exists
    local item_exists
    item_exists=$(jq -r ".items[\"$item_id\"] // \"null\"" "$STATE_FILE")
    if [[ "$item_exists" == "null" ]]; then
        echo -e "${RED}Error: Unknown item: ${item_id}${NC}"
        exit 1
    fi

    local current_status
    current_status=$(get_item_status "$item_id")
    local timestamp
    timestamp=$(get_timestamp)
    local item_name
    item_name=$(get_item_name "$item_id")

    case "$action" in
        "start")
            local needs_adr
            needs_adr=$(jq -r ".items[\"$item_id\"].documentation.needs_adr // false" "$STATE_FILE")
            local needs_spec
            needs_spec=$(jq -r ".items[\"$item_id\"].documentation.needs_spec // false" "$STATE_FILE")

            local new_status="implementing"
            if [[ "$needs_adr" == "true" || "$needs_spec" == "true" ]]; then
                new_status="doc_pending"
            fi

            update_state ".items[\"$item_id\"].status = \"$new_status\" | .items[\"$item_id\"].implementation.started_at = \"$timestamp\" | .current_item = \"$item_id\""
            add_session_log "$item_id" "started" "${notes:-Starting work}"

            echo -e "${GREEN}Started: ${item_id} - ${item_name}${NC}"
            echo -e "Status: ${new_status}"

            if [[ "$new_status" == "doc_pending" ]]; then
                echo ""
                echo "Documentation required first:"
                [[ "$needs_adr" == "true" ]] && echo "  - ADR needed"
                [[ "$needs_spec" == "true" ]] && echo "  - Feature spec needed"
            fi
            ;;
        "doc-complete")
            if [[ "$current_status" != "doc_pending" ]]; then
                echo -e "${YELLOW}Warning: Item not in doc_pending state (current: ${current_status})${NC}"
            fi

            update_state ".items[\"$item_id\"].status = \"implementing\" | .items[\"$item_id\"].documentation.adr_status = \"complete\" | .items[\"$item_id\"].documentation.spec_status = \"complete\""
            add_session_log "$item_id" "doc_complete" "${notes:-Documentation complete}"

            echo -e "${GREEN}Documentation complete for: ${item_id}${NC}"
            echo "Now proceed with TDD implementation."
            ;;
        "ci-passed")
            if [[ "$current_status" != "implementing" ]]; then
                echo -e "${YELLOW}Warning: Item not in implementing state (current: ${current_status})${NC}"
            fi

            update_state ".items[\"$item_id\"].status = \"validating\" | .items[\"$item_id\"].validation.ci_check_passed = true"
            add_session_log "$item_id" "ci_passed" "${notes:-CI check passed}"

            echo -e "${GREEN}CI passed for: ${item_id}${NC}"
            echo "Now validate with dogfood baseline and add changelog entry."
            ;;
        "validated")
            update_state ".items[\"$item_id\"].status = \"complete\" | .items[\"$item_id\"].implementation.completed_at = \"$timestamp\" | .items[\"$item_id\"].validation.validated_at = \"$timestamp\" | .items[\"$item_id\"].validation.changelog_entry_added = true | .metrics.completed_items += 1 | .metrics.in_progress_items -= 1 | .current_item = null"
            add_session_log "$item_id" "validated" "${notes:-Item validated and complete}"

            echo -e "${GREEN}${BOLD}Item validated: ${item_id} - ${item_name}${NC}"
            echo ""
            echo "Don't forget to:"
            echo "  - Update roadmap checkbox in strategic-improvements-2026.md"
            echo "  - Verify changelog entry in docs/changelog/unreleased.md"
            ;;
        *)
            echo -e "${RED}Unknown action: ${action}${NC}"
            echo "Valid actions: start, doc-complete, ci-passed, validated"
            exit 1
            ;;
    esac
}

# Pause with notes
cmd_pause() {
    local notes="$*"

    if [[ -z "$notes" ]]; then
        echo "Usage: ./scripts/roadmap-exec.sh pause \"<detailed notes>\""
        echo ""
        echo "Capture:"
        echo "  - Current file being edited"
        echo "  - Test status (written/passing/failing)"
        echo "  - Implementation percentage"
        echo "  - Specific next steps"
        echo "  - Any blockers"
        exit 1
    fi

    local current_item
    current_item=$(get_current_item)

    if [[ "$current_item" != "none" && "$current_item" != "null" ]]; then
        add_session_log "$current_item" "paused" "$notes"
    fi

    update_state ".notes = \"$notes\""

    echo -e "${YELLOW}${BOLD}Session paused.${NC}"
    echo ""
    echo "Notes saved. Resume with:"
    echo -e "  ${CYAN}./scripts/roadmap-exec.sh start${NC}"
}

# Complete current phase
cmd_phase_complete() {
    local phase="$1"

    if [[ -z "$phase" ]]; then
        echo "Usage: ./scripts/roadmap-exec.sh phase <n> complete"
        exit 1
    fi

    local current_phase
    current_phase=$(get_current_phase)

    if [[ "$phase" != "$current_phase" ]]; then
        echo -e "${RED}Error: Can only complete current phase (${current_phase})${NC}"
        exit 1
    fi

    # Check all items complete
    local pending
    pending=$(jq -r ".phases[\"$phase\"].items[] as \$id | .items[\$id].status | select(. != \"complete\")" "$STATE_FILE" | wc -l | tr -d ' ')

    if [[ "$pending" -gt 0 ]]; then
        echo -e "${YELLOW}Warning: ${pending} items not complete in Phase ${phase}${NC}"
        echo "Complete all items before advancing phase."
        echo ""
        jq -r ".phases[\"$phase\"].items[] as \$id | .items[\$id] | select(.status != \"complete\") | \"\(.id): \(.status)\"" "$STATE_FILE"
        exit 1
    fi

    local timestamp
    timestamp=$(get_timestamp)
    local next_phase=$((phase + 1))

    update_state ".phases[\"$phase\"].status = \"complete\" | .phases[\"$phase\"].completed_at = \"$timestamp\" | .phases[\"$next_phase\"].status = \"in_progress\" | .phases[\"$next_phase\"].started_at = \"$timestamp\" | .current_phase = $next_phase | .current_item = null"

    echo -e "${GREEN}${BOLD}Phase ${phase} complete!${NC}"
    echo ""
    echo -e "Now in ${GREEN}Phase ${next_phase}${NC}: $(jq -r ".phases[\"$next_phase\"].name" "$STATE_FILE")"
    echo ""
    echo "Run dogfood baseline to capture before state:"
    echo -e "  ${CYAN}./scripts/dogfood-baseline.sh --tier1${NC}"
}

# Run dogfood baseline
cmd_baseline() {
    echo -e "${BOLD}Running dogfood baseline...${NC}"
    echo ""

    if [[ ! -x "$DOGFOOD_BASELINE" ]]; then
        echo -e "${RED}Error: Baseline script not found or not executable: ${DOGFOOD_BASELINE}${NC}"
        exit 1
    fi

    "$DOGFOOD_BASELINE" --all

    # Parse results
    local results_file="${PROJECT_ROOT}/.aman-pm/validation/baseline-results.json"
    if [[ -f "$results_file" ]]; then
        local tier1_passed
        tier1_passed=$(jq -r '.summary.tier1.passed' "$results_file")
        local tier1_total
        tier1_total=$(jq -r '.summary.tier1.total' "$results_file")
        local pass_rate
        pass_rate=$(echo "scale=2; $tier1_passed / $tier1_total" | bc)

        update_state ".metrics.dogfood_tier1_current = $pass_rate"

        echo ""
        echo -e "${BOLD}Baseline captured:${NC} Tier 1 pass rate = ${pass_rate} (${tier1_passed}/${tier1_total})"
    fi
}

# Generate progress report
cmd_report() {
    echo -e "${BOLD}${CYAN}=== Roadmap Execution Report ===${NC}"
    echo ""

    # Overall progress
    local total
    total=$(jq -r '.metrics.total_items' "$STATE_FILE")
    local completed
    completed=$(jq -r '.metrics.completed_items' "$STATE_FILE")
    local pct
    pct=$(echo "scale=1; $completed * 100 / $total" | bc)

    echo -e "${BOLD}Overall Progress:${NC} ${completed}/${total} (${pct}%)"
    echo ""

    # Per-phase breakdown
    echo -e "${BOLD}Phase Breakdown:${NC}"
    for phase in 0 1 2 3 4 5; do
        local p_name
        p_name=$(jq -r ".phases[\"$phase\"].name" "$STATE_FILE")
        local p_status
        p_status=$(jq -r ".phases[\"$phase\"].status" "$STATE_FILE")
        local p_items
        p_items=$(jq -r ".phases[\"$phase\"].items | length" "$STATE_FILE")
        local p_complete
        p_complete=$(jq -r "[.phases[\"$phase\"].items[] as \$id | .items[\$id].status | select(. == \"complete\")] | length" "$STATE_FILE")

        printf "  Phase %d (%-30s): %d/%d complete [%s]\n" \
            "$phase" "$p_name" "$p_complete" "$p_items" "$p_status"
    done
    echo ""

    # Dogfood metrics
    echo -e "${BOLD}Dogfood Metrics:${NC}"
    local tier1
    tier1=$(jq -r '.metrics.dogfood_tier1_current' "$STATE_FILE")
    local tier1_target
    tier1_target=$(jq -r '.metrics.dogfood_tier1_target' "$STATE_FILE")
    echo "  Tier 1 Pass Rate: ${tier1} / ${tier1_target} target"
}

# Add blocker
cmd_blocker() {
    local action="$1"
    local text="$2"

    case "$action" in
        "add")
            if [[ -z "$text" ]]; then
                echo "Usage: ./scripts/roadmap-exec.sh blocker add \"<blocker text>\""
                exit 1
            fi
            jq --arg blocker "$text" '.blockers += [$blocker]' "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
            echo -e "${RED}Blocker added.${NC}"
            ;;
        "clear")
            jq '.blockers = []' "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
            echo -e "${GREEN}Blockers cleared.${NC}"
            ;;
        *)
            echo "Usage: ./scripts/roadmap-exec.sh blocker add|clear [text]"
            exit 1
            ;;
    esac
}

# Show help
cmd_help() {
    echo -e "${BOLD}${CYAN}AmanMCP Roadmap Execution CLI${NC}"
    echo ""
    echo "Usage: ./scripts/roadmap-exec.sh <command> [args]"
    echo ""
    echo "Commands:"
    echo "  status              Show current execution state"
    echo "  start               Start/resume execution from last state"
    echo "  item <id> start     Begin work on item"
    echo "  item <id> doc-complete  Mark documentation done"
    echo "  item <id> ci-passed Mark CI check passed"
    echo "  item <id> validated Mark item complete"
    echo "  pause \"<notes>\"     Pause with detailed notes"
    echo "  phase <n> complete  Advance to next phase"
    echo "  baseline            Run dogfood baseline"
    echo "  next                Show next action"
    echo "  report              Generate progress report"
    echo "  blocker add|clear   Manage blockers"
    echo "  help                Show this help"
    echo ""
    echo "State File: ${STATE_FILE}"
    echo "Roadmap: ${ROADMAP_FILE}"
}

# Main
main() {
    check_deps
    ensure_state_file

    local command="${1:-help}"
    shift || true

    case "$command" in
        "status")
            cmd_status
            ;;
        "start")
            cmd_start
            ;;
        "next")
            cmd_next
            ;;
        "item")
            cmd_item "$@"
            ;;
        "pause")
            cmd_pause "$@"
            ;;
        "phase")
            local phase="$1"
            local action="$2"
            if [[ "$action" == "complete" ]]; then
                cmd_phase_complete "$phase"
            else
                echo "Usage: ./scripts/roadmap-exec.sh phase <n> complete"
                exit 1
            fi
            ;;
        "baseline")
            cmd_baseline
            ;;
        "report")
            cmd_report
            ;;
        "blocker")
            cmd_blocker "$@"
            ;;
        "help"|"--help"|"-h")
            cmd_help
            ;;
        *)
            echo -e "${RED}Unknown command: ${command}${NC}"
            echo ""
            cmd_help
            exit 1
            ;;
    esac
}

main "$@"
