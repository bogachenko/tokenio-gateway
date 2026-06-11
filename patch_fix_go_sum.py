#!/usr/bin/env python3
from __future__ import annotations

import subprocess
import sys
from pathlib import Path

REPO = Path.home() / "src/github.com/bogachenko/tokenio-gateway"

def run(cmd: list[str]) -> int:
    print(f"$ {' '.join(cmd)}")
    p = subprocess.run(cmd, cwd=REPO, text=True)
    return p.returncode

def main() -> int:
    go_mod = REPO / "go.mod"
    if not go_mod.exists():
        print(f"ERROR: go.mod not found: {go_mod}", file=sys.stderr)
        return 1

    if "module github.com/bogachenko/tokenio-gateway" not in go_mod.read_text():
        print(f"ERROR: wrong repo: {REPO}", file=sys.stderr)
        return 1

    if run(["go", "mod", "tidy"]) != 0:
        return 1

    print()
    run(["git", "diff", "--", "go.mod", "go.sum"])

    print()
    run(["grep", "-n", "golang.org/x/time", "go.mod", "go.sum"])

    print()
    return run(["go", "test", "./..."])

if __name__ == "__main__":
    raise SystemExit(main())
