package provisioning

import (
	"errors"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

var (
	ErrInvalidRequest = &ports.ApplicationError{
		Code:         domain.ErrorCodeProvisioningInvalidRequest,
		SafeMessage:  "Invalid provisioning request",
		Category:     ports.FailureCategoryInvalidRequest,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("invalid provisioning request"),
	}
	ErrConflict = &ports.ApplicationError{
		Code:         domain.ErrorCodeProvisioningConflict,
		SafeMessage:  "Provisioning request conflicts with existing state",
		Category:     ports.FailureCategoryConflict,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("provisioning conflict"),
	}
	ErrExpired = &ports.ApplicationError{
		Code:         domain.ErrorCodeProvisioningExpired,
		SafeMessage:  "Provisioning delivery window has expired",
		Category:     ports.FailureCategoryGone,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("provisioning expired"),
	}
	ErrStoreUnavailable = &ports.ApplicationError{
		Code:         domain.ErrorCodeProvisioningStoreUnavailable,
		SafeMessage:  "Provisioning store is unavailable",
		Category:     ports.FailureCategoryUnavailable,
		Retryability: ports.RetryabilityRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("provisioning store unavailable"),
	}
	ErrCryptoUnavailable = &ports.ApplicationError{
		Code:         domain.ErrorCodeProvisioningCryptoUnavailable,
		SafeMessage:  "Provisioning encryption is unavailable",
		Category:     ports.FailureCategoryInternal,
		Retryability: ports.RetryabilityUnknown,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("provisioning crypto unavailable"),
	}
	ErrInternal = &ports.ApplicationError{
		Code:         domain.ErrorCodeInternalError,
		SafeMessage:  "Internal error",
		Category:     ports.FailureCategoryInternal,
		Retryability: ports.RetryabilityUnknown,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("provisioning internal error"),
	}
)
