#!/usr/bin/env sh
set -eu

if [ -f .env.docker ]; then
  # shellcheck disable=SC1091
  . ./.env.docker
elif [ -f .env.docker.example ]; then
  # shellcheck disable=SC1091
  . ./.env.docker.example
fi

HTTP_PORT="${TOKENIO_HTTP_PORT:-8080}"
READY_URL="http://127.0.0.1:${HTTP_PORT}/readyz"

docker compose up --build -d postgres
./scripts/docker-compose-migrate.sh
docker compose up --build -d app

attempt=1
while [ "$attempt" -le 30 ]; do
  if command -v curl >/dev/null 2>&1; then
    if curl -fsS "$READY_URL" >/dev/null; then
      echo "app is ready: $READY_URL"
      break
    fi
  elif command -v wget >/dev/null 2>&1; then
    if wget -q -O /dev/null "$READY_URL"; then
      echo "app is ready: $READY_URL"
      exit 0
    fi
  else
    echo "curl or wget is required for readiness check" >&2
    exit 1
  fi

  attempt=$((attempt + 1))
  sleep 1
done

if [ "$attempt" -gt 30 ]; then
  echo "app did not become ready at $READY_URL" >&2
  docker compose logs app >&2
  exit 1
fi

MODELS_URL="http://127.0.0.1:${HTTP_PORT}/v1/models"
if command -v curl >/dev/null 2>&1; then
  models_status="$(curl -sS -o /dev/null -w "%{http_code}" "$MODELS_URL")"
elif command -v wget >/dev/null 2>&1; then
  if wget -q -O /dev/null "$MODELS_URL"; then
    models_status=200
  else
    models_status=$?
  fi
else
  echo "curl or wget is required for route smoke check" >&2
  exit 1
fi

if [ "$models_status" != "401" ]; then
  echo "unexpected /v1/models status: $models_status, want 401 without Authorization" >&2
  docker compose logs app >&2
  exit 1
fi

echo "public route smoke passed: /v1/models requires Authorization"
