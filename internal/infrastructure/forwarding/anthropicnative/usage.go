package anthropicnative

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

var (
	ErrUsageNotFound = errors.New("anthropic usage not found")
	ErrInvalidUsage  = errors.New("invalid anthropic usage")
)

type Usage struct {
	InputTokens  int64
	OutputTokens int64
}

func ExtractUsage(body []byte) (Usage, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	var payload struct {
		Usage *struct {
			InputTokens  json.RawMessage `json:"input_tokens"`
			OutputTokens json.RawMessage `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := decoder.Decode(&payload); err != nil {
		return Usage{}, fmt.Errorf("%w: %v", ErrInvalidUsage, err)
	}
	if payload.Usage == nil ||
		len(payload.Usage.InputTokens) == 0 ||
		len(payload.Usage.OutputTokens) == 0 {
		return Usage{}, ErrUsageNotFound
	}

	inputTokens, err := parseTokenCount(payload.Usage.InputTokens)
	if err != nil {
		return Usage{}, err
	}
	outputTokens, err := parseTokenCount(payload.Usage.OutputTokens)
	if err != nil {
		return Usage{}, err
	}

	return Usage{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}, nil
}

func parseTokenCount(raw json.RawMessage) (int64, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] == '"' {
		return 0, fmt.Errorf("%w: token count must be a JSON number", ErrInvalidUsage)
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()

	var value json.Number
	if err := decoder.Decode(&value); err != nil {
		return 0, fmt.Errorf("%w: token count must be a JSON number", ErrInvalidUsage)
	}
	count, err := value.Int64()
	if err != nil || count < 0 {
		return 0, fmt.Errorf("%w: token count must be a non-negative integer", ErrInvalidUsage)
	}
	return count, nil
}

func (usage Usage) ForwardUsage() *ports.ForwardUsage {
	return &ports.ForwardUsage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
	}
}
