package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/application/pricing"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type LLMRequestRoutePreflighter struct {
	secrets        ports.SecretPresenceChecker
	pricer         *pricing.PreflightPricer
	capacity       llmrequest.RouteCapacityChecker
	rewriteSupport ports.ModelIdentifierRewriteSupport
}

var _ llmrequest.RouteCandidatePreflighter = (*LLMRequestRoutePreflighter)(nil)

func NewLLMRequestRoutePreflighter(
	secrets ports.SecretPresenceChecker,
	pricer *pricing.PreflightPricer,
	capacity llmrequest.RouteCapacityChecker,
	rewriteSupport ports.ModelIdentifierRewriteSupport,
) (*LLMRequestRoutePreflighter, error) {
	if secrets == nil ||
		pricer == nil ||
		capacity == nil ||
		rewriteSupport == nil {
		return nil, llmrequest.ErrDependencyRequired
	}
	return &LLMRequestRoutePreflighter{
		secrets:        secrets,
		pricer:         pricer,
		capacity:       capacity,
		rewriteSupport: rewriteSupport,
	}, nil
}

func (p *LLMRequestRoutePreflighter) Evaluate(
	ctx context.Context,
	input llmrequest.RouteCandidatePreflightInput,
) (llmrequest.RouteCandidatePreflightResult, error) {
	if p == nil ||
		p.secrets == nil ||
		p.pricer == nil ||
		p.capacity == nil ||
		p.rewriteSupport == nil {
		return llmrequest.RouteCandidatePreflightResult{},
			llmrequest.ErrDependencyRequired
	}
	if ctx == nil {
		return llmrequest.RouteCandidatePreflightResult{}, fmt.Errorf(
			"%w: nil route preflight context",
			llmrequest.ErrInvalidInput,
		)
	}
	if err := ctx.Err(); err != nil {
		return llmrequest.RouteCandidatePreflightResult{}, err
	}
	if err := validateLLMRequestRoutePreflightInput(input); err != nil {
		return llmrequest.RouteCandidatePreflightResult{}, err
	}

	result := llmrequest.RouteCandidatePreflightResult{
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
			return llmrequest.RouteCandidatePreflightResult{},
				fmt.Errorf(
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
			return llmrequest.RouteCandidatePreflightResult{},
				contextError
		}
		return result, nil
	}

	result.CostAvailable = true
	result.EstimatedUsage = priced.EstimatedUsage
	result.EstimatedClientAmountCents =
		priced.EstimatedClientAmountCents
	result.EstimatedUpstreamCostCents =
		priced.EstimatedUpstreamCostCents
	result.Currency = priced.Currency
	result.Confidence = priced.Confidence

	capacity, err := p.capacity.Check(
		ctx,
		llmrequest.RouteCapacityInput{
			Route:          input.Route,
			Reseller:       input.Reseller,
			EstimatedUsage: priced.EstimatedUsage,
		},
	)
	if err != nil {
		return llmrequest.RouteCandidatePreflightResult{},
			fmt.Errorf("check route capacity: %w", err)
	}
	result.RateLimitAllowed = capacity.RateLimitAllowed
	result.ConcurrencyAllowed = capacity.ConcurrencyAllowed
	return result, nil
}

func validateLLMRequestRoutePreflightInput(
	input llmrequest.RouteCandidatePreflightInput,
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
			llmrequest.ErrStageContractViolation,
		)
	}
	if input.Price != nil &&
		input.Price.RouteID != input.Route.ID {
		return fmt.Errorf(
			"%w: route price identity mismatch",
			llmrequest.ErrStageContractViolation,
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
