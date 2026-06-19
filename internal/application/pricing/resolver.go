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

	// ActualUsage is a trusted usage value already extracted by a native
	// forwarding adapter. It lets native adapters preserve the upstream
	// response body byte-for-byte while still passing normalized usage to
	// pricing and ledger finalization.
	ActualUsage *domain.TokenUsage

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
	if input.ActualUsage != nil {
		return r.actualForwardedUsage(ctx, input)
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
	required := requiredUsagePresence(input.Route.EndpointKind)
	if completeness == UsageCompletenessAggregate &&
		!presenceContains(extracted.Presence, required) {
		return r.partialActualResult(
			ctx,
			input,
			extracted,
			required,
		)
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

func (r *UsageResolver) actualForwardedUsage(
	ctx context.Context,
	input ResolveUsageInput,
) (ResolvedUsageResult, error) {
	usage := *input.ActualUsage
	if err := ValidateUsage(usage); err != nil {
		return ResolvedUsageResult{}, err
	}
	if IsZeroUsage(usage) && !input.ZeroUsageAllowed {
		return r.estimateFallback(ctx, input, UsageCompletenessEstimated, nil)
	}
	calculation, err := r.calculator.CalculateActual(
		ActualCalculationInput{
			Usage:      usage,
			Price:      input.Price,
			InputMode:  InputPricingModeDetailed,
			Modalities: input.Modalities,
		},
	)
	if err != nil {
		return ResolvedUsageResult{}, err
	}
	return resolvedFromCalculation(
		calculation,
		UsageCompletenessDetailed,
		"",
		"",
	), nil
}

func (r *UsageResolver) partialActualResult(
	ctx context.Context,
	input ResolveUsageInput,
	extracted ports.UsageExtractionResult,
	required ports.UsageDimensionPresence,
) (ResolvedUsageResult, error) {
	estimate, err := r.estimate(ctx, input)
	if err != nil {
		return ResolvedUsageResult{}, fmt.Errorf(
			"%w: estimate missing usage: %v",
			ErrUsageUnresolved,
			err,
		)
	}
	estimatedUsage := selectMissingRequiredUsage(
		estimate.Usage,
		extracted.Presence,
		required,
	)
	calculation, err := r.calculator.CalculateMixed(
		MixedCalculationInput{
			ActualUsage:        extracted.Usage,
			EstimatedUsage:     estimatedUsage,
			Price:              input.Price,
			ActualInputMode:    InputPricingModeAggregateMax,
			EstimatedInputMode: InputPricingModeDetailed,
			Modalities:         input.Modalities,
		},
	)
	if err != nil {
		return ResolvedUsageResult{}, err
	}
	return resolvedFromCalculation(
		calculation,
		UsageCompletenessAggregate,
		extracted.ProviderRequestID,
		extracted.ProviderResponseModel,
	), nil
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
	estimate, err := r.estimate(ctx, input)
	if err != nil {
		if cause != nil {
			return ResolvedUsageResult{}, fmt.Errorf("%w: extract usage failed: %w; estimate usage failed: %v", ErrUsageUnresolved, cause, err)
		}
		return ResolvedUsageResult{}, fmt.Errorf("%w: estimate usage failed: %v", ErrUsageUnresolved, err)
	}
	calculation, err := r.calculator.CalculateEstimate(EstimateCalculationInput{Usage: estimate.Usage, Price: input.Price, InputMode: InputPricingModeDetailed, Modalities: input.Modalities})
	if err != nil {
		return ResolvedUsageResult{}, err
	}
	return resolvedFromCalculation(calculation, completeness, "", ""), nil
}

func (r *UsageResolver) estimate(
	ctx context.Context,
	input ResolveUsageInput,
) (ports.TokenEstimate, error) {
	estimate, err := r.estimator.Estimate(ctx, ports.TokenEstimateRequest{
		APIFamily:              input.Route.APIFamily,
		EndpointKind:           input.Route.EndpointKind,
		ClientModel:            input.Route.ClientModel,
		RequestBody:            append([]byte(nil), input.RequestBody...),
		DefaultMaxOutputTokens: input.Route.DefaultMaxOutputTokens,
		RequestedCapabilities:  input.RequestedCapabilities,
	})
	if err != nil {
		return ports.TokenEstimate{}, err
	}
	if err := ValidateUsage(estimate.Usage); err != nil {
		return ports.TokenEstimate{}, err
	}
	return estimate, nil
}

func requiredUsagePresence(
	endpoint domain.EndpointKind,
) ports.UsageDimensionPresence {
	switch endpoint {
	case domain.EndpointChat:
		return ports.UsageDimensionPresence{
			InputTokens:  true,
			OutputTokens: true,
		}
	case domain.EndpointEmbeddings:
		return ports.UsageDimensionPresence{
			InputTokens: true,
		}
	case domain.EndpointImagesGeneration:
		return ports.UsageDimensionPresence{
			ImageGenerationUnits: true,
		}
	default:
		return ports.UsageDimensionPresence{}
	}
}

func presenceContains(
	actual ports.UsageDimensionPresence,
	required ports.UsageDimensionPresence,
) bool {
	return (!required.InputTokens || actual.InputTokens) &&
		(!required.CachedInputTokens || actual.CachedInputTokens) &&
		(!required.OutputTokens || actual.OutputTokens) &&
		(!required.ReasoningTokens || actual.ReasoningTokens) &&
		(!required.ImageInputTokens || actual.ImageInputTokens) &&
		(!required.AudioInputTokens || actual.AudioInputTokens) &&
		(!required.AudioOutputTokens || actual.AudioOutputTokens) &&
		(!required.FileInputTokens || actual.FileInputTokens) &&
		(!required.VideoInputTokens || actual.VideoInputTokens) &&
		(!required.ImageGenerationUnits || actual.ImageGenerationUnits)
}

func selectMissingRequiredUsage(
	estimate domain.TokenUsage,
	actual ports.UsageDimensionPresence,
	required ports.UsageDimensionPresence,
) domain.TokenUsage {
	var selected domain.TokenUsage
	if required.InputTokens && !actual.InputTokens {
		selected.InputTokens = estimate.InputTokens
	}
	if required.CachedInputTokens && !actual.CachedInputTokens {
		selected.CachedInputTokens = estimate.CachedInputTokens
	}
	if required.OutputTokens && !actual.OutputTokens {
		selected.OutputTokens = estimate.OutputTokens
	}
	if required.ReasoningTokens && !actual.ReasoningTokens {
		selected.ReasoningTokens = estimate.ReasoningTokens
	}
	if required.ImageInputTokens && !actual.ImageInputTokens {
		selected.ImageInputTokens = estimate.ImageInputTokens
	}
	if required.AudioInputTokens && !actual.AudioInputTokens {
		selected.AudioInputTokens = estimate.AudioInputTokens
	}
	if required.AudioOutputTokens && !actual.AudioOutputTokens {
		selected.AudioOutputTokens = estimate.AudioOutputTokens
	}
	if required.FileInputTokens && !actual.FileInputTokens {
		selected.FileInputTokens = estimate.FileInputTokens
	}
	if required.VideoInputTokens && !actual.VideoInputTokens {
		selected.VideoInputTokens = estimate.VideoInputTokens
	}
	if required.ImageGenerationUnits && !actual.ImageGenerationUnits {
		selected.ImageGenerationUnits = estimate.ImageGenerationUnits
	}
	return selected
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
