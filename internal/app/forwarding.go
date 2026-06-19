package app

import (
	"fmt"
	"net/http"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/forwarding/anthropicnative"
	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/forwarding/gemininative"
	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/forwarding/ollamanative"
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
	providerTypes := openAICompatibleProviderTypes()
	classifierRegistrations := make(
		[]openaicompat.ClassifierRegistration,
		0,
		len(providerTypes),
	)
	for _, providerType := range providerTypes {
		classifierRegistrations = append(
			classifierRegistrations,
			openaicompat.ClassifierRegistration{
				ProviderType: providerType,
				Classifier:   openaicompat.StatusClassifier{},
			},
		)
	}
	classifierRegistry, err := openaicompat.NewClassifierRegistry(
		classifierRegistrations...,
	)
	if err != nil {
		return ForwardingInfrastructureGraph{},
			fmt.Errorf(
				"construct openai-compatible classifier registry: %w",
				err,
			)
	}
	openAICompatibleFactory, err := openaicompat.NewFactory(
		transport.Clone(),
		classifierRegistry,
	)
	if err != nil {
		return ForwardingInfrastructureGraph{},
			fmt.Errorf(
				"construct openai-compatible forwarding factory: %w",
				err,
			)
	}
	anthropicFactory, err := anthropicnative.NewFactory(
		transport.Clone(),
	)
	if err != nil {
		return ForwardingInfrastructureGraph{},
			fmt.Errorf(
				"construct anthropic-native forwarding factory: %w",
				err,
			)
	}
	geminiFactory, err := gemininative.NewFactory(
		transport.Clone(),
	)
	if err != nil {
		return ForwardingInfrastructureGraph{},
			fmt.Errorf(
				"construct gemini-native forwarding factory: %w",
				err,
			)
	}
	ollamaFactory, err := ollamanative.NewFactory(
		transport.Clone(),
	)
	if err != nil {
		return ForwardingInfrastructureGraph{},
			fmt.Errorf(
				"construct ollama-native forwarding factory: %w",
				err,
			)
	}

	adapterRegistrations := openAICompatibleRegistrations(
		openAICompatibleFactory,
	)
	adapterRegistrations = append(
		adapterRegistrations,
		registry.Registration{
			Key: registry.Key{
				APIFamily:    domain.APIFamilyAnthropicNative,
				ProviderType: domain.ProviderAnthropic,
			},
			Factory: anthropicFactory,
		},
		registry.Registration{
			Key: registry.Key{
				APIFamily:    domain.APIFamilyGeminiNative,
				ProviderType: domain.ProviderGemini,
			},
			Factory: geminiFactory,
		},
		registry.Registration{
			Key: registry.Key{
				APIFamily:    domain.APIFamilyOllamaNative,
				ProviderType: domain.ProviderOllama,
			},
			Factory: ollamaFactory,
		},
	)
	adapterRegistry, err := registry.New(
		adapterRegistrations...,
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

func openAICompatibleProviderTypes() []domain.ProviderType {
	return []domain.ProviderType{
		domain.ProviderOpenAI,
		domain.ProviderOpenRouter,
		domain.ProviderTogether,
		domain.ProviderGroq,
		domain.ProviderOllama,
		domain.ProviderLMStudio,
		domain.ProviderVLLM,
		domain.ProviderHydra,
	}
}

func openAICompatibleRegistrations(
	factory ports.ForwardingAdapterFactory,
) []registry.Registration {
	providerTypes := openAICompatibleProviderTypes()
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
