package llmrequest

import (
	"errors"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/bogachenko/tokenio-gateway/internal/ports/llmrequestmetadata"
)

var (
	ErrDependencyRequired = errors.New("llm request dependency is required")

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
