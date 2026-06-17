package llmrequest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type routeCooldownStoreStub struct {
	calls  int
	events []domain.RouteEvent
	fn     func(context.Context, domain.Route, domain.Route, domain.RouteEvent) (domain.Route, error)
}

func (stub *routeCooldownStoreStub) CompareAndSwapRouteCooldownWithEvent(
	ctx context.Context, expected domain.Route, next domain.Route, event domain.RouteEvent,
) (domain.Route, error) {
	stub.calls++
	if stub.fn != nil {
		return stub.fn(ctx, expected, next, event)
	}
	return next, nil
}

func TestCooldownDurationForFailureKind(t *testing.T) {
	policy := mustValidRoutingPolicy(t)
	tests := []struct {
		kind string
		want time.Duration
		ok   bool
	}{
		{"rate_limited", policy.CooldownRateLimit(), true},
		{"quota_exceeded", policy.CooldownQuotaExceeded(), true},
		{"provider_5xx", policy.Cooldown5XX(), true},
		{"timeout", policy.CooldownTimeout(), true},
		{"auth_error", policy.CooldownAuthError(), true},
		{"request_error", 0, false},
		{"connection_error", 0, false},
		{"uncertain_processing", 0, false},
		{"malformed_response", 0, false},
	}
	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			got, ok := cooldownDurationForFailureKind(policy, test.kind)
			if got != test.want || ok != test.ok {
				t.Fatalf("got=(%s,%v) want=(%s,%v)", got, ok, test.want, test.ok)
			}
		})
	}
}

func TestForwardingStagePersistsCooldownAfterClassifiedFailure(t *testing.T) {
	prepared := validForwardingPreparedRequest()
	cooldowns := &routeCooldownStoreStub{
		fn: func(_ context.Context, expected, next domain.Route, event domain.RouteEvent) (domain.Route, error) {
			if expected.ID != prepared.Plan.Route.ID {
				t.Fatalf("expected route=%+v", expected)
			}
			if next.CooldownReason != "auth_error" || next.LastErrorCode != "auth_error" ||
				next.CooldownUntil == nil || next.LastErrorAt == nil {
				t.Fatalf("next=%+v", next)
			}
			if event.EventType != domain.RouteEventTypeCooldownSet ||
				event.Reason != "auth_error" || event.LocalRequestID != prepared.LocalRequestID {
				t.Fatalf("event=%+v", event)
			}
			return next, nil
		},
	}
	forwardErr := &retryFailure{
		kind:         "auth_error",
		statusCode:   401,
		attemptState: string(domain.ForwardingAttemptStateResponseReceived),
		retry:        false,
		cause:        errors.New("auth failed"),
	}
	stage, err := NewForwardingStage(
		validForwardingCapacityManager(nil),
		reserveFunc(func(_ context.Context, input ReservationInput) (ReservationResult, error) {
			return validReservation(input), nil
		}),
		&routeReservationTransferStub{},
		&forwardingAttemptStoreStub{},
		cooldowns,
		forwardingStageClock{now: validForwardingStageTime()},
		forwardingExecutorFunc(func(context.Context, ForwardingExecutionInput) (ForwardingExecutionResult, error) {
			return ForwardingExecutionResult{}, forwardErr
		}),
		mustValidRoutingPolicy(t),
		immediateRetryWaiter{},
	)
	if err != nil {
		t.Fatalf("NewForwardingStage: %v", err)
	}
	_, err = stage.Execute(context.Background(), prepared, validForwardingAdmission(prepared))
	if !errors.Is(err, forwardErr) {
		t.Fatalf("error=%v", err)
	}
	if cooldowns.calls != 1 {
		t.Fatalf("calls=%d want=1", cooldowns.calls)
	}
}
