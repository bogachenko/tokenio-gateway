package app

import (
	"fmt"
	"net/http"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/forwarding/openaicompat"
	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/forwarding/registry"
	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/forwarding/rewritesupport"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type ForwardingInfrastructureGraph struct {
	AdapterSupport      ports.ForwardingAdapterSupport
	ModelRewriteSupport ports.ModelIdentifierRewriteSupport
	AdapterFactory      ports.ForwardingAdapterFactory
	UsageExtractor      ports.UsageExtractor
}

func NewForwardingInfrastructureGraph() (ForwardingInfrastructureGraph, error) {
	rewriteRegistry, err := rewritesupport.NewRegistry(
		openaicompat.NewModelRewriteSupport(),
	)
	if err != nil {
		return ForwardingInfrastructureGraph{},
			fmt.Errorf(
				"construct model rewrite support registry: %w",
				err,
			)
	}

	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return ForwardingInfrastructureGraph{},
			fmt.Errorf("default HTTP transport has unexpected type")
	}
	openAICompatibleFactory, err := openaicompat.NewFactory(
		transport.Clone(),
		openaicompat.StatusClassifier{},
	)
	if err != nil {
		return ForwardingInfrastructureGraph{},
			fmt.Errorf(
				"construct openai-compatible forwarding factory: %w",
				err,
			)
	}

	adapterRegistry, err := registry.New(
		openAICompatibleRegistrations(
			openAICompatibleFactory,
		)...,
	)
	if err != nil {
		return ForwardingInfrastructureGraph{},
			fmt.Errorf(
				"construct forwarding adapter factory registry: %w",
				err,
			)
	}

	graph := ForwardingInfrastructureGraph{
		AdapterSupport:      adapterRegistry,
		ModelRewriteSupport: rewriteRegistry,
		AdapterFactory:      adapterRegistry,
		UsageExtractor:      openaicompat.NewUsageExtractor(),
	}
	if err := graph.Validate(); err != nil {
		return ForwardingInfrastructureGraph{},
			fmt.Errorf(
				"validate forwarding infrastructure graph: %w",
				err,
			)
	}
	return graph, nil
}

func (g ForwardingInfrastructureGraph) Validate() error {
	if g.AdapterSupport == nil {
		return fmt.Errorf("forwarding adapter support registry is nil")
	}
	if g.ModelRewriteSupport == nil {
		return fmt.Errorf(
			"model rewrite support registry is nil",
		)
	}
	if g.AdapterFactory == nil {
		return fmt.Errorf(
			"forwarding adapter factory registry is nil",
		)
	}
	if g.UsageExtractor == nil {
		return fmt.Errorf(
			"usage extractor is nil",
		)
	}
	return nil
}

func openAICompatibleRegistrations(
	factory ports.ForwardingAdapterFactory,
) []registry.Registration {
	providerTypes := [...]domain.ProviderType{
		domain.ProviderOpenAI,
		domain.ProviderOpenRouter,
		domain.ProviderTogether,
		domain.ProviderGroq,
		domain.ProviderOllama,
		domain.ProviderLMStudio,
		domain.ProviderVLLM,
		domain.ProviderHydra,
	}
	registrations := make(
		[]registry.Registration,
		0,
		len(providerTypes),
	)
	for _, providerType := range providerTypes {
		registrations = append(
			registrations,
			registry.Registration{
				Key: registry.Key{
					APIFamily: domain.
						APIFamilyOpenAICompatible,
					ProviderType: providerType,
				},
				Factory: factory,
			},
		)
	}
	return registrations
}
