package llmrequest

import "errors"

var (
	ErrDependencyRequired     = errors.New("llm request dependency is required")
	ErrInvalidInput           = errors.New("invalid llm request input")
	ErrStageContractViolation = errors.New("llm request stage contract violation")
)
