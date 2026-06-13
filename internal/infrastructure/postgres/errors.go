package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrInvalidDatabaseConfig = errors.New("invalid database configuration")

// NormalizeError converts PostgreSQL and pgx errors into safe port-level store
// errors. Callers must pass only errors produced by database operations.
func NormalizeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, ports.ErrNotFound) ||
		errors.Is(err, ports.ErrStoreUnavailable) ||
		errors.Is(err, ports.ErrStoreConflict) ||
		errors.Is(err, ports.ErrStoreContractViolation) ||
		errors.Is(err, ErrInvalidDatabaseConfig) {
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}

	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		return normalizeSQLState(pgError.Code)
	}

	var parseError *pgconn.ParseConfigError
	if errors.As(err, &parseError) {
		return ErrInvalidDatabaseConfig
	}

	if pgconn.Timeout(err) || pgconn.SafeToRetry(err) {
		return ports.ErrStoreUnavailable
	}
	return ports.ErrStoreUnavailable
}

func normalizeSQLState(code string) error {
	switch code {
	case
		"23503",
		"23505",
		"23P01",
		"40001",
		"40P01":
		return ports.ErrStoreConflict

	case
		"22000",
		"22003",
		"22007",
		"22008",
		"22023",
		"23502",
		"23514",
		"3F000",
		"42P01",
		"42703":
		return ports.ErrStoreContractViolation
	}

	if strings.HasPrefix(code, "08") ||
		strings.HasPrefix(code, "53") ||
		code == "57P01" ||
		code == "57P02" ||
		code == "57P03" {
		return ports.ErrStoreUnavailable
	}
	return ports.ErrStoreUnavailable
}
