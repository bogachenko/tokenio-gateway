# Integration tests

Integration tests live under this directory and are built only with:

```bash
go test -tags=integration ./integration/...
```

Rules:

- do not call production services;
- use local Docker Compose dependencies or in-process fakes;
- keep fake services deterministic;
- every added scenario must document the exact fake dependency it uses.
## PostgreSQL dependency

Start local Postgres with the checked-in Docker Compose stack:

```bash
./scripts/integration-postgres-up.sh
```

Run the Postgres integration test with the printed DSN:

```bash
TOKENIO_INTEGRATION_DATABASE_DSN='postgres://tokenio:tokenio_dev_password@127.0.0.1:5432/tokenio?sslmode=disable' go test -tags=integration ./integration/...
```

Stop and remove local state:

```bash
./scripts/integration-postgres-down.sh
```

