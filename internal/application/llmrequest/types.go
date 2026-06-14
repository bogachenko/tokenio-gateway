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
