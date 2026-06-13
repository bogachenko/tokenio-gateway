package modelcatalog

import "errors"

var (
	ErrInvalidInput       = errors.New("invalid model catalog input")
	ErrCatalogUnavailable = errors.New("model catalog unavailable")
)
