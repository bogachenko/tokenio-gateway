package admin

import (
	"context"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type CommandContext struct {
	RequestID    string
	AdminSubject string
}

type Pagination struct {
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
	Total  int64 `json:"total"`
}

type ListResult[T any] struct {
	Data       []T
	Pagination Pagination
}

type UserListInput struct {
	Enabled *bool
	Email   string
	Limit   int
	Offset  int
}

type CreateUserInput struct {
	ID                    string
	ExternalBillingUserID string
	Email                 string
	Name                  string
}

type APIKeyView struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	Enabled    bool       `json:"enabled"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
}

type CreatedAPIKey struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	APIKey    string    `json:"api_key"`
	KeyPrefix string    `json:"key_prefix"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateAPIKeyInput struct {
	UserID    string
	Name      string
	ExpiresAt *time.Time
}

type ResellerListInput struct {
	ProviderType domain.ProviderType
	Enabled      *bool
	Limit        int
	Offset       int
}

type ResellerView struct {
	ID                  string              `json:"id"`
	Name                string              `json:"name"`
	ProviderType        domain.ProviderType `json:"provider_type"`
	BaseURL             string              `json:"base_url"`
	APIKeyEnv           string              `json:"api_key_env"`
	APIKeyEnvPresent    bool                `json:"api_key_env_present"`
	Enabled             bool                `json:"enabled"`
	BalanceCents        int64               `json:"balance_cents"`
	ReservedCents       int64               `json:"reserved_cents"`
	MinimumBalanceCents int64               `json:"minimum_balance_cents"`
	CreatedAt           time.Time           `json:"created_at"`
	UpdatedAt           time.Time           `json:"updated_at"`
}

type CreateResellerInput struct{ Reseller domain.Reseller }

type UpdateResellerInput struct {
	ID                  string
	Name                *string
	BaseURL             *string
	APIKeyEnv           *string
	Enabled             *bool
	MinimumBalanceCents *int64
}

type ResellerBalance struct {
	ResellerID            string `json:"reseller_id"`
	BalanceCents          int64  `json:"balance_cents"`
	ReservedCents         int64  `json:"reserved_cents"`
	MinimumBalanceCents   int64  `json:"minimum_balance_cents"`
	AvailableBalanceCents int64  `json:"available_balance_cents"`
	Currency              string `json:"currency"`
}

type RouteListInput struct {
	ResellerID   string
	ProviderType domain.ProviderType
	APIFamily    domain.APIFamily
	EndpointKind domain.EndpointKind
	ClientModel  string
	Enabled      *bool
	Limit        int
	Offset       int
}

type UpdateRouteInput struct {
	ID                     string
	ProviderModel          *string
	ModelRewritePolicy     *domain.ModelRewritePolicy
	Enabled                *bool
	Priority               *int
	RequestsPerMinute      *int
	TokensPerMinute        *int
	ConcurrentRequests     *int
	DefaultMaxOutputTokens *int64
	Capabilities           *domain.CapabilitySet
}

type SetCooldownInput struct {
	RouteID        string
	CooldownUntil  time.Time
	CooldownReason string
}

type UsageListInput struct {
	UserID             string
	Status             domain.UsageStatus
	ProviderType       domain.ProviderType
	ClientModel        string
	SelectedRouteID    string
	SelectedResellerID string
	CreatedFrom        *time.Time
	CreatedTo          *time.Time
	Limit              int
	Offset             int
}

type ResolveBillableInput struct {
	LocalRequestID          string
	InputTokens             int64
	OutputTokens            int64
	ClientAmountCents       int64
	ActualUpstreamCostCents int64
	Reason                  string
}

type ResolveFailedInput struct{ LocalRequestID, Reason string }

type ResolveChargedInput struct {
	LocalRequestID         string
	ChargedAmountCents     int64
	BillingChargeRequestID string
	Reason                 string
}

type BillingBatchListInput struct {
	UserID       string
	ProviderType domain.ProviderType
	ClientModel  string
	Status       domain.BillingChargeStatus
	CreatedFrom  *time.Time
	CreatedTo    *time.Time
	Limit        int
	Offset       int
}

type AuditListInput struct {
	AdminSubject string
	Action       domain.AuditAction
	EntityType   string
	EntityID     string
	CreatedFrom  *time.Time
	CreatedTo    *time.Time
	Limit        int
	Offset       int
}

type FailedChargeBatchRetrier interface {
	RetryFailedBatch(context.Context, string, domain.AuditContext) (domain.BillingChargeBatch, error)
}

type RoutePriceValidator interface {
	ValidateRoutePrice(domain.RoutePrice) error
}

type UsagePolicy interface {
	ValidateRecord(domain.UsageRecord) error
	ValidateTransition(domain.UsageStatus, domain.UsageStatus) error
}

type Dependencies struct {
	Users          ports.AdminUserRepository
	APIKeys        ports.AdminAPIKeyRepository
	Provisionings  ports.AdminAPIKeyProvisioningRepository
	Resellers      ports.ResellerRepository
	Routes         ports.AdminRouteRepository
	Prices         ports.AdminRoutePriceRepository
	PriceValidator RoutePriceValidator
	UsagePolicy    UsagePolicy
	Ledger         ports.AdminUsageLedger
	Audit          ports.AdminAuditStore
	Secrets        ports.SecretPresenceChecker
	AdapterSupport ports.ForwardingAdapterSupport
	KeyGenerator   ports.APIKeyGenerator
	Hasher         APIKeyHasher
	Clock          ports.Clock
	BatchRetrier   FailedChargeBatchRetrier
}

type APIKeyHasher interface{ Hash(string) string }
