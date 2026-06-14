package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

const resellerBalanceColumns = `
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
`

const findResellerBalanceForUpdateSQL = `
SELECT
` + resellerBalanceColumns + `
FROM tokenio_resellers
WHERE id = $1
FOR UPDATE
`

const updateResellerBalanceReserveSQL = `
UPDATE tokenio_resellers
SET
    reserved_cents = $2,
    updated_at = GREATEST(updated_at, $3)
WHERE id = $1
RETURNING
` + resellerBalanceColumns

type ResellerBalanceStore struct {
	db *DB
}

var _ ports.ResellerBalanceStore = (*ResellerBalanceStore)(nil)

func NewResellerBalanceStore(
	db *DB,
) (*ResellerBalanceStore, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &ResellerBalanceStore{db: db}, nil
}

func (s *ResellerBalanceStore) ReserveEstimatedUpstreamCost(
	ctx context.Context,
	resellerID string,
	amountCents int64,
	reservedAt time.Time,
) (ports.ResellerBalanceReserveResult, error) {
	if err := validateResellerBalanceReserveInput(
		resellerID,
		amountCents,
		reservedAt,
	); err != nil {
		return ports.ResellerBalanceReserveResult{}, err
	}

	canonicalReservedAt := reservedAt.UTC().Truncate(time.Microsecond)
	var result ports.ResellerBalanceReserveResult

	err := InTx(
		ctx,
		s.db,
		pgx.TxOptions{},
		func(tx pgx.Tx) error {
			current, err := scanReseller(tx.QueryRow(
				ctx,
				findResellerBalanceForUpdateSQL,
				resellerID,
			))
			if err != nil {
				return err
			}

			if !domain.CanReserveResellerBalance(
				current,
				amountCents,
			) {
				result = ports.ResellerBalanceReserveResult{
					Applied:  false,
					Reseller: current,
				}
				return nil
			}

			if amountCents == 0 {
				result = ports.ResellerBalanceReserveResult{
					Applied:  true,
					Reseller: current,
				}
				return nil
			}

			expected := current
			expected.ReservedCents += amountCents
			if canonicalReservedAt.After(expected.UpdatedAt) {
				expected.UpdatedAt = canonicalReservedAt
			}

			persisted, err := scanReseller(tx.QueryRow(
				ctx,
				updateResellerBalanceReserveSQL,
				resellerID,
				expected.ReservedCents,
				canonicalReservedAt,
			))
			if err != nil {
				return err
			}
			if !sameResellerBalanceSnapshot(persisted, expected) {
				return ports.ErrStoreContractViolation
			}

			result = ports.ResellerBalanceReserveResult{
				Applied:  true,
				Reseller: persisted,
			}
			return nil
		},
	)
	if err != nil {
		return ports.ResellerBalanceReserveResult{}, err
	}
	return result, nil
}

func validateResellerBalanceReserveInput(
	resellerID string,
	amountCents int64,
	reservedAt time.Time,
) error {
	if strings.TrimSpace(resellerID) == "" ||
		amountCents < 0 ||
		reservedAt.IsZero() ||
		reservedAt.Location() != time.UTC {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func sameResellerBalanceSnapshot(
	left domain.Reseller,
	right domain.Reseller,
) bool {
	return left.ID == right.ID &&
		left.Name == right.Name &&
		left.ProviderType == right.ProviderType &&
		left.BaseURL == right.BaseURL &&
		left.APIKeyEnv == right.APIKeyEnv &&
		left.Enabled == right.Enabled &&
		left.BalanceCents == right.BalanceCents &&
		left.ReservedCents == right.ReservedCents &&
		left.MinimumBalanceCents == right.MinimumBalanceCents &&
		left.CreatedAt.Equal(right.CreatedAt) &&
		left.UpdatedAt.Equal(right.UpdatedAt) &&
		sameResellerBalanceTimePointer(
			left.DisabledAt,
			right.DisabledAt,
		)
}

func sameResellerBalanceTimePointer(
	left *time.Time,
	right *time.Time,
) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return left.Equal(*right)
	}
}
