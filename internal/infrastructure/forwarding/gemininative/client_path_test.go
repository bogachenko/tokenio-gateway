package gemininative

import (
	"context"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type captureGeminiClientAdapter struct{ path string }

func (a *captureGeminiClientAdapter) Forward(_ context.Context, request ports.ForwardRequest) (ports.ForwardResponse, error) {
	a.path = request.Path
	return ports.ForwardResponse{StatusCode: 200, Body: []byte(`{}`)}, nil
}

func TestClientPreservesGeminiNativeOperationPath(t *testing.T) {
	tests := []struct {
		name string
		kind domain.EndpointKind
		path string
	}{
		{name: "generateContent", kind: domain.EndpointChat, path: "/v1beta/models/gemini-client:generateContent"},
		{name: "streamGenerateContent", kind: domain.EndpointChat, path: "/v1beta/models/gemini-client:streamGenerateContent"},
		{name: "embedContent", kind: domain.EndpointEmbeddings, path: "/v1beta/models/gemini-client:embedContent"},
		{name: "batchEmbedContents", kind: domain.EndpointEmbeddings, path: "/v1beta/models/gemini-client:batchEmbedContents"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := &captureGeminiClientAdapter{}
			client, err := newClient(adapter)
			if err != nil { t.Fatalf("newClient: %v", err) }
			_, err = client.Forward(context.Background(), ports.ForwardingClientRequest{
				Route: geminiRoute(test.kind, "gemini-client", "gemini-client", domain.ModelRewritePolicyNone),
				Path: test.path,
				Body: []byte(`{"contents":[]}`),
			})
			if err != nil { t.Fatalf("Forward: %v", err) }
			if adapter.path != test.path { t.Fatalf("path=%q want %q", adapter.path, test.path) }
		})
	}
}
