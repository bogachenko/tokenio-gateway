package openaicompat

import (
	"errors"
	"net/http"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestFactoryBuildsOpenAICompatibleClient(t *testing.T) {
	var gotMethod string
	var gotPath string
	factory, err := NewFactory(
		roundTripFunc(
			func(request *http.Request) (*http.Response, error) {
				gotMethod = request.Method
				gotPath = request.URL.Path
				return response(200, "ok"), nil
			},
		),
		StatusClassifier{},
	)
	if err != nil {
		t.Fatalf("NewFactory: %v", err)
	}

	input := ports.ForwardingAdapterFactoryInput{
		Route:                baseRoute(domain.EndpointChat),
		Reseller:             baseReseller(),
		ResellerAPIKey:       resellerSecret,
		MaxResponseBodyBytes: 64,
	}
	client, err := factory.Build(input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := client.Forward(
		t.Context(),
		ports.ForwardingClientRequest{
			Route: input.Route,
			Body:  []byte(`{"model":"client-model"}`),
		},
	)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if result.StatusCode != 200 ||
		gotMethod != http.MethodPost ||
		gotPath != "/api/v1/chat/completions" {
		t.Fatalf(
			"result=%+v method=%q path=%q",
			result,
			gotMethod,
			gotPath,
		)
	}
}

func TestFactoryRejectsWrongAPIFamily(t *testing.T) {
	factory, err := NewFactory(
		roundTripFunc(
			func(*http.Request) (*http.Response, error) {
				return nil, nil
			},
		),
		StatusClassifier{},
	)
	if err != nil {
		t.Fatalf("NewFactory: %v", err)
	}

	input := ports.ForwardingAdapterFactoryInput{
		Route:                baseRoute(domain.EndpointChat),
		Reseller:             baseReseller(),
		ResellerAPIKey:       resellerSecret,
		MaxResponseBodyBytes: 64,
	}
	input.Route.APIFamily = domain.APIFamilyGeminiNative
	_, err = factory.Build(input)
	if !errors.Is(err, ErrUnsupportedRoute) {
		t.Fatalf("error=%v", err)
	}
}
