package registry

import (
	"fmt"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type Key struct {
	APIFamily    domain.APIFamily
	ProviderType domain.ProviderType
}

type Registration struct {
	Key     Key
	Factory ports.ForwardingAdapterFactory
}

type Registry struct {
	factories map[Key]ports.ForwardingAdapterFactory
}

var (
	_ ports.ForwardingAdapterFactory = (*Registry)(nil)
	_ ports.ForwardingAdapterSupport = (*Registry)(nil)
)

func New(registrations ...Registration) (*Registry, error) {
	if len(registrations) == 0 {
		return nil, ErrInvalidRegistration
	}
	factories := make(
		map[Key]ports.ForwardingAdapterFactory,
		len(registrations),
	)
	for _, registration := range registrations {
		if registration.Key.APIFamily == "" ||
			registration.Key.ProviderType == "" ||
			registration.Factory == nil {
			return nil, ErrInvalidRegistration
		}
		if _, exists := factories[registration.Key]; exists {
			return nil, fmt.Errorf(
				"%w: api_family=%q provider_type=%q",
				ErrDuplicateRegistration,
				registration.Key.APIFamily,
				registration.Key.ProviderType,
			)
		}
		factories[registration.Key] = registration.Factory
	}
	return &Registry{factories: factories}, nil
}

func (r *Registry) SupportsForwardingAdapter(
	apiFamily domain.APIFamily,
	providerType domain.ProviderType,
) bool {
	if r == nil {
		return false
	}
	_, exists := r.factories[Key{
		APIFamily:    apiFamily,
		ProviderType: providerType,
	}]
	return exists
}

func (r *Registry) Build(
	input ports.ForwardingAdapterFactoryInput,
) (ports.ForwardingClient, error) {
	if r == nil || len(r.factories) == 0 {
		return nil, ErrInvalidRegistration
	}
	if err := validateBuildInput(input); err != nil {
		return nil, err
	}
	key := Key{
		APIFamily:    input.Route.APIFamily,
		ProviderType: input.Route.ProviderType,
	}
	factory, exists := r.factories[key]
	if !exists {
		return nil, fmt.Errorf(
			"%w: api_family=%q provider_type=%q",
			ErrFactoryNotRegistered,
			key.APIFamily,
			key.ProviderType,
		)
	}
	client, err := factory.Build(input)
	if err != nil {
		return nil, fmt.Errorf(
			"build forwarding client for api_family=%q provider_type=%q: %w",
			key.APIFamily,
			key.ProviderType,
			err,
		)
	}
	if client == nil {
		return nil, fmt.Errorf(
			"%w: factory returned nil client",
			ErrInvalidBuildInput,
		)
	}
	return client, nil
}

func validateBuildInput(
	input ports.ForwardingAdapterFactoryInput,
) error {
	if strings.TrimSpace(input.Route.ID) == "" ||
		strings.TrimSpace(input.Route.ResellerID) == "" ||
		strings.TrimSpace(input.Reseller.ID) == "" ||
		input.Route.ResellerID != input.Reseller.ID ||
		input.Route.ProviderType == "" ||
		input.Route.ProviderType != input.Reseller.ProviderType ||
		input.Route.APIFamily == "" ||
		strings.TrimSpace(input.ResellerAPIKey) == "" ||
		input.MaxResponseBodyBytes <= 0 {
		return ErrInvalidBuildInput
	}
	return nil
}
