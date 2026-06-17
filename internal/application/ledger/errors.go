package ledger

import (
	"errors"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

var (
	ErrInvalidLedgerInput          = errors.New("invalid ledger input")
	ErrInvalidUsageStatus          = domain.ErrInvalidUsageStatus
	ErrInvalidStateTransition      = errors.New("invalid usage state transition")
	ErrUsageNotFound               = errors.New("usage record not found")
	ErrUsageStoreUnavailable       = errors.New("usage store unavailable")
	ErrUsageStoreContractViolation = errors.New("usage store contract violation")
	ErrLocalRequestConflict        = &ports.ApplicationError{
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
	ErrUnresolvedUsage     = domain.ErrUnresolvedUsage
	ErrLedgerStateConflict = errors.New("ledger state conflict")
	ErrInsufficientFunds   = domain.ErrInsufficientFunds
	ErrAmountOverflow      = domain.ErrFinancialAmountOverflow
	ErrRecordCorrupt       = domain.ErrUsageRecordCorrupt
)
