#!/usr/bin/env python3
from __future__ import annotations

import argparse
import difflib
import subprocess
import sys
from pathlib import Path

MODULE = "module github.com/bogachenko/tokenio-gateway"

UP = Path("db/migrations/000001_registry.up.sql")
DOWN = Path("db/migrations/000001_registry.down.sql")
TEST = Path("internal/infrastructure/postgres/migrations_registry_contract_test.go")

UP_SQL = """\
CREATE TABLE tokenio_users (
    id TEXT PRIMARY KEY,
    external_billing_user_id TEXT NOT NULL,
    email TEXT,
    name TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX tokenio_users_external_billing_user_id_uq
    ON tokenio_users (external_billing_user_id);

CREATE INDEX tokenio_users_enabled_idx
    ON tokenio_users (enabled);

CREATE TABLE tokenio_api_keys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES tokenio_users(id),
    name TEXT NOT NULL,
    key_hash TEXT NOT NULL,
    key_prefix TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX tokenio_api_keys_key_hash_uq
    ON tokenio_api_keys (key_hash);

CREATE INDEX tokenio_api_keys_user_id_idx
    ON tokenio_api_keys (user_id);

CREATE INDEX tokenio_api_keys_enabled_idx
    ON tokenio_api_keys (enabled);

CREATE INDEX tokenio_api_keys_key_prefix_idx
    ON tokenio_api_keys (key_prefix);

CREATE TABLE tokenio_resellers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    provider_type TEXT NOT NULL,
    base_url TEXT NOT NULL,
    api_key_env TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,

    balance_cents BIGINT NOT NULL DEFAULT 0,
    reserved_cents BIGINT NOT NULL DEFAULT 0,
    minimum_balance_cents BIGINT NOT NULL DEFAULT 0,

    last_balance_alert_at TIMESTAMPTZ,
    last_healthcheck_at TIMESTAMPTZ,
    last_healthcheck_status TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at TIMESTAMPTZ,

    CHECK (reserved_cents >= 0),
    CHECK (minimum_balance_cents >= 0)
);

ALTER TABLE tokenio_resellers
ADD CONSTRAINT tokenio_resellers_provider_type_chk
CHECK (
    provider_type IN (
        'openai',
        'openrouter',
        'together',
        'groq',
        'ollama',
        'lmstudio',
        'vllm',
        'gemini',
        'anthropic',
        'hydra'
    )
);

CREATE INDEX tokenio_resellers_provider_type_idx
    ON tokenio_resellers (provider_type);

CREATE INDEX tokenio_resellers_enabled_idx
    ON tokenio_resellers (enabled);

CREATE TABLE tokenio_routes (
    id TEXT PRIMARY KEY,
    reseller_id TEXT NOT NULL REFERENCES tokenio_resellers(id),

    provider_type TEXT NOT NULL,
    api_family TEXT NOT NULL,
    endpoint_kind TEXT NOT NULL,

    client_model TEXT NOT NULL,
    provider_model TEXT NOT NULL,
    model_rewrite_policy TEXT NOT NULL DEFAULT 'none',

    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    priority INTEGER NOT NULL DEFAULT 100,

    requests_per_minute INTEGER NOT NULL DEFAULT 0,
    tokens_per_minute INTEGER NOT NULL DEFAULT 0,
    concurrent_requests INTEGER NOT NULL DEFAULT 0,

    default_max_output_tokens BIGINT NOT NULL DEFAULT 0,

    capabilities JSONB NOT NULL DEFAULT '{}'::jsonb,

    cooldown_until TIMESTAMPTZ,
    cooldown_reason TEXT,

    last_error_code TEXT,
    last_error_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at TIMESTAMPTZ,

    CHECK (priority >= 0),
    CHECK (requests_per_minute >= 0),
    CHECK (tokens_per_minute >= 0),
    CHECK (concurrent_requests >= 0),
    CHECK (default_max_output_tokens >= 0)
);

ALTER TABLE tokenio_routes
ADD CONSTRAINT tokenio_routes_api_family_chk
CHECK (
    api_family IN (
        'openai_compatible',
        'gemini_native',
        'anthropic_native',
        'ollama_native'
    )
);

ALTER TABLE tokenio_routes
ADD CONSTRAINT tokenio_routes_endpoint_kind_chk
CHECK (
    endpoint_kind IN (
        'chat',
        'embeddings',
        'images_generation'
    )
);

ALTER TABLE tokenio_routes
ADD CONSTRAINT tokenio_routes_model_rewrite_policy_chk
CHECK (
    model_rewrite_policy IN (
        'none',
        'provider_model'
    )
);

CREATE UNIQUE INDEX tokenio_routes_unique_provider_model_route_uq
    ON tokenio_routes (
        reseller_id,
        api_family,
        endpoint_kind,
        client_model,
        provider_model
    );

CREATE INDEX tokenio_routes_lookup_idx
    ON tokenio_routes (
        api_family,
        endpoint_kind,
        client_model,
        enabled
    );

CREATE INDEX tokenio_routes_cooldown_idx
    ON tokenio_routes (cooldown_until);

CREATE TABLE tokenio_route_prices (
    route_id TEXT PRIMARY KEY REFERENCES tokenio_routes(id),

    currency TEXT NOT NULL DEFAULT 'RUB',

    input_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,
    cached_input_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,
    output_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,
    reasoning_output_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,

    image_input_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,
    audio_input_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,
    audio_output_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,
    file_input_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,
    video_input_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,

    image_generation_price_per_unit_cents BIGINT NOT NULL DEFAULT 0,
    image_generation_unit_kind TEXT NOT NULL DEFAULT 'none',

    markup_coefficient DOUBLE PRECISION NOT NULL DEFAULT 1.0,

    enabled BOOLEAN NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (currency = 'RUB'),
    CHECK (input_price_per_1m_tokens_cents >= 0),
    CHECK (cached_input_price_per_1m_tokens_cents >= 0),
    CHECK (output_price_per_1m_tokens_cents >= 0),
    CHECK (reasoning_output_price_per_1m_tokens_cents >= 0),
    CHECK (image_input_price_per_1m_tokens_cents >= 0),
    CHECK (audio_input_price_per_1m_tokens_cents >= 0),
    CHECK (audio_output_price_per_1m_tokens_cents >= 0),
    CHECK (file_input_price_per_1m_tokens_cents >= 0),
    CHECK (video_input_price_per_1m_tokens_cents >= 0),
    CHECK (image_generation_price_per_unit_cents >= 0),
    CHECK (image_generation_unit_kind IN ('none', 'generated_image')),
    CHECK (
        markup_coefficient > 0
        AND markup_coefficient < 'Infinity'::double precision
    )
);
"""

DOWN_SQL = """\
DROP TABLE tokenio_route_prices;
DROP TABLE tokenio_routes;
DROP TABLE tokenio_resellers;
DROP TABLE tokenio_api_keys;
DROP TABLE tokenio_users;
"""

TEST_GO = r"""package postgres_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func migrationFile(t *testing.T, name string) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}

	path := filepath.Join(
		filepath.Dir(currentFile),
		"..",
		"..",
		"..",
		"db",
		"migrations",
		name,
	)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration %s: %v", name, err)
	}
	return string(content)
}

func TestRegistryMigrationContainsCanonicalTablesAndConstraints(t *testing.T) {
	up := migrationFile(t, "000001_registry.up.sql")

	required := []string{
		"CREATE TABLE tokenio_users",
		"CREATE TABLE tokenio_api_keys",
		"CREATE TABLE tokenio_resellers",
		"CREATE TABLE tokenio_routes",
		"CREATE TABLE tokenio_route_prices",
		"tokenio_users_external_billing_user_id_uq",
		"tokenio_api_keys_key_hash_uq",
		"tokenio_resellers_provider_type_chk",
		"tokenio_routes_api_family_chk",
		"tokenio_routes_endpoint_kind_chk",
		"tokenio_routes_model_rewrite_policy_chk",
		"tokenio_routes_unique_provider_model_route_uq",
		"tokenio_routes_lookup_idx",
		"markup_coefficient DOUBLE PRECISION",
		"markup_coefficient < 'Infinity'::double precision",
	}
	for _, fragment := range required {
		if !strings.Contains(up, fragment) {
			t.Errorf("up migration missing %q", fragment)
		}
	}

	forbidden := []string{
		"raw_api_key",
		"billing_jwt",
		"billing_service_token",
		"admin_token",
		"NUMERIC(12, 6)",
		"ON DELETE CASCADE",
	}
	for _, fragment := range forbidden {
		if strings.Contains(up, fragment) {
			t.Errorf("up migration contains forbidden fragment %q", fragment)
		}
	}
}

func TestRegistryDownMigrationDropsTablesInDependencyOrder(t *testing.T) {
	down := migrationFile(t, "000001_registry.down.sql")
	expected := []string{
		"DROP TABLE tokenio_route_prices;",
		"DROP TABLE tokenio_routes;",
		"DROP TABLE tokenio_resellers;",
		"DROP TABLE tokenio_api_keys;",
		"DROP TABLE tokenio_users;",
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


def write_exact(repo: Path, relative: Path, content: str) -> tuple[str | None, str]:
    absolute = repo / relative
    before = absolute.read_text(encoding="utf-8") if absolute.exists() else None
    if before is not None and before != content:
        fail(f"{relative} already exists with different content")
    absolute.parent.mkdir(parents=True, exist_ok=True)
    absolute.write_text(content, encoding="utf-8")
    return before, content


def print_diff(relative: Path, before: str | None, after: str) -> None:
    diff = difflib.unified_diff(
        (before or "").splitlines(keepends=True),
        after.splitlines(keepends=True),
        fromfile=f"a/{relative.as_posix()}",
        tofile=f"b/{relative.as_posix()}",
    )
    sys.stdout.writelines(diff)


def main() -> int:
    parser = argparse.ArgumentParser(
        description=(
            "Add the canonical PostgreSQL registry migration for users, API keys, "
            "resellers, routes, and route prices."
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
        help="skip go test ./...; structural verification still runs",
    )
    args = parser.parse_args()

    repo = Path(args.repo).resolve()
    go_mod = repo / "go.mod"
    if not go_mod.is_file() or MODULE not in go_mod.read_text(encoding="utf-8"):
        fail(f"{repo} is not the expected tokenio-gateway repository")

    migrations = repo / "db/migrations"
    conflicting = []
    if migrations.exists():
        for path in migrations.glob("000001_*"):
            if path.name not in {UP.name, DOWN.name}:
                conflicting.append(path.relative_to(repo).as_posix())
    if conflicting:
        fail("migration number 000001 is already used: " + ", ".join(conflicting))

    originals: dict[Path, str | None] = {}
    targets = {
        UP: UP_SQL,
        DOWN: DOWN_SQL,
        TEST: TEST_GO,
    }

    try:
        for relative, content in targets.items():
            before, _ = write_exact(repo, relative, content)
            originals[relative] = before

        run(["gofmt", "-w", TEST.as_posix()], repo)

        print("\n--- PATCH DIFF ---")
        for relative in targets:
            after = (repo / relative).read_text(encoding="utf-8")
            print_diff(relative, originals[relative], after)

        up = (repo / UP).read_text(encoding="utf-8")
        down = (repo / DOWN).read_text(encoding="utf-8")

        required_up = (
            "CREATE TABLE tokenio_users",
            "CREATE TABLE tokenio_api_keys",
            "CREATE TABLE tokenio_resellers",
            "CREATE TABLE tokenio_routes",
            "CREATE TABLE tokenio_route_prices",
            "markup_coefficient DOUBLE PRECISION",
            "tokenio_routes_lookup_idx",
        )
        for fragment in required_up:
            if fragment not in up:
                fail(f"up migration missing required fragment: {fragment}")

        forbidden_up = (
            "raw_api_key",
            "billing_jwt",
            "billing_service_token",
            "admin_token",
            "NUMERIC(12, 6)",
            "ON DELETE CASCADE",
        )
        for fragment in forbidden_up:
            if fragment in up:
                fail(f"up migration contains forbidden fragment: {fragment}")

        expected_down = (
            "DROP TABLE tokenio_route_prices;",
            "DROP TABLE tokenio_routes;",
            "DROP TABLE tokenio_resellers;",
            "DROP TABLE tokenio_api_keys;",
            "DROP TABLE tokenio_users;",
        )
        cursor = -1
        for statement in expected_down:
            next_cursor = down.find(statement)
            if next_cursor <= cursor:
                fail(f"down migration order invalid at: {statement}")
            cursor = next_cursor

        run(["go", "test", "./internal/infrastructure/postgres"], repo)
        if not args.skip_tests:
            run(["go", "test", "./..."], repo)
        run(["git", "diff", "--check"], repo)

        print("\n--- VERIFICATION ---")
        run(
            [
                "grep",
                "-nE",
                (
                    "CREATE TABLE tokenio_(users|api_keys|resellers|routes|route_prices)"
                    "|markup_coefficient DOUBLE PRECISION"
                ),
                UP.as_posix(),
            ],
            repo,
        )
        print("\nOK: canonical registry migration applied")
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
