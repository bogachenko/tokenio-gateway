package ledger

import "errors"

var (
	ErrInvalidLedgerInput            = errors.New("invalid ledger input")
	ErrInvalidUsageStatus            = errors.New("invalid usage status")
	ErrInvalidStateTransition        = errors.New("invalid usage state transition")
	ErrUsageNotFound                 = errors.New("usage record not found")
	ErrUsageStoreUnavailable         = errors.New("usage store unavailable")
	ErrUsageStoreContractViolation   = errors.New("usage store contract violation")
	ErrLocalRequestConflict          = errors.New("local request conflict")
	ErrRequestInProgress             = errors.New("request in progress")
	ErrIdempotencyReplayNotAvailable = errors.New("idempotency replay not available")
	ErrIdempotencyKeyReused          = errors.New("idempotency key reused")
	ErrUnresolvedUsage               = errors.New("unresolved usage")
	ErrLedgerStateConflict           = errors.New("ledger state conflict")
	ErrInsufficientFunds             = errors.New("insufficient funds")
	ErrAmountOverflow                = errors.New("ledger amount overflow")
	ErrRecordCorrupt                 = errors.New("corrupt usage record")
)
