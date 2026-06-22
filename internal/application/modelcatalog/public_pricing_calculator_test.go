package modelcatalog

import (
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestRoutePricePublicPricingCalculatorUsesInjectedCalculator(t *testing.T) {
	adapter, err := NewRoutePricePublicPricingCalculator(publicUnitPriceCalculatorFake{})
	if err != nil {
		t.Fatalf("New adapter: %v", err)
	}

	result, err := adapter.CalculatePublicPricing(domain.RoutePrice{
		Currency:                    "RUB",
		InputPricePer1MTokensCents:  101,
		OutputPricePer1MTokensCents: 100,
		MarkupCoefficient:           1.5,
		ImageGenerationUnitKind:     domain.ImageGenerationUnitKindNone,
	})
	if err != nil {
		t.Fatalf("CalculatePublicPricing: %v", err)
	}
	if result.InputPricePer1MTokensCents != 152 || result.OutputPricePer1MTokensCents != 150 {
		t.Fatalf("pricing = %+v", result)
	}
}

func TestNewRoutePricePublicPricingCalculatorRejectsNil(t *testing.T) {
	_, err := NewRoutePricePublicPricingCalculator(nil)
	if !errors.Is(err, ErrInvalidPublicPricingCalculator) {
		t.Fatalf("error = %v", err)
	}
}

type publicUnitPriceCalculatorFake struct{}

func (publicUnitPriceCalculatorFake) CalculatePublicUnitPriceCents(unitPriceCents int64, markup float64) (int64, error) {
	if unitPriceCents == 101 && markup == 1.5 {
		return 152, nil
	}
	if unitPriceCents == 100 && markup == 1.5 {
		return 150, nil
	}
	return 0, nil
}
