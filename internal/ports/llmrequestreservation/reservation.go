package llmrequestreservation

import (
	"context"
	"errors"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

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

type Principal struct {
	UserID               string
	APIKeyID             string
	BillingSubjectUserID string
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

var (
	ErrDependencyRequired = errors.New("llm request dependency is required")

	ErrLocalRequestConflict = &ports.ApplicationError{
		Code:         domain.ErrorCodeIdempotencyKeyReused,
		SafeMessage:  "Idempotency key conflicts with an existing request",
		Category:     ports.FailureCategoryConflict,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("local request conflict"),
	}
	ErrRequestInProgress = &ports.ApplicationError{
		Code:         domain.ErrorCodeRequestInProgress,
		SafeMessage:  "Request is already in progress",
		Category:     ports.FailureCategoryConflict,
		Retryability: ports.RetryabilityRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("request in progress"),
	}
	ErrIdempotencyReplayNotAvailable = &ports.ApplicationError{
		Code:         domain.ErrorCodeIdempotencyReplayNotAvailable,
		SafeMessage:  "Idempotency replay is not available",
		Category:     ports.FailureCategoryConflict,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("idempotency replay not available"),
	}
	ErrIdempotencyKeyReused = &ports.ApplicationError{
		Code:         domain.ErrorCodeIdempotencyKeyReused,
		SafeMessage:  "Idempotency key conflicts with an existing request",
		Category:     ports.FailureCategoryConflict,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("idempotency key reused"),
	}
	ErrUnresolvedUsage = &ports.ApplicationError{
		Code:         domain.ErrorCodeUnresolvedUsage,
		SafeMessage:  "Previous usage requires resolution",
		Category:     ports.FailureCategoryConflict,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        domain.ErrUnresolvedUsage,
	}
	ErrResellerReserveUnavailable = errors.New("reseller reserve unavailable")
)
