package routecapacity

import (
	"errors"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

var (
	ErrInvalidInput        = errors.New("invalid route capacity input")
	ErrCapacityUnavailable = ports.ErrRouteCapacityUnavailable
	ErrReservationConflict = ports.ErrRouteCapacityReservationConflict
	ErrAmountOverflow      = errors.New("route capacity amount overflow")
)
