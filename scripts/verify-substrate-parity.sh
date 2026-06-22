#!/bin/bash
#
# verify-substrate-parity.sh - DEBT-034 closure gate for AmanPM substrate parity
#
# Verifies that adapted skills, command docs, agents, rules, and guides exist
# and contain no unadapted AmanERP-only path references.
#
# Usage: ./scripts/verify-substrate-parity.sh
#
# Exit codes:
#   0 - All required substrate surfaces present
#   1 - Missing required artifact or forbidden ERP reference found

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
NC='\033[0m'

ERRORS=0

error() {
    echo -e "${RED}ERROR:${NC} $1"
    ERRORS=$((ERRORS + 1))
}

pass() {
    echo -e "${GREEN}✓${NC} $1"
}

header() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo " $1"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
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
ROOT="$(pwd)"

header "DEBT-034 Substrate Parity Verification"

header "Required agents"
require_file ".claude/agents/pm-mechanical.md" "pm-mechanical agent"
require_file ".claude/agents/audit-agent.md" "audit-agent"
require_file ".claude/agents/coding-agent.md" "coding-agent"
require_file ".claude/agents/code-health-auditor.md" "code-health-auditor"
require_file ".claude/agents/test-health-auditor.md" "test-health-auditor"
require_file ".claude/agents/search-quality-auditor.md" "search-quality-auditor"

header "Required rules"
require_file ".claude/rules/agent-patterns.md" "agent-patterns rule"
require_file ".claude/rules/validation.md" "validation rule"
require_file ".claude/rules/changelog.md" "changelog rule"
require_file ".claude/rules/code-conventions.md" "code-conventions rule"

header "Required guides"
require_file ".claude/guides/pm/amanpm-constitution.md" "amanpm-constitution guide"
require_file ".claude/guides/engineering/premium-engineering-standard.md" "premium-engineering-standard guide"
require_file ".claude/guides/pm/discovery-protocol.md" "discovery-protocol guide"

header "Required residual skills (DEBT-034 closure)"
require_dir_skill ".claude/skills/aman-discovery" "aman-discovery skill"
require_dir_skill ".claude/skills/aman-quality-gates" "aman-quality-gates skill"
require_dir_skill ".claude/skills/aman-spec-development" "aman-spec-development skill"

header "Agents mirror (.agents/skills)"
require_dir_skill ".agents/skills/aman-discovery" "agents aman-discovery mirror"
require_dir_skill ".agents/skills/aman-quality-gates" "agents aman-quality-gates mirror"
require_dir_skill ".agents/skills/aman-spec-development" "agents aman-spec-development mirror"

header "Required /aman command docs"
REQUIRED_COMMANDS=(
    audit
    capture-learning
    pre-commit
    checklist
    feature-gate
    heal
)
for cmd in "${REQUIRED_COMMANDS[@]}"; do
    require_file ".claude/skills/aman/commands/${cmd}.md" "/aman ${cmd} command (.claude)"
    require_file ".agents/skills/aman/commands/${cmd}.md" "/aman ${cmd} command (.agents)"
done

header "Checklist registry"
require_file ".claude/checklists/README.md" "checklist registry"

header "Migration guide"
require_file ".aman-pm/guides/amanpm-substrate-port.md" "substrate port migration guide"

header "Skill registry references"
if ! grep -q 'aman-discovery' .claude/skill-registry.md; then
    error "skill-registry.md missing aman-discovery entry"
else
    pass "skill-registry references aman-discovery"
fi
if ! grep -q 'aman-quality-gates' .claude/skill-registry.md; then
    error "skill-registry.md missing aman-quality-gates entry"
else
    pass "skill-registry references aman-quality-gates"
fi
if ! grep -q 'aman-spec-development' .claude/skill-registry.md; then
    error "skill-registry.md missing aman-spec-development entry"
else
    pass "skill-registry references aman-spec-development"
fi

header "Forbidden unadapted ERP references"
SCAN_PATHS=(
    .claude/skills/aman-discovery
    .claude/skills/aman-quality-gates
    .claude/skills/aman-spec-development
    .claude/skills/aman/commands/audit.md
    .claude/skills/aman/commands/capture-learning.md
    .claude/skills/aman/commands/pre-commit.md
    .claude/skills/aman/commands/checklist.md
    .claude/skills/aman/commands/feature-gate.md
    .claude/skills/aman/commands/heal.md
    .agents/skills/aman/commands/audit.md
    .agents/skills/aman/commands/capture-learning.md
    .agents/skills/aman/commands/pre-commit.md
    .agents/skills/aman/commands/checklist.md
    .agents/skills/aman/commands/feature-gate.md
    .agents/skills/aman/commands/heal.md
    .agents/skills/aman-discovery
    .agents/skills/aman-quality-gates
    .agents/skills/aman-spec-development
)

search_forbidden() {
    local pattern="$1"
    local path="$2"
    if command -v rg >/dev/null 2>&1; then
        rg -n "$pattern" "$path" 2>/dev/null || true
        return
    fi
    grep -RIn "$pattern" "$path" 2>/dev/null || true
}

FORBIDDEN_PATTERNS=(
    'OpenFGA'
    'PostgreSQL RLS'
    'ZITADEL'
    'SigNoz'
)

for path in "${SCAN_PATHS[@]}"; do
    if [[ ! -e "$path" ]]; then
        continue
    fi
    for pattern in "${FORBIDDEN_PATTERNS[@]}"; do
        if [[ -n "$(search_forbidden "$pattern" "$path")" ]]; then
            error "Forbidden ERP pattern '$pattern' found in $path"
        fi
    done
    # services/ and apps/ are forbidden as path assumptions, not in negative guidance.
    while IFS= read -r hit; do
        [[ -z "$hit" ]] && continue
        if [[ "$hit" == *"Never assume"* ]] || [[ "$hit" == *"not "* ]] || [[ "$hit" == *"SKIP"* ]]; then
            continue
        fi
        error "Forbidden ERP path reference in $path: $hit"
    done < <(search_forbidden 'services/' "$path"; search_forbidden 'apps/' "$path")
done
if [[ "$ERRORS" -eq 0 ]]; then
    pass "No forbidden ERP-only references in new substrate artifacts"
fi

header "Summary"
if [[ "$ERRORS" -gt 0 ]]; then
    echo -e "${RED}FAILED:${NC} $ERRORS error(s)"
    exit 1
fi

echo "PASSED: DEBT-034 substrate parity surfaces verified"
exit 0