package registry

import (
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type factoryFunc func(
	ports.ForwardingAdapterFactoryInput,
) (ports.ForwardingAdapter, error)

func (function factoryFunc) Build(
	input ports.ForwardingAdapterFactoryInput,
) (ports.ForwardingAdapter, error) {
	return function(input)
}

type adapterStub struct {
	ports.ForwardingAdapter
}

func TestRegistrySelectsExactStructuralKey(t *testing.T) {
	var got ports.ForwardingAdapterFactoryInput
	factory := factoryFunc(
		func(
			input ports.ForwardingAdapterFactoryInput,
		) (ports.ForwardingAdapter, error) {
			got = input
			return adapterStub{}, nil
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
	adapter, err := registry.Build(input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if adapter == nil || got.Route.ID != input.Route.ID {
		t.Fatalf("adapter=%v input=%+v", adapter, got)
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
				) (ports.ForwardingAdapter, error) {
					return adapterStub{}, nil
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
		) (ports.ForwardingAdapter, error) {
			return adapterStub{}, nil
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
				) (ports.ForwardingAdapter, error) {
					return adapterStub{}, nil
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
