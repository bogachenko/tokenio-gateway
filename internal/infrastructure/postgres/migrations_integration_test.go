package postgres

import (
	"os"
	"testing"
)

func TestPostgresMigrationsIntegration(t *testing.T) {
	dsn := os.Getenv("TOKENIO_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TOKENIO_TEST_DATABASE_DSN is not set")
	}

	db, err := Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := db.ApplyMigrations(t.Context()); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	if err := db.ValidateSchema(t.Context()); err != nil {
		t.Fatalf("ValidateSchema: %v", err)
	}
}
