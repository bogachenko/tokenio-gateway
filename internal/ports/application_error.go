package ports

import (
	"errors"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type FailureCategory string

const (
	FailureCategoryDependencyUnavailable FailureCategory = "dependency_unavailable"
	FailureCategoryInvalidRequest        FailureCategory = "invalid_request"
	FailureCategoryUnauthorized          FailureCategory = "unauthorized"
	FailureCategoryForbidden             FailureCategory = "forbidden"
	FailureCategoryPaymentRequired       FailureCategory = "payment_required"
	FailureCategoryConflict              FailureCategory = "conflict"
	FailureCategoryGone                  FailureCategory = "gone"
	FailureCategoryUnavailable           FailureCategory = "unavailable"
	FailureCategoryInternal              FailureCategory = "internal"
)

type Retryability string

const (
	RetryabilityUnknown      Retryability = "unknown"
	RetryabilityRetryable    Retryability = "retryable"
	RetryabilityNonRetryable Retryability = "non_retryable"
)

type RequestStage string

const (
	RequestStagePreForwarding  RequestStage = "pre_forwarding"
	RequestStageForwarding     RequestStage = "forwarding"
	RequestStagePostForwarding RequestStage = "post_forwarding"
)

type ApplicationError struct {
	Code         domain.ErrorCode
	SafeMessage  string
	Category     FailureCategory
	Retryability Retryability
	RequestStage RequestStage
	Cause        error
}

func (e *ApplicationError) Error() string {
	if e == nil || e.SafeMessage == "" {
		return "application error"
	}
	return e.SafeMessage
}

func (e *ApplicationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func AsApplicationError(err error) (*ApplicationError, bool) {
	var target *ApplicationError
	if !errors.As(err, &target) || target == nil {
		return nil, false
	}
	return target, true
}
