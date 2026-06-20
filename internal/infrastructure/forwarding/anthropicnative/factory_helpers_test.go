package anthropicnative

import "github.com/bogachenko/tokenio-gateway/internal/ports"

func anthropicFactoryInput() ports.ForwardingAdapterFactoryInput {
	route := baseRoute()
	reseller := baseReseller()
	reseller.BaseURL = "https://anthropic.example/root"
	return ports.ForwardingAdapterFactoryInput{
		Route:                route,
		Reseller:             reseller,
		ResellerAPIKey:       resellerSecret,
		MaxResponseBodyBytes: 1024,
	}
}
