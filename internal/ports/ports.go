package ports

import (
	"context"
	"errors"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

var ErrNotFound = errors.New("not found")

type APIKeyRepository interface {
	FindByHash(ctx context.Context, keyHash string) (*domain.APIKeyRecord, error)
}

type UserRepository interface {
	FindByID(ctx context.Context, userID string) (*domain.User, error)
}

type RouteQuery struct {
	APIFamily    domain.APIFamily
	EndpointKind domain.EndpointKind
	ClientModel  string
}

type RouteRepository interface {
	FindRoutes(ctx context.Context, query RouteQuery) ([]domain.Route, error)
}

type RoutePriceRepository interface {
	FindByRouteIDs(ctx context.Context, routeIDs []string) (map[string]domain.RoutePrice, error)
}

type SecretResolver interface {
	Resolve(ctx context.Context, name string) (string, error)
}

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	NewLocalRequestID() string
	NewAdminRequestID() string
	NewBillingChargeRequestID() string
}

type BillingIdentityService interface {
	TokenForSubject(ctx context.Context, billingSubjectUserID string) (string, error)
}

type BillingBalance struct {
	Currency     string
	BalanceCents int64
}

type BillingBalanceClient interface {
	GetBalance(ctx context.Context, billingToken string) (BillingBalance, error)
}

type BillingChargeRequest struct {
	RequestID string
	UserID    string
	Model     string

	InputTokens  int64
	OutputTokens int64

	AmountCents int64
	Currency    string
}

type BillingChargeResult struct {
	BalanceCents *int64
}

type BillingChargeClient interface {
	Charge(ctx context.Context, request BillingChargeRequest) (BillingChargeResult, error)
}

type TokenEstimateRequest struct {
	APIFamily              domain.APIFamily
	EndpointKind           domain.EndpointKind
	ClientModel            string
	RequestBody            []byte
	DefaultMaxOutputTokens int64
	RequestedCapabilities  domain.CapabilitySet
}

type TokenEstimate struct {
	Usage      domain.TokenUsage
	Confidence string
}

type TokenEstimator interface {
	Estimate(ctx context.Context, request TokenEstimateRequest) (TokenEstimate, error)
}

type UsageExtractionRequest struct {
	APIFamily    domain.APIFamily
	EndpointKind domain.EndpointKind
	ClientModel  string

	RequestBody  []byte
	ResponseBody []byte
}

type UsageExtractionResult struct {
	Usage                 domain.TokenUsage
	Completeness          string
	ProviderRequestID     string
	ProviderResponseModel string
}

type UsageExtractor interface {
	Extract(ctx context.Context, request UsageExtractionRequest) (UsageExtractionResult, error)
}

type ForwardRequest struct {
	Route domain.Route

	Method  string
	Path    string
	Headers map[string][]string
	Body    []byte
}

type ForwardResponse struct {
	StatusCode int
	Headers    map[string][]string
	Body       []byte
}

type ForwardingAdapter interface {
	Forward(ctx context.Context, request ForwardRequest) (ForwardResponse, error)
}

type UsageReserveOutcome string

const (
	UsageReserveOutcomeCreated            UsageReserveOutcome = "created"
	UsageReserveOutcomeLocalRequestExists UsageReserveOutcome = "local_request_exists"
	UsageReserveOutcomeIdempotencyExists  UsageReserveOutcome = "idempotency_exists"
	UsageReserveOutcomeUnresolvedUsage    UsageReserveOutcome = "unresolved_usage"
)

type UsageReserveResult struct {
	Outcome  UsageReserveOutcome
	Existing *domain.UsageRecord
}

type UsageTransitionResult struct {
	Applied bool
	Current *domain.UsageRecord
}

type UsageExposureSnapshot struct {
	Currency string

	ReservedEstimatedAmountCents         int64
	BillableRemainingAmountCents         int64
	PartiallyChargedRemainingAmountCents int64
	PricingFailedCount                   int64
}

type UsageLedger interface {
	// CreateReserved atomically checks unresolved user pricing failures, local_request_id
	// uniqueness, optional client idempotency scope (user_id + endpoint_kind +
	// idempotency_key), and inserts the reserved usage record in one persistence
	// transaction. Callers must not emulate this with a Find-then-Insert sequence.
	CreateReserved(ctx context.Context, record domain.UsageRecord) (UsageReserveResult, error)
	FindByLocalRequestID(ctx context.Context, localRequestID string) (*domain.UsageRecord, error)
	// CompareAndSwap persists next only when the current record identified by
	// localRequestID is still in expectedStatus; when Applied is false Current
	// contains the actual current record.
	CompareAndSwap(ctx context.Context, localRequestID string, expectedStatus domain.UsageStatus, next domain.UsageRecord) (UsageTransitionResult, error)
	LoadExposure(ctx context.Context, userID string, currency string) (UsageExposureSnapshot, error)
}
