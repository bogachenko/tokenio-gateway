package billing

import (
	"errors"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

var (
	ErrInvalidBillingInput        = errors.New("invalid billing input")
	ErrBillingIdentityUnavailable = &ports.ApplicationError{
		Code:         domain.ErrorCodeBillingUnavailable,
		SafeMessage:  "Billing service is unavailable",
		Category:     ports.FailureCategoryDependencyUnavailable,
		Retryability: ports.RetryabilityRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("billing identity unavailable"),
	}
	ErrBillingUnavailable = &ports.ApplicationError{
		Code:         domain.ErrorCodeBillingUnavailable,
		SafeMessage:  "Billing service is unavailable",
		Category:     ports.FailureCategoryDependencyUnavailable,
		Retryability: ports.RetryabilityRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("billing unavailable"),
	}
	ErrBillingStoreUnavailable       = errors.New("billing store unavailable")
	ErrBillingStoreContractViolation = errors.New("billing store contract violation")
	ErrUnresolvedUsage               = &ports.ApplicationError{
		Code:         domain.ErrorCodeUnresolvedUsage,
		SafeMessage:  "Previous usage requires resolution",
		Category:     ports.FailureCategoryConflict,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        domain.ErrUnresolvedUsage,
	}
	ErrInsufficientFunds = &ports.ApplicationError{
		Code:         domain.ErrorCodeInsufficientFunds,
		SafeMessage:  "Insufficient balance",
		Category:     ports.FailureCategoryPaymentRequired,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        domain.ErrInsufficientFunds,
	}
	ErrChargeDeferred               = errors.New("charge deferred")
	ErrChargeReconciliationRequired = errors.New("charge reconciliation required")
	ErrInvalidChargePlan            = errors.New("invalid charge plan")
	ErrTokenOverflow                = errors.New("billing token overflow")
)
