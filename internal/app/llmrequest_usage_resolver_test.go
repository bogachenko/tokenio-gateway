package app

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	pricingapp "github.com/bogachenko/tokenio-gateway/internal/application/pricing"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type usageResolverExtractor struct{}

func (usageResolverExtractor) Extract(
	context.Context,
	ports.UsageExtractionRequest,
) (ports.UsageExtractionResult, error) {
	return ports.UsageExtractionResult{
		Usage: domain.TokenUsage{
			InputTokens:  10,
			OutputTokens: 5,
		},
		Completeness: "detailed",
	}, nil
}

type failingUsageResolverExtractor struct {
	called *bool
}

func (extractor failingUsageResolverExtractor) Extract(
	context.Context,
	ports.UsageExtractionRequest,
) (ports.UsageExtractionResult, error) {
	if extractor.called != nil {
		*extractor.called = true
	}
	return ports.UsageExtractionResult{}, errors.New(
		"extractor must not be called when forward usage is present",
	)
}

type usageResolverEstimator struct{}

func (usageResolverEstimator) Estimate(
	context.Context,
	ports.TokenEstimateRequest,
) (ports.TokenEstimate, error) {
	return ports.TokenEstimate{}, errors.New("unexpected estimation")
}

func TestLLMRequestUsageResolverMapsPricingResult(t *testing.T) {
	calculator, err := pricingapp.NewCalculator(1.25, 1.10)
	if err != nil {
		t.Fatal(err)
	}
	pricingResolver, err := pricingapp.NewUsageResolver(
		usageResolverExtractor{},
		usageResolverEstimator{},
		calculator,
	)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := NewLLMRequestUsageResolver(pricingResolver)
	if err != nil {
		t.Fatal(err)
	}

	result, err := resolver.Resolve(
		context.Background(),
		llmrequest.UsageResolutionInput{
			Reserved: llmrequest.ReservedRequest{
				Prepared: llmrequest.PreparedRequest{
					RequestedCapabilities: domain.CapabilitySet{
						Chat: true,
					},
					Payload: []byte(`{"model":"model-1"}`),
					Plan: llmrequest.RoutePlan{
						Route: domain.Route{
							ID:           "route-1",
							APIFamily:    domain.APIFamilyOpenAICompatible,
							EndpointKind: domain.EndpointChat,
							ClientModel:  "model-1",
						},
						Price: domain.RoutePrice{
							RouteID:                     "route-1",
							Currency:                    "RUB",
							InputPricePer1MTokensCents:  1000000,
							OutputPricePer1MTokensCents: 2000000,
							MarkupCoefficient:           1,
							Enabled:                     true,
						},
					},
				},
			},
			Response: ports.ForwardResponse{
				StatusCode: 200,
				Body:       []byte(`{"usage":{}}`),
			},
		},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if result.Usage.InputTokens != 10 ||
		result.Usage.OutputTokens != 5 ||
		result.Completeness != "detailed" ||
		result.Estimated ||
		result.UpstreamCostCents <= 0 ||
		result.ClientAmountCents <= 0 ||
		result.Currency != "RUB" {
		t.Fatalf("result = %+v", result)
	}
}

func TestLLMRequestUsageResolverUsesForwardingUsageBeforeResponseExtraction(t *testing.T) {
	calculator, err := pricingapp.NewCalculator(1.25, 1.10)
	if err != nil {
		t.Fatal(err)
	}
	pricingResolver, err := pricingapp.NewUsageResolver(
		failingUsageResolverExtractor{},
		usageResolverEstimator{},
		calculator,
	)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := NewLLMRequestUsageResolver(pricingResolver)
	if err != nil {
		t.Fatal(err)
	}

	result, err := resolver.Resolve(
		context.Background(),
		llmrequest.UsageResolutionInput{
			Reserved: llmrequest.ReservedRequest{
				Prepared: llmrequest.PreparedRequest{
					RequestedCapabilities: domain.CapabilitySet{
						Chat: true,
					},
					Payload: []byte(`{"model":"claude-client"}`),
					Plan: llmrequest.RoutePlan{
						Route: domain.Route{
							ID:           "route-anthropic",
							APIFamily:    domain.APIFamilyAnthropicNative,
							EndpointKind: domain.EndpointChat,
							ClientModel:  "claude-client",
						},
						Price: domain.RoutePrice{
							RouteID:                     "route-anthropic",
							Currency:                    "RUB",
							InputPricePer1MTokensCents:  1000000,
							OutputPricePer1MTokensCents: 2000000,
							MarkupCoefficient:           1,
							Enabled:                     true,
						},
					},
				},
			},
			Response: ports.ForwardResponse{
				StatusCode: 200,
				Body:       []byte(`{"opaque":"anthropic response kept byte-for-byte"}`),
				Usage: &ports.ForwardUsage{
					InputTokens:  12,
					OutputTokens: 8,
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if result.Usage.InputTokens != 12 ||
		result.Usage.OutputTokens != 8 ||
		result.Completeness != "detailed" ||
		result.Estimated ||
		result.UpstreamCostCents <= 0 ||
		result.ClientAmountCents <= 0 ||
		result.Currency != "RUB" {
		t.Fatalf("result = %+v", result)
	}
}

func TestLLMRequestUsageResolverUsesForwardResponseUsageWithoutReparsingBody(t *testing.T) {
	calculator, err := pricingapp.NewCalculator(1.25, 1.10)
	if err != nil {
		t.Fatal(err)
	}
	extractorCalled := false
	pricingResolver, err := pricingapp.NewUsageResolver(
		failingUsageResolverExtractor{called: &extractorCalled},
		usageResolverEstimator{},
		calculator,
	)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := NewLLMRequestUsageResolver(pricingResolver)
	if err != nil {
		t.Fatal(err)
	}

	result, err := resolver.Resolve(
		context.Background(),
		llmrequest.UsageResolutionInput{
			Reserved: llmrequest.ReservedRequest{
				Prepared: llmrequest.PreparedRequest{
					RequestedCapabilities: domain.CapabilitySet{
						Chat: true,
					},
					Payload: []byte(`{"model":"claude-3-5-sonnet","messages":[{"role":"user","content":"hi"}]}`),
					Plan: llmrequest.RoutePlan{
						Route: domain.Route{
							ID:           "anthropic-route-1",
							APIFamily:    domain.APIFamilyAnthropicNative,
							EndpointKind: domain.EndpointChat,
							ClientModel:  "claude-3-5-sonnet",
						},
						Price: domain.RoutePrice{
							RouteID:                     "anthropic-route-1",
							Currency:                    "RUB",
							InputPricePer1MTokensCents:  1000000,
							OutputPricePer1MTokensCents: 2000000,
							MarkupCoefficient:           1,
							Enabled:                     true,
						},
					},
				},
			},
			Response: ports.ForwardResponse{
				StatusCode: http.StatusOK,
				Body:       []byte(`{"id":"msg_1","usage":{"input_tokens":"must-not-be-parsed"}}`),
				Usage: &ports.ForwardUsage{
					InputTokens:  12,
					OutputTokens: 7,
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if extractorCalled {
		t.Fatal("response body extractor was called despite forward usage")
	}
	if result.Usage.InputTokens != 12 ||
		result.Usage.OutputTokens != 7 ||
		result.Completeness != "detailed" ||
		result.Estimated ||
		result.UpstreamCostCents <= 0 ||
		result.ClientAmountCents <= 0 ||
		result.Currency != "RUB" {
		t.Fatalf("result = %+v", result)
	}
}
