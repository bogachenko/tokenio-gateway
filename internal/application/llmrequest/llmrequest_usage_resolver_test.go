package llmrequest

import (
	"context"
	"errors"
	"net/http"
	"testing"

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
	resolver := mustLLMRequestUsageResolver(t, usageResolverExtractor{})

	result, err := resolver.Resolve(
		context.Background(),
		UsageResolutionInput{
			Reserved: usageResolverReservedRequest(
				domain.APIFamilyOpenAICompatible,
				"route-1",
				"model-1",
				[]byte(`{"model":"model-1"}`),
			),
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
	resolver := mustLLMRequestUsageResolver(t, failingUsageResolverExtractor{})

	result, err := resolver.Resolve(
		context.Background(),
		UsageResolutionInput{
			Reserved: usageResolverReservedRequest(
				domain.APIFamilyAnthropicNative,
				"route-anthropic",
				"claude-client",
				[]byte(`{"model":"claude-client"}`),
			),
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
	extractorCalled := false
	resolver := mustLLMRequestUsageResolver(
		t,
		failingUsageResolverExtractor{called: &extractorCalled},
	)

	result, err := resolver.Resolve(
		context.Background(),
		UsageResolutionInput{
			Reserved: usageResolverReservedRequest(
				domain.APIFamilyAnthropicNative,
				"anthropic-route-1",
				"claude-3-5-sonnet",
				[]byte(`{"model":"claude-3-5-sonnet","messages":[{"role":"user","content":"hi"}]}`),
			),
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

func mustLLMRequestUsageResolver(
	t *testing.T,
	extractor ports.UsageExtractor,
) *LLMRequestUsageResolver {
	t.Helper()

	calculator, err := pricingapp.NewCalculator(1.25, 1.10)
	if err != nil {
		t.Fatal(err)
	}
	pricingResolver, err := pricingapp.NewUsageResolver(
		extractor,
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
	return resolver
}

func usageResolverReservedRequest(
	apiFamily domain.APIFamily,
	routeID string,
	clientModel string,
	payload []byte,
) ReservedRequest {
	return ReservedRequest{
		Prepared: PreparedRequest{
			RequestedCapabilities: domain.CapabilitySet{Chat: true},
			Payload:               payload,
			Plan: RoutePlan{
				Route: domain.Route{
					ID:           routeID,
					APIFamily:    apiFamily,
					EndpointKind: domain.EndpointChat,
					ClientModel:  clientModel,
				},
				Price: domain.RoutePrice{
					RouteID:                     routeID,
					Currency:                    "RUB",
					InputPricePer1MTokensCents:  1000000,
					OutputPricePer1MTokensCents: 2000000,
					MarkupCoefficient:           1,
					Enabled:                     true,
				},
			},
		},
	}
}
