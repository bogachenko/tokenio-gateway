package anthropicnative

import (
	"fmt"
	"net/http"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type Factory struct {
	transport http.RoundTripper
}

var (
	_ ports.ForwardingAdapterFactory         = (*Factory)(nil)
	_ ports.ForwardingAdapterEndpointSupport = (*Factory)(nil)
)

func NewFactory(transport http.RoundTripper) (*Factory, error) {
	if transport == nil {
		return nil, ErrInvalidAdapterConfig
	}
	return &Factory{transport: transport}, nil
}

func (f *Factory) SupportsForwardingEndpoint(
	endpointKind domain.EndpointKind,
) bool {
	return f != nil && endpointKind == domain.EndpointChat
}

func (f *Factory) Build(
	input ports.ForwardingAdapterFactoryInput,
) (ports.ForwardingClient, error) {
	if f == nil || f.transport == nil {
		return nil, ErrInvalidAdapterConfig
	}
	if input.Route.APIFamily != domain.APIFamilyAnthropicNative ||
		input.Route.EndpointKind != domain.EndpointChat ||
		input.Route.ProviderType == "" ||
		input.Route.ProviderType != input.Reseller.ProviderType ||
		input.Route.ResellerID != input.Reseller.ID {
		return nil, ErrUnsupportedRoute
	}
	adapter, err := NewAdapter(Config{
		Reseller:             input.Reseller,
		ResellerAPIKey:       input.ResellerAPIKey,
		Transport:            f.transport,
		MaxResponseBodyBytes: input.MaxResponseBodyBytes,
	})
	if err != nil {
		return nil, fmt.Errorf(
			"construct anthropic native forwarding adapter: %w",
			err,
		)
	}
	client, err := newClient(adapter)
	if err != nil {
		return nil, fmt.Errorf(
			"construct anthropic native forwarding client: %w",
			err,
		)
	}
	return client, nil
}
