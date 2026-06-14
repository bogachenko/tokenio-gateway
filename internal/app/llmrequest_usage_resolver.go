package app

import (
	"context"
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	pricingapp "github.com/bogachenko/tokenio-gateway/internal/application/pricing"
)

type LLMRequestUsageResolver struct {
	pricing *pricingapp.UsageResolver
}

var _ llmrequest.UsageResolver = (*LLMRequestUsageResolver)(nil)

func NewLLMRequestUsageResolver(
	pricing *pricingapp.UsageResolver,
) (*LLMRequestUsageResolver, error) {
	if pricing == nil {
		return nil, llmrequest.ErrDependencyRequired
	}
	return &LLMRequestUsageResolver{pricing: pricing}, nil
}

func (r *LLMRequestUsageResolver) Resolve(
	ctx context.Context,
	input llmrequest.UsageResolutionInput,
) (llmrequest.UsageResolutionResult, error) {
	if r == nil || r.pricing == nil {
		return llmrequest.UsageResolutionResult{},
			llmrequest.ErrDependencyRequired
	}
	if ctx == nil {
		return llmrequest.UsageResolutionResult{}, fmt.Errorf(
			"%w: nil usage resolution context",
			llmrequest.ErrInvalidInput,
		)
	}
	if err := ctx.Err(); err != nil {
		return llmrequest.UsageResolutionResult{}, err
	}

	prepared := input.Reserved.Prepared
	result, err := r.pricing.Resolve(
		ctx,
		pricingapp.ResolveUsageInput{
			Route:        prepared.Plan.Route,
			Price:        prepared.Plan.Price,
			RequestBody:  append([]byte(nil), prepared.Payload...),
			ResponseBody: append([]byte(nil), input.Response.Body...),
			RequestedCapabilities: prepared.
				RequestedCapabilities,
			Modalities: pricingapp.InputModalities{
				Image: prepared.RequestedCapabilities.ImageInput,
				Audio: prepared.RequestedCapabilities.AudioInput,
				File:  prepared.RequestedCapabilities.FileInput,
				Video: prepared.RequestedCapabilities.VideoInput,
			},
			ZeroUsageAllowed: false,
		},
	)
	if err != nil {
		return llmrequest.UsageResolutionResult{}, fmt.Errorf(
			"resolve LLM-request usage and pricing: %w",
			err,
		)
	}

	return llmrequest.UsageResolutionResult{
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
