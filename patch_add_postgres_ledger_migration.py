#!/usr/bin/env python3
from __future__ import annotations

import argparse
import difflib
import subprocess
import sys
from pathlib import Path

MODULE = "module github.com/bogachenko/tokenio-gateway"

UP = Path("db/migrations/000002_ledger_and_charge_command.up.sql")
DOWN = Path("db/migrations/000002_ledger_and_charge_command.down.sql")
TEST = Path("internal/infrastructure/postgres/migrations_ledger_contract_test.go")

UP_SQL = """\
CREATE TABLE tokenio_usage_records (
    local_request_id TEXT PRIMARY KEY,

    idempotency_key TEXT,
    user_id TEXT NOT NULL REFERENCES tokenio_users(id),
    api_key_id TEXT REFERENCES tokenio_api_keys(id),

    api_family TEXT NOT NULL,
    endpoint_kind TEXT NOT NULL,
    client_model TEXT NOT NULL,
    billing_model TEXT NOT NULL,

    selected_reseller_id TEXT REFERENCES tokenio_resellers(id),
    selected_route_id TEXT REFERENCES tokenio_routes(id),

    provider_type TEXT NOT NULL,
    provider_model TEXT NOT NULL,

    provider_request_id TEXT,
    provider_response_model TEXT,

    estimated_input_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_cached_input_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_output_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_reasoning_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_image_input_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_audio_input_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_audio_output_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_file_input_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_video_input_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_image_generation_units BIGINT NOT NULL DEFAULT 0,
    estimated_client_amount_cents BIGINT NOT NULL DEFAULT 0,
    estimated_upstream_cost_cents BIGINT NOT NULL DEFAULT 0,

    input_tokens BIGINT NOT NULL DEFAULT 0,
    cached_input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    reasoning_tokens BIGINT NOT NULL DEFAULT 0,
    image_input_tokens BIGINT NOT NULL DEFAULT 0,
    audio_input_tokens BIGINT NOT NULL DEFAULT 0,
    audio_output_tokens BIGINT NOT NULL DEFAULT 0,
    file_input_tokens BIGINT NOT NULL DEFAULT 0,
    video_input_tokens BIGINT NOT NULL DEFAULT 0,
    image_generation_units BIGINT NOT NULL DEFAULT 0,

    client_amount_cents BIGINT NOT NULL DEFAULT 0,
    charged_amount_cents BIGINT NOT NULL DEFAULT 0,
    remaining_amount_cents BIGINT NOT NULL DEFAULT 0,

    actual_upstream_cost_cents BIGINT NOT NULL DEFAULT 0,

    currency TEXT NOT NULL DEFAULT 'RUB',

    usage_completeness TEXT NOT NULL DEFAULT 'missing',
    status TEXT NOT NULL,

    failure_reason TEXT,
    billing_charge_request_id TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    reserved_at TIMESTAMPTZ,
    released_at TIMESTAMPTZ,
    billable_at TIMESTAMPTZ,
    charged_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (currency = 'RUB'),

    CHECK (estimated_input_tokens >= 0),
    CHECK (estimated_cached_input_tokens >= 0),
    CHECK (estimated_output_tokens >= 0),
    CHECK (estimated_reasoning_tokens >= 0),
    CHECK (estimated_image_input_tokens >= 0),
    CHECK (estimated_audio_input_tokens >= 0),
    CHECK (estimated_audio_output_tokens >= 0),
    CHECK (estimated_file_input_tokens >= 0),
    CHECK (estimated_video_input_tokens >= 0),
    CHECK (estimated_image_generation_units >= 0),
    CHECK (estimated_client_amount_cents >= 0),
    CHECK (estimated_upstream_cost_cents >= 0),

    CHECK (input_tokens >= 0),
    CHECK (cached_input_tokens >= 0),
    CHECK (output_tokens >= 0),
    CHECK (reasoning_tokens >= 0),
    CHECK (image_input_tokens >= 0),
    CHECK (audio_input_tokens >= 0),
    CHECK (audio_output_tokens >= 0),
    CHECK (file_input_tokens >= 0),
    CHECK (video_input_tokens >= 0),
    CHECK (image_generation_units >= 0),

    CHECK (client_amount_cents >= 0),
    CHECK (charged_amount_cents >= 0),
    CHECK (remaining_amount_cents >= 0),
    CHECK (actual_upstream_cost_cents >= 0)
);

ALTER TABLE tokenio_usage_records
ADD CONSTRAINT tokenio_usage_records_status_chk
CHECK (
    status IN (
        'reserved',
        'released',
        'billable',
        'partially_charged',
        'charged',
        'failed',
        'pricing_failed'
    )
);

ALTER TABLE tokenio_usage_records
ADD CONSTRAINT tokenio_usage_records_usage_completeness_chk
CHECK (
    usage_completeness IN (
        'detailed',
        'aggregate',
        'estimated',
        'missing',
        'failed'
    )
);

CREATE UNIQUE INDEX tokenio_usage_records_idempotency_uq
    ON tokenio_usage_records (user_id, endpoint_kind, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE INDEX tokenio_usage_records_pending_idx
    ON tokenio_usage_records (user_id, status, created_at)
    WHERE status IN ('reserved', 'billable', 'partially_charged', 'pricing_failed');

CREATE INDEX tokenio_usage_records_chargeable_idx
    ON tokenio_usage_records (user_id, status, created_at)
    WHERE status IN ('billable', 'partially_charged');

CREATE INDEX tokenio_usage_records_route_idx
    ON tokenio_usage_records (selected_route_id, created_at);

CREATE INDEX tokenio_usage_records_billing_group_idx
    ON tokenio_usage_records (
        user_id,
        provider_type,
        client_model,
        currency,
        status
    );

CREATE TABLE tokenio_billing_sessions (
    user_id TEXT PRIMARY KEY REFERENCES tokenio_users(id),
    billing_subject_user_id TEXT NOT NULL,

    remote_balance_cents BIGINT NOT NULL DEFAULT 0,
    pending_amount_cents_cached BIGINT NOT NULL DEFAULT 0,

    currency TEXT NOT NULL DEFAULT 'RUB',

    fetched_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (currency = 'RUB'),
    CHECK (remote_balance_cents >= 0),
    CHECK (pending_amount_cents_cached >= 0)
);

CREATE TABLE tokenio_billing_charge_batches (
    id TEXT PRIMARY KEY,

    user_id TEXT NOT NULL REFERENCES tokenio_users(id),
    billing_subject_user_id TEXT NOT NULL,

    provider_type TEXT NOT NULL,
    client_model TEXT NOT NULL,
    billing_model TEXT NOT NULL,

    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,

    amount_cents BIGINT NOT NULL,
    currency TEXT NOT NULL DEFAULT 'RUB',

    billing_status TEXT NOT NULL,

    billing_response_balance_cents BIGINT,
    billing_error_code TEXT NOT NULL DEFAULT '',

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    charged_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (currency = 'RUB'),
    CHECK (input_tokens >= 0),
    CHECK (output_tokens >= 0),
    CHECK (amount_cents > 0),
    CHECK (
        billing_response_balance_cents IS NULL
        OR billing_response_balance_cents >= 0
    ),
    CHECK (
        (
            billing_status = 'pending'
            AND charged_at IS NULL
            AND failed_at IS NULL
            AND billing_error_code = ''
        )
        OR
        (
            billing_status = 'failed'
            AND charged_at IS NULL
            AND failed_at IS NOT NULL
            AND billing_error_code <> ''
        )
        OR
        (
            billing_status = 'succeeded'
            AND charged_at IS NOT NULL
            AND failed_at IS NULL
            AND billing_error_code = ''
        )
    )
);

ALTER TABLE tokenio_billing_charge_batches
ADD CONSTRAINT tokenio_billing_charge_batches_status_chk
CHECK (billing_status IN ('pending', 'succeeded', 'failed'));

CREATE INDEX tokenio_billing_charge_batches_user_idx
    ON tokenio_billing_charge_batches (user_id, created_at);

CREATE INDEX tokenio_billing_charge_batches_model_idx
    ON tokenio_billing_charge_batches (
        provider_type,
        client_model,
        created_at
    );

CREATE TABLE tokenio_billing_charge_allocations (
    id TEXT PRIMARY KEY,

    batch_id TEXT NOT NULL REFERENCES tokenio_billing_charge_batches(id),
    local_request_id TEXT NOT NULL
        REFERENCES tokenio_usage_records(local_request_id),
    position INTEGER NOT NULL,

    charged_amount_cents BIGINT NOT NULL,
    remaining_amount_cents BIGINT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (position >= 0),
    CHECK (charged_amount_cents > 0),
    CHECK (remaining_amount_cents >= 0)
);

CREATE UNIQUE INDEX tokenio_billing_charge_allocations_batch_usage_uq
    ON tokenio_billing_charge_allocations (batch_id, local_request_id);

CREATE UNIQUE INDEX tokenio_billing_charge_allocations_batch_position_uq
    ON tokenio_billing_charge_allocations (batch_id, position);

CREATE INDEX tokenio_billing_charge_allocations_usage_idx
    ON tokenio_billing_charge_allocations (local_request_id);

CREATE TABLE tokenio_billing_charge_expected_records (
    batch_id TEXT NOT NULL
        REFERENCES tokenio_billing_charge_batches(id),

    local_request_id TEXT NOT NULL
        REFERENCES tokenio_usage_records(local_request_id),

    position INTEGER NOT NULL,

    expected_record JSONB NOT NULL,

    created_at TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (batch_id, local_request_id),
    UNIQUE (batch_id, position),

    CHECK (position >= 0),
    CHECK (jsonb_typeof(expected_record) = 'object')
);

CREATE INDEX tokenio_billing_charge_expected_records_request_idx
    ON tokenio_billing_charge_expected_records (local_request_id);
"""

DOWN_SQL = """\
DROP TABLE tokenio_billing_charge_expected_records;
DROP TABLE tokenio_billing_charge_allocations;
DROP TABLE tokenio_billing_charge_batches;
DROP TABLE tokenio_billing_sessions;
DROP TABLE tokenio_usage_records;
"""

TEST_GO = r"""package postgres_test

import (
	"strings"
	"testing"
)

func TestLedgerMigrationContainsCanonicalTables(t *testing.T) {
	up := migrationFile(t, "000002_ledger_and_charge_command.up.sql")

	required := []string{
		"CREATE TABLE tokenio_usage_records",
		"CREATE TABLE tokenio_billing_sessions",
		"CREATE TABLE tokenio_billing_charge_batches",
		"CREATE TABLE tokenio_billing_charge_allocations",
		"CREATE TABLE tokenio_billing_charge_expected_records",
		"tokenio_usage_records_status_chk",
		"tokenio_usage_records_usage_completeness_chk",
		"tokenio_usage_records_idempotency_uq",
		"tokenio_usage_records_pending_idx",
		"tokenio_usage_records_chargeable_idx",
		"tokenio_usage_records_billing_group_idx",
		"tokenio_billing_charge_batches_status_chk",
		"tokenio_billing_charge_allocations_batch_position_uq",
		"tokenio_billing_charge_expected_records_request_idx",
		"expected_record JSONB NOT NULL",
		"CHECK (amount_cents > 0)",
		"CHECK (charged_amount_cents > 0)",
		"billing_error_code TEXT NOT NULL DEFAULT ''",
		"updated_at TIMESTAMPTZ NOT NULL DEFAULT now()",
	}
	for _, fragment := range required {
		if !strings.Contains(up, fragment) {
			t.Errorf("ledger migration missing %q", fragment)
		}
	}

	forbidden := []string{
		"billing_error_body",
		"ON DELETE CASCADE",
		"NUMERIC(12, 6)",
	}
	for _, fragment := range forbidden {
		if strings.Contains(up, fragment) {
			t.Errorf("ledger migration contains forbidden fragment %q", fragment)
		}
	}
}

func TestUsageMigrationPersistsAllUsageDimensions(t *testing.T) {
	up := migrationFile(t, "000002_ledger_and_charge_command.up.sql")

	dimensions := []string{
		"input_tokens",
		"cached_input_tokens",
		"output_tokens",
		"reasoning_tokens",
		"image_input_tokens",
		"audio_input_tokens",
		"audio_output_tokens",
		"file_input_tokens",
		"video_input_tokens",
		"image_generation_units",
	}

	for _, dimension := range dimensions {
		actual := "\n    " + dimension + " BIGINT NOT NULL DEFAULT 0"
		estimated := "\n    estimated_" + dimension + " BIGINT NOT NULL DEFAULT 0"
		if !strings.Contains(up, actual) {
			t.Errorf("usage migration missing actual dimension %q", dimension)
		}
		if !strings.Contains(up, estimated) {
			t.Errorf("usage migration missing estimated dimension %q", dimension)
		}
	}
}

func TestLedgerDownMigrationDropsTablesInDependencyOrder(t *testing.T) {
	down := migrationFile(t, "000002_ledger_and_charge_command.down.sql")
	expected := []string{
		"DROP TABLE tokenio_billing_charge_expected_records;",
		"DROP TABLE tokenio_billing_charge_allocations;",
		"DROP TABLE tokenio_billing_charge_batches;",
		"DROP TABLE tokenio_billing_sessions;",
		"DROP TABLE tokenio_usage_records;",
	}

	position := -1
	for _, statement := range expected {
		next := strings.Index(down, statement)
		if next < 0 {
			t.Fatalf("down migration missing %q", statement)
		}
		if next <= position {
			t.Fatalf("down migration dependency order is invalid at %q", statement)
		}
		position = next
	}
}
"""


def fail(message: str) -> None:
    raise RuntimeError(message)


def run(command: list[str], cwd: Path) -> None:
    print("+", " ".join(command), flush=True)
    completed = subprocess.run(command, cwd=cwd, text=True)
    if completed.returncode != 0:
        fail(f"command failed with exit code {completed.returncode}: {' '.join(command)}")


def write_exact(repo: Path, relative: Path, content: str) -> str | None:
    absolute = repo / relative
    before = absolute.read_text(encoding="utf-8") if absolute.exists() else None
    if before is not None and before != content:
        fail(f"{relative} already exists with different content")
    absolute.parent.mkdir(parents=True, exist_ok=True)
    absolute.write_text(content, encoding="utf-8")
    return before


def print_diff(relative: Path, before: str | None, after: str) -> None:
    diff = difflib.unified_diff(
        (before or "").splitlines(keepends=True),
        after.splitlines(keepends=True),
        fromfile=f"a/{relative.as_posix()}",
        tofile=f"b/{relative.as_posix()}",
    )
    sys.stdout.writelines(diff)


def verify_order(content: str, statements: tuple[str, ...], label: str) -> None:
    cursor = -1
    for statement in statements:
        next_cursor = content.find(statement)
        if next_cursor < 0:
            fail(f"{label} missing: {statement}")
        if next_cursor <= cursor:
            fail(f"{label} order invalid at: {statement}")
        cursor = next_cursor


def main() -> int:
    parser = argparse.ArgumentParser(
        description=(
            "Add canonical PostgreSQL migration for usage ledger, billing sessions, "
            "and durable billing charge commands."
        )
    )
    parser.add_argument(
        "repo",
        nargs="?",
        default=".",
        help="path to tokenio-gateway repository (default: current directory)",
    )
    parser.add_argument(
        "--skip-full-suite",
        action="store_true",
        help="run PostgreSQL infrastructure package tests only",
    )
    args = parser.parse_args()

    repo = Path(args.repo).resolve()
    go_mod = repo / "go.mod"
    if not go_mod.is_file() or MODULE not in go_mod.read_text(encoding="utf-8"):
        fail(f"{repo} is not the expected tokenio-gateway repository")

    required_registry = repo / "db/migrations/000001_registry.up.sql"
    if not required_registry.is_file():
        fail("required predecessor migration is missing: db/migrations/000001_registry.up.sql")

    migrations = repo / "db/migrations"
    conflicting = []
    if migrations.exists():
        for path in migrations.glob("000002_*"):
            if path.name not in {UP.name, DOWN.name}:
                conflicting.append(path.relative_to(repo).as_posix())
    if conflicting:
        fail("migration number 000002 is already used: " + ", ".join(conflicting))

    targets = {
        UP: UP_SQL,
        DOWN: DOWN_SQL,
        TEST: TEST_GO,
    }
    originals: dict[Path, str | None] = {}

    try:
        for relative, content in targets.items():
            originals[relative] = write_exact(repo, relative, content)

        run(["gofmt", "-w", TEST.as_posix()], repo)

        print("\n--- PATCH DIFF ---")
        for relative in targets:
            after = (repo / relative).read_text(encoding="utf-8")
            print_diff(relative, originals[relative], after)

        up = (repo / UP).read_text(encoding="utf-8")
        down = (repo / DOWN).read_text(encoding="utf-8")

        required_up = (
            "CREATE TABLE tokenio_usage_records",
            "CREATE TABLE tokenio_billing_sessions",
            "CREATE TABLE tokenio_billing_charge_batches",
            "CREATE TABLE tokenio_billing_charge_allocations",
            "CREATE TABLE tokenio_billing_charge_expected_records",
            "estimated_cached_input_tokens BIGINT NOT NULL DEFAULT 0",
            "estimated_image_generation_units BIGINT NOT NULL DEFAULT 0",
            "billing_response_balance_cents BIGINT",
            "billing_error_code TEXT NOT NULL DEFAULT ''",
            "CHECK (amount_cents > 0)",
            "CHECK (charged_amount_cents > 0)",
            "expected_record JSONB NOT NULL",
            "UNIQUE (batch_id, position)",
        )
        for fragment in required_up:
            if fragment not in up:
                fail(f"up migration missing required fragment: {fragment}")

        forbidden_up = (
            "billing_error_body",
            "ON DELETE CASCADE",
            "NUMERIC(12, 6)",
        )
        for fragment in forbidden_up:
            if fragment in up:
                fail(f"up migration contains forbidden fragment: {fragment}")

        verify_order(
            up,
            (
                "CREATE TABLE tokenio_usage_records",
                "CREATE TABLE tokenio_billing_sessions",
                "CREATE TABLE tokenio_billing_charge_batches",
                "CREATE TABLE tokenio_billing_charge_allocations",
                "CREATE TABLE tokenio_billing_charge_expected_records",
            ),
            "up migration",
        )
        verify_order(
            down,
            (
                "DROP TABLE tokenio_billing_charge_expected_records;",
                "DROP TABLE tokenio_billing_charge_allocations;",
                "DROP TABLE tokenio_billing_charge_batches;",
                "DROP TABLE tokenio_billing_sessions;",
                "DROP TABLE tokenio_usage_records;",
            ),
            "down migration",
        )

        run(["go", "test", "./internal/infrastructure/postgres"], repo)
        if not args.skip_full_suite:
            run(["go", "test", "./..."], repo)
        run(
            [
                "git",
                "diff",
                "--check",
                "--",
                "db/migrations",
                "internal/infrastructure/postgres",
            ],
            repo,
        )

        print("\n--- VERIFICATION ---")
        run(
            [
                "grep",
                "-nE",
                (
                    "CREATE TABLE tokenio_(usage_records|billing_sessions|"
                    "billing_charge_batches|billing_charge_allocations|"
                    "billing_charge_expected_records)"
                ),
                UP.as_posix(),
            ],
            repo,
        )
        print("\nOK: canonical ledger and charge-command migration applied")
        return 0

    except Exception:
        print("\nROLLBACK: restoring files changed by this script", file=sys.stderr)
        for relative, before in originals.items():
            absolute = repo / relative
            if before is None:
                if absolute.exists():
                    absolute.unlink()
            else:
                absolute.parent.mkdir(parents=True, exist_ok=True)
                absolute.write_text(before, encoding="utf-8")
        raise


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        raise SystemExit(1)
