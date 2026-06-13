package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"

	migrationfiles "github.com/bogachenko/tokenio-gateway/db/migrations"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const migrationLockID int64 = 0x746f6b656e696f

const createMigrationTableSQL = `
CREATE TABLE IF NOT EXISTS tokenio_schema_migrations (
    version BIGINT PRIMARY KEY,
    name TEXT NOT NULL,
    checksum TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`

var migrationFilenamePattern = regexp.MustCompile(
	`^([0-9]{6})_([a-z0-9_]+)\.up\.sql$`,
)

type migration struct {
	Version  int64
	Name     string
	Filename string
	SQL      string
	Checksum string
}

func (db *DB) ApplyMigrations(ctx context.Context) error {
	if db == nil || db.pool == nil {
		return ErrInvalidDatabaseConfig
	}

	items, err := loadMigrations()
	if err != nil {
		return err
	}

	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		return NormalizeError(err)
	}
	defer conn.Release()

	if _, err := conn.Exec(
		ctx,
		"SELECT pg_advisory_lock($1)",
		migrationLockID,
	); err != nil {
		return NormalizeError(err)
	}
	defer func() {
		_, _ = conn.Exec(
			context.WithoutCancel(ctx),
			"SELECT pg_advisory_unlock($1)",
			migrationLockID,
		)
	}()

	if _, err := conn.Exec(ctx, createMigrationTableSQL); err != nil {
		return NormalizeError(err)
	}

	for _, item := range items {
		applied, err := migrationApplied(ctx, conn, item)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := applyMigration(ctx, conn, item); err != nil {
			return err
		}
	}

	return validateSchema(ctx, conn, items)
}

func (db *DB) ValidateSchema(ctx context.Context) error {
	if db == nil || db.pool == nil {
		return ErrInvalidDatabaseConfig
	}
	items, err := loadMigrations()
	if err != nil {
		return err
	}
	return validateSchema(ctx, db.pool, items)
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationfiles.Files, ".")
	if err != nil {
		return nil, ports.ErrStoreContractViolation
	}

	result := make([]migration, 0, len(entries))
	seenVersions := make(map[int64]struct{})
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}

		matches := migrationFilenamePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			return nil, ports.ErrStoreContractViolation
		}
		version, err := strconv.ParseInt(matches[1], 10, 64)
		if err != nil || version <= 0 {
			return nil, ports.ErrStoreContractViolation
		}
		if _, exists := seenVersions[version]; exists {
			return nil, ports.ErrStoreContractViolation
		}
		seenVersions[version] = struct{}{}

		content, err := fs.ReadFile(migrationfiles.Files, entry.Name())
		if err != nil || len(strings.TrimSpace(string(content))) == 0 {
			return nil, ports.ErrStoreContractViolation
		}
		sum := sha256.Sum256(content)
		result = append(result, migration{
			Version:  version,
			Name:     matches[2],
			Filename: entry.Name(),
			SQL:      string(content),
			Checksum: hex.EncodeToString(sum[:]),
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Version < result[j].Version
	})
	if len(result) == 0 {
		return nil, ports.ErrStoreContractViolation
	}
	for index, item := range result {
		if item.Version != int64(index+1) {
			return nil, ports.ErrStoreContractViolation
		}
	}
	return result, nil
}

func migrationApplied(
	ctx context.Context,
	conn *pgxpool.Conn,
	item migration,
) (bool, error) {
	var name string
	var checksum string
	err := conn.QueryRow(
		ctx,
		`
SELECT name, checksum
FROM tokenio_schema_migrations
WHERE version = $1
`,
		item.Version,
	).Scan(&name, &checksum)
	if err == nil {
		if name != item.Name || checksum != item.Checksum {
			return false, ports.ErrStoreContractViolation
		}
		return true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return false, NormalizeError(err)
}

func applyMigration(
	ctx context.Context,
	conn *pgxpool.Conn,
	item migration,
) error {
	return InTx(ctx, conn, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := tx.Conn().PgConn().Exec(ctx, item.SQL).ReadAll(); err != nil {
			return NormalizeError(err)
		}
		if _, err := tx.Exec(
			ctx,
			`
INSERT INTO tokenio_schema_migrations (
    version,
    name,
    checksum
)
VALUES ($1, $2, $3)
`,
			item.Version,
			item.Name,
			item.Checksum,
		); err != nil {
			return NormalizeError(err)
		}
		return nil
	})
}

func validateSchema(
	ctx context.Context,
	db DBTX,
	expected []migration,
) error {
	rows, err := db.Query(
		ctx,
		`
SELECT version, name, checksum
FROM tokenio_schema_migrations
ORDER BY version ASC
`,
	)
	if err != nil {
		return NormalizeError(err)
	}
	defer rows.Close()

	actual := make([]migration, 0, len(expected))
	for rows.Next() {
		var item migration
		if err := rows.Scan(
			&item.Version,
			&item.Name,
			&item.Checksum,
		); err != nil {
			return ports.ErrStoreContractViolation
		}
		actual = append(actual, item)
	}
	if err := rows.Err(); err != nil {
		return NormalizeError(err)
	}
	if len(actual) != len(expected) {
		return ports.ErrStoreContractViolation
	}
	for index := range expected {
		if actual[index].Version != expected[index].Version ||
			actual[index].Name != expected[index].Name ||
			actual[index].Checksum != expected[index].Checksum {
			return ports.ErrStoreContractViolation
		}
	}
	return nil
}
