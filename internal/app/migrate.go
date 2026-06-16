package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/postgres"
)

type migrationDatabase interface {
	ApplyMigrations(context.Context) error
	ValidateSchema(context.Context) error
	Close()
}

type migrationDatabaseOpener func(
	context.Context,
	string,
) (migrationDatabase, error)

func RunMigrations(ctx context.Context, databaseDSN string) error {
	return runMigrations(
		ctx,
		databaseDSN,
		func(
			ctx context.Context,
			dsn string,
		) (migrationDatabase, error) {
			return postgres.Open(ctx, dsn)
		},
	)
}

func runMigrations(
	ctx context.Context,
	databaseDSN string,
	opener migrationDatabaseOpener,
) error {
	if ctx == nil {
		return fmt.Errorf("migration context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(databaseDSN) == "" {
		return fmt.Errorf("TOKENIO_DATABASE_DSN is required")
	}
	if opener == nil {
		return fmt.Errorf("migration database opener is required")
	}

	database, err := opener(ctx, databaseDSN)
	if err != nil {
		return fmt.Errorf("open PostgreSQL for migrations: %w", err)
	}
	if database == nil {
		return fmt.Errorf("migration database opener returned nil database")
	}
	defer database.Close()

	if err := database.ApplyMigrations(ctx); err != nil {
		return fmt.Errorf("apply PostgreSQL migrations: %w", err)
	}
	if err := database.ValidateSchema(ctx); err != nil {
		return fmt.Errorf("validate PostgreSQL schema after migrations: %w", err)
	}
	return nil
}
