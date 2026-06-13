package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestNormalizeError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "no rows", err: pgx.ErrNoRows, want: ports.ErrNotFound},
		{
			name: "unique violation",
			err:  &pgconn.PgError{Code: "23505"},
			want: ports.ErrStoreConflict,
		},
		{
			name: "serialization failure",
			err:  &pgconn.PgError{Code: "40001"},
			want: ports.ErrStoreConflict,
		},
		{
			name: "check violation",
			err:  &pgconn.PgError{Code: "23514"},
			want: ports.ErrStoreContractViolation,
		},
		{
			name: "undefined table",
			err:  &pgconn.PgError{Code: "42P01"},
			want: ports.ErrStoreContractViolation,
		},
		{
			name: "connection failure",
			err:  &pgconn.PgError{Code: "08006"},
			want: ports.ErrStoreUnavailable,
		},
		{
			name: "unknown postgres error",
			err:  &pgconn.PgError{Code: "XX000"},
			want: ports.ErrStoreUnavailable,
		},
		{
			name: "context canceled",
			err:  context.Canceled,
			want: context.Canceled,
		},
		{
			name: "deadline exceeded",
			err:  context.DeadlineExceeded,
			want: context.DeadlineExceeded,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := NormalizeError(test.err)
			if !errors.Is(got, test.want) {
				t.Fatalf("NormalizeError() = %v, want %v", got, test.want)
			}
		})
	}
}
