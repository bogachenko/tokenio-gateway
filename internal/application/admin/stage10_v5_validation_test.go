package admin

import (
	"math"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func stage10V5ValidAdminRoute() domain.Route {
	at := time.Unix(1, 0).UTC()
	return domain.Route{
		ID:                     "route_1",
		ResellerID:             "reseller_1",
		ProviderType:           domain.ProviderOpenAI,
		APIFamily:              domain.APIFamilyOpenAICompatible,
		EndpointKind:           domain.EndpointChat,
		ClientModel:            "model-a",
		ProviderModel:          "model-a",
		ModelRewritePolicy:     domain.ModelRewritePolicyNone,
		Enabled:                true,
		Priority:               100,
		DefaultMaxOutputTokens: 4096,
		Capabilities:           domain.CapabilitySet{Chat: true},
		CreatedAt:              at,
		UpdatedAt:              at,
	}
}

func TestStage10V5RouteValidationContracts(t *testing.T) {
	base := stage10V5ValidAdminRoute()
	if err := validateRoute(base); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*domain.Route)
	}{
		{"provider", func(route *domain.Route) { route.ProviderType = "unknown" }},
		{"api family", func(route *domain.Route) { route.APIFamily = "unknown" }},
		{"endpoint", func(route *domain.Route) { route.EndpointKind = domain.EndpointModels }},
		{"rewrite mismatch", func(route *domain.Route) { route.ProviderModel = "provider-model" }},
		{"chat max output", func(route *domain.Route) { route.DefaultMaxOutputTokens = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			route := base
			tc.mutate(&route)
			if err := validateRoute(route); err == nil {
				t.Fatalf("invalid route accepted: %+v", route)
			}
		})
	}
}

func TestStage10V5PriceValidationContracts(t *testing.T) {
	at := time.Unix(1, 0).UTC()
	base := domain.RoutePrice{
		RouteID:                 "route_1",
		Currency:                "RUB",
		ImageGenerationUnitKind: domain.ImageGenerationUnitKindNone,
		MarkupCoefficient:       1,
		Enabled:                 true,
		CreatedAt:               at,
		UpdatedAt:               at,
	}
	validator := &fakePriceValidator{}
	service := &Service{
		deps: Dependencies{
			PriceValidator: validator,
		},
	}
	if err := service.validatePrice(base); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name         string
		mutate       func(*domain.RoutePrice)
		validatorErr error
	}{
		{"currency", func(price *domain.RoutePrice) { price.Currency = "USD" }, ErrInvalidRequest},
		{"negative field", func(price *domain.RoutePrice) { price.InputPricePer1MTokensCents = -1 }, ErrInvalidRequest},
		{"zero markup", func(price *domain.RoutePrice) { price.MarkupCoefficient = 0 }, nil},
		{"nan markup", func(price *domain.RoutePrice) { price.MarkupCoefficient = math.NaN() }, nil},
		{"unit kind", func(price *domain.RoutePrice) { price.ImageGenerationUnitKind = "unknown" }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			validator.err = tc.validatorErr
			price := base
			tc.mutate(&price)
			if err := service.validatePrice(price); err == nil {
				t.Fatalf("invalid price accepted: %+v", price)
			}
		})
	}
}

func TestStage10V5ResellerBaseURLMatchesRuntimeAdapterContract(t *testing.T) {
	at := time.Unix(2, 0).UTC()
	base := domain.Reseller{
		ID:                  "reseller_url",
		Name:                "URL contract",
		ProviderType:        domain.ProviderOpenAI,
		BaseURL:             "https://provider.example/v1",
		APIKeyEnv:           "PROVIDER_KEY",
		Enabled:             true,
		BalanceCents:        100,
		MinimumBalanceCents: 1,
		CreatedAt:           at,
		UpdatedAt:           at,
	}
	if err := validateReseller(base); err != nil {
		t.Fatalf("valid base URL rejected: %v", err)
	}
	for _, invalid := range []string{
		"provider.example",
		"ftp://provider.example",
		"https://user:password@provider.example",
		"https://provider.example?token=secret",
		"https://provider.example#fragment",
		"https:///missing-host",
	} {
		candidate := base
		candidate.BaseURL = invalid
		if err := validateReseller(candidate); err == nil {
			t.Fatalf("invalid base URL accepted: %q", invalid)
		}
	}
}
