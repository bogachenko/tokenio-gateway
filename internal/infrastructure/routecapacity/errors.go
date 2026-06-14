package routecapacity

import "errors"

var (
	ErrInvalidInput        = errors.New("invalid route capacity input")
	ErrCapacityUnavailable = errors.New("route capacity unavailable")
	ErrReservationConflict = errors.New("route capacity reservation conflict")
	ErrAmountOverflow      = errors.New("route capacity amount overflow")
)
