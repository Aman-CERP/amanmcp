"""Gitwildmatch-style path matching without external dependencies."""

from __future__ import annotations

import fnmatch
import re


def normalize_path(path: str) -> str:
    return path.replace("\\", "/").lstrip("./")


def glob_match(path: str, pattern: str) -> bool:
    """Return True if repo-relative path matches a gitignore-style glob."""
    path = normalize_path(path)
    pattern = normalize_path(pattern)

    if pattern == "**":
        return True

    if "**" not in pattern:
        return fnmatch.fnmatch(path, pattern)

    # Convert gitwildmatch with ** to anchored regex.
    regex_parts: list[str] = ["^"]
    i = 0
    while i < len(pattern):
        ch = pattern[i]
        if ch == "*":
            if i + 1 < len(pattern) and pattern[i + 1] == "*":
                regex_parts.append(".*")
                i += 2
                if i < len(pattern) and pattern[i] == "/":
                    i += 1
                continue
            regex_parts.append("[^/]*")
            i += 1
            continue
        if ch == "?":
            regex_parts.append("[^/]")
            i += 1
            continue
        regex_parts.append(re.escape(ch))
        i += 1
    regex_parts.append("$")
    return re.match("".join(regex_parts), path) is not None


def matches_any(path: str, patterns: list[str]) -> bool:
    return any(glob_match(path, pat) for pat in patterns)