package llmrequest

import (
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type Input struct {
	LocalRequestID string
	RawAPIKey      string
	IdempotencyKey *string

	APIFamily    domain.APIFamily
	EndpointKind domain.EndpointKind

	Payload []byte
}

type Principal struct {
	UserID               string
	APIKeyID             string
	BillingSubjectUserID string
}

type ParseInput struct {
	APIFamily    domain.APIFamily
	EndpointKind domain.EndpointKind
	Payload      []byte
}

type ParsedRequest struct {
	ClientModel string
}

type CapabilityInput struct {
	APIFamily    domain.APIFamily
	EndpointKind domain.EndpointKind
	ClientModel  string
	Payload      []byte
}

type RoutePlanInput struct {
	Principal Principal

	APIFamily             domain.APIFamily
	EndpointKind          domain.EndpointKind
	ClientModel           string
	RequestedCapabilities domain.CapabilitySet
	Payload               []byte
}

type RouteFallbackPlan struct {
	Route    domain.Route
	Reseller domain.Reseller
	Price    domain.RoutePrice

	BillingModel   string
	EstimatedUsage domain.TokenUsage

	EstimatedClientAmountCents int64
	EstimatedUpstreamCostCents int64

	Currency   string
	Confidence string
}

type RoutePlan struct {
	Route    domain.Route
	Reseller domain.Reseller
	Price    domain.RoutePrice

	BillingModel   string
	EstimatedUsage domain.TokenUsage

	EstimatedClientAmountCents int64
	EstimatedUpstreamCostCents int64

	Currency   string
	Confidence string

	Fallbacks []RouteFallbackPlan
}

type PreparedRequest struct {
	LocalRequestID string
	IdempotencyKey *string

	Principal Principal

	APIFamily             domain.APIFamily
	EndpointKind          domain.EndpointKind
	ClientModel           string
	RequestedCapabilities domain.CapabilitySet

	Payload []byte
	Plan    RoutePlan
}

type BillingAdmissionInput struct {
	Principal Principal

	RequiredReserveCents int64
	Currency             string
}

type BillingAdmissionResult struct {
	Allowed bool

	RemoteBalanceCents    int64
	PendingAmountCents    int64
	EffectiveBalanceCents int64
	RequiredReserveCents  int64
	Currency              string
}

type ReservationInput struct {
	LocalRequestID string
	IdempotencyKey *string

	Principal Principal

	APIFamily    domain.APIFamily
	EndpointKind domain.EndpointKind
	ClientModel  string
	BillingModel string

	Route    domain.Route
	Reseller domain.Reseller

	EstimatedUsage domain.TokenUsage

	EstimatedClientAmountCents int64
	EstimatedUpstreamCostCents int64
	Currency                   string
}

type RouteReservationTransferInput struct {
	// ExpectedUsage is the exact currently persisted reserved usage snapshot.
	// The transfer must fail rather than overwrite a different committed state.
	ExpectedUsage domain.UsageRecord

	// Target is the immutable fallback snapshot selected during the original
	// routing decision. The transfer must not re-query or re-price the route.
	Target RouteFallbackPlan
}

type RouteReservationTransferResult struct {
	// Usage is the exact committed reserved usage snapshot after the transfer.
	Usage domain.UsageRecord

	// ReleasedReseller is the committed previous reseller balance snapshot after
	// its unused estimated upstream reserve has been removed.
	ReleasedReseller domain.Reseller

	// ReservedReseller is the committed target reseller balance snapshot after
	// the target estimated upstream reserve has been added.
	ReservedReseller domain.Reseller
}

type ReservationDisposition string

const (
	ReservationDispositionCreated         ReservationDisposition = "created"
	ReservationDispositionAlreadyReserved ReservationDisposition = "already_reserved"
)

type ReservationResult struct {
	Disposition ReservationDisposition
	Usage       domain.UsageRecord
	Reseller    *domain.Reseller
}

type ReservedRequest struct {
	Prepared    PreparedRequest
	Admission   BillingAdmissionResult
	Reservation ReservationResult
}

type UsageResolutionInput struct {
	Reserved ReservedRequest
	Response ports.ForwardResponse
}

type UsageResolutionResult struct {
	Usage        domain.TokenUsage
	Completeness string
	Estimated    bool

	UpstreamCostCents int64
	ClientAmountCents int64
	Currency          string

	ProviderRequestID     string
	ProviderResponseModel string
}

type FinalizationInput struct {
	Reserved      ReservedRequest
	ResolvedUsage UsageResolutionResult
}

type PricingFailureInput struct {
	Reserved      ReservedRequest
	FailureReason string
}

type FinalizationResult struct {
	Usage domain.UsageRecord
}

type AutoChargeStatus string

const (
	AutoChargeStatusDeferred  AutoChargeStatus = "deferred"
	AutoChargeStatusProcessed AutoChargeStatus = "processed"
	AutoChargeStatusFailed    AutoChargeStatus = "failed"
)

type AutoChargeInput struct {
	Principal        Principal
	FinalUsageRecord domain.UsageRecord
}

type AutoChargeResult struct {
	Status AutoChargeStatus

	ProcessedBatchIDs  []string
	ChargedAmountCents int64

	BillingBalanceCents *int64
}
