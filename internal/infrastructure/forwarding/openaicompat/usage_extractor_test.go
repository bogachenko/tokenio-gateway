package openaicompat

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestUsageExtractorExtractsDetailedChatUsageWithoutMutation(
	t *testing.T,
) {
	extractor := NewUsageExtractor()
	response := []byte(`{
		"id":"chatcmpl_1",
		"model":"provider-model",
		"usage":{
			"prompt_tokens":120,
			"completion_tokens":30,
			"prompt_tokens_details":{"cached_tokens":20},
			"completion_tokens_details":{"reasoning_tokens":7}
		}
	}`)
	original := append([]byte(nil), response...)

	result, err := extractor.Extract(
		context.Background(),
		ports.UsageExtractionRequest{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointChat,
			ClientModel:  "client-model",
			RequestBody:  []byte(`{"model":"client-model"}`),
			ResponseBody: response,
		},
	)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Completeness != "detailed" ||
		result.ProviderRequestID != "chatcmpl_1" ||
		result.ProviderResponseModel != "provider-model" ||
		result.Usage.InputTokens != 120 ||
		result.Usage.CachedInputTokens != 20 ||
		result.Usage.OutputTokens != 30 ||
		result.Usage.ReasoningTokens != 7 {
		t.Fatalf("result = %+v", result)
	}
	if !bytes.Equal(response, original) {
		t.Fatalf("response mutated: got %q want %q", response, original)
	}
}

func TestUsageExtractorSupportsCurrentOpenAITokenNames(
	t *testing.T,
) {
	extractor := NewUsageExtractor()
	result, err := extractor.Extract(
		context.Background(),
		ports.UsageExtractionRequest{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointChat,
			ClientModel:  "client-model",
			ResponseBody: []byte(`{
				"usage":{
					"input_tokens":11,
					"output_tokens":5,
					"input_tokens_details":{"cached_tokens":3},
					"output_tokens_details":{"reasoning_tokens":2}
				}
			}`),
		},
	)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Completeness != "detailed" ||
		result.Usage.InputTokens != 11 ||
		result.Usage.OutputTokens != 5 ||
		result.Usage.CachedInputTokens != 3 ||
		result.Usage.ReasoningTokens != 2 {
		t.Fatalf("result = %+v", result)
	}
}

func TestUsageExtractorExtractsDetailedEmbeddingUsage(
	t *testing.T,
) {
	extractor := NewUsageExtractor()
	result, err := extractor.Extract(
		context.Background(),
		ports.UsageExtractionRequest{
			APIFamily: domain.
				APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointEmbeddings,
			ClientModel:  "embedding-model",
			ResponseBody: []byte(`{
				"object":"list",
				"model":"provider-embedding-model",
				"usage":{"prompt_tokens":17,"total_tokens":17}
			}`),
		},
	)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Completeness != "detailed" ||
		result.ProviderResponseModel !=
			"provider-embedding-model" ||
		result.Usage.InputTokens != 17 ||
		result.Usage.OutputTokens != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestUsageExtractorUsesEmbeddingTotalTokensAsInputFallback(
	t *testing.T,
) {
	extractor := NewUsageExtractor()
	result, err := extractor.Extract(
		context.Background(),
		ports.UsageExtractionRequest{
			APIFamily: domain.
				APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointEmbeddings,
			ClientModel:  "embedding-model",
			ResponseBody: []byte(`{
				"usage":{"total_tokens":23}
			}`),
		},
	)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Completeness != "aggregate" ||
		result.Usage.InputTokens != 23 ||
		result.Usage.OutputTokens != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestUsageExtractorPrefersEmbeddingInputTokensOverTotal(
	t *testing.T,
) {
	extractor := NewUsageExtractor()
	result, err := extractor.Extract(
		context.Background(),
		ports.UsageExtractionRequest{
			APIFamily: domain.
				APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointEmbeddings,
			ClientModel:  "embedding-model",
			ResponseBody: []byte(`{
				"usage":{
					"input_tokens":19,
					"total_tokens":99
				}
			}`),
		},
	)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Completeness != "detailed" ||
		result.Usage.InputTokens != 19 {
		t.Fatalf("result = %+v", result)
	}
}

func TestUsageExtractorExtractsImageGenerationUnits(
	t *testing.T,
) {
	extractor := NewUsageExtractor()
	response := []byte(`{
		"created":1710000000,
		"data":[
			{"url":"https://example.test/1.png"},
			{"b64_json":"AAAA"}
		]
	}`)
	original := append([]byte(nil), response...)

	result, err := extractor.Extract(
		context.Background(),
		ports.UsageExtractionRequest{
			APIFamily: domain.
				APIFamilyOpenAICompatible,
			EndpointKind: domain.
				EndpointImagesGeneration,
			ClientModel:  "image-model",
			ResponseBody: response,
		},
	)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Completeness != "detailed" ||
		result.Usage.ImageGenerationUnits != 2 ||
		result.Usage.InputTokens != 0 ||
		result.Usage.OutputTokens != 0 {
		t.Fatalf("result = %+v", result)
	}
	if !bytes.Equal(response, original) {
		t.Fatalf("response mutated")
	}
}

func TestUsageExtractorReturnsMissingForImageResponseWithoutData(
	t *testing.T,
) {
	extractor := NewUsageExtractor()
	result, err := extractor.Extract(
		context.Background(),
		ports.UsageExtractionRequest{
			APIFamily: domain.
				APIFamilyOpenAICompatible,
			EndpointKind: domain.
				EndpointImagesGeneration,
			ClientModel:  "image-model",
			ResponseBody: []byte(`{"created":1710000000}`),
		},
	)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Completeness != "missing" ||
		result.Usage != (domain.TokenUsage{}) {
		t.Fatalf("result = %+v", result)
	}
}

func TestUsageExtractorRejectsMalformedImageResponseData(
	t *testing.T,
) {
	extractor := NewUsageExtractor()
	for _, response := range []string{
		`{"data":{}}`,
		`{"data":[]}`,
	} {
		_, err := extractor.Extract(
			context.Background(),
			ports.UsageExtractionRequest{
				APIFamily: domain.
					APIFamilyOpenAICompatible,
				EndpointKind: domain.
					EndpointImagesGeneration,
				ClientModel:  "image-model",
				ResponseBody: []byte(response),
			},
		)
		if !errors.Is(
			err,
			ErrUsageExtractionUnavailable,
		) {
			t.Fatalf(
				"response=%s error=%v",
				response,
				err,
			)
		}
	}
}

func TestUsageExtractorReturnsMissingWhenUsageIsAbsent(
	t *testing.T,
) {
	extractor := NewUsageExtractor()
	result, err := extractor.Extract(
		context.Background(),
		ports.UsageExtractionRequest{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointChat,
			ClientModel:  "client-model",
			ResponseBody: []byte(`{"id":"chatcmpl_1"}`),
		},
	)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Completeness != "missing" ||
		result.Usage != (domain.TokenUsage{}) {
		t.Fatalf("result = %+v", result)
	}
}

func TestUsageExtractorRejectsMalformedOrNegativeUsage(
	t *testing.T,
) {
	tests := []string{
		`{"usage":[]}`,
		`{"usage":{"prompt_tokens":-1}}`,
		`{"usage":{"completion_tokens":1.5}}`,
		`{"usage":{"prompt_tokens_details":"invalid"}}`,
	}
	extractor := NewUsageExtractor()
	for _, response := range tests {
		_, err := extractor.Extract(
			context.Background(),
			ports.UsageExtractionRequest{
				APIFamily: domain.
					APIFamilyOpenAICompatible,
				EndpointKind: domain.EndpointChat,
				ClientModel:  "client-model",
				ResponseBody: []byte(response),
			},
		)
		if !errors.Is(
			err,
			ErrUsageExtractionUnavailable,
		) {
			t.Fatalf(
				"response=%s error=%v",
				response,
				err,
			)
		}
	}
}

func TestUsageExtractorRejectsUnsupportedFamilyAndEndpoint(
	t *testing.T,
) {
	extractor := NewUsageExtractor()
	tests := []ports.UsageExtractionRequest{
		{
			APIFamily:    domain.APIFamilyGeminiNative,
			EndpointKind: domain.EndpointChat,
			ClientModel:  "model",
			ResponseBody: []byte(`{}`),
		},
		{
			APIFamily: domain.
				APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointModels,
			ClientModel:  "model",
			ResponseBody: []byte(`{}`),
		},
	}
	for _, request := range tests {
		_, err := extractor.Extract(
			context.Background(),
			request,
		)
		if !errors.Is(
			err,
			ErrUsageExtractionUnavailable,
		) {
			t.Fatalf("error = %v", err)
		}
	}
}
