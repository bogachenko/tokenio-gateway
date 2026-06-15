package llmrequest

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type forwardingCapacityManager struct {
	checkFunc   func(context.Context, ports.RouteCapacityCheckInput) (ports.RouteCapacityResult, error)
	acquireFunc func(context.Context, ports.RouteCapacityAcquireInput) (ports.RouteCapacityReservation, error)
	releaseFunc func(context.Context, ports.RouteCapacityReservation) error
}

func (manager *forwardingCapacityManager) Check(
	ctx context.Context,
	input ports.RouteCapacityCheckInput,
) (ports.RouteCapacityResult, error) {
	if manager.checkFunc == nil {
		panic("unexpected capacity Check")
	}
	return manager.checkFunc(ctx, input)
}

func (manager *forwardingCapacityManager) Acquire(
	ctx context.Context,
	input ports.RouteCapacityAcquireInput,
) (ports.RouteCapacityReservation, error) {
	return manager.acquireFunc(ctx, input)
}

func (manager *forwardingCapacityManager) Release(
	ctx context.Context,
	reservation ports.RouteCapacityReservation,
) error {
	return manager.releaseFunc(ctx, reservation)
}

type forwardingStageClock struct {
	now time.Time
}

func (clock forwardingStageClock) Now() time.Time {
	return clock.now
}

type forwardingAttemptStoreStub struct {
	startFunc func(
		context.Context,
		domain.ForwardingAttempt,
	) (domain.ForwardingAttempt, error)
	completeFunc func(
		context.Context,
		domain.ForwardingAttempt,
	) (domain.ForwardingAttempt, error)
}

func (store *forwardingAttemptStoreStub) StartAttempt(
	ctx context.Context,
	attempt domain.ForwardingAttempt,
) (domain.ForwardingAttempt, error) {
	if store.startFunc != nil {
		return store.startFunc(ctx, attempt)
	}
	return attempt, nil
}

func (store *forwardingAttemptStoreStub) CompleteAttempt(
	ctx context.Context,
	attempt domain.ForwardingAttempt,
) (domain.ForwardingAttempt, error) {
	if store.completeFunc != nil {
		return store.completeFunc(ctx, attempt)
	}
	return attempt, nil
}

func (*forwardingAttemptStoreStub) LoadAttempts(
	context.Context,
	string,
) ([]domain.ForwardingAttempt, error) {
	return nil, nil
}

type forwardingExecutorFunc func(
	context.Context,
	ForwardingExecutionInput,
) (ForwardingExecutionResult, error)

func (function forwardingExecutorFunc) Forward(
	ctx context.Context,
	input ForwardingExecutionInput,
) (ForwardingExecutionResult, error) {
	return function(ctx, input)
}

func TestForwardingStageExecutesCapacityReservationForwardRelease(
	t *testing.T,
) {
	var calls []string
	prepared := validForwardingPreparedRequest()
	admission := validAdmission(
		BillingAdmissionInput{
			Principal:            prepared.Principal,
			RequiredReserveCents: prepared.Plan.EstimatedClientAmountCents,
			Currency:             prepared.Plan.Currency,
		},
	)
	capacity := validForwardingCapacityManager(&calls)
	reservation := reserveFunc(
		func(
			_ context.Context,
			input ReservationInput,
		) (ReservationResult, error) {
			calls = append(calls, "reservation")
			return validReservation(input), nil
		},
	)
	forwarder := forwardingExecutorFunc(
		func(
			_ context.Context,
			input ForwardingExecutionInput,
		) (ForwardingExecutionResult, error) {
			calls = append(calls, "forward")
			if input.Prepared.LocalRequestID !=
				prepared.LocalRequestID ||
				input.Reservation.Usage.SelectedRouteID !=
					prepared.Plan.Route.ID {
				t.Fatalf("forward input = %+v", input)
			}
			input.Prepared.Payload[0] = 'X'
			return ForwardingExecutionResult{
				Response: ports.ForwardResponse{
					StatusCode: 200,
					Headers: map[string][]string{
						"Content-Type": []string{"application/json"},
					},
					Body: []byte(`{"ok":true}`),
				},
			}, nil
		},
	)

	stage := mustForwardingStage(
		t,
		capacity,
		reservation,
		forwarder,
	)
	result, err := stage.Execute(
		context.Background(),
		prepared,
		admission,
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	wantCalls := []string{
		"acquire",
		"reservation",
		"forward",
		"release",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
	if !bytes.Equal(
		prepared.Payload,
		[]byte(`{"model":"model-1"}`),
	) {
		t.Fatalf("caller payload mutated: %q", prepared.Payload)
	}
	if result.Response.StatusCode != 200 ||
		!bytes.Equal(
			result.Response.Body,
			[]byte(`{"ok":true}`),
		) {
		t.Fatalf("result = %+v", result)
	}
}

func TestForwardingStageStopsWhenCapacityAcquireFails(t *testing.T) {
	stageError := errors.New("capacity failed")
	var calls []string
	capacity := validForwardingCapacityManager(&calls)
	capacity.acquireFunc = func(
		context.Context,
		ports.RouteCapacityAcquireInput,
	) (ports.RouteCapacityReservation, error) {
		calls = append(calls, "acquire")
		return ports.RouteCapacityReservation{}, stageError
	}

	stage := mustForwardingStage(
		t,
		capacity,
		reserveFunc(
			func(
				context.Context,
				ReservationInput,
			) (ReservationResult, error) {
				t.Fatal("reservation must not be called")
				return ReservationResult{}, nil
			},
		),
		forwardingExecutorFunc(
			func(
				context.Context,
				ForwardingExecutionInput,
			) (ForwardingExecutionResult, error) {
				t.Fatal("forwarder must not be called")
				return ForwardingExecutionResult{}, nil
			},
		),
	)

	prepared := validForwardingPreparedRequest()
	_, err := stage.Execute(
		context.Background(),
		prepared,
		validForwardingAdmission(prepared),
	)
	if !errors.Is(err, stageError) {
		t.Fatalf("error = %v, want capacity error", err)
	}
	if !reflect.DeepEqual(calls, []string{"acquire"}) {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestForwardingStageReleasesWhenAtomicReservationFails(
	t *testing.T,
) {
	stageError := errors.New("reservation failed")
	var calls []string
	capacity := validForwardingCapacityManager(&calls)
	stage := mustForwardingStage(
		t,
		capacity,
		reserveFunc(
			func(
				context.Context,
				ReservationInput,
			) (ReservationResult, error) {
				calls = append(calls, "reservation")
				return ReservationResult{}, stageError
			},
		),
		forwardingExecutorFunc(
			func(
				context.Context,
				ForwardingExecutionInput,
			) (ForwardingExecutionResult, error) {
				t.Fatal("forwarder must not be called")
				return ForwardingExecutionResult{}, nil
			},
		),
	)

	prepared := validForwardingPreparedRequest()
	_, err := stage.Execute(
		context.Background(),
		prepared,
		validForwardingAdmission(prepared),
	)
	if !errors.Is(err, stageError) {
		t.Fatalf("error = %v, want reservation error", err)
	}
	if !reflect.DeepEqual(
		calls,
		[]string{"acquire", "reservation", "release"},
	) {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestForwardingStageReleasesWhenForwardingFails(t *testing.T) {
	stageError := errors.New("forward failed")
	var calls []string
	capacity := validForwardingCapacityManager(&calls)
	stage := mustForwardingStage(
		t,
		capacity,
		reserveFunc(
			func(
				_ context.Context,
				input ReservationInput,
			) (ReservationResult, error) {
				calls = append(calls, "reservation")
				return validReservation(input), nil
			},
		),
		forwardingExecutorFunc(
			func(
				context.Context,
				ForwardingExecutionInput,
			) (ForwardingExecutionResult, error) {
				calls = append(calls, "forward")
				return ForwardingExecutionResult{}, stageError
			},
		),
	)

	prepared := validForwardingPreparedRequest()
	_, err := stage.Execute(
		context.Background(),
		prepared,
		validForwardingAdmission(prepared),
	)
	if !errors.Is(err, stageError) {
		t.Fatalf("error = %v, want forwarding error", err)
	}
	if !reflect.DeepEqual(
		calls,
		[]string{"acquire", "reservation", "forward", "release"},
	) {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestForwardingStageReleaseIgnoresCallerCancellation(
	t *testing.T,
) {
	var calls []string
	ctx, cancel := context.WithCancel(context.Background())
	capacity := validForwardingCapacityManager(&calls)
	capacity.releaseFunc = func(
		releaseCtx context.Context,
		_ ports.RouteCapacityReservation,
	) error {
		calls = append(calls, "release")
		if releaseCtx.Err() != nil {
			t.Fatalf(
				"release context is canceled: %v",
				releaseCtx.Err(),
			)
		}
		return nil
	}
	stage := mustForwardingStage(
		t,
		capacity,
		reserveFunc(
			func(
				_ context.Context,
				input ReservationInput,
			) (ReservationResult, error) {
				calls = append(calls, "reservation")
				return validReservation(input), nil
			},
		),
		forwardingExecutorFunc(
			func(
				context.Context,
				ForwardingExecutionInput,
			) (ForwardingExecutionResult, error) {
				calls = append(calls, "forward")
				cancel()
				return ForwardingExecutionResult{},
					context.Canceled
			},
		),
	)

	prepared := validForwardingPreparedRequest()
	_, err := stage.Execute(
		ctx,
		prepared,
		validForwardingAdmission(prepared),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if calls[len(calls)-1] != "release" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestForwardingStageJoinsReleaseFailure(t *testing.T) {
	forwardError := errors.New("forward failed")
	releaseError := errors.New("release failed")
	capacity := validForwardingCapacityManager(nil)
	capacity.releaseFunc = func(
		context.Context,
		ports.RouteCapacityReservation,
	) error {
		return releaseError
	}

	stage := mustForwardingStage(
		t,
		capacity,
		reserveFunc(
			func(
				_ context.Context,
				input ReservationInput,
			) (ReservationResult, error) {
				return validReservation(input), nil
			},
		),
		forwardingExecutorFunc(
			func(
				context.Context,
				ForwardingExecutionInput,
			) (ForwardingExecutionResult, error) {
				return ForwardingExecutionResult{}, forwardError
			},
		),
	)

	prepared := validForwardingPreparedRequest()
	_, err := stage.Execute(
		context.Background(),
		prepared,
		validForwardingAdmission(prepared),
	)
	if !errors.Is(err, forwardError) ||
		!errors.Is(err, releaseError) {
		t.Fatalf("joined error = %v", err)
	}
}

func TestNewForwardingStageRequiresDependencies(t *testing.T) {
	validCapacity := validForwardingCapacityManager(nil)
	validAtomicReservation := reserveFunc(
		func(
			_ context.Context,
			input ReservationInput,
		) (ReservationResult, error) {
			return validReservation(input), nil
		},
	)
	validAttemptStore := &forwardingAttemptStoreStub{}
	validClock := forwardingStageClock{
		now: validForwardingStageTime(),
	}
	validForwarder := forwardingExecutorFunc(
		func(
			context.Context,
			ForwardingExecutionInput,
		) (ForwardingExecutionResult, error) {
			return ForwardingExecutionResult{}, nil
		},
	)

	tests := []struct {
		name        string
		capacity    ports.RouteCapacityManager
		reservation AtomicReservation
		attempts    ports.ForwardingAttemptStore
		clock       ports.Clock
		forwarder   ForwardingExecutor
	}{
		{
			name:        "capacity",
			reservation: validAtomicReservation,
			attempts:    validAttemptStore,
			clock:       validClock,
			forwarder:   validForwarder,
		},
		{
			name:      "reservation",
			capacity:  validCapacity,
			attempts:  validAttemptStore,
			clock:     validClock,
			forwarder: validForwarder,
		},
		{
			name:        "attempts",
			capacity:    validCapacity,
			reservation: validAtomicReservation,
			clock:       validClock,
			forwarder:   validForwarder,
		},
		{
			name:        "clock",
			capacity:    validCapacity,
			reservation: validAtomicReservation,
			attempts:    validAttemptStore,
			forwarder:   validForwarder,
		},
		{
			name:        "forwarder",
			capacity:    validCapacity,
			reservation: validAtomicReservation,
			attempts:    validAttemptStore,
			clock:       validClock,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewForwardingStage(
				test.capacity,
				test.reservation,
				test.attempts,
				test.clock,
				test.forwarder,
			)
			if !errors.Is(err, ErrDependencyRequired) {
				t.Fatalf(
					"error = %v, want dependency required",
					err,
				)
			}
		})
	}
}

func mustForwardingStage(
	t *testing.T,
	capacity ports.RouteCapacityManager,
	reservation AtomicReservation,
	forwarder ForwardingExecutor,
) *ForwardingStage {
	t.Helper()

	stage, err := NewForwardingStage(
		capacity,
		reservation,
		&forwardingAttemptStoreStub{},
		forwardingStageClock{
			now: validForwardingStageTime(),
		},
		forwarder,
	)
	if err != nil {
		t.Fatalf("NewForwardingStage: %v", err)
	}
	return stage
}

func validForwardingStageTime() time.Time {
	return time.Date(
		2026,
		time.June,
		15,
		12,
		0,
		0,
		0,
		time.UTC,
	)
}

func validForwardingCapacityManager(
	calls *[]string,
) *forwardingCapacityManager {
	record := func(value string) {
		if calls != nil {
			*calls = append(*calls, value)
		}
	}
	return &forwardingCapacityManager{
		acquireFunc: func(
			_ context.Context,
			input ports.RouteCapacityAcquireInput,
		) (ports.RouteCapacityReservation, error) {
			record("acquire")
			return ports.RouteCapacityReservation{
				LocalRequestID: input.LocalRequestID,
				RouteID:        input.Route.ID,
			}, nil
		},
		releaseFunc: func(
			_ context.Context,
			_ ports.RouteCapacityReservation,
		) error {
			record("release")
			return nil
		},
	}
}

func validForwardingPreparedRequest() PreparedRequest {
	input := validInput()
	return PreparedRequest{
		LocalRequestID: input.LocalRequestID,
		IdempotencyKey: cloneStringPointer(input.IdempotencyKey),
		Principal: Principal{
			UserID:               "user-1",
			APIKeyID:             "key-1",
			BillingSubjectUserID: "billing-1",
		},
		APIFamily:             input.APIFamily,
		EndpointKind:          input.EndpointKind,
		ClientModel:           "model-1",
		RequestedCapabilities: validRoutePlan().Route.Capabilities,
		Payload:               cloneBytes(input.Payload),
		Plan:                  validRoutePlan(),
	}
}

func validForwardingAdmission(
	prepared PreparedRequest,
) BillingAdmissionResult {
	return validAdmission(
		BillingAdmissionInput{
			Principal: prepared.Principal,
			RequiredReserveCents: prepared.Plan.
				EstimatedClientAmountCents,
			Currency: prepared.Plan.Currency,
		},
	)
}
