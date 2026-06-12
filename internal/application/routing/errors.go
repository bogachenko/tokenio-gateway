package routing

import "errors"

var (
	ErrInvalidSelectionInput = errors.New("invalid route selection input")
	ErrUnknownModel          = errors.New("unknown model")
	ErrUnsupportedCapability = errors.New("unsupported capability")
	ErrNoRouteAvailable      = errors.New("no route available")
	ErrInvalidRouteData      = errors.New("invalid route data")
)
