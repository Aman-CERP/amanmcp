# ADR-021: TDD as Mandatory Practice

**Status:** Implemented
**Date:** 2025-12-28
**Supersedes:** None
**Superseded by:** None

---

## Context

AmanMCP uses AI-native development where Claude Code implements features autonomously. This creates quality challenges:

1. **AI-generated code quality**: Without constraints, AI may produce untested code
2. **Regression risk**: Changes can break existing functionality silently
3. **API design**: Interfaces may be awkward if designed implementation-first
4. **Confidence in changes**: Hard to know if modifications are safe

Traditional "test after" approaches don't work well with AI development because:
- AI tends to focus on implementation, forgetting tests
- "It compiles" becomes the de facto quality bar
- Refactoring becomes risky without test coverage

---

## Decision

We will require **Test-Driven Development (TDD)** for all implementation work. The mandatory cycle is:

```
RED → GREEN → REFACTOR → LINT → CI-CHECK
```

1. **RED**: Write a failing test first
2. **GREEN**: Write minimum code to pass the test
3. **REFACTOR**: Clean up code while keeping tests green
4. **LINT**: Run linter, fix warnings
5. **CI-CHECK**: Validate with `make ci-check` before marking complete

---

## Rationale

### Alternatives Considered

| Option | Pros | Cons |
|--------|------|------|
| Test after implementation | Faster initial coding | Tests often skipped, poor coverage |
| No formal testing process | Maximum flexibility | Quality varies wildly |
| **Chosen: Mandatory TDD** | Guaranteed coverage, better design | Slower initial velocity |
| Integration tests only | Tests real behavior | Slow, hard to pinpoint failures |

### Why Mandatory TDD

From `.claude/philosophy/tdd-philosophy.md`:

> "Tests don't slow down development. Manually checking if your code works definitely does."

1. **Safe refactoring**: Tests catch regressions immediately
2. **Better API design**: Writing tests first reveals awkward interfaces
3. **Documentation**: Tests document expected behavior
4. **AI constraint**: Forces structured approach, prevents "just make it work"

### The Testing Pyramid

| Level | Percentage | Purpose |
|-------|------------|---------|
| Unit Tests | 80% | Fast, isolated, high coverage |
| Integration Tests | 15% | Real dependencies, slower |
| E2E Tests | 5% | Full system, validation guides |

---

## Consequences

### Positive

- Guaranteed test coverage (25%+ threshold)
- Safe refactoring at any time
- Better API design from consumer perspective
- Documentation of expected behavior
- Faster debugging (tests pinpoint issues)

### Negative

- Slower initial implementation
- Requires discipline to follow
- Some overhead for trivial changes

### Neutral

- Learning curve for test-first thinking
- Test code must also be maintained

---

## Implementation Notes

### TDD Skill

The TDD workflow is codified in `.claude/skills/tdd-workflow/SKILL.md`:

```markdown
## TDD Implementation Loop (13 Steps)

1. Read feature spec
2. Identify acceptance criteria
3. Write first failing test (RED)
4. Run test, verify it fails
5. Write minimum code to pass (GREEN)
6. Run test, verify it passes
7. Refactor if needed
8. Run tests, verify still green
9. Repeat for next acceptance criterion
10. Run `make lint`
11. Run `make ci-check`
12. Create validation guide
13. Report completion
```

### Quality Gates

A feature is NOT complete until:

```bash
make ci-check  # Must exit 0
```

This validates:
- All tests pass with race detector
- Coverage meets threshold (25%+)
- No lint warnings
- Build succeeds

### Skill Invocation

When user says "implement F##":
```
Skill(skill: "tdd-workflow")
```

This loads the TDD context and enforces the cycle.

---

## Related

- [TDD Philosophy](../strategy/tdd-philosophy.md) - Why we use TDD
- [TDD Skill](.claude/skills/tdd-workflow/SKILL.md) - Implementation guide
- [CI Check Skill](.claude/skills/ci-check/SKILL.md) - Validation
- [Workflow](../workflow.md) - Overall development process
- [CLAUDE.md](../../CLAUDE.md) - AI behavior rules

---

## Changelog

| Date | Change |
|------|--------|
| 2025-12-28 | Initial implementation |
