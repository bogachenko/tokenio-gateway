package app

import (
	"errors"
	"testing"

	pricing "github.com/bogachenko/tokenio-gateway/internal/application/pricing"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestModelCatalogPublicPricingCalculatorUsesSharedCalculator(
	t *testing.T,
) {
	calculator, err := pricing.NewCalculator(1.25, 1.10)
	if err != nil {
		t.Fatalf("NewCalculator: %v", err)
	}
	adapter, err :=
		NewModelCatalogPublicPricingCalculator(calculator)
	if err != nil {
		t.Fatalf("New adapter: %v", err)
	}
	result, err := adapter.CalculatePublicPricing(
		domain.RoutePrice{
			Currency:                    "RUB",
			InputPricePer1MTokensCents:  101,
			OutputPricePer1MTokensCents: 100,
			MarkupCoefficient:           1.5,
			ImageGenerationUnitKind:     domain.ImageGenerationUnitKindNone,
		},
	)
	if err != nil {
		t.Fatalf("CalculatePublicPricing: %v", err)
	}
	if result.InputPricePer1MTokensCents != 152 ||
		result.OutputPricePer1MTokensCents != 150 {
		t.Fatalf("pricing = %+v", result)
	}
}

func TestNewModelCatalogPublicPricingCalculatorRejectsNil(
	t *testing.T,
) {
	_, err := NewModelCatalogPublicPricingCalculator(nil)
	if !errors.Is(
		err,
		ErrInvalidModelCatalogPricingAdapter,
	) {
		t.Fatalf("error = %v", err)
	}
}
