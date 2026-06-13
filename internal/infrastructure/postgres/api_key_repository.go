package postgres

import (
	"context"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const findAPIKeyByHashSQL = `
SELECT
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
FROM tokenio_api_keys
WHERE key_hash = $1
`

type APIKeyRepository struct {
	db DBTX
}

var _ ports.APIKeyRepository = (*APIKeyRepository)(nil)

func NewAPIKeyRepository(db DBTX) (*APIKeyRepository, error) {
	if db == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &APIKeyRepository{db: db}, nil
}

func (r *APIKeyRepository) FindByHash(
	ctx context.Context,
	keyHash string,
) (*domain.APIKeyRecord, error) {
	key, err := scanAPIKey(r.db.QueryRow(ctx, findAPIKeyByHashSQL, keyHash))
	if err != nil {
		return nil, err
	}
	return &key, nil
}

func scanAPIKey(row pgx.Row) (domain.APIKeyRecord, error) {
	var value domain.APIKeyRecord
	var lastUsedAt pgtype.Timestamptz
	var revokedAt pgtype.Timestamptz
	var expiresAt pgtype.Timestamptz

	if err := row.Scan(
		&value.ID,
		&value.UserID,
		&value.Name,
		&value.KeyHash,
		&value.KeyPrefix,
		&value.Enabled,
		&value.CreatedAt,
		&value.UpdatedAt,
		&lastUsedAt,
		&revokedAt,
		&expiresAt,
	); err != nil {
		return domain.APIKeyRecord{}, normalizeRegistryReadError(err)
	}

	value.CreatedAt = canonicalTime(value.CreatedAt)
	value.UpdatedAt = canonicalTime(value.UpdatedAt)
	value.LastUsedAt = optionalTime(lastUsedAt)
	value.RevokedAt = optionalTime(revokedAt)
	value.ExpiresAt = optionalTime(expiresAt)
	return value, nil
}
