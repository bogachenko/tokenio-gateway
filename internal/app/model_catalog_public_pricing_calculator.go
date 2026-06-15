package app

import (
	"errors"
	"fmt"

	modelcatalog "github.com/bogachenko/tokenio-gateway/internal/application/modelcatalog"
	pricing "github.com/bogachenko/tokenio-gateway/internal/application/pricing"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

var ErrInvalidModelCatalogPricingAdapter = errors.New(
	"invalid model catalog pricing adapter",
)

type ModelCatalogPublicPricingCalculator struct {
	calculator *pricing.Calculator
}

func NewModelCatalogPublicPricingCalculator(
	calculator *pricing.Calculator,
) (*ModelCatalogPublicPricingCalculator, error) {
	if calculator == nil {
		return nil, ErrInvalidModelCatalogPricingAdapter
	}
	return &ModelCatalogPublicPricingCalculator{
		calculator: calculator,
	}, nil
}

func (a *ModelCatalogPublicPricingCalculator) CalculatePublicPricing(
	price domain.RoutePrice,
) (modelcatalog.Pricing, error) {
	if a == nil || a.calculator == nil {
		return modelcatalog.Pricing{},
			ErrInvalidModelCatalogPricingAdapter
	}
	values := []int64{
		price.InputPricePer1MTokensCents,
		price.CachedInputPricePer1MTokensCents,
		price.OutputPricePer1MTokensCents,
		price.ReasoningOutputPricePer1MTokensCents,
		price.ImageInputPricePer1MTokensCents,
		price.AudioInputPricePer1MTokensCents,
		price.AudioOutputPricePer1MTokensCents,
		price.FileInputPricePer1MTokensCents,
		price.VideoInputPricePer1MTokensCents,
		price.ImageGenerationPricePerUnitCents,
	}
	public := make([]int64, len(values))
	for index, value := range values {
		calculated, err :=
			a.calculator.CalculatePublicUnitPriceCents(
				value,
				price.MarkupCoefficient,
			)
		if err != nil {
			return modelcatalog.Pricing{}, fmt.Errorf(
				"calculate public unit price: %w",
				err,
			)
		}
		public[index] = calculated
	}
	return modelcatalog.Pricing{
		Currency:                             price.Currency,
		InputPricePer1MTokensCents:           public[0],
		CachedInputPricePer1MTokensCents:     public[1],
		OutputPricePer1MTokensCents:          public[2],
		ReasoningOutputPricePer1MTokensCents: public[3],
		ImageInputPricePer1MTokensCents:      public[4],
		AudioInputPricePer1MTokensCents:      public[5],
		AudioOutputPricePer1MTokensCents:     public[6],
		FileInputPricePer1MTokensCents:       public[7],
		VideoInputPricePer1MTokensCents:      public[8],
		ImageGenerationPricePerUnitCents:     public[9],
		ImageGenerationUnitKind:              price.ImageGenerationUnitKind,
	}, nil
}

var _ modelcatalog.PublicPricingCalculator = (*ModelCatalogPublicPricingCalculator)(nil)
