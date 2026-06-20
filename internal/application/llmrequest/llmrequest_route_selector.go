package llmrequest

import (
	"context"
	"errors"
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/application/routing"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type LLMRequestRouteSelector struct {
	clock ports.Clock
}

var _ RouteCandidateSelector = (*LLMRequestRouteSelector)(nil)

func NewLLMRequestRouteSelector(
	clock ports.Clock,
) (*LLMRequestRouteSelector, error) {
	if clock == nil {
		return nil, fmt.Errorf(
			"%w: nil route selector clock",
			ErrDependencyRequired,
		)
	}
	return &LLMRequestRouteSelector{
		clock: clock,
	}, nil
}

func (s *LLMRequestRouteSelector) Select(
	ctx context.Context,
	input RouteSelectionInput,
) (RouteSelectionResult, error) {
	if s == nil || s.clock == nil {
		return RouteSelectionResult{}, ErrDependencyRequired
	}
	if ctx == nil {
		return RouteSelectionResult{}, fmt.Errorf(
			"%w: nil route selector context",
			ErrInvalidInput,
		)
	}
	if err := ctx.Err(); err != nil {
		return RouteSelectionResult{}, err
	}

	now := s.clock.Now()
	if now.IsZero() {
		return RouteSelectionResult{}, fmt.Errorf(
			"%w: zero route selector clock",
			ErrStageContractViolation,
		)
	}

	candidates := make(
		[]routing.Candidate,
		len(input.Candidates),
	)
	for index, candidate := range input.Candidates {
		candidates[index] = routing.Candidate{
			Route:                         candidate.Route,
			Reseller:                      candidate.Reseller,
			SecretAvailable:               candidate.Preflight.SecretAvailable,
			CostAvailable:                 candidate.Preflight.CostAvailable,
			ForwardingAdapterAvailable:    candidate.Preflight.ForwardingAdapterAvailable,
			EstimatedUpstreamCostCents:    candidate.Preflight.EstimatedUpstreamCostCents,
			RateLimitAllowed:              candidate.Preflight.RateLimitAllowed,
			ConcurrencyAllowed:            candidate.Preflight.ConcurrencyAllowed,
			ModelIdentifierRewriteAllowed: candidate.Preflight.ModelIdentifierRewriteAllowed,
		}
	}

	selected, err := routing.Select(
		routing.SelectionInput{
			Query: ports.RouteQuery{
				APIFamily:    input.APIFamily,
				EndpointKind: input.EndpointKind,
				ClientModel:  input.ClientModel,
			},
			RequestedCapabilities: input.RequestedCapabilities,
			Candidates:            candidates,
			Now:                   now,
		},
	)
	if err != nil {
		return RouteSelectionResult{},
			mapLLMRequestRouteSelectionError(selected, err)
	}

	fallbackRouteIDs := make(
		[]string,
		len(selected.Fallbacks),
	)
	for index, fallback := range selected.Fallbacks {
		fallbackRouteIDs[index] = fallback.Route.ID
	}
	return RouteSelectionResult{
		SelectedRouteID:  selected.Selected.Route.ID,
		FallbackRouteIDs: fallbackRouteIDs,
	}, nil
}

func mapLLMRequestRouteSelectionError(
	result routing.SelectionResult,
	err error,
) error {
	switch {
	case errors.Is(err, routing.ErrUnknownModel):
		return ErrUnknownModel
	case errors.Is(err, routing.ErrUnsupportedCapability):
		return ErrUnsupportedCapability
	case errors.Is(err, routing.ErrNoRouteAvailable):
		if compatibleRoutesOnlyLackPricing(result.Skipped) {
			return ErrPricingUnavailable
		}
		return ErrNoRouteAvailable
	case errors.Is(err, routing.ErrInvalidSelectionInput),
		errors.Is(err, routing.ErrInvalidRouteData):
		return fmt.Errorf(
			"%w: %v",
			ErrStageContractViolation,
			err,
		)
	default:
		return fmt.Errorf("select route candidate: %w", err)
	}
}

func compatibleRoutesOnlyLackPricing(
	skipped []routing.SkippedRoute,
) bool {
	pricingUnavailable := 0
	for _, skippedRoute := range skipped {
		switch skippedRoute.Reason {
		case routing.SkipReasonMissingCapability:
			continue
		case routing.SkipReasonPricingUnavailable:
			pricingUnavailable++
		default:
			return false
		}
	}
	return pricingUnavailable > 0
}
