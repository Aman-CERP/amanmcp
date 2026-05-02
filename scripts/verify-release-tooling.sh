#!/usr/bin/env bash
# Verify release-authority invariants that must be true before a tag push.

set -euo pipefail

fail() {
    echo "release-tooling check failed: $1" >&2
    exit 1
}

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || fail "not inside a git repository"
cd "$repo_root"

release_workflows=()
while IFS= read -r workflow; do
    release_workflows+=("$workflow")
done < <(find .github/workflows -maxdepth 1 \( -name 'release.yml' -o -name 'release.yaml' \) -print | sort)

if [[ "${#release_workflows[@]}" -ne 1 ]]; then
    fail "expected exactly one release workflow file, found ${#release_workflows[@]}: ${release_workflows[*]:-none}"
fi

if [[ "${release_workflows[0]}" != ".github/workflows/release.yml" ]]; then
    fail "expected canonical workflow path .github/workflows/release.yml, found ${release_workflows[0]}"
fi

release_name_count="$(grep -hE '^name:[[:space:]]+Release$' .github/workflows/release.y* 2>/dev/null | wc -l | tr -d ' ')"
if [[ "$release_name_count" != "1" ]]; then
    fail "expected exactly one workflow named Release, found $release_name_count"
fi

grep -q 'license: "Apache-2.0"' .goreleaser.yaml || fail ".goreleaser.yaml must declare Apache-2.0 license metadata"
grep -q 'git status --porcelain --untracked-files=all' scripts/release.sh || fail "release script must check tracked and untracked dirtiness"
grep -q -- '--dry-run' scripts/release.sh || fail "release script must expose --dry-run"
grep -q 'Authored-By: Niraj Kumar <nirajkvinit@gmail.com>' scripts/release.sh || fail "release script must emit the required Authored-By trailer"

if grep -nE 'Co-Authored-By|Generated with|claude\.com' scripts/release.sh; then
    fail "release script contains a forbidden generated/co-author trailer"
fi

echo "release-tooling check passed"
