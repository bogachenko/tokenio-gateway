package ports

import (
	"context"
	"errors"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

var (
	ErrProvisioningUserDisabled = errors.New(
		"provisioning user disabled",
	)
	ErrProvisioningExpired = errors.New(
		"provisioning expired",
	)
)

type APIKeyProvisioningRequest struct {
	IdempotencyKey      string
	SourceReferenceHash string

	ExternalBillingUserID string

	NewUser domain.User

	ProvisioningID string
	APIKeyID       string
	KeyName        string

	CreatedAt time.Time
	ExpiresAt time.Time
}

type APIKeyProvisioningMaterialRequest struct {
	User domain.User

	ProvisioningID string
	APIKeyID       string
	KeyName        string

	CreatedAt time.Time
	ExpiresAt time.Time
}

type APIKeyProvisioningMaterial struct {
	APIKey domain.APIKeyRecord

	EncryptedRawKey      []byte
	EncryptionNonce      []byte
	EncryptionKeyVersion string
}

// APIKeyProvisioningMaterialFactory must be a local deterministic/crypto
// capability. It must not perform network I/O while the store transaction is
// open. The store persists only APIKey.KeyHash/KeyPrefix and encrypted delivery
// material; plaintext raw API keys are outside this contract.
type APIKeyProvisioningMaterialFactory interface {
	CreateProvisioningMaterial(
		context.Context,
		APIKeyProvisioningMaterialRequest,
	) (APIKeyProvisioningMaterial, error)
}

type APIKeyProvisioningMaterialDecryptor interface {
	DecryptProvisioningMaterial(
		context.Context,
		domain.APIKeyProvisioning,
		domain.APIKeyRecord,
	) (string, error)
}

type APIKeyProvisioningOutcome string

const (
	APIKeyProvisioningOutcomeCreated            APIKeyProvisioningOutcome = "created"
	APIKeyProvisioningOutcomeReplayedPending    APIKeyProvisioningOutcome = "replayed_pending"
	APIKeyProvisioningOutcomeReplayedDelivered  APIKeyProvisioningOutcome = "replayed_delivered"
	APIKeyProvisioningOutcomeAlreadyProvisioned APIKeyProvisioningOutcome = "already_provisioned"
	APIKeyProvisioningOutcomeExpired            APIKeyProvisioningOutcome = "expired"
)

type APIKeyProvisioningResult struct {
	Outcome APIKeyProvisioningOutcome

	User         domain.User
	APIKey       domain.APIKeyRecord
	Provisioning domain.APIKeyProvisioning
}

type APIKeyProvisioningStore interface {
	ProvisionAPIKey(
		context.Context,
		APIKeyProvisioningRequest,
		APIKeyProvisioningMaterialFactory,
	) (APIKeyProvisioningResult, error)

	FindAPIKeyProvisioningByID(
		context.Context,
		string,
	) (*domain.APIKeyProvisioning, error)

	FindAPIKeyProvisioningByIdempotencyKey(
		context.Context,
		string,
	) (*domain.APIKeyProvisioning, error)

	RecordAPIKeyDeliveryAttempt(
		context.Context,
		string,
		time.Time,
	) (domain.APIKeyProvisioning, error)

	ConfirmAPIKeyDelivery(
		context.Context,
		string,
		time.Time,
	) (domain.APIKeyProvisioning, error)

	ListPendingAPIKeyProvisioningsDue(
		context.Context,
		time.Time,
		int,
	) ([]domain.APIKeyProvisioning, error)

	ExpireAPIKeyProvisioning(
		context.Context,
		string,
		time.Time,
	) (domain.APIKeyProvisioning, error)
}
