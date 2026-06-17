package llmrequest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type retryFailure struct {
	kind         string
	statusCode   int
	attemptState string
	retry        bool
	cause        error
}

func (failure *retryFailure) Error() string                    { return failure.cause.Error() }
func (failure *retryFailure) Unwrap() error                    { return failure.cause }
func (failure *retryFailure) FailureKindValue() string         { return failure.kind }
func (failure *retryFailure) FailureStatusCode() int           { return failure.statusCode }
func (failure *retryFailure) FailureAttemptStateValue() string { return failure.attemptState }
func (failure *retryFailure) FailureRouteRetryCandidate() bool { return failure.retry }

type retryTransferFunc func(
	context.Context,
	RouteReservationTransferInput,
) (RouteReservationTransferResult, error)

func (f retryTransferFunc) Transfer(
	ctx context.Context,
	input RouteReservationTransferInput,
) (RouteReservationTransferResult, error) {
	return f(ctx, input)
}

func TestForwardingStageRetriesOrderedFallback(t *testing.T) {
	prepared := validForwardingPreparedRequest()
	prepared.Plan.Fallbacks = []RouteFallbackPlan{
		retryFallbackPlan(prepared, "route-2", "reseller-2"),
		retryFallbackPlan(prepared, "route-3", "reseller-3"),
	}
	var calls []string
	var attemptRoutes []string
	var reservationIDs []string

	capacity := validForwardingCapacityManager(nil)
	capacity.acquireFunc = func(
		_ context.Context,
		input ports.RouteCapacityAcquireInput,
	) (ports.RouteCapacityReservation, error) {
		calls = append(calls, "acquire:"+input.Route.ID)
		reservationIDs = append(reservationIDs, input.ReservationID)
		return ports.RouteCapacityReservation{
			LocalRequestID: input.LocalRequestID,
			ReservationID:  input.ReservationID,
			RouteID:        input.Route.ID,
		}, nil
	}
	capacity.releaseFunc = func(
		_ context.Context,
		lease ports.RouteCapacityReservation,
	) error {
		calls = append(calls, "release:"+lease.RouteID)
		return nil
	}

	reservation := reserveFunc(func(
		_ context.Context,
		input ReservationInput,
	) (ReservationResult, error) {
		calls = append(calls, "reserve:"+input.Route.ID)
		return validReservation(input), nil
	})
	transfer := retryTransferFunc(func(
		_ context.Context,
		input RouteReservationTransferInput,
	) (RouteReservationTransferResult, error) {
		calls = append(calls, "transfer:"+input.Target.Route.ID)
		usage := input.ExpectedUsage
		usage.SelectedRouteID = input.Target.Route.ID
		usage.SelectedResellerID = input.Target.Reseller.ID
		usage.ProviderType = input.Target.Route.ProviderType
		usage.ProviderModel = input.Target.Route.ProviderModel
		usage.BillingModel = input.Target.BillingModel
		usage.EstimatedUsage = input.Target.EstimatedUsage
		usage.EstimatedClientAmountCents = input.Target.EstimatedClientAmountCents
		usage.EstimatedUpstreamCostCents = input.Target.EstimatedUpstreamCostCents
		usage.Currency = input.Target.Currency
		return RouteReservationTransferResult{
			Usage:            usage,
			ReservedReseller: input.Target.Reseller,
		}, nil
	})
	attempts := &forwardingAttemptStoreStub{
		startFunc: func(
			_ context.Context,
			attempt domain.ForwardingAttempt,
		) (domain.ForwardingAttempt, error) {
			calls = append(calls, fmt.Sprintf(
				"start:%d:%s",
				attempt.AttemptNumber,
				attempt.RouteID,
			))
			attemptRoutes = append(attemptRoutes, attempt.RouteID)
			return attempt, nil
		},
		completeFunc: func(
			_ context.Context,
			attempt domain.ForwardingAttempt,
		) (domain.ForwardingAttempt, error) {
			calls = append(calls, fmt.Sprintf(
				"complete:%d:%s",
				attempt.AttemptNumber,
				attempt.Status,
			))
			return attempt, nil
		},
	}
	forwardCalls := 0
	forwarder := forwardingExecutorFunc(func(
		_ context.Context,
		input ForwardingExecutionInput,
	) (ForwardingExecutionResult, error) {
		calls = append(calls, "forward:"+input.Prepared.Plan.Route.ID)
		forwardCalls++
		if forwardCalls == 1 {
			return ForwardingExecutionResult{}, &retryFailure{
				kind:         "rate_limited",
				statusCode:   429,
				attemptState: string(domain.ForwardingAttemptStateResponseReceived),
				retry:        true,
				cause:        errors.New("rate limited"),
			}
		}
		return ForwardingExecutionResult{
			Response: ports.ForwardResponse{
				StatusCode: 200,
				Body:       []byte(`{"ok":true}`),
			},
		}, nil
	})

	stage, err := NewForwardingStage(
		capacity,
		reservation,
		transfer,
		attempts,
		forwardingStageClock{now: validForwardingStageTime()},
		forwarder,
		mustValidRoutingPolicy(t),
		immediateRetryWaiter{},
	)
	if err != nil {
		t.Fatalf("NewForwardingStage: %v", err)
	}
	result, err := stage.Execute(
		context.Background(),
		prepared,
		validForwardingAdmission(prepared),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Reserved.Prepared.Plan.Route.ID != "route-2" ||
		result.Reserved.Reservation.Usage.SelectedRouteID != "route-2" {
		t.Fatalf("result = %+v", result)
	}
	if !reflect.DeepEqual(
		attemptRoutes,
		[]string{prepared.Plan.Route.ID, "route-2"},
	) {
		t.Fatalf("attempt routes = %#v", attemptRoutes)
	}
	if !reflect.DeepEqual(
		reservationIDs,
		[]string{
			prepared.LocalRequestID + ":attempt:1",
			prepared.LocalRequestID + ":attempt:2",
		},
	) {
		t.Fatalf("reservation IDs = %#v", reservationIDs)
	}
	wantCalls := []string{
		"acquire:" + prepared.Plan.Route.ID,
		"reserve:" + prepared.Plan.Route.ID,
		"start:1:" + prepared.Plan.Route.ID,
		"forward:" + prepared.Plan.Route.ID,
		"complete:1:failed",
		"release:" + prepared.Plan.Route.ID,
		"acquire:route-2",
		"transfer:route-2",
		"start:2:route-2",
		"forward:route-2",
		"complete:2:succeeded",
		"release:route-2",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestForwardingStageSkipsCapacityUnavailableCandidate(t *testing.T) {
	prepared := validForwardingPreparedRequest()
	prepared.Plan.Fallbacks = []RouteFallbackPlan{
		retryFallbackPlan(prepared, "route-2", "reseller-2"),
	}
	capacity := validForwardingCapacityManager(nil)
	acquires := 0
	capacity.acquireFunc = func(
		_ context.Context,
		input ports.RouteCapacityAcquireInput,
	) (ports.RouteCapacityReservation, error) {
		acquires++
		if acquires == 1 {
			return ports.RouteCapacityReservation{}, ports.ErrRouteCapacityUnavailable
		}
		return ports.RouteCapacityReservation{
			LocalRequestID: input.LocalRequestID,
			ReservationID:  input.ReservationID,
			RouteID:        input.Route.ID,
		}, nil
	}
	reservedRoute := ""
	stage, err := NewForwardingStage(
		capacity,
		reserveFunc(func(
			_ context.Context,
			input ReservationInput,
		) (ReservationResult, error) {
			reservedRoute = input.Route.ID
			return validReservation(input), nil
		}),
		retryTransferFunc(func(
			context.Context,
			RouteReservationTransferInput,
		) (RouteReservationTransferResult, error) {
			t.Fatal("transfer must not run before initial reserve")
			return RouteReservationTransferResult{}, nil
		}),
		&forwardingAttemptStoreStub{},
		forwardingStageClock{now: validForwardingStageTime()},
		forwardingExecutorFunc(func(
			_ context.Context,
			input ForwardingExecutionInput,
		) (ForwardingExecutionResult, error) {
			return ForwardingExecutionResult{
				Response: ports.ForwardResponse{StatusCode: 200},
			}, nil
		}),
		mustValidRoutingPolicy(t),
		immediateRetryWaiter{},
	)
	if err != nil {
		t.Fatalf("NewForwardingStage: %v", err)
	}
	result, err := stage.Execute(
		context.Background(),
		prepared,
		validForwardingAdmission(prepared),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if reservedRoute != "route-2" ||
		result.Reserved.Prepared.Plan.Route.ID != "route-2" {
		t.Fatalf("reserved=%q result=%+v", reservedRoute, result)
	}
}

func TestForwardingStageReturnsRouteUnavailableAfterCapacityFallbacksExhausted(
	t *testing.T,
) {
	prepared := validForwardingPreparedRequest()
	prepared.Plan.Fallbacks = []RouteFallbackPlan{
		retryFallbackPlan(prepared, "route-2", "reseller-2"),
	}

	capacity := validForwardingCapacityManager(nil)
	acquiredRoutes := []string{}
	capacity.acquireFunc = func(
		_ context.Context,
		input ports.RouteCapacityAcquireInput,
	) (ports.RouteCapacityReservation, error) {
		acquiredRoutes = append(acquiredRoutes, input.Route.ID)
		return ports.RouteCapacityReservation{},
			ports.ErrRouteCapacityUnavailable
	}

	reserveCalled := false
	forwardCalled := false
	stage, err := NewForwardingStage(
		capacity,
		reserveFunc(func(
			context.Context,
			ReservationInput,
		) (ReservationResult, error) {
			reserveCalled = true
			return ReservationResult{}, nil
		}),
		retryTransferFunc(func(
			context.Context,
			RouteReservationTransferInput,
		) (RouteReservationTransferResult, error) {
			t.Fatal("transfer must not run without a reservation")
			return RouteReservationTransferResult{}, nil
		}),
		&forwardingAttemptStoreStub{},
		forwardingStageClock{now: validForwardingStageTime()},
		forwardingExecutorFunc(func(
			context.Context,
			ForwardingExecutionInput,
		) (ForwardingExecutionResult, error) {
			forwardCalled = true
			return ForwardingExecutionResult{}, nil
		}),
		mustValidRoutingPolicy(t),
		immediateRetryWaiter{},
	)
	if err != nil {
		t.Fatalf("NewForwardingStage: %v", err)
	}

	_, err = stage.Execute(
		context.Background(),
		prepared,
		validForwardingAdmission(prepared),
	)
	if !errors.Is(err, ErrRouteUnavailable) {
		t.Fatalf("error = %v, want route unavailable", err)
	}
	if !errors.Is(err, ports.ErrRouteCapacityUnavailable) {
		t.Fatalf("error = %v, want capacity cause", err)
	}
	if !reflect.DeepEqual(
		acquiredRoutes,
		[]string{prepared.Plan.Route.ID, "route-2"},
	) {
		t.Fatalf("acquired routes = %#v", acquiredRoutes)
	}
	if reserveCalled || forwardCalled {
		t.Fatalf(
			"reserve called = %v, forward called = %v",
			reserveCalled,
			forwardCalled,
		)
	}
}

func TestForwardingStageDoesNotRetrySentNoResponse(t *testing.T) {
	prepared := validForwardingPreparedRequest()
	prepared.Plan.Fallbacks = []RouteFallbackPlan{
		retryFallbackPlan(prepared, "route-2", "reseller-2"),
	}
	capacity := validForwardingCapacityManager(nil)
	acquires := 0
	capacity.acquireFunc = func(
		_ context.Context,
		input ports.RouteCapacityAcquireInput,
	) (ports.RouteCapacityReservation, error) {
		acquires++
		return ports.RouteCapacityReservation{
			LocalRequestID: input.LocalRequestID,
			ReservationID:  input.ReservationID,
			RouteID:        input.Route.ID,
		}, nil
	}
	forwardErr := &retryFailure{
		kind:         "unavailable",
		statusCode:   0,
		attemptState: string(domain.ForwardingAttemptStateSentNoResponse),
		retry:        true,
		cause:        errors.New("connection lost"),
	}
	stage, err := NewForwardingStage(
		capacity,
		reserveFunc(func(
			_ context.Context,
			input ReservationInput,
		) (ReservationResult, error) {
			return validReservation(input), nil
		}),
		retryTransferFunc(func(
			context.Context,
			RouteReservationTransferInput,
		) (RouteReservationTransferResult, error) {
			t.Fatal("transfer must not run")
			return RouteReservationTransferResult{}, nil
		}),
		&forwardingAttemptStoreStub{},
		forwardingStageClock{now: validForwardingStageTime()},
		forwardingExecutorFunc(func(
			context.Context,
			ForwardingExecutionInput,
		) (ForwardingExecutionResult, error) {
			return ForwardingExecutionResult{}, forwardErr
		}),
		mustValidRoutingPolicy(t),
		immediateRetryWaiter{},
	)
	if err != nil {
		t.Fatalf("NewForwardingStage: %v", err)
	}
	_, err = stage.Execute(
		context.Background(),
		prepared,
		validForwardingAdmission(prepared),
	)
	if !errors.Is(err, forwardErr) {
		t.Fatalf("error = %v", err)
	}
	if acquires != 1 {
		t.Fatalf("acquires = %d, want 1", acquires)
	}
}

func retryFallbackPlan(
	prepared PreparedRequest,
	routeID string,
	resellerID string,
) RouteFallbackPlan {
	route := prepared.Plan.Route
	route.ID = routeID
	route.ResellerID = resellerID
	route.ProviderModel = "provider-" + routeID
	reseller := prepared.Plan.Reseller
	reseller.ID = resellerID
	reseller.APIKeyEnv = "KEY_" + resellerID
	return RouteFallbackPlan{
		Route:                      route,
		Reseller:                   reseller,
		Price:                      prepared.Plan.Price,
		BillingModel:               prepared.Plan.BillingModel,
		EstimatedUsage:             prepared.Plan.EstimatedUsage,
		EstimatedClientAmountCents: prepared.Plan.EstimatedClientAmountCents,
		EstimatedUpstreamCostCents: prepared.Plan.EstimatedUpstreamCostCents,
		Currency:                   prepared.Plan.Currency,
		Confidence:                 prepared.Plan.Confidence,
	}
}

func TestForwardingStageStopsAfterConfiguredMaximumCandidates(
	t *testing.T,
) {
	prepared := validForwardingPreparedRequest()
	prepared.Plan.Fallbacks = []RouteFallbackPlan{
		retryFallbackPlan(prepared, "route-2", "reseller-2"),
		retryFallbackPlan(prepared, "route-3", "reseller-3"),
	}

	var acquiredRoutes []string
	capacity := validForwardingCapacityManager(nil)
	capacity.acquireFunc = func(
		_ context.Context,
		input ports.RouteCapacityAcquireInput,
	) (ports.RouteCapacityReservation, error) {
		acquiredRoutes = append(acquiredRoutes, input.Route.ID)
		return ports.RouteCapacityReservation{},
			ports.ErrRouteCapacityUnavailable
	}

	policyInput := validRoutingPolicyInput()
	policyInput.UpstreamMaxAttempts = 2
	policy, err := NewRoutingPolicy(policyInput)
	if err != nil {
		t.Fatalf("NewRoutingPolicy: %v", err)
	}

	stage, err := NewForwardingStage(
		capacity,
		reserveFunc(func(
			context.Context,
			ReservationInput,
		) (ReservationResult, error) {
			t.Fatal("reservation must not run without capacity")
			return ReservationResult{}, nil
		}),
		retryTransferFunc(func(
			context.Context,
			RouteReservationTransferInput,
		) (RouteReservationTransferResult, error) {
			t.Fatal("transfer must not run without reservation")
			return RouteReservationTransferResult{}, nil
		}),
		&forwardingAttemptStoreStub{},
		forwardingStageClock{now: validForwardingStageTime()},
		forwardingExecutorFunc(func(
			context.Context,
			ForwardingExecutionInput,
		) (ForwardingExecutionResult, error) {
			t.Fatal("forwarder must not run without capacity")
			return ForwardingExecutionResult{}, nil
		}),
		policy,
		immediateRetryWaiter{},
	)
	if err != nil {
		t.Fatalf("NewForwardingStage: %v", err)
	}

	_, err = stage.Execute(
		context.Background(),
		prepared,
		validForwardingAdmission(prepared),
	)
	if !errors.Is(err, ErrRouteUnavailable) {
		t.Fatalf("error = %v, want route unavailable", err)
	}
	if !reflect.DeepEqual(
		acquiredRoutes,
		[]string{prepared.Plan.Route.ID, "route-2"},
	) {
		t.Fatalf("acquired routes = %#v", acquiredRoutes)
	}
}

func TestForwardingStageUsesSeparateTimeoutForEachUpstreamAttempt(
	t *testing.T,
) {
	prepared := validForwardingPreparedRequest()
	timeout := 50 * time.Millisecond

	policyInput := validRoutingPolicyInput()
	policyInput.UpstreamTimeout = timeout
	policy, err := NewRoutingPolicy(policyInput)
	if err != nil {
		t.Fatalf("NewRoutingPolicy: %v", err)
	}

	var observedDeadline time.Time
	stage, err := NewForwardingStage(
		validForwardingCapacityManager(nil),
		reserveFunc(func(
			_ context.Context,
			input ReservationInput,
		) (ReservationResult, error) {
			return validReservation(input), nil
		}),
		&routeReservationTransferStub{},
		&forwardingAttemptStoreStub{},
		forwardingStageClock{now: validForwardingStageTime()},
		forwardingExecutorFunc(func(
			ctx context.Context,
			_ ForwardingExecutionInput,
		) (ForwardingExecutionResult, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("upstream attempt context has no deadline")
			}
			observedDeadline = deadline
			return ForwardingExecutionResult{
				Response: ports.ForwardResponse{
					StatusCode: 200,
					Body:       []byte(`{"ok":true}`),
				},
			}, nil
		}),
		policy,
		immediateRetryWaiter{},
	)
	if err != nil {
		t.Fatalf("NewForwardingStage: %v", err)
	}

	_, err = stage.Execute(
		context.Background(),
		prepared,
		validForwardingAdmission(prepared),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	remaining := time.Until(observedDeadline)
	if remaining <= 0 || remaining > timeout {
		t.Fatalf(
			"attempt deadline remaining = %s, want > 0 and <= %s",
			remaining,
			timeout,
		)
	}
}

type recordingRetryWaiter struct {
	delays []time.Duration
	err    error
}

func (waiter *recordingRetryWaiter) Wait(
	_ context.Context,
	delay time.Duration,
) error {
	waiter.delays = append(waiter.delays, delay)
	return waiter.err
}

type retryAfterFailure struct {
	*retryFailure
	present bool
	delay   time.Duration
	at      time.Time
}

func (failure *retryAfterFailure) FailureRetryAfterPresent() bool {
	return failure.present
}

func (failure *retryAfterFailure) FailureRetryAfterDelay() time.Duration {
	return failure.delay
}

func (failure *retryAfterFailure) FailureRetryAfterTime() time.Time {
	return failure.at
}

func TestForwardingStageUsesBoundedExponentialBackoff(t *testing.T) {
	prepared := validForwardingPreparedRequest()
	prepared.Plan.Fallbacks = []RouteFallbackPlan{
		retryFallbackPlan(prepared, "route-2", "reseller-2"),
		retryFallbackPlan(prepared, "route-3", "reseller-3"),
	}

	waiter := &recordingRetryWaiter{}
	forwardCalls := 0
	stage := mustRetryTestForwardingStage(
		t,
		prepared,
		waiter,
		forwardingExecutorFunc(func(
			_ context.Context,
			_ ForwardingExecutionInput,
		) (ForwardingExecutionResult, error) {
			forwardCalls++
			if forwardCalls < 3 {
				return ForwardingExecutionResult{}, &retryFailure{
					kind:       "server_error",
					statusCode: 503,
					attemptState: string(
						domain.ForwardingAttemptStateResponseReceived,
					),
					retry: true,
					cause: errors.New("retryable"),
				}
			}
			return ForwardingExecutionResult{
				Response: ports.ForwardResponse{StatusCode: 200},
			}, nil
		}),
		mustValidRoutingPolicy(t),
	)

	_, err := stage.Execute(
		context.Background(),
		prepared,
		validForwardingAdmission(prepared),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !reflect.DeepEqual(
		waiter.delays,
		[]time.Duration{time.Second, 2 * time.Second},
	) {
		t.Fatalf("delays = %#v", waiter.delays)
	}
}

func TestForwardingStagePrefersAndBoundsRetryAfter(t *testing.T) {
	prepared := validForwardingPreparedRequest()
	prepared.Plan.Fallbacks = []RouteFallbackPlan{
		retryFallbackPlan(prepared, "route-2", "reseller-2"),
	}

	policyInput := validRoutingPolicyInput()
	policyInput.UpstreamMaxBackoff = 2 * time.Second
	policyInput.RateLimitMaxWait = 500 * time.Millisecond
	policy, err := NewRoutingPolicy(policyInput)
	if err != nil {
		t.Fatalf("NewRoutingPolicy: %v", err)
	}

	waiter := &recordingRetryWaiter{}
	forwardCalls := 0
	stage := mustRetryTestForwardingStage(
		t,
		prepared,
		waiter,
		forwardingExecutorFunc(func(
			_ context.Context,
			_ ForwardingExecutionInput,
		) (ForwardingExecutionResult, error) {
			forwardCalls++
			if forwardCalls == 1 {
				return ForwardingExecutionResult{}, &retryAfterFailure{
					retryFailure: &retryFailure{
						kind:       "rate_limited",
						statusCode: 429,
						attemptState: string(
							domain.ForwardingAttemptStateResponseReceived,
						),
						retry: true,
						cause: errors.New("rate limited"),
					},
					present: true,
					delay:   10 * time.Second,
				}
			}
			return ForwardingExecutionResult{
				Response: ports.ForwardResponse{StatusCode: 200},
			}, nil
		}),
		policy,
	)

	_, err = stage.Execute(
		context.Background(),
		prepared,
		validForwardingAdmission(prepared),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !reflect.DeepEqual(
		waiter.delays,
		[]time.Duration{500 * time.Millisecond},
	) {
		t.Fatalf("delays = %#v", waiter.delays)
	}
}

func TestForwardingStageHonorsExplicitZeroRetryAfter(t *testing.T) {
	prepared := validForwardingPreparedRequest()
	prepared.Plan.Fallbacks = []RouteFallbackPlan{
		retryFallbackPlan(prepared, "route-2", "reseller-2"),
	}

	waiter := &recordingRetryWaiter{}
	forwardCalls := 0
	stage := mustRetryTestForwardingStage(
		t,
		prepared,
		waiter,
		forwardingExecutorFunc(func(
			_ context.Context,
			_ ForwardingExecutionInput,
		) (ForwardingExecutionResult, error) {
			forwardCalls++
			if forwardCalls == 1 {
				return ForwardingExecutionResult{}, &retryAfterFailure{
					retryFailure: &retryFailure{
						kind:       "rate_limited",
						statusCode: 429,
						attemptState: string(
							domain.ForwardingAttemptStateResponseReceived,
						),
						retry: true,
						cause: errors.New("rate limited"),
					},
					present: true,
				}
			}
			return ForwardingExecutionResult{
				Response: ports.ForwardResponse{StatusCode: 200},
			}, nil
		}),
		mustValidRoutingPolicy(t),
	)

	_, err := stage.Execute(
		context.Background(),
		prepared,
		validForwardingAdmission(prepared),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(waiter.delays) != 0 {
		t.Fatalf("explicit zero retry-after waited: %#v", waiter.delays)
	}
}

func TestForwardingStageStopsWhenRetryWaitIsCancelled(t *testing.T) {
	prepared := validForwardingPreparedRequest()
	prepared.Plan.Fallbacks = []RouteFallbackPlan{
		retryFallbackPlan(prepared, "route-2", "reseller-2"),
	}

	waiter := &recordingRetryWaiter{err: context.Canceled}
	forwardCalls := 0
	stage := mustRetryTestForwardingStage(
		t,
		prepared,
		waiter,
		forwardingExecutorFunc(func(
			_ context.Context,
			_ ForwardingExecutionInput,
		) (ForwardingExecutionResult, error) {
			forwardCalls++
			return ForwardingExecutionResult{}, &retryFailure{
				kind:       "server_error",
				statusCode: 503,
				attemptState: string(
					domain.ForwardingAttemptStateResponseReceived,
				),
				retry: true,
				cause: errors.New("retryable"),
			}
		}),
		mustValidRoutingPolicy(t),
	)

	_, err := stage.Execute(
		context.Background(),
		prepared,
		validForwardingAdmission(prepared),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if forwardCalls != 1 {
		t.Fatalf("forward calls = %d, want 1", forwardCalls)
	}
}

func mustRetryTestForwardingStage(
	t *testing.T,
	prepared PreparedRequest,
	waiter RetryWaiter,
	forwarder ForwardingExecutor,
	policy RoutingPolicy,
) *ForwardingStage {
	t.Helper()

	capacity := validForwardingCapacityManager(nil)
	reservation := reserveFunc(func(
		_ context.Context,
		input ReservationInput,
	) (ReservationResult, error) {
		return validReservation(input), nil
	})
	transfer := retryTransferFunc(func(
		_ context.Context,
		input RouteReservationTransferInput,
	) (RouteReservationTransferResult, error) {
		usage := input.ExpectedUsage
		usage.SelectedRouteID = input.Target.Route.ID
		usage.SelectedResellerID = input.Target.Reseller.ID
		usage.ProviderType = input.Target.Route.ProviderType
		usage.ProviderModel = input.Target.Route.ProviderModel
		usage.BillingModel = input.Target.BillingModel
		usage.EstimatedUsage = input.Target.EstimatedUsage
		usage.EstimatedClientAmountCents =
			input.Target.EstimatedClientAmountCents
		usage.EstimatedUpstreamCostCents =
			input.Target.EstimatedUpstreamCostCents
		usage.Currency = input.Target.Currency
		return RouteReservationTransferResult{
			Usage:            usage,
			ReservedReseller: input.Target.Reseller,
		}, nil
	})

	stage, err := NewForwardingStage(
		capacity,
		reservation,
		transfer,
		&forwardingAttemptStoreStub{},
		forwardingStageClock{now: validForwardingStageTime()},
		forwarder,
		policy,
		waiter,
	)
	if err != nil {
		t.Fatalf("NewForwardingStage: %v", err)
	}
	return stage
}
