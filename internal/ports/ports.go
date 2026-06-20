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

// RouteCapacityManager implementations must return
// ErrRouteCapacityUnavailable when a valid route cannot accept this
// attempt because of RPM, TPM, or concurrency limits. Contract and
// identity violations must not be reported as capacity exhaustion.
type RouteCapacityManager interface {
	RouteCapacityChecker

	// Acquire atomically re-checks route limits and records one route attempt
	// against RPM, TPM, and concurrency. Repeated acquisition for the same
	// ReservationID must be idempotent. LocalRequestID is correlation only and
	// may be shared by multiple sequential attempt reservations.
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

	// LoadStartedBefore returns at most limit durable attempts that are
	// still started and have started_at strictly before cutoff. Results
	// are ordered by started_at ASC, local_request_id ASC, then
	// attempt_number ASC.
	LoadStartedBefore(
		context.Context,
		time.Time,
		int,
	) ([]domain.ForwardingAttempt, error)
}
