package llmrequest

import (
	"context"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func (stub *routeCooldownStoreStub) AppendRouteEvent(
	_ context.Context,
	event domain.RouteEvent,
) error {
	stub.events = append(stub.events, event)
	return nil
}

func (*routeCooldownStoreStub) FindRouteEventByID(
	context.Context,
	string,
) (*domain.RouteEvent, error) {
	return nil, ports.ErrNotFound
}

func (*routeCooldownStoreStub) ListRouteEvents(
	context.Context,
	ports.RouteEventListFilter,
) (ports.Page[domain.RouteEvent], error) {
	return ports.Page[domain.RouteEvent]{}, nil
}

func (stub *routeCooldownStoreStub) CompareAndSwapRouteCooldownExpiryWithEvent(
	_ context.Context,
	_ domain.Route,
	next domain.Route,
	event domain.RouteEvent,
) (domain.Route, error) {
	stub.events = append(stub.events, event)
	return next, nil
}

func TestRouteCandidateSkipReasonDeterministic(t *testing.T) {
	now := validForwardingStageTime()
	candidate := RouteSelectionCandidate{
		Route:    domain.Route{Enabled: true},
		Reseller: domain.Reseller{Enabled: true},
		Price:    &domain.RoutePrice{},
		Preflight: RouteCandidatePreflightResult{
			SecretAvailable:               true,
			CostAvailable:                 true,
			ForwardingAdapterAvailable:    true,
			RateLimitAllowed:              true,
			ConcurrencyAllowed:            true,
			ModelIdentifierRewriteAllowed: true,
		},
	}
	if got := routeCandidateSkipReason(candidate, now); got != routeEventReasonNotSelected {
		t.Fatalf("reason=%q", got)
	}
	candidate.Preflight.RateLimitAllowed = false
	if got := routeCandidateSkipReason(candidate, now); got != routeEventReasonRateLimit {
		t.Fatalf("reason=%q", got)
	}
}

func TestExpiredCooldownTransitionBuildsCorrelatedEvent(t *testing.T) {
	now := validForwardingStageTime()
	expiredAt := now.Add(-time.Second)
	route := validPlannerRoute("route-a", "reseller-a")
	route.CooldownUntil = &expiredAt
	route.CooldownReason = "provider_5xx"
	route.UpdatedAt = now.Add(-time.Minute)

	store := &routeCooldownStoreStub{}
	planner := &RepositoryRoutePlanner{
		operational: store,
		clock:       forwardingStageClock{now: now},
	}
	input := validRoutePlanInput()
	routes, err := planner.expireCooldowns(
		context.Background(),
		input,
		[]domain.Route{route},
	)
	if err != nil {
		t.Fatalf("expireCooldowns: %v", err)
	}
	if routes[0].CooldownUntil != nil || routes[0].CooldownReason != "" {
		t.Fatalf("route=%+v", routes[0])
	}
	if len(store.events) != 1 {
		t.Fatalf("events=%+v", store.events)
	}
	event := store.events[0]
	if event.EventType != domain.RouteEventTypeCooldownExpired ||
		event.LocalRequestID != input.LocalRequestID ||
		event.RouteID != route.ID ||
		event.Reason != routeEventReasonCooldownElapsed {
		t.Fatalf("event=%+v", event)
	}
}
