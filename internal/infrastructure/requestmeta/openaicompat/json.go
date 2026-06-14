package openaicompat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

const maxJSONNestingDepth = 128

type jsonValueKind uint8

const (
	jsonValueNull jsonValueKind = iota
	jsonValueObject
	jsonValueArray
	jsonValueString
	jsonValueBoolean
	jsonValueNumber
)

type jsonValue struct {
	kind    jsonValueKind
	object  map[string]jsonValue
	array   []jsonValue
	text    string
	number  json.Number
	boolean bool
}

type requestInspection struct {
	clientModel                string
	capabilities               domain.CapabilitySet
	embeddingInputTokenCeiling int64
	imageGenerationUnits       int64
}

func inspect(
	apiFamily domain.APIFamily,
	endpointKind domain.EndpointKind,
	payload []byte,
) (requestInspection, error) {
	if apiFamily != domain.APIFamilyOpenAICompatible {
		return requestInspection{}, fmt.Errorf(
			"%w: unsupported API family %q",
			llmrequest.ErrStageContractViolation,
			apiFamily,
		)
	}
	if !supportedEndpointKind(endpointKind) {
		return requestInspection{}, fmt.Errorf(
			"%w: unsupported endpoint kind %q",
			llmrequest.ErrStageContractViolation,
			endpointKind,
		)
	}
	if !utf8.Valid(payload) {
		return requestInspection{}, fmt.Errorf(
			"%w: request body is not valid UTF-8",
			llmrequest.ErrInvalidJSON,
		)
	}

	root, err := decodeRootJSON(payload)
	if err != nil {
		return requestInspection{}, err
	}

	modelValue, exists := root.object["model"]
	if !exists {
		return requestInspection{}, llmrequest.ErrModelRequired
	}
	if modelValue.kind != jsonValueString {
		return requestInspection{}, fmt.Errorf(
			"%w: model must be a string",
			llmrequest.ErrInvalidJSON,
		)
	}
	if strings.TrimSpace(modelValue.text) == "" {
		return requestInspection{}, llmrequest.ErrModelRequired
	}

	if streamValue, exists := root.object["stream"]; exists {
		if streamValue.kind != jsonValueBoolean {
			return requestInspection{}, fmt.Errorf(
				"%w: stream must be a boolean",
				llmrequest.ErrInvalidJSON,
			)
		}
		if streamValue.boolean {
			return requestInspection{},
				llmrequest.ErrStreamingUnsupported
		}
	}

	capabilities := baseCapabilities(endpointKind)
	if endpointKind == domain.EndpointChat {
		detectChatCapabilities(root, &capabilities)
	}

	var embeddingInputTokenCeiling int64
	if endpointKind == domain.EndpointEmbeddings {
		embeddingInputTokenCeiling, err =
			inspectEmbeddingInput(root)
		if err != nil {
			return requestInspection{}, err
		}
	}

	var imageGenerationUnits int64
	if endpointKind == domain.EndpointImagesGeneration {
		imageGenerationUnits, err =
			inspectImageGenerationRequest(root)
		if err != nil {
			return requestInspection{}, err
		}
	}

	return requestInspection{
		clientModel:                modelValue.text,
		capabilities:               capabilities,
		embeddingInputTokenCeiling: embeddingInputTokenCeiling,
		imageGenerationUnits:       imageGenerationUnits,
	}, nil
}

func supportedEndpointKind(value domain.EndpointKind) bool {
	switch value {
	case domain.EndpointChat,
		domain.EndpointEmbeddings,
		domain.EndpointImagesGeneration:
		return true
	default:
		return false
	}
}

func baseCapabilities(value domain.EndpointKind) domain.CapabilitySet {
	switch value {
	case domain.EndpointChat:
		return domain.CapabilitySet{Chat: true}
	case domain.EndpointEmbeddings:
		return domain.CapabilitySet{Embeddings: true}
	case domain.EndpointImagesGeneration:
		return domain.CapabilitySet{ImagesGeneration: true}
	default:
		return domain.CapabilitySet{}
	}
}

func inspectImageGenerationRequest(
	root jsonValue,
) (int64, error) {
	prompt, exists := root.object["prompt"]
	if !exists {
		return 0, fmt.Errorf(
			"%w: image generation prompt is required",
			llmrequest.ErrInvalidJSON,
		)
	}
	if prompt.kind != jsonValueString ||
		strings.TrimSpace(prompt.text) == "" {
		return 0, fmt.Errorf(
			"%w: image generation prompt must be a non-empty string",
			llmrequest.ErrInvalidJSON,
		)
	}

	for _, name := range []string{
		"size",
		"quality",
		"response_format",
	} {
		value, exists := root.object[name]
		if !exists {
			continue
		}
		if value.kind != jsonValueString ||
			strings.TrimSpace(value.text) == "" {
			return 0, fmt.Errorf(
				"%w: image generation %s must be a non-empty string",
				llmrequest.ErrInvalidJSON,
				name,
			)
		}
	}

	n, exists := root.object["n"]
	if !exists {
		return 1, nil
	}
	if n.kind != jsonValueNumber {
		return 0, fmt.Errorf(
			"%w: image generation n must be an integer",
			llmrequest.ErrInvalidJSON,
		)
	}
	units, err := strconv.ParseInt(
		n.number.String(),
		10,
		64,
	)
	if err != nil || units < 1 || units > 10 {
		return 0, fmt.Errorf(
			"%w: image generation n must be between 1 and 10",
			llmrequest.ErrInvalidJSON,
		)
	}
	return units, nil
}

func inspectEmbeddingInput(root jsonValue) (int64, error) {
	input, exists := root.object["input"]
	if !exists {
		return 0, fmt.Errorf(
			"%w: embeddings input is required",
			llmrequest.ErrInvalidJSON,
		)
	}

	switch input.kind {
	case jsonValueString:
		if input.text == "" {
			return 0, fmt.Errorf(
				"%w: embeddings input string is empty",
				llmrequest.ErrInvalidJSON,
			)
		}
		return int64(len([]byte(input.text))), nil
	case jsonValueArray:
		return inspectEmbeddingInputArray(input.array)
	default:
		return 0, fmt.Errorf(
			"%w: embeddings input has unsupported type",
			llmrequest.ErrInvalidJSON,
		)
	}
}

func inspectEmbeddingInputArray(
	values []jsonValue,
) (int64, error) {
	if len(values) == 0 {
		return 0, fmt.Errorf(
			"%w: embeddings input array is empty",
			llmrequest.ErrInvalidJSON,
		)
	}

	switch values[0].kind {
	case jsonValueString:
		return embeddingStringArrayCeiling(values)
	case jsonValueNumber:
		return embeddingTokenArrayCeiling(values)
	case jsonValueArray:
		return embeddingTokenBatchCeiling(values)
	default:
		return 0, fmt.Errorf(
			"%w: embeddings input array has unsupported element type",
			llmrequest.ErrInvalidJSON,
		)
	}
}

func embeddingStringArrayCeiling(
	values []jsonValue,
) (int64, error) {
	var total int64
	for _, value := range values {
		if value.kind != jsonValueString || value.text == "" {
			return 0, fmt.Errorf(
				"%w: embeddings input must be non-empty strings",
				llmrequest.ErrInvalidJSON,
			)
		}
		var err error
		total, err = addEmbeddingInputUnits(
			total,
			int64(len([]byte(value.text))),
		)
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

func embeddingTokenArrayCeiling(
	values []jsonValue,
) (int64, error) {
	for _, value := range values {
		if err := validateEmbeddingToken(value); err != nil {
			return 0, err
		}
	}
	return int64(len(values)), nil
}

func embeddingTokenBatchCeiling(
	values []jsonValue,
) (int64, error) {
	var total int64
	for _, value := range values {
		if value.kind != jsonValueArray ||
			len(value.array) == 0 {
			return 0, fmt.Errorf(
				"%w: embeddings token arrays must be non-empty",
				llmrequest.ErrInvalidJSON,
			)
		}
		units, err := embeddingTokenArrayCeiling(value.array)
		if err != nil {
			return 0, err
		}
		total, err = addEmbeddingInputUnits(total, units)
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

func validateEmbeddingToken(value jsonValue) error {
	if value.kind != jsonValueNumber {
		return fmt.Errorf(
			"%w: embeddings token IDs must be integers",
			llmrequest.ErrInvalidJSON,
		)
	}
	token, err := strconv.ParseInt(
		value.number.String(),
		10,
		64,
	)
	if err != nil || token < 0 {
		return fmt.Errorf(
			"%w: embeddings token IDs must be non-negative integers",
			llmrequest.ErrInvalidJSON,
		)
	}
	return nil
}

func addEmbeddingInputUnits(left, right int64) (int64, error) {
	const maxInt64 = int64(^uint64(0) >> 1)
	if right < 0 || left > maxInt64-right {
		return 0, fmt.Errorf(
			"%w: embeddings input size overflow",
			llmrequest.ErrInvalidJSON,
		)
	}
	return left + right, nil
}

func detectChatCapabilities(
	root jsonValue,
	capabilities *domain.CapabilitySet,
) {
	if _, exists := root.object["tools"]; exists {
		capabilities.Tools = true
	}
	if _, exists := root.object["tool_choice"]; exists {
		capabilities.ToolChoice = true
	}
	if responseFormat, exists := root.object["response_format"]; exists {
		capabilities.ResponseFormat = true
		if responseFormat.kind == jsonValueObject {
			if typeValue, ok := responseFormat.object["type"]; ok &&
				typeValue.kind == jsonValueString &&
				typeValue.text == "json_schema" {
				capabilities.JSONSchema = true
			}
			if _, ok := responseFormat.object["json_schema"]; ok {
				capabilities.JSONSchema = true
			}
		}
	}
	if _, exists := root.object["reasoning_effort"]; exists {
		capabilities.Reasoning = true
	}

	messages, exists := root.object["messages"]
	if !exists || messages.kind != jsonValueArray {
		return
	}
	for _, message := range messages.array {
		if message.kind != jsonValueObject {
			continue
		}
		content, exists := message.object["content"]
		if !exists || content.kind != jsonValueArray {
			continue
		}
		for _, part := range content.array {
			detectMediaPart(part, capabilities)
		}
	}
}

func detectMediaPart(
	part jsonValue,
	capabilities *domain.CapabilitySet,
) {
	if part.kind != jsonValueObject {
		return
	}

	if typeValue, exists := part.object["type"]; exists &&
		typeValue.kind == jsonValueString {
		setMediaCapability(typeValue.text, capabilities)
	}
	for key := range part.object {
		setMediaCapability(key, capabilities)
	}
}

func setMediaCapability(
	value string,
	capabilities *domain.CapabilitySet,
) {
	switch value {
	case "image_url", "input_image":
		capabilities.ImageInput = true
	case "input_audio", "audio":
		capabilities.AudioInput = true
	case "file", "file_id", "input_file", "document":
		capabilities.FileInput = true
	case "video_url", "input_video":
		capabilities.VideoInput = true
	}
}

func decodeRootJSON(payload []byte) (jsonValue, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()

	root, err := decodeJSONValue(decoder, 1)
	if err != nil {
		return jsonValue{}, err
	}
	if root.kind != jsonValueObject {
		return jsonValue{}, fmt.Errorf(
			"%w: top-level JSON value must be an object",
			llmrequest.ErrInvalidJSON,
		)
	}

	_, err = decoder.Token()
	switch {
	case errors.Is(err, io.EOF):
		return root, nil
	case err == nil:
		return jsonValue{}, fmt.Errorf(
			"%w: trailing JSON value",
			llmrequest.ErrInvalidJSON,
		)
	default:
		return jsonValue{}, invalidJSONError(
			"read trailing data",
			err,
		)
	}
}

func decodeJSONValue(
	decoder *json.Decoder,
	depth int,
) (jsonValue, error) {
	token, err := decoder.Token()
	if err != nil {
		return jsonValue{}, invalidJSONError("read value", err)
	}

	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			return decodeJSONObject(decoder, depth)
		case '[':
			return decodeJSONArray(decoder, depth)
		default:
			return jsonValue{}, fmt.Errorf(
				"%w: unexpected delimiter %q",
				llmrequest.ErrInvalidJSON,
				value,
			)
		}
	case string:
		return jsonValue{
			kind: jsonValueString,
			text: value,
		}, nil
	case bool:
		return jsonValue{
			kind:    jsonValueBoolean,
			boolean: value,
		}, nil
	case nil:
		return jsonValue{kind: jsonValueNull}, nil
	case json.Number:
		return jsonValue{
			kind:   jsonValueNumber,
			number: value,
		}, nil
	case float64:
		return jsonValue{
			kind: jsonValueNumber,
			number: json.Number(strconv.FormatFloat(
				value,
				'f',
				-1,
				64,
			)),
		}, nil
	default:
		return jsonValue{}, fmt.Errorf(
			"%w: unsupported JSON token",
			llmrequest.ErrInvalidJSON,
		)
	}
}

func decodeJSONObject(
	decoder *json.Decoder,
	depth int,
) (jsonValue, error) {
	if depth > maxJSONNestingDepth {
		return jsonValue{}, fmt.Errorf(
			"%w: JSON nesting depth exceeds %d",
			llmrequest.ErrInvalidJSON,
			maxJSONNestingDepth,
		)
	}

	object := make(map[string]jsonValue)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return jsonValue{}, invalidJSONError(
				"read object key",
				err,
			)
		}
		key, ok := keyToken.(string)
		if !ok {
			return jsonValue{}, fmt.Errorf(
				"%w: object key is not a string",
				llmrequest.ErrInvalidJSON,
			)
		}
		if _, exists := object[key]; exists {
			return jsonValue{}, fmt.Errorf(
				"%w: duplicate object key %q",
				llmrequest.ErrInvalidJSON,
				key,
			)
		}

		value, err := decodeJSONValue(decoder, depth+1)
		if err != nil {
			return jsonValue{}, err
		}
		object[key] = value
	}

	closing, err := decoder.Token()
	if err != nil {
		return jsonValue{}, invalidJSONError(
			"close object",
			err,
		)
	}
	if closing != json.Delim('}') {
		return jsonValue{}, fmt.Errorf(
			"%w: object closing delimiter mismatch",
			llmrequest.ErrInvalidJSON,
		)
	}
	return jsonValue{
		kind:   jsonValueObject,
		object: object,
	}, nil
}

func decodeJSONArray(
	decoder *json.Decoder,
	depth int,
) (jsonValue, error) {
	if depth > maxJSONNestingDepth {
		return jsonValue{}, fmt.Errorf(
			"%w: JSON nesting depth exceeds %d",
			llmrequest.ErrInvalidJSON,
			maxJSONNestingDepth,
		)
	}

	values := make([]jsonValue, 0)
	for decoder.More() {
		value, err := decodeJSONValue(decoder, depth+1)
		if err != nil {
			return jsonValue{}, err
		}
		values = append(values, value)
	}

	closing, err := decoder.Token()
	if err != nil {
		return jsonValue{}, invalidJSONError(
			"close array",
			err,
		)
	}
	if closing != json.Delim(']') {
		return jsonValue{}, fmt.Errorf(
			"%w: array closing delimiter mismatch",
			llmrequest.ErrInvalidJSON,
		)
	}
	return jsonValue{
		kind:  jsonValueArray,
		array: values,
	}, nil
}

func invalidJSONError(action string, err error) error {
	return fmt.Errorf(
		"%w: %s: %v",
		llmrequest.ErrInvalidJSON,
		action,
		err,
	)
}
