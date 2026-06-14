package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

var ErrEstimationUnavailable = errors.New(
	"OpenAI-compatible token estimation unavailable",
)

const structuralByteCeilingConfidence = "conservative_structural_byte_ceiling"

type TokenEstimator struct{}

var _ ports.TokenEstimator = (*TokenEstimator)(nil)

func NewTokenEstimator() *TokenEstimator {
	return &TokenEstimator{}
}

func (e *TokenEstimator) Estimate(
	ctx context.Context,
	request ports.TokenEstimateRequest,
) (ports.TokenEstimate, error) {
	if e == nil {
		return ports.TokenEstimate{}, fmt.Errorf(
			"%w: nil estimator",
			ErrEstimationUnavailable,
		)
	}
	if err := validateEstimatorContext(ctx); err != nil {
		return ports.TokenEstimate{}, err
	}
	if request.RequestBody == nil ||
		strings.TrimSpace(request.ClientModel) == "" {
		return ports.TokenEstimate{}, fmt.Errorf(
			"%w: invalid request contract",
			ErrEstimationUnavailable,
		)
	}

	inspection, err := inspect(
		request.APIFamily,
		request.EndpointKind,
		request.RequestBody,
	)
	if err != nil {
		return ports.TokenEstimate{}, err
	}
	if inspection.clientModel != request.ClientModel ||
		inspection.capabilities != request.RequestedCapabilities {
		return ports.TokenEstimate{}, fmt.Errorf(
			"%w: request metadata mismatch",
			ErrEstimationUnavailable,
		)
	}

	root, err := decodeEstimatorRoot(request.RequestBody)
	if err != nil {
		return ports.TokenEstimate{}, err
	}

	usage, err := estimateStructuralUsage(
		request,
		root,
		inspection,
	)
	if err != nil {
		return ports.TokenEstimate{}, err
	}
	return ports.TokenEstimate{
		Usage:      usage,
		Confidence: structuralByteCeilingConfidence,
	}, nil
}

func validateEstimatorContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf(
			"%w: nil context",
			ErrEstimationUnavailable,
		)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func decodeEstimatorRoot(
	payload []byte,
) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()

	var root map[string]json.RawMessage
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf(
			"%w: decode request: %v",
			ErrEstimationUnavailable,
			err,
		)
	}
	if root == nil {
		return nil, fmt.Errorf(
			"%w: request root is not an object",
			ErrEstimationUnavailable,
		)
	}
	return root, nil
}

func estimateStructuralUsage(
	request ports.TokenEstimateRequest,
	root map[string]json.RawMessage,
	inspection requestInspection,
) (domain.TokenUsage, error) {
	switch request.EndpointKind {
	case domain.EndpointChat:
		return estimateChatUsage(request, root)
	case domain.EndpointEmbeddings:
		return domain.TokenUsage{
			InputTokens: inspection.
				embeddingInputTokenCeiling,
		}, nil
	case domain.EndpointImagesGeneration:
		units, err := positiveIntegerField(root, "n", 1)
		if err != nil {
			return domain.TokenUsage{}, err
		}
		return domain.TokenUsage{
			ImageGenerationUnits: units,
		}, nil
	default:
		return domain.TokenUsage{}, fmt.Errorf(
			"%w: unsupported endpoint %q",
			ErrEstimationUnavailable,
			request.EndpointKind,
		)
	}
}

func estimateChatUsage(
	request ports.TokenEstimateRequest,
	root map[string]json.RawMessage,
) (domain.TokenUsage, error) {
	outputLimit, found, err := maximumPositiveIntegerFields(
		root,
		"max_tokens",
		"max_completion_tokens",
	)
	if err != nil {
		return domain.TokenUsage{}, err
	}
	if !found {
		outputLimit = request.DefaultMaxOutputTokens
	}
	if outputLimit <= 0 {
		return domain.TokenUsage{}, fmt.Errorf(
			"%w: positive output limit is required",
			ErrEstimationUnavailable,
		)
	}

	usage := structuralInputUsage(request)
	usage.OutputTokens = outputLimit
	if request.RequestedCapabilities.Reasoning {
		usage.ReasoningTokens = outputLimit
	}
	return usage, nil
}

func structuralInputUsage(
	request ports.TokenEstimateRequest,
) domain.TokenUsage {
	ceiling := int64(len(request.RequestBody))
	usage := domain.TokenUsage{
		InputTokens: ceiling,
	}
	capabilities := request.RequestedCapabilities
	if capabilities.ImageInput {
		usage.ImageInputTokens = ceiling
	}
	if capabilities.AudioInput {
		usage.AudioInputTokens = ceiling
	}
	if capabilities.FileInput {
		usage.FileInputTokens = ceiling
	}
	if capabilities.VideoInput {
		usage.VideoInputTokens = ceiling
	}
	return usage
}

func positiveIntegerField(
	root map[string]json.RawMessage,
	name string,
	defaultValue int64,
) (int64, error) {
	raw, exists := root[name]
	if !exists {
		return defaultValue, nil
	}
	value, err := decodePositiveInteger(raw, name)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func maximumPositiveIntegerFields(
	root map[string]json.RawMessage,
	names ...string,
) (int64, bool, error) {
	var maximum int64
	var found bool
	for _, name := range names {
		raw, exists := root[name]
		if !exists {
			continue
		}
		value, err := decodePositiveInteger(raw, name)
		if err != nil {
			return 0, false, err
		}
		if !found || value > maximum {
			maximum = value
		}
		found = true
	}
	return maximum, found, nil
}

func decodePositiveInteger(
	raw json.RawMessage,
	name string,
) (int64, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return 0, fmt.Errorf(
			"%w: %s must be a positive integer",
			ErrEstimationUnavailable,
			name,
		)
	}
	number, ok := decoded.(json.Number)
	if !ok {
		return 0, fmt.Errorf(
			"%w: %s must be a positive integer",
			ErrEstimationUnavailable,
			name,
		)
	}
	value, err := number.Int64()
	if err != nil || value <= 0 || value == math.MaxInt64 {
		return 0, fmt.Errorf(
			"%w: %s must be a positive integer",
			ErrEstimationUnavailable,
			name,
		)
	}
	return value, nil
}
