package openaicompatrequest

import "errors"

var (
	ErrInvalidJSON          = errors.New("invalid JSON request body")
	ErrModelRequired        = errors.New("model is required")
	ErrStreamingUnsupported = errors.New("streaming is not supported")
	ErrUnsupportedEndpoint  = errors.New("unsupported endpoint kind")
)
