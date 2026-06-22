#!/bin/bash
#
# verify-amanpm-wave2.sh — DEBT-046 Wave 2 tooling parity gate
#
# Verifies spawn-rules-for infrastructure, DoD harness, and adapted skills.
#
# Usage: ./scripts/verify-amanpm-wave2.sh

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

ERRORS=0

error() {
    echo -e "${RED}ERROR:${NC} $1"
    ERRORS=$((ERRORS + 1))
}

pass() {
    echo -e "${GREEN}✓${NC} $1"
}

require_file() {
    local path="$1"
    local label="$2"
    if [[ ! -f "$path" ]]; then
        error "$label missing: $path"
        return 1
    fi
    pass "$label"
}

require_dir_skill() {
    local path="$1"
    local label="$2"
    if [[ ! -f "$path/SKILL.md" ]]; then
        error "$label missing SKILL.md: $path/SKILL.md"
        return 1
    fi
    pass "$label"
}

cd "$(dirname "$0")/.."

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo " DEBT-046 Wave 2 Tooling Verification"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

require_file ".claude/rule-registry.yaml" "rule registry"
require_file "scripts/python/rules_for_paths.py" "rules_for_paths.py"
require_file "scripts/python/spawn_prompt_skeleton.py" "spawn_prompt_skeleton.py"
require_file "scripts/python/scoped_claude_md_for.py" "scoped_claude_md_for.py"
require_file ".aman-pm/scripts/amanpm/verify_feature_complete_with_gates.py" "verify_feature_complete_with_gates.py"
require_file ".claude/guides/definition-of-done.md" "definition-of-done guide"
require_file ".claude/guides/rule-discovery.md" "rule-discovery guide"
require_file ".aman-pm/CLAUDE.md" "scoped .aman-pm/CLAUDE.md"
require_file "internal/CLAUDE.md" "scoped internal/CLAUDE.md"
require_file ".aman-pm/checklists/CL-IMPL-DC-PreCommit.md" "CL-IMPL-DC-PreCommit checklist"

require_dir_skill ".claude/skills/spawn-rules-for" "spawn-rules-for skill"
require_dir_skill ".claude/skills/scope-challenge" "scope-challenge skill"
require_dir_skill ".claude/skills/adversarial-review" "adversarial-review skill"
require_dir_skill ".agents/skills/spawn-rules-for" "agents spawn-rules-for mirror"
require_dir_skill ".agents/skills/scope-challenge" "agents scope-challenge mirror"
require_dir_skill ".agents/skills/adversarial-review" "agents adversarial-review mirror"

if ! grep -q 'spawn-rules-for' .claude/skill-registry.md; then
    error "skill-registry.md missing spawn-rules-for entry"
else
    pass "skill-registry references spawn-rules-for"
fi

if ! python3 scripts/python/rules_for_paths.py internal/search/engine.go >/dev/null 2>&1; then
    error "rules_for_paths.py failed on sample path"
else
    pass "rules_for_paths.py executes"
fi

if ! python3 scripts/python/spawn_prompt_skeleton.py internal/search/engine.go >/dev/null 2>&1; then
    error "spawn_prompt_skeleton.py failed on sample path"
else
    pass "spawn_prompt_skeleton.py executes"
fi

echo ""
if [[ "$ERRORS" -gt 0 ]]; then
    echo -e "${RED}FAILED:${NC} $ERRORS error(s)"
    exit 1
fi

echo "PASSED: DEBT-046 Wave 2 tooling surfaces verified"
exit 0