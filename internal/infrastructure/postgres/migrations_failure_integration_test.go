package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type isolatedMigrationDatabase struct {
	dsn        string
	schemaName string
	database   *DB
}

func newIsolatedMigrationDatabase(
	t *testing.T,
	prefix string,
) isolatedMigrationDatabase {
	t.Helper()

	dsn := os.Getenv("TOKENIO_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TOKENIO_TEST_DATABASE_DSN is not set")
	}

	ctx := t.Context()
	control, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open control database: %v", err)
	}
	t.Cleanup(control.Close)

	schemaName := fmt.Sprintf(
		"tokenio_%s_%d",
		prefix,
		time.Now().UTC().UnixNano(),
	)
	schemaIdentifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err := control.Exec(
		ctx,
		"CREATE SCHEMA "+schemaIdentifier,
	); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cancel()
		if _, err := control.Exec(
			cleanupCtx,
			"DROP SCHEMA "+schemaIdentifier+" CASCADE",
		); err != nil {
			t.Errorf("drop isolated schema: %v", err)
		}
	})

	database := openMigrationDatabaseInSchema(
		t,
		dsn,
		schemaName,
	)
	return isolatedMigrationDatabase{
		dsn:        dsn,
		schemaName: schemaName,
		database:   database,
	}
}

func openMigrationDatabaseInSchema(
	t *testing.T,
	dsn string,
	schemaName string,
) *DB {
	t.Helper()

	config, err := poolConfig(dsn)
	if err != nil {
		t.Fatalf("poolConfig: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName

	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatalf("open isolated database pool: %v", err)
	}
	database := &DB{pool: pool}
	t.Cleanup(database.Close)

	if err := database.Ping(t.Context()); err != nil {
		t.Fatalf("ping isolated database: %v", err)
	}
	return database
}

func TestValidateSchemaRejectsEmptySchemaForGatewayStartup(
	t *testing.T,
) {
	isolated := newIsolatedMigrationDatabase(
		t,
		"gateway_missing_schema",
	)

	if err := isolated.database.ValidateSchema(t.Context()); err == nil {
		t.Fatal("empty schema must fail gateway validation")
	}
}

func TestApplyMigrationsUpgradesPreviousSchemaVersion(
	t *testing.T,
) {
	isolated := newIsolatedMigrationDatabase(
		t,
		"migration_upgrade",
	)
	items, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(items) < 2 {
		t.Fatalf("migration count=%d, want at least 2", len(items))
	}

	conn, err := isolated.database.pool.Acquire(t.Context())
	if err != nil {
		t.Fatalf("acquire migration connection: %v", err)
	}
	if _, err := conn.Exec(
		t.Context(),
		createMigrationTableSQL,
	); err != nil {
		conn.Release()
		t.Fatalf("create migration table: %v", err)
	}
	for _, item := range items[:len(items)-1] {
		if err := applyMigration(t.Context(), conn, item); err != nil {
			conn.Release()
			t.Fatalf(
				"apply previous migration %d: %v",
				item.Version,
				err,
			)
		}
	}
	conn.Release()

	if err := isolated.database.ValidateSchema(t.Context()); !errors.Is(
		err,
		ports.ErrStoreContractViolation,
	) {
		t.Fatalf("behind schema validation err=%v", err)
	}
	if err := isolated.database.ApplyMigrations(t.Context()); err != nil {
		t.Fatalf("upgrade previous schema: %v", err)
	}
	if err := isolated.database.ValidateSchema(t.Context()); err != nil {
		t.Fatalf("validate upgraded schema: %v", err)
	}
}

func TestMigrationChecksumMismatchIsRejected(
	t *testing.T,
) {
	isolated := newIsolatedMigrationDatabase(
		t,
		"migration_checksum",
	)
	if err := isolated.database.ApplyMigrations(t.Context()); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	if _, err := isolated.database.Exec(
		t.Context(),
		`
UPDATE tokenio_schema_migrations
SET checksum = repeat('0', 64)
WHERE version = 1
`,
	); err != nil {
		t.Fatalf("tamper migration checksum: %v", err)
	}

	if err := isolated.database.ValidateSchema(t.Context()); !errors.Is(
		err,
		ports.ErrStoreContractViolation,
	) {
		t.Fatalf("ValidateSchema checksum err=%v", err)
	}
	if err := isolated.database.ApplyMigrations(t.Context()); !errors.Is(
		err,
		ports.ErrStoreContractViolation,
	) {
		t.Fatalf("ApplyMigrations checksum err=%v", err)
	}
}

func TestSchemaAheadOfBinaryIsRejected(
	t *testing.T,
) {
	isolated := newIsolatedMigrationDatabase(
		t,
		"migration_ahead",
	)
	if err := isolated.database.ApplyMigrations(t.Context()); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	if _, err := isolated.database.Exec(
		t.Context(),
		`
INSERT INTO tokenio_schema_migrations (
    version,
    name,
    checksum
)
VALUES (999999, 'future_schema', repeat('f', 64))
`,
	); err != nil {
		t.Fatalf("insert future migration: %v", err)
	}

	if err := isolated.database.ValidateSchema(t.Context()); !errors.Is(
		err,
		ports.ErrStoreContractViolation,
	) {
		t.Fatalf("ahead schema validation err=%v", err)
	}
}

func TestFailedMigrationRollsBackDDLAndVersionRecord(
	t *testing.T,
) {
	isolated := newIsolatedMigrationDatabase(
		t,
		"migration_rollback",
	)
	if err := isolated.database.ApplyMigrations(t.Context()); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	conn, err := isolated.database.pool.Acquire(t.Context())
	if err != nil {
		t.Fatalf("acquire migration connection: %v", err)
	}
	defer conn.Release()

	failing := migration{
		Version:  999998,
		Name:     "rollback_probe",
		Filename: "999998_rollback_probe.up.sql",
		Checksum: "test-checksum",
		SQL: `
CREATE TABLE migration_rollback_probe (
    id BIGINT PRIMARY KEY
);
SELECT * FROM migration_relation_that_must_not_exist;
`,
	}
	if err := applyMigration(t.Context(), conn, failing); err == nil {
		t.Fatal("expected failing migration")
	}

	var relationName *string
	if err := isolated.database.QueryRow(
		t.Context(),
		"SELECT to_regclass('migration_rollback_probe')::text",
	).Scan(&relationName); err != nil {
		t.Fatalf("query rollback probe relation: %v", err)
	}
	if relationName != nil {
		t.Fatalf("failed migration left relation %q", *relationName)
	}

	var appliedCount int
	if err := isolated.database.QueryRow(
		t.Context(),
		`
SELECT COUNT(*)
FROM tokenio_schema_migrations
WHERE version = $1
`,
		failing.Version,
	).Scan(&appliedCount); err != nil {
		t.Fatalf("query failed migration record: %v", err)
	}
	if appliedCount != 0 {
		t.Fatalf(
			"failed migration record count=%d want=0",
			appliedCount,
		)
	}
}

func TestConcurrentMigrationCommandsSerializeAndConverge(
	t *testing.T,
) {
	isolated := newIsolatedMigrationDatabase(
		t,
		"migration_concurrent",
	)
	second := openMigrationDatabaseInSchema(
		t,
		isolated.dsn,
		isolated.schemaName,
	)

	databases := []*DB{isolated.database, second}
	start := make(chan struct{})
	errorsByRunner := make([]error, len(databases))
	var wait sync.WaitGroup
	wait.Add(len(databases))

	for index, database := range databases {
		index := index
		database := database
		go func() {
			defer wait.Done()
			<-start
			errorsByRunner[index] = database.ApplyMigrations(
				context.Background(),
			)
		}()
	}
	close(start)
	wait.Wait()

	for index, err := range errorsByRunner {
		if err != nil {
			t.Fatalf("migrator %d: %v", index, err)
		}
	}
	if err := isolated.database.ValidateSchema(t.Context()); err != nil {
		t.Fatalf("validate converged schema: %v", err)
	}

	expected, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	var appliedCount int
	if err := isolated.database.QueryRow(
		t.Context(),
		"SELECT COUNT(*) FROM tokenio_schema_migrations",
	).Scan(&appliedCount); err != nil {
		t.Fatalf("count applied migrations: %v", err)
	}
	if appliedCount != len(expected) {
		t.Fatalf(
			"applied migration count=%d want=%d",
			appliedCount,
			len(expected),
		)
	}
}
