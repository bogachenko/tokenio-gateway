package openaicompat

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestAdapterParseReturnsExactModelWithoutMutatingPayload(
	t *testing.T,
) {
	adapter := NewAdapter()
	payload := []byte(`{"model":" model-1 ","stream":false}`)
	original := append([]byte(nil), payload...)

	result, err := adapter.Parse(
		context.Background(),
		llmrequest.ParseInput{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointChat,
			Payload:      payload,
		},
	)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if result.ClientModel != " model-1 " {
		t.Fatalf(
			"client model = %q, want exact JSON string",
			result.ClientModel,
		)
	}
	if !bytes.Equal(payload, original) {
		t.Fatalf("payload mutated: got %q, want %q", payload, original)
	}
}

func TestAdapterDetectChatCapabilitiesStructurally(
	t *testing.T,
) {
	adapter := NewAdapter()
	payload := []byte(`{
		"model":"model-1",
		"tools":null,
		"tool_choice":"auto",
		"response_format":{
			"type":"json_schema",
			"json_schema":{"name":"answer"}
		},
		"reasoning_effort":"high",
		"messages":[{
			"content":[
				{"type":"image_url","image_url":{"url":"x"}},
				{"type":"input_audio","input_audio":{"data":"x"}},
				{"file_id":"file-1"},
				{"type":"input_video"}
			]
		}]
	}`)
	original := append([]byte(nil), payload...)

	result, err := adapter.Detect(
		context.Background(),
		llmrequest.CapabilityInput{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointChat,
			ClientModel:  "model-1",
			Payload:      payload,
		},
	)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	want := domain.CapabilitySet{
		Chat:           true,
		Tools:          true,
		ToolChoice:     true,
		ResponseFormat: true,
		JSONSchema:     true,
		ImageInput:     true,
		AudioInput:     true,
		FileInput:      true,
		VideoInput:     true,
		Reasoning:      true,
	}
	if result != want {
		t.Fatalf("capabilities = %+v, want %+v", result, want)
	}
	if !bytes.Equal(payload, original) {
		t.Fatalf("payload mutated: got %q, want %q", payload, original)
	}
}

func TestAdapterDetectUsesExactStructuralSignalsOnly(
	t *testing.T,
) {
	adapter := NewAdapter()
	payload := []byte(`{
		"model":"model-1",
		"metadata":{
			"tools":true,
			"reasoning_effort":"high"
		},
		"messages":[{
			"content":[
				{"type":"my_image_url","image_url_extra":"x"},
				{"type":"text","text":"tools image_url input_audio file video_url reasoning_effort"}
			]
		}]
	}`)

	result, err := adapter.Detect(
		context.Background(),
		llmrequest.CapabilityInput{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointChat,
			ClientModel:  "model-1",
			Payload:      payload,
		},
	)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if result != (domain.CapabilitySet{Chat: true}) {
		t.Fatalf("capabilities = %+v, want chat only", result)
	}
}

func TestAdapterParsesSupportedEmbeddingInputForms(
	t *testing.T,
) {
	adapter := NewAdapter()
	tests := []string{
		`{"model":"embedding-1","input":"hello"}`,
		`{"model":"embedding-1","input":["a","b"]}`,
		`{"model":"embedding-1","input":[1,2,3]}`,
		`{"model":"embedding-1","input":[[1,2],[3]]}`,
	}
	for _, payload := range tests {
		result, err := adapter.Parse(
			context.Background(),
			llmrequest.ParseInput{
				APIFamily: domain.
					APIFamilyOpenAICompatible,
				EndpointKind: domain.EndpointEmbeddings,
				Payload:      []byte(payload),
			},
		)
		if err != nil {
			t.Fatalf("payload=%s Parse: %v", payload, err)
		}
		if result.ClientModel != "embedding-1" {
			t.Fatalf("result=%+v", result)
		}
	}
}

func TestAdapterRejectsInvalidEmbeddingInputForms(
	t *testing.T,
) {
	adapter := NewAdapter()
	tests := []string{
		`{"model":"embedding-1"}`,
		`{"model":"embedding-1","input":null}`,
		`{"model":"embedding-1","input":""}`,
		`{"model":"embedding-1","input":[]}`,
		`{"model":"embedding-1","input":["ok",""]}`,
		`{"model":"embedding-1","input":["text",1]}`,
		`{"model":"embedding-1","input":[1,-2]}`,
		`{"model":"embedding-1","input":[1,2.5]}`,
		`{"model":"embedding-1","input":[[1],[]]}`,
		`{"model":"embedding-1","input":[[1],["x"]]}`,
	}
	for _, payload := range tests {
		_, err := adapter.Parse(
			context.Background(),
			llmrequest.ParseInput{
				APIFamily: domain.
					APIFamilyOpenAICompatible,
				EndpointKind: domain.EndpointEmbeddings,
				Payload:      []byte(payload),
			},
		)
		if !errors.Is(err, llmrequest.ErrInvalidJSON) {
			t.Fatalf(
				"payload=%s error=%v, want invalid JSON",
				payload,
				err,
			)
		}
	}
}

func TestAdapterDetectBaseCapabilityByEndpoint(t *testing.T) {
	adapter := NewAdapter()
	tests := []struct {
		name     string
		endpoint domain.EndpointKind
		want     domain.CapabilitySet
	}{
		{
			name:     "chat",
			endpoint: domain.EndpointChat,
			want:     domain.CapabilitySet{Chat: true},
		},
		{
			name:     "embeddings",
			endpoint: domain.EndpointEmbeddings,
			want: domain.CapabilitySet{
				Embeddings: true,
			},
		},
		{
			name:     "images generation",
			endpoint: domain.EndpointImagesGeneration,
			want: domain.CapabilitySet{
				ImagesGeneration: true,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := adapter.Detect(
				context.Background(),
				llmrequest.CapabilityInput{
					APIFamily: domain.
						APIFamilyOpenAICompatible,
					EndpointKind: test.endpoint,
					ClientModel:  "model-1",
					Payload: endpointPayload(
						test.endpoint,
					),
				},
			)
			if err != nil {
				t.Fatalf("Detect: %v", err)
			}
			if result != test.want {
				t.Fatalf(
					"capabilities = %+v, want %+v",
					result,
					test.want,
				)
			}
		})
	}
}

func TestAdapterDetectJSONSchemaByFieldPresence(t *testing.T) {
	adapter := NewAdapter()
	result, err := adapter.Detect(
		context.Background(),
		llmrequest.CapabilityInput{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointChat,
			ClientModel:  "model-1",
			Payload: []byte(`{
				"model":"model-1",
				"response_format":{
					"type":"text",
					"json_schema":null
				}
			}`),
		},
	)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !result.Chat ||
		!result.ResponseFormat ||
		!result.JSONSchema {
		t.Fatalf("capabilities = %+v", result)
	}
}

func TestAdapterRejectsInvalidStructuralJSON(t *testing.T) {
	adapter := NewAdapter()
	invalidUTF8 := append(
		[]byte(`{"model":"model-1","x":"`),
		0xff,
	)
	invalidUTF8 = append(invalidUTF8, []byte(`"}`)...)

	tests := []struct {
		name    string
		payload []byte
		want    error
	}{
		{
			name:    "empty",
			payload: nil,
			want:    llmrequest.ErrInvalidJSON,
		},
		{
			name:    "invalid UTF-8",
			payload: invalidUTF8,
			want:    llmrequest.ErrInvalidJSON,
		},
		{
			name:    "invalid JSON",
			payload: []byte(`{"model":`),
			want:    llmrequest.ErrInvalidJSON,
		},
		{
			name:    "top-level array",
			payload: []byte(`["model-1"]`),
			want:    llmrequest.ErrInvalidJSON,
		},
		{
			name: "trailing JSON value",
			payload: []byte(
				`{"model":"model-1"} {}`,
			),
			want: llmrequest.ErrInvalidJSON,
		},
		{
			name: "duplicate top-level key",
			payload: []byte(
				`{"model":"model-1","model":"model-2"}`,
			),
			want: llmrequest.ErrInvalidJSON,
		},
		{
			name: "duplicate nested key",
			payload: []byte(`{
				"model":"model-1",
				"messages":[{
					"content":[{
						"type":"text",
						"metadata":{"x":1,"x":2}
					}]
				}]
			}`),
			want: llmrequest.ErrInvalidJSON,
		},
		{
			name: "model wrong type",
			payload: []byte(
				`{"model":123}`,
			),
			want: llmrequest.ErrInvalidJSON,
		},
		{
			name: "stream wrong type",
			payload: []byte(
				`{"model":"model-1","stream":"false"}`,
			),
			want: llmrequest.ErrInvalidJSON,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := adapter.Parse(
				context.Background(),
				llmrequest.ParseInput{
					APIFamily: domain.
						APIFamilyOpenAICompatible,
					EndpointKind: domain.EndpointChat,
					Payload:      test.payload,
				},
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestAdapterRejectsModelAndStreamingContracts(
	t *testing.T,
) {
	adapter := NewAdapter()
	tests := []struct {
		name    string
		payload []byte
		want    error
	}{
		{
			name:    "model absent",
			payload: []byte(`{}`),
			want:    llmrequest.ErrModelRequired,
		},
		{
			name: "model blank",
			payload: []byte(
				`{"model":" \t "}`,
			),
			want: llmrequest.ErrModelRequired,
		},
		{
			name: "streaming true",
			payload: []byte(
				`{"model":"model-1","stream":true}`,
			),
			want: llmrequest.ErrStreamingUnsupported,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := adapter.Parse(
				context.Background(),
				llmrequest.ParseInput{
					APIFamily: domain.
						APIFamilyOpenAICompatible,
					EndpointKind: domain.EndpointChat,
					Payload:      test.payload,
				},
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestAdapterJSONNestingDepth(t *testing.T) {
	adapter := NewAdapter()

	_, err := adapter.Parse(
		context.Background(),
		llmrequest.ParseInput{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointChat,
			Payload:      nestedPayload(127),
		},
	)
	if err != nil {
		t.Fatalf("depth 128 should be accepted: %v", err)
	}

	_, err = adapter.Parse(
		context.Background(),
		llmrequest.ParseInput{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointChat,
			Payload:      nestedPayload(128),
		},
	)
	if !errors.Is(err, llmrequest.ErrInvalidJSON) {
		t.Fatalf(
			"depth 129 error = %v, want invalid JSON",
			err,
		)
	}
}

func TestAdapterDetectRejectsModelMismatch(t *testing.T) {
	adapter := NewAdapter()
	_, err := adapter.Detect(
		context.Background(),
		llmrequest.CapabilityInput{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointChat,
			ClientModel:  "other-model",
			Payload: []byte(
				`{"model":"model-1"}`,
			),
		},
	)
	if !errors.Is(err, llmrequest.ErrStageContractViolation) {
		t.Fatalf(
			"error = %v, want stage contract violation",
			err,
		)
	}
}

func TestAdapterRejectsUnsupportedInvocation(t *testing.T) {
	adapter := NewAdapter()
	tests := []llmrequest.ParseInput{
		{
			APIFamily:    domain.APIFamilyGeminiNative,
			EndpointKind: domain.EndpointChat,
			Payload:      []byte(`{"model":"model-1"}`),
		},
		{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointModels,
			Payload:      []byte(`{"model":"model-1"}`),
		},
	}

	for _, input := range tests {
		_, err := adapter.Parse(context.Background(), input)
		if !errors.Is(err, llmrequest.ErrStageContractViolation) {
			t.Fatalf(
				"error = %v, want stage contract violation",
				err,
			)
		}
	}
}

func TestAdapterHonorsCanceledContext(t *testing.T) {
	adapter := NewAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := adapter.Parse(
		ctx,
		llmrequest.ParseInput{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointChat,
			Payload:      []byte(`{"model":"model-1"}`),
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func nestedPayload(nestedArrays int) []byte {
	var builder strings.Builder
	builder.WriteString(`{"model":"model-1","extra":`)
	builder.WriteString(strings.Repeat("[", nestedArrays))
	builder.WriteByte('0')
	builder.WriteString(strings.Repeat("]", nestedArrays))
	builder.WriteByte('}')
	return []byte(builder.String())
}

func endpointPayload(endpoint domain.EndpointKind) []byte {
	switch endpoint {
	case domain.EndpointEmbeddings:
		return []byte(
			`{"model":"model-1","input":"x"}`,
		)
	default:
		return []byte(`{"model":"model-1"}`)
	}
}
