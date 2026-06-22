"""Shared YAML loading for AmanPM Python tooling (PyYAML with Ruby fallback)."""

from __future__ import annotations

import json
import shutil
import subprocess
from pathlib import Path
from typing import Any


class YAMLLoadError(RuntimeError):
    pass


def load_yaml(path: Path) -> Any:
    try:
        import yaml  # type: ignore[import-untyped]
    except ModuleNotFoundError:
        return _load_yaml_with_ruby(path)

    try:
        with path.open("r", encoding="utf-8") as fh:
            return yaml.safe_load(fh) or {}
    except Exception as exc:
        raise YAMLLoadError(f"failed to parse YAML {path}: {exc}") from exc


def _load_yaml_with_ruby(path: Path) -> Any:
    ruby = shutil.which("ruby")
    if not ruby:
        raise YAMLLoadError(
            "PyYAML is not installed and Ruby is unavailable; install PyYAML or Ruby to parse YAML"
        )

    script = (
        "require 'yaml'; require 'json'; require 'date'; require 'time'; "
        "data = YAML.safe_load(File.read(ARGV[0]), "
        "permitted_classes: [Date, Time, Symbol], aliases: true); "
        "puts JSON.generate(data || {})"
    )
    proc = subprocess.run(
        [ruby, "-e", script, str(path)],
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    if proc.returncode != 0:
        raise YAMLLoadError(f"failed to parse YAML {path}: {proc.stderr.strip()}")
    return json.loads(proc.stdout)