package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestValidateRouteCooldownTransition(t *testing.T) {
	expected, next, event := validRouteCooldownTransition()
	if err := validateRouteCooldownTransition(expected, next, event); err != nil {
		t.Fatalf("valid transition: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*domain.Route, *domain.RouteEvent)
	}{
		{
			name: "missing cooldown",
			mutate: func(route *domain.Route, _ *domain.RouteEvent) {
				route.CooldownUntil = nil
			},
		},
		{
			name: "missing reason",
			mutate: func(route *domain.Route, _ *domain.RouteEvent) {
				route.CooldownReason = ""
			},
		},
		{
			name: "missing last error code",
			mutate: func(route *domain.Route, _ *domain.RouteEvent) {
				route.LastErrorCode = ""
			},
		},
		{
			name: "immutable route mutation",
			mutate: func(route *domain.Route, _ *domain.RouteEvent) {
				route.ProviderModel = "other-model"
			},
		},
		{
			name: "wrong event type",
			mutate: func(_ *domain.Route, event *domain.RouteEvent) {
				event.EventType = domain.RouteEventTypeForwardingFailed
			},
		},
		{
			name: "event route mismatch",
			mutate: func(_ *domain.Route, event *domain.RouteEvent) {
				event.RouteID = "other-route"
			},
		},
		{
			name: "event reason mismatch",
			mutate: func(_ *domain.Route, event *domain.RouteEvent) {
				event.Reason = "other-reason"
			},
		},
		{
			name: "missing local request ID",
			mutate: func(_ *domain.Route, event *domain.RouteEvent) {
				event.LocalRequestID = ""
			},
		},
		{
			name: "event time mismatch",
			mutate: func(_ *domain.Route, event *domain.RouteEvent) {
				event.CreatedAt = event.CreatedAt.Add(time.Second)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutatedNext := next
			mutatedEvent := event
			test.mutate(&mutatedNext, &mutatedEvent)
			err := validateRouteCooldownTransition(
				expected,
				mutatedNext,
				mutatedEvent,
			)
			if !errors.Is(err, ports.ErrStoreContractViolation) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func validRouteCooldownTransition() (
	domain.Route,
	domain.Route,
	domain.RouteEvent,
) {
	createdAt := time.Date(2026, time.June, 17, 15, 0, 0, 0, time.UTC)
	expected := domain.Route{
		ID:                     "route-1",
		ResellerID:             "reseller-1",
		ProviderType:           domain.ProviderOpenAI,
		APIFamily:              domain.APIFamilyOpenAICompatible,
		EndpointKind:           domain.EndpointChat,
		ClientModel:            "model-1",
		ProviderModel:          "model-1",
		ModelRewritePolicy:     domain.ModelRewritePolicyNone,
		Enabled:                true,
		Priority:               10,
		DefaultMaxOutputTokens: 128,
		Capabilities:           domain.CapabilitySet{Chat: true},
		CreatedAt:              createdAt,
		UpdatedAt:              createdAt,
	}
	transitionAt := createdAt.Add(time.Minute)
	cooldownUntil := transitionAt.Add(time.Minute)
	lastErrorAt := transitionAt
	next := expected
	next.CooldownUntil = &cooldownUntil
	next.CooldownReason = "rate_limited"
	next.LastErrorCode = "rate_limited"
	next.LastErrorAt = &lastErrorAt
	next.UpdatedAt = transitionAt
	event := domain.RouteEvent{
		ID:             "route-event-1",
		RouteID:        next.ID,
		ResellerID:     next.ResellerID,
		ProviderType:   next.ProviderType,
		APIFamily:      next.APIFamily,
		EndpointKind:   next.EndpointKind,
		ClientModel:    next.ClientModel,
		EventType:      domain.RouteEventTypeCooldownSet,
		Reason:         next.CooldownReason,
		LocalRequestID: "llmreq-1",
		Metadata: domain.RouteEventMetadata{
			"failure_kind": "rate_limited",
		},
		CreatedAt: transitionAt,
	}
	return expected, next, event
}
