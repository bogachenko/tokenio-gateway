package llmrequest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type recoveryAttemptStore struct {
	loadFunc func(
		context.Context,
		time.Time,
		int,
	) ([]domain.ForwardingAttempt, error)
	completeFunc func(
		context.Context,
		domain.ForwardingAttempt,
	) (domain.ForwardingAttempt, error)
}

func (*recoveryAttemptStore) StartAttempt(
	context.Context,
	domain.ForwardingAttempt,
) (domain.ForwardingAttempt, error) {
	panic("unexpected StartAttempt")
}

func (s *recoveryAttemptStore) CompleteAttempt(
	ctx context.Context,
	attempt domain.ForwardingAttempt,
) (domain.ForwardingAttempt, error) {
	return s.completeFunc(ctx, attempt)
}

func (*recoveryAttemptStore) LoadAttempts(
	context.Context,
	string,
) ([]domain.ForwardingAttempt, error) {
	panic("unexpected LoadAttempts")
}

func (s *recoveryAttemptStore) LoadStartedBefore(
	ctx context.Context,
	cutoff time.Time,
	limit int,
) ([]domain.ForwardingAttempt, error) {
	return s.loadFunc(ctx, cutoff, limit)
}

func TestForwardingAttemptRecoveryCompletesStaleStartedAttempts(
	t *testing.T,
) {
	now := validForwardingStageTime()
	staleAfter := 2 * time.Minute
	first := recoveryStartedAttempt(
		"llmreq_first",
		1,
		now.Add(-4*time.Minute),
	)
	second := recoveryStartedAttempt(
		"llmreq_second",
		2,
		now.Add(-3*time.Minute),
	)
	var completed []domain.ForwardingAttempt

	store := &recoveryAttemptStore{
		loadFunc: func(
			_ context.Context,
			cutoff time.Time,
			limit int,
		) ([]domain.ForwardingAttempt, error) {
			if !cutoff.Equal(now.Add(-staleAfter)) ||
				limit != 10 {
				t.Fatalf(
					"load cutoff=%v limit=%d",
					cutoff,
					limit,
				)
			}
			return []domain.ForwardingAttempt{
				first,
				second,
			}, nil
		},
		completeFunc: func(
			ctx context.Context,
			attempt domain.ForwardingAttempt,
		) (domain.ForwardingAttempt, error) {
			if ctx.Err() != nil {
				t.Fatalf(
					"completion context canceled: %v",
					ctx.Err(),
				)
			}
			completed = append(completed, attempt)
			return attempt, nil
		},
	}
	recovery, err := NewForwardingAttemptRecovery(
		store,
		forwardingStageClock{now: now},
		staleAfter,
	)
	if err != nil {
		t.Fatalf("NewForwardingAttemptRecovery: %v", err)
	}

	result, err := recovery.Recover(
		context.Background(),
		10,
	)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if result.Loaded != 2 || result.Completed != 2 {
		t.Fatalf("result = %+v", result)
	}
	if len(completed) != 2 {
		t.Fatalf("completed = %d", len(completed))
	}
	for index, terminal := range completed {
		started := []domain.ForwardingAttempt{
			first,
			second,
		}[index]
		if terminal.LocalRequestID !=
			started.LocalRequestID ||
			terminal.AttemptNumber !=
				started.AttemptNumber ||
			terminal.Status !=
				domain.ForwardingAttemptStatusFailed ||
			terminal.AttemptState !=
				domain.ForwardingAttemptStateSentNoResponse ||
			terminal.FailureKind !=
				ForwardingFailureKindProcessInterrupted ||
			terminal.RouteRetryCandidate ||
			terminal.CompletedAt == nil ||
			!terminal.CompletedAt.Equal(now) {
			t.Fatalf("terminal = %+v", terminal)
		}
	}
}

func TestForwardingAttemptRecoveryStopsOnCompletionFailure(
	t *testing.T,
) {
	now := validForwardingStageTime()
	first := recoveryStartedAttempt(
		"llmreq_first",
		1,
		now.Add(-4*time.Minute),
	)
	second := recoveryStartedAttempt(
		"llmreq_second",
		1,
		now.Add(-3*time.Minute),
	)
	completionErr := errors.New("completion failed")
	calls := 0

	store := &recoveryAttemptStore{
		loadFunc: func(
			context.Context,
			time.Time,
			int,
		) ([]domain.ForwardingAttempt, error) {
			return []domain.ForwardingAttempt{
				first,
				second,
			}, nil
		},
		completeFunc: func(
			context.Context,
			domain.ForwardingAttempt,
		) (domain.ForwardingAttempt, error) {
			calls++
			return domain.ForwardingAttempt{},
				completionErr
		},
	}
	recovery, err := NewForwardingAttemptRecovery(
		store,
		forwardingStageClock{now: now},
		time.Minute,
	)
	if err != nil {
		t.Fatalf("NewForwardingAttemptRecovery: %v", err)
	}

	result, err := recovery.Recover(
		context.Background(),
		10,
	)
	if !errors.Is(err, completionErr) {
		t.Fatalf("error = %v", err)
	}
	if result.Loaded != 2 ||
		result.Completed != 0 ||
		calls != 1 {
		t.Fatalf(
			"result=%+v calls=%d",
			result,
			calls,
		)
	}
}

func TestInterruptedForwardingAttemptPreservesIdentity(
	t *testing.T,
) {
	now := validForwardingStageTime()
	started := recoveryStartedAttempt(
		"llmreq_test",
		3,
		now.Add(-time.Hour),
	)
	terminal := interruptedForwardingAttempt(started, now)

	if !reflect.DeepEqual(
		forwardingAttemptIdentityForRecovery(started),
		forwardingAttemptIdentityForRecovery(terminal),
	) {
		t.Fatalf(
			"identity changed: started=%+v terminal=%+v",
			started,
			terminal,
		)
	}
	if terminal.AttemptState !=
		domain.ForwardingAttemptStateSentNoResponse {
		t.Fatalf("terminal = %+v", terminal)
	}
}

func TestNewForwardingAttemptRecoveryRequiresDependencies(
	t *testing.T,
) {
	store := &recoveryAttemptStore{}
	clock := forwardingStageClock{
		now: validForwardingStageTime(),
	}
	tests := []struct {
		name       string
		store      ports.ForwardingAttemptStore
		clock      ports.Clock
		staleAfter time.Duration
	}{
		{
			name:       "store",
			clock:      clock,
			staleAfter: time.Minute,
		},
		{
			name:       "clock",
			store:      store,
			staleAfter: time.Minute,
		},
		{
			name:  "stale after",
			store: store,
			clock: clock,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewForwardingAttemptRecovery(
				test.store,
				test.clock,
				test.staleAfter,
			)
			if !errors.Is(err, ErrDependencyRequired) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func recoveryStartedAttempt(
	localRequestID string,
	attemptNumber int,
	startedAt time.Time,
) domain.ForwardingAttempt {
	return domain.ForwardingAttempt{
		LocalRequestID: localRequestID,
		AttemptNumber:  attemptNumber,
		RouteID:        "route-1",
		ResellerID:     "reseller-1",
		APIFamily: domain.
			APIFamilyOpenAICompatible,
		EndpointKind:  domain.EndpointChat,
		ClientModel:   "client-model",
		ProviderType:  domain.ProviderOpenAI,
		ProviderModel: "provider-model",
		Status: domain.
			ForwardingAttemptStatusStarted,
		StartedAt: startedAt,
	}
}

func forwardingAttemptIdentityForRecovery(
	attempt domain.ForwardingAttempt,
) []any {
	return []any{
		attempt.LocalRequestID,
		attempt.AttemptNumber,
		attempt.RouteID,
		attempt.ResellerID,
		attempt.APIFamily,
		attempt.EndpointKind,
		attempt.ClientModel,
		attempt.ProviderType,
		attempt.ProviderModel,
		attempt.StartedAt,
	}
}
