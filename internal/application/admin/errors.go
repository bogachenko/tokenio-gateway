package admin

import "errors"

var (
	ErrInvalidRequest     = errors.New("invalid admin request")
	ErrNotFound           = errors.New("admin resource not found")
	ErrConflict           = errors.New("admin resource conflict")
	ErrStateConflict      = errors.New("admin resource state conflict")
	ErrSecretNotAvailable = errors.New("admin secret not available")
	ErrStoreUnavailable   = errors.New("admin store unavailable")
	ErrInternal           = errors.New("admin internal error")

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
