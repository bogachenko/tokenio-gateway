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
)

type APIKeyRepository interface {
	FindByHash(ctx context.Context, keyHash string) (*domain.APIKeyRecord, error)
}

type APIKeyUsageRecorder interface {
	RecordLastUsedAt(
		context.Context,
		string,
		time.Time,
	) error
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
	ListModelCatalogRoutes(
		context.Context,
		domain.APIFamily,
	) ([]domain.Route, error)
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
	Check(
		context.Context,
		RouteCapacityCheckInput,
	) (RouteCapacityResult, error)
}

type RouteCapacityAcquireInput struct {
	LocalRequestID string
	Route          domain.Route
	Reseller       domain.Reseller
	EstimatedUsage domain.TokenUsage
}

type RouteCapacityReservation struct {
	LocalRequestID string
	RouteID        string
}

type RouteCapacityManager interface {
	RouteCapacityChecker

	// Acquire atomically re-checks route limits and records one selected
	// request against RPM, TPM, and concurrency. Repeated acquisition for the
	// same LocalRequestID must be idempotent.
	Acquire(
		context.Context,
		RouteCapacityAcquireInput,
	) (RouteCapacityReservation, error)

	// Release removes only the in-flight concurrency slot. RPM and TPM usage
	// remain accounted for until their limiter window expires. Repeated release
	// of the same reservation must be idempotent.
	Release(
		context.Context,
		RouteCapacityReservation,
	) error
}

type SecretResolver interface {
	Resolve(ctx context.Context, name string) (string, error)
}

type ForwardingAdapterEndpointSupport interface {
	SupportsForwardingEndpoint(
		domain.EndpointKind,
	) bool
}

type ForwardingAdapterSupport interface {
	SupportsForwardingAdapter(
		domain.APIFamily,
		domain.ProviderType,
		domain.EndpointKind,
	) bool
}

type ModelIdentifierRewriteSupport interface {
	SupportsModelIdentifierRewrite(
		domain.APIFamily,
		domain.ProviderType,
	) bool
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

type ForwardingClientRequest struct {
	Route domain.Route
	Body  []byte
}

type ForwardingClient interface {
	Forward(
		context.Context,
		ForwardingClientRequest,
	) (ForwardResponse, error)
}

type ForwardingAdapterFactoryInput struct {
	Route    domain.Route
	Reseller domain.Reseller

	ResellerAPIKey       string
	MaxResponseBodyBytes int64
}

type ForwardingAdapterFactory interface {
	Build(
		ForwardingAdapterFactoryInput,
	) (ForwardingClient, error)
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
	// StartAttempt durably inserts exactly one started attempt before any
	// upstream network write. The pair local_request_id + attempt_number is
	// unique. Repeating an identical command is idempotent; conflicting facts
	// must return ErrStoreConflict or ErrStoreContractViolation.
	StartAttempt(
		context.Context,
		domain.ForwardingAttempt,
	) (domain.ForwardingAttempt, error)

	// CompleteAttempt atomically transitions the exact persisted started
	// attempt to succeeded or failed. Terminal attempts are immutable.
	// Repeating the identical completion is idempotent.
	CompleteAttempt(
		context.Context,
		domain.ForwardingAttempt,
	) (domain.ForwardingAttempt, error)

	// LoadAttempts returns every persisted attempt for one request ordered by
	// attempt_number ASC. Missing requests return an empty slice.
	LoadAttempts(
		context.Context,
		string,
	) ([]domain.ForwardingAttempt, error)
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
	// LoadOpenChargeBatches returns durable pending/failed batches that must be
	// retried before creating new charge batches. Succeeded batches are not open.
	// Repository order is unspecified; application code must process snapshots by
	// batch.created_at ASC, then batch.id ASC.
	LoadOpenChargeBatches(ctx context.Context, userID string, billingSubjectUserID string, currency string) ([]BillingChargeBatchSnapshot, error)
	// LoadChargeCandidates returns chargeable billable or partially_charged records
	// with positive remaining amount. Billable records must be unclaimed. Partially
	// charged records must reference a succeeded historical charge batch and must
	// not be owned by any pending/failed active charge batch; PrepareChargeBatch
	// must atomically re-check this before replacing the claim for remaining amount.
	LoadChargeCandidates(ctx context.Context, userID string, currency string) ([]domain.UsageRecord, error)
	// PrepareChargeBatch atomically verifies ExpectedRecords, creates the pending
	// batch and allocations, and claims all records with BillingChargeRequestID.
	PrepareChargeBatch(ctx context.Context, plan UsageChargeBatchPlan) (BillingChargeBatchSnapshot, error)
	// MarkChargeBatchFailed atomically transitions pending -> failed only when
	// the current batch status matches expectedStatus. Succeeded is terminal;
	// an identical already-terminal succeeded batch must not be overwritten.
	MarkChargeBatchFailed(ctx context.Context, batchID string, expectedStatus domain.BillingChargeStatus, billingErrorCode string, failedAt time.Time) error
	// ApplyChargeSuccess atomically loads the persisted immutable batch command,
	// verifies caller metadata against persisted allocations/expected records,
	// transitions pending|failed -> succeeded, applies allocation deltas to usage
	// records, sets charged/partially_charged record status, clears any failed
	// metadata, and treats identical already-succeeded batches as idempotent success.
	ApplyChargeSuccess(ctx context.Context, success UsageChargeSuccess) error
}
