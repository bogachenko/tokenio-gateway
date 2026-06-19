package gemininative

import (
	"bytes"
	"encoding/json"
	"errors"
)

var (
	ErrUsageNotFound = errors.New("gemini usage not found")
	ErrInvalidUsage  = errors.New("invalid gemini usage")
)

type Usage struct {
	InputTokens  int64
	OutputTokens int64
}

func ExtractUsage(body []byte) (Usage, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil || root == nil {
		return Usage{}, ErrInvalidUsage
	}

	rawMetadata, ok := root["usageMetadata"]
	if !ok {
		return Usage{}, ErrUsageNotFound
	}

	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(rawMetadata, &metadata); err != nil || metadata == nil {
		return Usage{}, ErrInvalidUsage
	}

	inputTokens, ok, err := readOptionalInt64(metadata, "promptTokenCount")
	if err != nil {
		return Usage{}, err
	}
	if !ok {
		return Usage{}, ErrInvalidUsage
	}

	outputTokens, ok, err := readOptionalInt64(metadata, "candidatesTokenCount")
	if err != nil {
		return Usage{}, err
	}
	if !ok {
		totalTokens, totalOK, totalErr := readOptionalInt64(metadata, "totalTokenCount")
		if totalErr != nil {
			return Usage{}, totalErr
		}
		if totalOK {
			if totalTokens < inputTokens {
				return Usage{}, ErrInvalidUsage
			}
			outputTokens = totalTokens - inputTokens
		}
	}

	return Usage{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}, nil
}

func readOptionalInt64(
	metadata map[string]json.RawMessage,
	key string,
) (int64, bool, error) {
	raw, ok := metadata[key]
	if !ok {
		return 0, false, nil
	}
	value, err := readJSONInt64(raw)
	if err != nil {
		return 0, false, err
	}
	return value, true, nil
}

func readJSONInt64(raw json.RawMessage) (int64, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 ||
		trimmed[0] == '"' ||
		trimmed[0] == '-' ||
		bytes.ContainsAny(trimmed, ".eE") {
		return 0, ErrInvalidUsage
	}

	var value int64
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return 0, ErrInvalidUsage
	}
	if value < 0 {
		return 0, ErrInvalidUsage
	}
	return value, nil
}
