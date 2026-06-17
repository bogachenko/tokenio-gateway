package admin

import (
	"errors"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

var (
	ErrInvalidRequest = &ports.ApplicationError{
		Code:         domain.ErrorCodeAdminValidationError,
		SafeMessage:  "Invalid admin request",
		Category:     ports.FailureCategoryInvalidRequest,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("invalid admin request"),
	}
	ErrNotFound = &ports.ApplicationError{
		Code:         domain.ErrorCodeAdminNotFound,
		SafeMessage:  "Resource not found",
		Category:     ports.FailureCategoryNotFound,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("admin resource not found"),
	}
	ErrConflict = &ports.ApplicationError{
		Code:         domain.ErrorCodeAdminConflict,
		SafeMessage:  "Resource conflict",
		Category:     ports.FailureCategoryConflict,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("admin resource conflict"),
	}
	ErrStateConflict = &ports.ApplicationError{
		Code:         domain.ErrorCodeAdminStateConflict,
		SafeMessage:  "Resource is in incompatible state",
		Category:     ports.FailureCategoryConflict,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("admin resource state conflict"),
	}
	ErrSecretNotAvailable = &ports.ApplicationError{
		Code:         domain.ErrorCodeAdminSecretNotAvailable,
		SafeMessage:  "Required secret is not available in environment",
		Category:     ports.FailureCategoryConflict,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("admin secret not available"),
	}
	ErrStoreUnavailable = &ports.ApplicationError{
		Code:         domain.ErrorCodeStoreUnavailable,
		SafeMessage:  "Store is unavailable",
		Category:     ports.FailureCategoryUnavailable,
		Retryability: ports.RetryabilityRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("admin store unavailable"),
	}
	ErrInternal = &ports.ApplicationError{
		Code:         domain.ErrorCodeInternalError,
		SafeMessage:  "Internal error",
		Category:     ports.FailureCategoryInternal,
		Retryability: ports.RetryabilityUnknown,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("admin internal error"),
	}

	ErrBatchRetryNotFound = errors.New(
		"admin failed billing batch retry target not found",
	)
	ErrBatchRetryStateConflict = errors.New(
		"admin failed billing batch retry state conflict",
	)
	ErrBatchRetryUnavailable = errors.New(
		"admin failed billing batch retry unavailable",
	)
	ErrBatchRetryInternal = errors.New(
		"admin failed billing batch retry internal error",
	)
)
