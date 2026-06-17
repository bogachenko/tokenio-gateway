package openaicompat

import (
	"errors"
	"net/http"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/application/forwarding"
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
		mustTestClassifierRegistry(t),
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
		mustTestClassifierRegistry(t),
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

type providerClassifier struct {
	kind forwarding.FailureKind
}

func (classifier providerClassifier) Classify(int, map[string][]string, []byte, bool) forwarding.Classification {
	return forwarding.Classification{Kind: classifier.kind}
}

func TestFactoryResolvesClassifierByProviderType(t *testing.T) {
	classifiers, err := NewClassifierRegistry(
		ClassifierRegistration{
			ProviderType: domain.ProviderOpenAI,
			Classifier:   providerClassifier{kind: forwarding.FailureKindAuthError},
		},
		ClassifierRegistration{
			ProviderType: domain.ProviderOpenRouter,
			Classifier:   providerClassifier{kind: forwarding.FailureKindRateLimited},
		},
	)
	if err != nil {
		t.Fatalf("NewClassifierRegistry: %v", err)
	}
	factory, err := NewFactory(
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusBadRequest, "classified"), nil
		}),
		classifiers,
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
	input.Route.ProviderType = domain.ProviderOpenRouter
	input.Reseller.ProviderType = domain.ProviderOpenRouter

	client, err := factory.Build(input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	_, err = client.Forward(
		t.Context(),
		ports.ForwardingClientRequest{
			Route: input.Route,
			Body:  []byte(`{"model":"client-model"}`),
		},
	)
	var failure *forwarding.Failure
	if !errors.As(err, &failure) || failure.Kind != forwarding.FailureKindRateLimited {
		t.Fatalf("failure=%#v err=%v", failure, err)
	}
}

func TestFactoryRejectsProviderWithoutClassifier(t *testing.T) {
	classifiers, err := NewClassifierRegistry(
		ClassifierRegistration{ProviderType: domain.ProviderOpenAI, Classifier: StatusClassifier{}},
	)
	if err != nil {
		t.Fatalf("NewClassifierRegistry: %v", err)
	}
	factory, err := NewFactory(
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusOK, "ok"), nil
		}),
		classifiers,
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
	input.Route.ProviderType = domain.ProviderHydra
	input.Reseller.ProviderType = domain.ProviderHydra

	_, err = factory.Build(input)
	if !errors.Is(err, ErrClassifierNotRegistered) {
		t.Fatalf("error=%v", err)
	}
}

func mustTestClassifierRegistry(t *testing.T) *ClassifierRegistry {
	t.Helper()
	registry, err := NewClassifierRegistry(
		ClassifierRegistration{ProviderType: domain.ProviderOpenAI, Classifier: StatusClassifier{}},
	)
	if err != nil {
		t.Fatalf("NewClassifierRegistry: %v", err)
	}
	return registry
}
