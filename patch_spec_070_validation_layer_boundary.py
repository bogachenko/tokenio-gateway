#!/usr/bin/env python3
from __future__ import annotations

import argparse
import difflib
import subprocess
import sys
from pathlib import Path

MODULE = "module github.com/bogachenko/tokenio-gateway"
SPEC = Path("docs/spec/070-database-schema.ru.md")

OLD = "После decode record проходит canonical `ledger.ValidateRecord`."

NEW = """После strict decode infrastructure adapter возвращает exact `domain.UsageRecord` через canonical port.

Infrastructure adapter валидирует только persistence representation и structural storage contract:

```text
required JSON keys присутствуют
unknown JSON keys отсутствуют
JSON types корректны
timestamps имеют RFC3339Nano UTC representation
enum/scalar values могут быть отображены в canonical domain types
числовые значения помещаются в canonical Go types
order/cardinality persisted command не нарушены
```

Infrastructure adapter:

```text
не импортирует internal/application/**
не вызывает ledger.ValidateRecord
не копирует application validator в infrastructure
не принимает business/state-transition decisions
```

Canonical business validation выполняется application layer после получения entity через port и до use-case side effects. Для billing charge snapshot application использует `billing.ValidateChargeSnapshot`, который включает canonical validation batch, allocations и expected usage records. Для остальных port reads соответствующий application use case применяет принадлежащий ему canonical validator.

Если strict decoder не может построить exact canonical entity или обнаруживает structural storage corruption, adapter возвращает normalized store contract error без raw SQL/driver/JSON details. Application layer преобразует такой результат в свой safe contract error.

Layering source of truth:

```text
infrastructure -> domain + ports
application -> domain + ports
infrastructure -/-> application
```
"""


def fail(message: str) -> None:
    raise RuntimeError(message)


def run(command: list[str], cwd: Path) -> None:
    print("+", " ".join(command), flush=True)
    completed = subprocess.run(command, cwd=cwd, text=True)
    if completed.returncode != 0:
        fail(f"command failed with exit code {completed.returncode}: {' '.join(command)}")


def print_diff(path: Path, before: str, after: str) -> None:
    if before == after:
        return
    sys.stdout.writelines(
        difflib.unified_diff(
            before.splitlines(keepends=True),
            after.splitlines(keepends=True),
            fromfile=f"a/{path.as_posix()}",
            tofile=f"b/{path.as_posix()}",
        )
    )


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Align spec 070 validation ownership with the accepted Go layering ADR."
    )
    parser.add_argument("repo", nargs="?", default=".")
    parser.add_argument("--skip-tests", action="store_true")
    args = parser.parse_args()

    repo = Path(args.repo).resolve()
    go_mod = repo / "go.mod"
    if not go_mod.is_file() or MODULE not in go_mod.read_text(encoding="utf-8"):
        fail(f"{repo} is not the expected tokenio-gateway repository")

    path = repo / SPEC
    if not path.is_file():
        fail(f"required file not found: {SPEC}")

    before = path.read_text(encoding="utf-8")
    after = before

    marker = "Infrastructure adapter валидирует только persistence representation"
    if marker not in after:
        count = after.count(OLD)
        if count != 1:
            fail(f"expected exactly one obsolete validation sentence, found {count}")
        after = after.replace(OLD, NEW, 1)
    elif OLD in after:
        fail("spec contains both obsolete and corrected validation contracts")

    try:
        path.write_text(after, encoding="utf-8")

        print("\n--- PATCH DIFF ---")
        print_diff(SPEC, before, after)

        if not args.skip_tests:
            run(["go", "test", "./..."], repo)
            run(["go", "vet", "./..."], repo)

        run(["git", "diff", "--check"], repo)

        print("\n--- VERIFICATION ---")
        run(["grep", "-n", marker, SPEC.as_posix()], repo)
        run(["grep", "-n", "infrastructure -/-> application", SPEC.as_posix()], repo)

        if OLD in path.read_text(encoding="utf-8"):
            fail("obsolete ledger.ValidateRecord ownership sentence is still present")

        print("\nOK: persistence validation ownership aligned with layering ADR")
        return 0
    except Exception:
        print("\nROLLBACK: restoring changed specification", file=sys.stderr)
        path.write_text(before, encoding="utf-8")
        raise


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        raise SystemExit(1)
