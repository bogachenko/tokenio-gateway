package postgres

import (
	"errors"
	"testing"
)

func TestPoolConfigForcesUTC(t *testing.T) {
	config, err := poolConfig(
		"postgres://user:password@localhost:5432/tokenio?sslmode=disable",
	)
	if err != nil {
		t.Fatalf("poolConfig: %v", err)
	}
	if got := config.ConnConfig.RuntimeParams["timezone"]; got != "UTC" {
		t.Fatalf("timezone = %q, want UTC", got)
	}
}

func TestPoolConfigRejectsEmptyDSN(t *testing.T) {
	_, err := poolConfig(" ")
	if !errors.Is(err, ErrInvalidDatabaseConfig) {
		t.Fatalf("error = %v, want ErrInvalidDatabaseConfig", err)
	}
}
