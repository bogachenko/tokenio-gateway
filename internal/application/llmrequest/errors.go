package llmrequest

import "errors"

var (
	ErrDependencyRequired            = errors.New("llm request dependency is required")
	ErrInvalidInput                  = errors.New("invalid llm request input")
	ErrStageContractViolation        = errors.New("llm request stage contract violation")
	ErrLocalRequestConflict          = errors.New("local request conflict")
	ErrRequestInProgress             = errors.New("request in progress")
	ErrIdempotencyReplayNotAvailable = errors.New("idempotency replay not available")
	ErrIdempotencyKeyReused          = errors.New("idempotency key reused")
	ErrUnresolvedUsage               = errors.New("unresolved usage")
	ErrResellerReserveUnavailable    = errors.New("reseller reserve unavailable")
)
