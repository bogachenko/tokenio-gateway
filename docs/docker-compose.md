# Docker Compose local stack

This repository provides a single local Docker Compose stack for the app and
Postgres.

## Start the stack

```bash
cp .env.docker.example .env.docker
docker compose up --build -d postgres
```

## Run database migrations

Migrations are intentionally explicit. The app container does not mutate the
database on startup.

Run them after Postgres is healthy:

```bash
./scripts/docker-compose-migrate.sh
```

The script executes every `*.sql` file mounted from `./db` into the Postgres
container at `/migrations`.

## Start the app

```bash
docker compose up --build app
```

The app service depends on the Postgres healthcheck. It still expects the
database schema to have been applied explicitly before use.

## Reset local state

```bash
docker compose down -v
```
