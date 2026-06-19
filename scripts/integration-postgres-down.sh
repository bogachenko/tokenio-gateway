#!/usr/bin/env sh
set -eu

docker compose down -v --remove-orphans
echo "integration Postgres state removed"
