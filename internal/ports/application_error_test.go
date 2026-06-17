package ports

import (
	"errors"
	"fmt"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestApplicationErrorPreservesSafeContractAndCause(t *testing.T) {
	cause := errors.New("postgres password secret-value")
	failure := &ApplicationError{
		Code:         domain.ErrorCodeStoreUnavailable,
		SafeMessage:  "Store is unavailable",
		Category:     FailureCategoryDependencyUnavailable,
		Retryability: RetryabilityRetryable,
		RequestStage: RequestStagePreForwarding,
		Cause:        cause,
	}

	wrapped := fmt.Errorf("catalog: %w", failure)
	actual, ok := AsApplicationError(wrapped)
	if !ok {
		t.Fatal("wrapped application error was not recognized")
	}
	if actual != failure {
		t.Fatal("application error identity was not preserved")
	}
	if actual.Error() != "Store is unavailable" {
		t.Fatalf("safe message = %q", actual.Error())
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("cause was not preserved for internal classification/logging")
	}
	if actual.Error() == cause.Error() {
		t.Fatal("raw cause leaked through Error()")
	}
}

func TestAsApplicationErrorRejectsPlainError(t *testing.T) {
	if _, ok := AsApplicationError(errors.New("plain")); ok {
		t.Fatal("plain error was classified as application error")
	}
}
