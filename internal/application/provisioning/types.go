package provisioning

import (
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type ResultType string

const (
	ResultTypeCreated            ResultType = "created"
	ResultTypeReplayed           ResultType = "replayed"
	ResultTypeAlreadyProvisioned ResultType = "already_provisioned"
)

type ProvisionInput struct {
	IdempotencyKey        string
	ExternalBillingUserID string
	SourceReference       string
	KeyName               string
}

type ProvisionResult struct {
	Result ResultType

	ProvisioningID     string
	ProvisioningStatus domain.APIKeyProvisioningStatus
	APIKeyID           string

	APIKey    string
	KeyPrefix string
	ExpiresAt *time.Time
}

type ConfirmDeliveryResult struct {
	ProvisioningID string
	Status         domain.APIKeyProvisioningStatus
	DeliveredAt    *time.Time
}
