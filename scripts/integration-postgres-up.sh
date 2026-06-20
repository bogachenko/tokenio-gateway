#!/usr/bin/env sh
set -eu

DB_NAME="${TOKENIO_POSTGRES_DB:-tokenio}"
DB_USER="${TOKENIO_POSTGRES_USER:-tokenio}"

docker compose up --build -d postgres

attempt=0
while :; do
  attempt=$((attempt + 1))

  if docker compose exec -T postgres pg_isready -U "$DB_USER" -d "$DB_NAME" >/dev/null 2>&1; then
    break
  fi

  if [ "$attempt" -ge 60 ]; then
    echo "Postgres did not become ready after $attempt attempts" >&2
    docker compose logs postgres >&2 || true
    exit 1
  fi

  sleep 1
done

echo "Postgres is ready for integration tests."
echo "Use:"
echo "TOKENIO_INTEGRATION_DATABASE_DSN='postgres://tokenio:tokenio_dev_password@127.0.0.1:${TOKENIO_POSTGRES_PORT:-5432}/tokenio?sslmode=disable' go test -tags=integration ./integration/..."
