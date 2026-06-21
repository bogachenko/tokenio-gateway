package gemininative

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestAdapterPreservesGeminiRawQueryWithoutRewrite(t *testing.T) {
	var upstreamPath string
	var upstreamQuery string
	adapter, err := NewAdapter(Config{
		Reseller: domain.Reseller{ID: "reseller-gemini", ProviderType: domain.ProviderGemini, BaseURL: "https://gemini.example/base"},
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
		Route:  geminiRoute(domain.EndpointChat, "gemini-client", "gemini-client", domain.ModelRewritePolicyNone),
		Method: http.MethodPost,
		Path:   "/v1beta/models/gemini-client:streamGenerateContent?alt=sse",
		Body:   []byte(`{"contents":[]}`),
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if upstreamPath != "/base/v1beta/models/gemini-client:streamGenerateContent" || upstreamQuery != "alt=sse" {
		t.Fatalf("path=%q query=%q", upstreamPath, upstreamQuery)
	}
}

func TestAdapterPreservesGeminiRawQueryWhileRewritingPathModel(t *testing.T) {
	var upstreamPath string
	var upstreamQuery string
	adapter, err := NewAdapter(Config{
		Reseller: domain.Reseller{ID: "reseller-gemini", ProviderType: domain.ProviderGemini, BaseURL: "https://gemini.example"},
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
		Route:  geminiRoute(domain.EndpointChat, "client-model", "provider-model", domain.ModelRewritePolicyProviderModel),
		Method: http.MethodPost,
		Path:   "/v1beta/models/client-model:generateContent?alt=sse",
		Body:   []byte(`{"contents":[]}`),
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if upstreamPath != "/v1beta/models/provider-model:generateContent" || upstreamQuery != "alt=sse" {
		t.Fatalf("path=%q query=%q", upstreamPath, upstreamQuery)
	}
}

func TestAdapterMatchesGeminiOperationsWithRawQuery(t *testing.T) {
	adapter, err := NewAdapter(Config{
		Reseller:             domain.Reseller{ID: "reseller-gemini", ProviderType: domain.ProviderGemini, BaseURL: "https://gemini.example"},
		ResellerAPIKey:       "sk_provider_secret",
		MaxResponseBodyBytes: 1024,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.RawQuery != "alt=sse" {
				t.Fatalf("query=%q", request.URL.RawQuery)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path string
		kind domain.EndpointKind
	}{
		{path: "/v1beta/models/gemini-client:generateContent?alt=sse", kind: domain.EndpointChat},
		{path: "/v1beta/models/gemini-client:streamGenerateContent?alt=sse", kind: domain.EndpointChat},
		{path: "/v1beta/models/gemini-client:embedContent?alt=sse", kind: domain.EndpointEmbeddings},
		{path: "/v1beta/models/gemini-client:batchEmbedContents?alt=sse", kind: domain.EndpointEmbeddings},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			_, err := adapter.Forward(context.Background(), ports.ForwardRequest{
				Route:  geminiRoute(test.kind, "gemini-client", "gemini-client", domain.ModelRewritePolicyNone),
				Method: http.MethodPost,
				Path:   test.path,
				Body:   []byte(`{"contents":[]}`),
			})
			if err != nil {
				t.Fatalf("Forward: %v", err)
			}
		})
	}
}
