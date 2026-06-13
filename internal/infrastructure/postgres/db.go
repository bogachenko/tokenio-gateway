package postgres

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, dsn string) (*DB, error) {
	config, err := poolConfig(dsn)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, NormalizeError(err)
	}

	db := &DB{pool: pool}
	if err := db.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return db, nil
}

func poolConfig(dsn string) (*pgxpool.Config, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, ErrInvalidDatabaseConfig
	}

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, ErrInvalidDatabaseConfig
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.RuntimeParams["timezone"] = "UTC"
	return config, nil
}

func (db *DB) Close() {
	if db == nil || db.pool == nil {
		return
	}
	db.pool.Close()
}

func (db *DB) Ping(ctx context.Context) error {
	if db == nil || db.pool == nil {
		return ErrInvalidDatabaseConfig
	}
	return NormalizeError(db.pool.Ping(ctx))
}

func (db *DB) BeginTx(
	ctx context.Context,
	options pgx.TxOptions,
) (pgx.Tx, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	tx, err := db.pool.BeginTx(ctx, options)
	if err != nil {
		return nil, NormalizeError(err)
	}
	return tx, nil
}

func (db *DB) Exec(
	ctx context.Context,
	sql string,
	arguments ...any,
) (pgconn.CommandTag, error) {
	if db == nil || db.pool == nil {
		return pgconn.CommandTag{}, ErrInvalidDatabaseConfig
	}
	tag, err := db.pool.Exec(ctx, sql, arguments...)
	return tag, NormalizeError(err)
}

func (db *DB) Query(
	ctx context.Context,
	sql string,
	arguments ...any,
) (pgx.Rows, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	rows, err := db.pool.Query(ctx, sql, arguments...)
	if err != nil {
		return nil, NormalizeError(err)
	}
	return rows, nil
}

func (db *DB) QueryRow(
	ctx context.Context,
	sql string,
	arguments ...any,
) pgx.Row {
	if db == nil || db.pool == nil {
		return errorRow{err: ErrInvalidDatabaseConfig}
	}
	return db.pool.QueryRow(ctx, sql, arguments...)
}

type errorRow struct {
	err error
}

func (r errorRow) Scan(_ ...any) error {
	return r.err
}
