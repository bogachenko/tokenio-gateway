package pricing

import (
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type UsageCompleteness = domain.UsageCompleteness

const (
	UsageCompletenessDetailed  = domain.UsageCompletenessDetailed
	UsageCompletenessAggregate = domain.UsageCompletenessAggregate
	UsageCompletenessEstimated = domain.UsageCompletenessEstimated
	UsageCompletenessMissing   = domain.UsageCompletenessMissing
	UsageCompletenessFailed    = domain.UsageCompletenessFailed
)

func ParseUsageCompleteness(value string) (UsageCompleteness, error) {
	result, err := domain.ParseUsageCompleteness(value)
	if err != nil {
		return "", fmt.Errorf("%w: %q", ErrInvalidUsageCompleteness, value)
	}
	return result, nil
}

func ValidateUsage(usage domain.TokenUsage) error {
	if err := domain.ValidateTokenUsage(usage); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidUsage, err)
	}
	return nil
}

func IsZeroUsage(usage domain.TokenUsage) bool {
	return usage.InputTokens == 0 &&
		usage.CachedInputTokens == 0 &&
		usage.OutputTokens == 0 &&
		usage.ReasoningTokens == 0 &&
		usage.ImageInputTokens == 0 &&
		usage.AudioInputTokens == 0 &&
		usage.AudioOutputTokens == 0 &&
		usage.FileInputTokens == 0 &&
		usage.VideoInputTokens == 0 &&
		usage.ImageGenerationUnits == 0
}

type InputModalities struct {
	Image bool
	Audio bool
	File  bool
	Video bool
}

type InputPricingMode string

const (
	InputPricingModeDetailed     InputPricingMode = "detailed"
	InputPricingModeAggregateMax InputPricingMode = "aggregate_max"
)
