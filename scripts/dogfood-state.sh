#!/bin/bash
# Dogfooding State Machine CLI
# Manages the dogfooding workflow state for session continuity
#
# Usage: ./scripts/dogfood-state.sh <command> [args]
#
# Commands:
#   status    - Show current state and progress
#   start     - Initialize dogfooding (NOT_STARTED -> PHASE_1_SETUP)
#   pause     - Pause current work (any -> PAUSED)
#   resume    - Resume from PAUSED state
#   next      - Show next task based on current state
#   complete  - Mark current phase complete, advance state
#   reset     - Reset to NOT_STARTED (with confirmation)
#
# State file: .aman-pm/validation/state.json

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color
BOLD='\033[1m'

# Find project root (where state file lives)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
STATE_FILE="${PROJECT_ROOT}/.aman-pm/validation/state.json"

# State constants
declare -a STATES=(
    "NOT_STARTED"
    "PHASE_1_SETUP"
    "PHASE_1_COMPLETE"
    "PHASE_2_BASELINE"
    "PHASE_2_COMPLETE"
    "PHASE_3_DAILY"
    "PHASE_4_AUDIT"
    "PHASE_5_ROADMAP"
    "COMPLETE"
    "PAUSED"
)

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
        echo -e "${YELLOW}State file not found. Creating initial state...${NC}"
        mkdir -p "$(dirname "$STATE_FILE")"
        cat > "$STATE_FILE" << 'EOF'
{
  "current_state": "NOT_STARTED",
  "previous_state": null,
  "last_updated": null,
  "session_id": null,
  "phases": {
    "phase_1": {
      "status": "pending",
      "tasks": {
        "install_fresh_macos": false,
        "install_fresh_linux": false,
        "configure_mcp": false,
        "verify_index": false,
        "verify_serve": false
      },
      "started_at": null,
      "completed_at": null
    },
    "phase_2": {
      "status": "pending",
      "tier1_passed": 0,
      "tier1_total": 12,
      "tier2_passed": 0,
      "tier2_total": 4,
      "negative_passed": 0,
      "negative_total": 4,
      "results_file": null,
      "started_at": null,
      "completed_at": null
    },
    "phase_3": {
      "status": "pending",
      "log_entries": 0,
      "issues_found": 0,
      "started_at": null
    },
    "phase_4": {
      "status": "pending",
      "docs_audited": [],
      "drift_items": 0,
      "started_at": null,
      "completed_at": null
    },
    "phase_5": {
      "status": "pending",
      "roadmap_items": 0,
      "started_at": null,
      "completed_at": null
    }
  },
  "blockers": [],
  "notes": ""
}
EOF
        echo -e "${GREEN}Created state file at: ${STATE_FILE}${NC}"
    fi
}

# Get current state
get_state() {
    jq -r '.current_state' "$STATE_FILE"
}

# Set state with timestamp
set_state() {
    local new_state="$1"
    local current_state
    current_state=$(get_state)

    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    jq --arg new_state "$new_state" \
       --arg prev_state "$current_state" \
       --arg timestamp "$timestamp" \
       '.previous_state = $prev_state | .current_state = $new_state | .last_updated = $timestamp' \
       "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
}

# Update phase status
update_phase() {
    local phase="$1"
    local field="$2"
    local value="$3"

    jq --arg phase "$phase" \
       --arg field "$field" \
       --arg value "$value" \
       '.phases[$phase][$field] = $value' \
       "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
}

# Show status
cmd_status() {
    local current_state
    current_state=$(get_state)
    local last_updated
    last_updated=$(jq -r '.last_updated // "never"' "$STATE_FILE")
    local previous_state
    previous_state=$(jq -r '.previous_state // "none"' "$STATE_FILE")

    echo -e "${BOLD}${CYAN}=== AmanMCP Dogfooding Status ===${NC}"
    echo ""
    echo -e "${BOLD}Current State:${NC} ${GREEN}${current_state}${NC}"
    echo -e "${BOLD}Previous State:${NC} ${previous_state}"
    echo -e "${BOLD}Last Updated:${NC} ${last_updated}"
    echo ""

    # Show phase progress
    echo -e "${BOLD}Phase Progress:${NC}"
    echo ""

    # Phase 1
    local p1_status
    p1_status=$(jq -r '.phases.phase_1.status' "$STATE_FILE")
    local p1_tasks_done
    p1_tasks_done=$(jq '[.phases.phase_1.tasks | to_entries[] | select(.value == true)] | length' "$STATE_FILE")
    local p1_tasks_total
    p1_tasks_total=$(jq '[.phases.phase_1.tasks | to_entries[]] | length' "$STATE_FILE")
    echo -e "  Phase 1 (Setup):    ${p1_status} [${p1_tasks_done}/${p1_tasks_total} tasks]"

    # Phase 2
    local p2_status
    p2_status=$(jq -r '.phases.phase_2.status' "$STATE_FILE")
    local p2_t1
    p2_t1=$(jq -r '.phases.phase_2.tier1_passed' "$STATE_FILE")
    local p2_t1_total
    p2_t1_total=$(jq -r '.phases.phase_2.tier1_total' "$STATE_FILE")
    local p2_t2
    p2_t2=$(jq -r '.phases.phase_2.tier2_passed' "$STATE_FILE")
    local p2_t2_total
    p2_t2_total=$(jq -r '.phases.phase_2.tier2_total' "$STATE_FILE")
    echo -e "  Phase 2 (Baseline): ${p2_status} [Tier1: ${p2_t1}/${p2_t1_total}, Tier2: ${p2_t2}/${p2_t2_total}]"

    # Phase 3
    local p3_status
    p3_status=$(jq -r '.phases.phase_3.status' "$STATE_FILE")
    local p3_entries
    p3_entries=$(jq -r '.phases.phase_3.log_entries' "$STATE_FILE")
    local p3_issues
    p3_issues=$(jq -r '.phases.phase_3.issues_found' "$STATE_FILE")
    echo -e "  Phase 3 (Daily):    ${p3_status} [${p3_entries} log entries, ${p3_issues} issues]"

    # Phase 4
    local p4_status
    p4_status=$(jq -r '.phases.phase_4.status' "$STATE_FILE")
    local p4_audited
    p4_audited=$(jq '[.phases.phase_4.docs_audited[]] | length' "$STATE_FILE")
    local p4_drift
    p4_drift=$(jq -r '.phases.phase_4.drift_items' "$STATE_FILE")
    echo -e "  Phase 4 (Audit):    ${p4_status} [${p4_audited} docs audited, ${p4_drift} drift items]"

    # Phase 5
    local p5_status
    p5_status=$(jq -r '.phases.phase_5.status' "$STATE_FILE")
    local p5_items
    p5_items=$(jq -r '.phases.phase_5.roadmap_items' "$STATE_FILE")
    echo -e "  Phase 5 (Roadmap):  ${p5_status} [${p5_items} roadmap items]"

    echo ""

    # Show blockers if any
    local blockers
    blockers=$(jq -r '.blockers | length' "$STATE_FILE")
    if [[ "$blockers" -gt 0 ]]; then
        echo -e "${RED}${BOLD}Blockers:${NC}"
        jq -r '.blockers[]' "$STATE_FILE" | while read -r blocker; do
            echo -e "  - ${blocker}"
        done
        echo ""
    fi

    # Show notes if any
    local notes
    notes=$(jq -r '.notes' "$STATE_FILE")
    if [[ -n "$notes" && "$notes" != "" ]]; then
        echo -e "${YELLOW}${BOLD}Notes:${NC} ${notes}"
        echo ""
    fi
}

# Start dogfooding
cmd_start() {
    local current_state
    current_state=$(get_state)

    if [[ "$current_state" != "NOT_STARTED" ]]; then
        echo -e "${YELLOW}Dogfooding already started. Current state: ${current_state}${NC}"
        echo "Use 'reset' to start over, or 'resume' if paused."
        exit 1
    fi

    local session_id
    session_id=$(date +%Y%m%d%H%M%S)
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    jq --arg session_id "$session_id" \
       --arg timestamp "$timestamp" \
       '.session_id = $session_id | .phases.phase_1.status = "in_progress" | .phases.phase_1.started_at = $timestamp' \
       "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"

    set_state "PHASE_1_SETUP"

    echo -e "${GREEN}${BOLD}Dogfooding started!${NC}"
    echo -e "Session ID: ${session_id}"
    echo ""
    echo -e "${BOLD}Phase 1 Tasks:${NC}"
    echo "  1. Install on fresh macOS"
    echo "  2. Install on fresh Linux"
    echo "  3. Configure Claude Code MCP"
    echo "  4. Verify 'amanmcp index .'"
    echo "  5. Verify 'amanmcp serve'"
    echo ""
    echo -e "Run ${CYAN}./scripts/dogfood-state.sh next${NC} to see detailed next steps."
}

# Pause dogfooding
cmd_pause() {
    local current_state
    current_state=$(get_state)

    if [[ "$current_state" == "NOT_STARTED" || "$current_state" == "COMPLETE" || "$current_state" == "PAUSED" ]]; then
        echo -e "${YELLOW}Cannot pause from state: ${current_state}${NC}"
        exit 1
    fi

    set_state "PAUSED"

    echo -e "${YELLOW}${BOLD}Dogfooding paused.${NC}"
    echo -e "Previous state saved: ${current_state}"
    echo ""
    echo -e "Run ${CYAN}./scripts/dogfood-state.sh resume${NC} to continue."
}

# Resume dogfooding
cmd_resume() {
    local current_state
    current_state=$(get_state)

    if [[ "$current_state" != "PAUSED" ]]; then
        echo -e "${YELLOW}Not in PAUSED state. Current state: ${current_state}${NC}"
        exit 1
    fi

    local previous_state
    previous_state=$(jq -r '.previous_state' "$STATE_FILE")

    if [[ -z "$previous_state" || "$previous_state" == "null" ]]; then
        echo -e "${RED}No previous state to resume to.${NC}"
        exit 1
    fi

    set_state "$previous_state"

    echo -e "${GREEN}${BOLD}Resumed dogfooding.${NC}"
    echo -e "Current state: ${previous_state}"
    echo ""
    cmd_next
}

# Show next task
cmd_next() {
    local current_state
    current_state=$(get_state)

    echo -e "${BOLD}${CYAN}=== Next Steps for ${current_state} ===${NC}"
    echo ""

    case "$current_state" in
        "NOT_STARTED")
            echo "Run './scripts/dogfood-state.sh start' to begin dogfooding."
            ;;
        "PHASE_1_SETUP")
            echo -e "${BOLD}Phase 1: Setup & Installation${NC}"
            echo ""
            echo "Tasks to complete:"

            local tasks
            tasks=$(jq '.phases.phase_1.tasks' "$STATE_FILE")

            if [[ $(echo "$tasks" | jq -r '.install_fresh_macos') == "false" ]]; then
                echo -e "  ${RED}[ ]${NC} Test install.sh on fresh macOS"
                echo "      curl -sSL https://raw.githubusercontent.com/Aman-CERP/amanmcp/main/scripts/install.sh | sh"
            else
                echo -e "  ${GREEN}[x]${NC} Install on fresh macOS"
            fi

            if [[ $(echo "$tasks" | jq -r '.install_fresh_linux') == "false" ]]; then
                echo -e "  ${RED}[ ]${NC} Test install.sh on fresh Linux"
            else
                echo -e "  ${GREEN}[x]${NC} Install on fresh Linux"
            fi

            if [[ $(echo "$tasks" | jq -r '.configure_mcp') == "false" ]]; then
                echo -e "  ${RED}[ ]${NC} Configure Claude Code MCP (add to settings.json)"
            else
                echo -e "  ${GREEN}[x]${NC} Configure Claude Code MCP"
            fi

            if [[ $(echo "$tasks" | jq -r '.verify_index') == "false" ]]; then
                echo -e "  ${RED}[ ]${NC} Verify 'amanmcp index .' works"
            else
                echo -e "  ${GREEN}[x]${NC} Verify 'amanmcp index .'"
            fi

            if [[ $(echo "$tasks" | jq -r '.verify_serve') == "false" ]]; then
                echo -e "  ${RED}[ ]${NC} Verify 'amanmcp serve' works"
            else
                echo -e "  ${GREEN}[x]${NC} Verify 'amanmcp serve'"
            fi

            echo ""
            echo "Mark task complete: ./scripts/dogfood-state.sh task <task_name> done"
            echo "When all done: ./scripts/dogfood-state.sh complete"
            ;;
        "PHASE_1_COMPLETE")
            echo "Phase 1 complete. Run './scripts/dogfood-state.sh complete' to start Phase 2."
            ;;
        "PHASE_2_BASELINE")
            echo -e "${BOLD}Phase 2: Baseline Testing${NC}"
            echo ""
            echo "Run baseline queries:"
            echo "  ./scripts/dogfood-baseline.sh --all"
            echo ""
            echo "Or run tiers separately:"
            echo "  ./scripts/dogfood-baseline.sh --tier1    # 12 must-pass queries"
            echo "  ./scripts/dogfood-baseline.sh --tier2    # 4 should-pass queries"
            echo "  ./scripts/dogfood-baseline.sh --negative # 4 failure scenario tests"
            echo ""
            local t1
            t1=$(jq -r '.phases.phase_2.tier1_passed' "$STATE_FILE")
            local t2
            t2=$(jq -r '.phases.phase_2.tier2_passed' "$STATE_FILE")
            echo "Current progress: Tier1: ${t1}/12, Tier2: ${t2}/4"
            ;;
        "PHASE_2_COMPLETE")
            echo "Phase 2 complete. Run './scripts/dogfood-state.sh complete' to start Phase 3."
            ;;
        "PHASE_3_DAILY")
            echo -e "${BOLD}Phase 3: Daily Dogfooding${NC}"
            echo ""
            echo "Use AmanMCP MCP tools during development:"
            echo "  - search, search_code, search_docs, index_status"
            echo ""
            echo "Log issues to: .aman-pm/validation/log.md"
            echo "  ./scripts/dogfood-state.sh log \"<issue description>\""
            echo ""
            local entries
            entries=$(jq -r '.phases.phase_3.log_entries' "$STATE_FILE")
            local issues
            issues=$(jq -r '.phases.phase_3.issues_found' "$STATE_FILE")
            echo "Current: ${entries} log entries, ${issues} issues found"
            ;;
        "PHASE_4_AUDIT")
            echo -e "${BOLD}Phase 4: Documentation Audit${NC}"
            echo ""
            echo "Run drift detection:"
            echo "  ./scripts/drift-check.sh --all"
            echo ""
            echo "Or check specific areas:"
            echo "  ./scripts/drift-check.sh --area embedder"
            echo "  ./scripts/drift-check.sh --area config"
            echo "  ./scripts/drift-check.sh --area errors"
            echo "  ./scripts/drift-check.sh --area cli"
            ;;
        "PHASE_5_ROADMAP")
            echo -e "${BOLD}Phase 5: Improvement Roadmap${NC}"
            echo ""
            echo "Review findings from phases 3-4 and compile v1.1 roadmap."
            echo ""
            echo "Files to review:"
            echo "  - .aman-pm/validation/log.md"
            echo "  - .aman-pm/validation/drift-report.md"
            echo "  - .aman-pm/validation/baseline-results.json"
            ;;
        "COMPLETE")
            echo -e "${GREEN}Dogfooding complete!${NC}"
            echo ""
            echo "All phases finished. Review final reports:"
            echo "  - .aman-pm/validation/validation-report.md"
            echo "  - .aman-pm/validation/log.md"
            ;;
        "PAUSED")
            echo "Dogfooding is paused. Run './scripts/dogfood-state.sh resume' to continue."
            ;;
        *)
            echo "Unknown state: ${current_state}"
            ;;
    esac
}

# Complete current phase
cmd_complete() {
    local current_state
    current_state=$(get_state)
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    case "$current_state" in
        "PHASE_1_SETUP")
            jq --arg timestamp "$timestamp" \
               '.phases.phase_1.status = "complete" | .phases.phase_1.completed_at = $timestamp | .phases.phase_2.status = "in_progress" | .phases.phase_2.started_at = $timestamp' \
               "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
            set_state "PHASE_2_BASELINE"
            echo -e "${GREEN}Phase 1 complete. Now in PHASE_2_BASELINE.${NC}"
            ;;
        "PHASE_1_COMPLETE"|"PHASE_2_BASELINE")
            jq --arg timestamp "$timestamp" \
               '.phases.phase_2.status = "complete" | .phases.phase_2.completed_at = $timestamp | .phases.phase_3.status = "in_progress" | .phases.phase_3.started_at = $timestamp' \
               "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
            set_state "PHASE_3_DAILY"
            echo -e "${GREEN}Phase 2 complete. Now in PHASE_3_DAILY.${NC}"
            ;;
        "PHASE_2_COMPLETE"|"PHASE_3_DAILY")
            jq --arg timestamp "$timestamp" \
               '.phases.phase_3.status = "complete" | .phases.phase_4.status = "in_progress" | .phases.phase_4.started_at = $timestamp' \
               "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
            set_state "PHASE_4_AUDIT"
            echo -e "${GREEN}Phase 3 complete. Now in PHASE_4_AUDIT.${NC}"
            ;;
        "PHASE_4_AUDIT")
            jq --arg timestamp "$timestamp" \
               '.phases.phase_4.status = "complete" | .phases.phase_4.completed_at = $timestamp | .phases.phase_5.status = "in_progress" | .phases.phase_5.started_at = $timestamp' \
               "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
            set_state "PHASE_5_ROADMAP"
            echo -e "${GREEN}Phase 4 complete. Now in PHASE_5_ROADMAP.${NC}"
            ;;
        "PHASE_5_ROADMAP")
            jq --arg timestamp "$timestamp" \
               '.phases.phase_5.status = "complete" | .phases.phase_5.completed_at = $timestamp' \
               "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
            set_state "COMPLETE"
            echo -e "${GREEN}${BOLD}Dogfooding complete!${NC}"
            echo ""
            cmd_status
            ;;
        *)
            echo -e "${YELLOW}Cannot complete from state: ${current_state}${NC}"
            exit 1
            ;;
    esac
}

# Reset state
cmd_reset() {
    echo -e "${RED}${BOLD}WARNING: This will reset all dogfooding progress!${NC}"
    read -p "Are you sure? (yes/no): " confirm

    if [[ "$confirm" != "yes" ]]; then
        echo "Reset cancelled."
        exit 0
    fi

    cat > "$STATE_FILE" << 'EOF'
{
  "current_state": "NOT_STARTED",
  "previous_state": null,
  "last_updated": null,
  "session_id": null,
  "phases": {
    "phase_1": {
      "status": "pending",
      "tasks": {
        "install_fresh_macos": false,
        "install_fresh_linux": false,
        "configure_mcp": false,
        "verify_index": false,
        "verify_serve": false
      },
      "started_at": null,
      "completed_at": null
    },
    "phase_2": {
      "status": "pending",
      "tier1_passed": 0,
      "tier1_total": 12,
      "tier2_passed": 0,
      "tier2_total": 4,
      "negative_passed": 0,
      "negative_total": 4,
      "results_file": null,
      "started_at": null,
      "completed_at": null
    },
    "phase_3": {
      "status": "pending",
      "log_entries": 0,
      "issues_found": 0,
      "started_at": null
    },
    "phase_4": {
      "status": "pending",
      "docs_audited": [],
      "drift_items": 0,
      "started_at": null,
      "completed_at": null
    },
    "phase_5": {
      "status": "pending",
      "roadmap_items": 0,
      "started_at": null,
      "completed_at": null
    }
  },
  "blockers": [],
  "notes": ""
}
EOF

    echo -e "${GREEN}State reset to NOT_STARTED.${NC}"
}

# Mark a Phase 1 task complete
cmd_task() {
    local task_name="$1"
    local action="$2"

    if [[ -z "$task_name" ]]; then
        echo "Usage: ./scripts/dogfood-state.sh task <task_name> done"
        echo ""
        echo "Task names:"
        echo "  install_fresh_macos"
        echo "  install_fresh_linux"
        echo "  configure_mcp"
        echo "  verify_index"
        echo "  verify_serve"
        exit 1
    fi

    if [[ "$action" != "done" ]]; then
        echo "Usage: ./scripts/dogfood-state.sh task <task_name> done"
        exit 1
    fi

    jq --arg task "$task_name" \
       '.phases.phase_1.tasks[$task] = true' \
       "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"

    echo -e "${GREEN}Task '${task_name}' marked complete.${NC}"
}

# Add a note
cmd_note() {
    local note="$1"

    if [[ -z "$note" ]]; then
        echo "Usage: ./scripts/dogfood-state.sh note \"<note text>\""
        exit 1
    fi

    jq --arg note "$note" '.notes = $note' "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
    echo -e "${GREEN}Note saved.${NC}"
}

# Add a blocker
cmd_blocker() {
    local action="$1"
    local text="$2"

    case "$action" in
        "add")
            if [[ -z "$text" ]]; then
                echo "Usage: ./scripts/dogfood-state.sh blocker add \"<blocker text>\""
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
            echo "Usage: ./scripts/dogfood-state.sh blocker add|clear [text]"
            exit 1
            ;;
    esac
}

# Show help
cmd_help() {
    echo -e "${BOLD}${CYAN}AmanMCP Dogfooding State Machine${NC}"
    echo ""
    echo "Usage: ./scripts/dogfood-state.sh <command> [args]"
    echo ""
    echo "Commands:"
    echo "  status              Show current state and progress"
    echo "  start               Begin dogfooding (NOT_STARTED -> PHASE_1_SETUP)"
    echo "  pause               Pause current work"
    echo "  resume              Resume from PAUSED state"
    echo "  next                Show next tasks for current state"
    echo "  complete            Mark current phase complete, advance"
    echo "  reset               Reset to NOT_STARTED (with confirmation)"
    echo "  task <name> done    Mark Phase 1 task complete"
    echo "  note \"<text>\"       Set a note"
    echo "  blocker add|clear   Manage blockers"
    echo "  help                Show this help"
    echo ""
    echo "State File: ${STATE_FILE}"
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
        "pause")
            cmd_pause
            ;;
        "resume")
            cmd_resume
            ;;
        "next")
            cmd_next
            ;;
        "complete")
            cmd_complete
            ;;
        "reset")
            cmd_reset
            ;;
        "task")
            cmd_task "$@"
            ;;
        "note")
            cmd_note "$@"
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
