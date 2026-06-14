package llmrequest

import (
	"context"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type Authenticator interface {
	Authenticate(context.Context, string) (Principal, error)
}

type RequestParser interface {
	Parse(context.Context, ParseInput) (ParsedRequest, error)
}

type CapabilityDetector interface {
	Detect(context.Context, CapabilityInput) (domain.CapabilitySet, error)
}

type RoutePlanner interface {
	Plan(context.Context, RoutePlanInput) (RoutePlan, error)
}

type BillingAdmitter interface {
	Admit(context.Context, BillingAdmissionInput) (BillingAdmissionResult, error)
}

type AtomicReservation interface {
	// Reserve must create the usage reserve and increment the selected reseller
	// reserve in one atomic operation. An error must leave both states unchanged.
	Reserve(context.Context, ReservationInput) (ReservationResult, error)
}

type ForwardingStageExecutor interface {
	Execute(
		context.Context,
		PreparedRequest,
		BillingAdmissionResult,
	) (ForwardedRequest, error)
}

type UsageResolver interface {
	Resolve(
		context.Context,
		UsageResolutionInput,
	) (UsageResolutionResult, error)
}

type Dependencies struct {
	Authenticator      Authenticator
	RequestParser      RequestParser
	CapabilityDetector CapabilityDetector
	RoutePlanner       RoutePlanner
	BillingAdmitter    BillingAdmitter
	Forwarding         ForwardingStageExecutor
	UsageResolver      UsageResolver
}
