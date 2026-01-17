# Guides: How-To Documentation

Task-based guides for getting things done with AmanMCP. Each guide solves a specific problem or shows you how to accomplish a task.

---

## Guide Navigation Map

```mermaid
graph TD
    Start([New to AmanMCP?]) --> FirstTime[First-Time User Guide]
    Start --> Homebrew{Using<br/>Homebrew?}
    Homebrew -->|Yes| HomebrewGuide[Homebrew Setup]
    Homebrew -->|No| FirstTime

    FirstTime --> Installed{Successfully<br/>Installed?}
    Installed -->|Yes| Config[Configuration & Optimization]
    Installed -->|No| Troubleshoot[Troubleshooting Section]

    Config --> AppleSilicon{Have Apple<br/>Silicon Mac?}
    AppleSilicon -->|Yes| MLXSetup[MLX Setup Guide]
    AppleSilicon -->|No| Features[Feature Guides]

    MLXSetup --> CompareBackends[Backend Switching Guide]
    Features --> AutoReindex[Auto-Reindexing Guide]
    Features --> Thermal[Thermal Management]
    CompareBackends --> Features

    AutoReindex --> Advanced([Ready for Advanced Use])
    Thermal --> Advanced

    style Start fill:#e1f5ff,stroke:#01579b,stroke-width:2px
    style Advanced fill:#c8e6c9,stroke:#2e7d32,stroke-width:2px
    style Installed fill:#fff9c4,stroke:#f57f17,stroke-width:2px
    style AppleSilicon fill:#fff9c4,stroke:#f57f17,stroke-width:2px
    style Homebrew fill:#fff9c4,stroke:#f57f17,stroke-width:2px
    style FirstTime fill:#b3e5fc,stroke:#0277bd,stroke-width:2px
    style Config fill:#b3e5fc,stroke:#0277bd,stroke-width:2px
    style Features fill:#b3e5fc,stroke:#0277bd,stroke-width:2px
```

---

## Getting Started

| Guide | What You'll Learn | When to Use |
|-------|-------------------|-------------|
| [First-Time User Guide](first-time-user-guide.md) | Complete walkthrough from install to first search | You're new to AmanMCP |
| [Homebrew Setup](homebrew-setup-guide.md) | Installing via Homebrew on macOS | You prefer Homebrew package manager |

---

## Configuration & Optimization

| Guide | What You'll Learn | When to Use |
|-------|-------------------|-------------|
| [MLX Setup](mlx-setup.md) | Using MLX embeddings on Apple Silicon | You have a Mac and want faster embeddings |
| [Backend Switching](backend-switching.md) | Switching between Ollama and MLX | You want to try different embedding backends |
| [Auto-Reindexing](auto-reindexing.md) | Automatic index updates on file changes | You want seamless background updates |
| [Thermal Management](thermal-management.md) | CPU temperature optimization | Your laptop runs hot during indexing |

---

## By Use Case

### "I want to optimize performance"
1. [MLX Setup](mlx-setup.md) - ~1.7x faster on Apple Silicon
2. [Thermal Management](thermal-management.md) - Reduce CPU heat
3. [Backend Switching](backend-switching.md) - Compare backends

### "I'm setting up for the first time"
1. [First-Time User Guide](first-time-user-guide.md) - Complete walkthrough
2. [Homebrew Setup](homebrew-setup-guide.md) - If using Homebrew

### "I want automatic updates"
1. [Auto-Reindexing](auto-reindexing.md) - File watcher setup

---

## User Journey Map

```mermaid
---
config:
  layout: elk
  theme: neo
  look: handDrawn
---
flowchart TB
 subgraph Beginner["Beginner (Day 1)"]
        B1["Install AmanMCP"]
        B2["Initialize Project"]
        B3["First Index"]
        B4["First Search"]
  end
 subgraph Intermediate["Intermediate (Week 1)"]
        I1["Understand Backend Options"]
        I2["Try MLX for Speed"]
        I3["Enable Auto-Reindexing"]
        I4["Learn Search Patterns"]
  end
 subgraph Advanced["Advanced (Week 2+) (Optional)"]
        A1["Optimize Config"]
        A2["Manage Thermal Load"]
        A3["Switch Backends"]
        A4["Fine-tune Exclusions"]
  end
    B1 --> B2
    B2 --> B3
    B3 --> B4
    I1 --> I2
    I2 --> I3
    I3 --> I4
    A1 --> A2
    A2 --> A3
    A3 --> A4
    B4 -- 1 Week --> I1
    I4 -- 2 Weeks --> A1

    style Beginner fill:#e1f5ff,stroke:#01579b,stroke-width:2px
    style Intermediate fill:#fff9c4,stroke:#f57f17,stroke-width:2px
    style Advanced fill:#c8e6c9,stroke:#2e7d32,stroke-width:2px
```

---

## Related Documentation

- [Concepts](../concepts/) - Understand how systems work
- [Reference](../reference/) - Command and config reference
- [Getting Started](../getting-started/) - Quick start installation

---

## Contributing Guides

Found a task that needs a guide? Want to improve existing guides?
1. Check [open issues](https://github.com/Aman-CERP/amanmcp/issues?q=is%3Aissue+is%3Aopen+label%3Adocumentation)
2. File a new issue describing the guide needed
3. Submit a PR with your guide

Good guides:
- ✅ Solve a specific task or problem
- ✅ Include step-by-step instructions
- ✅ Show expected output
- ✅ Link to related concepts for deeper learning
