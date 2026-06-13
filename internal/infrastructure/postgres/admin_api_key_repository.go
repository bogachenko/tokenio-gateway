package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

const adminAPIKeyColumns = `
    id,
    user_id,
    name,
    key_hash,
    key_prefix,
    enabled,
    created_at,
    updated_at,
    last_used_at,
    revoked_at,
    expires_at
`

const findAdminAPIKeyByHashSQL = `
SELECT
` + adminAPIKeyColumns + `
FROM tokenio_api_keys
WHERE key_hash = $1
`

const findAdminAPIKeyByIDSQL = `
SELECT
` + adminAPIKeyColumns + `
FROM tokenio_api_keys
WHERE id = $1
`

const findAdminAPIKeyByIDForUpdateSQL = `
SELECT
` + adminAPIKeyColumns + `
FROM tokenio_api_keys
WHERE id = $1
FOR UPDATE
`

const insertAdminAPIKeySQL = `
INSERT INTO tokenio_api_keys (
    id,
    user_id,
    name,
    key_hash,
    key_prefix,
    enabled,
    created_at,
    updated_at,
    last_used_at,
    revoked_at,
    expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING
` + adminAPIKeyColumns

const updateAdminAPIKeySQL = `
UPDATE tokenio_api_keys
SET
    user_id = $2,
    name = $3,
    key_hash = $4,
    key_prefix = $5,
    enabled = $6,
    created_at = $7,
    updated_at = $8,
    last_used_at = $9,
    revoked_at = $10,
    expires_at = $11
WHERE id = $1
RETURNING
` + adminAPIKeyColumns

type AdminAPIKeyRepository struct {
	db *DB
}

var _ ports.AdminAPIKeyRepository = (*AdminAPIKeyRepository)(nil)

func NewAdminAPIKeyRepository(
	db *DB,
) (*AdminAPIKeyRepository, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &AdminAPIKeyRepository{db: db}, nil
}

func (r *AdminAPIKeyRepository) FindByHash(
	ctx context.Context,
	keyHash string,
) (*domain.APIKeyRecord, error) {
	value, err := scanAPIKey(
		r.db.QueryRow(ctx, findAdminAPIKeyByHashSQL, keyHash),
	)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func (r *AdminAPIKeyRepository) FindAPIKeyByID(
	ctx context.Context,
	apiKeyID string,
) (*domain.APIKeyRecord, error) {
	value, err := scanAPIKey(
		r.db.QueryRow(ctx, findAdminAPIKeyByIDSQL, apiKeyID),
	)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func (r *AdminAPIKeyRepository) ListAPIKeys(
	ctx context.Context,
	filter ports.APIKeyListFilter,
) (ports.Page[domain.APIKeyRecord], error) {
	if err := validateAdminPage(filter.Page); err != nil {
		return ports.Page[domain.APIKeyRecord]{}, err
	}

	where, args := buildAPIKeyFilter(filter)
	var result ports.Page[domain.APIKeyRecord]

	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadOnly,
		},
		func(tx pgx.Tx) error {
			countSQL := "SELECT COUNT(*) FROM tokenio_api_keys" + where
			if err := tx.QueryRow(ctx, countSQL, args...).Scan(&result.Total); err != nil {
				return normalizeRegistryReadError(err)
			}

			listArgs := append([]any(nil), args...)
			limitPosition := len(listArgs) + 1
			listArgs = append(listArgs, filter.Page.Limit)
			offsetPosition := len(listArgs) + 1
			listArgs = append(listArgs, filter.Page.Offset)

			listSQL := `
SELECT
` + adminAPIKeyColumns + `
FROM tokenio_api_keys` + where + fmt.Sprintf(`
ORDER BY created_at DESC, id ASC
LIMIT $%d OFFSET $%d
`, limitPosition, offsetPosition)

			rows, err := tx.Query(ctx, listSQL, listArgs...)
			if err != nil {
				return normalizeRegistryReadError(err)
			}
			defer rows.Close()

			result.Items = make([]domain.APIKeyRecord, 0)
			for rows.Next() {
				value, err := scanAPIKey(rows)
				if err != nil {
					return err
				}
				result.Items = append(result.Items, value)
			}
			if err := rows.Err(); err != nil {
				return normalizeRegistryReadError(err)
			}
			return nil
		},
	)
	if err != nil {
		return ports.Page[domain.APIKeyRecord]{}, err
	}
	return result, nil
}

func (r *AdminAPIKeyRepository) CreateAPIKeyWithAudit(
	ctx context.Context,
	requested domain.APIKeyRecord,
	audit domain.AuditContext,
) error {
	if err := validateAdminAPIKeyRecord(requested); err != nil {
		return err
	}
	if !requested.CreatedAt.Equal(requested.UpdatedAt) ||
		!requested.Enabled ||
		requested.RevokedAt != nil {
		return ports.ErrStoreContractViolation
	}
	if err := validateAuditForEntity(
		audit,
		domain.AuditActionAPIKeyCreate,
		"api_key",
		requested.ID,
		domain.AuditState{},
		adminAPIKeyState(requested),
		requested.CreatedAt,
	); err != nil {
		return err
	}

	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			value, err := scanAPIKey(tx.QueryRow(
				ctx,
				insertAdminAPIKeySQL,
				requested.ID,
				requested.UserID,
				requested.Name,
				requested.KeyHash,
				requested.KeyPrefix,
				requested.Enabled,
				requested.CreatedAt,
				requested.UpdatedAt,
				canonicalTimePointer(requested.LastUsedAt),
				canonicalTimePointer(requested.RevokedAt),
				canonicalTimePointer(requested.ExpiresAt),
			))
			if err != nil {
				return normalizeAdminWriteError(err)
			}
			if !sameAPIKeyPersistence(value, requested) {
				return ports.ErrStoreContractViolation
			}
			return insertAdminAudit(ctx, tx, audit)
		},
	)
	if errors.Is(err, ports.ErrStoreConflict) {
		return ports.ErrAdminConflict
	}
	return err
}

func (r *AdminAPIKeyRepository) CompareAndSwapAPIKeyWithAudit(
	ctx context.Context,
	expected domain.APIKeyRecord,
	next domain.APIKeyRecord,
	audit domain.AuditContext,
) (domain.APIKeyRecord, error) {
	if err := validateAdminAPIKeyRecord(expected); err != nil {
		return domain.APIKeyRecord{}, err
	}
	if err := validateAdminAPIKeyRecord(next); err != nil {
		return domain.APIKeyRecord{}, err
	}
	if expected.ID != next.ID ||
		expected.UserID != next.UserID ||
		expected.Name != next.Name ||
		expected.KeyHash != next.KeyHash ||
		expected.KeyPrefix != next.KeyPrefix ||
		!expected.CreatedAt.Equal(next.CreatedAt) ||
		!sameAdminTimePointer(expected.LastUsedAt, next.LastUsedAt) ||
		!sameAdminTimePointer(expected.ExpiresAt, next.ExpiresAt) ||
		!expected.Enabled ||
		next.Enabled ||
		expected.RevokedAt != nil ||
		next.RevokedAt == nil ||
		!next.RevokedAt.Equal(next.UpdatedAt) ||
		!next.UpdatedAt.After(expected.UpdatedAt) {
		return domain.APIKeyRecord{}, ports.ErrStoreContractViolation
	}

	if err := validateAuditForEntity(
		audit,
		domain.AuditActionAPIKeyRevoke,
		"api_key",
		next.ID,
		adminAPIKeyState(expected),
		adminAPIKeyState(next),
		next.UpdatedAt,
	); err != nil {
		return domain.APIKeyRecord{}, err
	}

	var updated domain.APIKeyRecord
	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			current, err := scanAPIKey(tx.QueryRow(
				ctx,
				findAdminAPIKeyByIDForUpdateSQL,
				expected.ID,
			))
			if err != nil {
				return err
			}
			if !sameAPIKeyPersistence(current, expected) {
				return ports.ErrAdminStateConflict
			}

			value, err := scanAPIKey(tx.QueryRow(
				ctx,
				updateAdminAPIKeySQL,
				next.ID,
				next.UserID,
				next.Name,
				next.KeyHash,
				next.KeyPrefix,
				next.Enabled,
				next.CreatedAt,
				next.UpdatedAt,
				canonicalTimePointer(next.LastUsedAt),
				canonicalTimePointer(next.RevokedAt),
				canonicalTimePointer(next.ExpiresAt),
			))
			if err != nil {
				return normalizeAdminWriteError(err)
			}
			if !sameAPIKeyPersistence(value, next) {
				return ports.ErrStoreContractViolation
			}
			if err := insertAdminAudit(ctx, tx, audit); err != nil {
				return err
			}
			updated = value
			return nil
		},
	)
	if err != nil {
		if errors.Is(err, ports.ErrStoreConflict) {
			return domain.APIKeyRecord{}, ports.ErrAdminStateConflict
		}
		return domain.APIKeyRecord{}, err
	}
	return updated, nil
}

func validateAdminAPIKeyRecord(value domain.APIKeyRecord) error {
	if value.ID == "" ||
		value.UserID == "" ||
		value.Name == "" ||
		value.KeyHash == "" ||
		value.KeyPrefix == "" ||
		!isAdminUTCTime(value.CreatedAt) ||
		!isAdminUTCTime(value.UpdatedAt) ||
		value.UpdatedAt.Before(value.CreatedAt) ||
		value.LastUsedAt != nil &&
			!isAdminUTCTime(*value.LastUsedAt) ||
		value.RevokedAt != nil &&
			!isAdminUTCTime(*value.RevokedAt) ||
		value.ExpiresAt != nil &&
			!isAdminUTCTime(*value.ExpiresAt) {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func buildAPIKeyFilter(
	filter ports.APIKeyListFilter,
) (string, []any) {
	var clauses []string
	var args []any

	if filter.UserID != "" {
		args = append(args, filter.UserID)
		clauses = append(
			clauses,
			fmt.Sprintf("user_id = $%d", len(args)),
		)
	}

	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}
