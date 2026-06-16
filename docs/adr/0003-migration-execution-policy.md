# 0003. Explicit migration execution

## Status

Accepted

## Context

Postgres is the source of truth for persisted Tokenio state. SQL migrations can
contain schema changes whose execution has operational consequences.

Automatically applying migrations from the gateway serving process makes
startup behavior deployment-dependent and can allow multiple replicas to race
for schema ownership. Merely documenting that destructive migrations should not
run silently is not an executable process boundary.

## Decision

Migration application and gateway startup are separate process responsibilities.

```text
cmd/migrate:
  connects to PostgreSQL;
  applies embedded canonical SQL migrations;
  verifies migration checksums and final schema version;
  exits non-zero on any failure.

cmd/gateway:
  connects to PostgreSQL;
  does not apply schema migrations;
  validates that the required schema version and checksums are compatible;
  exits non-zero when the schema is missing, behind, ahead incompatibly or
  otherwise invalid.
```

Deployments must run `cmd/migrate` explicitly before starting the new gateway
revision.

The first version uses the embedded canonical migration set. A runtime
`TOKENIO_MIGRATIONS_DIR` override is not part of the production contract unless
a future ADR defines its trust, checksum and packaging semantics.

Migration code may use locking to prevent concurrent migration commands, but
the serving gateway process never becomes a migration owner.

## Consequences

- Deployment order is explicit and repeatable.
- Gateway replicas do not mutate schema during normal startup.
- Missing or incompatible schema causes fail-fast startup.
- Migration application and schema validation require separate integration
  tests.
- Existing runtime auto-apply behavior, if present, must be removed in the
  storage/migrations roadmap stage rather than preserved as a fallback.

## Rejected alternatives

### Gateway always applies migrations on startup

Rejected because serving lifecycle and schema ownership become coupled.

### Optional silent auto-migrate mode

Rejected because it creates divergent production paths and weakens acceptance
evidence.

### Filesystem migration directory override in production

Rejected for the first version because the canonical embedded migration set is
the deterministic source of truth.
