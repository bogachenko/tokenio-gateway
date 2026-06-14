package openaicompat

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestTokenEstimatorChatUsesStructuralByteCeiling(
	t *testing.T,
) {
	estimator := NewTokenEstimator()
	body := []byte(`{
		"model":"model-1",
		"max_completion_tokens":120,
		"messages":[{
			"content":[
				{"type":"text","text":"hello"},
				{"type":"image_url","image_url":{"url":"x"}},
				{"type":"input_audio","input_audio":{"data":"x"}},
				{"type":"input_file","file_id":"file-1"},
				{"type":"input_video","video_url":"x"}
			]
		}]
	}`)
	original := append([]byte(nil), body...)
	capabilities := domain.CapabilitySet{
		Chat:       true,
		ImageInput: true,
		AudioInput: true,
		FileInput:  true,
		VideoInput: true,
	}

	result, err := estimator.Estimate(
		context.Background(),
		ports.TokenEstimateRequest{
			APIFamily: domain.
				APIFamilyOpenAICompatible,
			EndpointKind:           domain.EndpointChat,
			ClientModel:            "model-1",
			RequestBody:            body,
			DefaultMaxOutputTokens: 999,
			RequestedCapabilities:  capabilities,
		},
	)
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}

	ceiling := int64(len(body))
	want := domain.TokenUsage{
		InputTokens:      ceiling,
		OutputTokens:     120,
		ImageInputTokens: ceiling,
		AudioInputTokens: ceiling,
		FileInputTokens:  ceiling,
		VideoInputTokens: ceiling,
	}
	if result.Usage != want {
		t.Fatalf("usage = %+v, want %+v", result.Usage, want)
	}
	if result.Confidence != structuralByteCeilingConfidence {
		t.Fatalf("confidence = %q", result.Confidence)
	}
	if !bytes.Equal(body, original) {
		t.Fatalf("request body mutated")
	}
}

func TestTokenEstimatorChatUsesLargestExplicitOutputLimit(
	t *testing.T,
) {
	estimator := NewTokenEstimator()
	body := []byte(
		`{"model":"model-1","max_tokens":40,` +
			`"max_completion_tokens":80,"reasoning_effort":"high"}`,
	)

	result, err := estimator.Estimate(
		context.Background(),
		ports.TokenEstimateRequest{
			APIFamily:              domain.APIFamilyOpenAICompatible,
			EndpointKind:           domain.EndpointChat,
			ClientModel:            "model-1",
			RequestBody:            body,
			DefaultMaxOutputTokens: 10,
			RequestedCapabilities: domain.CapabilitySet{
				Chat:      true,
				Reasoning: true,
			},
		},
	)
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if result.Usage.OutputTokens != 80 ||
		result.Usage.ReasoningTokens != 80 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestTokenEstimatorChatUsesRouteDefaultOutputLimit(
	t *testing.T,
) {
	estimator := NewTokenEstimator()
	body := []byte(`{"model":"model-1","messages":[]}`)

	result, err := estimator.Estimate(
		context.Background(),
		ports.TokenEstimateRequest{
			APIFamily:              domain.APIFamilyOpenAICompatible,
			EndpointKind:           domain.EndpointChat,
			ClientModel:            "model-1",
			RequestBody:            body,
			DefaultMaxOutputTokens: 64,
			RequestedCapabilities: domain.CapabilitySet{
				Chat: true,
			},
		},
	)
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if result.Usage.OutputTokens != 64 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestTokenEstimatorRejectsMissingOutputLimit(
	t *testing.T,
) {
	estimator := NewTokenEstimator()
	_, err := estimator.Estimate(
		context.Background(),
		ports.TokenEstimateRequest{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointChat,
			ClientModel:  "model-1",
			RequestBody: []byte(
				`{"model":"model-1","messages":[]}`,
			),
			RequestedCapabilities: domain.CapabilitySet{
				Chat: true,
			},
		},
	)
	if !errors.Is(err, ErrEstimationUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestTokenEstimatorEmbeddingsUsesRequestByteCeiling(
	t *testing.T,
) {
	estimator := NewTokenEstimator()
	body := []byte(
		`{"model":"embedding-1","input":["a","b"]}`,
	)

	result, err := estimator.Estimate(
		context.Background(),
		ports.TokenEstimateRequest{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointEmbeddings,
			ClientModel:  "embedding-1",
			RequestBody:  body,
			RequestedCapabilities: domain.CapabilitySet{
				Embeddings: true,
			},
		},
	)
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if result.Usage != (domain.TokenUsage{
		InputTokens: int64(len(body)),
	}) {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestTokenEstimatorImagesUsesExplicitOrDefaultUnits(
	t *testing.T,
) {
	estimator := NewTokenEstimator()
	for _, test := range []struct {
		name string
		body string
		want int64
	}{
		{
			name: "explicit",
			body: `{"model":"image-1","prompt":"x","n":3}`,
			want: 3,
		},
		{
			name: "default",
			body: `{"model":"image-1","prompt":"x"}`,
			want: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := estimator.Estimate(
				context.Background(),
				ports.TokenEstimateRequest{
					APIFamily: domain.
						APIFamilyOpenAICompatible,
					EndpointKind: domain.
						EndpointImagesGeneration,
					ClientModel: "image-1",
					RequestBody: []byte(test.body),
					RequestedCapabilities: domain.CapabilitySet{
						ImagesGeneration: true,
					},
				},
			)
			if err != nil {
				t.Fatalf("Estimate: %v", err)
			}
			if result.Usage.ImageGenerationUnits != test.want {
				t.Fatalf("usage = %+v", result.Usage)
			}
		})
	}
}

func TestTokenEstimatorRejectsInvalidStructuralLimits(
	t *testing.T,
) {
	estimator := NewTokenEstimator()
	for _, body := range []string{
		`{"model":"model-1","max_tokens":0}`,
		`{"model":"model-1","max_tokens":1.5}`,
		`{"model":"model-1","max_tokens":"10"}`,
	} {
		_, err := estimator.Estimate(
			context.Background(),
			ports.TokenEstimateRequest{
				APIFamily: domain.
					APIFamilyOpenAICompatible,
				EndpointKind: domain.EndpointChat,
				ClientModel:  "model-1",
				RequestBody:  []byte(body),
				RequestedCapabilities: domain.CapabilitySet{
					Chat: true,
				},
			},
		)
		if !errors.Is(err, ErrEstimationUnavailable) {
			t.Fatalf("body=%s error=%v", body, err)
		}
	}
}

func TestTokenEstimatorRejectsMetadataMismatch(
	t *testing.T,
) {
	estimator := NewTokenEstimator()
	_, err := estimator.Estimate(
		context.Background(),
		ports.TokenEstimateRequest{
			APIFamily:              domain.APIFamilyOpenAICompatible,
			EndpointKind:           domain.EndpointChat,
			ClientModel:            "other-model",
			RequestBody:            []byte(`{"model":"model-1"}`),
			DefaultMaxOutputTokens: 10,
			RequestedCapabilities: domain.CapabilitySet{
				Chat: true,
			},
		},
	)
	if !errors.Is(err, ErrEstimationUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestTokenEstimatorRejectsNilAndCanceledContext(
	t *testing.T,
) {
	estimator := NewTokenEstimator()
	request := ports.TokenEstimateRequest{
		APIFamily:              domain.APIFamilyOpenAICompatible,
		EndpointKind:           domain.EndpointChat,
		ClientModel:            "model-1",
		RequestBody:            []byte(`{"model":"model-1"}`),
		DefaultMaxOutputTokens: 10,
		RequestedCapabilities: domain.CapabilitySet{
			Chat: true,
		},
	}

	_, err := estimator.Estimate(nil, request)
	if !errors.Is(err, ErrEstimationUnavailable) {
		t.Fatalf("nil context error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = estimator.Estimate(ctx, request)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v", err)
	}
}
