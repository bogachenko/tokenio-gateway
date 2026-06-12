package pricing

import (
	"errors"
	"reflect"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestValidateUsage(t *testing.T) {
	if err := ValidateUsage(domain.TokenUsage{}); err != nil {
		t.Fatalf("zero usage invalid: %v", err)
	}
	positive := map[string]domain.TokenUsage{
		"input": {InputTokens: 1}, "cached": {CachedInputTokens: 1}, "output": {OutputTokens: 1}, "reasoning": {ReasoningTokens: 1},
		"image input": {ImageInputTokens: 1}, "audio input": {AudioInputTokens: 1}, "audio output": {AudioOutputTokens: 1},
		"file input": {FileInputTokens: 1}, "video input": {VideoInputTokens: 1}, "image units": {ImageGenerationUnits: 1},
	}
	for name, usage := range positive {
		t.Run(name, func(t *testing.T) {
			if err := ValidateUsage(usage); err != nil {
				t.Fatalf("positive category invalid: %v", err)
			}
			if IsZeroUsage(usage) {
				t.Fatalf("positive category treated as zero")
			}
		})
	}
	negative := map[string]domain.TokenUsage{
		"input": {InputTokens: -1}, "cached": {CachedInputTokens: -1}, "output": {OutputTokens: -1}, "reasoning": {ReasoningTokens: -1},
		"image input": {ImageInputTokens: -1}, "audio input": {AudioInputTokens: -1}, "audio output": {AudioOutputTokens: -1},
		"file input": {FileInputTokens: -1}, "video input": {VideoInputTokens: -1}, "image units": {ImageGenerationUnits: -1},
	}
	for name, usage := range negative {
		t.Run("negative "+name, func(t *testing.T) {
			if err := ValidateUsage(usage); !errors.Is(err, ErrInvalidUsage) {
				t.Fatalf("err = %v, want ErrInvalidUsage", err)
			}
		})
	}
	if !IsZeroUsage(domain.TokenUsage{}) {
		t.Fatalf("all-zero usage should be zero")
	}
}

func TestParseUsageCompleteness(t *testing.T) {
	for _, value := range []string{"detailed", "aggregate", "estimated", "missing", "failed"} {
		t.Run(value, func(t *testing.T) {
			got, err := ParseUsageCompleteness(value)
			if err != nil || string(got) != value {
				t.Fatalf("got %q err %v", got, err)
			}
		})
	}
	for _, value := range []string{"unknown", "Detailed", " detailed", "detailed "} {
		t.Run("reject "+value, func(t *testing.T) {
			_, err := ParseUsageCompleteness(value)
			if !errors.Is(err, ErrInvalidUsageCompleteness) {
				t.Fatalf("err = %v, want ErrInvalidUsageCompleteness", err)
			}
		})
	}
}

func TestExportedResultDTOsDoNotContainSecretsOrBodies(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(CalculationResult{}), reflect.TypeOf(PreflightResult{}), reflect.TypeOf(ResolvedUsageResult{})} {
		for _, forbidden := range []string{"RawAPIKey", "ResellerAPIKey", "Authorization", "BillingJWT", "ServiceToken", "APIKeyEnv", "RequestBody", "ResponseBody"} {
			if _, ok := typ.FieldByName(forbidden); ok {
				t.Fatalf("%s contains forbidden field %s", typ.Name(), forbidden)
			}
		}
	}
}
