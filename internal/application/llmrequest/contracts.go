package llmrequest

import (
	"context"

	"github.com/bogachenko/tokenio-gateway/internal/ports/llmrequestmetadata"
	"github.com/bogachenko/tokenio-gateway/internal/ports/llmrequestreservation"
)

type Authenticator interface {
	Authenticate(context.Context, string) (Principal, error)
}

type RequestParser = llmrequestmetadata.RequestParser

type CapabilityDetector = llmrequestmetadata.CapabilityDetector

type RoutePlanner interface {
	Plan(context.Context, RoutePlanInput) (RoutePlan, error)
}

type BillingAdmitter interface {
	Admit(context.Context, BillingAdmissionInput) (BillingAdmissionResult, error)
}

type AtomicReservation = llmrequestreservation.AtomicReservation

type RouteReservationTransfer = llmrequestreservation.RouteReservationTransfer

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

type Finalizer interface {
	Commit(
		context.Context,
		FinalizationInput,
	) (FinalizationResult, error)
	MarkPricingFailed(
		context.Context,
		PricingFailureInput,
	) (FinalizationResult, error)
}

type AutoCharger interface {
	Run(context.Context, AutoChargeInput) AutoChargeResult
}

type Dependencies struct {
	Authenticator      Authenticator
	RequestParser      RequestParser
	CapabilityDetector CapabilityDetector
	RoutePlanner       RoutePlanner
	BillingAdmitter    BillingAdmitter
	Forwarding         ForwardingStageExecutor
	UsageResolver      UsageResolver
	Finalizer          Finalizer
	AutoCharger        AutoCharger
}
