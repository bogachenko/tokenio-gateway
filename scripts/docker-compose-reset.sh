#!/usr/bin/env sh
set -eu

docker compose down -v --remove-orphans
echo "local Docker Compose state removed"
