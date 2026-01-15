# ADR-011: Version Pinning Strategy

**Status:** Implemented
**Date:** 2025-12-28
**Supersedes:** None
**Superseded by:** None

---

## Context

During development, we experienced CI failures due to version drift between local development and CI environments. Specifically:

- Local golangci-lint v1.62.2 vs CI "latest" (v1.64.8)
- Different linting rules produced different results
- 6 failed CI runs, ~2 hours debugging

This is a common problem: "works on my machine" failures.

---

## Decision

We will **pin all tool versions explicitly** with no use of "latest" tags.

The **Makefile is the single source of truth** for all tool versions.

---

## Rationale

### The Problem with "latest"

```yaml
# BAD: Version drift
- uses: golangci/golangci-lint-action@v4
  with:
    version: latest  # Could be anything!
```

### The Solution: Explicit Versions

```yaml
# GOOD: Pinned version
- uses: golangci/golangci-lint-action@v4
  with:
    version: v1.62.2  # Matches Makefile
```

### Single Source of Truth

```makefile
# Makefile - All versions defined here
GO_VERSION = 1.25.5
GOLANGCI_LINT_VERSION = v1.62.2
```

All other files (CI workflows, scripts) read from or match Makefile.

---

## Consequences

### Positive

- CI behavior matches local development exactly
- Reproducible builds across all environments
- Version updates are intentional, not accidental
- Easy to track version changes in git history

### Negative

- Must manually update versions (no auto-updates)
- Need consistency checking script
- Slightly more maintenance overhead

### Neutral

- Requires documentation of version update process

---

## Implementation Notes

### Version Consistency Check

Run before pushing:

```bash
./scripts/check-version-consistency.sh
```

Checks:

1. Go version matches across Makefile, CI, go.mod
2. golangci-lint version matches across Makefile, CI
3. No "latest" tags anywhere
4. Tool compatibility (e.g., golangci-lint v2.x for Go 1.25+)

### Makefile Pattern

```makefile
# Tool Versions (Single Source of Truth - ADR-011)
GO_VERSION = 1.25.5
GOLANGCI_LINT_VERSION = v1.62.2

lint:
    go run github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run
```

---

## Related

- [RCA-001](../postmortems/RCA-001-ci-lint-version-drift.md) - Incident that led to this decision
- [CI Parity Guide](../.claude/guides/ci-parity-check.md) - How to validate locally

---

## Changelog

| Date | Change |
|------|--------|
| 2025-12-28 | Initial decision based on RCA-001 |
