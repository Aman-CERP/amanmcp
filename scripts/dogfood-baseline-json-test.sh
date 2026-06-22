#!/usr/bin/env bash
# DEBT-040 regression test: dogfood-baseline.sh must ALWAYS emit valid JSON,
# even when captured command output contains characters that break naive string
# interpolation. This is what made .aman-pm/validation/baseline-results.json
# invalid (an unescaped query="..." in a log capture) and forced graph_status to
# `partial`.
#
# Strategy: source the script (its main-guard prevents execution), then call its
# JSON builders with adversarial inputs and assert `jq -e .` accepts the result.
# No amanmcp binary or index is required.
#
# Run: ./scripts/dogfood-baseline-json-test.sh   (exit 0 = pass)

# The counter/STATE_FILE/RESULTS_FILE globals set below are read by save_results,
# which lives in the sourced dogfood-baseline.sh; shellcheck cannot follow the
# source and flags them as unused (SC2034). They are intentional, so disable
# SC2034 for this file.
# shellcheck disable=SC2034

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
source "${SCRIPT_DIR}/dogfood-baseline.sh"
set +e  # the sourced script enables `set -e`; manage flow explicitly here

fail=0

assert_valid_json() { # <name> <json-string>
    local name="$1" json="$2"
    if printf '%s' "$json" | jq -e . >/dev/null 2>&1; then
        echo "  ok   - ${name}"
    else
        echo "  FAIL - ${name}"
        printf '         got: %s\n' "$json"
        fail=1
    fi
}

# One value containing every JSON-hostile construct:
#  - embedded double quotes (the exact original bug)
#  - a backslash
#  - tab + newline (control chars U+0009 / U+000A)
#  - an ANSI ESC color sequence (control char U+001B)
#  - an invalid UTF-8 byte (0xFF)
nasty=$(printf 'log query="x" path=a\\b\tTAB\nNL \033[31mRED\033[0m bad\377utf8')

echo "== result_json emits valid JSON for adversarial values =="
assert_valid_json "embedded quotes (the original baseline-results.json bug)" \
    "$(result_json tier tier1 query 'Search function' expected internal/search status fail output 'INFO query="Search function" results=4 [ {')"
assert_valid_json "backslash + control chars + ANSI + invalid UTF-8" \
    "$(result_json tier tier1 query q expected e status fail output "$nasty")"
assert_valid_json "hostile chars in the query field itself" \
    "$(result_json tier negative query 'func(); DROP "x"\ ;' description sql status pass)"
assert_valid_json "empty values" \
    "$(result_json tier negative query '' description 'empty query' status pass)"

# The invalid byte must be coerced to U+FFFD (jq / Go encoding-json behavior),
# not dropped or left to corrupt the stream.
got=$(result_json tier t query q expected e status fail output "$nasty")
if printf '%s' "$got" | jq -e '.output | contains("�")' >/dev/null 2>&1; then
    echo "  ok   - invalid UTF-8 byte replaced with U+FFFD"
else
    echo "  FAIL - invalid UTF-8 byte not replaced with U+FFFD"
    fail=1
fi

echo "== save_results writes a valid JSON document =="
RESULTS=()
RESULTS+=("$(result_json tier tier1 query 'Search function' expected internal/search status fail output "$nasty")")
RESULTS+=("$(result_json tier negative query '"weird"\back' description edge status pass)")
TIER1_PASSED=10; TIER1_FAILED=2
TIER2_PASSED=3;  TIER2_FAILED=1
NEGATIVE_PASSED=4; NEGATIVE_FAILED=0

tmp_results=$(mktemp)
RESULTS_FILE="$tmp_results"
STATE_FILE="$(mktemp -u)"  # non-existent: do not touch the real state.json
save_results >/dev/null 2>&1

assert_valid_json "emitted baseline-results.json file" "$(cat "$tmp_results")"
if jq -e '.results | length == 2' "$tmp_results" >/dev/null 2>&1 \
   && jq -e '.summary.tier1.passed == 10 and .summary.negative.failed == 0' "$tmp_results" >/dev/null 2>&1; then
    echo "  ok   - document structure (results array + summary counts)"
else
    echo "  FAIL - document structure"
    fail=1
fi

# Empty results set must still produce a valid document with an empty array.
RESULTS=()
tmp_empty=$(mktemp)
RESULTS_FILE="$tmp_empty"
save_results >/dev/null 2>&1
if jq -e '.results == []' "$tmp_empty" >/dev/null 2>&1; then
    echo "  ok   - empty results -> valid document with []"
else
    echo "  FAIL - empty results case"
    fail=1
fi

rm -f "$tmp_results" "$tmp_empty"

# Guard against the set -e + post-increment abort (Codex P2-A): `((VAR++))`
# evaluates to the old value, so the first increment from 0 returns exit 1 and
# `set -e` aborts mid-run. The script must use the prefix form `((++VAR))`.
echo "== no set-e-unsafe post-increment counters in dogfood-baseline.sh =="
if grep -nE '\(\([A-Za-z_]+\+\+\)\)' "${SCRIPT_DIR}/dogfood-baseline.sh"; then
    echo "  FAIL - found ((VAR++)) post-increment; use ((++VAR)) (aborts under set -e from 0)"
    fail=1
else
    echo "  ok   - all counter increments use the set-e-safe prefix form"
fi

echo ""
if [[ $fail -eq 0 ]]; then
    echo "PASS: DEBT-040 JSON-safety checks all green"
    exit 0
fi
echo "FAIL: DEBT-040 JSON-safety checks failed"
exit 1
