package postgres

import (
	"context"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const findResellersByIDsSQL = `
SELECT
    id,
    name,
    provider_type,
    base_url,
    api_key_env,
    enabled,
    balance_cents,
    reserved_cents,
    minimum_balance_cents,
    created_at,
    updated_at,
    disabled_at
FROM tokenio_resellers
WHERE id = ANY($1::text[])
ORDER BY id ASC
`

type ResellerRepository struct {
	db DBTX
}

var _ ports.ResellerQueryRepository = (*ResellerRepository)(nil)

func NewResellerRepository(db DBTX) (*ResellerRepository, error) {
	if db == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &ResellerRepository{db: db}, nil
}

func (r *ResellerRepository) FindByIDs(
	ctx context.Context,
	resellerIDs []string,
) (map[string]domain.Reseller, error) {
	ids := uniqueIDs(resellerIDs)
	result := make(map[string]domain.Reseller, len(ids))
	if len(ids) == 0 {
		return result, nil
	}

	rows, err := r.db.Query(ctx, findResellersByIDsSQL, ids)
	if err != nil {
		return nil, normalizeRegistryReadError(err)
	}
	defer rows.Close()

	for rows.Next() {
		value, err := scanReseller(rows)
		if err != nil {
			return nil, err
		}
		if _, exists := result[value.ID]; exists {
			return nil, ports.ErrStoreContractViolation
		}
		result[value.ID] = value
	}
	if err := rows.Err(); err != nil {
		return nil, normalizeRegistryReadError(err)
	}
	return result, nil
}

func scanReseller(row pgx.Row) (domain.Reseller, error) {
	var value domain.Reseller
	var providerType string
	var disabledAt pgtype.Timestamptz

	if err := row.Scan(
		&value.ID,
		&value.Name,
		&providerType,
		&value.BaseURL,
		&value.APIKeyEnv,
		&value.Enabled,
		&value.BalanceCents,
		&value.ReservedCents,
		&value.MinimumBalanceCents,
		&value.CreatedAt,
		&value.UpdatedAt,
		&disabledAt,
	); err != nil {
		return domain.Reseller{}, normalizeRegistryReadError(err)
	}

	value.ProviderType = domain.ProviderType(providerType)
	value.CreatedAt = canonicalTime(value.CreatedAt)
	value.UpdatedAt = canonicalTime(value.UpdatedAt)
	value.DisabledAt = optionalTime(disabledAt)
	return value, nil
}
