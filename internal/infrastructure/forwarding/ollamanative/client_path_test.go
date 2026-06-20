package ollamanative

import (
	"context"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type captureOllamaClientAdapter struct{ path string }

func (a *captureOllamaClientAdapter) Forward(_ context.Context, request ports.ForwardRequest) (ports.ForwardResponse, error) {
	a.path = request.Path
	return ports.ForwardResponse{StatusCode: 200, Body: []byte(`{}`)}, nil
}

func TestClientPreservesOllamaNativePath(t *testing.T) {
	tests := []struct {
		name string
		kind domain.EndpointKind
		path string
	}{
		{name: "chat", kind: domain.EndpointChat, path: "/api/chat"},
		{name: "generate", kind: domain.EndpointChat, path: "/api/generate"},
		{name: "embeddings", kind: domain.EndpointEmbeddings, path: "/api/embeddings"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := &captureOllamaClientAdapter{}
			client, err := newClient(adapter)
			if err != nil { t.Fatalf("newClient: %v", err) }
			_, err = client.Forward(context.Background(), ports.ForwardingClientRequest{
				Route: ollamaRoute(test.kind, "llama-client", "llama-client", domain.ModelRewritePolicyNone),
				Path: test.path,
				Body: []byte(`{"model":"llama-client"}`),
			})
			if err != nil { t.Fatalf("Forward: %v", err) }
			if adapter.path != test.path { t.Fatalf("path=%q want %q", adapter.path, test.path) }
		})
	}
}
