package llmrequest

import "github.com/bogachenko/tokenio-gateway/internal/domain"

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
