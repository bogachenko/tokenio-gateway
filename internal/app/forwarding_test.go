package app

import (
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
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
			"Anthropic native model rewrite support must not be wired through OpenAI-compatible rewrite registry",
		)
	}
	if !graph.AdapterSupport.SupportsForwardingAdapter(
		domain.APIFamilyAnthropicNative,
		domain.ProviderAnthropic,
		domain.EndpointChat,
	) {
		t.Fatal("Anthropic native forwarding adapter support is absent")
	}
	if graph.AdapterSupport.SupportsForwardingAdapter(
		domain.APIFamilyAnthropicNative,
		domain.ProviderAnthropic,
		domain.EndpointEmbeddings,
	) {
		t.Fatal("Anthropic native embeddings adapter support is present")
	}
	if !graph.AdapterSupport.SupportsForwardingAdapter(
		domain.APIFamilyGeminiNative,
		domain.ProviderGemini,
		domain.EndpointChat,
	) {
		t.Fatal("Gemini native chat adapter support is absent")
	}
	if !graph.AdapterSupport.SupportsForwardingAdapter(
		domain.APIFamilyGeminiNative,
		domain.ProviderGemini,
		domain.EndpointEmbeddings,
	) {
		t.Fatal("Gemini native embeddings adapter support is absent")
	}
	if graph.AdapterSupport.SupportsForwardingAdapter(
		domain.APIFamilyGeminiNative,
		domain.ProviderGemini,
		domain.EndpointImagesGeneration,
	) {
		t.Fatal("Gemini native images adapter support is present")
	}
	if !graph.AdapterSupport.SupportsForwardingAdapter(
		domain.APIFamilyOllamaNative,
		domain.ProviderOllama,
		domain.EndpointChat,
	) {
		t.Fatal("Ollama native chat adapter support is absent")
	}
	if !graph.AdapterSupport.SupportsForwardingAdapter(
		domain.APIFamilyOllamaNative,
		domain.ProviderOllama,
		domain.EndpointEmbeddings,
	) {
		t.Fatal("Ollama native embeddings adapter support is absent")
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

func TestForwardingInfrastructureGraphWiresOllamaNativeFactory(
	t *testing.T,
) {
	graph, err := NewForwardingInfrastructureGraph()
	if err != nil {
		t.Fatalf("NewForwardingInfrastructureGraph: %v", err)
	}
	input := ports.ForwardingAdapterFactoryInput{
		Route: domain.Route{
			ID:                 "route-ollama",
			ResellerID:         "reseller-ollama",
			ProviderType:       domain.ProviderOllama,
			APIFamily:          domain.APIFamilyOllamaNative,
			EndpointKind:       domain.EndpointChat,
			ClientModel:        "llama-client",
			ProviderModel:      "llama-client",
			ModelRewritePolicy: domain.ModelRewritePolicyNone,
		},
		Reseller: domain.Reseller{
			ID:           "reseller-ollama",
			ProviderType: domain.ProviderOllama,
			BaseURL:      "https://ollama.example",
		},
		ResellerAPIKey:       "rk_ollama_secret",
		MaxResponseBodyBytes: 1024,
	}
	client, err := graph.AdapterFactory.Build(input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if client == nil {
		t.Fatal("Ollama native client is nil")
	}
}

func TestForwardingInfrastructureGraphWiresAnthropicNativeFactory(
	t *testing.T,
) {
	graph, err := NewForwardingInfrastructureGraph()
	if err != nil {
		t.Fatalf("NewForwardingInfrastructureGraph: %v", err)
	}
	input := ports.ForwardingAdapterFactoryInput{
		Route: domain.Route{
			ID:                 "route-anthropic",
			ResellerID:         "reseller-anthropic",
			ProviderType:       domain.ProviderAnthropic,
			APIFamily:          domain.APIFamilyAnthropicNative,
			EndpointKind:       domain.EndpointChat,
			ClientModel:        "claude-client",
			ProviderModel:      "claude-client",
			ModelRewritePolicy: domain.ModelRewritePolicyNone,
		},
		Reseller: domain.Reseller{
			ID:           "reseller-anthropic",
			ProviderType: domain.ProviderAnthropic,
			BaseURL:      "https://anthropic.example",
		},
		ResellerAPIKey:       "rk_anthropic_secret",
		MaxResponseBodyBytes: 1024,
	}
	client, err := graph.AdapterFactory.Build(input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if client == nil {
		t.Fatal("Anthropic native client is nil")
	}
}

func TestForwardingInfrastructureGraphWiresGeminiNativeFactory(
	t *testing.T,
) {
	graph, err := NewForwardingInfrastructureGraph()
	if err != nil {
		t.Fatalf("NewForwardingInfrastructureGraph: %v", err)
	}
	input := ports.ForwardingAdapterFactoryInput{
		Route: domain.Route{
			ID:                 "route-gemini",
			ResellerID:         "reseller-gemini",
			ProviderType:       domain.ProviderGemini,
			APIFamily:          domain.APIFamilyGeminiNative,
			EndpointKind:       domain.EndpointChat,
			ClientModel:        "gemini-client",
			ProviderModel:      "gemini-client",
			ModelRewritePolicy: domain.ModelRewritePolicyNone,
		},
		Reseller: domain.Reseller{
			ID:           "reseller-gemini",
			ProviderType: domain.ProviderGemini,
			BaseURL:      "https://gemini.example",
		},
		ResellerAPIKey:       "rk_gemini_secret",
		MaxResponseBodyBytes: 1024,
	}
	client, err := graph.AdapterFactory.Build(input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if client == nil {
		t.Fatal("Gemini native client is nil")
	}
}
