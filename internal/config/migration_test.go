package config

import "testing"

func TestLoadMigrationRequiresOnlyDatabaseDSN(t *testing.T) {
	t.Setenv("TOKENIO_DATABASE_DSN", " postgres://tokenio ")

	cfg, err := LoadMigration()
	if err != nil {
		t.Fatalf("LoadMigration: %v", err)
	}
	if cfg.DatabaseDSN != "postgres://tokenio" {
		t.Fatalf("DatabaseDSN = %q, want trimmed DSN", cfg.DatabaseDSN)
	}
}

func TestLoadMigrationRejectsMissingDatabaseDSN(t *testing.T) {
	t.Setenv("TOKENIO_DATABASE_DSN", " ")

	_, err := LoadMigration()
	if err == nil {
		t.Fatal("expected missing TOKENIO_DATABASE_DSN error")
	}
}
