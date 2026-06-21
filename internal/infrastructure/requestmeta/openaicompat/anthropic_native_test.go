package openaicompat

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestAdapterParsesAnthropicNativeMessagesModelFromBody(t *testing.T) {
	adapter := NewAdapter()
	payload := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"max_tokens":32}`)
	original := append([]byte(nil), payload...)

	result, err := adapter.Parse(
		context.Background(),
		llmrequest.ParseInput{
			APIFamily:    domain.APIFamilyAnthropicNative,
			EndpointKind: domain.EndpointChat,
			Payload:      payload,
		},
	)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if result.ClientModel != "claude-sonnet-4-5" {
		t.Fatalf("client model = %q", result.ClientModel)
	}
	if !bytes.Equal(payload, original) {
		t.Fatalf("payload mutated: got %q want %q", payload, original)
	}
}

func TestAdapterDetectsAnthropicNativeMessagesChatCapability(t *testing.T) {
	adapter := NewAdapter()
	payload := []byte(`{"model":"claude-sonnet-4-5","messages":[],"max_tokens":32}`)

	result, err := adapter.Detect(
		context.Background(),
		llmrequest.CapabilityInput{
			APIFamily:    domain.APIFamilyAnthropicNative,
			EndpointKind: domain.EndpointChat,
			ClientModel:  "claude-sonnet-4-5",
			Payload:      payload,
		},
	)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if result != (domain.CapabilitySet{Chat: true}) {
		t.Fatalf("capabilities = %+v", result)
	}
}

func TestAdapterRejectsInvalidAnthropicNativeMessagesModelBody(t *testing.T) {
	adapter := NewAdapter()
	tests := []struct {
		name    string
		payload []byte
		want    error
	}{
		{
			name:    "missing model",
			payload: []byte(`{"messages":[]}`),
			want:    llmrequest.ErrModelRequired,
		},
		{
			name:    "blank model",
			payload: []byte(`{"model":" \t ","messages":[]}`),
			want:    llmrequest.ErrModelRequired,
		},
		{
			name:    "model wrong type",
			payload: []byte(`{"model":123,"messages":[]}`),
			want:    llmrequest.ErrInvalidJSON,
		},
		{
			name:    "invalid json",
			payload: []byte(`{"model":`),
			want:    llmrequest.ErrInvalidJSON,
		},
		{
			name:    "trailing json",
			payload: []byte(`{"model":"claude-sonnet-4-5"} {}`),
			want:    llmrequest.ErrInvalidJSON,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := adapter.Parse(
				context.Background(),
				llmrequest.ParseInput{
					APIFamily:    domain.APIFamilyAnthropicNative,
					EndpointKind: domain.EndpointChat,
					Payload:      test.payload,
				},
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v want %v", err, test.want)
			}
		})
	}
}
