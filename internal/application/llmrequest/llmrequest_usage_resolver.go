package llmrequest

import (
	"context"
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type UsagePricingInput struct {
	Route        domain.Route
	Price        domain.RoutePrice
	RequestBody  []byte
	ResponseBody []byte
	ActualUsage  *domain.TokenUsage

	RequestedCapabilities domain.CapabilitySet
	Modalities            UsagePricingInputModalities
	ZeroUsageAllowed      bool
}

type UsagePricingInputModalities struct {
	Image bool
	Audio bool
	File  bool
	Video bool
}

type UsagePricingResult struct {
	Usage        domain.TokenUsage
	Completeness domain.UsageCompleteness
	Estimated    bool

	UpstreamCostCents int64
	ClientAmountCents int64
	Currency          string

	ProviderRequestID     string
	ProviderResponseModel string
}

type UsagePricingResolver interface {
	Resolve(context.Context, UsagePricingInput) (UsagePricingResult, error)
}

type LLMRequestUsageResolver struct {
	pricing UsagePricingResolver
}

var _ UsageResolver = (*LLMRequestUsageResolver)(nil)

func NewLLMRequestUsageResolver(
	pricing UsagePricingResolver,
) (*LLMRequestUsageResolver, error) {
	if pricing == nil {
		return nil, ErrDependencyRequired
	}
	return &LLMRequestUsageResolver{pricing: pricing}, nil
}

func forwardUsageToTokenUsage(
	value *ports.ForwardUsage,
) *domain.TokenUsage {
	if value == nil {
		return nil
	}
	return &domain.TokenUsage{
		InputTokens:  value.InputTokens,
		OutputTokens: value.OutputTokens,
	}
}

func (r *LLMRequestUsageResolver) Resolve(
	ctx context.Context,
	input UsageResolutionInput,
) (UsageResolutionResult, error) {
	if r == nil || r.pricing == nil {
		return UsageResolutionResult{}, ErrDependencyRequired
	}
	if ctx == nil {
		return UsageResolutionResult{}, fmt.Errorf(
			"%w: nil usage resolution context",
			ErrInvalidInput,
		)
	}
	if err := ctx.Err(); err != nil {
		return UsageResolutionResult{}, err
	}

	prepared := input.Reserved.Prepared
	result, err := r.pricing.Resolve(
		ctx,
		UsagePricingInput{
			Route:        prepared.Plan.Route,
			Price:        prepared.Plan.Price,
			RequestBody:  append([]byte(nil), prepared.Payload...),
			ResponseBody: append([]byte(nil), input.Response.Body...),
			ActualUsage:  forwardUsageToTokenUsage(input.Response.Usage),
			RequestedCapabilities: prepared.
				RequestedCapabilities,
			Modalities: UsagePricingInputModalities{
				Image: prepared.RequestedCapabilities.ImageInput,
				Audio: prepared.RequestedCapabilities.AudioInput,
				File:  prepared.RequestedCapabilities.FileInput,
				Video: prepared.RequestedCapabilities.VideoInput,
			},
			ZeroUsageAllowed: false,
		},
	)
	if err != nil {
		return UsageResolutionResult{}, fmt.Errorf(
			"resolve LLM-request usage and pricing: %w",
			err,
		)
	}

	return UsageResolutionResult{
		Usage:                 result.Usage,
		Completeness:          string(result.Completeness),
		Estimated:             result.Estimated,
		UpstreamCostCents:     result.UpstreamCostCents,
		ClientAmountCents:     result.ClientAmountCents,
		Currency:              result.Currency,
		ProviderRequestID:     result.ProviderRequestID,
		ProviderResponseModel: result.ProviderResponseModel,
	}, nil
}
