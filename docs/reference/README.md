# Reference Documentation

Complete technical reference for AmanMCP. This section contains authoritative, fact-based documentation for CLI commands, configuration options, error codes, and system architecture.

---

## Quick Access

| I Need... | Document |
|-----------|----------|
| CLI commands | [commands.md](commands.md) |
| Configuration options | [configuration.md](configuration.md) |
| Error codes | [error-codes.md](error-codes.md) |
| Technical terminology | [glossary.md](glossary.md) |
| Architecture overview | [architecture/](architecture/) |
| Design patterns | [architecture-patterns.md](architecture-patterns.md) |
| All technical decisions | [architecture-decisions-summary.md](architecture-decisions-summary.md) |

---

## Document Index

### Core Reference

| Document | Purpose | Audience |
|----------|---------|----------|
| [commands.md](commands.md) | All CLI commands with options and examples | Users |
| [configuration.md](configuration.md) | `.amanmcp.yaml` options and environment variables | Users, Developers |
| [error-codes.md](error-codes.md) | Error code catalog with troubleshooting steps | Users |
| [glossary.md](glossary.md) | Technical terminology definitions | All |

### Architecture & Design

| Document | Purpose | Audience |
|----------|---------|----------|
| [architecture/](architecture/) | System design, data flow, components | Developers |
| [architecture-patterns.md](architecture-patterns.md) | Design patterns used in AmanMCP | Developers |
| [architecture-decisions-summary.md](architecture-decisions-summary.md) | Summary of all technical decisions | Developers, Researchers |

---

## Reference by Topic

### CLI Usage

```bash
# Initialize a project
amanmcp init

# Search your codebase
amanmcp search "authentication"

# Check system health
amanmcp doctor

# View status
amanmcp status
```

Full command reference: [commands.md](commands.md)

### Configuration

```yaml
# .amanmcp.yaml example
embedding:
  backend: ollama          # or: mlx, static
  model: nomic-embed-text  # default model

search:
  bm25_weight: 0.35        # keyword search weight
  semantic_weight: 0.65    # semantic search weight

exclude:
  - "vendor/**"
  - "node_modules/**"
```

Full configuration reference: [configuration.md](configuration.md)

### Troubleshooting

When you see an error:
1. Note the error code (e.g., `E1001`)
2. Look it up in [error-codes.md](error-codes.md)
3. Follow the troubleshooting steps
4. Run `amanmcp doctor` if issues persist

---

## Navigation Map

```mermaid
graph TB
    Ref[Reference Docs] --> CLI[commands.md<br/>CLI Reference]
    Ref --> Config[configuration.md<br/>Config Options]
    Ref --> Errors[error-codes.md<br/>Error Catalog]
    Ref --> Glossary[glossary.md<br/>Terminology]
    Ref --> Arch[architecture/<br/>System Design]
    Ref --> Patterns[architecture-patterns.md<br/>Design Patterns]
    Ref --> Decisions[architecture-decisions-summary.md<br/>Technical Decisions]

    CLI --> Usage{User Task}
    Config --> Usage
    Errors --> Usage

    Usage -->|"How do I...?"| Guides[../guides/]
    Usage -->|"Why does...?"| Concepts[../concepts/]
    Usage -->|"I want to contribute"| Contributing[../contributing/]

    Arch --> DeepDive{Deep Dive}
    Patterns --> DeepDive
    Decisions --> DeepDive

    DeepDive -->|Research| Research[../research/]
    DeepDive -->|Articles| Articles[../articles/]

    style Ref fill:#e1f5ff,stroke:#01579b,stroke-width:2px
    style CLI fill:#c8e6c9,stroke:#27ae60,stroke-width:2px
    style Config fill:#c8e6c9,stroke:#27ae60,stroke-width:2px
    style Errors fill:#c8e6c9,stroke:#27ae60,stroke-width:2px
    style Glossary fill:#c8e6c9,stroke:#27ae60,stroke-width:2px
    style Arch fill:#ffe0b2,stroke:#f39c12,stroke-width:2px
    style Patterns fill:#ffe0b2,stroke:#f39c12,stroke-width:2px
    style Decisions fill:#ffe0b2,stroke:#f39c12,stroke-width:2px
```

---

## Related Documentation

| Need | Go To |
|------|-------|
| Step-by-step guides | [guides/](../guides/) |
| How systems work | [concepts/](../concepts/) |
| Technical research | [research/](../research/) |
| Contributing | [contributing/](../contributing/) |

---

*Reference docs are fact-based and complete. For task-oriented help, see [Guides](../guides/).*
