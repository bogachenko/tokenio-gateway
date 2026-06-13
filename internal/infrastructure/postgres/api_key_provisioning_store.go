package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

const findProvisioningByIDSQL = `
SELECT
` + apiKeyProvisioningColumns + `
FROM tokenio_api_key_provisionings
WHERE id = $1
`

const findProvisioningByIDForUpdateSQL = `
SELECT
` + apiKeyProvisioningColumns + `
FROM tokenio_api_key_provisionings
WHERE id = $1
FOR UPDATE
`

const findProvisioningByIdempotencySQL = `
SELECT
` + apiKeyProvisioningColumns + `
FROM tokenio_api_key_provisionings
WHERE idempotency_key = $1
`

const findProvisioningByIdempotencyForUpdateSQL = `
SELECT
` + apiKeyProvisioningColumns + `
FROM tokenio_api_key_provisionings
WHERE idempotency_key = $1
FOR UPDATE
`

const insertProvisioningUserSQL = `
INSERT INTO tokenio_users (
    id,
    external_billing_user_id,
    email,
    name,
    enabled,
    created_at,
    updated_at,
    disabled_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING
    id,
    external_billing_user_id,
    email,
    name,
    enabled,
    created_at,
    updated_at,
    disabled_at
`

const insertProvisioningAPIKeySQL = `
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

const insertAPIKeyProvisioningSQL = `
INSERT INTO tokenio_api_key_provisionings (
    id,
    idempotency_key,
    source_reference_hash,
    external_billing_user_id,
    user_id,
    api_key_id,
    result_type,
    status,
    encrypted_raw_key,
    encryption_nonce,
    encryption_key_version,
    delivery_attempts,
    created_at,
    updated_at,
    expires_at,
    delivered_at,
    expired_at
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9,
    $10, $11, $12, $13, $14, $15, $16, $17
)
RETURNING
` + apiKeyProvisioningColumns

type APIKeyProvisioningStore struct {
	db *DB
}

var _ ports.APIKeyProvisioningStore = (*APIKeyProvisioningStore)(nil)

func NewAPIKeyProvisioningStore(
	db *DB,
) (*APIKeyProvisioningStore, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &APIKeyProvisioningStore{db: db}, nil
}

func (s *APIKeyProvisioningStore) ProvisionAPIKey(
	ctx context.Context,
	request ports.APIKeyProvisioningRequest,
	factory ports.APIKeyProvisioningMaterialFactory,
) (ports.APIKeyProvisioningResult, error) {
	if factory == nil {
		return ports.APIKeyProvisioningResult{},
			ports.ErrStoreContractViolation
	}
	if err := validateProvisioningRequest(request); err != nil {
		return ports.APIKeyProvisioningResult{}, err
	}

	var result ports.APIKeyProvisioningResult
	err := InTx(
		ctx,
		s.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			if err := lockProvisioningScopes(
				ctx,
				tx,
				request.IdempotencyKey,
				request.ExternalBillingUserID,
			); err != nil {
				return err
			}

			existing, err := scanAPIKeyProvisioning(
				tx.QueryRow(
					ctx,
					findProvisioningByIdempotencyForUpdateSQL,
					request.IdempotencyKey,
				),
			)
			switch {
			case err == nil:
				if existing.ExternalBillingUserID !=
					request.ExternalBillingUserID ||
					existing.SourceReferenceHash !=
						request.SourceReferenceHash {
					return ports.ErrStoreConflict
				}
				loaded, err := loadProvisioningResult(
					ctx,
					tx,
					existing,
				)
				if err != nil {
					return err
				}
				switch existing.Status {
				case domain.APIKeyProvisioningStatusPendingDelivery:
					loaded.Outcome =
						ports.APIKeyProvisioningOutcomeReplayedPending
				case domain.APIKeyProvisioningStatusDelivered:
					loaded.Outcome =
						ports.APIKeyProvisioningOutcomeReplayedDelivered
				case domain.APIKeyProvisioningStatusExpired:
					loaded.Outcome =
						ports.APIKeyProvisioningOutcomeExpired
				default:
					return ports.ErrStoreContractViolation
				}
				result = loaded
				return nil

			case errors.Is(err, ports.ErrNotFound):
			default:
				return err
			}

			user, err := resolveProvisioningUser(
				ctx,
				tx,
				request,
			)
			if err != nil {
				return err
			}
			if !user.Enabled || user.DisabledAt != nil {
				return ports.ErrProvisioningUserDisabled
			}

			pendingExists, err := pendingProvisioningExists(
				ctx,
				tx,
				user.ID,
			)
			if err != nil {
				return err
			}
			if pendingExists {
				return ports.ErrStoreConflict
			}

			activeKey, err := findActiveAPIKey(
				ctx,
				tx,
				user.ID,
				request.CreatedAt,
			)
			switch {
			case err == nil:
				provisioning := alreadyProvisionedRecord(
					request,
					user,
					*activeKey,
				)
				persisted, err := insertProvisioning(
					ctx,
					tx,
					provisioning,
				)
				if err != nil {
					return err
				}
				result = ports.APIKeyProvisioningResult{
					Outcome:      ports.APIKeyProvisioningOutcomeAlreadyProvisioned,
					User:         user,
					APIKey:       *activeKey,
					Provisioning: persisted,
				}
				return nil

			case errors.Is(err, ports.ErrNotFound):
			default:
				return err
			}

			materialRequest :=
				ports.APIKeyProvisioningMaterialRequest{
					User:           user,
					ProvisioningID: request.ProvisioningID,
					APIKeyID:       request.APIKeyID,
					KeyName:        request.KeyName,
					CreatedAt:      request.CreatedAt,
					ExpiresAt:      request.ExpiresAt,
				}
			material, err := factory.CreateProvisioningMaterial(
				ctx,
				materialRequest,
			)
			if err != nil {
				return err
			}
			if err := validateProvisioningMaterial(
				materialRequest,
				material,
			); err != nil {
				return err
			}

			key := canonicalProvisioningAPIKey(material.APIKey)
			persistedKey, err := scanAPIKey(tx.QueryRow(
				ctx,
				insertProvisioningAPIKeySQL,
				key.ID,
				key.UserID,
				key.Name,
				key.KeyHash,
				key.KeyPrefix,
				key.Enabled,
				key.CreatedAt,
				key.UpdatedAt,
				provisioningTimeArg(key.LastUsedAt),
				provisioningTimeArg(key.RevokedAt),
				provisioningTimeArg(key.ExpiresAt),
			))
			if err != nil {
				return NormalizeError(err)
			}

			provisioning := domain.APIKeyProvisioning{
				ID:                    request.ProvisioningID,
				IdempotencyKey:        request.IdempotencyKey,
				SourceReferenceHash:   request.SourceReferenceHash,
				ExternalBillingUserID: request.ExternalBillingUserID,
				UserID:                user.ID,
				APIKeyID:              persistedKey.ID,
				ResultType:            domain.APIKeyProvisioningResultTypeKeyCreated,
				Status:                domain.APIKeyProvisioningStatusPendingDelivery,
				EncryptedRawKey:       cloneBytes(material.EncryptedRawKey),
				EncryptionNonce:       cloneBytes(material.EncryptionNonce),
				EncryptionKeyVersion:  material.EncryptionKeyVersion,
				DeliveryAttempts:      0,
				CreatedAt:             request.CreatedAt,
				UpdatedAt:             request.CreatedAt,
				ExpiresAt:             &request.ExpiresAt,
			}
			persistedProvisioning, err := insertProvisioning(
				ctx,
				tx,
				provisioning,
			)
			if err != nil {
				return err
			}

			result = ports.APIKeyProvisioningResult{
				Outcome:      ports.APIKeyProvisioningOutcomeCreated,
				User:         user,
				APIKey:       persistedKey,
				Provisioning: persistedProvisioning,
			}
			return nil
		},
	)
	if err != nil {
		return ports.APIKeyProvisioningResult{}, err
	}
	return result, nil
}

func (s *APIKeyProvisioningStore) FindAPIKeyProvisioningByID(
	ctx context.Context,
	provisioningID string,
) (*domain.APIKeyProvisioning, error) {
	value, err := scanAPIKeyProvisioning(
		s.db.QueryRow(ctx, findProvisioningByIDSQL, provisioningID),
	)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func (s *APIKeyProvisioningStore) FindAPIKeyProvisioningByIdempotencyKey(
	ctx context.Context,
	idempotencyKey string,
) (*domain.APIKeyProvisioning, error) {
	value, err := scanAPIKeyProvisioning(
		s.db.QueryRow(
			ctx,
			findProvisioningByIdempotencySQL,
			idempotencyKey,
		),
	)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func validateProvisioningRequest(
	request ports.APIKeyProvisioningRequest,
) error {
	if request.IdempotencyKey == "" ||
		request.SourceReferenceHash == "" ||
		request.ExternalBillingUserID == "" ||
		request.ProvisioningID == "" ||
		request.APIKeyID == "" ||
		request.KeyName == "" ||
		!isProvisioningUTCTime(request.CreatedAt) ||
		!isProvisioningUTCTime(request.ExpiresAt) ||
		!request.ExpiresAt.After(request.CreatedAt) {
		return ports.ErrStoreContractViolation
	}
	return validateProvisioningUserCandidate(
		request.NewUser,
		request.ExternalBillingUserID,
	)
}

func lockProvisioningScopes(
	ctx context.Context,
	tx pgx.Tx,
	idempotencyKey string,
	externalBillingUserID string,
) error {
	for _, scope := range []string{
		"tokenio_provisioning_idempotency:" + idempotencyKey,
		"tokenio_provisioning_user:" + externalBillingUserID,
	} {
		if _, err := tx.Exec(
			ctx,
			`
SELECT pg_advisory_xact_lock(hashtextextended($1, 0))
`,
			scope,
		); err != nil {
			return NormalizeError(err)
		}
	}
	return nil
}

func resolveProvisioningUser(
	ctx context.Context,
	tx pgx.Tx,
	request ports.APIKeyProvisioningRequest,
) (domain.User, error) {
	user, err := scanUser(tx.QueryRow(
		ctx,
		`
SELECT
    id,
    external_billing_user_id,
    email,
    name,
    enabled,
    created_at,
    updated_at,
    disabled_at
FROM tokenio_users
WHERE external_billing_user_id = $1
FOR UPDATE
`,
		request.ExternalBillingUserID,
	))
	switch {
	case err == nil:
		return user, nil
	case errors.Is(err, ports.ErrNotFound):
	default:
		return domain.User{}, err
	}

	candidate := canonicalProvisioningUser(request.NewUser)
	created, err := scanUser(tx.QueryRow(
		ctx,
		insertProvisioningUserSQL,
		candidate.ID,
		candidate.ExternalBillingUserID,
		nullIfEmpty(candidate.Email),
		nullIfEmpty(candidate.Name),
		candidate.Enabled,
		candidate.CreatedAt,
		candidate.UpdatedAt,
		provisioningTimeArg(candidate.DisabledAt),
	))
	if err != nil {
		return domain.User{}, NormalizeError(err)
	}
	return created, nil
}

func pendingProvisioningExists(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
) (bool, error) {
	var exists bool
	if err := tx.QueryRow(
		ctx,
		`
SELECT EXISTS (
    SELECT 1
    FROM tokenio_api_key_provisionings
    WHERE user_id = $1
      AND status = 'pending_delivery'
)
`,
		userID,
	).Scan(&exists); err != nil {
		return false, normalizeRegistryReadError(err)
	}
	return exists, nil
}

func findActiveAPIKey(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	at time.Time,
) (*domain.APIKeyRecord, error) {
	key, err := scanAPIKey(tx.QueryRow(
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
WHERE user_id = $1
  AND enabled = TRUE
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > $2)
ORDER BY created_at ASC, id ASC
LIMIT 1
FOR UPDATE
`,
		userID,
		provisioningTime(at),
	))
	if err != nil {
		return nil, err
	}
	return &key, nil
}

func alreadyProvisionedRecord(
	request ports.APIKeyProvisioningRequest,
	user domain.User,
	key domain.APIKeyRecord,
) domain.APIKeyProvisioning {
	deliveredAt := request.CreatedAt
	return domain.APIKeyProvisioning{
		ID:                    request.ProvisioningID,
		IdempotencyKey:        request.IdempotencyKey,
		SourceReferenceHash:   request.SourceReferenceHash,
		ExternalBillingUserID: request.ExternalBillingUserID,
		UserID:                user.ID,
		APIKeyID:              key.ID,
		ResultType:            domain.APIKeyProvisioningResultTypeAlreadyProvisioned,
		Status:                domain.APIKeyProvisioningStatusDelivered,
		DeliveryAttempts:      0,
		CreatedAt:             request.CreatedAt,
		UpdatedAt:             request.CreatedAt,
		DeliveredAt:           &deliveredAt,
	}
}

func insertProvisioning(
	ctx context.Context,
	tx pgx.Tx,
	value domain.APIKeyProvisioning,
) (domain.APIKeyProvisioning, error) {
	canonical := canonicalProvisioning(value)
	if err := validateProvisioningRecord(canonical); err != nil {
		return domain.APIKeyProvisioning{}, err
	}
	persisted, err := scanAPIKeyProvisioning(tx.QueryRow(
		ctx,
		insertAPIKeyProvisioningSQL,
		canonical.ID,
		canonical.IdempotencyKey,
		canonical.SourceReferenceHash,
		canonical.ExternalBillingUserID,
		canonical.UserID,
		canonical.APIKeyID,
		string(canonical.ResultType),
		string(canonical.Status),
		nullableBytes(canonical.EncryptedRawKey),
		nullableBytes(canonical.EncryptionNonce),
		nullIfEmpty(canonical.EncryptionKeyVersion),
		canonical.DeliveryAttempts,
		canonical.CreatedAt,
		canonical.UpdatedAt,
		provisioningTimeArg(canonical.ExpiresAt),
		provisioningTimeArg(canonical.DeliveredAt),
		provisioningTimeArg(canonical.ExpiredAt),
	))
	if err != nil {
		return domain.APIKeyProvisioning{}, NormalizeError(err)
	}
	return persisted, nil
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func loadProvisioningResult(
	ctx context.Context,
	tx pgx.Tx,
	provisioning domain.APIKeyProvisioning,
) (ports.APIKeyProvisioningResult, error) {
	user, err := scanUser(tx.QueryRow(
		ctx,
		findUserByIDSQL,
		provisioning.UserID,
	))
	if err != nil {
		return ports.APIKeyProvisioningResult{}, err
	}
	key, err := scanAPIKey(tx.QueryRow(
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
`,
		provisioning.APIKeyID,
	))
	if err != nil {
		return ports.APIKeyProvisioningResult{}, err
	}
	return ports.APIKeyProvisioningResult{
		User:         user,
		APIKey:       key,
		Provisioning: provisioning,
	}, nil
}
