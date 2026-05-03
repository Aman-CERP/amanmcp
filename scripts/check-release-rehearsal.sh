#!/usr/bin/env bash
# Check that the current commit has a recent green release rehearsal report.

set -euo pipefail

DEFAULT_REPORTS_DIR=".aman-pm/validation/release-rehearsal"

COMMIT=""
MAX_AGE_HOURS="${AMANMCP_REHEARSE_MAX_AGE_HOURS:-24}"
REPORTS_DIR="${AMANMCP_REHEARSE_REPORTS_DIR:-$DEFAULT_REPORTS_DIR}"

usage() {
    cat <<'EOF'
Usage: ./scripts/check-release-rehearsal.sh [OPTIONS]

Options:
  --commit SHA          Commit SHA to check (default: git rev-parse HEAD)
  --max-age-hours N     Maximum report age in hours (default: 24)
  --reports-dir PATH    Rehearsal reports directory
  -h, --help            Show this help

The guard requires a report containing:
  status: green
  commit: <SHA>
EOF
}

fail() {
    echo "release-guard: $1" >&2
    exit 1
}

file_mtime_epoch() {
    local path="$1"

    if stat -f %m "$path" >/dev/null 2>&1; then
        stat -f %m "$path"
        return
    fi

    stat -c %Y "$path"
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --commit)
            COMMIT="${2:-}"
            shift 2
            ;;
        --max-age-hours)
            MAX_AGE_HOURS="${2:-}"
            shift 2
            ;;
        --reports-dir)
            REPORTS_DIR="${2:-}"
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            fail "unknown option: $1"
            ;;
    esac
done

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || fail "not inside a git repository"
cd "$repo_root"

if [[ -z "$COMMIT" ]]; then
    COMMIT="$(git rev-parse HEAD)"
fi

if ! [[ "$MAX_AGE_HOURS" =~ ^[0-9]+$ ]]; then
    fail "--max-age-hours must be a non-negative integer"
fi

if [[ ! -d "$REPORTS_DIR" ]]; then
    fail "missing rehearsal report for commit ${COMMIT}; reports directory does not exist: ${REPORTS_DIR}"
fi

now_epoch="$(date -u +%s)"
max_age_seconds=$((MAX_AGE_HOURS * 3600))
newest_stale=""
newest_stale_age=""

shopt -s nullglob
for report in "$REPORTS_DIR"/*.md; do
    if ! grep -Eq '^status:[[:space:]]+green$' "$report"; then
        continue
    fi
    if ! grep -Eq "^commit:[[:space:]]+${COMMIT}$" "$report"; then
        continue
    fi

    mtime_epoch="$(file_mtime_epoch "$report")"
    age_seconds=$((now_epoch - mtime_epoch))
    if (( age_seconds <= max_age_seconds )); then
        echo "release-guard: green rehearsal report found"
        echo "  commit: ${COMMIT}"
        echo "  report: ${report}"
        echo "  age_seconds: ${age_seconds}"
        exit 0
    fi

    if [[ -z "$newest_stale_age" ]] || (( age_seconds < newest_stale_age )); then
        newest_stale="$report"
        newest_stale_age="$age_seconds"
    fi
done
shopt -u nullglob

if [[ -n "$newest_stale" ]]; then
    fail "stale rehearsal report for commit ${COMMIT}; newest=${newest_stale} age_seconds=${newest_stale_age} max_age_seconds=${max_age_seconds}"
fi

fail "missing green rehearsal report for commit ${COMMIT} in ${REPORTS_DIR}"
