package anthropicnative

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestFactorySupportsOnlyAnthropicChatEndpoint(t *testing.T) {
	factory, err := NewFactory(roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, nil }))
	if err != nil { t.Fatalf("NewFactory: %v", err) }
	if !factory.SupportsForwardingEndpoint(domain.EndpointChat) { t.Fatal("chat endpoint unsupported") }
	for _, endpointKind := range []domain.EndpointKind{domain.EndpointEmbeddings, domain.EndpointImagesGeneration, domain.EndpointModels} {
		if factory.SupportsForwardingEndpoint(endpointKind) { t.Fatalf("%s endpoint unexpectedly supported", endpointKind) }
	}
}

func TestFactoryBuildsAnthropicMessagesClient(t *testing.T) {
	var seenURL string
	var seenHeader http.Header
	var seenBody []byte
	factory, err := NewFactory(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		seenURL = request.URL.String(); seenHeader = request.Header.Clone()
		var readErr error; seenBody, readErr = io.ReadAll(request.Body); if readErr != nil { t.Fatalf("read body: %v", readErr) }
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"msg_1","usage":{"input_tokens":11,"output_tokens":7}}`))}, nil
	}))
	if err != nil { t.Fatalf("NewFactory: %v", err) }
	body := []byte(`{"model":"claude-client","messages":[{"role":"user","content":"hi"}]}`)
	input := anthropicFactoryInput()
	client, err := factory.Build(input)
	if err != nil { t.Fatalf("Build: %v", err) }
	response, err := client.Forward(context.Background(), ports.ForwardingClientRequest{Route: input.Route, Path: "/v1/messages", Body: body})
	if err != nil { t.Fatalf("Forward: %v", err) }
	if seenURL != "https://anthropic.example/root/v1/messages" { t.Fatalf("seen URL = %q", seenURL) }
	if seenHeader.Get("x-api-key") != resellerSecret { t.Fatalf("x-api-key = %q", seenHeader.Get("x-api-key")) }
	if seenHeader.Get("Content-Type") != "application/json" { t.Fatalf("Content-Type = %q", seenHeader.Get("Content-Type")) }
	if string(seenBody) != string(body) { t.Fatalf("body changed: %s", seenBody) }
	if string(response.Body) != `{"id":"msg_1","usage":{"input_tokens":11,"output_tokens":7}}` { t.Fatalf("response body changed: %s", response.Body) }
	if response.Usage == nil || response.Usage.InputTokens != 11 || response.Usage.OutputTokens != 7 { t.Fatalf("usage = %#v", response.Usage) }
}

func TestFactoryRejectsCrossFamilyOrUnsupportedRoutes(t *testing.T) {
	factory, err := NewFactory(roundTripFunc(func(*http.Request) (*http.Response, error) { return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil }))
	if err != nil { t.Fatalf("NewFactory: %v", err) }
	tests := []struct { name string; mutate func(*ports.ForwardingAdapterFactoryInput) }{
		{name: "openai compatible family", mutate: func(input *ports.ForwardingAdapterFactoryInput) { input.Route.APIFamily = domain.APIFamilyOpenAICompatible }},
		{name: "unsupported endpoint", mutate: func(input *ports.ForwardingAdapterFactoryInput) { input.Route.EndpointKind = domain.EndpointEmbeddings }},
		{name: "reseller mismatch", mutate: func(input *ports.ForwardingAdapterFactoryInput) { input.Reseller.ProviderType = domain.ProviderOpenAI }},
	}
	for _, test := range tests { t.Run(test.name, func(t *testing.T) { input := anthropicFactoryInput(); test.mutate(&input); _, err := factory.Build(input); if !errors.Is(err, ErrUnsupportedRoute) { t.Fatalf("error=%v", err) } }) }
}
