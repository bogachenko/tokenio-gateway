package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func openIsolatedPostgresIntegrationDB(t *testing.T) *DB {
	t.Helper()

	dsn := os.Getenv("TOKENIO_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TOKENIO_TEST_DATABASE_DSN is not set")
	}

	ctx := t.Context()
	adminDB, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open schema administrator DB: %v", err)
	}

	schema := "tokenio_test_" + randomIntegrationSchemaSuffix(t)
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := adminDB.Exec(
		ctx,
		"CREATE SCHEMA "+identifier,
	); err != nil {
		adminDB.Close()
		t.Fatalf("create isolated schema: %v", err)
	}

	config, err := poolConfig(dsn)
	if err != nil {
		_, _ = adminDB.Exec(
			context.Background(),
			"DROP SCHEMA "+identifier+" CASCADE",
		)
		adminDB.Close()
		t.Fatalf("parse isolated database config: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		_, _ = adminDB.Exec(
			context.Background(),
			"DROP SCHEMA "+identifier+" CASCADE",
		)
		adminDB.Close()
		t.Fatalf("open isolated database pool: %v", err)
	}
	db := &DB{pool: pool}
	if err := db.Ping(ctx); err != nil {
		db.Close()
		_, _ = adminDB.Exec(
			context.Background(),
			"DROP SCHEMA "+identifier+" CASCADE",
		)
		adminDB.Close()
		t.Fatalf("ping isolated database: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
		if _, err := adminDB.Exec(
			context.Background(),
			"DROP SCHEMA "+identifier+" CASCADE",
		); err != nil {
			t.Errorf("drop isolated schema %s: %v", schema, err)
		}
		adminDB.Close()
	})

	if err := db.ApplyMigrations(ctx); err != nil {
		t.Fatalf("apply isolated migrations: %v", err)
	}
	return db
}

func randomIntegrationSchemaSuffix(t *testing.T) string {
	t.Helper()

	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		t.Fatalf("generate isolated schema suffix: %v", err)
	}
	return hex.EncodeToString(value[:])
}
