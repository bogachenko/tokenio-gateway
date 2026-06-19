//go:build integration

package integration_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestPostgresIntegrationEnvironment(t *testing.T) {
	t.Parallel()

	dsn := os.Getenv("TOKENIO_INTEGRATION_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TOKENIO_INTEGRATION_DATABASE_DSN is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer conn.Close(context.Background())

	if err := conn.Ping(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
}
