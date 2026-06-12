package pricing

import (
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type UsageCompleteness string

const (
	UsageCompletenessDetailed  UsageCompleteness = "detailed"
	UsageCompletenessAggregate UsageCompleteness = "aggregate"
	UsageCompletenessEstimated UsageCompleteness = "estimated"
	UsageCompletenessMissing   UsageCompleteness = "missing"
	UsageCompletenessFailed    UsageCompleteness = "failed"
)

func ParseUsageCompleteness(value string) (UsageCompleteness, error) {
	switch UsageCompleteness(value) {
	case UsageCompletenessDetailed, UsageCompletenessAggregate, UsageCompletenessEstimated, UsageCompletenessMissing, UsageCompletenessFailed:
		return UsageCompleteness(value), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidUsageCompleteness, value)
	}
}

func ValidateUsage(usage domain.TokenUsage) error {
	fields := []struct {
		name  string
		value int64
	}{
		{"input_tokens", usage.InputTokens},
		{"cached_input_tokens", usage.CachedInputTokens},
		{"output_tokens", usage.OutputTokens},
		{"reasoning_tokens", usage.ReasoningTokens},
		{"image_input_tokens", usage.ImageInputTokens},
		{"audio_input_tokens", usage.AudioInputTokens},
		{"audio_output_tokens", usage.AudioOutputTokens},
		{"file_input_tokens", usage.FileInputTokens},
		{"video_input_tokens", usage.VideoInputTokens},
		{"image_generation_units", usage.ImageGenerationUnits},
	}
	for _, field := range fields {
		if field.value < 0 {
			return fmt.Errorf("%w: %s is negative", ErrInvalidUsage, field.name)
		}
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
