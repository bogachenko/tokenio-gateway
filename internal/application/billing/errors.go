package billing

import "errors"

var (
	ErrInvalidBillingInput           = errors.New("invalid billing input")
	ErrBillingIdentityUnavailable    = errors.New("billing identity unavailable")
	ErrBillingUnavailable            = errors.New("billing unavailable")
	ErrBillingStoreUnavailable       = errors.New("billing store unavailable")
	ErrBillingStoreContractViolation = errors.New("billing store contract violation")
	ErrChargeDeferred                = errors.New("charge deferred")
	ErrChargeReconciliationRequired  = errors.New("charge reconciliation required")
	ErrInvalidChargePlan             = errors.New("invalid charge plan")
	ErrTokenOverflow                 = errors.New("billing token overflow")
)
