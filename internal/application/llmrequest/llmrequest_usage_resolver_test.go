package llmrequest

import (
	"context"
	"net/http"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type usagePricingResolverFunc func(
	context.Context,
	UsagePricingInput,
) (UsagePricingResult, error)

func (function usagePricingResolverFunc) Resolve(
	ctx context.Context,
	input UsagePricingInput,
) (UsagePricingResult, error) {
	return function(ctx, input)
}

func TestLLMRequestUsageResolverMapsPricingResult(t *testing.T) {
	resolver := mustLLMRequestUsageResolver(t, nil)

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
	resolver := mustLLMRequestUsageResolver(t, func(t *testing.T, input UsagePricingInput) {
		t.Helper()
		if input.ActualUsage == nil || input.ActualUsage.InputTokens != 12 || input.ActualUsage.OutputTokens != 8 {
			t.Fatalf("actual usage = %+v", input.ActualUsage)
		}
	})

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
	resolver := mustLLMRequestUsageResolver(t, func(t *testing.T, input UsagePricingInput) {
		t.Helper()
		if input.ActualUsage == nil || input.ActualUsage.InputTokens != 12 || input.ActualUsage.OutputTokens != 7 {
			t.Fatalf("actual usage = %+v", input.ActualUsage)
		}
	})

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
	assert func(*testing.T, UsagePricingInput),
) *LLMRequestUsageResolver {
	t.Helper()

	resolver, err := NewLLMRequestUsageResolver(
		usagePricingResolverFunc(
			func(_ context.Context, input UsagePricingInput) (UsagePricingResult, error) {
				if assert != nil {
					assert(t, input)
				}
				usage := domain.TokenUsage{InputTokens: 10, OutputTokens: 5}
				if input.ActualUsage != nil {
					usage = *input.ActualUsage
				}
				return UsagePricingResult{
					Usage:             usage,
					Completeness:      domain.UsageCompletenessDetailed,
					UpstreamCostCents: 7,
					ClientAmountCents: 11,
					Currency:          "RUB",
				}, nil
			},
		),
	)
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
