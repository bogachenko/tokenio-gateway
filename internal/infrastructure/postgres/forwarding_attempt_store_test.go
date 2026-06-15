package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestValidateStartedForwardingAttempt(t *testing.T) {
	attempt := validStartedForwardingAttempt()
	if err := validateStartedForwardingAttempt(attempt); err != nil {
		t.Fatalf("validate started: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*domain.ForwardingAttempt)
	}{
		{
			name: "blank request",
			mutate: func(value *domain.ForwardingAttempt) {
				value.LocalRequestID = ""
			},
		},
		{
			name: "non-positive number",
			mutate: func(value *domain.ForwardingAttempt) {
				value.AttemptNumber = 0
			},
		},
		{
			name: "terminal state",
			mutate: func(value *domain.ForwardingAttempt) {
				value.AttemptState =
					domain.ForwardingAttemptStateNotSent
			},
		},
		{
			name: "completed",
			mutate: func(value *domain.ForwardingAttempt) {
				completedAt := value.StartedAt
				value.CompletedAt = &completedAt
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := attempt
			test.mutate(&value)
			if !errors.Is(
				validateStartedForwardingAttempt(value),
				ports.ErrStoreContractViolation,
			) {
				t.Fatalf("attempt accepted: %+v", value)
			}
		})
	}
}

func TestValidateTerminalForwardingAttempt(t *testing.T) {
	started := validStartedForwardingAttempt()
	completedAt := started.StartedAt.Add(time.Second)

	succeeded := started
	succeeded.Status = domain.ForwardingAttemptStatusSucceeded
	succeeded.AttemptState =
		domain.ForwardingAttemptStateResponseReceived
	succeeded.UpstreamStatusCode = 200
	succeeded.CompletedAt = &completedAt
	if err := validateTerminalForwardingAttempt(succeeded); err != nil {
		t.Fatalf("validate succeeded: %v", err)
	}

	failed := started
	failed.Status = domain.ForwardingAttemptStatusFailed
	failed.AttemptState = domain.ForwardingAttemptStateNotSent
	failed.FailureKind = "unavailable"
	failed.RouteRetryCandidate = true
	failed.CompletedAt = &completedAt
	if err := validateTerminalForwardingAttempt(failed); err != nil {
		t.Fatalf("validate failed: %v", err)
	}

	failed.AttemptState = ""
	if !errors.Is(
		validateTerminalForwardingAttempt(failed),
		ports.ErrStoreContractViolation,
	) {
		t.Fatalf("failed attempt without state accepted: %+v", failed)
	}
}

func TestForwardingAttemptIdentityAndTerminalEquality(t *testing.T) {
	started := validStartedForwardingAttempt()
	replay := started
	if !forwardingAttemptsEqual(started, replay) {
		t.Fatal("identical started attempts differ")
	}

	completedAt := started.StartedAt.Add(time.Second)
	left := started
	left.Status = domain.ForwardingAttemptStatusFailed
	left.AttemptState = domain.ForwardingAttemptStateNotSent
	left.FailureKind = "unavailable"
	left.RouteRetryCandidate = true
	left.CompletedAt = &completedAt

	right := left
	copiedCompletedAt := completedAt
	right.CompletedAt = &copiedCompletedAt
	if !forwardingAttemptsEqual(left, right) {
		t.Fatal("identical terminal attempts differ")
	}

	right.RouteRetryCandidate = false
	if forwardingAttemptsEqual(left, right) {
		t.Fatal("conflicting terminal attempts compare equal")
	}
}

func validStartedForwardingAttempt() domain.ForwardingAttempt {
	return domain.ForwardingAttempt{
		LocalRequestID: "llmreq_test",
		AttemptNumber:  1,
		RouteID:        "route-1",
		ResellerID:     "reseller-1",
		APIFamily:      domain.APIFamilyOpenAICompatible,
		EndpointKind:   domain.EndpointChat,
		ClientModel:    "client-model",
		ProviderType:   domain.ProviderOpenAI,
		ProviderModel:  "provider-model",
		Status:         domain.ForwardingAttemptStatusStarted,
		StartedAt: time.Date(
			2026,
			time.June,
			15,
			12,
			0,
			0,
			0,
			time.UTC,
		),
	}
}
