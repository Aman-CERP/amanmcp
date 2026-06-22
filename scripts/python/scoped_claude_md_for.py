#!/usr/bin/env python3
"""scoped_claude_md_for.py — emit scoped CLAUDE.md citation blocks for spawn prompts."""

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

PREAMBLE = """\
# Scoped CLAUDE.mds for this work scope

The following scoped CLAUDE.md files document non-negotiable danger zones
for the paths you are about to touch. The runtime auto-loads these for the
LEAD session, but NOT for subagents — which is why this block exists.

You MUST read each in full BEFORE making any edit in its scope.
"""

EMPTY_BODY = "(no scoped CLAUDE.mds apply to the input paths.)\n"


def discover_scoped_claude_mds(paths: list[str], registry_path: Path) -> list[dict[str, Any]]:
    rules = rfp.load_registry(registry_path)
    return rfp.discover(paths, rules, type_filter={"scoped-claude-md"})


def emit_reference(matches: list[dict[str, Any]]) -> str:
    if not matches:
        return PREAMBLE + "\n" + EMPTY_BODY
    lines = [PREAMBLE, ""]
    for entry in matches:
        file = entry["file"]
        summary = entry["summary"]
        triggered_by = ", ".join(entry["matched_paths"])
        lines.append(f"@{file}")
        lines.append(f"  ({summary} — applies because you touch: {triggered_by})")
        lines.append("")
    return "\n".join(lines)


def emit_inline(matches: list[dict[str, Any]], repo_root: Path) -> str:
    if not matches:
        return PREAMBLE + "\n" + EMPTY_BODY
    parts = [PREAMBLE, ""]
    for entry in matches:
        file = entry["file"]
        summary = entry["summary"]
        triggered_by = ", ".join(entry["matched_paths"])
        path = repo_root / file
        parts.append("=" * 78)
        parts.append(f"FILE: {file}")
        parts.append(f"SUMMARY: {summary}")
        parts.append(f"APPLIES BECAUSE YOU TOUCH: {triggered_by}")
        parts.append("=" * 78)
        try:
            parts.append(path.read_text(encoding="utf-8"))
        except OSError as e:
            parts.append(f"[ERROR: could not read {file}: {e}]")
        parts.append("")
    return "\n".join(parts)


def emit_json(paths: list[str], matches: list[dict[str, Any]], inline: bool, repo_root: Path) -> str:
    out: dict[str, Any] = {
        "paths": list(paths),
        "scoped_claude_mds": [],
        "mode": "inline" if inline else "reference",
    }
    for entry in matches:
        rec = {
            "id": entry["id"],
            "file": entry["file"],
            "summary": entry["summary"],
            "applies_to": entry["applies_to"],
            "matched_paths": entry["matched_paths"],
        }
        if inline:
            try:
                rec["content"] = (repo_root / entry["file"]).read_text(encoding="utf-8")
            except OSError as e:
                rec["content"] = f"[ERROR: could not read {entry['file']}: {e}]"
        out["scoped_claude_mds"].append(rec)
    return json.dumps(out, indent=2)


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="scoped_claude_md_for",
        description="Emit scoped CLAUDE.md citation block for spawn prompts",
    )
    parser.add_argument("paths", nargs="*", help="Repo-relative paths the spawn will work on")
    parser.add_argument("--inline", action="store_true", help="Emit full file content")
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
    repo_root = Path(os.environ.get("REPO_ROOT", str(REPO_ROOT)))
    matches = discover_scoped_claude_mds(args.paths, registry_path)

    if args.json:
        print(emit_json(args.paths, matches, args.inline, repo_root))
    elif args.inline:
        print(emit_inline(matches, repo_root))
    else:
        print(emit_reference(matches))
    return 0


if __name__ == "__main__":
    sys.exit(main())