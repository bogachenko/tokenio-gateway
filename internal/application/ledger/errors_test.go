package ledger

import (
	"errors"
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

func TestUsageStoreUnavailableUsesStageAwareApplicationContract(
	t *testing.T,
) {
	repositoryCause := errors.New("repository unavailable")

	for _, stage := range []ports.RequestStage{
		ports.RequestStagePreForwarding,
		ports.RequestStagePostForwarding,
	} {
		t.Run(string(stage), func(t *testing.T) {
			err := usageStoreUnavailable(stage, repositoryCause)

			failure, ok := ports.AsApplicationError(err)
			if !ok {
				t.Fatal("usage-store error is not normalized")
			}
			if failure.Code != domain.ErrorCodeUsageStoreUnavailable ||
				failure.SafeMessage != "Usage store is unavailable" ||
				failure.Category != ports.FailureCategoryUnavailable ||
				failure.Retryability != ports.RetryabilityRetryable ||
				failure.RequestStage != stage ||
				failure.Cause == nil {
				t.Fatalf("failure = %+v", failure)
			}
			if !errors.Is(err, ErrUsageStoreUnavailable) {
				t.Fatal("ledger sentinel identity was not preserved")
			}
			if !errors.Is(err, repositoryCause) {
				t.Fatal("repository cause was not preserved")
			}
			if err.Error() != "Usage store is unavailable" {
				t.Fatalf("public error = %q", err.Error())
			}
		})
	}
}
