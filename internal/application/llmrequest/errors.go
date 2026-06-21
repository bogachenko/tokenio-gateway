package llmrequest

import (
	"errors"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/bogachenko/tokenio-gateway/internal/ports/llmrequestmetadata"
	"github.com/bogachenko/tokenio-gateway/internal/ports/llmrequestreservation"
)

var (
	ErrDependencyRequired = llmrequestreservation.ErrDependencyRequired

	ErrInvalidInput           = llmrequestmetadata.ErrInvalidInput
	ErrStageContractViolation = llmrequestmetadata.ErrStageContractViolation
	ErrInvalidJSON            = llmrequestmetadata.ErrInvalidJSON
	ErrModelRequired          = llmrequestmetadata.ErrModelRequired
	ErrStreamingUnsupported   = llmrequestmetadata.ErrStreamingUnsupported

	ErrUnknownModel = &ports.ApplicationError{
		Code:         domain.ErrorCodeUnknownModel,
		SafeMessage:  "Unknown model",
		Category:     ports.FailureCategoryInvalidRequest,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("unknown model"),
	}
	ErrUnsupportedCapability = &ports.ApplicationError{
		Code:         domain.ErrorCodeUnsupportedCapability,
		SafeMessage:  "Unsupported capability",
		Category:     ports.FailureCategoryInvalidRequest,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("unsupported capability"),
	}
	ErrNoRouteAvailable = &ports.ApplicationError{
		Code:         domain.ErrorCodeNoRouteAvailable,
		SafeMessage:  "No route is available",
		Category:     ports.FailureCategoryUnavailable,
		Retryability: ports.RetryabilityRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("no route available"),
	}
	ErrPricingUnavailable = &ports.ApplicationError{
		Code:         domain.ErrorCodePricingUnavailable,
		SafeMessage:  "Pricing is unavailable",
		Category:     ports.FailureCategoryUnavailable,
		Retryability: ports.RetryabilityRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("pricing unavailable"),
	}
	ErrRouteUnavailable = &ports.ApplicationError{
		Code:         domain.ErrorCodeRouteUnavailable,
		SafeMessage:  "Selected route is unavailable",
		Category:     ports.FailureCategoryUnavailable,
		Retryability: ports.RetryabilityRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("selected route unavailable"),
	}

	ErrLocalRequestConflict = llmrequestreservation.ErrLocalRequestConflict
	ErrRequestInProgress = llmrequestreservation.ErrRequestInProgress
	ErrIdempotencyReplayNotAvailable = llmrequestreservation.ErrIdempotencyReplayNotAvailable
	ErrIdempotencyKeyReused = llmrequestreservation.ErrIdempotencyKeyReused
	ErrUnresolvedUsage = llmrequestreservation.ErrUnresolvedUsage
	ErrResellerReserveUnavailable = llmrequestreservation.ErrResellerReserveUnavailable
)

func upstreamTimeoutError(cause error) error {
	return &ports.ApplicationError{
		Code:         domain.ErrorCodeUpstreamUnavailable,
		SafeMessage:  "Upstream request timed out",
		Category:     ports.FailureCategoryDependencyUnavailable,
		Retryability: ports.RetryabilityRetryable,
		RequestStage: ports.RequestStageForwarding,
		Cause:        cause,
	}
}
