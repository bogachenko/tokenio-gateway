package llmrequest

import (
	"context"
	"fmt"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/application/pricing"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type LLMRequestRoutePreflighter struct {
	secrets        ports.SecretPresenceChecker
	pricer         *pricing.PreflightPricer
	capacity       ports.RouteCapacityChecker
	adapterSupport ports.ForwardingAdapterSupport
	rewriteSupport ports.ModelIdentifierRewriteSupport
}

var _ RouteCandidatePreflighter = (*LLMRequestRoutePreflighter)(nil)

func NewLLMRequestRoutePreflighter(
	secrets ports.SecretPresenceChecker,
	pricer *pricing.PreflightPricer,
	capacity ports.RouteCapacityChecker,
	adapterSupport ports.ForwardingAdapterSupport,
	rewriteSupport ports.ModelIdentifierRewriteSupport,
) (*LLMRequestRoutePreflighter, error) {
	if secrets == nil ||
		pricer == nil ||
		capacity == nil ||
		adapterSupport == nil ||
		rewriteSupport == nil {
		return nil, ErrDependencyRequired
	}
	return &LLMRequestRoutePreflighter{
		secrets:        secrets,
		pricer:         pricer,
		capacity:       capacity,
		adapterSupport: adapterSupport,
		rewriteSupport: rewriteSupport,
	}, nil
}

func (p *LLMRequestRoutePreflighter) Evaluate(
	ctx context.Context,
	input RouteCandidatePreflightInput,
) (RouteCandidatePreflightResult, error) {
	if p == nil ||
		p.secrets == nil ||
		p.pricer == nil ||
		p.capacity == nil ||
		p.adapterSupport == nil ||
		p.rewriteSupport == nil {
		return RouteCandidatePreflightResult{}, ErrDependencyRequired
	}
	if ctx == nil {
		return RouteCandidatePreflightResult{}, fmt.Errorf(
			"%w: nil route preflight context",
			ErrInvalidInput,
		)
	}
	if err := ctx.Err(); err != nil {
		return RouteCandidatePreflightResult{}, err
	}
	if err := validateLLMRequestRoutePreflightInput(input); err != nil {
		return RouteCandidatePreflightResult{}, err
	}

	result := RouteCandidatePreflightResult{
		ForwardingAdapterAvailable: p.adapterSupport.SupportsForwardingAdapter(
			input.Route.APIFamily,
			input.Route.ProviderType,
			input.Route.EndpointKind,
		),
		ModelIdentifierRewriteAllowed: llmRequestModelRewriteAllowed(
			p.rewriteSupport,
			input.Route,
		),
	}

	if !input.Route.Enabled || !input.Reseller.Enabled {
		return result, nil
	}

	if strings.TrimSpace(input.Reseller.APIKeyEnv) != "" {
		available, err := p.secrets.Exists(
			ctx,
			input.Reseller.APIKeyEnv,
		)
		if err != nil {
			return RouteCandidatePreflightResult{}, fmt.Errorf(
				"check reseller secret presence: %w",
				err,
			)
		}
		result.SecretAvailable = available
	}

	if input.Price == nil {
		return result, nil
	}

	priced, err := p.pricer.Price(
		ctx,
		pricing.PreflightInput{
			Route:                 input.Route,
			Price:                 *input.Price,
			RequestBody:           append([]byte(nil), input.Payload...),
			RequestedCapabilities: input.RequestedCapabilities,
		},
	)
	if err != nil {
		if contextError := ctx.Err(); contextError != nil {
			return RouteCandidatePreflightResult{}, contextError
		}
		return result, nil
	}

	result.CostAvailable = true
	result.EstimatedUsage = priced.EstimatedUsage
	result.EstimatedClientAmountCents = priced.EstimatedClientAmountCents
	result.EstimatedUpstreamCostCents = priced.EstimatedUpstreamCostCents
	result.Currency = priced.Currency
	result.Confidence = priced.Confidence

	capacity, err := p.capacity.Check(
		ctx,
		ports.RouteCapacityCheckInput{
			Route:          input.Route,
			Reseller:       input.Reseller,
			EstimatedUsage: priced.EstimatedUsage,
		},
	)
	if err != nil {
		return RouteCandidatePreflightResult{}, fmt.Errorf("check route capacity: %w", err)
	}
	result.RateLimitAllowed = capacity.RateLimitAllowed
	result.ConcurrencyAllowed = capacity.ConcurrencyAllowed
	return result, nil
}

func validateLLMRequestRoutePreflightInput(
	input RouteCandidatePreflightInput,
) error {
	if strings.TrimSpace(input.Route.ID) == "" ||
		strings.TrimSpace(input.Route.ResellerID) == "" ||
		strings.TrimSpace(input.Reseller.ID) == "" ||
		input.Route.ResellerID != input.Reseller.ID ||
		input.Route.ProviderType == "" ||
		input.Route.ProviderType != input.Reseller.ProviderType ||
		input.Route.APIFamily == "" ||
		input.Route.EndpointKind == "" ||
		strings.TrimSpace(input.Route.ClientModel) == "" ||
		input.Payload == nil {
		return fmt.Errorf(
			"%w: invalid route preflight input",
			ErrStageContractViolation,
		)
	}
	if input.Price != nil && input.Price.RouteID != input.Route.ID {
		return fmt.Errorf(
			"%w: route price identity mismatch",
			ErrStageContractViolation,
		)
	}
	return nil
}

func llmRequestModelRewriteAllowed(
	support ports.ModelIdentifierRewriteSupport,
	route domain.Route,
) bool {
	switch route.ModelRewritePolicy {
	case domain.ModelRewritePolicyNone:
		return true
	case domain.ModelRewritePolicyProviderModel:
		return support.SupportsModelIdentifierRewrite(
			route.APIFamily,
			route.ProviderType,
		)
	default:
		return false
	}
}
