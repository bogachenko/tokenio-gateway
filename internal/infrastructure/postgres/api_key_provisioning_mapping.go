package postgres

import (
	"bytes"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5/pgtype"
)

const apiKeyProvisioningColumns = `
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
`

type provisioningRowScanner interface {
	Scan(dest ...any) error
}

func provisioningTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

func provisioningTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	canonical := provisioningTime(*value)
	return &canonical
}

func provisioningTimeArg(value *time.Time) any {
	if value == nil {
		return nil
	}
	return provisioningTime(*value)
}

func isProvisioningUTCTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	result := make([]byte, len(value))
	copy(result, value)
	return result
}

func scanAPIKeyProvisioning(
	row provisioningRowScanner,
) (domain.APIKeyProvisioning, error) {
	var value domain.APIKeyProvisioning
	var resultType string
	var status string
	var encryptedRawKey []byte
	var encryptionNonce []byte
	var encryptionKeyVersion pgtype.Text
	var expiresAt pgtype.Timestamptz
	var deliveredAt pgtype.Timestamptz
	var expiredAt pgtype.Timestamptz

	if err := row.Scan(
		&value.ID,
		&value.IdempotencyKey,
		&value.SourceReferenceHash,
		&value.ExternalBillingUserID,
		&value.UserID,
		&value.APIKeyID,
		&resultType,
		&status,
		&encryptedRawKey,
		&encryptionNonce,
		&encryptionKeyVersion,
		&value.DeliveryAttempts,
		&value.CreatedAt,
		&value.UpdatedAt,
		&expiresAt,
		&deliveredAt,
		&expiredAt,
	); err != nil {
		return domain.APIKeyProvisioning{},
			normalizeRegistryReadError(err)
	}

	value.ResultType =
		domain.APIKeyProvisioningResultType(resultType)
	value.Status = domain.APIKeyProvisioningStatus(status)
	value.EncryptedRawKey = cloneBytes(encryptedRawKey)
	value.EncryptionNonce = cloneBytes(encryptionNonce)
	value.EncryptionKeyVersion = optionalText(encryptionKeyVersion)
	value.CreatedAt = provisioningTime(value.CreatedAt)
	value.UpdatedAt = provisioningTime(value.UpdatedAt)
	value.ExpiresAt = optionalTime(expiresAt)
	value.DeliveredAt = optionalTime(deliveredAt)
	value.ExpiredAt = optionalTime(expiredAt)

	if err := validateProvisioningRecord(value); err != nil {
		return domain.APIKeyProvisioning{}, err
	}
	return value, nil
}

func validateProvisioningRecord(
	value domain.APIKeyProvisioning,
) error {
	if value.ID == "" ||
		value.IdempotencyKey == "" ||
		value.SourceReferenceHash == "" ||
		value.ExternalBillingUserID == "" ||
		value.UserID == "" ||
		value.APIKeyID == "" ||
		value.DeliveryAttempts < 0 ||
		!isProvisioningUTCTime(value.CreatedAt) ||
		!isProvisioningUTCTime(value.UpdatedAt) ||
		provisioningTime(value.UpdatedAt).Before(
			provisioningTime(value.CreatedAt),
		) ||
		value.ExpiresAt != nil &&
			!isProvisioningUTCTime(*value.ExpiresAt) ||
		value.DeliveredAt != nil &&
			!isProvisioningUTCTime(*value.DeliveredAt) ||
		value.ExpiredAt != nil &&
			!isProvisioningUTCTime(*value.ExpiredAt) {
		return ports.ErrStoreContractViolation
	}

	switch value.ResultType {
	case domain.APIKeyProvisioningResultTypeKeyCreated:
		if value.EncryptionKeyVersion == "" ||
			value.ExpiresAt == nil {
			return ports.ErrStoreContractViolation
		}
	case domain.APIKeyProvisioningResultTypeAlreadyProvisioned:
		if value.Status !=
			domain.APIKeyProvisioningStatusDelivered ||
			value.EncryptionKeyVersion != "" ||
			value.ExpiresAt != nil ||
			value.DeliveryAttempts != 0 {
			return ports.ErrStoreContractViolation
		}
	default:
		return ports.ErrStoreContractViolation
	}

	switch value.Status {
	case domain.APIKeyProvisioningStatusPendingDelivery:
		if value.ResultType !=
			domain.APIKeyProvisioningResultTypeKeyCreated ||
			len(value.EncryptedRawKey) == 0 ||
			len(value.EncryptionNonce) == 0 ||
			value.EncryptionKeyVersion == "" ||
			value.ExpiresAt == nil ||
			!value.ExpiresAt.After(value.CreatedAt) ||
			value.DeliveredAt != nil ||
			value.ExpiredAt != nil {
			return ports.ErrStoreContractViolation
		}

	case domain.APIKeyProvisioningStatusDelivered:
		if len(value.EncryptedRawKey) != 0 ||
			len(value.EncryptionNonce) != 0 ||
			value.DeliveredAt == nil ||
			value.ExpiredAt != nil ||
			value.DeliveredAt.Before(value.CreatedAt) {
			return ports.ErrStoreContractViolation
		}
		if value.ResultType ==
			domain.APIKeyProvisioningResultTypeAlreadyProvisioned &&
			value.ExpiresAt != nil {
			return ports.ErrStoreContractViolation
		}

	case domain.APIKeyProvisioningStatusExpired:
		if value.ResultType !=
			domain.APIKeyProvisioningResultTypeKeyCreated ||
			len(value.EncryptedRawKey) != 0 ||
			len(value.EncryptionNonce) != 0 ||
			value.DeliveredAt != nil ||
			value.ExpiredAt == nil ||
			value.ExpiresAt == nil ||
			value.ExpiredAt.Before(*value.ExpiresAt) {
			return ports.ErrStoreContractViolation
		}

	default:
		return ports.ErrStoreContractViolation
	}
	return nil
}

func canonicalProvisioning(
	value domain.APIKeyProvisioning,
) domain.APIKeyProvisioning {
	result := value
	result.EncryptedRawKey = cloneBytes(value.EncryptedRawKey)
	result.EncryptionNonce = cloneBytes(value.EncryptionNonce)
	result.CreatedAt = provisioningTime(value.CreatedAt)
	result.UpdatedAt = provisioningTime(value.UpdatedAt)
	result.ExpiresAt = provisioningTimePointer(value.ExpiresAt)
	result.DeliveredAt = provisioningTimePointer(value.DeliveredAt)
	result.ExpiredAt = provisioningTimePointer(value.ExpiredAt)
	return result
}

func sameProvisioning(
	left domain.APIKeyProvisioning,
	right domain.APIKeyProvisioning,
) bool {
	return left.ID == right.ID &&
		left.IdempotencyKey == right.IdempotencyKey &&
		left.SourceReferenceHash == right.SourceReferenceHash &&
		left.ExternalBillingUserID ==
			right.ExternalBillingUserID &&
		left.UserID == right.UserID &&
		left.APIKeyID == right.APIKeyID &&
		left.ResultType == right.ResultType &&
		left.Status == right.Status &&
		bytes.Equal(
			left.EncryptedRawKey,
			right.EncryptedRawKey,
		) &&
		bytes.Equal(
			left.EncryptionNonce,
			right.EncryptionNonce,
		) &&
		left.EncryptionKeyVersion ==
			right.EncryptionKeyVersion &&
		left.DeliveryAttempts == right.DeliveryAttempts &&
		provisioningTime(left.CreatedAt).Equal(
			provisioningTime(right.CreatedAt),
		) &&
		provisioningTime(left.UpdatedAt).Equal(
			provisioningTime(right.UpdatedAt),
		) &&
		sameProvisioningTimePointer(
			left.ExpiresAt,
			right.ExpiresAt,
		) &&
		sameProvisioningTimePointer(
			left.DeliveredAt,
			right.DeliveredAt,
		) &&
		sameProvisioningTimePointer(
			left.ExpiredAt,
			right.ExpiredAt,
		)
}

func sameProvisioningTimePointer(
	left *time.Time,
	right *time.Time,
) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return provisioningTime(*left).Equal(
			provisioningTime(*right),
		)
	}
}

func canonicalProvisioningUser(value domain.User) domain.User {
	result := value
	result.CreatedAt = provisioningTime(value.CreatedAt)
	result.UpdatedAt = provisioningTime(value.UpdatedAt)
	result.DisabledAt = provisioningTimePointer(value.DisabledAt)
	return result
}

func validateProvisioningUserCandidate(
	value domain.User,
	externalBillingUserID string,
) error {
	if value.ID == "" ||
		value.ExternalBillingUserID != externalBillingUserID ||
		!value.Enabled ||
		value.DisabledAt != nil ||
		!isProvisioningUTCTime(value.CreatedAt) ||
		!isProvisioningUTCTime(value.UpdatedAt) ||
		!provisioningTime(value.CreatedAt).Equal(
			provisioningTime(value.UpdatedAt),
		) {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func canonicalProvisioningAPIKey(
	value domain.APIKeyRecord,
) domain.APIKeyRecord {
	result := value
	result.CreatedAt = provisioningTime(value.CreatedAt)
	result.UpdatedAt = provisioningTime(value.UpdatedAt)
	result.LastUsedAt = provisioningTimePointer(value.LastUsedAt)
	result.RevokedAt = provisioningTimePointer(value.RevokedAt)
	result.ExpiresAt = provisioningTimePointer(value.ExpiresAt)
	return result
}

func validateProvisioningMaterial(
	request ports.APIKeyProvisioningMaterialRequest,
	material ports.APIKeyProvisioningMaterial,
) error {
	key := material.APIKey
	if key.ID != request.APIKeyID ||
		key.UserID != request.User.ID ||
		key.Name != request.KeyName ||
		key.KeyHash == "" ||
		key.KeyPrefix == "" ||
		!key.Enabled ||
		key.LastUsedAt != nil ||
		key.RevokedAt != nil ||
		key.ExpiresAt != nil ||
		!isProvisioningUTCTime(key.CreatedAt) ||
		!isProvisioningUTCTime(key.UpdatedAt) ||
		!provisioningTime(key.CreatedAt).Equal(
			provisioningTime(request.CreatedAt),
		) ||
		!provisioningTime(key.UpdatedAt).Equal(
			provisioningTime(request.CreatedAt),
		) ||
		len(material.EncryptedRawKey) == 0 ||
		len(material.EncryptionNonce) == 0 ||
		material.EncryptionKeyVersion == "" {
		return ports.ErrStoreContractViolation
	}
	return nil
}
