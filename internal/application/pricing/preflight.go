package pricing

import (
	"context"
	"fmt"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type PreflightInput struct {
	Route                 domain.Route
	Price                 domain.RoutePrice
	RequestBody           []byte
	RequestedCapabilities domain.CapabilitySet

	InputMode  InputPricingMode
	Modalities InputModalities
}

type PreflightResult struct {
	EstimatedUsage domain.TokenUsage

	EstimatedUpstreamCostCents int64
	EstimatedClientAmountCents int64

	Currency   string
	Confidence string
}

type PreflightPricer struct {
	estimator  ports.TokenEstimator
	calculator *Calculator
}

func NewPreflightPricer(estimator ports.TokenEstimator, calculator *Calculator) (*PreflightPricer, error) {
	if estimator == nil {
		return nil, fmt.Errorf("%w: nil token estimator", ErrInvalidPricingInput)
	}
	if calculator == nil {
		return nil, fmt.Errorf("%w: nil calculator", ErrInvalidPricingInput)
	}
	return &PreflightPricer{estimator: estimator, calculator: calculator}, nil
}

func (p *PreflightPricer) Price(ctx context.Context, input PreflightInput) (PreflightResult, error) {
	if p == nil || p.estimator == nil || p.calculator == nil {
		return PreflightResult{}, fmt.Errorf("%w: nil preflight pricer", ErrInvalidPricingInput)
	}
	if err := validateRouteAndPrice(input.Route, input.Price); err != nil {
		return PreflightResult{}, err
	}
	if input.RequestBody == nil {
		return PreflightResult{}, fmt.Errorf("%w: request body is nil", ErrInvalidPricingInput)
	}
	estimate, err := p.estimator.Estimate(ctx, ports.TokenEstimateRequest{
		APIFamily:              input.Route.APIFamily,
		EndpointKind:           input.Route.EndpointKind,
		ClientModel:            input.Route.ClientModel,
		RequestBody:            append([]byte(nil), input.RequestBody...),
		DefaultMaxOutputTokens: input.Route.DefaultMaxOutputTokens,
		RequestedCapabilities:  input.RequestedCapabilities,
	})
	if err != nil {
		return PreflightResult{}, fmt.Errorf("%w: estimate usage: %w", ErrPricingUnavailable, err)
	}
	if err := ValidateUsage(estimate.Usage); err != nil {
		return PreflightResult{}, err
	}
	if strings.TrimSpace(estimate.Confidence) == "" {
		return PreflightResult{}, fmt.Errorf("%w: estimator confidence is blank", ErrPricingUnavailable)
	}
	calculation, err := p.calculator.CalculateEstimate(EstimateCalculationInput{
		Usage:      estimate.Usage,
		Price:      input.Price,
		InputMode:  input.InputMode,
		Modalities: input.Modalities,
	})
	if err != nil {
		return PreflightResult{}, err
	}
	return PreflightResult{
		EstimatedUsage:             calculation.Usage,
		EstimatedUpstreamCostCents: calculation.UpstreamCostCents,
		EstimatedClientAmountCents: calculation.ClientAmountCents,
		Currency:                   calculation.Currency,
		Confidence:                 estimate.Confidence,
	}, nil
}

func validateRouteAndPrice(route domain.Route, price domain.RoutePrice) error {
	if route.ID == "" {
		return fmt.Errorf("%w: route id is empty", ErrInvalidPricingInput)
	}
	if price.RouteID == "" {
		return fmt.Errorf("%w: price route id is empty", ErrInvalidPricingInput)
	}
	if price.RouteID != route.ID {
		return fmt.Errorf("%w: price route id mismatch", ErrInvalidPricingInput)
	}
	if route.APIFamily == "" {
		return fmt.Errorf("%w: api family is empty", ErrInvalidPricingInput)
	}
	if route.EndpointKind == "" {
		return fmt.Errorf("%w: endpoint kind is empty", ErrInvalidPricingInput)
	}
	if route.ClientModel == "" {
		return fmt.Errorf("%w: client model is empty", ErrInvalidPricingInput)
	}
	return nil
}
