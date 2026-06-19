#!/usr/bin/env sh
set -eu

cleanup() {
  ./scripts/integration-postgres-down.sh >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

./scripts/integration-postgres-up.sh
./scripts/docker-compose-migrate.sh
./scripts/docker-compose-smoke.sh

echo "integration lifecycle smoke completed"
