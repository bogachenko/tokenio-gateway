package admin

import (
	"math"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func stage10MajorValidAdminRoute() domain.Route {
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

func TestRouteValidationContracts(t *testing.T) {
	base := stage10MajorValidAdminRoute()
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

func TestPriceValidationContracts(t *testing.T) {
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
