package openaicompatrequest

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestParseChatExtractsStructuralMetadataAndPreservesBody(
	t *testing.T,
) {
	body := []byte(`{
		"model":"gpt-test",
		"stream":false,
		"tools":[],
		"tool_choice":"auto",
		"response_format":{
			"type":"json_schema",
			"json_schema":{"name":"result"}
		},
		"reasoning_effort":"medium",
		"messages":[{
			"role":"user",
			"content":[
				{"type":"image_url","image_url":{"url":"x"}},
				{"type":"input_audio","input_audio":{"data":"x"}},
				{"type":"file","file":{"file_id":"file_1"}},
				{"type":"video_url","video_url":{"url":"x"}}
			]
		}]
	}`)
	original := bytes.Clone(body)

	parsed, err := Parse(
		domain.EndpointChat,
		body,
	)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if parsed.Query.APIFamily !=
		domain.APIFamilyOpenAICompatible ||
		parsed.Query.EndpointKind !=
			domain.EndpointChat ||
		parsed.Query.ClientModel != "gpt-test" {
		t.Fatalf("query = %+v", parsed.Query)
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
	if parsed.RequestedCapabilities != want {
		t.Fatalf(
			"capabilities = %+v, want %+v",
			parsed.RequestedCapabilities,
			want,
		)
	}
	if !bytes.Equal(parsed.Body, original) {
		t.Fatal("parser changed request body bytes")
	}

	body[0] = 'X'
	if !bytes.Equal(parsed.Body, original) {
		t.Fatal(
			"parsed body aliases caller-owned input",
		)
	}
}

func TestParseDoesNotInferCapabilitiesFromPromptText(
	t *testing.T,
) {
	body := []byte(`{
		"model":"gpt-test",
		"messages":[{
			"role":"user",
			"content":"Use tools, image_url, input_audio, file_id and video_url"
		}]
	}`)

	parsed, err := Parse(
		domain.EndpointChat,
		body,
	)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := domain.CapabilitySet{Chat: true}
	if parsed.RequestedCapabilities != want {
		t.Fatalf(
			"semantic text changed capabilities: %+v",
			parsed.RequestedCapabilities,
		)
	}
}

func TestParseAssignsEndpointCapability(
	t *testing.T,
) {
	tests := []struct {
		name     string
		endpoint domain.EndpointKind
		want     domain.CapabilitySet
	}{
		{
			name:     "chat",
			endpoint: domain.EndpointChat,
			want: domain.CapabilitySet{
				Chat: true,
			},
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
			parsed, err := Parse(
				test.endpoint,
				[]byte(`{"model":"model-1"}`),
			)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if parsed.RequestedCapabilities !=
				test.want {
				t.Fatalf(
					"capabilities=%+v want=%+v",
					parsed.RequestedCapabilities,
					test.want,
				)
			}
		})
	}
}

func TestParseRejectsInvalidStructuralContracts(
	t *testing.T,
) {
	tests := []struct {
		name     string
		endpoint domain.EndpointKind
		body     string
		want     error
	}{
		{
			name:     "empty body",
			endpoint: domain.EndpointChat,
			body:     "",
			want:     ErrInvalidJSON,
		},
		{
			name:     "invalid syntax",
			endpoint: domain.EndpointChat,
			body:     `{"model":`,
			want:     ErrInvalidJSON,
		},
		{
			name:     "top level null",
			endpoint: domain.EndpointChat,
			body:     `null`,
			want:     ErrInvalidJSON,
		},
		{
			name:     "top level array",
			endpoint: domain.EndpointChat,
			body:     `["model"]`,
			want:     ErrInvalidJSON,
		},
		{
			name:     "trailing JSON value",
			endpoint: domain.EndpointChat,
			body:     `{"model":"a"} {"model":"b"}`,
			want:     ErrInvalidJSON,
		},
		{
			name:     "duplicate model",
			endpoint: domain.EndpointChat,
			body:     `{"model":"a","model":"b"}`,
			want:     ErrInvalidJSON,
		},
		{
			name:     "nested duplicate key",
			endpoint: domain.EndpointChat,
			body: `{
				"model":"a",
				"messages":[{
					"content":[{
						"type":"image_url",
						"type":"text"
					}]
				}]
			}`,
			want: ErrInvalidJSON,
		},
		{
			name:     "missing model",
			endpoint: domain.EndpointChat,
			body:     `{}`,
			want:     ErrModelRequired,
		},
		{
			name:     "blank model",
			endpoint: domain.EndpointChat,
			body:     `{"model":"   "}`,
			want:     ErrModelRequired,
		},
		{
			name:     "non string model",
			endpoint: domain.EndpointChat,
			body:     `{"model":42}`,
			want:     ErrInvalidJSON,
		},
		{
			name:     "stream true",
			endpoint: domain.EndpointChat,
			body: `{
				"model":"a",
				"stream":true
			}`,
			want: ErrStreamingUnsupported,
		},
		{
			name:     "stream wrong type",
			endpoint: domain.EndpointChat,
			body: `{
				"model":"a",
				"stream":"false"
			}`,
			want: ErrInvalidJSON,
		},
		{
			name:     "unsupported endpoint",
			endpoint: domain.EndpointKind("responses"),
			body:     `{"model":"a"}`,
			want:     ErrUnsupportedEndpoint,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := Parse(
				test.endpoint,
				[]byte(test.body),
			)
			if !errors.Is(err, test.want) {
				t.Fatalf(
					"error=%v want=%v",
					err,
					test.want,
				)
			}
			if result.Body != nil ||
				result.Query.APIFamily != "" ||
				result.RequestedCapabilities !=
					(domain.CapabilitySet{}) {
				t.Fatalf(
					"partial result returned: %+v",
					result,
				)
			}
		})
	}
}

func TestParseDetectsMediaByExactStructuralFields(
	t *testing.T,
) {
	body := []byte(`{
		"model":"gpt-test",
		"messages":[{
			"content":[
				{"input_image":{}},
				{"audio":{}},
				{"file_id":"file_1"},
				{"input_video":{}}
			]
		}]
	}`)

	parsed, err := Parse(
		domain.EndpointChat,
		body,
	)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	capabilities := parsed.RequestedCapabilities
	if !capabilities.ImageInput ||
		!capabilities.AudioInput ||
		!capabilities.FileInput ||
		!capabilities.VideoInput {
		t.Fatalf(
			"capabilities = %+v",
			capabilities,
		)
	}
}

func TestParseErrorsDoNotLeakBody(
	t *testing.T,
) {
	body := []byte(
		`{"model":"sk_live_secret","stream":true}`,
	)

	_, err := Parse(
		domain.EndpointChat,
		body,
	)
	if !errors.Is(
		err,
		ErrStreamingUnsupported,
	) {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(
		err.Error(),
		"sk_live_",
	) {
		t.Fatalf("body leaked through error: %v", err)
	}
}
