package app

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/application/pricing"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type llmRequestSecretPresenceFunc func(
	context.Context,
	string,
) (bool, error)

func (function llmRequestSecretPresenceFunc) Exists(
	ctx context.Context,
	name string,
) (bool, error) {
	return function(ctx, name)
}

type llmRequestRouteCapacityFunc func(
	context.Context,
	llmrequest.RouteCapacityInput,
) (llmrequest.RouteCapacityResult, error)

func (function llmRequestRouteCapacityFunc) Check(
	ctx context.Context,
	input llmrequest.RouteCapacityInput,
) (llmrequest.RouteCapacityResult, error) {
	return function(ctx, input)
}

type llmRequestRewriteSupportFunc func(
	domain.APIFamily,
	domain.ProviderType,
) bool

func (function llmRequestRewriteSupportFunc) SupportsModelIdentifierRewrite(
	apiFamily domain.APIFamily,
	providerType domain.ProviderType,
) bool {
	return function(apiFamily, providerType)
}

type llmRequestTokenEstimatorFunc func(
	context.Context,
	ports.TokenEstimateRequest,
) (ports.TokenEstimate, error)

func (function llmRequestTokenEstimatorFunc) Estimate(
	ctx context.Context,
	request ports.TokenEstimateRequest,
) (ports.TokenEstimate, error) {
	return function(ctx, request)
}

func TestLLMRequestRoutePreflighterBuildsCompleteFacts(t *testing.T) {
	input := validLLMRequestRoutePreflightInput()
	originalPayload := append([]byte(nil), input.Payload...)

	var gotSecretName string
	var gotCapacity llmrequest.RouteCapacityInput
	var gotEstimateClientModel string
	var gotEstimateBody []byte
	var gotRewriteAPI domain.APIFamily
	var gotRewriteProvider domain.ProviderType

	preflighter := mustLLMRequestRoutePreflighter(
		t,
		llmRequestSecretPresenceFunc(
			func(
				_ context.Context,
				name string,
			) (bool, error) {
				gotSecretName = name
				return true, nil
			},
		),
		mustLLMRequestPreflightPricer(
			t,
			llmRequestTokenEstimatorFunc(
				func(
					_ context.Context,
					request ports.TokenEstimateRequest,
				) (ports.TokenEstimate, error) {
					gotEstimateClientModel = request.ClientModel
					gotEstimateBody = append(
						[]byte(nil),
						request.RequestBody...,
					)
					request.RequestBody[0] = 'X'
					return ports.TokenEstimate{
						Usage: domain.TokenUsage{
							InputTokens:  10,
							OutputTokens: 5,
						},
						Confidence: "conservative",
					}, nil
				},
			),
		),
		llmRequestRouteCapacityFunc(
			func(
				_ context.Context,
				value llmrequest.RouteCapacityInput,
			) (llmrequest.RouteCapacityResult, error) {
				gotCapacity = value
				return llmrequest.RouteCapacityResult{
					RateLimitAllowed:   true,
					ConcurrencyAllowed: true,
				}, nil
			},
		),
		llmRequestRewriteSupportFunc(
			func(
				apiFamily domain.APIFamily,
				providerType domain.ProviderType,
			) bool {
				gotRewriteAPI = apiFamily
				gotRewriteProvider = providerType
				return true
			},
		),
	)

	result, err := preflighter.Evaluate(
		context.Background(),
		input,
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if gotSecretName != "RESELLER_API_KEY" {
		t.Fatalf("secret name = %q", gotSecretName)
	}
	if gotCapacity.Route.ID != input.Route.ID ||
		gotCapacity.Reseller.ID != input.Reseller.ID ||
		gotCapacity.EstimatedUsage != (domain.TokenUsage{
			InputTokens:  10,
			OutputTokens: 5,
		}) {
		t.Fatalf("capacity input = %+v", gotCapacity)
	}
	if gotEstimateClientModel != input.Route.ClientModel ||
		!bytes.Equal(gotEstimateBody, originalPayload) {
		t.Fatalf(
			"estimate request model/body = %q/%q",
			gotEstimateClientModel,
			gotEstimateBody,
		)
	}
	if gotRewriteAPI != input.Route.APIFamily ||
		gotRewriteProvider != input.Route.ProviderType {
		t.Fatalf(
			"rewrite lookup = %q/%q",
			gotRewriteAPI,
			gotRewriteProvider,
		)
	}
	if !bytes.Equal(input.Payload, originalPayload) {
		t.Fatalf("caller payload mutated: %q", input.Payload)
	}

	if !result.SecretAvailable ||
		!result.CostAvailable ||
		!result.RateLimitAllowed ||
		!result.ConcurrencyAllowed ||
		!result.ModelIdentifierRewriteAllowed ||
		result.EstimatedUpstreamCostCents != 20 ||
		result.EstimatedClientAmountCents != 30 ||
		result.Currency != "RUB" ||
		result.Confidence != "conservative" {
		t.Fatalf("result = %+v", result)
	}
}

func TestLLMRequestRoutePreflighterMissingPriceIsUnavailable(
	t *testing.T,
) {
	input := validLLMRequestRoutePreflightInput()
	input.Price = nil
	estimatorCalled := false
	capacityCalled := false

	preflighter := mustLLMRequestRoutePreflighter(
		t,
		alwaysPresentLLMRequestSecret(),
		mustLLMRequestPreflightPricer(
			t,
			llmRequestTokenEstimatorFunc(
				func(
					context.Context,
					ports.TokenEstimateRequest,
				) (ports.TokenEstimate, error) {
					estimatorCalled = true
					return ports.TokenEstimate{}, nil
				},
			),
		),
		llmRequestRouteCapacityFunc(
			func(
				context.Context,
				llmrequest.RouteCapacityInput,
			) (llmrequest.RouteCapacityResult, error) {
				capacityCalled = true
				return llmrequest.RouteCapacityResult{}, nil
			},
		),
		allowedLLMRequestRewrite(),
	)

	result, err := preflighter.Evaluate(
		context.Background(),
		input,
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.CostAvailable ||
		estimatorCalled ||
		capacityCalled {
		t.Fatalf(
			"result = %+v, estimator called = %v, capacity called = %v",
			result,
			estimatorCalled,
			capacityCalled,
		)
	}
}

func TestLLMRequestRoutePreflighterPricingErrorMarksUnavailable(
	t *testing.T,
) {
	input := validLLMRequestRoutePreflightInput()
	input.Price.Currency = "USD"
	capacityCalled := false

	preflighter := mustLLMRequestRoutePreflighter(
		t,
		alwaysPresentLLMRequestSecret(),
		mustLLMRequestPreflightPricer(
			t,
			llmRequestTokenEstimatorFunc(
				func(
					context.Context,
					ports.TokenEstimateRequest,
				) (ports.TokenEstimate, error) {
					return ports.TokenEstimate{
						Usage: domain.TokenUsage{
							InputTokens: 1,
						},
						Confidence: "conservative",
					}, nil
				},
			),
		),
		llmRequestRouteCapacityFunc(
			func(
				context.Context,
				llmrequest.RouteCapacityInput,
			) (llmrequest.RouteCapacityResult, error) {
				capacityCalled = true
				return llmrequest.RouteCapacityResult{}, nil
			},
		),
		allowedLLMRequestRewrite(),
	)

	result, err := preflighter.Evaluate(
		context.Background(),
		input,
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.CostAvailable ||
		!result.SecretAvailable ||
		result.RateLimitAllowed ||
		result.ConcurrencyAllowed ||
		capacityCalled {
		t.Fatalf(
			"result = %+v, capacity called = %v",
			result,
			capacityCalled,
		)
	}
}

func TestLLMRequestRoutePreflighterPreservesCapacityDenials(
	t *testing.T,
) {
	preflighter := mustLLMRequestRoutePreflighter(
		t,
		alwaysPresentLLMRequestSecret(),
		mustLLMRequestPreflightPricer(
			t,
			staticLLMRequestEstimator(),
		),
		llmRequestRouteCapacityFunc(
			func(
				context.Context,
				llmrequest.RouteCapacityInput,
			) (llmrequest.RouteCapacityResult, error) {
				return llmrequest.RouteCapacityResult{
					RateLimitAllowed:   false,
					ConcurrencyAllowed: false,
				}, nil
			},
		),
		allowedLLMRequestRewrite(),
	)

	result, err := preflighter.Evaluate(
		context.Background(),
		validLLMRequestRoutePreflightInput(),
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.RateLimitAllowed ||
		result.ConcurrencyAllowed ||
		!result.CostAvailable {
		t.Fatalf("result = %+v", result)
	}
}

func TestLLMRequestRoutePreflighterChecksRewriteOnlyWhenRequired(
	t *testing.T,
) {
	input := validLLMRequestRoutePreflightInput()
	input.Route.ModelRewritePolicy =
		domain.ModelRewritePolicyNone
	input.Route.ProviderModel = input.Route.ClientModel
	rewriteCalls := 0

	preflighter := mustLLMRequestRoutePreflighter(
		t,
		alwaysPresentLLMRequestSecret(),
		mustLLMRequestPreflightPricer(
			t,
			staticLLMRequestEstimator(),
		),
		allowedLLMRequestCapacity(),
		llmRequestRewriteSupportFunc(
			func(
				domain.APIFamily,
				domain.ProviderType,
			) bool {
				rewriteCalls++
				return false
			},
		),
	)

	result, err := preflighter.Evaluate(
		context.Background(),
		input,
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !result.ModelIdentifierRewriteAllowed ||
		rewriteCalls != 0 {
		t.Fatalf(
			"result = %+v, rewrite calls = %d",
			result,
			rewriteCalls,
		)
	}
}

func TestLLMRequestRoutePreflighterPropagatesOperationalErrors(
	t *testing.T,
) {
	dependencyError := errors.New("dependency failed")

	tests := []struct {
		name     string
		secrets  ports.SecretPresenceChecker
		capacity llmrequest.RouteCapacityChecker
	}{
		{
			name: "secret presence",
			secrets: llmRequestSecretPresenceFunc(
				func(
					context.Context,
					string,
				) (bool, error) {
					return false, dependencyError
				},
			),
			capacity: allowedLLMRequestCapacity(),
		},
		{
			name:    "capacity",
			secrets: alwaysPresentLLMRequestSecret(),
			capacity: llmRequestRouteCapacityFunc(
				func(
					context.Context,
					llmrequest.RouteCapacityInput,
				) (llmrequest.RouteCapacityResult, error) {
					return llmrequest.RouteCapacityResult{},
						dependencyError
				},
			),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			preflighter := mustLLMRequestRoutePreflighter(
				t,
				test.secrets,
				mustLLMRequestPreflightPricer(
					t,
					staticLLMRequestEstimator(),
				),
				test.capacity,
				allowedLLMRequestRewrite(),
			)

			_, err := preflighter.Evaluate(
				context.Background(),
				validLLMRequestRoutePreflightInput(),
			)
			if !errors.Is(err, dependencyError) {
				t.Fatalf(
					"error = %v, want dependency error",
					err,
				)
			}
		})
	}
}

func TestNewLLMRequestRoutePreflighterRequiresDependencies(
	t *testing.T,
) {
	pricer := mustLLMRequestPreflightPricer(
		t,
		staticLLMRequestEstimator(),
	)
	validSecrets := alwaysPresentLLMRequestSecret()
	validCapacity := allowedLLMRequestCapacity()
	validRewrite := allowedLLMRequestRewrite()

	tests := []struct {
		name     string
		secrets  ports.SecretPresenceChecker
		pricer   *pricing.PreflightPricer
		capacity llmrequest.RouteCapacityChecker
		rewrite  ports.ModelIdentifierRewriteSupport
	}{
		{
			name:     "secrets",
			pricer:   pricer,
			capacity: validCapacity,
			rewrite:  validRewrite,
		},
		{
			name:     "pricer",
			secrets:  validSecrets,
			capacity: validCapacity,
			rewrite:  validRewrite,
		},
		{
			name:    "capacity",
			secrets: validSecrets,
			pricer:  pricer,
			rewrite: validRewrite,
		},
		{
			name:     "rewrite",
			secrets:  validSecrets,
			pricer:   pricer,
			capacity: validCapacity,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewLLMRequestRoutePreflighter(
				test.secrets,
				test.pricer,
				test.capacity,
				test.rewrite,
			)
			if !errors.Is(
				err,
				llmrequest.ErrDependencyRequired,
			) {
				t.Fatalf(
					"error = %v, want dependency required",
					err,
				)
			}
		})
	}
}

func mustLLMRequestRoutePreflighter(
	t *testing.T,
	secrets ports.SecretPresenceChecker,
	pricer *pricing.PreflightPricer,
	capacity llmrequest.RouteCapacityChecker,
	rewrite ports.ModelIdentifierRewriteSupport,
) *LLMRequestRoutePreflighter {
	t.Helper()

	value, err := NewLLMRequestRoutePreflighter(
		secrets,
		pricer,
		capacity,
		rewrite,
	)
	if err != nil {
		t.Fatalf("NewLLMRequestRoutePreflighter: %v", err)
	}
	return value
}

func mustLLMRequestPreflightPricer(
	t *testing.T,
	estimator ports.TokenEstimator,
) *pricing.PreflightPricer {
	t.Helper()

	calculator, err := pricing.NewCalculator(1, 1)
	if err != nil {
		t.Fatalf("NewCalculator: %v", err)
	}
	value, err := pricing.NewPreflightPricer(
		estimator,
		calculator,
	)
	if err != nil {
		t.Fatalf("NewPreflightPricer: %v", err)
	}
	return value
}

func validLLMRequestRoutePreflightInput() llmrequest.
	RouteCandidatePreflightInput {
	price := domain.RoutePrice{
		RouteID:                     "route-1",
		Currency:                    "RUB",
		InputPricePer1MTokensCents:  1_000_000,
		OutputPricePer1MTokensCents: 2_000_000,
		MarkupCoefficient:           1.5,
		Enabled:                     true,
	}
	return llmrequest.RouteCandidatePreflightInput{
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
		func(
			context.Context,
			string,
		) (bool, error) {
			return true, nil
		},
	)
}

func allowedLLMRequestCapacity() llmrequest.RouteCapacityChecker {
	return llmRequestRouteCapacityFunc(
		func(
			context.Context,
			llmrequest.RouteCapacityInput,
		) (llmrequest.RouteCapacityResult, error) {
			return llmrequest.RouteCapacityResult{
				RateLimitAllowed:   true,
				ConcurrencyAllowed: true,
			}, nil
		},
	)
}

func allowedLLMRequestRewrite() ports.ModelIdentifierRewriteSupport {
	return llmRequestRewriteSupportFunc(
		func(
			domain.APIFamily,
			domain.ProviderType,
		) bool {
			return true
		},
	)
}

func staticLLMRequestEstimator() ports.TokenEstimator {
	return llmRequestTokenEstimatorFunc(
		func(
			context.Context,
			ports.TokenEstimateRequest,
		) (ports.TokenEstimate, error) {
			return ports.TokenEstimate{
				Usage: domain.TokenUsage{
					InputTokens:  10,
					OutputTokens: 5,
				},
				Confidence: "conservative",
			}, nil
		},
	)
}
