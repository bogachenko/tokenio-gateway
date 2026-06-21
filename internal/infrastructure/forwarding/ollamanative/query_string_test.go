package ollamanative

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestAdapterPreservesOllamaRawQueryForSupportedPaths(t *testing.T) {
	tests := []struct {
		path     string
		endpoint domain.EndpointKind
		body     string
	}{
		{path: "/api/chat?some=value", endpoint: domain.EndpointChat, body: `{"model":"llama-client","messages":[]}`},
		{path: "/api/generate?some=value", endpoint: domain.EndpointChat, body: `{"model":"llama-client","prompt":"hi"}`},
		{path: "/api/embeddings?some=value", endpoint: domain.EndpointEmbeddings, body: `{"model":"llama-client","prompt":"hi"}`},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			var upstreamPath string
			var upstreamQuery string
			adapter, err := NewAdapter(Config{
				Reseller:             domain.Reseller{ID: "reseller-ollama", ProviderType: domain.ProviderOllama, BaseURL: "https://ollama.example/base"},
				ResellerAPIKey:       "sk_provider_secret",
				MaxResponseBodyBytes: 1024,
				Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					upstreamPath = request.URL.EscapedPath()
					upstreamQuery = request.URL.RawQuery
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}

			_, err = adapter.Forward(context.Background(), ports.ForwardRequest{
				Route:  ollamaRoute(test.endpoint, "llama-client", "llama-client", domain.ModelRewritePolicyNone),
				Method: http.MethodPost,
				Path:   test.path,
				Body:   []byte(test.body),
			})
			if err != nil {
				t.Fatalf("Forward: %v", err)
			}
			wantPath := "/base" + strings.Split(test.path, "?")[0]
			if upstreamPath != wantPath || upstreamQuery != "some=value" {
				t.Fatalf("path=%q want=%q query=%q", upstreamPath, wantPath, upstreamQuery)
			}
		})
	}
}
