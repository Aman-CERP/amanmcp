# Getting Started with AmanMCP

Your path from zero to productive with AmanMCP. This section contains everything you need to get started.

---

## Start Here

| Step | Document | What You'll Learn |
|------|----------|-------------------|
| 1 | [Introduction](introduction.md) | What AmanMCP is, why it exists, how it solves the RAG problem |
| 2 | [System Requirements](SYSTEM_REQUIREMENTS.md) | Hardware, software, and platform requirements |
| 3 | [First-Time User Guide](../guides/first-time-user-guide.md) | Step-by-step installation and first search |

---

## Reading Flow

```mermaid
graph TD
    Start([Getting Started]) --> Intro[Introduction<br/>What is AmanMCP?]
    Intro --> SysReq[System Requirements<br/>Can my system run it?]
    SysReq --> Decision{Ready to install?}

    Decision -->|Yes| Install[First-Time User Guide<br/>Install and configure]
    Decision -->|Need more info| Concepts[Concepts<br/>How search works]

    Install --> FirstSearch[Your First Search<br/>Try it out]
    FirstSearch --> NextSteps{What's next?}

    NextSteps -->|Optimize| Guides[Guides<br/>MLX, backends, etc.]
    NextSteps -->|Understand| Concepts
    NextSteps -->|Contribute| Contributing[Contributing<br/>Help build AmanMCP]

    Concepts --> Install

    style Start fill:#e1f5ff,stroke:#01579b,stroke-width:2px
    style Install fill:#c8e6c9,stroke:#27ae60,stroke-width:2px
    style FirstSearch fill:#c8e6c9,stroke:#27ae60,stroke-width:2px
    style NextSteps fill:#fff9c4,stroke:#f57f17,stroke-width:2px
```

---

## Quick Install (TL;DR)

If you want to get started immediately:

```bash
# Install via Homebrew (macOS)
brew install amanmcp

# Or build from source
git clone https://github.com/Aman-CERP/amanmcp
cd amanmcp
make build && make install-local

# Initialize your project
cd your-project
amanmcp init

# Start searching
amanmcp search "authentication"
```

**Need more details?** Follow the [First-Time User Guide](../guides/first-time-user-guide.md).

---

## Prerequisites Checklist

Before installing, ensure you have:

| Requirement | Check Command | Expected |
|-------------|---------------|----------|
| macOS 12+ / Linux / Windows 10+ | `uname -a` | Your OS version |
| 16GB+ RAM recommended | `free -h` (Linux) or Activity Monitor | Available memory |
| 100MB+ free disk space | `df -h .` | Free space in project directory |

**Verify everything:** After install, run `amanmcp doctor` to check your system.

---

## What's in This Section

### [Introduction](introduction.md)
The "why" behind AmanMCP. Understand the problem it solves: friction-free code search for AI assistants. Read this if you want to understand what makes AmanMCP different from grep or cloud-based RAG tools.

### [System Requirements](SYSTEM_REQUIREMENTS.md)
Detailed hardware and software requirements. Memory sizing guide, platform compatibility, and troubleshooting. Check this if you're unsure whether your system can run AmanMCP.

---

## After Getting Started

Once you're up and running:

| Goal | Go To |
|------|-------|
| Learn how search works | [Concepts](../concepts/) |
| Optimize for Apple Silicon | [MLX Setup Guide](../guides/mlx-setup.md) |
| Enable auto-reindexing | [Auto-Reindexing Guide](../guides/auto-reindexing.md) |
| Contribute code | [Contributing](../contributing/) |

---

## Need Help?

- **Something not working?** Run `amanmcp doctor`
- **Configuration issues?** See [Reference](../reference/)
- **Have a question?** [GitHub Discussions](https://github.com/Aman-CERP/amanmcp/discussions)

---

*Next: [Introduction](introduction.md) - Understand what AmanMCP is and why it exists*
