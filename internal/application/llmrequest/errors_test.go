package llmrequest

import (
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestPublicErrorsUseNormalizedApplicationContract(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		code         domain.ErrorCode
		message      string
		category     ports.FailureCategory
		retryability ports.Retryability
	}{
		{
			name:         "invalid json",
			err:          ErrInvalidJSON,
			code:         domain.ErrorCodeInvalidJSON,
			message:      "Request body must contain valid JSON",
			category:     ports.FailureCategoryInvalidRequest,
			retryability: ports.RetryabilityNonRetryable,
		},
		{
			name:         "model required",
			err:          ErrModelRequired,
			code:         domain.ErrorCodeModelRequired,
			message:      "Model is required",
			category:     ports.FailureCategoryInvalidRequest,
			retryability: ports.RetryabilityNonRetryable,
		},
		{
			name:         "streaming unsupported",
			err:          ErrStreamingUnsupported,
			code:         domain.ErrorCodeStreamingUnsupported,
			message:      "Streaming is not supported",
			category:     ports.FailureCategoryInvalidRequest,
			retryability: ports.RetryabilityNonRetryable,
		},
		{
			name:         "unknown model",
			err:          ErrUnknownModel,
			code:         domain.ErrorCodeUnknownModel,
			message:      "Unknown model",
			category:     ports.FailureCategoryInvalidRequest,
			retryability: ports.RetryabilityNonRetryable,
		},
		{
			name:         "unsupported capability",
			err:          ErrUnsupportedCapability,
			code:         domain.ErrorCodeUnsupportedCapability,
			message:      "Unsupported capability",
			category:     ports.FailureCategoryInvalidRequest,
			retryability: ports.RetryabilityNonRetryable,
		},
		{
			name:         "no route available",
			err:          ErrNoRouteAvailable,
			code:         domain.ErrorCodeNoRouteAvailable,
			message:      "No route is available",
			category:     ports.FailureCategoryUnavailable,
			retryability: ports.RetryabilityRetryable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failure, ok := ports.AsApplicationError(test.err)
			if !ok {
				t.Fatal("error is not a normalized application error")
			}
			if failure.Code != test.code ||
				failure.SafeMessage != test.message ||
				failure.Category != test.category ||
				failure.Retryability != test.retryability ||
				failure.RequestStage != ports.RequestStagePreForwarding ||
				failure.Cause == nil {
				t.Fatalf("failure = %+v", failure)
			}
			if failure.Error() != test.message {
				t.Fatalf("Error() = %q", failure.Error())
			}
			if !errors.Is(test.err, failure) {
				t.Fatal("error identity was not preserved")
			}
		})
	}
}

func TestIdempotencyErrorsUseNormalizedApplicationContract(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		code         domain.ErrorCode
		message      string
		retryability ports.Retryability
	}{
		{
			name:         "local request conflict",
			err:          ErrLocalRequestConflict,
			code:         domain.ErrorCodeIdempotencyKeyReused,
			message:      "Idempotency key conflicts with an existing request",
			retryability: ports.RetryabilityNonRetryable,
		},
		{
			name:         "request in progress",
			err:          ErrRequestInProgress,
			code:         domain.ErrorCodeRequestInProgress,
			message:      "Request is already in progress",
			retryability: ports.RetryabilityRetryable,
		},
		{
			name:         "replay unavailable",
			err:          ErrIdempotencyReplayNotAvailable,
			code:         domain.ErrorCodeIdempotencyReplayNotAvailable,
			message:      "Idempotency replay is not available",
			retryability: ports.RetryabilityNonRetryable,
		},
		{
			name:         "key reused",
			err:          ErrIdempotencyKeyReused,
			code:         domain.ErrorCodeIdempotencyKeyReused,
			message:      "Idempotency key conflicts with an existing request",
			retryability: ports.RetryabilityNonRetryable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failure, ok := ports.AsApplicationError(test.err)
			if !ok {
				t.Fatal("idempotency error is not normalized")
			}
			if failure.Code != test.code ||
				failure.SafeMessage != test.message ||
				failure.Category != ports.FailureCategoryConflict ||
				failure.Retryability != test.retryability ||
				failure.RequestStage != ports.RequestStagePreForwarding ||
				failure.Cause == nil {
				t.Fatalf("failure = %+v", failure)
			}
		})
	}
}
