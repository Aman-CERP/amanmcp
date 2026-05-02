#!/usr/bin/env bash
#
# verify-release-claims.sh — Cross-check PM "done" claims against verifiable git tags.
#
# Implements POL-014 ("tag-producing tasks need verifiable artifact") at the
# script layer. Walks done/resolved PM items whose frontmatter declares
# architecture_tags containing "release", reads each item's evidence_tag, and
# verifies any concrete version tag exists in `git tag -l`. Sentinel values
# ("preflight", "preparatory", "skipped", "none") are accepted as explicit
# opt-outs that document why no real tag is expected.
#
# This is the script-side companion to internal/pmmutation.enforceReleaseEvidence,
# which prevents new release-tagged items from being moved to a terminal status
# without an evidence payload. The script catches drift in items that landed
# before the gate existed, were edited directly (bypassing pm.mutate), or whose
# claimed tag has been deleted upstream.
#
# Usage:
#   scripts/verify-release-claims.sh           # human-readable report
#   scripts/verify-release-claims.sh --quiet   # only print drift; exit code is signal
#   scripts/verify-release-claims.sh --json    # JSON report for hook/banner consumption
#
# Exit codes:
#   0 - all release claims verified (or no release-tagged done items found)
#   1 - drift detected (claim missing evidence, or claimed tag absent)
#   2 - tooling missing (not a git repo, or required commands unavailable)

set -u  # NOT -e — we accumulate findings before exiting.

SCRIPT_NAME="verify-release-claims"

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "$REPO_ROOT" ]]; then
    echo "$SCRIPT_NAME: not inside a git repository" >&2
    exit 2
fi
cd "$REPO_ROOT"

QUIET=0
JSON=0
for arg in "$@"; do
    case "$arg" in
        --quiet) QUIET=1 ;;
        --json)  JSON=1 ;;
        --help|-h)
            grep -E '^#( |$)' "$0" | sed 's/^# \?//'
            exit 0
            ;;
        *)
            echo "$SCRIPT_NAME: unknown flag: $arg" >&2
            exit 2
            ;;
    esac
done

log() {
    if [[ "$QUIET" -eq 0 && "$JSON" -eq 0 ]]; then
        printf '%s\n' "$*"
    fi
}

# Sentinel evidence values that are valid opt-outs (no tag actually produced).
sentinel_values="preflight preparatory skipped none n/a"

# is_sentinel_evidence VALUE — return 0 if VALUE is a recognised opt-out.
is_sentinel_evidence() {
    local value="${1,,}"  # lowercase
    for candidate in $sentinel_values; do
        [[ "$value" == "$candidate" ]] && return 0
    done
    return 1
}

# is_version_tag VALUE — return 0 if VALUE looks like a git release tag.
is_version_tag() {
    [[ "$1" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.]+)?$ ]]
}

# extract_frontmatter_field FIELD FILE — print the value of FIELD from the
# leading YAML frontmatter, or empty if absent. Strips quotes/whitespace.
extract_frontmatter_field() {
    local field="$1"
    local file="$2"
    awk -v field="$field" '
        BEGIN { in_fm = 0; depth = 0 }
        /^---$/ {
            if (depth == 0) { in_fm = 1; depth = 1; next }
            else if (in_fm) { exit }
        }
        in_fm && $0 ~ "^" field ":" {
            sub("^" field ":[ \t]*", "", $0)
            gsub(/^["'"'"']|["'"'"']$/, "", $0)
            sub(/[ \t]+$/, "", $0)
            print $0
            exit
        }
    ' "$file"
}

# has_release_arch_tag FILE — return 0 if frontmatter lists "release" in
# architecture_tags (inline or block-list form).
has_release_arch_tag() {
    local file="$1"
    awk '
        BEGIN { in_fm = 0; in_tags = 0; depth = 0 }
        /^---$/ {
            if (depth == 0) { in_fm = 1; depth = 1; next }
            else if (in_fm) { exit }
        }
        in_fm && /^architecture_tags:/ {
            line = $0
            sub(/^architecture_tags:[ \t]*/, "", line)
            if (line ~ /^\[/) {
                # Inline list form: [release, devops, ...]
                gsub(/[][ \t"'"'"']/, "", line)
                n = split(line, arr, ",")
                for (i = 1; i <= n; i++) {
                    if (arr[i] == "release") { print "yes"; exit }
                }
                in_tags = 0
                next
            }
            in_tags = 1
            next
        }
        in_fm && in_tags {
            # Continue scanning block-list "  - value" entries until we hit a
            # non-list line (different YAML key or blank).
            if ($0 ~ /^[ \t]*-[ \t]/) {
                value = $0
                sub(/^[ \t]*-[ \t]+/, "", value)
                gsub(/^["'"'"']|["'"'"']$/, "", value)
                sub(/[ \t]+$/, "", value)
                if (value == "release") { print "yes"; exit }
                next
            }
            if ($0 ~ /^[ \t]*$/) { next }
            in_tags = 0
        }
    ' "$file" | grep -q yes
}

# Gather candidates from all terminal-status folders.
candidate_dirs=(
    .aman-pm/backlog/tasks/done
    .aman-pm/backlog/tasks/resolved
    .aman-pm/backlog/features/done
    .aman-pm/backlog/features/resolved
    .aman-pm/backlog/bugs/resolved
    .aman-pm/backlog/debt/resolved
    .aman-pm/backlog/debt/done
    .aman-pm/backlog/spikes/done
    .aman-pm/backlog/epics/done
)

candidates=()
for dir in "${candidate_dirs[@]}"; do
    [[ -d "$dir" ]] || continue
    while IFS= read -r -d '' file; do
        candidates+=("$file")
    done < <(find "$dir" -maxdepth 1 -type f -name '*.md' -print0 2>/dev/null)
done

if [[ "${#candidates[@]}" -eq 0 ]]; then
    log "$SCRIPT_NAME: no done items found"
    exit 0
fi

verified=()
sentinel=()
drift_missing=()
drift_absent_tag=()

local_tags=""
if command -v git >/dev/null 2>&1; then
    local_tags="$(git tag -l 2>/dev/null || true)"
fi

for file in "${candidates[@]}"; do
    has_release_arch_tag "$file" || continue

    item_id="$(extract_frontmatter_field id "$file")"
    [[ -z "$item_id" ]] && item_id="$(basename "$file" .md)"

    evidence_tag="$(extract_frontmatter_field evidence_tag "$file")"

    if [[ -z "$evidence_tag" ]]; then
        drift_missing+=("$item_id|$file")
        continue
    fi

    if is_sentinel_evidence "$evidence_tag"; then
        sentinel+=("$item_id|$evidence_tag|$file")
        continue
    fi

    if is_version_tag "$evidence_tag"; then
        if printf '%s\n' "$local_tags" | grep -qx "$evidence_tag"; then
            verified+=("$item_id|$evidence_tag|$file")
        else
            drift_absent_tag+=("$item_id|$evidence_tag|$file")
        fi
        continue
    fi

    # Unrecognised value — treat as drift so reviewers notice.
    drift_missing+=("$item_id|$file (unrecognised evidence_tag value: $evidence_tag)")
done

drift_total=$(( ${#drift_missing[@]} + ${#drift_absent_tag[@]} ))

if [[ "$JSON" -eq 1 ]]; then
    printf '{'
    printf '"verified":%d,' "${#verified[@]}"
    printf '"sentinel":%d,' "${#sentinel[@]}"
    printf '"drift_missing":%d,' "${#drift_missing[@]}"
    printf '"drift_absent_tag":%d,' "${#drift_absent_tag[@]}"
    printf '"drift_total":%d,' "$drift_total"

    printf '"missing":['
    for i in "${!drift_missing[@]}"; do
        IFS='|' read -r id rest <<<"${drift_missing[$i]}"
        printf '%s{"id":"%s","detail":"%s"}' \
            "$( ((i>0)) && echo , )" "$id" "${rest//\"/\\\"}"
    done
    printf '],'

    printf '"absent_tags":['
    for i in "${!drift_absent_tag[@]}"; do
        IFS='|' read -r id tag file <<<"${drift_absent_tag[$i]}"
        printf '%s{"id":"%s","tag":"%s","file":"%s"}' \
            "$( ((i>0)) && echo , )" "$id" "$tag" "$file"
    done
    printf ']'
    printf '}\n'
else
    log "$SCRIPT_NAME report"
    log "  verified           : ${#verified[@]}"
    log "  sentinel opt-outs  : ${#sentinel[@]}"
    log "  drift (no evidence): ${#drift_missing[@]}"
    log "  drift (tag absent) : ${#drift_absent_tag[@]}"

    if [[ "${#verified[@]}" -gt 0 && "$QUIET" -eq 0 ]]; then
        log ""
        log "Verified release claims:"
        for entry in "${verified[@]}"; do
            IFS='|' read -r id tag file <<<"$entry"
            log "  ✓ $id → $tag  ($file)"
        done
    fi

    if [[ "${#sentinel[@]}" -gt 0 && "$QUIET" -eq 0 ]]; then
        log ""
        log "Sentinel opt-outs (no tag claimed):"
        for entry in "${sentinel[@]}"; do
            IFS='|' read -r id tag file <<<"$entry"
            log "  · $id → $tag  ($file)"
        done
    fi

    if [[ "${#drift_missing[@]}" -gt 0 ]]; then
        printf '\nDRIFT — release-tagged done items missing evidence_tag:\n'
        for entry in "${drift_missing[@]}"; do
            IFS='|' read -r id rest <<<"$entry"
            printf '  ✗ %s  %s\n' "$id" "$rest"
        done
    fi

    if [[ "${#drift_absent_tag[@]}" -gt 0 ]]; then
        printf '\nDRIFT — claimed tag not present in local git tags:\n'
        for entry in "${drift_absent_tag[@]}"; do
            IFS='|' read -r id tag file <<<"$entry"
            printf '  ✗ %s  evidence_tag=%s  %s\n' "$id" "$tag" "$file"
        done
        printf '\nFix: either run `git fetch --tags`, push the missing tag, or supersede the claim.\n'
    fi
fi

if [[ "$drift_total" -gt 0 ]]; then
    exit 1
fi

exit 0
