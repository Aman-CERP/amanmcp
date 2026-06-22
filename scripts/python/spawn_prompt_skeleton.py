#!/usr/bin/env python3
"""spawn_prompt_skeleton.py — paste-ready spawn-prompt rule-citation generator."""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path
from typing import Any

SCRIPT_DIR = Path(__file__).resolve().parent
if str(SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPT_DIR))

import rules_for_paths as rfp  # noqa: E402


REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_REGISTRY = REPO_ROOT / ".claude" / "rule-registry.yaml"
VALID_PROFILES = {"code-mod", "research", "review", "pm-mechanical"}

PROFILE_PREAMBLES = {
    "code-mod": """\
# Spawn-Prompt Rule-Citation Block (profile: code-mod)

You are a coding agent. Rules, scoped CLAUDE.mds, and harness gates below
encode discipline from prior incidents — treat as hard, not advisory.
MUST: (1) read scoped CLAUDE.mds in full before editing in their scope;
(2) treat project rules as hard rules; (3) run the DoD command before
reporting complete.
""",
    "research": """\
# Spawn-Prompt Rule-Citation Block (profile: research)

You are a research/exploration agent. You will read code but not edit it.
The following rules + scoped CLAUDE.mds give you context for the area you
are exploring; treat them as orientation, not edit-time discipline.
""",
    "review": """\
# Spawn-Prompt Rule-Citation Block (profile: review)

You are a code-review agent. The following rules, scoped CLAUDE.mds, and
harness gates encode the discipline the code under review SHOULD have
respected. Use them as the contract you are reviewing against.
""",
    "pm-mechanical": """\
# Spawn-Prompt Rule-Citation Block (profile: pm-mechanical)

You are a PM-mechanical agent (closed-form work). Light citation only.
Apply the .aman-pm/CLAUDE.md discipline if PM paths are in scope.
""",
}


def select_for_profile(matches: list[dict[str, Any]], profile: str) -> dict[str, list[dict[str, Any]]]:
    by_type: dict[str, list[dict[str, Any]]] = {
        "scoped-claude-md": [],
        "project-rule": [],
        "harness-gate": [],
        "precommit-hook": [],
    }
    for m in matches:
        if m["type"] in by_type:
            by_type[m["type"]].append(m)

    if profile == "research":
        by_type["harness-gate"] = []
        by_type["precommit-hook"] = []

    if profile == "pm-mechanical":
        by_type["scoped-claude-md"] = [
            m for m in by_type["scoped-claude-md"] if m.get("file") == ".aman-pm/CLAUDE.md"
        ]
        by_type["project-rule"] = [
            m for m in by_type["project-rule"] if m.get("id") in {"rule-changelog", "rule-agent-patterns"}
        ]
        by_type["harness-gate"] = []
        by_type["precommit-hook"] = []

    return by_type


def emit_scoped_claude_md_section(matches: list[dict[str, Any]]) -> str:
    if not matches:
        return ""
    lines = ["## Scoped CLAUDE.mds (read each before editing in its scope)\n"]
    for m in matches:
        lines.append(f"- `@{m['file']}` — {m['summary']}")
    lines.append("")
    return "\n".join(lines)


def emit_project_rules_section(matches: list[dict[str, Any]]) -> str:
    if not matches:
        return ""
    lines = ["## Project rules (.claude/rules/) — hard rules, not advisory\n"]
    for m in matches:
        lines.append(f"- `{m['file']}` — {m['summary']}")
    lines.append("")
    return "\n".join(lines)


def emit_harness_gates_section(matches: list[dict[str, Any]]) -> str:
    if not matches:
        return ""
    lines = ["## Harness gates (run locally before reporting complete)\n"]
    for m in matches:
        target = m.get("target", "?")
        children = m.get("children")
        if children:
            lines.append(f"- `make {target}` — {m['summary']} (wraps: {', '.join(children)})")
        else:
            lines.append(f"- `make {target}` — {m['summary']}")
    lines.append("")
    return "\n".join(lines)


def emit_definition_of_done_section(profile: str) -> str:
    if profile == "research":
        return ""
    lines = ["## Definition of done\n"]
    lines.append(
        "Run BEFORE reporting complete; cite the verification record path "
        "in your final message:\n"
    )
    lines.append("```")
    lines.append("make verify-feature-complete-with-gates ID=<your-work-item-id>")
    lines.append("```")
    lines.append("")
    lines.append(
        "Record lands at `.aman-pm/sprints/active/verifications/<id>.yaml` "
        "when a sprint is active, otherwise "
        "`.aman-pm/audits/evidence/verification-records/<id>.yaml`.\n"
    )
    if profile in {"code-mod", "review"}:
        guide = ".claude/guides/engineering/premium-engineering-standard.md"
        lines.append(
            f"**Premium Engineering Standard:** read `{guide}` before "
            "writing/modifying code; re-read at refactor + review phases.\n"
        )
    return "\n".join(lines)


def emit_paths_section(paths: list[str]) -> str:
    if not paths:
        return ""
    lines = ["## Paths in scope\n"]
    for p in paths:
        lines.append(f"- `{p}`")
    lines.append("")
    return "\n".join(lines)


def emit_text(paths: list[str], profile: str, by_type: dict[str, list[dict[str, Any]]]) -> str:
    parts = [PROFILE_PREAMBLES[profile]]
    parts.append(emit_paths_section(paths))
    parts.append(emit_scoped_claude_md_section(by_type.get("scoped-claude-md", [])))
    parts.append(emit_project_rules_section(by_type.get("project-rule", [])))
    if profile != "research":
        parts.append(emit_harness_gates_section(by_type.get("harness-gate", [])))
    parts.append(emit_definition_of_done_section(profile))
    return "\n".join(p for p in parts if p)


def emit_json(paths: list[str], profile: str, by_type: dict[str, list[dict[str, Any]]]) -> str:
    return json.dumps(
        {
            "paths": list(paths),
            "profile": profile,
            "scoped_claude_mds": by_type.get("scoped-claude-md", []),
            "rules_by_type": by_type,
            "harness_gates": by_type.get("harness-gate", []),
            "definition_of_done": {
                "command": "make verify-feature-complete-with-gates ID=<your-work-item-id>",
                "record_path_template": ".aman-pm/sprints/active/verifications/<id>.yaml",
                "skip_for_profile": profile == "research",
            },
        },
        indent=2,
        default=str,
    )


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="spawn_prompt_skeleton",
        description="Generate paste-ready spawn-prompt rule-citation block",
    )
    parser.add_argument("paths", nargs="*", help="Repo-relative paths the spawn will work on")
    parser.add_argument(
        "--profile",
        default="code-mod",
        choices=sorted(VALID_PROFILES),
        help="Spawn-target type (default: code-mod)",
    )
    parser.add_argument("--json", action="store_true", help="Emit machine-readable JSON")
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    if argv is None:
        argv = sys.argv[1:]
    try:
        args = parse_args(argv)
    except SystemExit as e:
        return 3 if e.code != 0 else 0

    registry_path = Path(os.environ.get("RULE_REGISTRY", str(DEFAULT_REGISTRY)))
    rules = rfp.load_registry(registry_path)

    if not args.paths:
        if args.json:
            print(json.dumps({
                "paths": [],
                "profile": args.profile,
                "warning": "no paths supplied — specify paths the spawn will work on",
            }))
        else:
            print(PROFILE_PREAMBLES[args.profile])
            print("\n(no paths supplied — specify paths the spawn will work on)\n")
        return 0

    matches = rfp.discover(args.paths, rules, type_filter=None)
    by_type = select_for_profile(matches, args.profile)

    if args.json:
        print(emit_json(args.paths, args.profile, by_type))
    else:
        print(emit_text(args.paths, args.profile, by_type))
    return 0


if __name__ == "__main__":
    sys.exit(main())