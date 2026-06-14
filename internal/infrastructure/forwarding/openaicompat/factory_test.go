package openaicompat

import (
	"errors"
	"net/http"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestFactoryBuildsOpenAICompatibleAdapter(t *testing.T) {
	factory, err := NewFactory(
		roundTripFunc(
			func(*http.Request) (*http.Response, error) {
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
	adapter, err := factory.Build(input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := adapter.Forward(
		t.Context(),
		forwardReq(domain.EndpointChat),
	)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if result.StatusCode != 200 {
		t.Fatalf("result=%+v", result)
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
