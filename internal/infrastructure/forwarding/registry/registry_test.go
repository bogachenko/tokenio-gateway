package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type factoryFunc func(
	ports.ForwardingAdapterFactoryInput,
) (ports.ForwardingClient, error)

func (function factoryFunc) Build(
	input ports.ForwardingAdapterFactoryInput,
) (ports.ForwardingClient, error) {
	return function(input)
}

func (factoryFunc) SupportsForwardingEndpoint(
	endpointKind domain.EndpointKind,
) bool {
	return endpointKind == domain.EndpointChat
}

type clientStub struct{}

func (clientStub) Forward(
	context.Context,
	ports.ForwardingClientRequest,
) (ports.ForwardResponse, error) {
	return ports.ForwardResponse{}, nil
}

func TestRegistrySelectsExactStructuralKey(t *testing.T) {
	var got ports.ForwardingAdapterFactoryInput
	factory := factoryFunc(
		func(
			input ports.ForwardingAdapterFactoryInput,
		) (ports.ForwardingClient, error) {
			got = input
			return clientStub{}, nil
		},
	)
	registry, err := New(
		Registration{
			Key: Key{
				APIFamily:    domain.APIFamilyOpenAICompatible,
				ProviderType: domain.ProviderOpenRouter,
			},
			Factory: factory,
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	input := validBuildInput()
	input.Route.ProviderType = domain.ProviderOpenRouter
	input.Reseller.ProviderType = domain.ProviderOpenRouter
	client, err := registry.Build(input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if client == nil || got.Route.ID != input.Route.ID {
		t.Fatalf("client=%v input=%+v", client, got)
	}
}

func TestRegistryReportsExactForwardingSupport(t *testing.T) {
	registry, err := New(Registration{
		Key: Key{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			ProviderType: domain.ProviderOpenAI,
		},
		Factory: factoryFunc(func(
			ports.ForwardingAdapterFactoryInput,
		) (ports.ForwardingClient, error) {
			return clientStub{}, nil
		}),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !registry.SupportsForwardingAdapter(
		domain.APIFamilyOpenAICompatible,
		domain.ProviderOpenAI,
		domain.EndpointChat,
	) {
		t.Fatal("registered adapter unavailable")
	}
	if registry.SupportsForwardingAdapter(
		domain.APIFamilyGeminiNative,
		domain.ProviderOpenAI,
		domain.EndpointChat,
	) {
		t.Fatal("unknown key available")
	}
	if registry.SupportsForwardingAdapter(
		domain.APIFamilyOpenAICompatible,
		domain.ProviderOpenAI,
		domain.EndpointEmbeddings,
	) {
		t.Fatal("unsupported endpoint available")
	}
}

func TestRegistryRejectsUnknownCombination(t *testing.T) {
	registry, err := New(
		Registration{
			Key: Key{
				APIFamily:    domain.APIFamilyOpenAICompatible,
				ProviderType: domain.ProviderOpenAI,
			},
			Factory: factoryFunc(
				func(
					ports.ForwardingAdapterFactoryInput,
				) (ports.ForwardingClient, error) {
					return clientStub{}, nil
				},
			),
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	input := validBuildInput()
	input.Route.APIFamily = domain.APIFamilyGeminiNative
	_, err = registry.Build(input)
	if !errors.Is(err, ErrFactoryNotRegistered) {
		t.Fatalf("error=%v", err)
	}
}

func TestRegistryRejectsDuplicateRegistration(t *testing.T) {
	key := Key{
		APIFamily:    domain.APIFamilyOpenAICompatible,
		ProviderType: domain.ProviderOpenAI,
	}
	factory := factoryFunc(
		func(
			ports.ForwardingAdapterFactoryInput,
		) (ports.ForwardingClient, error) {
			return clientStub{}, nil
		},
	)
	_, err := New(
		Registration{Key: key, Factory: factory},
		Registration{Key: key, Factory: factory},
	)
	if !errors.Is(err, ErrDuplicateRegistration) {
		t.Fatalf("error=%v", err)
	}
}

func TestRegistryRejectsInvalidBuildInput(t *testing.T) {
	registry, err := New(
		Registration{
			Key: Key{
				APIFamily:    domain.APIFamilyOpenAICompatible,
				ProviderType: domain.ProviderOpenAI,
			},
			Factory: factoryFunc(
				func(
					ports.ForwardingAdapterFactoryInput,
				) (ports.ForwardingClient, error) {
					return clientStub{}, nil
				},
			),
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	input := validBuildInput()
	input.Route.ResellerID = "other"
	_, err = registry.Build(input)
	if !errors.Is(err, ErrInvalidBuildInput) {
		t.Fatalf("error=%v", err)
	}
}

func validBuildInput() ports.ForwardingAdapterFactoryInput {
	return ports.ForwardingAdapterFactoryInput{
		Route: domain.Route{
			ID:           "route-1",
			ResellerID:   "reseller-1",
			ProviderType: domain.ProviderOpenAI,
			APIFamily:    domain.APIFamilyOpenAICompatible,
		},
		Reseller: domain.Reseller{
			ID:           "reseller-1",
			ProviderType: domain.ProviderOpenAI,
		},
		ResellerAPIKey:       "secret",
		MaxResponseBodyBytes: 1024,
	}
}
