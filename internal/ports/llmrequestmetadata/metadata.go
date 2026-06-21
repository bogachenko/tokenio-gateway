package llmrequestmetadata

import (
	"context"
	"errors"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type RequestParser interface {
	Parse(context.Context, ParseInput) (ParsedRequest, error)
}

type CapabilityDetector interface {
	Detect(context.Context, CapabilityInput) (domain.CapabilitySet, error)
}

type ParseInput struct {
	APIFamily    domain.APIFamily
	EndpointKind domain.EndpointKind
	PathModel    string
	Payload      []byte
}

type ParsedRequest struct {
	ClientModel string
}

type CapabilityInput struct {
	APIFamily    domain.APIFamily
	EndpointKind domain.EndpointKind
	ClientModel  string
	PathModel    string
	Payload      []byte
}

var (
	ErrInvalidInput           = errors.New("invalid llm request input")
	ErrStageContractViolation = errors.New("llm request stage contract violation")

	ErrInvalidJSON = &ports.ApplicationError{
		Code:         domain.ErrorCodeInvalidJSON,
		SafeMessage:  "Request body must contain valid JSON",
		Category:     ports.FailureCategoryInvalidRequest,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("invalid json"),
	}
	ErrModelRequired = &ports.ApplicationError{
		Code:         domain.ErrorCodeModelRequired,
		SafeMessage:  "Model is required",
		Category:     ports.FailureCategoryInvalidRequest,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("model required"),
	}
	ErrStreamingUnsupported = &ports.ApplicationError{
		Code:         domain.ErrorCodeStreamingUnsupported,
		SafeMessage:  "Streaming is not supported",
		Category:     ports.FailureCategoryInvalidRequest,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("streaming unsupported"),
	}
)
