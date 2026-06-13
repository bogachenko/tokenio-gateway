package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DBTX interface {
	Exec(
		ctx context.Context,
		sql string,
		arguments ...any,
	) (pgconn.CommandTag, error)
	Query(
		ctx context.Context,
		sql string,
		arguments ...any,
	) (pgx.Rows, error)
	QueryRow(
		ctx context.Context,
		sql string,
		arguments ...any,
	) pgx.Row
}

type TxBeginner interface {
	BeginTx(
		ctx context.Context,
		options pgx.TxOptions,
	) (pgx.Tx, error)
}

func InTx(
	ctx context.Context,
	beginner TxBeginner,
	options pgx.TxOptions,
	fn func(pgx.Tx) error,
) error {
	if beginner == nil || fn == nil {
		return ErrInvalidDatabaseConfig
	}

	tx, err := beginner.BeginTx(ctx, options)
	if err != nil {
		return NormalizeError(err)
	}
	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	if err := fn(tx); err != nil {
		return err
	}
	return NormalizeError(tx.Commit(ctx))
}
