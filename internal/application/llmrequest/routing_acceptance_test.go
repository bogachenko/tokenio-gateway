package llmrequest

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestRoutingAcceptanceSuccessfulExecutionEventTrail(t *testing.T) {
	prepared := validForwardingPreparedRequest()
	cooldowns := &routeCooldownStoreStub{}

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
		cooldowns,
		forwardingStageClock{now: validForwardingStageTime()},
		forwardingExecutorFunc(func(
			context.Context,
			ForwardingExecutionInput,
		) (ForwardingExecutionResult, error) {
			return ForwardingExecutionResult{
				Response: ports.ForwardResponse{
					StatusCode: 200,
					Body:       []byte(`{"ok":true}`),
				},
			}, nil
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
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	assertRouteEventTrail(
		t,
		cooldowns.events,
		[]expectedRouteEvent{
			{
				eventType:     domain.RouteEventTypeForwardingStarted,
				reason:        routeEventReasonAttemptStarted,
				routeID:       prepared.Plan.Route.ID,
				attemptNumber: 1,
			},
			{
				eventType:     domain.RouteEventTypeForwardingSucceeded,
				reason:        routeEventReasonSucceeded,
				routeID:       prepared.Plan.Route.ID,
				attemptNumber: 1,
			},
		},
		prepared.LocalRequestID,
	)
}

func TestRoutingAcceptanceRetryFallbackEventTrail(t *testing.T) {
	prepared := validForwardingPreparedRequest()
	prepared.Plan.Fallbacks = []RouteFallbackPlan{
		retryFallbackPlan(prepared, "route-2", "reseller-2"),
	}

	cooldowns := &routeCooldownStoreStub{}
	cooldowns.fn = func(
		_ context.Context,
		_ domain.Route,
		next domain.Route,
		event domain.RouteEvent,
	) (domain.Route, error) {
		cooldowns.events = append(cooldowns.events, event)
		return next, nil
	}

	forwardCalls := 0
	forwarder := forwardingExecutorFunc(func(
		_ context.Context,
		_ ForwardingExecutionInput,
	) (ForwardingExecutionResult, error) {
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
		validForwardingCapacityManager(nil),
		reserveFunc(func(
			_ context.Context,
			input ReservationInput,
		) (ReservationResult, error) {
			return validReservation(input), nil
		}),
		transfer,
		&forwardingAttemptStoreStub{},
		cooldowns,
		forwardingStageClock{now: validForwardingStageTime()},
		forwarder,
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
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	assertRouteEventTrail(
		t,
		cooldowns.events,
		[]expectedRouteEvent{
			{
				eventType:     domain.RouteEventTypeForwardingStarted,
				reason:        routeEventReasonAttemptStarted,
				routeID:       prepared.Plan.Route.ID,
				attemptNumber: 1,
			},
			{
				eventType:     domain.RouteEventTypeForwardingFailed,
				reason:        "rate_limited",
				routeID:       prepared.Plan.Route.ID,
				attemptNumber: 1,
			},
			{
				eventType:     domain.RouteEventTypeCooldownSet,
				reason:        "rate_limited",
				routeID:       prepared.Plan.Route.ID,
				attemptNumber: 1,
			},
			{
				eventType:     domain.RouteEventTypeRetryScheduled,
				reason:        routeEventReasonRetryScheduled,
				routeID:       prepared.Plan.Route.ID,
				attemptNumber: 1,
			},
			{
				eventType:     domain.RouteEventTypeForwardingStarted,
				reason:        routeEventReasonAttemptStarted,
				routeID:       "route-2",
				attemptNumber: 2,
			},
			{
				eventType:     domain.RouteEventTypeForwardingSucceeded,
				reason:        routeEventReasonSucceeded,
				routeID:       "route-2",
				attemptNumber: 2,
			},
		},
		prepared.LocalRequestID,
	)
}

type expectedRouteEvent struct {
	eventType     domain.RouteEventType
	reason        string
	routeID       string
	attemptNumber int
}

func assertRouteEventTrail(
	t *testing.T,
	events []domain.RouteEvent,
	expected []expectedRouteEvent,
	localRequestID string,
) {
	t.Helper()

	if len(events) != len(expected) {
		t.Fatalf("event count=%d want=%d events=%+v", len(events), len(expected), events)
	}

	for index, want := range expected {
		event := events[index]
		if event.EventType != want.eventType ||
			event.Reason != want.reason ||
			event.RouteID != want.routeID ||
			event.LocalRequestID != localRequestID {
			t.Fatalf(
				"event[%d]=%+v want type=%q reason=%q route=%q request=%q",
				index,
				event,
				want.eventType,
				want.reason,
				want.routeID,
				localRequestID,
			)
		}

		gotAttempt, ok := event.Metadata["attempt_number"]
		if !ok {
			t.Fatalf("event[%d] missing attempt_number: %+v", index, event)
		}
		if !reflect.DeepEqual(gotAttempt, want.attemptNumber) {
			t.Fatalf(
				"event[%d] attempt_number=%v want=%d",
				index,
				gotAttempt,
				want.attemptNumber,
			)
		}

		forbiddenMetadataKeys := []string{
			"api_key",
			"authorization",
			"jwt",
			"payload",
			"request_body",
			"response_body",
			"secret",
		}
		for _, key := range forbiddenMetadataKeys {
			if _, exists := event.Metadata[key]; exists {
				t.Fatalf(
					"event[%d] contains forbidden metadata key %q",
					index,
					key,
				)
			}
		}
	}
}
