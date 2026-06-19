package gemininative

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
	return f != nil &&
		(endpointKind == domain.EndpointChat ||
			endpointKind == domain.EndpointEmbeddings)
}

func (f *Factory) Build(
	input ports.ForwardingAdapterFactoryInput,
) (ports.ForwardingClient, error) {
	if f == nil || f.transport == nil {
		return nil, ErrInvalidAdapterConfig
	}
	if input.Route.APIFamily != domain.APIFamilyGeminiNative ||
		input.Route.ProviderType != domain.ProviderGemini ||
		input.Route.ProviderType != input.Reseller.ProviderType ||
		input.Route.ResellerID != input.Reseller.ID ||
		!f.SupportsForwardingEndpoint(input.Route.EndpointKind) {
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
			"construct gemini native forwarding adapter: %w",
			err,
		)
	}
	client, err := newClient(adapter)
	if err != nil {
		return nil, fmt.Errorf(
			"construct gemini native forwarding client: %w",
			err,
		)
	}
	return client, nil
}
