package anthropicnative

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const (
	resellerSecret = "rk_anthropic_reseller_secret"
	tokenioSecret  = "sk_tokenio_client_secret"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func baseReseller() domain.Reseller {
	return domain.Reseller{
		ID:           "reseller-anthropic",
		ProviderType: "anthropic",
		BaseURL:      "https://anthropic.example/api",
	}
}

func baseRoute() domain.Route {
	return domain.Route{
		ID:                 "route-anthropic",
		ResellerID:         "reseller-anthropic",
		ProviderType:       "anthropic",
		APIFamily:          domain.APIFamilyAnthropicNative,
		EndpointKind:       domain.EndpointChat,
		ClientModel:        "claude-client",
		ProviderModel:      "claude-client",
		ModelRewritePolicy: domain.ModelRewritePolicyNone,
	}
}

func newTestAdapter(t *testing.T, transport http.RoundTripper, opts ...func(*Config)) *Adapter {
	t.Helper()
	config := Config{
		Reseller:             baseReseller(),
		ResellerAPIKey:       resellerSecret,
		Transport:            transport,
		MaxResponseBodyBytes: 1024,
	}
	for _, opt := range opts {
		opt(&config)
	}
	adapter, err := NewAdapter(config)
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	return adapter
}

func baseForwardRequest() ports.ForwardRequest {
	return ports.ForwardRequest{
		Route:  baseRoute(),
		Method: http.MethodPost,
		Path:   "/v1/messages",
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
		},
		Body: []byte(`{"model":"claude-client","messages":[{"role":"user","content":"hello"}]}`),
	}
}

func TestForwardPreservesBodyAndStripsTokenioCredentials(t *testing.T) {
	body := []byte("{ \n  \"model\" : \"claude-client\", \"messages\" : [{\"role\":\"user\",\"content\":\"hello\"}], \"unknown\": true }")
	original := append([]byte(nil), body...)
	var seenBody []byte
	var seenHeader http.Header
	var seenURL string
	adapter := newTestAdapter(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		seenURL = request.URL.String()
		seenHeader = request.Header.Clone()
		var err error
		seenBody, err = io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1"}`)),
		}, nil
	}))
	req := baseForwardRequest()
	req.Body = body
	req.Headers = map[string][]string{
		"Content-Type":      {"application/json"},
		"x-api-key":         {tokenioSecret},
		"Authorization":     {"Bearer " + tokenioSecret},
		"x-goog-api-key":    {tokenioSecret},
		"X-Service-Token":   {"service-token"},
		"Connection":        {"Foo"},
		"Foo":               {"remove"},
		"anthropic-version": {"2023-06-01"},
	}

	response, err := adapter.Forward(context.Background(), req)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if seenURL != "https://anthropic.example/api/v1/messages" {
		t.Fatalf("upstream URL = %q", seenURL)
	}
	if !bytes.Equal(body, original) {
		t.Fatalf("caller body mutated: %q", body)
	}
	if !bytes.Equal(seenBody, original) {
		t.Fatalf("upstream body changed: %q", seenBody)
	}
	if seenHeader.Get("x-api-key") != resellerSecret {
		t.Fatalf("x-api-key = %q", seenHeader.Get("x-api-key"))
	}
	for _, name := range []string{"Authorization", "x-goog-api-key", "X-Service-Token", "Foo"} {
		if seenHeader.Get(name) != "" {
			t.Fatalf("%s leaked: %#v", name, seenHeader.Values(name))
		}
	}
	if seenHeader.Get("anthropic-version") != "2023-06-01" {
		t.Fatalf("ordinary native header lost: %#v", seenHeader)
	}
	if response.StatusCode != http.StatusOK || string(response.Body) != `{"id":"msg_1"}` {
		t.Fatalf("response = %#v", response)
	}
}

func TestForwardRejectsCrossFamilyRouteSelection(t *testing.T) {
	adapter := newTestAdapter(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	}))
	tests := []struct {
		name   string
		mutate func(*ports.ForwardRequest)
		want   error
	}{
		{"wrong API family", func(request *ports.ForwardRequest) { request.Route.APIFamily = domain.APIFamilyOpenAICompatible }, ErrUnsupportedRoute},
		{"wrong endpoint kind", func(request *ports.ForwardRequest) { request.Route.EndpointKind = domain.EndpointEmbeddings }, ErrUnsupportedRoute},
		{"wrong method", func(request *ports.ForwardRequest) { request.Method = http.MethodGet }, ErrInvalidForwardRequest},
		{"wrong path", func(request *ports.ForwardRequest) { request.Path = "/v1/chat/completions" }, ErrUnsupportedRoute},
		{"wrong reseller", func(request *ports.ForwardRequest) { request.Route.ResellerID = "other" }, ErrUnsupportedRoute},
		{"wrong provider", func(request *ports.ForwardRequest) { request.Route.ProviderType = "other" }, ErrUnsupportedRoute},
		{"provider model without rewrite", func(request *ports.ForwardRequest) { request.Route.ProviderModel = "claude-provider" }, ErrUnsupportedRoute},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := baseForwardRequest()
			test.mutate(&req)
			_, err := adapter.Forward(context.Background(), req)
			if !errors.Is(err, test.want) {
				t.Fatalf("err = %v, want %v", err, test.want)
			}
		})
	}
}

func TestProviderModelRewriteOnlyChangesTopLevelModel(t *testing.T) {
	var seenBody []byte
	adapter := newTestAdapter(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var err error
		seenBody, err = io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	}))
	req := baseForwardRequest()
	req.Route.ProviderModel = "claude-provider"
	req.Route.ModelRewritePolicy = domain.ModelRewritePolicyProviderModel
	req.Body = []byte(`{"model":"claude-client","nested":{"model":"claude-client"}}`)
	original := append([]byte(nil), req.Body...)
	_, err := adapter.Forward(context.Background(), req)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if !bytes.Equal(req.Body, original) {
		t.Fatalf("caller body mutated: %q", req.Body)
	}
	if !strings.Contains(string(seenBody), `"model":"claude-provider"`) ||
		!strings.Contains(string(seenBody), `"nested":{"model":"claude-client"}`) {
		t.Fatalf("rewrite body = %s", seenBody)
	}
}

func TestConfigAndURLValidation(t *testing.T) {
	validTransport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})
	valid := Config{Reseller: baseReseller(), ResellerAPIKey: resellerSecret, Transport: validTransport, MaxResponseBodyBytes: 1}
	tests := []func(*Config){
		func(value *Config) { value.Reseller.ID = "" },
		func(value *Config) { value.Reseller.ProviderType = "" },
		func(value *Config) { value.Reseller.BaseURL = "" },
		func(value *Config) { value.ResellerAPIKey = "" },
		func(value *Config) { value.Transport = nil },
		func(value *Config) { value.MaxResponseBodyBytes = 0 },
		func(value *Config) { value.Reseller.BaseURL = "provider.example" },
		func(value *Config) { value.Reseller.BaseURL = "ftp://provider.example" },
		func(value *Config) { value.Reseller.BaseURL = "https://user:pass@provider.example" },
		func(value *Config) { value.Reseller.BaseURL = "https://provider.example?key=secret" },
	}
	for _, mutate := range tests {
		config := valid
		mutate(&config)
		if _, err := NewAdapter(config); !errors.Is(err, ErrInvalidAdapterConfig) {
			t.Fatalf("err = %v", err)
		}
	}
}

func TestInputHeadersAreNotMutated(t *testing.T) {
	input := map[string][]string{"x-api-key": {tokenioSecret}, "anthropic-version": {"2023-06-01"}}
	original := cloneHeaders(input)
	adapter := newTestAdapter(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	}))
	req := baseForwardRequest()
	req.Headers = input
	_, err := adapter.Forward(context.Background(), req)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if !reflect.DeepEqual(input, original) {
		t.Fatalf("headers mutated: %#v", input)
	}
}
