package ledger

import (
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

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
				t.Fatal("ledger error is not normalized")
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
