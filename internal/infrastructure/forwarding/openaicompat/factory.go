package openaicompat

import (
	"fmt"
	"net/http"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type Factory struct {
	transport  http.RoundTripper
	classifier ErrorClassifier
}

var _ ports.ForwardingAdapterFactory = (*Factory)(nil)

func NewFactory(
	transport http.RoundTripper,
	classifier ErrorClassifier,
) (*Factory, error) {
	if transport == nil || classifier == nil {
		return nil, ErrInvalidAdapterConfig
	}
	return &Factory{
		transport:  transport,
		classifier: classifier,
	}, nil
}

func (f *Factory) Build(
	input ports.ForwardingAdapterFactoryInput,
) (ports.ForwardingAdapter, error) {
	if f == nil || f.transport == nil || f.classifier == nil {
		return nil, ErrInvalidAdapterConfig
	}
	if input.Route.APIFamily !=
		domain.APIFamilyOpenAICompatible ||
		input.Route.ProviderType == "" ||
		input.Route.ProviderType != input.Reseller.ProviderType ||
		input.Route.ResellerID != input.Reseller.ID {
		return nil, ErrUnsupportedRoute
	}
	adapter, err := NewAdapter(
		Config{
			Reseller:             input.Reseller,
			ResellerAPIKey:       input.ResellerAPIKey,
			Transport:            f.transport,
			MaxResponseBodyBytes: input.MaxResponseBodyBytes,
		},
		f.classifier,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"construct openai-compatible forwarding adapter: %w",
			err,
		)
	}
	return adapter, nil
}
