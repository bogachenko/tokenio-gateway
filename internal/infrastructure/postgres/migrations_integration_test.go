package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresMigrationsIntegration(t *testing.T) {
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
		"tokenio_migrations_test_%d",
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

	var initialTableCount int
	if err := control.QueryRow(
		ctx,
		`
SELECT COUNT(*)
FROM information_schema.tables
WHERE table_schema = $1
`,
		schemaName,
	).Scan(&initialTableCount); err != nil {
		t.Fatalf("count initial schema tables: %v", err)
	}
	if initialTableCount != 0 {
		t.Fatalf(
			"isolated schema table count = %d, want 0",
			initialTableCount,
		)
	}

	config, err := poolConfig(dsn)
	if err != nil {
		t.Fatalf("poolConfig: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open isolated database pool: %v", err)
	}
	db := &DB{pool: pool}
	t.Cleanup(db.Close)
	if err := db.Ping(ctx); err != nil {
		t.Fatalf("ping isolated database: %v", err)
	}

	for run := 1; run <= 2; run++ {
		if err := db.ApplyMigrations(ctx); err != nil {
			t.Fatalf("ApplyMigrations run %d: %v", run, err)
		}
		if err := db.ValidateSchema(ctx); err != nil {
			t.Fatalf("ValidateSchema run %d: %v", run, err)
		}
	}

	expected, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	var appliedCount int
	if err := db.QueryRow(
		ctx,
		"SELECT COUNT(*) FROM tokenio_schema_migrations",
	).Scan(&appliedCount); err != nil {
		t.Fatalf("count applied migrations: %v", err)
	}
	if appliedCount != len(expected) {
		t.Fatalf(
			"applied migration count = %d, want %d",
			appliedCount,
			len(expected),
		)
	}
}
