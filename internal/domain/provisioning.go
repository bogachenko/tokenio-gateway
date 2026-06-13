package domain

import "time"

type APIKeyProvisioningResultType string

const (
	APIKeyProvisioningResultTypeKeyCreated         APIKeyProvisioningResultType = "key_created"
	APIKeyProvisioningResultTypeAlreadyProvisioned APIKeyProvisioningResultType = "already_provisioned"
)

type APIKeyProvisioningStatus string

const (
	APIKeyProvisioningStatusPendingDelivery APIKeyProvisioningStatus = "pending_delivery"
	APIKeyProvisioningStatusDelivered       APIKeyProvisioningStatus = "delivered"
	APIKeyProvisioningStatusExpired         APIKeyProvisioningStatus = "expired"
)

type APIKeyProvisioning struct {
	ID string

	IdempotencyKey      string
	SourceReferenceHash string

	ExternalBillingUserID string
	UserID                string
	APIKeyID              string

	ResultType APIKeyProvisioningResultType
	Status     APIKeyProvisioningStatus

	EncryptedRawKey      []byte
	EncryptionNonce      []byte
	EncryptionKeyVersion string

	DeliveryAttempts int

	CreatedAt   time.Time
	UpdatedAt   time.Time
	ExpiresAt   *time.Time
	DeliveredAt *time.Time
	ExpiredAt   *time.Time
}
