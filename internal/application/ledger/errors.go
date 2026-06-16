package ledger

import (
	"errors"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

var (
	ErrInvalidLedgerInput            = errors.New("invalid ledger input")
	ErrInvalidUsageStatus            = domain.ErrInvalidUsageStatus
	ErrInvalidStateTransition        = errors.New("invalid usage state transition")
	ErrUsageNotFound                 = errors.New("usage record not found")
	ErrUsageStoreUnavailable         = errors.New("usage store unavailable")
	ErrUsageStoreContractViolation   = errors.New("usage store contract violation")
	ErrLocalRequestConflict          = errors.New("local request conflict")
	ErrRequestInProgress             = errors.New("request in progress")
	ErrIdempotencyReplayNotAvailable = errors.New("idempotency replay not available")
	ErrIdempotencyKeyReused          = errors.New("idempotency key reused")
	ErrUnresolvedUsage               = domain.ErrUnresolvedUsage
	ErrLedgerStateConflict           = errors.New("ledger state conflict")
	ErrInsufficientFunds             = domain.ErrInsufficientFunds
	ErrAmountOverflow                = domain.ErrFinancialAmountOverflow
	ErrRecordCorrupt                 = domain.ErrUsageRecordCorrupt
)
