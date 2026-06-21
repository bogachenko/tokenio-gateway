package modelcatalog

import (
	"errors"
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

var ErrInvalidPublicPricingCalculator = errors.New(
	"invalid model catalog public pricing calculator",
)

type PublicUnitPriceCalculator interface {
	CalculatePublicUnitPriceCents(
		unitPriceCents int64,
		markup float64,
	) (int64, error)
}

type RoutePricePublicPricingCalculator struct {
	calculator PublicUnitPriceCalculator
}

func NewRoutePricePublicPricingCalculator(
	calculator PublicUnitPriceCalculator,
) (*RoutePricePublicPricingCalculator, error) {
	if calculator == nil {
		return nil, ErrInvalidPublicPricingCalculator
	}
	return &RoutePricePublicPricingCalculator{
		calculator: calculator,
	}, nil
}

func (c *RoutePricePublicPricingCalculator) CalculatePublicPricing(
	price domain.RoutePrice,
) (Pricing, error) {
	if c == nil || c.calculator == nil {
		return Pricing{}, ErrInvalidPublicPricingCalculator
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
			c.calculator.CalculatePublicUnitPriceCents(
				value,
				price.MarkupCoefficient,
			)
		if err != nil {
			return Pricing{}, fmt.Errorf(
				"calculate public unit price: %w",
				err,
			)
		}
		public[index] = calculated
	}
	return Pricing{
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

var _ PublicPricingCalculator = (*RoutePricePublicPricingCalculator)(nil)
