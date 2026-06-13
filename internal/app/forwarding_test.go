package app

import (
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
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
