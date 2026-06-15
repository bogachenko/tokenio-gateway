package llmrequest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

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
