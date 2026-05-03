#!/usr/bin/env bash
# Exercise release surfaces against explicit rehearsal remotes only.

set -euo pipefail

REPORT_DIR="${AMANMCP_REHEARSE_REPORT_DIR:-.aman-pm/validation/release-rehearsal}"
PUBLIC_REPO_PATH="${AMANMCP_REHEARSE_PUBLIC_REPO:-$HOME/.local/code/amanmcp-rehearse}"
CHECK_CONFIG=false

RAW_REMOTE="${AMANMCP_REHEARSE_RAW_REMOTE:-}"
PUBLIC_MIRROR_REMOTE="${AMANMCP_REHEARSE_PUBLIC_MIRROR_REMOTE:-}"
RAW_REMOTE_RESOLVED=""
PUBLIC_MIRROR_REMOTE_RESOLVED=""
PUBLIC_ORIGIN_RESOLVED=""

REHEARSAL_TAG=""
RAW_TAG_PUSHED=false
LOCAL_TAG_CREATED=false

PHASE_NAMES=()
PHASE_STATUSES=()
PHASE_EXITS=()
PHASE_COMMANDS=()
PHASE_LOGS=()

OVERALL_STATUS="green"
FAILING_PHASE=""
FAILING_EXIT=""

usage() {
    cat <<'EOF'
Usage: ./scripts/release-rehearse.sh [OPTIONS]

Options:
  --check-config   Validate rehearsal env/remotes only; do not tag, push, or run
  -h, --help       Show this help

Required environment:
  AMANMCP_REHEARSE_RAW_REMOTE
  AMANMCP_REHEARSE_PUBLIC_MIRROR_REMOTE

Optional environment:
  AMANMCP_REHEARSE_PUBLIC_REPO   Public mirror clone path
  AMANMCP_REHEARSE_REPORT_DIR    Report directory
EOF
}

fail() {
    echo "release-rehearse: $1" >&2
    return 1
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --check-config)
            CHECK_CONFIG=true
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo "release-rehearse: unknown option: $1" >&2
            exit 2
            ;;
    esac
done

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || {
    echo "release-rehearse: not inside a git repository" >&2
    exit 1
}
cd "$repo_root"

resolve_remote_ref() {
    local repo="$1"
    local ref="$2"

    if [[ -d "$repo/.git" ]] && git -C "$repo" remote get-url "$ref" >/dev/null 2>&1; then
        git -C "$repo" remote get-url "$ref"
        return
    fi

    printf '%s\n' "$ref"
}

remote_compare_key() {
    local value="$1"

    value="${value%.git}"
    value="${value%/}"
    printf '%s\n' "$value" | tr '[:upper:]' '[:lower:]'
}

is_canonical_remote() {
    local value
    value="$(remote_compare_key "$1")"

    [[ "$value" == *"github.com:aman-cerp/amanmcp"* ]] && return 0
    [[ "$value" == *"github.com/aman-cerp/amanmcp"* ]] && return 0
    [[ "$value" == *"aman-cerp/amanmcp"* ]] && return 0

    return 1
}

validate_config() {
    if [[ -z "$RAW_REMOTE" ]]; then
        fail "missing AMANMCP_REHEARSE_RAW_REMOTE; no fallback to canonical remotes is allowed"
        return 1
    fi
    if [[ -z "$PUBLIC_MIRROR_REMOTE" ]]; then
        fail "missing AMANMCP_REHEARSE_PUBLIC_MIRROR_REMOTE; no fallback to canonical remotes is allowed"
        return 1
    fi

    RAW_REMOTE_RESOLVED="$(resolve_remote_ref "$repo_root" "$RAW_REMOTE")"
    if is_canonical_remote "$RAW_REMOTE_RESOLVED"; then
        fail "raw rehearsal remote resolves to canonical remote: ${RAW_REMOTE_RESOLVED}"
        return 1
    fi

    PUBLIC_MIRROR_REMOTE_RESOLVED="$(resolve_remote_ref "$PUBLIC_REPO_PATH" "$PUBLIC_MIRROR_REMOTE")"
    if is_canonical_remote "$PUBLIC_MIRROR_REMOTE_RESOLVED"; then
        fail "public rehearsal remote resolves to canonical remote: ${PUBLIC_MIRROR_REMOTE_RESOLVED}"
        return 1
    fi

    if [[ ! -d "$PUBLIC_REPO_PATH/.git" ]]; then
        fail "public rehearsal clone not found at ${PUBLIC_REPO_PATH}; create it once and set AMANMCP_REHEARSE_PUBLIC_REPO if needed"
        return 1
    fi

    PUBLIC_ORIGIN_RESOLVED="$(git -C "$PUBLIC_REPO_PATH" remote get-url origin 2>/dev/null || true)"
    if [[ -z "$PUBLIC_ORIGIN_RESOLVED" ]]; then
        fail "public rehearsal clone has no origin remote: ${PUBLIC_REPO_PATH}"
        return 1
    fi
    if is_canonical_remote "$PUBLIC_ORIGIN_RESOLVED"; then
        fail "public rehearsal clone origin resolves to canonical remote: ${PUBLIC_ORIGIN_RESOLVED}"
        return 1
    fi

    if [[ "$(remote_compare_key "$PUBLIC_ORIGIN_RESOLVED")" != "$(remote_compare_key "$PUBLIC_MIRROR_REMOTE_RESOLVED")" ]]; then
        fail "AMANMCP_REHEARSE_PUBLIC_MIRROR_REMOTE must match ${PUBLIC_REPO_PATH} origin; origin=${PUBLIC_ORIGIN_RESOLVED} configured=${PUBLIC_MIRROR_REMOTE_RESOLVED}"
        return 1
    fi
}

if [[ "$CHECK_CONFIG" == "true" ]]; then
    validate_config
    echo "release-rehearse: config check passed"
    exit 0
fi

mkdir -p "$REPORT_DIR"
temp_dir="$(mktemp -d)"
logs_dir="${temp_dir}/logs"
mkdir -p "$logs_dir"

started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
timestamp="$(date -u +%Y%m%d%H%M%S)"
commit_sha="$(git rev-parse HEAD)"
short_commit="$(git rev-parse --short HEAD)"
REHEARSAL_TAG="v0.0.0-rehearse-${timestamp}"
report_path="${REPORT_DIR}/${commit_sha}-${timestamp}.md"

record_phase() {
    local phase="$1"
    local status="$2"
    local exit_code="$3"
    local command="$4"
    local log_file="$5"

    PHASE_NAMES+=("$phase")
    PHASE_STATUSES+=("$status")
    PHASE_EXITS+=("$exit_code")
    PHASE_COMMANDS+=("$command")
    PHASE_LOGS+=("$log_file")

    if [[ "$status" != "pass" && "$OVERALL_STATUS" == "green" ]]; then
        OVERALL_STATUS="red"
        FAILING_PHASE="$phase"
        FAILING_EXIT="$exit_code"
    fi
}

run_phase() {
    local phase="$1"
    local command_display="$2"
    shift 2

    local log_file="${logs_dir}/${#PHASE_NAMES[@]}-${phase}.log"
    local exit_code

    echo "release-rehearse: phase=${phase}"
    set +e
    "$@" >"$log_file" 2>&1
    exit_code=$?
    set -e

    if [[ "$exit_code" -eq 0 ]]; then
        record_phase "$phase" "pass" "$exit_code" "$command_display" "$log_file"
        return 0
    fi

    record_phase "$phase" "fail" "$exit_code" "$command_display" "$log_file"
    return 1
}

create_and_push_tag() {
    git tag -a "$REHEARSAL_TAG" -m "Release rehearsal ${REHEARSAL_TAG}

Commit: ${commit_sha}

Authored-By: Niraj Kumar <nirajkvinit@gmail.com>"
    LOCAL_TAG_CREATED=true
    git push "$RAW_REMOTE" "$REHEARSAL_TAG"
    RAW_TAG_PUSHED=true
}

run_goreleaser_snapshot() {
    command -v goreleaser >/dev/null 2>&1 || {
        echo "goreleaser not found; install GoReleaser before release rehearsal" >&2
        return 1
    }

    goreleaser release --snapshot --clean
}

verify_artifact_matrix() {
    local matrix_file=""
    local unexpected

    for candidate in .aman-pm/validation/release-v*/goreleaser-dry-run.md; do
        [[ -e "$candidate" ]] || continue
        matrix_file="$candidate"
    done

    if [[ -z "$matrix_file" ]]; then
        echo "no prior goreleaser dry-run matrix found under .aman-pm/validation/release-v*/" >&2
        return 1
    fi

    grep -q 'darwin/arm64' "$matrix_file" || {
        echo "matrix file does not document darwin/arm64: ${matrix_file}" >&2
        return 1
    }

    compgen -G 'dist/amanmcp_*_darwin_arm64.tar.gz' >/dev/null || {
        echo "missing darwin/arm64 archive in dist/" >&2
        return 1
    }

    [[ -f dist/checksums.txt ]] || {
        echo "missing dist/checksums.txt" >&2
        return 1
    }

    [[ -f dist/homebrew/Casks/amanmcp.rb ]] || {
        echo "missing dist/homebrew/Casks/amanmcp.rb" >&2
        return 1
    }

    unexpected="$(find dist -maxdepth 1 -type f \( -name '*linux*' -o -name '*darwin_amd64*' \) -print | head -n 1)"
    if [[ -n "$unexpected" ]]; then
        echo "unexpected artifact outside documented darwin/arm64 matrix: ${unexpected}" >&2
        return 1
    fi
}

sync_to_public() {
    PRIVATE_REPO="$repo_root" PUBLIC_REPO="$PUBLIC_REPO_PATH" ./scripts/sync-to-public.sh --auto
}

verify_public_push() {
    local local_head
    local remote_head

    local_head="$(git -C "$PUBLIC_REPO_PATH" rev-parse HEAD)"
    remote_head="$(git -C "$PUBLIC_REPO_PATH" ls-remote "$PUBLIC_MIRROR_REMOTE_RESOLVED" refs/heads/main | awk '{print $1}')"

    if [[ -z "$remote_head" ]]; then
        echo "could not read refs/heads/main from ${PUBLIC_MIRROR_REMOTE_RESOLVED}" >&2
        return 1
    fi

    if [[ "$remote_head" != "$local_head" ]]; then
        echo "public mirror push did not land cleanly: local=${local_head} remote=${remote_head}" >&2
        return 1
    fi
}

run_public_mirror_tests() {
    if [[ -n "$(git -C "$PUBLIC_REPO_PATH" status --porcelain)" ]]; then
        git -C "$PUBLIC_REPO_PATH" status --short
        echo "public rehearsal clone is dirty after sync" >&2
        return 1
    fi

    (cd "$PUBLIC_REPO_PATH" && CGO_ENABLED=1 go test -race ./...)
}

teardown_tag() {
    local exit_code=0

    if [[ "$RAW_TAG_PUSHED" == "true" ]]; then
        git push "$RAW_REMOTE" ":refs/tags/${REHEARSAL_TAG}" || exit_code=$?
    fi

    if [[ "$LOCAL_TAG_CREATED" == "true" ]] || git rev-parse -q --verify "refs/tags/${REHEARSAL_TAG}" >/dev/null; then
        git tag -d "$REHEARSAL_TAG" || exit_code=$?
    fi

    return "$exit_code"
}

write_report() {
    local ended_at
    local final_exit
    local idx

    ended_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    final_exit=0
    if [[ "$OVERALL_STATUS" != "green" ]]; then
        final_exit=1
    fi

    {
        echo "# Release Rehearsal Report"
        echo ""
        echo "status: ${OVERALL_STATUS}"
        echo "commit: ${commit_sha}"
        echo "short_commit: ${short_commit}"
        echo "tag: ${REHEARSAL_TAG}"
        echo "started_at: ${started_at}"
        echo "ended_at: ${ended_at}"
        echo "raw_remote: ${RAW_REMOTE_RESOLVED:-unresolved}"
        echo "public_mirror_remote: ${PUBLIC_MIRROR_REMOTE_RESOLVED:-unresolved}"
        echo "public_repo: ${PUBLIC_REPO_PATH}"
        echo "goreleaser_command: goreleaser release --snapshot --clean"
        echo "canonical_remotes_refused: Aman-CERP/amanmcp-raw, Aman-CERP/amanmcp"
        echo "failing_phase: ${FAILING_PHASE:-none}"
        echo "failing_exit_code: ${FAILING_EXIT:-0}"
        echo "final_exit_code: ${final_exit}"
        echo ""
        echo "## Phases"
        echo ""
        echo "| Phase | Status | Exit | Command |"
        echo "| --- | --- | ---: | --- |"
        for idx in "${!PHASE_NAMES[@]}"; do
            echo "| ${PHASE_NAMES[$idx]} | ${PHASE_STATUSES[$idx]} | ${PHASE_EXITS[$idx]} | \`${PHASE_COMMANDS[$idx]}\` |"
        done

        if [[ "$OVERALL_STATUS" != "green" ]]; then
            echo ""
            echo "## Failing Phase Log"
            echo ""
            for idx in "${!PHASE_NAMES[@]}"; do
                if [[ "${PHASE_NAMES[$idx]}" == "$FAILING_PHASE" ]]; then
                    echo '```text'
                    tail -n 200 "${PHASE_LOGS[$idx]}" 2>/dev/null || true
                    echo '```'
                    break
                fi
            done
        fi
    } > "$report_path"

    echo "release-rehearse: report=${report_path}"
}

if ! run_phase "config" "validate rehearsal remotes" validate_config; then
    run_phase "tag-teardown" "delete rehearsal tag locally/remotely" teardown_tag || true
    write_report
    exit 1
fi

if [[ "$OVERALL_STATUS" == "green" ]] && ! run_phase "raw-tag-push" "git tag -a ${REHEARSAL_TAG} && git push ${RAW_REMOTE} ${REHEARSAL_TAG}" create_and_push_tag; then
    :
fi

if [[ "$OVERALL_STATUS" == "green" ]] && ! run_phase "snapshot" "goreleaser release --snapshot --clean" run_goreleaser_snapshot; then
    :
fi

if [[ "$OVERALL_STATUS" == "green" ]] && ! run_phase "artifact-matrix" "verify dist artifacts against documented darwin/arm64 matrix" verify_artifact_matrix; then
    :
fi

if [[ "$OVERALL_STATUS" == "green" ]] && ! run_phase "sync-to-public" "PRIVATE_REPO=${repo_root} PUBLIC_REPO=${PUBLIC_REPO_PATH} ./scripts/sync-to-public.sh --auto" sync_to_public; then
    :
fi

if [[ "$OVERALL_STATUS" == "green" ]] && ! run_phase "public-push-verify" "git ls-remote ${PUBLIC_MIRROR_REMOTE_RESOLVED:-public-mirror} refs/heads/main" verify_public_push; then
    :
fi

if [[ "$OVERALL_STATUS" == "green" ]] && ! run_phase "public-mirror-tests" "cd ${PUBLIC_REPO_PATH} && CGO_ENABLED=1 go test -race ./..." run_public_mirror_tests; then
    :
fi

if ! run_phase "tag-teardown" "delete rehearsal tag locally/remotely" teardown_tag; then
    :
fi

write_report

if [[ "$OVERALL_STATUS" == "green" ]]; then
    exit 0
fi

exit 1
