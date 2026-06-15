package app

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"

	llmrequest "github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	forwardingattemptrecovery "github.com/bogachenko/tokenio-gateway/internal/worker/forwardingattemptrecovery"
)

func TestForwardingAttemptRecoveryLogObserverUsesSafeFields(
	t *testing.T,
) {
	var buffer bytes.Buffer
	observer, err :=
		NewForwardingAttemptRecoveryLogObserver(
			log.New(&buffer, "", 0),
		)
	if err != nil {
		t.Fatalf("New observer: %v", err)
	}
	raw := "secret upstream payload"
	observer.ObserveForwardingAttemptRecoveryCycle(
		forwardingattemptrecovery.Cycle{
			Result: llmrequest.ForwardingAttemptRecoveryResult{
				Loaded:    3,
				Completed: 2,
			},
			Err: errors.New(raw),
		},
	)

	output := buffer.String()
	if strings.Contains(output, raw) {
		t.Fatalf("raw error leaked: %q", output)
	}
	for _, expected := range []string{
		"loaded=3",
		"completed=2",
		"error_type=",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf(
				"missing %q in %q",
				expected,
				output,
			)
		}
	}
}

func TestNewForwardingAttemptRecoveryLogObserverRejectsNilLogger(
	t *testing.T,
) {
	_, err := NewForwardingAttemptRecoveryLogObserver(nil)
	if !errors.Is(
		err,
		ErrInvalidForwardingAttemptRecoveryObserver,
	) {
		t.Fatalf("error = %v", err)
	}
}
