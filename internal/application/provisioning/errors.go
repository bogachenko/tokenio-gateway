package provisioning

import "errors"

var (
	ErrInvalidRequest = errors.New(
		"invalid provisioning request",
	)
	ErrConflict = errors.New(
		"provisioning conflict",
	)
	ErrExpired = errors.New(
		"provisioning expired",
	)
	ErrStoreUnavailable = errors.New(
		"provisioning store unavailable",
	)
	ErrCryptoUnavailable = errors.New(
		"provisioning crypto unavailable",
	)
	ErrInternal = errors.New(
		"provisioning internal error",
	)
)
