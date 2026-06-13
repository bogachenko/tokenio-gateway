package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

func (s *APIKeyProvisioningStore) RecordAPIKeyDeliveryAttempt(
	ctx context.Context,
	provisioningID string,
	attemptedAt time.Time,
) (domain.APIKeyProvisioning, error) {
	if provisioningID == "" ||
		!isProvisioningUTCTime(attemptedAt) {
		return domain.APIKeyProvisioning{},
			ports.ErrStoreContractViolation
	}

	var result domain.APIKeyProvisioning
	err := InTx(
		ctx,
		s.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			current, err := scanAPIKeyProvisioning(
				tx.QueryRow(
					ctx,
					findProvisioningByIDForUpdateSQL,
					provisioningID,
				),
			)
			if err != nil {
				return err
			}
			switch current.Status {
			case domain.APIKeyProvisioningStatusPendingDelivery:
			case domain.APIKeyProvisioningStatusExpired:
				return ports.ErrProvisioningExpired
			default:
				return ports.ErrStoreConflict
			}
			if provisioningTime(attemptedAt).Before(
				current.UpdatedAt,
			) {
				return ports.ErrStoreContractViolation
			}

			updated, err := scanAPIKeyProvisioning(
				tx.QueryRow(
					ctx,
					`
UPDATE tokenio_api_key_provisionings
SET delivery_attempts = delivery_attempts + 1,
    updated_at = $2
WHERE id = $1
  AND status = 'pending_delivery'
RETURNING
`+apiKeyProvisioningColumns,
					provisioningID,
					provisioningTime(attemptedAt),
				),
			)
			if err != nil {
				return NormalizeError(err)
			}
			result = updated
			return nil
		},
	)
	if err != nil {
		return domain.APIKeyProvisioning{}, err
	}
	return result, nil
}

func (s *APIKeyProvisioningStore) ConfirmAPIKeyDelivery(
	ctx context.Context,
	provisioningID string,
	deliveredAt time.Time,
) (domain.APIKeyProvisioning, error) {
	if provisioningID == "" ||
		!isProvisioningUTCTime(deliveredAt) {
		return domain.APIKeyProvisioning{},
			ports.ErrStoreContractViolation
	}

	var result domain.APIKeyProvisioning
	err := InTx(
		ctx,
		s.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			current, err := scanAPIKeyProvisioning(
				tx.QueryRow(
					ctx,
					findProvisioningByIDForUpdateSQL,
					provisioningID,
				),
			)
			if err != nil {
				return err
			}

			switch current.Status {
			case domain.APIKeyProvisioningStatusDelivered:
				result = current
				return nil
			case domain.APIKeyProvisioningStatusExpired:
				return ports.ErrProvisioningExpired
			case domain.APIKeyProvisioningStatusPendingDelivery:
			default:
				return ports.ErrStoreContractViolation
			}

			canonicalDeliveredAt := provisioningTime(deliveredAt)
			if canonicalDeliveredAt.Before(current.CreatedAt) ||
				canonicalDeliveredAt.Before(current.UpdatedAt) ||
				current.ExpiresAt == nil ||
				canonicalDeliveredAt.After(*current.ExpiresAt) {
				return ports.ErrStoreConflict
			}

			updated, err := scanAPIKeyProvisioning(
				tx.QueryRow(
					ctx,
					`
UPDATE tokenio_api_key_provisionings
SET status = 'delivered',
    encrypted_raw_key = NULL,
    encryption_nonce = NULL,
    updated_at = $2,
    delivered_at = $2
WHERE id = $1
  AND status = 'pending_delivery'
RETURNING
`+apiKeyProvisioningColumns,
					provisioningID,
					canonicalDeliveredAt,
				),
			)
			if err != nil {
				return NormalizeError(err)
			}
			result = updated
			return nil
		},
	)
	if err != nil {
		return domain.APIKeyProvisioning{}, err
	}
	return result, nil
}

func (s *APIKeyProvisioningStore) ListPendingAPIKeyProvisioningsDue(
	ctx context.Context,
	asOf time.Time,
	limit int,
) ([]domain.APIKeyProvisioning, error) {
	if !isProvisioningUTCTime(asOf) || limit <= 0 {
		return nil, ports.ErrStoreContractViolation
	}

	rows, err := s.db.Query(
		ctx,
		`
SELECT
`+apiKeyProvisioningColumns+`
FROM tokenio_api_key_provisionings
WHERE status = 'pending_delivery'
  AND expires_at <= $1
ORDER BY expires_at ASC, id ASC
LIMIT $2
`,
		provisioningTime(asOf),
		limit,
	)
	if err != nil {
		return nil, normalizeRegistryReadError(err)
	}
	defer rows.Close()

	result := make([]domain.APIKeyProvisioning, 0)
	for rows.Next() {
		value, err := scanAPIKeyProvisioning(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, normalizeRegistryReadError(err)
	}
	return result, nil
}

func (s *APIKeyProvisioningStore) ExpireAPIKeyProvisioning(
	ctx context.Context,
	provisioningID string,
	expiredAt time.Time,
) (domain.APIKeyProvisioning, error) {
	if provisioningID == "" ||
		!isProvisioningUTCTime(expiredAt) {
		return domain.APIKeyProvisioning{},
			ports.ErrStoreContractViolation
	}

	var result domain.APIKeyProvisioning
	err := InTx(
		ctx,
		s.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			current, err := scanAPIKeyProvisioning(
				tx.QueryRow(
					ctx,
					findProvisioningByIDForUpdateSQL,
					provisioningID,
				),
			)
			if err != nil {
				return err
			}

			switch current.Status {
			case domain.APIKeyProvisioningStatusExpired:
				result = current
				return nil
			case domain.APIKeyProvisioningStatusDelivered:
				return ports.ErrStoreConflict
			case domain.APIKeyProvisioningStatusPendingDelivery:
			default:
				return ports.ErrStoreContractViolation
			}

			canonicalExpiredAt := provisioningTime(expiredAt)
			if current.ExpiresAt == nil ||
				canonicalExpiredAt.Before(*current.ExpiresAt) ||
				canonicalExpiredAt.Before(current.UpdatedAt) {
				return ports.ErrStoreConflict
			}

			var key domain.APIKeyRecord
			key, err = scanAPIKey(tx.QueryRow(
				ctx,
				`
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
WHERE id = $1
FOR UPDATE
`,
				current.APIKeyID,
			))
			if err != nil {
				return err
			}
			if key.UserID != current.UserID ||
				!key.Enabled ||
				key.RevokedAt != nil ||
				canonicalExpiredAt.Before(key.UpdatedAt) {
				return ports.ErrStoreConflict
			}

			tag, err := tx.Exec(
				ctx,
				`
UPDATE tokenio_api_keys
SET enabled = FALSE,
    revoked_at = $2,
    updated_at = $2
WHERE id = $1
  AND enabled = TRUE
  AND revoked_at IS NULL
`,
				current.APIKeyID,
				canonicalExpiredAt,
			)
			if err != nil {
				return NormalizeError(err)
			}
			if tag.RowsAffected() != 1 {
				return ports.ErrStoreConflict
			}

			updated, err := scanAPIKeyProvisioning(
				tx.QueryRow(
					ctx,
					`
UPDATE tokenio_api_key_provisionings
SET status = 'expired',
    encrypted_raw_key = NULL,
    encryption_nonce = NULL,
    updated_at = $2,
    expired_at = $2
WHERE id = $1
  AND status = 'pending_delivery'
RETURNING
`+apiKeyProvisioningColumns,
					provisioningID,
					canonicalExpiredAt,
				),
			)
			if err != nil {
				return NormalizeError(err)
			}
			result = updated
			return nil
		},
	)
	if err != nil {
		if errors.Is(err, ports.ErrStoreConflict) {
			return domain.APIKeyProvisioning{},
				ports.ErrStoreConflict
		}
		return domain.APIKeyProvisioning{}, err
	}
	return result, nil
}
