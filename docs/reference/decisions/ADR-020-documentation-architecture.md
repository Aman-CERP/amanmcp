# ADR-020: Documentation Architecture (SSOT)

**Status:** Implemented
**Date:** 2025-12-28
**Supersedes:** None
**Superseded by:** None

---

## Context

AmanMCP uses AI-native development where Claude Code acts as an autonomous executor. This creates unique documentation challenges:

1. **Information drift**: Same information in multiple files becomes inconsistent
2. **AI confusion**: Claude reads multiple files and encounters contradictions
3. **Update burden**: Changes require updating many files
4. **Source of truth unclear**: When files conflict, which is authoritative?

Traditional documentation approaches don't address AI-as-reader scenarios where the reader processes many files simultaneously.

---

## Decision

We will implement a **Single Source of Truth (SSOT) hierarchy** where each concept has exactly one authoritative source, and other files link to it rather than duplicate.

### Authoritative Sources

| Information | Authoritative Source | Notes |
|-------------|---------------------|-------|
| Feature status | `.aman-pm/product/features/index.md` | Never duplicate elsewhere |
| Current version | `VERSION` file | Single line, no formatting |
| Session state | `docs/context.md` | Links to index.md for features |
| ADR decisions | `docs/decisions/index.md` | Technology choices |
| Process/workflow | `docs/workflow.md` | How development works |
| AI behavior | `CLAUDE.md` | When to use skills, tools |

### Conflict Resolution Rules

1. **Feature status conflict**: `index.md` wins - update other files to match
2. **Version conflict**: `VERSION` file wins - update docs to match
3. **Process conflict**: `workflow.md` wins for "how", `CLAUDE.md` wins for "AI behavior"

---

## Rationale

### Alternatives Considered

| Option | Pros | Cons |
|--------|------|------|
| Central wiki | Single location | Doesn't integrate with code |
| README-only | Simple | Grows unwieldy, hard to navigate |
| **Chosen: SSOT hierarchy** | Clear ownership, links not duplication | Requires discipline |
| Generated docs | Always in sync | Complex tooling, less readable |

### Why SSOT Hierarchy

1. **Prevents drift**: One source per concept means no contradictions
2. **Clear updates**: Know exactly which file to modify
3. **AI-friendly**: Claude can trust authoritative sources
4. **Maintainable**: Changes propagate via links, not copy-paste
5. **Auditable**: Easy to verify consistency

---

## Consequences

### Positive

- No information conflicts for AI or humans
- Clear ownership of each type of information
- Faster updates (change one file, not many)
- Documentation stays accurate

### Negative

- Requires discipline to follow
- More files to navigate initially
- Links can become stale

### Neutral

- Learning curve for new contributors
- Some redundancy in links

---

## Implementation Notes

### Directory Structure

```
docs/
├── context.md              # Session state (SSOT)
├── workflow.md             # Development process (SSOT)
├── specs/
│   └── features/
│       └── index.md        # Feature status (SSOT)
├── decisions/
│   └── index.md            # ADR registry (SSOT)
├── validation/
│   └── index.md            # Validation guide index
└── guides/                 # Educational (not SSOT)

VERSION                     # Version number (SSOT)
CLAUDE.md                   # AI behavior (SSOT)
```

### Reference Pattern

```markdown
<!-- Wrong: Duplicating information -->
Current version: v0.1.19
Features completed: F01-F14

<!-- Right: Reference authoritative source -->
For feature status, see [Feature Catalog](.aman-pm/product/features/index.md)
For current version, see VERSION file
```

### Reading Semantics for AI

- "Read X" means read that specific file
- Do NOT recursively follow links unless instructed
- Files reference each other for navigation, not automatic loading

---

## Related

- [CLAUDE.md](../../CLAUDE.md) - AI behavior configuration
- [Workflow](../workflow.md) - Development process
- [Feature Catalog](../specs/features/index.md) - Feature status
- [Context](../context.md) - Session state

---

## Changelog

| Date | Change |
|------|--------|
| 2025-12-28 | Initial implementation |
