package pricing

import (
	"fmt"
	"math"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type TokenCalculator struct {
	Currency                    string
	TokenEstimationSafetyFactor float64
	CostEstimationSafetyFactor  float64
}

type AmountInput struct {
	Usage              domain.TokenUsage
	Price              domain.RoutePrice
	Estimated          bool
	UseMarkup          bool
	UseSafetyFactor    bool
	MultimodalMaxInput bool
}

func (c TokenCalculator) AmountCents(input AmountInput) (int64, error) {
	if input.Price.Currency != "RUB" {
		return 0, fmt.Errorf("unsupported currency: %s", input.Price.Currency)
	}

	markup := input.Price.MarkupCoefficient
	if !input.UseMarkup {
		markup = 1
	}
	if markup <= 0 {
		return 0, fmt.Errorf("markup coefficient must be positive")
	}

	safety := 1.0
	if input.UseSafetyFactor {
		safety = c.CostEstimationSafetyFactor
		if safety <= 0 {
			safety = 1
		}
	}

	usage := input.Usage
	if input.Estimated && c.TokenEstimationSafetyFactor > 1 {
		usage = multiplyUsage(usage, c.TokenEstimationSafetyFactor)
	}

	raw := int64(0)

	if input.MultimodalMaxInput {
		maxInputPrice := maxInt64(
			input.Price.InputPricePer1MTokensCents,
			input.Price.ImageInputPricePer1MTokensCents,
			input.Price.AudioInputPricePer1MTokensCents,
			input.Price.FileInputPricePer1MTokensCents,
			input.Price.VideoInputPricePer1MTokensCents,
		)
		totalInput := usage.InputTokens +
			usage.CachedInputTokens +
			usage.ImageInputTokens +
			usage.AudioInputTokens +
			usage.FileInputTokens +
			usage.VideoInputTokens

		raw += totalInput * maxInputPrice
	} else {
		raw += usage.InputTokens * input.Price.InputPricePer1MTokensCents
		raw += usage.CachedInputTokens * input.Price.CachedInputPricePer1MTokensCents
		raw += usage.ImageInputTokens * input.Price.ImageInputPricePer1MTokensCents
		raw += usage.AudioInputTokens * input.Price.AudioInputPricePer1MTokensCents
		raw += usage.FileInputTokens * input.Price.FileInputPricePer1MTokensCents
		raw += usage.VideoInputTokens * input.Price.VideoInputPricePer1MTokensCents
	}

	raw += usage.OutputTokens * input.Price.OutputPricePer1MTokensCents
	raw += usage.ReasoningTokens * input.Price.ReasoningOutputPricePer1MTokensCents
	raw += usage.AudioOutputTokens * input.Price.AudioOutputPricePer1MTokensCents

	if raw <= 0 {
		return 0, nil
	}

	return int64(math.Ceil((float64(raw) / 1_000_000.0) * markup * safety)), nil
}

func multiplyUsage(usage domain.TokenUsage, factor float64) domain.TokenUsage {
	return domain.TokenUsage{
		InputTokens:          ceilMul(usage.InputTokens, factor),
		CachedInputTokens:    ceilMul(usage.CachedInputTokens, factor),
		OutputTokens:         ceilMul(usage.OutputTokens, factor),
		ReasoningTokens:      ceilMul(usage.ReasoningTokens, factor),
		ImageInputTokens:     ceilMul(usage.ImageInputTokens, factor),
		AudioInputTokens:     ceilMul(usage.AudioInputTokens, factor),
		AudioOutputTokens:    ceilMul(usage.AudioOutputTokens, factor),
		FileInputTokens:      ceilMul(usage.FileInputTokens, factor),
		VideoInputTokens:     ceilMul(usage.VideoInputTokens, factor),
		ImageGenerationUnits: usage.ImageGenerationUnits,
	}
}

func ceilMul(value int64, factor float64) int64 {
	if value <= 0 {
		return 0
	}
	return int64(math.Ceil(float64(value) * factor))
}

func maxInt64(values ...int64) int64 {
	max := int64(0)
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}
