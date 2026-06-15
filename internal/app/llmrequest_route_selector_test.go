package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type llmRequestRouteSelectorClock struct {
	now time.Time
}

func (clock llmRequestRouteSelectorClock) Now() time.Time {
	return clock.now
}

func TestLLMRequestRouteSelectorDelegatesDeterministicSelection(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.June,
		14,
		12,
		0,
		0,
		0,
		time.UTC,
	)
	selector := mustLLMRequestRouteSelector(t, now)
	input := validLLMRequestRouteSelectionInput()
	input.Candidates = []llmrequest.RouteSelectionCandidate{
		validLLMRequestRouteSelectionCandidate(
			"route-b",
			20,
			1,
		),
		validLLMRequestRouteSelectionCandidate(
			"route-a",
			10,
			10,
		),
	}

	result, err := selector.Select(context.Background(), input)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if result.SelectedRouteID != "route-a" {
		t.Fatalf(
			"selected route = %q, want route-a",
			result.SelectedRouteID,
		)
	}
	if len(result.FallbackRouteIDs) != 1 ||
		result.FallbackRouteIDs[0] != "route-b" {
		t.Fatalf(
			"fallback routes = %#v, want route-b",
			result.FallbackRouteIDs,
		)
	}
	if input.Candidates[0].Route.ID != "route-b" ||
		input.Candidates[1].Route.ID != "route-a" {
		t.Fatalf("input candidate order was mutated")
	}
}

func TestLLMRequestRouteSelectorMapsUnsupportedCapability(
	t *testing.T,
) {
	selector := mustLLMRequestRouteSelector(
		t,
		time.Now().UTC(),
	)
	input := validLLMRequestRouteSelectionInput()
	input.RequestedCapabilities.Tools = true
	input.Candidates = []llmrequest.RouteSelectionCandidate{
		validLLMRequestRouteSelectionCandidate(
			"route-a",
			10,
			1,
		),
	}

	_, err := selector.Select(context.Background(), input)
	if !errors.Is(err, llmrequest.ErrUnsupportedCapability) {
		t.Fatalf(
			"error = %v, want unsupported capability",
			err,
		)
	}
}

func TestLLMRequestRouteSelectorMapsNoRouteAvailable(
	t *testing.T,
) {
	selector := mustLLMRequestRouteSelector(
		t,
		time.Now().UTC(),
	)
	input := validLLMRequestRouteSelectionInput()
	candidate := validLLMRequestRouteSelectionCandidate(
		"route-a",
		10,
		1,
	)
	candidate.Preflight.SecretAvailable = false
	input.Candidates = []llmrequest.RouteSelectionCandidate{
		candidate,
	}

	_, err := selector.Select(context.Background(), input)
	if !errors.Is(err, llmrequest.ErrNoRouteAvailable) {
		t.Fatalf(
			"error = %v, want no route available",
			err,
		)
	}
}

func TestLLMRequestRouteSelectorUsesClockForCooldown(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.June,
		14,
		12,
		0,
		0,
		0,
		time.UTC,
	)
	selector := mustLLMRequestRouteSelector(t, now)
	input := validLLMRequestRouteSelectionInput()
	candidate := validLLMRequestRouteSelectionCandidate(
		"route-a",
		10,
		1,
	)
	cooldownUntil := now.Add(time.Minute)
	candidate.Route.CooldownUntil = &cooldownUntil
	input.Candidates = []llmrequest.RouteSelectionCandidate{
		candidate,
	}

	_, err := selector.Select(context.Background(), input)
	if !errors.Is(err, llmrequest.ErrNoRouteAvailable) {
		t.Fatalf(
			"error = %v, want no route available",
			err,
		)
	}
}

func TestLLMRequestRouteSelectorMapsInvalidRouteData(
	t *testing.T,
) {
	selector := mustLLMRequestRouteSelector(
		t,
		time.Now().UTC(),
	)
	input := validLLMRequestRouteSelectionInput()
	candidate := validLLMRequestRouteSelectionCandidate(
		"route-a",
		10,
		1,
	)
	candidate.Route.ResellerID = "other-reseller"
	input.Candidates = []llmrequest.RouteSelectionCandidate{
		candidate,
	}

	_, err := selector.Select(context.Background(), input)
	if !errors.Is(err, llmrequest.ErrStageContractViolation) {
		t.Fatalf(
			"error = %v, want stage contract violation",
			err,
		)
	}
}

func TestLLMRequestRouteSelectorHonorsCanceledContext(
	t *testing.T,
) {
	selector := mustLLMRequestRouteSelector(
		t,
		time.Now().UTC(),
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := selector.Select(
		ctx,
		validLLMRequestRouteSelectionInput(),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestNewLLMRequestRouteSelectorRequiresClock(t *testing.T) {
	_, err := NewLLMRequestRouteSelector(nil)
	if !errors.Is(err, llmrequest.ErrDependencyRequired) {
		t.Fatalf(
			"error = %v, want dependency required",
			err,
		)
	}
}

func mustLLMRequestRouteSelector(
	t *testing.T,
	now time.Time,
) *LLMRequestRouteSelector {
	t.Helper()

	selector, err := NewLLMRequestRouteSelector(
		llmRequestRouteSelectorClock{now: now},
	)
	if err != nil {
		t.Fatalf("NewLLMRequestRouteSelector: %v", err)
	}
	return selector
}

func validLLMRequestRouteSelectionInput() llmrequest.RouteSelectionInput {
	return llmrequest.RouteSelectionInput{
		APIFamily:    domain.APIFamilyOpenAICompatible,
		EndpointKind: domain.EndpointChat,
		ClientModel:  "model-1",
		RequestedCapabilities: domain.CapabilitySet{
			Chat: true,
		},
	}
}

func validLLMRequestRouteSelectionCandidate(
	routeID string,
	estimatedUpstreamCostCents int64,
	priority int,
) llmrequest.RouteSelectionCandidate {
	resellerID := "reseller-" + routeID
	return llmrequest.RouteSelectionCandidate{
		Route: domain.Route{
			ID:                 routeID,
			ResellerID:         resellerID,
			ProviderType:       domain.ProviderOpenAI,
			APIFamily:          domain.APIFamilyOpenAICompatible,
			EndpointKind:       domain.EndpointChat,
			ClientModel:        "model-1",
			ProviderModel:      "model-1",
			ModelRewritePolicy: domain.ModelRewritePolicyNone,
			Enabled:            true,
			Priority:           priority,
			Capabilities: domain.CapabilitySet{
				Chat: true,
			},
		},
		Reseller: domain.Reseller{
			ID:                  resellerID,
			ProviderType:        domain.ProviderOpenAI,
			Enabled:             true,
			BalanceCents:        1000,
			ReservedCents:       100,
			MinimumBalanceCents: 100,
		},
		Preflight: llmrequest.RouteCandidatePreflightResult{
			ForwardingAdapterAvailable:    true,
			SecretAvailable:               true,
			CostAvailable:                 true,
			EstimatedUpstreamCostCents:    estimatedUpstreamCostCents,
			RateLimitAllowed:              true,
			ConcurrencyAllowed:            true,
			ModelIdentifierRewriteAllowed: true,
		},
	}
}
