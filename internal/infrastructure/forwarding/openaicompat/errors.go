package openaicompat

import "errors"

var (
	ErrInvalidAdapterConfig     = errors.New("invalid forwarding adapter config")
	ErrInvalidForwardRequest    = errors.New("invalid forward request")
	ErrUnsupportedRoute         = errors.New("unsupported forwarding route")
	ErrModelRewriteFailed       = errors.New("model rewrite failed")
	ErrInvalidUpstreamURL       = errors.New("invalid upstream url")
	ErrUpstreamResponseTooLarge = errors.New("upstream response too large")
)
