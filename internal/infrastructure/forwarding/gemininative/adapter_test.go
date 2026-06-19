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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestAdapterForwardsGenerateContentByteForByte(t *testing.T) {
	body := []byte(`{"contents":[{"parts":[{"text":"hello"}]}]}`)
	var upstreamBody string
	var upstreamPath string
	var upstreamKey string
	adapter, err := NewAdapter(Config{
		Reseller: domain.Reseller{
			ID:           "reseller-gemini",
			ProviderType: domain.ProviderGemini,
			BaseURL:      "https://gemini.example/base",
		},
		ResellerAPIKey:       "sk_provider_secret",
		MaxResponseBodyBytes: 1024,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			upstreamPath = request.URL.EscapedPath()
			upstreamKey = request.Header.Get("x-goog-api-key")
			data, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			upstreamBody = string(data)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": {"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`)),
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	response, err := adapter.Forward(context.Background(), ports.ForwardRequest{
		Route:  geminiRoute(domain.EndpointChat, "gemini-client", "gemini-client", domain.ModelRewritePolicyNone),
		Method: http.MethodPost,
		Path:   "/v1beta/models/gemini-client:generateContent",
		Headers: map[string][]string{
			"Authorization":  {"Bearer sk_tokenio_secret"},
			"x-goog-api-key": {"sk_tokenio_secret"},
			"Connection":     {"X-Remove-Me"},
			"X-Remove-Me":    {"strip"},
		},
		Body: body,
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if response.StatusCode != http.StatusOK ||
		string(response.Body) != `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}` ||
		response.Headers["Content-Type"][0] != "application/json" {
		t.Fatalf("response=%+v", response)
	}
	if upstreamPath != "/base/v1beta/models/gemini-client:generateContent" ||
		upstreamKey != "sk_provider_secret" ||
		upstreamBody != string(body) {
		t.Fatalf("upstream path=%q key=%q body=%q", upstreamPath, upstreamKey, upstreamBody)
	}
}

func TestAdapterRewritesGeminiPathModelOnly(t *testing.T) {
	var upstreamPath string
	adapter, err := NewAdapter(Config{
		Reseller: domain.Reseller{
			ID:           "reseller-gemini",
			ProviderType: domain.ProviderGemini,
			BaseURL:      "https://gemini.example",
		},
		ResellerAPIKey:       "sk_provider_secret",
		MaxResponseBodyBytes: 1024,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			upstreamPath = request.URL.EscapedPath()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = adapter.Forward(context.Background(), ports.ForwardRequest{
		Route:  geminiRoute(domain.EndpointChat, "client-model", "provider-model", domain.ModelRewritePolicyProviderModel),
		Method: http.MethodPost,
		Path:   "/v1beta/models/client-model:generateContent",
		Body:   []byte(`{"contents":[]}`),
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if upstreamPath != "/v1beta/models/provider-model:generateContent" {
		t.Fatalf("upstream path=%q", upstreamPath)
	}
}

func TestAdapterForwardsEmbeddingOperations(t *testing.T) {
	adapter, err := NewAdapter(Config{
		Reseller: domain.Reseller{
			ID:           "reseller-gemini",
			ProviderType: domain.ProviderGemini,
			BaseURL:      "https://gemini.example",
		},
		ResellerAPIKey:       "sk_provider_secret",
		MaxResponseBodyBytes: 1024,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"embedding":{"values":[1]}}`)),
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		"/v1beta/models/embed-client:embedContent",
		"/v1beta/models/embed-client:batchEmbedContents",
	} {
		_, err := adapter.Forward(context.Background(), ports.ForwardRequest{
			Route:  geminiRoute(domain.EndpointEmbeddings, "embed-client", "embed-client", domain.ModelRewritePolicyNone),
			Method: http.MethodPost,
			Path:   path,
			Body:   []byte(`{"content":{"parts":[{"text":"hello"}]}}`),
		})
		if err != nil {
			t.Fatalf("path=%s Forward: %v", path, err)
		}
	}
}

func TestAdapterRejectsUnsupportedGeminiRoute(t *testing.T) {
	adapter, err := NewAdapter(Config{
		Reseller: domain.Reseller{
			ID:           "reseller-gemini",
			ProviderType: domain.ProviderGemini,
			BaseURL:      "https://gemini.example",
		},
		ResellerAPIKey:       "sk_provider_secret",
		MaxResponseBodyBytes: 1024,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			t.Fatal("transport must not be called")
			return nil, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []ports.ForwardRequest{
		{
			Route:  geminiRoute(domain.EndpointChat, "client-model", "client-model", domain.ModelRewritePolicyNone),
			Method: http.MethodGet,
			Path:   "/v1beta/models/client-model:generateContent",
			Body:   []byte(`{}`),
		},
		{
			Route:  geminiRoute(domain.EndpointChat, "client-model", "client-model", domain.ModelRewritePolicyNone),
			Method: http.MethodPost,
			Path:   "/v1beta/models/other:generateContent",
			Body:   []byte(`{}`),
		},
		{
			Route:  geminiRoute(domain.EndpointChat, "client-model", "client-model", domain.ModelRewritePolicyNone),
			Method: http.MethodPost,
			Path:   "/v1beta/models/client-model:embedContent",
			Body:   []byte(`{}`),
		},
	}
	for _, request := range tests {
		if _, err := adapter.Forward(context.Background(), request); err == nil {
			t.Fatalf("request=%+v unexpectedly succeeded", request)
		}
	}
}

func geminiRoute(endpoint domain.EndpointKind, clientModel string, providerModel string, policy domain.ModelRewritePolicy) domain.Route {
	return domain.Route{
		ID:                 "route-gemini",
		ResellerID:         "reseller-gemini",
		ProviderType:       domain.ProviderGemini,
		APIFamily:          domain.APIFamilyGeminiNative,
		EndpointKind:       endpoint,
		ClientModel:        clientModel,
		ProviderModel:      providerModel,
		ModelRewritePolicy: policy,
	}
}
