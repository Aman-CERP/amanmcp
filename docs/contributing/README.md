# Contributing to AmanMCP

Welcome! We're excited that you want to contribute. This section contains detailed guides for developers.

**Start here**: [CONTRIBUTING.md](../../CONTRIBUTING.md) - Main contribution guide

---

## Developer Documentation

| Guide | What You'll Learn | Read This If... |
|-------|-------------------|-----------------|
| [Go for Newcomers](go-for-newcomers.md) | Go basics for non-Go developers | You're new to Go |
| [Code Conventions](code-conventions.md) | Go patterns, naming, structure | You're writing Go code for AmanMCP |
| [TDD Rationale](tdd-rationale.md) | Why test-first, the TDD cycle | You want to understand our testing philosophy |
| [Testing Guide](testing-guide.md) | How to run tests, validation framework | You're adding features or fixing bugs |

---

## Quick Links for Contributors

### Getting Started

1. **Fork & Clone** - [CONTRIBUTING.md](../../CONTRIBUTING.md#setup)
2. **Development Setup** - Install dependencies, build locally
3. **Run Tests** - `make test` and `make ci-check`
4. **Code Conventions** - [code-conventions.md](code-conventions.md)

### Making Changes

1. **Create Branch** - `git checkout -b feature/your-feature`
2. **Write Tests First** - TDD approach (see [testing-guide.md](testing-guide.md))
3. **Follow Conventions** - [code-conventions.md](code-conventions.md)
4. **Run CI Locally** - `make ci-check` (must pass)
5. **Add Changelog Entry** - `.aman-pm/changelog/unreleased.md`

### Submitting

1. **Commit Messages** - Use [Conventional Commits](https://www.conventionalcommits.org/)
2. **Push & PR** - See [CONTRIBUTING.md](../../CONTRIBUTING.md#pull-requests)
3. **CI Must Pass** - Tests, linting, coverage all green

### Contribution Workflow

```mermaid
flowchart TD
    Start([Want to Contribute]) --> Fork[Fork Repository<br/>github.com/Aman-CERP/amanmcp]
    style Start fill:#e1f5ff,stroke:#3498db,stroke-width:2px

    Fork --> Clone[Clone Your Fork<br/>git clone https://github.com/YOU/amanmcp]
    Clone --> Setup[Development Setup<br/>go mod download<br/>make build]
    style Setup fill:#ffe0b2,stroke:#f39c12,stroke-width:2px

    Setup --> Branch[Create Feature Branch<br/>git checkout -b feature/my-feature]
    style Branch fill:#e1f5ff,stroke:#3498db,stroke-width:2px

    Branch --> TDD{Follow TDD?}
    TDD -->|Yes| Test[Write Failing Test<br/>RED phase]
    TDD -->|No| Warning[⚠️ TDD Required<br/>See testing-guide.md]
    Warning --> Test
    style Warning fill:#ffe0b2,stroke:#f39c12,stroke-width:2px

    Test --> Impl[Implement Feature<br/>GREEN phase]
    Impl --> Refactor[Refactor Code<br/>REFACTOR phase]
    Refactor --> Lint[Run make lint<br/>Fix warnings]
    Lint --> CI[Run make ci-check<br/>Must exit 0]
    style CI fill:#ffe0b2,stroke:#f39c12,stroke-width:2px

    CI -->|Fails| Fix[Fix Issues]
    Fix --> Lint

    CI -->|Passes| Changelog[Add Changelog Entry]
    Changelog --> Commit[Commit Changes<br/>Conventional Commits format]
    style Commit fill:#e1f5ff,stroke:#3498db,stroke-width:2px

    Commit --> Push[Push to Your Fork<br/>git push origin feature/my-feature]
    Push --> PR[Create Pull Request<br/>Link related issues]
    style PR fill:#e1f5ff,stroke:#3498db,stroke-width:2px

    PR --> Review{Code Review}
    Review -->|Changes Requested| RequestChanges[Update Code]
    RequestChanges --> Lint
    style RequestChanges fill:#ffe0b2,stroke:#f39c12,stroke-width:2px

    Review -->|CI Fails| CIFail[Fix CI Issues]
    CIFail --> Lint
    style CIFail fill:#ffccbc,stroke:#e74c3c,stroke-width:2px

    Review -->|Approved + CI Green| Merge[Merged to Main<br/>🎉 Contribution Complete!]
    style Merge fill:#c8e6c9,stroke:#27ae60,stroke-width:3px

    Merge --> Thanks[Thank You!<br/>Listed in CONTRIBUTORS.md<br/>Mentioned in release notes]
    style Thanks fill:#c8e6c9,stroke:#27ae60,stroke-width:2px
```

---

## Contributor Journey

Your path from first contribution to maintainer:

```mermaid
timeline
    title Contributor Growth Path
    section First Steps
        Day 1 : Find an Issue
              : Read docs/contributing/
              : Fork repository
        Week 1 : Submit first PR
               : Fix a bug or improve docs
               : Learn TDD workflow
    section Regular Contributor
        Month 1 : Multiple PRs merged
                : Understand codebase structure
                : Help review other PRs
        Month 3 : Implement features
                : Contribute to design discussions
                : Mentor new contributors
    section Core Contributor
        Month 6 : Trusted reviewer
                : Design new features
                : Own a subsystem (e.g., search, indexing)
        Month 12 : Maintainer consideration
                 : Triage issues
                 : Guide project direction
```

**Key Milestones:**

| Stage | What You're Doing | Recognition |
|-------|-------------------|-------------|
| **First Timer** | Bug fixes, docs, small features | PR merged, listed in contributors |
| **Regular** | Multiple features, reviews | Mentioned in release notes |
| **Core** | Subsystem ownership, mentoring | Reviewer permissions |
| **Maintainer** | Project direction, releases | Full commit access |

**Growth Tips:**

- Start with issues labeled `good-first-issue`
- Ask questions in PR discussions
- Review others' PRs to learn patterns
- Own a feature from design to maintenance

---

## Contribution Areas

Looking for where to help? Check:

- [Open Issues](https://github.com/Aman-CERP/amanmcp/issues) - Bug fixes and features
- [CONTRIBUTING.md](../../CONTRIBUTING.md#priority-areas) - High-priority areas
- [Research](../research/) - Technical decisions (challenge or improve them!)

---

## Development Commands

```bash
# Build
make build

# Run tests
make test

# Run tests with race detector
make test-race

# Run linter
make lint

# Full CI check (run before PR)
make ci-check

# Install locally
make install-local
```

---

## Code Quality Standards

✅ **All tests pass** - `make test`
✅ **No race conditions** - `make test-race`
✅ **Linter clean** - `make lint`
✅ **Coverage ≥ 25%** - `make coverage`
✅ **CI passes** - `make ci-check`
✅ **Changelog entry** - `.aman-pm/changelog/unreleased.md`

---

## Questions?

- **General**: File an issue or ask in discussions
- **Code Review**: Tag maintainers in your PR
- **Architecture**: See [Architecture](../reference/architecture/architecture.md) or [Research](../research/)

---

## Related Documentation

- [CONTRIBUTING.md](../../CONTRIBUTING.md) - Main contribution guide
- [Architecture](../reference/architecture/architecture.md) - System design
- [Research](../research/) - Technical decisions
- [Concepts](../concepts/) - How systems work

---

## Documentation Structure

Navigate the documentation effectively:

```mermaid
graph TB
    Root[docs/]

    subgraph "Getting Started"
        GS1[README.md<br/>Quick Start]
        GS2[installation.md<br/>Setup Guide]
        GS3[quickstart.md<br/>First Steps]
        style GS1 fill:#c8e6c9,stroke:#27ae60,stroke-width:2px
        style GS2 fill:#c8e6c9,stroke:#27ae60,stroke-width:2px
        style GS3 fill:#c8e6c9,stroke:#27ae60,stroke-width:2px
    end

    subgraph "Contributing (You Are Here)"
        C1[contributing/README.md<br/>Overview]
        C2[code-conventions.md<br/>Go Patterns]
        C3[tdd-rationale.md<br/>Why Test-First]
        C4[testing-guide.md<br/>Validation Tests]
        style C1 fill:#e1f5ff,stroke:#3498db,stroke-width:3px
        style C2 fill:#e1f5ff,stroke:#3498db,stroke-width:2px
        style C3 fill:#e1f5ff,stroke:#3498db,stroke-width:2px
        style C4 fill:#e1f5ff,stroke:#3498db,stroke-width:2px
    end

    subgraph "Usage Guides"
        U1[guides/<br/>How-To Guides]
        U2[configuration-reference.md<br/>Config Options]
        U3[mcp-tools.md<br/>Tool Reference]
        style U1 fill:#fff9c4,stroke:#f57f17,stroke-width:2px
        style U2 fill:#fff9c4,stroke:#f57f17,stroke-width:2px
        style U3 fill:#fff9c4,stroke:#f57f17,stroke-width:2px
    end

    subgraph "Deep Dives"
        R1[reference/architecture/<br/>System Design]
        R2[reference/decisions/<br/>ADRs]
        R3[research/<br/>Experiments & Analysis]
        style R1 fill:#f3e5f5,stroke:#9b59b6,stroke-width:2px
        style R2 fill:#f3e5f5,stroke:#9b59b6,stroke-width:2px
        style R3 fill:#f3e5f5,stroke:#9b59b6,stroke-width:2px
    end

    subgraph "Concepts"
        D1[concepts/<br/>How Systems Work]
        D2[hybrid-search/<br/>BM25 + Semantic]
        D3[chunking.md<br/>Code Parsing]
        style D1 fill:#ffe0b2,stroke:#f39c12,stroke-width:2px
        style D2 fill:#ffe0b2,stroke:#f39c12,stroke-width:2px
        style D3 fill:#ffe0b2,stroke:#f39c12,stroke-width:2px
    end

    Root --> GS1
    Root --> C1
    Root --> U1
    Root --> R1
    Root --> D1

    GS1 --> GS2
    GS2 --> GS3

    C1 --> C2
    C1 --> C3
    C1 --> C4

    U1 --> U2
    U1 --> U3

    R1 --> R2
    R2 --> R3

    D1 --> D2
    D1 --> D3

    style Root fill:#e0e0e0,stroke:#424242,stroke-width:3px

    classDef highlight fill:#e1f5ff,stroke:#3498db,stroke-width:3px
```

**Quick Navigation:**

| I Want To... | Start Here |
|--------------|------------|
| **Contribute code** | [code-conventions.md](code-conventions.md) |
| **Understand TDD** | [tdd-rationale.md](tdd-rationale.md) |
| **Run tests** | [testing-guide.md](testing-guide.md) |
| **Learn architecture** | [Architecture](../reference/architecture/architecture.md) |
| **See design rationale** | [ADRs](../reference/decisions/) |
| **Understand search** | [Hybrid Search](../concepts/hybrid-search/) |
