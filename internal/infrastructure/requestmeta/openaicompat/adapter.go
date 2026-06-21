package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type Adapter struct{}

var (
	_ llmrequest.RequestParser      = (*Adapter)(nil)
	_ llmrequest.CapabilityDetector = (*Adapter)(nil)
)

func NewAdapter() *Adapter {
	return &Adapter{}
}

func (a *Adapter) Parse(
	ctx context.Context,
	input llmrequest.ParseInput,
) (llmrequest.ParsedRequest, error) {
	if a == nil {
		return llmrequest.ParsedRequest{}, fmt.Errorf(
			"%w: nil OpenAI-compatible request metadata adapter",
			llmrequest.ErrStageContractViolation,
		)
	}
	if err := validateContext(ctx); err != nil {
		return llmrequest.ParsedRequest{}, err
	}
	if input.APIFamily == domain.APIFamilyGeminiNative {
		return parseGeminiNative(input)
	}
	if input.APIFamily == domain.APIFamilyOllamaNative {
		return parseOllamaNative(input)
	}
	if input.APIFamily == domain.APIFamilyAnthropicNative {
		return parseAnthropicNative(input)
	}
	inspection, err := inspect(
		input.APIFamily,
		input.EndpointKind,
		input.Payload,
	)
	if err != nil {
		return llmrequest.ParsedRequest{}, err
	}
	return llmrequest.ParsedRequest{
		ClientModel: inspection.clientModel,
	}, nil
}

func (a *Adapter) Detect(
	ctx context.Context,
	input llmrequest.CapabilityInput,
) (domain.CapabilitySet, error) {
	if a == nil {
		return domain.CapabilitySet{}, fmt.Errorf(
			"%w: nil OpenAI-compatible request metadata adapter",
			llmrequest.ErrStageContractViolation,
		)
	}
	if err := validateContext(ctx); err != nil {
		return domain.CapabilitySet{}, err
	}
	if strings.TrimSpace(input.ClientModel) == "" {
		return domain.CapabilitySet{}, fmt.Errorf(
			"%w: blank parsed client model",
			llmrequest.ErrStageContractViolation,
		)
	}
	if input.APIFamily == domain.APIFamilyGeminiNative {
		if input.ClientModel != input.PathModel {
			return domain.CapabilitySet{}, fmt.Errorf(
				"%w: parsed Gemini path model mismatch",
				llmrequest.ErrStageContractViolation,
			)
		}
		return geminiNativeCapabilities(input.EndpointKind)
	}
	if input.APIFamily == domain.APIFamilyOllamaNative {
		parsed, err := parseOllamaNative(llmrequest.ParseInput{
			APIFamily:    input.APIFamily,
			EndpointKind: input.EndpointKind,
			Payload:      input.Payload,
		})
		if err != nil {
			return domain.CapabilitySet{}, err
		}
		if parsed.ClientModel != input.ClientModel {
			return domain.CapabilitySet{}, fmt.Errorf(
				"%w: parsed Ollama body model mismatch",
				llmrequest.ErrStageContractViolation,
			)
		}
		return ollamaNativeCapabilities(input.EndpointKind)
	}
	if input.APIFamily == domain.APIFamilyAnthropicNative {
		parsed, err := parseAnthropicNative(llmrequest.ParseInput{
			APIFamily:    input.APIFamily,
			EndpointKind: input.EndpointKind,
			Payload:      input.Payload,
		})
		if err != nil {
			return domain.CapabilitySet{}, err
		}
		if parsed.ClientModel != input.ClientModel {
			return domain.CapabilitySet{}, fmt.Errorf(
				"%w: parsed Anthropic body model mismatch",
				llmrequest.ErrStageContractViolation,
			)
		}
		return anthropicNativeCapabilities(input.EndpointKind)
	}
	inspection, err := inspect(
		input.APIFamily,
		input.EndpointKind,
		input.Payload,
	)
	if err != nil {
		return domain.CapabilitySet{}, err
	}
	if inspection.clientModel != input.ClientModel {
		return domain.CapabilitySet{}, fmt.Errorf(
			"%w: parsed client model mismatch",
			llmrequest.ErrStageContractViolation,
		)
	}
	return inspection.capabilities, nil
}

func parseGeminiNative(input llmrequest.ParseInput) (llmrequest.ParsedRequest, error) {
	if strings.TrimSpace(input.PathModel) == "" {
		return llmrequest.ParsedRequest{}, fmt.Errorf(
			"%w: Gemini path model is required",
			llmrequest.ErrInvalidInput,
		)
	}
	if _, err := geminiNativeCapabilities(input.EndpointKind); err != nil {
		return llmrequest.ParsedRequest{}, err
	}
	return llmrequest.ParsedRequest{
		ClientModel: input.PathModel,
	}, nil
}

func parseOllamaNative(input llmrequest.ParseInput) (llmrequest.ParsedRequest, error) {
	if _, err := ollamaNativeCapabilities(input.EndpointKind); err != nil {
		return llmrequest.ParsedRequest{}, err
	}
	if len(input.Payload) == 0 {
		return llmrequest.ParsedRequest{}, llmrequest.ErrInvalidJSON
	}

	var request struct {
		Model  string `json:"model"`
		Stream *bool  `json:"stream"`
	}
	decoder := json.NewDecoder(bytes.NewReader(input.Payload))
	if err := decoder.Decode(&request); err != nil {
		return llmrequest.ParsedRequest{}, fmt.Errorf(
			"%w: invalid Ollama JSON body",
			llmrequest.ErrInvalidJSON,
		)
	}
	if decoder.Decode(&struct{}{}) == nil {
		return llmrequest.ParsedRequest{}, fmt.Errorf(
			"%w: trailing Ollama JSON value",
			llmrequest.ErrInvalidJSON,
		)
	}
	if strings.TrimSpace(request.Model) == "" {
		return llmrequest.ParsedRequest{}, llmrequest.ErrModelRequired
	}
	if request.Stream != nil && *request.Stream {
		return llmrequest.ParsedRequest{}, llmrequest.ErrStreamingUnsupported
	}
	return llmrequest.ParsedRequest{
		ClientModel: request.Model,
	}, nil
}

func parseAnthropicNative(input llmrequest.ParseInput) (llmrequest.ParsedRequest, error) {
	if _, err := anthropicNativeCapabilities(input.EndpointKind); err != nil {
		return llmrequest.ParsedRequest{}, err
	}
	if len(input.Payload) == 0 {
		return llmrequest.ParsedRequest{}, llmrequest.ErrInvalidJSON
	}

	var request struct {
		Model string `json:"model"`
	}
	decoder := json.NewDecoder(bytes.NewReader(input.Payload))
	if err := decoder.Decode(&request); err != nil {
		return llmrequest.ParsedRequest{}, fmt.Errorf(
			"%w: invalid Anthropic JSON body",
			llmrequest.ErrInvalidJSON,
		)
	}
	if decoder.Decode(&struct{}{}) == nil {
		return llmrequest.ParsedRequest{}, fmt.Errorf(
			"%w: trailing Anthropic JSON value",
			llmrequest.ErrInvalidJSON,
		)
	}
	if strings.TrimSpace(request.Model) == "" {
		return llmrequest.ParsedRequest{}, llmrequest.ErrModelRequired
	}
	return llmrequest.ParsedRequest{
		ClientModel: request.Model,
	}, nil
}

func geminiNativeCapabilities(endpoint domain.EndpointKind) (domain.CapabilitySet, error) {
	switch endpoint {
	case domain.EndpointChat:
		return domain.CapabilitySet{Chat: true}, nil
	case domain.EndpointEmbeddings:
		return domain.CapabilitySet{Embeddings: true}, nil
	default:
		return domain.CapabilitySet{}, fmt.Errorf(
			"%w: unsupported Gemini native endpoint",
			llmrequest.ErrInvalidInput,
		)
	}
}

func ollamaNativeCapabilities(endpoint domain.EndpointKind) (domain.CapabilitySet, error) {
	switch endpoint {
	case domain.EndpointChat:
		return domain.CapabilitySet{Chat: true}, nil
	case domain.EndpointEmbeddings:
		return domain.CapabilitySet{Embeddings: true}, nil
	default:
		return domain.CapabilitySet{}, fmt.Errorf(
			"%w: unsupported Ollama native endpoint",
			llmrequest.ErrInvalidInput,
		)
	}
}

func anthropicNativeCapabilities(endpoint domain.EndpointKind) (domain.CapabilitySet, error) {
	switch endpoint {
	case domain.EndpointChat:
		return domain.CapabilitySet{Chat: true}, nil
	default:
		return domain.CapabilitySet{}, fmt.Errorf(
			"%w: unsupported Anthropic native endpoint",
			llmrequest.ErrInvalidInput,
		)
	}
}

func validateContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf(
			"%w: nil context",
			llmrequest.ErrInvalidInput,
		)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
