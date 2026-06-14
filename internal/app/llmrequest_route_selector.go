package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/application/routing"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type LLMRequestRouteSelector struct {
	clock ports.Clock
}

var _ llmrequest.RouteCandidateSelector = (*LLMRequestRouteSelector)(nil)

func NewLLMRequestRouteSelector(
	clock ports.Clock,
) (*LLMRequestRouteSelector, error) {
	if clock == nil {
		return nil, fmt.Errorf(
			"%w: nil route selector clock",
			llmrequest.ErrDependencyRequired,
		)
	}
	return &LLMRequestRouteSelector{
		clock: clock,
	}, nil
}

func (s *LLMRequestRouteSelector) Select(
	ctx context.Context,
	input llmrequest.RouteSelectionInput,
) (llmrequest.RouteSelectionResult, error) {
	if s == nil || s.clock == nil {
		return llmrequest.RouteSelectionResult{},
			llmrequest.ErrDependencyRequired
	}
	if ctx == nil {
		return llmrequest.RouteSelectionResult{}, fmt.Errorf(
			"%w: nil route selector context",
			llmrequest.ErrInvalidInput,
		)
	}
	if err := ctx.Err(); err != nil {
		return llmrequest.RouteSelectionResult{}, err
	}

	now := s.clock.Now()
	if now.IsZero() {
		return llmrequest.RouteSelectionResult{}, fmt.Errorf(
			"%w: zero route selector clock",
			llmrequest.ErrStageContractViolation,
		)
	}

	candidates := make(
		[]routing.Candidate,
		len(input.Candidates),
	)
	for index, candidate := range input.Candidates {
		candidates[index] = routing.Candidate{
			Route:           candidate.Route,
			Reseller:        candidate.Reseller,
			SecretAvailable: candidate.Preflight.SecretAvailable,
			CostAvailable:   candidate.Preflight.CostAvailable,
			ForwardingAdapterAvailable: candidate.Preflight.
				ForwardingAdapterAvailable,
			EstimatedUpstreamCostCents: candidate.Preflight.
				EstimatedUpstreamCostCents,
			RateLimitAllowed: candidate.Preflight.
				RateLimitAllowed,
			ConcurrencyAllowed: candidate.Preflight.
				ConcurrencyAllowed,
			ModelIdentifierRewriteAllowed: candidate.Preflight.
				ModelIdentifierRewriteAllowed,
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
		return llmrequest.RouteSelectionResult{},
			mapLLMRequestRouteSelectionError(err)
	}

	return llmrequest.RouteSelectionResult{
		SelectedRouteID: selected.Selected.Route.ID,
	}, nil
}

func mapLLMRequestRouteSelectionError(err error) error {
	switch {
	case errors.Is(err, routing.ErrUnknownModel):
		return llmrequest.ErrUnknownModel
	case errors.Is(err, routing.ErrUnsupportedCapability):
		return llmrequest.ErrUnsupportedCapability
	case errors.Is(err, routing.ErrNoRouteAvailable):
		return llmrequest.ErrNoRouteAvailable
	case errors.Is(err, routing.ErrInvalidSelectionInput),
		errors.Is(err, routing.ErrInvalidRouteData):
		return fmt.Errorf(
			"%w: %v",
			llmrequest.ErrStageContractViolation,
			err,
		)
	default:
		return fmt.Errorf("select route candidate: %w", err)
	}
}
