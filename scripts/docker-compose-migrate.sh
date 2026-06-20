#!/usr/bin/env sh
set -eu

COMPOSE_PROJECT_DIR="${COMPOSE_PROJECT_DIR:-$(pwd)}"
MIGRATIONS_DIR="${TOKENIO_MIGRATIONS_DIR:-/migrations}"
DB_NAME="${TOKENIO_POSTGRES_DB:-tokenio}"
DB_USER="${TOKENIO_POSTGRES_USER:-tokenio}"

docker compose exec -T postgres sh -eu -c '
  migrations_dir="$1"
  db_name="$2"
  db_user="$3"

  found=0

  for file in "$migrations_dir"/*.up.sql; do
    [ -e "$file" ] || continue
    found=1
    echo "applying $(basename "$file")"
    psql -v ON_ERROR_STOP=1 -U "$db_user" -d "$db_name" -f "$file"
  done

  if [ "$found" -eq 0 ]; then
    for file in "$migrations_dir"/*.sql; do
      [ -e "$file" ] || continue
      case "$(basename "$file")" in
        *.down.sql) continue ;;
      esac
      found=1
      echo "applying $(basename "$file")"
      psql -v ON_ERROR_STOP=1 -U "$db_user" -d "$db_name" -f "$file"
    done
  fi

  if [ "$found" -eq 0 ]; then
    echo "no forward SQL migrations found in $migrations_dir" >&2
    exit 1
  fi
' sh "$MIGRATIONS_DIR" "$DB_NAME" "$DB_USER"
