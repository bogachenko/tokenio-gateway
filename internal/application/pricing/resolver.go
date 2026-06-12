package pricing

import (
	"context"
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type ResolveUsageInput struct {
	Route        domain.Route
	Price        domain.RoutePrice
	RequestBody  []byte
	ResponseBody []byte

	RequestedCapabilities domain.CapabilitySet
	Modalities            InputModalities

	ZeroUsageAllowed bool
}

type ResolvedUsageResult struct {
	Usage        domain.TokenUsage
	Completeness UsageCompleteness
	Estimated    bool

	UpstreamCostCents int64
	ClientAmountCents int64
	Currency          string

	ProviderRequestID     string
	ProviderResponseModel string
}

type UsageResolver struct {
	extractor  ports.UsageExtractor
	estimator  ports.TokenEstimator
	calculator *Calculator
}

func NewUsageResolver(extractor ports.UsageExtractor, estimator ports.TokenEstimator, calculator *Calculator) (*UsageResolver, error) {
	if extractor == nil {
		return nil, fmt.Errorf("%w: nil usage extractor", ErrInvalidPricingInput)
	}
	if estimator == nil {
		return nil, fmt.Errorf("%w: nil token estimator", ErrInvalidPricingInput)
	}
	if calculator == nil {
		return nil, fmt.Errorf("%w: nil calculator", ErrInvalidPricingInput)
	}
	return &UsageResolver{extractor: extractor, estimator: estimator, calculator: calculator}, nil
}

func (r *UsageResolver) Resolve(ctx context.Context, input ResolveUsageInput) (ResolvedUsageResult, error) {
	if r == nil || r.extractor == nil || r.estimator == nil || r.calculator == nil {
		return ResolvedUsageResult{}, fmt.Errorf("%w: nil usage resolver", ErrInvalidPricingInput)
	}
	if err := validateRouteAndPrice(input.Route, input.Price); err != nil {
		return ResolvedUsageResult{}, err
	}
	if input.RequestBody == nil || input.ResponseBody == nil {
		return ResolvedUsageResult{}, fmt.Errorf("%w: request or response body is nil", ErrInvalidPricingInput)
	}
	extracted, err := r.extractor.Extract(ctx, ports.UsageExtractionRequest{
		APIFamily:    input.Route.APIFamily,
		EndpointKind: input.Route.EndpointKind,
		ClientModel:  input.Route.ClientModel,
		RequestBody:  append([]byte(nil), input.RequestBody...),
		ResponseBody: append([]byte(nil), input.ResponseBody...),
	})
	if err != nil {
		return r.estimateFallback(ctx, input, UsageCompletenessEstimated, err)
	}
	completeness, err := ParseUsageCompleteness(extracted.Completeness)
	if err != nil {
		return ResolvedUsageResult{}, err
	}
	if IsZeroUsage(extracted.Usage) && !input.ZeroUsageAllowed {
		return r.estimateFallback(ctx, input, UsageCompletenessEstimated, nil)
	}
	switch completeness {
	case UsageCompletenessDetailed:
		return r.actualResult(extracted, completeness, input.Price, InputPricingModeDetailed, input.Modalities)
	case UsageCompletenessAggregate:
		return r.actualResult(extracted, completeness, input.Price, InputPricingModeAggregateMax, input.Modalities)
	case UsageCompletenessEstimated:
		return r.estimatedResult(extracted, completeness, input.Price, input.Modalities)
	case UsageCompletenessMissing, UsageCompletenessFailed:
		return r.estimateFallback(ctx, input, UsageCompletenessEstimated, nil)
	default:
		return ResolvedUsageResult{}, fmt.Errorf("%w: %q", ErrInvalidUsageCompleteness, completeness)
	}
}

func (r *UsageResolver) actualResult(extracted ports.UsageExtractionResult, completeness UsageCompleteness, price domain.RoutePrice, mode InputPricingMode, modalities InputModalities) (ResolvedUsageResult, error) {
	calculation, err := r.calculator.CalculateActual(ActualCalculationInput{Usage: extracted.Usage, Price: price, InputMode: mode, Modalities: modalities})
	if err != nil {
		return ResolvedUsageResult{}, err
	}
	return resolvedFromCalculation(calculation, completeness, extracted.ProviderRequestID, extracted.ProviderResponseModel), nil
}

func (r *UsageResolver) estimatedResult(extracted ports.UsageExtractionResult, completeness UsageCompleteness, price domain.RoutePrice, modalities InputModalities) (ResolvedUsageResult, error) {
	calculation, err := r.calculator.CalculateEstimate(EstimateCalculationInput{Usage: extracted.Usage, Price: price, InputMode: InputPricingModeDetailed, Modalities: modalities})
	if err != nil {
		return ResolvedUsageResult{}, err
	}
	return resolvedFromCalculation(calculation, completeness, extracted.ProviderRequestID, extracted.ProviderResponseModel), nil
}

func (r *UsageResolver) estimateFallback(ctx context.Context, input ResolveUsageInput, completeness UsageCompleteness, cause error) (ResolvedUsageResult, error) {
	estimate, err := r.estimator.Estimate(ctx, ports.TokenEstimateRequest{
		APIFamily:              input.Route.APIFamily,
		EndpointKind:           input.Route.EndpointKind,
		ClientModel:            input.Route.ClientModel,
		RequestBody:            append([]byte(nil), input.RequestBody...),
		DefaultMaxOutputTokens: input.Route.DefaultMaxOutputTokens,
		RequestedCapabilities:  input.RequestedCapabilities,
	})
	if err != nil {
		if cause != nil {
			return ResolvedUsageResult{}, fmt.Errorf("%w: extract usage failed: %w; estimate usage failed: %w", ErrUsageUnresolved, cause, err)
		}
		return ResolvedUsageResult{}, fmt.Errorf("%w: estimate usage failed: %w", ErrUsageUnresolved, err)
	}
	if err := ValidateUsage(estimate.Usage); err != nil {
		return ResolvedUsageResult{}, err
	}
	calculation, err := r.calculator.CalculateEstimate(EstimateCalculationInput{Usage: estimate.Usage, Price: input.Price, InputMode: InputPricingModeDetailed, Modalities: input.Modalities})
	if err != nil {
		return ResolvedUsageResult{}, err
	}
	return resolvedFromCalculation(calculation, completeness, "", ""), nil
}

func resolvedFromCalculation(calculation CalculationResult, completeness UsageCompleteness, requestID, responseModel string) ResolvedUsageResult {
	return ResolvedUsageResult{
		Usage:                 calculation.Usage,
		Completeness:          completeness,
		Estimated:             calculation.Estimated,
		UpstreamCostCents:     calculation.UpstreamCostCents,
		ClientAmountCents:     calculation.ClientAmountCents,
		Currency:              calculation.Currency,
		ProviderRequestID:     requestID,
		ProviderResponseModel: responseModel,
	}
}
