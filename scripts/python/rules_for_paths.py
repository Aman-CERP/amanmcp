#!/usr/bin/env python3
"""rules_for_paths.py — rule-discovery primitive for AmanMCP spawn orchestration.

For a list of repository paths, emit the rules, scoped CLAUDE.mds, harness
gates, and pre-commit hooks that apply. Reads `.claude/rule-registry.yaml`.

Usage:
    rules_for_paths.py [--json] [--type=TYPE | --types=T1,T2,...] PATH [PATH...]
"""
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

from glob_match import matches_any  # noqa: E402
from yaml_util import YAMLLoadError, load_yaml  # noqa: E402


REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_REGISTRY = REPO_ROOT / ".claude" / "rule-registry.yaml"
VALID_TYPES = {"scoped-claude-md", "project-rule", "harness-gate", "precommit-hook"}
REQUIRED_FIELDS = {"id", "type", "applies_to", "summary"}


def fail(msg: str, code: int = 2) -> None:
    print(f"rules_for_paths: {msg}", file=sys.stderr)
    sys.exit(code)


def load_registry(path: Path) -> list[dict[str, Any]]:
    if not path.exists():
        fail(f"registry not found: {path}", code=2)
    try:
        data = load_yaml(path)
    except YAMLLoadError as e:
        fail(f"registry malformed at {path}: {e}", code=2)

    if not isinstance(data, dict) or "rules" not in data:
        fail(f"registry missing top-level 'rules' key: {path}", code=2)
    rules = data["rules"]
    if not isinstance(rules, list):
        fail(f"registry 'rules' must be a list: {path}", code=2)

    for idx, rule in enumerate(rules):
        if not isinstance(rule, dict):
            fail(f"rule[{idx}] is not a mapping: {path}", code=2)
        missing = REQUIRED_FIELDS - rule.keys()
        if missing:
            fail(f"rule[{idx}] missing required field(s) {sorted(missing)}: {path}", code=2)
        if rule["type"] not in VALID_TYPES:
            fail(
                f"rule[{idx}] id={rule.get('id', '?')} has invalid type "
                f"{rule['type']!r}; valid: {sorted(VALID_TYPES)}",
                code=2,
            )
        if not isinstance(rule["applies_to"], list) or not rule["applies_to"]:
            fail(f"rule[{idx}] applies_to must be a non-empty list", code=2)
        if "file" not in rule and "target" not in rule:
            fail(
                f"rule[{idx}] id={rule.get('id', '?')} must have either "
                "`file` (for rules/CLAUDE.mds) or `target` (for gates/hooks)",
                code=2,
            )
    return rules


def discover(
    paths: list[str],
    rules: list[dict[str, Any]],
    type_filter: set[str] | None,
) -> list[dict[str, Any]]:
    results: list[dict[str, Any]] = []
    for rule in rules:
        if type_filter is not None and rule["type"] not in type_filter:
            continue
        matched = [p for p in paths if matches_any(p, rule["applies_to"])]
        if not matched:
            continue
        out = dict(rule)
        out["matched_paths"] = matched
        results.append(out)
    return results


TYPE_LABELS = {
    "scoped-claude-md": "SCOPED CLAUDE.MDs (auto-loaded for these paths)",
    "project-rule": "PROJECT RULES (.claude/rules/)",
    "harness-gate": "HARNESS GATES (will run on PR)",
    "precommit-hook": "PRE-COMMIT HOOKS (will run on commit)",
}

TYPE_ORDER = ["scoped-claude-md", "project-rule", "harness-gate", "precommit-hook"]


def emit_text(paths: list[str], matches_: list[dict[str, Any]]) -> str:
    lines: list[str] = []
    lines.append("=== Rules applicable to:")
    for p in paths:
        lines.append(f"  {p}")
    lines.append("")

    by_type: dict[str, list[dict[str, Any]]] = {}
    for m in matches_:
        by_type.setdefault(m["type"], []).append(m)

    for rtype in TYPE_ORDER:
        rules = by_type.get(rtype, [])
        if not rules:
            continue
        lines.append(TYPE_LABELS[rtype])
        lines.append("-" * len(TYPE_LABELS[rtype]))
        for r in rules:
            anchor = r.get("file") or r.get("target") or r["id"]
            globs = ", ".join(r["applies_to"])
            lines.append(f"  - {anchor}")
            lines.append(f"    {r['summary']}")
            if rtype != "scoped-claude-md":
                lines.append(f"    applies_to: {globs}")
            children = r.get("children")
            if children:
                lines.append(f"    children: {', '.join(children)}")
        lines.append("")

    if not matches_:
        lines.append("(no matching rules — only always-on rules apply)")

    return "\n".join(lines)


def emit_json(paths: list[str], matches_: list[dict[str, Any]]) -> str:
    return json.dumps(
        {
            "paths": list(paths),
            "matches": matches_,
            "summary": {
                "total": len(matches_),
                "by_type": {t: sum(1 for m in matches_ if m["type"] == t) for t in VALID_TYPES},
            },
        },
        indent=2,
        default=str,
    )


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="rules_for_paths",
        description="Discover rules applicable to a set of paths",
    )
    parser.add_argument("paths", nargs="*", help="Paths to query")
    parser.add_argument("--json", action="store_true", help="Emit JSON")
    type_group = parser.add_mutually_exclusive_group()
    type_group.add_argument("--type", choices=sorted(VALID_TYPES), help="Filter to one rule type")
    type_group.add_argument("--types", help=f"CSV of rule types ({','.join(sorted(VALID_TYPES))})")
    return parser.parse_args(argv)


def resolve_type_filter(args: argparse.Namespace) -> set[str] | None:
    if args.type:
        return {args.type}
    if args.types:
        wanted = {t.strip() for t in args.types.split(",") if t.strip()}
        invalid = wanted - VALID_TYPES
        if invalid:
            fail(f"invalid type(s) in --types: {sorted(invalid)}", code=3)
        return wanted
    return None


def main(argv: list[str] | None = None) -> int:
    if argv is None:
        argv = sys.argv[1:]
    try:
        args = parse_args(argv)
    except SystemExit as e:
        return 3 if e.code != 0 else 0

    registry_path = Path(os.environ.get("RULE_REGISTRY", str(DEFAULT_REGISTRY)))
    rules = load_registry(registry_path)
    type_filter = resolve_type_filter(args)

    if not args.paths:
        msg = "no paths provided — nothing to discover"
        if args.json:
            print(json.dumps({"paths": [], "matches": [], "warning": msg}))
        else:
            print(msg, file=sys.stderr)
        return 0

    matches_ = discover(args.paths, rules, type_filter)
    if args.json:
        print(emit_json(args.paths, matches_))
    else:
        print(emit_text(args.paths, matches_))
    return 0


if __name__ == "__main__":
    sys.exit(main())