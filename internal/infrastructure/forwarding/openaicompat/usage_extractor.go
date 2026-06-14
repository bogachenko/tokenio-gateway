package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

var ErrUsageExtractionUnavailable = errors.New(
	"OpenAI-compatible usage extraction unavailable",
)

type UsageExtractor struct{}

var _ ports.UsageExtractor = (*UsageExtractor)(nil)

func NewUsageExtractor() *UsageExtractor {
	return &UsageExtractor{}
}

func (e *UsageExtractor) Extract(
	ctx context.Context,
	request ports.UsageExtractionRequest,
) (ports.UsageExtractionResult, error) {
	if e == nil {
		return ports.UsageExtractionResult{}, fmt.Errorf(
			"%w: nil extractor",
			ErrUsageExtractionUnavailable,
		)
	}
	if ctx == nil {
		return ports.UsageExtractionResult{}, fmt.Errorf(
			"%w: nil context",
			ErrUsageExtractionUnavailable,
		)
	}
	if err := ctx.Err(); err != nil {
		return ports.UsageExtractionResult{}, err
	}
	if request.APIFamily != domain.APIFamilyOpenAICompatible ||
		!supportedUsageEndpoint(request.EndpointKind) ||
		strings.TrimSpace(request.ClientModel) == "" ||
		request.ResponseBody == nil {
		return ports.UsageExtractionResult{}, fmt.Errorf(
			"%w: unsupported extraction contract",
			ErrUsageExtractionUnavailable,
		)
	}

	root, err := decodeUsageRoot(request.ResponseBody)
	if err != nil {
		return ports.UsageExtractionResult{}, err
	}

	result := ports.UsageExtractionResult{
		Completeness:          "missing",
		ProviderRequestID:     optionalJSONString(root, "id"),
		ProviderResponseModel: optionalJSONString(root, "model"),
	}

	if request.EndpointKind == domain.EndpointImagesGeneration {
		return extractImageGenerationUsage(result, root)
	}

	rawUsage, exists := root["usage"]
	if !exists || bytes.Equal(bytes.TrimSpace(rawUsage), []byte("null")) {
		return result, nil
	}

	usageRoot, err := decodeUsageObject(rawUsage)
	if err != nil {
		return ports.UsageExtractionResult{}, err
	}

	if request.EndpointKind == domain.EndpointEmbeddings {
		return extractEmbeddingUsage(result, usageRoot)
	}

	inputTokens, inputPresent, err := firstNonNegativeInteger(
		usageRoot,
		"input_tokens",
		"prompt_tokens",
	)
	if err != nil {
		return ports.UsageExtractionResult{}, err
	}
	outputTokens, outputPresent, err := firstNonNegativeInteger(
		usageRoot,
		"output_tokens",
		"completion_tokens",
	)
	if err != nil {
		return ports.UsageExtractionResult{}, err
	}

	cachedInputTokens, cachedPresent, err :=
		nestedNonNegativeInteger(
			usageRoot,
			[]string{
				"input_tokens_details",
				"prompt_tokens_details",
			},
			"cached_tokens",
		)
	if err != nil {
		return ports.UsageExtractionResult{}, err
	}
	reasoningTokens, reasoningPresent, err :=
		nestedNonNegativeInteger(
			usageRoot,
			[]string{
				"output_tokens_details",
				"completion_tokens_details",
			},
			"reasoning_tokens",
		)
	if err != nil {
		return ports.UsageExtractionResult{}, err
	}

	if !inputPresent && !outputPresent &&
		!cachedPresent && !reasoningPresent {
		return result, nil
	}

	result.Usage = domain.TokenUsage{
		InputTokens:       inputTokens,
		CachedInputTokens: cachedInputTokens,
		OutputTokens:      outputTokens,
		ReasoningTokens:   reasoningTokens,
	}
	if inputPresent && outputPresent {
		result.Completeness = "detailed"
	} else {
		result.Completeness = "aggregate"
	}
	return result, nil
}

func supportedUsageEndpoint(
	endpoint domain.EndpointKind,
) bool {
	switch endpoint {
	case domain.EndpointChat,
		domain.EndpointEmbeddings,
		domain.EndpointImagesGeneration:
		return true
	default:
		return false
	}
}

func extractImageGenerationUsage(
	result ports.UsageExtractionResult,
	root map[string]json.RawMessage,
) (ports.UsageExtractionResult, error) {
	rawData, exists := root["data"]
	if !exists || bytes.Equal(
		bytes.TrimSpace(rawData),
		[]byte("null"),
	) {
		return result, nil
	}

	var data []json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(rawData))
	if err := decoder.Decode(&data); err != nil || data == nil {
		return ports.UsageExtractionResult{}, fmt.Errorf(
			"%w: image response data is not an array",
			ErrUsageExtractionUnavailable,
		)
	}
	if len(data) == 0 {
		return ports.UsageExtractionResult{}, fmt.Errorf(
			"%w: image response data is empty",
			ErrUsageExtractionUnavailable,
		)
	}

	result.Usage = domain.TokenUsage{
		ImageGenerationUnits: int64(len(data)),
	}
	result.Completeness = "detailed"
	return result, nil
}

func extractEmbeddingUsage(
	result ports.UsageExtractionResult,
	usageRoot map[string]json.RawMessage,
) (ports.UsageExtractionResult, error) {
	inputTokens, inputPresent, err := firstNonNegativeInteger(
		usageRoot,
		"input_tokens",
		"prompt_tokens",
	)
	if err != nil {
		return ports.UsageExtractionResult{}, err
	}

	totalTokens, totalPresent, err := firstNonNegativeInteger(
		usageRoot,
		"total_tokens",
	)
	if err != nil {
		return ports.UsageExtractionResult{}, err
	}

	switch {
	case inputPresent:
		result.Usage = domain.TokenUsage{
			InputTokens: inputTokens,
		}
		result.Completeness = "detailed"
	case totalPresent:
		result.Usage = domain.TokenUsage{
			InputTokens: totalTokens,
		}
		result.Completeness = "aggregate"
	default:
		return result, nil
	}

	return result, nil
}

func decodeUsageRoot(
	payload []byte,
) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()

	var root map[string]json.RawMessage
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf(
			"%w: decode response: %v",
			ErrUsageExtractionUnavailable,
			err,
		)
	}
	if root == nil {
		return nil, fmt.Errorf(
			"%w: response root is not an object",
			ErrUsageExtractionUnavailable,
		)
	}

	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf(
			"%w: trailing JSON value",
			ErrUsageExtractionUnavailable,
		)
	}
	return root, nil
}

func decodeUsageObject(
	payload json.RawMessage,
) (map[string]json.RawMessage, error) {
	var value map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, fmt.Errorf(
			"%w: usage is not an object",
			ErrUsageExtractionUnavailable,
		)
	}
	return value, nil
}

func optionalJSONString(
	root map[string]json.RawMessage,
	name string,
) string {
	raw, exists := root[name]
	if !exists {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return value
}

func firstNonNegativeInteger(
	root map[string]json.RawMessage,
	names ...string,
) (int64, bool, error) {
	for _, name := range names {
		raw, exists := root[name]
		if !exists {
			continue
		}
		value, err := decodeNonNegativeInteger(raw, name)
		return value, true, err
	}
	return 0, false, nil
}

func nestedNonNegativeInteger(
	root map[string]json.RawMessage,
	parents []string,
	name string,
) (int64, bool, error) {
	for _, parent := range parents {
		rawParent, exists := root[parent]
		if !exists || bytes.Equal(
			bytes.TrimSpace(rawParent),
			[]byte("null"),
		) {
			continue
		}
		parentRoot, err := decodeUsageObject(rawParent)
		if err != nil {
			return 0, false, err
		}
		raw, exists := parentRoot[name]
		if !exists {
			continue
		}
		value, err := decodeNonNegativeInteger(
			raw,
			parent+"."+name,
		)
		return value, true, err
	}
	return 0, false, nil
}

func decodeNonNegativeInteger(
	raw json.RawMessage,
	name string,
) (int64, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return 0, fmt.Errorf(
			"%w: %s must be a non-negative integer",
			ErrUsageExtractionUnavailable,
			name,
		)
	}
	number, ok := decoded.(json.Number)
	if !ok {
		return 0, fmt.Errorf(
			"%w: %s must be a non-negative integer",
			ErrUsageExtractionUnavailable,
			name,
		)
	}
	value, err := number.Int64()
	if err != nil || value < 0 {
		return 0, fmt.Errorf(
			"%w: %s must be a non-negative integer",
			ErrUsageExtractionUnavailable,
			name,
		)
	}
	return value, nil
}
