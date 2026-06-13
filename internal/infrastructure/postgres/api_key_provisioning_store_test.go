package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestNewAPIKeyProvisioningStoreRejectsNilDB(t *testing.T) {
	_, err := NewAPIKeyProvisioningStore(nil)
	if !errors.Is(err, ErrInvalidDatabaseConfig) {
		t.Fatalf("error = %v, want invalid database config", err)
	}
}

func TestValidateProvisioningRecordLifecycle(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	expiresAt := now.Add(time.Hour)

	pending := domain.APIKeyProvisioning{
		ID:                    "prov-1",
		IdempotencyKey:        "idem-1",
		SourceReferenceHash:   "source-hash",
		ExternalBillingUserID: "billing-user-1",
		UserID:                "user-1",
		APIKeyID:              "key-1",
		ResultType:            domain.APIKeyProvisioningResultTypeKeyCreated,
		Status:                domain.APIKeyProvisioningStatusPendingDelivery,
		EncryptedRawKey:       []byte("ciphertext"),
		EncryptionNonce:       []byte("nonce"),
		EncryptionKeyVersion:  "v1",
		CreatedAt:             now,
		UpdatedAt:             now,
		ExpiresAt:             &expiresAt,
	}
	if err := validateProvisioningRecord(pending); err != nil {
		t.Fatalf("pending validation: %v", err)
	}

	deliveredAt := now.Add(time.Minute)
	delivered := pending
	delivered.Status = domain.APIKeyProvisioningStatusDelivered
	delivered.EncryptedRawKey = nil
	delivered.EncryptionNonce = nil
	delivered.UpdatedAt = deliveredAt
	delivered.DeliveredAt = &deliveredAt
	if err := validateProvisioningRecord(delivered); err != nil {
		t.Fatalf("delivered validation: %v", err)
	}

	expiredAt := expiresAt
	expired := pending
	expired.Status = domain.APIKeyProvisioningStatusExpired
	expired.EncryptedRawKey = nil
	expired.EncryptionNonce = nil
	expired.UpdatedAt = expiredAt
	expired.ExpiredAt = &expiredAt
	if err := validateProvisioningRecord(expired); err != nil {
		t.Fatalf("expired validation: %v", err)
	}
}

func TestValidateProvisioningRecordRejectsTerminalCiphertext(
	t *testing.T,
) {
	now := time.Unix(100, 0).UTC()
	deliveredAt := now.Add(time.Minute)
	value := domain.APIKeyProvisioning{
		ID:                    "prov-1",
		IdempotencyKey:        "idem-1",
		SourceReferenceHash:   "source-hash",
		ExternalBillingUserID: "billing-user-1",
		UserID:                "user-1",
		APIKeyID:              "key-1",
		ResultType:            domain.APIKeyProvisioningResultTypeKeyCreated,
		Status:                domain.APIKeyProvisioningStatusDelivered,
		EncryptedRawKey:       []byte("must-not-remain"),
		EncryptionKeyVersion:  "v1",
		CreatedAt:             now,
		UpdatedAt:             deliveredAt,
		DeliveredAt:           &deliveredAt,
	}
	if err := validateProvisioningRecord(value); !errors.Is(
		err,
		ports.ErrStoreContractViolation,
	) {
		t.Fatalf("error = %v, want contract violation", err)
	}
}

func TestValidateProvisioningMaterialBindsActualUserAndIDs(
	t *testing.T,
) {
	now := time.Unix(100, 0).UTC()
	expiresAt := now.Add(time.Hour)
	user := domain.User{
		ID:                    "user-actual",
		ExternalBillingUserID: "billing-user",
		Enabled:               true,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	request := ports.APIKeyProvisioningMaterialRequest{
		User:           user,
		ProvisioningID: "prov-1",
		APIKeyID:       "key-1",
		KeyName:        "Telegram key",
		CreatedAt:      now,
		ExpiresAt:      expiresAt,
	}
	material := ports.APIKeyProvisioningMaterial{
		APIKey: domain.APIKeyRecord{
			ID:        "key-1",
			UserID:    "user-actual",
			Name:      "Telegram key",
			KeyHash:   "hash",
			KeyPrefix: "sk_live_abcd...",
			Enabled:   true,
			CreatedAt: now,
			UpdatedAt: now,
		},
		EncryptedRawKey:      []byte("ciphertext"),
		EncryptionNonce:      []byte("nonce"),
		EncryptionKeyVersion: "v1",
	}
	if err := validateProvisioningMaterial(
		request,
		material,
	); err != nil {
		t.Fatalf("validation error: %v", err)
	}

	material.APIKey.UserID = "wrong-user"
	if err := validateProvisioningMaterial(
		request,
		material,
	); !errors.Is(err, ports.ErrStoreContractViolation) {
		t.Fatalf("wrong user error = %v", err)
	}
}

type provisioningMaterialFactoryFunc func(
	context.Context,
	ports.APIKeyProvisioningMaterialRequest,
) (ports.APIKeyProvisioningMaterial, error)

func (f provisioningMaterialFactoryFunc) CreateProvisioningMaterial(
	ctx context.Context,
	request ports.APIKeyProvisioningMaterialRequest,
) (ports.APIKeyProvisioningMaterial, error) {
	return f(ctx, request)
}

func TestValidateProvisioningRecordRequiresKeyCreatedTerminalMetadata(
	t *testing.T,
) {
	now := time.Unix(100, 0).UTC()
	deliveredAt := now.Add(time.Minute)
	value := domain.APIKeyProvisioning{
		ID:                    "prov-1",
		IdempotencyKey:        "idem-1",
		SourceReferenceHash:   "source-hash",
		ExternalBillingUserID: "billing-user-1",
		UserID:                "user-1",
		APIKeyID:              "key-1",
		ResultType:            domain.APIKeyProvisioningResultTypeKeyCreated,
		Status:                domain.APIKeyProvisioningStatusDelivered,
		CreatedAt:             now,
		UpdatedAt:             deliveredAt,
		DeliveredAt:           &deliveredAt,
	}
	if err := validateProvisioningRecord(value); !errors.Is(
		err,
		ports.ErrStoreContractViolation,
	) {
		t.Fatalf("error = %v, want contract violation", err)
	}
}
