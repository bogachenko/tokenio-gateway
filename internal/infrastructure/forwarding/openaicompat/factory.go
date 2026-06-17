package openaicompat

import (
	"fmt"
	"net/http"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type Factory struct {
	transport   http.RoundTripper
	classifiers ErrorClassifierResolver
}

var (
	_ ports.ForwardingAdapterFactory         = (*Factory)(nil)
	_ ports.ForwardingAdapterEndpointSupport = (*Factory)(nil)
)

func NewFactory(
	transport http.RoundTripper,
	classifiers ErrorClassifierResolver,
) (*Factory, error) {
	if transport == nil || classifiers == nil {
		return nil, ErrInvalidAdapterConfig
	}
	return &Factory{
		transport:   transport,
		classifiers: classifiers,
	}, nil
}

func (f *Factory) SupportsForwardingEndpoint(
	endpointKind domain.EndpointKind,
) bool {
	if f == nil {
		return false
	}
	_, supported := endpointPath(endpointKind)
	return supported
}

func (f *Factory) Build(
	input ports.ForwardingAdapterFactoryInput,
) (ports.ForwardingClient, error) {
	if f == nil || f.transport == nil || f.classifiers == nil {
		return nil, ErrInvalidAdapterConfig
	}
	if input.Route.APIFamily !=
		domain.APIFamilyOpenAICompatible ||
		input.Route.ProviderType == "" ||
		input.Route.ProviderType != input.Reseller.ProviderType ||
		input.Route.ResellerID != input.Reseller.ID {
		return nil, ErrUnsupportedRoute
	}
	classifier, err := f.classifiers.Resolve(input.Route.ProviderType)
	if err != nil {
		return nil, fmt.Errorf(
			"resolve openai-compatible error classifier: %w",
			err,
		)
	}
	adapter, err := NewAdapter(
		Config{
			Reseller:             input.Reseller,
			ResellerAPIKey:       input.ResellerAPIKey,
			Transport:            f.transport,
			MaxResponseBodyBytes: input.MaxResponseBodyBytes,
		},
		classifier,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"construct openai-compatible forwarding adapter: %w",
			err,
		)
	}
	client, err := newClient(adapter)
	if err != nil {
		return nil, fmt.Errorf(
			"construct openai-compatible forwarding client: %w",
			err,
		)
	}
	return client, nil
}
