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

func TestFactorySupportsGeminiChatAndEmbeddings(t *testing.T) {
	factory, err := NewFactory(roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("transport must not be called")
		return nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !factory.SupportsForwardingEndpoint(domain.EndpointChat) ||
		!factory.SupportsForwardingEndpoint(domain.EndpointEmbeddings) ||
		factory.SupportsForwardingEndpoint(domain.EndpointImagesGeneration) {
		t.Fatalf("unexpected endpoint support")
	}
}

func TestFactoryBuildsGeminiForwardingClient(t *testing.T) {
	var upstreamPath string
	factory, err := NewFactory(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		upstreamPath = request.URL.EscapedPath()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	client, err := factory.Build(ports.ForwardingAdapterFactoryInput{
		Route: geminiRoute(
			domain.EndpointChat,
			"gemini-client",
			"gemini-client",
			domain.ModelRewritePolicyNone,
		),
		Reseller: domain.Reseller{
			ID:           "reseller-gemini",
			ProviderType: domain.ProviderGemini,
			BaseURL:      "https://gemini.example",
		},
		ResellerAPIKey:       "sk_provider_secret",
		MaxResponseBodyBytes: 1024,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	response, err := client.Forward(
		context.Background(),
		ports.ForwardingClientRequest{
			Route: geminiRoute(
				domain.EndpointChat,
				"gemini-client",
				"gemini-client",
				domain.ModelRewritePolicyNone,
			),
			Body: []byte(`{"contents":[]}`),
		},
	)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if response.StatusCode != http.StatusOK || upstreamPath != "/v1beta/models/gemini-client:generateContent" {
		t.Fatalf("response=%+v upstreamPath=%q", response, upstreamPath)
	}
}

func TestFactoryRejectsUnsupportedGeminiRoute(t *testing.T) {
	factory, err := NewFactory(roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("transport must not be called")
		return nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = factory.Build(ports.ForwardingAdapterFactoryInput{
		Route: geminiRoute(
			domain.EndpointImagesGeneration,
			"image-client",
			"image-client",
			domain.ModelRewritePolicyNone,
		),
		Reseller: domain.Reseller{
			ID:           "reseller-gemini",
			ProviderType: domain.ProviderGemini,
			BaseURL:      "https://gemini.example",
		},
		ResellerAPIKey:       "sk_provider_secret",
		MaxResponseBodyBytes: 1024,
	})
	if err == nil {
		t.Fatal("expected unsupported route")
	}
}
