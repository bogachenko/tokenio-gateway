package registry

import "errors"

var (
	ErrInvalidRegistration   = errors.New("invalid forwarding factory registration")
	ErrDuplicateRegistration = errors.New("duplicate forwarding factory registration")
	ErrInvalidBuildInput     = errors.New("invalid forwarding adapter build input")
	ErrFactoryNotRegistered  = errors.New("forwarding adapter factory not registered")
)
