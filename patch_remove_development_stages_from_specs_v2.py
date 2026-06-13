#!/usr/bin/env python3
from __future__ import annotations

import argparse
import difflib
import re
import subprocess
import sys
from pathlib import Path

MODULE = "module github.com/bogachenko/tokenio-gateway"

STAGE_RE = re.compile(r"\bstage\s+\d+[a-z]?\b", re.IGNORECASE)
HEADING_RE = re.compile(r"^(#{1,6})\s+(.*?)(?:\r?\n)?$")


def fail(message: str) -> None:
    raise RuntimeError(message)


def run(command: list[str], cwd: Path) -> None:
    print("+", " ".join(command), flush=True)
    completed = subprocess.run(command, cwd=cwd, text=True)
    if completed.returncode != 0:
        fail(f"command failed with exit code {completed.returncode}: {' '.join(command)}")


def remove_stage_sections(text: str, path: Path) -> tuple[str, list[str]]:
    lines = text.splitlines(keepends=True)
    out: list[str] = []
    removed: list[str] = []
    i = 0

    while i < len(lines):
        match = HEADING_RE.match(lines[i])
        if match and STAGE_RE.search(match.group(2)):
            level = len(match.group(1))
            removed.append(lines[i].strip())
            i += 1

            while i < len(lines):
                next_heading = HEADING_RE.match(lines[i])
                if next_heading and len(next_heading.group(1)) <= level:
                    break
                i += 1

            # Remove a separator immediately preceding the deleted section,
            # but only when it is isolated by blank lines.
            while out and out[-1].strip() == "":
                out.pop()
            if out and out[-1].strip() == "---":
                out.pop()
            if out and out[-1].strip() != "":
                out.append("\n")
            continue

        out.append(lines[i])
        i += 1

    # Stage markers outside stage-named sections are roadmap/history residue.
    filtered: list[str] = []
    for line in out:
        if STAGE_RE.search(line):
            removed.append(line.strip())
            continue
        filtered.append(line)

    # Preserve normal Markdown content while enforcing exactly one newline
    # at EOF. This prevents git diff --check from reporting a blank line at EOF
    # after a trailing stage-specific section is removed.
    updated = "".join(filtered).rstrip("\r\n") + "\n"
    if STAGE_RE.search(updated):
        fail(f"{path}: stage marker remains after cleanup")

    return updated, removed


def unified_diff(path: Path, before: str, after: str) -> str:
    return "".join(
        difflib.unified_diff(
            before.splitlines(keepends=True),
            after.splitlines(keepends=True),
            fromfile=f"a/{path.as_posix()}",
            tofile=f"b/{path.as_posix()}",
        )
    )


def main() -> int:
    parser = argparse.ArgumentParser(
        description=(
            "Remove development-stage markers and stage-specific acceptance "
            "sections from Tokenio product specifications."
        )
    )
    parser.add_argument(
        "repo",
        nargs="?",
        default=".",
        help="path to tokenio-gateway repository (default: current directory)",
    )
    parser.add_argument(
        "--skip-tests",
        action="store_true",
        help="skip go test ./...; documentation verification still runs",
    )
    args = parser.parse_args()

    repo = Path(args.repo).resolve()
    go_mod = repo / "go.mod"
    if not go_mod.is_file() or MODULE not in go_mod.read_text(encoding="utf-8"):
        fail(f"{repo} is not the expected tokenio-gateway repository")

    spec_dir = repo / "docs/spec"
    if not spec_dir.is_dir():
        fail("docs/spec directory not found")

    spec_files = sorted(spec_dir.glob("*.md"))
    if not spec_files:
        fail("no specification files found")

    originals = {
        path: path.read_text(encoding="utf-8")
        for path in spec_files
    }

    changed: list[Path] = []
    removed_by_file: dict[Path, list[str]] = {}

    try:
        for path in spec_files:
            before = originals[path]
            after, removed = remove_stage_sections(before, path.relative_to(repo))
            if after != before:
                path.write_text(after, encoding="utf-8")
                changed.append(path)
                removed_by_file[path] = removed

        if not changed:
            print("OK: no development-stage markers found in docs/spec")
            return 0

        residual: list[str] = []
        for path in spec_files:
            for line_number, line in enumerate(
                path.read_text(encoding="utf-8").splitlines(),
                start=1,
            ):
                if STAGE_RE.search(line):
                    residual.append(
                        f"{path.relative_to(repo)}:{line_number}:{line.strip()}"
                    )

        if residual:
            fail(
                "stage markers remain in specifications:\n"
                + "\n".join(residual)
            )

        print("\n--- REMOVED STAGE-SPECIFIC CONTENT ---")
        for path in changed:
            print(path.relative_to(repo))
            for item in removed_by_file[path]:
                print(f"  - {item}")

        print("\n--- PATCH DIFF ---")
        for path in changed:
            before = originals[path]
            after = path.read_text(encoding="utf-8")
            sys.stdout.write(
                unified_diff(path.relative_to(repo), before, after)
            )

        # Product specs must not contain development stage markers.
        grep = subprocess.run(
            [
                "grep",
                "-RniE",
                r"\bstage[[:space:]]+[0-9]+[A-Za-z]?\b",
                "docs/spec",
            ],
            cwd=repo,
            text=True,
            capture_output=True,
        )
        if grep.returncode == 0:
            fail("verification grep found stage markers:\n" + grep.stdout)
        if grep.returncode != 1:
            fail("verification grep failed:\n" + grep.stderr)

        run(["git", "diff", "--check", "--", "docs/spec"], repo)

        if not args.skip_tests:
            run(["go", "test", "./..."], repo)

        print("\n--- VERIFICATION ---")
        print("OK: docs/spec contains no development-stage markers")
        print("OK: permanent product contracts were preserved outside removed sections")
        print("OK: only docs/spec/*.md files were written by this script")
        return 0

    except Exception:
        print(
            "\nROLLBACK: restoring specification files changed by this script",
            file=sys.stderr,
        )
        for path, content in originals.items():
            path.write_text(content, encoding="utf-8")
        raise


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        raise SystemExit(1)
