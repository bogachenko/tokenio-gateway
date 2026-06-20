package ports

import (
	"context"
	"errors"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

var (
	ErrNotFound               = errors.New("not found")
	ErrStoreUnavailable       = errors.New("store unavailable")
	ErrStoreConflict          = errors.New("store conflict")
	ErrStoreContractViolation = errors.New("store contract violation")

	ErrRouteCapacityUnavailable = errors.New(
		"route capacity unavailable",
	)
	ErrRouteCapacityReservationConflict = errors.New(
		"route capacity reservation conflict",
	)
)

type APIKeyRepository interface {
	FindByHash(ctx context.Context, keyHash string) (*domain.APIKeyRecord, error)
}

type APIKeyUsageRecorder interface {
	RecordLastUsedAt(context.Context, string, time.Time) error
}

type UserRepository interface {
	FindByID(ctx context.Context, userID string) (*domain.User, error)
}

type ResellerQueryRepository interface {
	FindByIDs(ctx context.Context, resellerIDs []string) (map[string]domain.Reseller, error)
}

type RouteQuery struct {
	APIFamily    domain.APIFamily
	EndpointKind domain.EndpointKind
	ClientModel  string
}

type RouteRepository interface {
	FindRoutes(ctx context.Context, query RouteQuery) ([]domain.Route, error)
}

type ModelCatalogRouteRepository interface {
	ListModelCatalogRoutes(context.Context, domain.APIFamily) ([]domain.Route, error)
}

type RoutePriceRepository interface {
	FindByRouteIDs(ctx context.Context, routeIDs []string) (map[string]domain.RoutePrice, error)
}

type RouteCapacityCheckInput struct {
	Route          domain.Route
	Reseller       domain.Reseller
	EstimatedUsage domain.TokenUsage
}

type RouteCapacityResult struct {
	RateLimitAllowed   bool
	ConcurrencyAllowed bool
}

type RouteCapacityChecker interface {
	Check(context.Context, RouteCapacityCheckInput) (RouteCapacityResult, error)
}

type RouteCapacityAcquireInput struct {
	LocalRequestID string
	ReservationID  string
	Route          domain.Route
	Reseller       domain.Reseller
	EstimatedUsage domain.TokenUsage
}

type RouteCapacityReservation struct {
	LocalRequestID string
	ReservationID  string
	RouteID        string
}

type RouteCapacityManager interface {
	RouteCapacityChecker
	Acquire(context.Context, RouteCapacityAcquireInput) (RouteCapacityReservation, error)
	Release(context.Context, RouteCapacityReservation) error
}

type SecretResolver interface {
	Resolve(ctx context.Context, name string) (string, error)
}

type ForwardingAdapterEndpointSupport interface {
	SupportsForwardingEndpoint(domain.EndpointKind) bool
}

type ForwardingAdapterSupport interface {
	SupportsForwardingAdapter(domain.APIFamily, domain.ProviderType, domain.EndpointKind) bool
}

type ModelIdentifierRewriteSupport interface {
	SupportsModelIdentifierRewrite(domain.APIFamily, domain.ProviderType) bool
}

type Clock interface {
	Now() time.Time
}

type RequestIDGenerator interface {
	NewLocalRequestID() (string, error)
	NewAdminRequestID() (string, error)
	NewProvisioningRequestID() (string, error)
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

type UsageDimensionPresence struct {
	InputTokens          bool
	CachedInputTokens    bool
	OutputTokens         bool
	ReasoningTokens      bool
	ImageInputTokens     bool
	AudioInputTokens     bool
	AudioOutputTokens    bool
	FileInputTokens      bool
	VideoInputTokens     bool
	ImageGenerationUnits bool
}

type UsageExtractionResult struct {
	Usage                 domain.TokenUsage
	Presence              UsageDimensionPresence
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

type ForwardUsage struct {
	InputTokens  int64
	OutputTokens int64
}

type ForwardResponse struct {
	StatusCode int
	Headers    map[string][]string
	Body       []byte
	Usage      *ForwardUsage
}

type ForwardingAdapter interface {
	Forward(ctx context.Context, request ForwardRequest) (ForwardResponse, error)
}

type ForwardingClientRequest struct {
	Route domain.Route
	Path  string
	Body  []byte
}

type ForwardingClient interface {
	Forward(context.Context, ForwardingClientRequest) (ForwardResponse, error)
}

type ForwardingAdapterFactoryInput struct {
	Route    domain.Route
	Reseller domain.Reseller

	ResellerAPIKey       string
	MaxResponseBodyBytes int64
}

type ForwardingAdapterFactory interface {
	Build(ForwardingAdapterFactoryInput) (ForwardingClient, error)
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

type ForwardingAttemptStore interface {
	StartAttempt(context.Context, domain.ForwardingAttempt) (domain.ForwardingAttempt, error)
	CompleteAttempt(context.Context, domain.ForwardingAttempt) (domain.ForwardingAttempt, error)
	LoadAttempts(context.Context, string) ([]domain.ForwardingAttempt, error)
	LoadStartedBefore(context.Context, time.Time, int) ([]domain.ForwardingAttempt, error)
}

type UsageExposureSnapshot struct {
	Currency string

	ReservedEstimatedAmountCents         int64
	BillableRemainingAmountCents         int64
	PartiallyChargedRemainingAmountCents int64
	PricingFailedCount                   int64
}

type BillingChargeBatchSnapshot struct {
	Batch           domain.BillingChargeBatch
	Allocations     []domain.BillingChargeAllocation
	ExpectedRecords []domain.UsageRecord
}

type BillingChargeSubject struct {
	UserID               string
	BillingSubjectUserID string
	Currency             string
	OldestChargeableAt   time.Time
}

type UsageChargeBatchPlan struct {
	Batch           domain.BillingChargeBatch
	Allocations     []domain.BillingChargeAllocation
	ExpectedRecords []domain.UsageRecord
}

type UsageChargeSuccess struct {
	BatchID             string
	BillingBalanceCents *int64
	ChargedAt           time.Time
	Allocations         []domain.BillingChargeAllocation
	ExpectedRecords     []domain.UsageRecord
}

type BillingRecoveryStore interface {
	ListOpenChargeBatchesForRecovery(ctx context.Context, limit int) ([]BillingChargeBatchSnapshot, error)
	ListChargeableBillingSubjects(ctx context.Context, limit int) ([]BillingChargeSubject, error)
}

type UsageLedger interface {
	CreateReserved(ctx context.Context, record domain.UsageRecord) (UsageReserveResult, error)
	FindByLocalRequestID(ctx context.Context, localRequestID string) (*domain.UsageRecord, error)
	CompareAndSwap(ctx context.Context, localRequestID string, expectedStatus domain.UsageStatus, next domain.UsageRecord) (UsageTransitionResult, error)
	LoadExposure(ctx context.Context, userID string, currency string) (UsageExposureSnapshot, error)
	LoadOpenChargeBatches(ctx context.Context, userID string, billingSubjectUserID string, currency string) ([]BillingChargeBatchSnapshot, error)
	LoadChargeCandidates(ctx context.Context, userID string, currency string) ([]domain.UsageRecord, error)
	PrepareChargeBatch(ctx context.Context, plan UsageChargeBatchPlan) (BillingChargeBatchSnapshot, error)
	MarkChargeBatchFailed(ctx context.Context, batchID string, expectedStatus domain.BillingChargeStatus, billingErrorCode string, failedAt time.Time) error
	ApplyChargeSuccess(ctx context.Context, success UsageChargeSuccess) error
}
