#!/usr/bin/env sh
set -eu

docker compose up --build -d postgres

echo "Postgres is ready for integration tests."
echo "Use:"
echo "TOKENIO_INTEGRATION_DATABASE_DSN='postgres://tokenio:tokenio_dev_password@127.0.0.1:${TOKENIO_POSTGRES_PORT:-5432}/tokenio?sslmode=disable' go test -tags=integration ./integration/..."
