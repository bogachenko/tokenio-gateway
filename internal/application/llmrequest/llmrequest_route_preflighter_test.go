package llmrequest

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type llmRequestSecretPresenceFunc func(context.Context, string) (bool, error)

func (function llmRequestSecretPresenceFunc) Exists(
	ctx context.Context,
	name string,
) (bool, error) {
	return function(ctx, name)
}

type llmRequestRouteCapacityFunc func(
	context.Context,
	ports.RouteCapacityCheckInput,
) (ports.RouteCapacityResult, error)

func (function llmRequestRouteCapacityFunc) Check(
	ctx context.Context,
	input ports.RouteCapacityCheckInput,
) (ports.RouteCapacityResult, error) {
	return function(ctx, input)
}

type llmRequestAdapterSupportFunc func(
	domain.APIFamily,
	domain.ProviderType,
	domain.EndpointKind,
) bool

func (function llmRequestAdapterSupportFunc) SupportsForwardingAdapter(
	apiFamily domain.APIFamily,
	providerType domain.ProviderType,
	endpointKind domain.EndpointKind,
) bool {
	return function(apiFamily, providerType, endpointKind)
}

type llmRequestRewriteSupportFunc func(domain.APIFamily, domain.ProviderType) bool

func (function llmRequestRewriteSupportFunc) SupportsModelIdentifierRewrite(
	apiFamily domain.APIFamily,
	providerType domain.ProviderType,
) bool {
	return function(apiFamily, providerType)
}

type llmRequestPreflightPricerFunc func(
	context.Context,
	PreflightPricingInput,
) (PreflightPricingResult, error)

func (function llmRequestPreflightPricerFunc) Price(
	ctx context.Context,
	input PreflightPricingInput,
) (PreflightPricingResult, error) {
	return function(ctx, input)
}

func TestLLMRequestRoutePreflighterBuildsCompleteFacts(t *testing.T) {
	input := validLLMRequestRoutePreflightInput()
	originalPayload := append([]byte(nil), input.Payload...)

	var gotSecretName string
	var gotCapacity ports.RouteCapacityCheckInput
	var gotPriceRouteID string
	var gotPriceBody []byte
	var gotRewriteAPI domain.APIFamily
	var gotRewriteProvider domain.ProviderType

	preflighter := mustLLMRequestRoutePreflighter(
		t,
		llmRequestSecretPresenceFunc(
			func(_ context.Context, name string) (bool, error) {
				gotSecretName = name
				return true, nil
			},
		),
		llmRequestPreflightPricerFunc(
			func(_ context.Context, request PreflightPricingInput) (PreflightPricingResult, error) {
				gotPriceRouteID = request.Route.ID
				gotPriceBody = append([]byte(nil), request.RequestBody...)
				request.RequestBody[0] = 'X'
				return PreflightPricingResult{
					EstimatedUsage:             domain.TokenUsage{InputTokens: 10, OutputTokens: 5},
					EstimatedUpstreamCostCents: 20,
					EstimatedClientAmountCents: 30,
					Currency:                   "RUB",
					Confidence:                 "conservative",
				}, nil
			},
		),
		llmRequestRouteCapacityFunc(
			func(_ context.Context, value ports.RouteCapacityCheckInput) (ports.RouteCapacityResult, error) {
				gotCapacity = value
				return ports.RouteCapacityResult{RateLimitAllowed: true, ConcurrencyAllowed: true}, nil
			},
		),
		llmRequestRewriteSupportFunc(
			func(apiFamily domain.APIFamily, providerType domain.ProviderType) bool {
				gotRewriteAPI = apiFamily
				gotRewriteProvider = providerType
				return true
			},
		),
	)

	result, err := preflighter.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if gotSecretName != "RESELLER_API_KEY" {
		t.Fatalf("secret name = %q", gotSecretName)
	}
	if gotCapacity.Route.ID != input.Route.ID ||
		gotCapacity.Reseller.ID != input.Reseller.ID ||
		gotCapacity.EstimatedUsage != (domain.TokenUsage{InputTokens: 10, OutputTokens: 5}) {
		t.Fatalf("capacity input = %+v", gotCapacity)
	}
	if gotPriceRouteID != input.Route.ID || !bytes.Equal(gotPriceBody, originalPayload) {
		t.Fatalf("price request route/body = %q/%q", gotPriceRouteID, gotPriceBody)
	}
	if gotRewriteAPI != input.Route.APIFamily || gotRewriteProvider != input.Route.ProviderType {
		t.Fatalf("rewrite lookup = %q/%q", gotRewriteAPI, gotRewriteProvider)
	}
	if !bytes.Equal(input.Payload, originalPayload) {
		t.Fatalf("caller payload mutated: %q", input.Payload)
	}
	if !result.SecretAvailable || !result.CostAvailable || !result.ForwardingAdapterAvailable ||
		!result.RateLimitAllowed || !result.ConcurrencyAllowed || !result.ModelIdentifierRewriteAllowed ||
		result.EstimatedUpstreamCostCents != 20 || result.EstimatedClientAmountCents != 30 ||
		result.Currency != "RUB" || result.Confidence != "conservative" {
		t.Fatalf("result = %+v", result)
	}
}

func TestLLMRequestRoutePreflighterMissingPriceIsUnavailable(t *testing.T) {
	input := validLLMRequestRoutePreflightInput()
	input.Price = nil
	pricerCalled := false
	capacityCalled := false

	preflighter := mustLLMRequestRoutePreflighter(
		t,
		alwaysPresentLLMRequestSecret(),
		llmRequestPreflightPricerFunc(
			func(context.Context, PreflightPricingInput) (PreflightPricingResult, error) {
				pricerCalled = true
				return PreflightPricingResult{}, nil
			},
		),
		llmRequestRouteCapacityFunc(
			func(context.Context, ports.RouteCapacityCheckInput) (ports.RouteCapacityResult, error) {
				capacityCalled = true
				return ports.RouteCapacityResult{}, nil
			},
		),
		allowedLLMRequestRewrite(),
	)

	result, err := preflighter.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.CostAvailable || pricerCalled || capacityCalled {
		t.Fatalf("result = %+v, pricer called = %v, capacity called = %v", result, pricerCalled, capacityCalled)
	}
}

func TestNewLLMRequestRoutePreflighterRequiresDependencies(t *testing.T) {
	pricer := staticLLMRequestPreflightPricer()
	validSecrets := alwaysPresentLLMRequestSecret()
	validCapacity := allowedLLMRequestCapacity()
	validAdapter := allowedLLMRequestAdapterSupport()
	validRewrite := allowedLLMRequestRewrite()

	tests := []struct {
		name     string
		secrets  ports.SecretPresenceChecker
		pricer   PreflightPricer
		capacity ports.RouteCapacityChecker
		adapter  ports.ForwardingAdapterSupport
		rewrite  ports.ModelIdentifierRewriteSupport
	}{
		{name: "secrets", pricer: pricer, capacity: validCapacity, adapter: validAdapter, rewrite: validRewrite},
		{name: "pricer", secrets: validSecrets, capacity: validCapacity, adapter: validAdapter, rewrite: validRewrite},
		{name: "capacity", secrets: validSecrets, pricer: pricer, adapter: validAdapter, rewrite: validRewrite},
		{name: "adapter", secrets: validSecrets, pricer: pricer, capacity: validCapacity, rewrite: validRewrite},
		{name: "rewrite", secrets: validSecrets, pricer: pricer, capacity: validCapacity, adapter: validAdapter},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewLLMRequestRoutePreflighter(
				test.secrets,
				test.pricer,
				test.capacity,
				test.adapter,
				test.rewrite,
			)
			if !errors.Is(err, ErrDependencyRequired) {
				t.Fatalf("error = %v, want dependency required", err)
			}
		})
	}
}

func mustLLMRequestRoutePreflighter(
	t *testing.T,
	secrets ports.SecretPresenceChecker,
	pricer PreflightPricer,
	capacity ports.RouteCapacityChecker,
	rewrite ports.ModelIdentifierRewriteSupport,
) *LLMRequestRoutePreflighter {
	t.Helper()

	value, err := NewLLMRequestRoutePreflighter(
		secrets,
		pricer,
		capacity,
		allowedLLMRequestAdapterSupport(),
		rewrite,
	)
	if err != nil {
		t.Fatalf("NewLLMRequestRoutePreflighter: %v", err)
	}
	return value
}

func validLLMRequestRoutePreflightInput() RouteCandidatePreflightInput {
	price := domain.RoutePrice{
		RouteID:                     "route-1",
		Currency:                    "RUB",
		InputPricePer1MTokensCents:  1_000_000,
		OutputPricePer1MTokensCents: 2_000_000,
		MarkupCoefficient:           1.5,
		Enabled:                     true,
	}
	return RouteCandidatePreflightInput{
		Route: domain.Route{
			ID:                 "route-1",
			ResellerID:         "reseller-1",
			ProviderType:       domain.ProviderOpenAI,
			APIFamily:          domain.APIFamilyOpenAICompatible,
			EndpointKind:       domain.EndpointChat,
			ClientModel:        "model-1",
			ProviderModel:      "provider-model-1",
			ModelRewritePolicy: domain.ModelRewritePolicyProviderModel,
			Enabled:            true,
		},
		Reseller: domain.Reseller{
			ID:           "reseller-1",
			ProviderType: domain.ProviderOpenAI,
			APIKeyEnv:    "RESELLER_API_KEY",
			Enabled:      true,
		},
		Price: &price,
		RequestedCapabilities: domain.CapabilitySet{
			Chat: true,
		},
		Payload: []byte(`{"model":"model-1"}`),
	}
}

func alwaysPresentLLMRequestSecret() ports.SecretPresenceChecker {
	return llmRequestSecretPresenceFunc(
		func(context.Context, string) (bool, error) { return true, nil },
	)
}

func allowedLLMRequestCapacity() ports.RouteCapacityChecker {
	return llmRequestRouteCapacityFunc(
		func(context.Context, ports.RouteCapacityCheckInput) (ports.RouteCapacityResult, error) {
			return ports.RouteCapacityResult{RateLimitAllowed: true, ConcurrencyAllowed: true}, nil
		},
	)
}

func allowedLLMRequestAdapterSupport() ports.ForwardingAdapterSupport {
	return llmRequestAdapterSupportFunc(
		func(domain.APIFamily, domain.ProviderType, domain.EndpointKind) bool { return true },
	)
}

func allowedLLMRequestRewrite() ports.ModelIdentifierRewriteSupport {
	return llmRequestRewriteSupportFunc(
		func(domain.APIFamily, domain.ProviderType) bool { return true },
	)
}

func staticLLMRequestPreflightPricer() PreflightPricer {
	return llmRequestPreflightPricerFunc(
		func(context.Context, PreflightPricingInput) (PreflightPricingResult, error) {
			return PreflightPricingResult{
				EstimatedUsage:             domain.TokenUsage{InputTokens: 10, OutputTokens: 5},
				EstimatedUpstreamCostCents: 20,
				EstimatedClientAmountCents: 30,
				Currency:                   "RUB",
				Confidence:                 "conservative",
			}, nil
		},
	)
}
