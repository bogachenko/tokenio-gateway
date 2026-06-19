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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestAdapterForwardsOllamaChatByteForByte(t *testing.T) {
	body := []byte(`{"model":"llama-client","messages":[{"role":"user","content":"hello"}]}`)
	var upstreamBody string
	var upstreamPath string
	var upstreamAuthorization string
	var removedHeader string
	adapter, err := NewAdapter(Config{
		Reseller: domain.Reseller{
			ID:           "reseller-ollama",
			ProviderType: domain.ProviderOllama,
			BaseURL:      "https://ollama.example/base",
		},
		ResellerAPIKey:       "sk_provider_secret",
		MaxResponseBodyBytes: 1024,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			upstreamPath = request.URL.EscapedPath()
			upstreamAuthorization = request.Header.Get("Authorization")
			removedHeader = request.Header.Get("X-Remove-Me")
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
				Body: io.NopCloser(strings.NewReader(`{"message":{"content":"ok"}}`)),
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	response, err := adapter.Forward(context.Background(), ports.ForwardRequest{
		Route:  ollamaRoute(domain.EndpointChat, "llama-client", "llama-client", domain.ModelRewritePolicyNone),
		Method: http.MethodPost,
		Path:   "/api/chat",
		Headers: map[string][]string{
			"Authorization": {"Bearer sk_tokenio_secret"},
			"Connection":    {"X-Remove-Me"},
			"X-Remove-Me":   {"strip"},
		},
		Body: body,
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if response.StatusCode != http.StatusOK ||
		string(response.Body) != `{"message":{"content":"ok"}}` ||
		response.Headers["Content-Type"][0] != "application/json" {
		t.Fatalf("response=%+v", response)
	}
	if upstreamPath != "/base/api/chat" ||
		upstreamAuthorization != "Bearer sk_provider_secret" ||
		removedHeader != "" ||
		upstreamBody != string(body) {
		t.Fatalf(
			"upstream path=%q auth=%q removed=%q body=%q",
			upstreamPath,
			upstreamAuthorization,
			removedHeader,
			upstreamBody,
		)
	}
}

func TestAdapterRewritesOllamaModelOnly(t *testing.T) {
	var upstreamBody string
	adapter, err := NewAdapter(Config{
		Reseller: domain.Reseller{
			ID:           "reseller-ollama",
			ProviderType: domain.ProviderOllama,
			BaseURL:      "https://ollama.example",
		},
		ResellerAPIKey:       "sk_provider_secret",
		MaxResponseBodyBytes: 1024,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			data, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			upstreamBody = string(data)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"done":true}`)),
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = adapter.Forward(context.Background(), ports.ForwardRequest{
		Route:  ollamaRoute(domain.EndpointChat, "client-model", "provider-model", domain.ModelRewritePolicyProviderModel),
		Method: http.MethodPost,
		Path:   "/api/chat",
		Body:   []byte(`{"model": "client-model", "messages":[]}`),
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if upstreamBody != `{"model": "provider-model", "messages":[]}` {
		t.Fatalf("upstream body=%q", upstreamBody)
	}
}

func TestAdapterForwardsOllamaGenerateAndEmbeddings(t *testing.T) {
	adapter, err := NewAdapter(Config{
		Reseller: domain.Reseller{
			ID:           "reseller-ollama",
			ProviderType: domain.ProviderOllama,
			BaseURL:      "https://ollama.example",
		},
		ResellerAPIKey:       "sk_provider_secret",
		MaxResponseBodyBytes: 1024,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
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

	tests := []struct {
		path     string
		endpoint domain.EndpointKind
		body     string
	}{
		{
			path:     "/api/generate",
			endpoint: domain.EndpointChat,
			body:     `{"model":"llama-client","prompt":"hello"}`,
		},
		{
			path:     "/api/embeddings",
			endpoint: domain.EndpointEmbeddings,
			body:     `{"model":"llama-client","prompt":"hello"}`,
		},
	}
	for _, test := range tests {
		_, err := adapter.Forward(context.Background(), ports.ForwardRequest{
			Route:  ollamaRoute(test.endpoint, "llama-client", "llama-client", domain.ModelRewritePolicyNone),
			Method: http.MethodPost,
			Path:   test.path,
			Body:   []byte(test.body),
		})
		if err != nil {
			t.Fatalf("path=%s Forward: %v", test.path, err)
		}
	}
}

func TestAdapterAttachesOllamaUsageMetadata(t *testing.T) {
	adapter, err := NewAdapter(Config{
		Reseller: domain.Reseller{
			ID:           "reseller-ollama",
			ProviderType: domain.ProviderOllama,
			BaseURL:      "https://ollama.example",
		},
		ResellerAPIKey:       "sk_provider_secret",
		MaxResponseBodyBytes: 1024,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body: io.NopCloser(strings.NewReader(
					`{"message":{"content":"ok"},"prompt_eval_count":17,"eval_count":5,"done":true}`,
				)),
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	response, err := adapter.Forward(context.Background(), ports.ForwardRequest{
		Route:  ollamaRoute(domain.EndpointChat, "llama-client", "llama-client", domain.ModelRewritePolicyNone),
		Method: http.MethodPost,
		Path:   "/api/chat",
		Body:   []byte(`{"model":"llama-client","messages":[]}`),
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if response.Usage == nil ||
		response.Usage.InputTokens != 17 ||
		response.Usage.OutputTokens != 5 {
		t.Fatalf("usage=%+v", response.Usage)
	}
}

func TestAdapterAcceptsOllamaSuccessWithoutUsageMetadata(t *testing.T) {
	adapter, err := NewAdapter(Config{
		Reseller: domain.Reseller{
			ID:           "reseller-ollama",
			ProviderType: domain.ProviderOllama,
			BaseURL:      "https://ollama.example",
		},
		ResellerAPIKey:       "sk_provider_secret",
		MaxResponseBodyBytes: 1024,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"message":{"content":"ok"}}`)),
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	response, err := adapter.Forward(context.Background(), ports.ForwardRequest{
		Route:  ollamaRoute(domain.EndpointChat, "llama-client", "llama-client", domain.ModelRewritePolicyNone),
		Method: http.MethodPost,
		Path:   "/api/chat",
		Body:   []byte(`{"model":"llama-client","messages":[]}`),
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if response.Usage != nil {
		t.Fatalf("usage=%+v", response.Usage)
	}
}

func TestExtractUsageRejectsMalformedOllamaUsage(t *testing.T) {
	tests := []string{
		`{"prompt_eval_count":-1,"eval_count":5}`,
		`{"prompt_eval_count":"17","eval_count":5}`,
		`{"prompt_eval_count":17.5,"eval_count":5}`,
		`{"prompt_eval_count":17,"eval_count":-5}`,
	}
	for _, payload := range tests {
		if _, err := ExtractUsage([]byte(payload)); err == nil {
			t.Fatalf("payload=%s unexpectedly succeeded", payload)
		}
	}
}

func TestAdapterRejectsUnsupportedOllamaRoute(t *testing.T) {
	adapter, err := NewAdapter(Config{
		Reseller: domain.Reseller{
			ID:           "reseller-ollama",
			ProviderType: domain.ProviderOllama,
			BaseURL:      "https://ollama.example",
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
			Route:  ollamaRoute(domain.EndpointChat, "client-model", "client-model", domain.ModelRewritePolicyNone),
			Method: http.MethodGet,
			Path:   "/api/chat",
			Body:   []byte(`{"model":"client-model"}`),
		},
		{
			Route:  ollamaRoute(domain.EndpointChat, "client-model", "client-model", domain.ModelRewritePolicyNone),
			Method: http.MethodPost,
			Path:   "/api/embeddings",
			Body:   []byte(`{"model":"client-model"}`),
		},
		{
			Route:  ollamaRoute(domain.EndpointChat, "client-model", "client-model", domain.ModelRewritePolicyNone),
			Method: http.MethodPost,
			Path:   "/api/chat",
			Body:   []byte(`{"model":"other-model"}`),
		},
	}
	for _, request := range tests {
		if _, err := adapter.Forward(context.Background(), request); err == nil {
			t.Fatalf("request=%+v unexpectedly succeeded", request)
		}
	}
}

func TestFactoryBuildsOllamaNativeClient(t *testing.T) {
	factory, err := NewFactory(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	client, err := factory.Build(ports.ForwardingAdapterFactoryInput{
		Route: ollamaRoute(
			domain.EndpointChat,
			"client-model",
			"client-model",
			domain.ModelRewritePolicyNone,
		),
		Reseller: domain.Reseller{
			ID:           "reseller-ollama",
			ProviderType: domain.ProviderOllama,
			BaseURL:      "https://ollama.example",
		},
		ResellerAPIKey:       "sk_provider_secret",
		MaxResponseBodyBytes: 1024,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}

func ollamaRoute(
	endpoint domain.EndpointKind,
	clientModel string,
	providerModel string,
	policy domain.ModelRewritePolicy,
) domain.Route {
	return domain.Route{
		ID:                 "route-ollama",
		ResellerID:         "reseller-ollama",
		ProviderType:       domain.ProviderOllama,
		APIFamily:          domain.APIFamilyOllamaNative,
		EndpointKind:       endpoint,
		ClientModel:        clientModel,
		ProviderModel:      providerModel,
		ModelRewritePolicy: policy,
		Enabled:            true,
	}
}
