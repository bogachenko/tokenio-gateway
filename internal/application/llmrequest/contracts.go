package llmrequest

import (
	"context"

	"github.com/bogachenko/tokenio-gateway/internal/ports/llmrequestmetadata"
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

type AtomicReservation interface {
	// Reserve must create the usage reserve and increment the selected reseller
	// reserve in one atomic operation. An error must leave both states unchanged.
	Reserve(context.Context, ReservationInput) (ReservationResult, error)
}

type RouteReservationTransfer interface {
	// Transfer atomically:
	//   1. verifies ExpectedUsage is still the current reserved usage record;
	//   2. removes its unused estimated upstream reserve from the previous reseller;
	//   3. adds Target.EstimatedUpstreamCostCents to the target reseller reserve;
	//   4. replaces the reserved usage routing, pricing, and estimate snapshot with
	//      the immutable Target snapshot.
	//
	// Any error must leave the usage record and both reseller balances unchanged.
	// Repeating the identical already-committed transfer is idempotent.
	Transfer(
		context.Context,
		RouteReservationTransferInput,
	) (RouteReservationTransferResult, error)
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
