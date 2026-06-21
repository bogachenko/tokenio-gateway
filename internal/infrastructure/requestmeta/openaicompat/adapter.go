package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	metadata "github.com/bogachenko/tokenio-gateway/internal/ports/llmrequestmetadata"
)

type Adapter struct{}

var (
	_ metadata.RequestParser      = (*Adapter)(nil)
	_ metadata.CapabilityDetector = (*Adapter)(nil)
)

func NewAdapter() *Adapter {
	return &Adapter{}
}

func (a *Adapter) Parse(
	ctx context.Context,
	input metadata.ParseInput,
) (metadata.ParsedRequest, error) {
	if a == nil {
		return metadata.ParsedRequest{}, fmt.Errorf(
			"%w: nil OpenAI-compatible request metadata adapter",
			metadata.ErrStageContractViolation,
		)
	}
	if err := validateContext(ctx); err != nil {
		return metadata.ParsedRequest{}, err
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
		return metadata.ParsedRequest{}, err
	}
	return metadata.ParsedRequest{
		ClientModel: inspection.clientModel,
	}, nil
}

func (a *Adapter) Detect(
	ctx context.Context,
	input metadata.CapabilityInput,
) (domain.CapabilitySet, error) {
	if a == nil {
		return domain.CapabilitySet{}, fmt.Errorf(
			"%w: nil OpenAI-compatible request metadata adapter",
			metadata.ErrStageContractViolation,
		)
	}
	if err := validateContext(ctx); err != nil {
		return domain.CapabilitySet{}, err
	}
	if strings.TrimSpace(input.ClientModel) == "" {
		return domain.CapabilitySet{}, fmt.Errorf(
			"%w: blank parsed client model",
			metadata.ErrStageContractViolation,
		)
	}
	if input.APIFamily == domain.APIFamilyGeminiNative {
		if input.ClientModel != input.PathModel {
			return domain.CapabilitySet{}, fmt.Errorf(
				"%w: parsed Gemini path model mismatch",
				metadata.ErrStageContractViolation,
			)
		}
		return geminiNativeCapabilities(input.EndpointKind)
	}
	if input.APIFamily == domain.APIFamilyOllamaNative {
		parsed, err := parseOllamaNative(metadata.ParseInput{
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
				metadata.ErrStageContractViolation,
			)
		}
		return ollamaNativeCapabilities(input.EndpointKind)
	}
	if input.APIFamily == domain.APIFamilyAnthropicNative {
		parsed, err := parseAnthropicNative(metadata.ParseInput{
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
				metadata.ErrStageContractViolation,
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
			metadata.ErrStageContractViolation,
		)
	}
	return inspection.capabilities, nil
}

func parseGeminiNative(input metadata.ParseInput) (metadata.ParsedRequest, error) {
	if strings.TrimSpace(input.PathModel) == "" {
		return metadata.ParsedRequest{}, fmt.Errorf(
			"%w: Gemini path model is required",
			metadata.ErrInvalidInput,
		)
	}
	if _, err := geminiNativeCapabilities(input.EndpointKind); err != nil {
		return metadata.ParsedRequest{}, err
	}
	return metadata.ParsedRequest{
		ClientModel: input.PathModel,
	}, nil
}

func parseOllamaNative(input metadata.ParseInput) (metadata.ParsedRequest, error) {
	if _, err := ollamaNativeCapabilities(input.EndpointKind); err != nil {
		return metadata.ParsedRequest{}, err
	}
	if len(input.Payload) == 0 {
		return metadata.ParsedRequest{}, metadata.ErrInvalidJSON
	}

	var request struct {
		Model  string `json:"model"`
		Stream *bool  `json:"stream"`
	}
	decoder := json.NewDecoder(bytes.NewReader(input.Payload))
	if err := decoder.Decode(&request); err != nil {
		return metadata.ParsedRequest{}, fmt.Errorf(
			"%w: invalid Ollama JSON body",
			metadata.ErrInvalidJSON,
		)
	}
	if decoder.Decode(&struct{}{}) == nil {
		return metadata.ParsedRequest{}, fmt.Errorf(
			"%w: trailing Ollama JSON value",
			metadata.ErrInvalidJSON,
		)
	}
	if strings.TrimSpace(request.Model) == "" {
		return metadata.ParsedRequest{}, metadata.ErrModelRequired
	}
	if request.Stream != nil && *request.Stream {
		return metadata.ParsedRequest{}, metadata.ErrStreamingUnsupported
	}
	return metadata.ParsedRequest{
		ClientModel: request.Model,
	}, nil
}

func parseAnthropicNative(input metadata.ParseInput) (metadata.ParsedRequest, error) {
	if _, err := anthropicNativeCapabilities(input.EndpointKind); err != nil {
		return metadata.ParsedRequest{}, err
	}
	if len(input.Payload) == 0 {
		return metadata.ParsedRequest{}, metadata.ErrInvalidJSON
	}

	root, err := decodeRootJSON(input.Payload)
	if err != nil {
		return metadata.ParsedRequest{}, err
	}
	modelValue, exists := root.object["model"]
	if !exists {
		return metadata.ParsedRequest{}, metadata.ErrModelRequired
	}
	if modelValue.kind != jsonValueString {
		return metadata.ParsedRequest{}, fmt.Errorf(
			"%w: Anthropic model must be a string",
			metadata.ErrInvalidJSON,
		)
	}
	if strings.TrimSpace(modelValue.text) == "" {
		return metadata.ParsedRequest{}, metadata.ErrModelRequired
	}
	return metadata.ParsedRequest{
		ClientModel: modelValue.text,
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
			metadata.ErrInvalidInput,
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
			metadata.ErrInvalidInput,
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
			metadata.ErrInvalidInput,
		)
	}
}

func validateContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf(
			"%w: nil context",
			metadata.ErrInvalidInput,
		)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
