package app

import (
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/forwarding/registry"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestNewForwardingInfrastructureGraph(
	t *testing.T,
) {
	graph, err := NewForwardingInfrastructureGraph()
	if err != nil {
		t.Fatalf(
			"NewForwardingInfrastructureGraph: %v",
			err,
		)
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf(
			"forwarding infrastructure graph: %v",
			err,
		)
	}
	if !graph.ModelRewriteSupport.
		SupportsModelIdentifierRewrite(
			domain.APIFamilyOpenAICompatible,
			domain.ProviderOpenAI,
		) {
		t.Fatal(
			"OpenAI-compatible adapter support is absent",
		)
	}
	if graph.UsageExtractor == nil {
		t.Fatal("usage extractor is not wired")
	}
	if graph.ModelRewriteSupport.
		SupportsModelIdentifierRewrite(
			domain.APIFamilyAnthropicNative,
			domain.ProviderAnthropic,
		) {
		t.Fatal(
			"unregistered native adapter support is present",
		)
	}
}

func TestForwardingInfrastructureGraphValidateRejectsMissingCapability(
	t *testing.T,
) {
	var graph ForwardingInfrastructureGraph
	if err := graph.Validate(); err == nil {
		t.Fatal(
			"expected incomplete forwarding graph error",
		)
	}
}

func TestForwardingInfrastructureGraphWiresFactoryRegistry(
	t *testing.T,
) {
	graph, err := NewForwardingInfrastructureGraph()
	if err != nil {
		t.Fatalf("NewForwardingInfrastructureGraph: %v", err)
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	input := ports.ForwardingAdapterFactoryInput{
		Route: domain.Route{
			ID:           "route-1",
			ResellerID:   "reseller-1",
			ProviderType: domain.ProviderOpenRouter,
			APIFamily:    domain.APIFamilyOpenAICompatible,
		},
		Reseller: domain.Reseller{
			ID:           "reseller-1",
			ProviderType: domain.ProviderOpenRouter,
			BaseURL:      "https://provider.example",
		},
		ResellerAPIKey:       "secret",
		MaxResponseBodyBytes: 1024,
	}
	adapter, err := graph.AdapterFactory.Build(input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if adapter == nil {
		t.Fatal("adapter is nil")
	}
}

func TestForwardingInfrastructureGraphRejectsUnregisteredNativeFactory(
	t *testing.T,
) {
	graph, err := NewForwardingInfrastructureGraph()
	if err != nil {
		t.Fatalf("NewForwardingInfrastructureGraph: %v", err)
	}
	input := ports.ForwardingAdapterFactoryInput{
		Route: domain.Route{
			ID:           "route-1",
			ResellerID:   "reseller-1",
			ProviderType: domain.ProviderGemini,
			APIFamily:    domain.APIFamilyGeminiNative,
		},
		Reseller: domain.Reseller{
			ID:           "reseller-1",
			ProviderType: domain.ProviderGemini,
			BaseURL:      "https://provider.example",
		},
		ResellerAPIKey:       "secret",
		MaxResponseBodyBytes: 1024,
	}
	_, err = graph.AdapterFactory.Build(input)
	if !errors.Is(err, registry.ErrFactoryNotRegistered) {
		t.Fatalf("error=%v", err)
	}
}
