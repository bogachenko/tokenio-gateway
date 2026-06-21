package llmrequest

import (
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/bogachenko/tokenio-gateway/internal/ports/llmrequestmetadata"
	"github.com/bogachenko/tokenio-gateway/internal/ports/llmrequestreservation"
)

type Input struct {
	LocalRequestID string
	RawAPIKey      string
	IdempotencyKey *string

	APIFamily    domain.APIFamily
	EndpointKind domain.EndpointKind
	PathModel    string
	UpstreamPath string

	Payload []byte
}

type Principal = llmrequestreservation.Principal

type ParseInput = llmrequestmetadata.ParseInput

type ParsedRequest = llmrequestmetadata.ParsedRequest

type CapabilityInput = llmrequestmetadata.CapabilityInput

type RoutePlanInput struct {
	LocalRequestID string
	Principal      Principal

	APIFamily             domain.APIFamily
	EndpointKind          domain.EndpointKind
	ClientModel           string
	RequestedCapabilities domain.CapabilitySet
	Payload               []byte
}

type RouteFallbackPlan = llmrequestreservation.RouteFallbackPlan

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
	UpstreamPath          string

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

type ReservationInput = llmrequestreservation.ReservationInput

type ReservationResult = llmrequestreservation.ReservationResult

type ReservationDisposition = llmrequestreservation.ReservationDisposition

const (
	ReservationDispositionCreated         = llmrequestreservation.ReservationDispositionCreated
	ReservationDispositionAlreadyReserved = llmrequestreservation.ReservationDispositionAlreadyReserved
)

type RouteReservationTransferInput = llmrequestreservation.RouteReservationTransferInput

type RouteReservationTransferResult = llmrequestreservation.RouteReservationTransferResult

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
