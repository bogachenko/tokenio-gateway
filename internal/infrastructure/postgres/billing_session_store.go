package postgres

import (
	"context"
	"errors"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

const billingSessionColumns = `
    user_id,
    billing_subject_user_id,
    remote_balance_cents,
    pending_amount_cents_cached,
    currency,
    fetched_at,
    created_at,
    updated_at
`

const findBillingSessionSQL = `
SELECT
` + billingSessionColumns + `
FROM tokenio_billing_sessions
WHERE user_id = $1
`

const findBillingSessionForUpdateSQL = `
SELECT
` + billingSessionColumns + `
FROM tokenio_billing_sessions
WHERE user_id = $1
FOR UPDATE
`

const insertBillingSessionSQL = `
INSERT INTO tokenio_billing_sessions (
    user_id,
    billing_subject_user_id,
    remote_balance_cents,
    pending_amount_cents_cached,
    currency,
    fetched_at,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING
` + billingSessionColumns

const updateBillingSessionSQL = `
UPDATE tokenio_billing_sessions
SET
    billing_subject_user_id = $2,
    remote_balance_cents = $3,
    pending_amount_cents_cached = $4,
    currency = $5,
    fetched_at = $6,
    created_at = $7,
    updated_at = $8
WHERE user_id = $1
RETURNING
` + billingSessionColumns

type BillingSessionStore struct {
	db *DB
}

var _ ports.BillingSessionStore = (*BillingSessionStore)(nil)

func NewBillingSessionStore(
	db *DB,
) (*BillingSessionStore, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &BillingSessionStore{db: db}, nil
}

func (s *BillingSessionStore) FindBillingSessionByUserID(
	ctx context.Context,
	userID string,
) (*domain.BillingSession, error) {
	value, err := scanBillingSession(
		s.db.QueryRow(ctx, findBillingSessionSQL, userID),
	)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func (s *BillingSessionStore) UpsertBillingSession(
	ctx context.Context,
	expected *domain.BillingSession,
	next domain.BillingSession,
) (domain.BillingSession, error) {
	if err := validateBillingSessionPersistence(next); err != nil {
		return domain.BillingSession{}, err
	}

	if expected == nil {
		if !operationalTime(next.CreatedAt).Equal(
			operationalTime(next.UpdatedAt),
		) {
			return domain.BillingSession{},
				ports.ErrStoreContractViolation
		}
	} else {
		if err := validateBillingSessionPersistence(
			*expected,
		); err != nil {
			return domain.BillingSession{}, err
		}
		if expected.UserID != next.UserID ||
			expected.BillingSubjectUserID !=
				next.BillingSubjectUserID ||
			expected.Currency != next.Currency ||
			!operationalTime(expected.CreatedAt).Equal(
				operationalTime(next.CreatedAt),
			) ||
			operationalTime(next.FetchedAt).Before(
				operationalTime(expected.FetchedAt),
			) ||
			!operationalTime(next.UpdatedAt).After(
				operationalTime(expected.UpdatedAt),
			) {
			return domain.BillingSession{},
				ports.ErrStoreContractViolation
		}
	}

	persistedNext := canonicalBillingSession(next)
	var result domain.BillingSession

	err := InTx(
		ctx,
		s.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			if err := validateBillingSessionSubject(
				ctx,
				tx,
				persistedNext.UserID,
				persistedNext.BillingSubjectUserID,
			); err != nil {
				return err
			}

			if expected == nil {
				_, err := scanBillingSession(tx.QueryRow(
					ctx,
					findBillingSessionForUpdateSQL,
					persistedNext.UserID,
				))
				switch {
				case err == nil:
					return ports.ErrStoreConflict
				case errors.Is(err, ports.ErrNotFound):
				default:
					return err
				}

				value, err := scanBillingSession(tx.QueryRow(
					ctx,
					insertBillingSessionSQL,
					persistedNext.UserID,
					persistedNext.BillingSubjectUserID,
					persistedNext.RemoteBalanceCents,
					persistedNext.PendingAmountCentsCached,
					persistedNext.Currency,
					persistedNext.FetchedAt,
					persistedNext.CreatedAt,
					persistedNext.UpdatedAt,
				))
				if err != nil {
					return NormalizeError(err)
				}
				if !sameBillingSession(value, persistedNext) {
					return ports.ErrStoreContractViolation
				}
				result = value
				return nil
			}

			current, err := scanBillingSession(tx.QueryRow(
				ctx,
				findBillingSessionForUpdateSQL,
				expected.UserID,
			))
			if err != nil {
				return err
			}
			if !sameBillingSession(current, *expected) {
				return ports.ErrStoreConflict
			}

			value, err := scanBillingSession(tx.QueryRow(
				ctx,
				updateBillingSessionSQL,
				persistedNext.UserID,
				persistedNext.BillingSubjectUserID,
				persistedNext.RemoteBalanceCents,
				persistedNext.PendingAmountCentsCached,
				persistedNext.Currency,
				persistedNext.FetchedAt,
				persistedNext.CreatedAt,
				persistedNext.UpdatedAt,
			))
			if err != nil {
				return NormalizeError(err)
			}
			if !sameBillingSession(value, persistedNext) {
				return ports.ErrStoreContractViolation
			}
			result = value
			return nil
		},
	)
	if err != nil {
		return domain.BillingSession{}, err
	}
	return result, nil
}

func validateBillingSessionSubject(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	billingSubjectUserID string,
) error {
	var persistedSubject string
	if err := tx.QueryRow(
		ctx,
		`
SELECT external_billing_user_id
FROM tokenio_users
WHERE id = $1
FOR KEY SHARE
`,
		userID,
	).Scan(&persistedSubject); err != nil {
		return normalizeRegistryReadError(err)
	}
	if persistedSubject != billingSubjectUserID {
		return ports.ErrStoreConflict
	}
	return nil
}
