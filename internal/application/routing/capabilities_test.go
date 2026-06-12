package routing

import (
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestSupportsChecksEveryCapabilityField(t *testing.T) {
	all := domain.CapabilitySet{
		Chat:             true,
		Embeddings:       true,
		ImagesGeneration: true,
		Tools:            true,
		ToolChoice:       true,
		ResponseFormat:   true,
		JSONSchema:       true,
		ImageInput:       true,
		AudioInput:       true,
		FileInput:        true,
		VideoInput:       true,
		Reasoning:        true,
	}

	if !Supports(domain.CapabilitySet{}, domain.CapabilitySet{}) {
		t.Fatal("requested=false capabilities must not require support")
	}
	if !Supports(all, all) {
		t.Fatal("all requested capabilities should be supported")
	}

	tests := []struct {
		name      string
		requested domain.CapabilitySet
		available domain.CapabilitySet
	}{
		{name: "chat", requested: domain.CapabilitySet{Chat: true}},
		{name: "embeddings", requested: domain.CapabilitySet{Embeddings: true}},
		{name: "images_generation", requested: domain.CapabilitySet{ImagesGeneration: true}},
		{name: "tools", requested: domain.CapabilitySet{Tools: true}},
		{name: "tool_choice", requested: domain.CapabilitySet{ToolChoice: true}},
		{name: "response_format", requested: domain.CapabilitySet{ResponseFormat: true}},
		{name: "json_schema", requested: domain.CapabilitySet{JSONSchema: true}},
		{name: "image_input", requested: domain.CapabilitySet{ImageInput: true}},
		{name: "audio_input", requested: domain.CapabilitySet{AudioInput: true}},
		{name: "file_input", requested: domain.CapabilitySet{FileInput: true}},
		{name: "video_input", requested: domain.CapabilitySet{VideoInput: true}},
		{name: "reasoning", requested: domain.CapabilitySet{Reasoning: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if Supports(tt.available, tt.requested) {
				t.Fatal("requested=true must require matching available=true")
			}
			if !Supports(all, tt.requested) {
				t.Fatal("requested field should pass when available has it")
			}
		})
	}
}

func TestSelectionCapabilityClassification(t *testing.T) {
	now := fixedNow()

	t.Run("missing capability skipped", func(t *testing.T) {
		missing := baseCandidate("route-missing")
		missing.Route.Capabilities = domain.CapabilitySet{Chat: true}
		good := baseCandidate("route-good")
		good.Route.Capabilities = domain.CapabilitySet{Chat: true, Tools: true}

		result, err := Select(baseInput(now, domain.CapabilitySet{Tools: true}, []Candidate{missing, good}))
		if err != nil {
			t.Fatalf("Select returned error: %v", err)
		}
		if result.Selected.Route.ID != "route-good" {
			t.Fatalf("selected = %q, want route-good", result.Selected.Route.ID)
		}
		assertSkippedReasons(t, result.Skipped, []SkipReason{SkipReasonMissingCapability})
	})

	t.Run("all exact key routes miss capabilities", func(t *testing.T) {
		candidate := baseCandidate("route-missing")
		candidate.Route.Capabilities = domain.CapabilitySet{Chat: true}

		result, err := Select(baseInput(now, domain.CapabilitySet{Embeddings: true}, []Candidate{candidate}))
		if !errors.Is(err, ErrUnsupportedCapability) {
			t.Fatalf("error = %v, want ErrUnsupportedCapability", err)
		}
		assertSkippedReasons(t, result.Skipped, []SkipReason{SkipReasonMissingCapability})
	})

	t.Run("capability compatible but operationally unavailable", func(t *testing.T) {
		candidate := baseCandidate("route-disabled")
		candidate.Route.Capabilities = domain.CapabilitySet{Chat: true}
		candidate.Route.Enabled = false

		result, err := Select(baseInput(now, domain.CapabilitySet{Chat: true}, []Candidate{candidate}))
		if !errors.Is(err, ErrNoRouteAvailable) {
			t.Fatalf("error = %v, want ErrNoRouteAvailable", err)
		}
		assertSkippedReasons(t, result.Skipped, []SkipReason{SkipReasonManualDisabled})
	})

	t.Run("capabilities are not inferred from provider type", func(t *testing.T) {
		candidate := baseCandidate("route-provider-label")
		candidate.Reseller.ProviderType = domain.ProviderType("tools-provider-label")
		candidate.Route.ProviderType = candidate.Reseller.ProviderType
		candidate.Route.Capabilities = domain.CapabilitySet{}

		_, err := Select(baseInput(now, domain.CapabilitySet{Tools: true}, []Candidate{candidate}))
		if !errors.Is(err, ErrUnsupportedCapability) {
			t.Fatalf("error = %v, want ErrUnsupportedCapability", err)
		}
	})

	t.Run("capabilities are not inferred from model name", func(t *testing.T) {
		candidate := baseCandidate("route-model-label")
		candidate.Route.ClientModel = "chat-tools-model"
		candidate.Route.ProviderModel = "chat-tools-model"
		candidate.Route.Capabilities = domain.CapabilitySet{}
		input := baseInput(now, domain.CapabilitySet{Tools: true}, []Candidate{candidate})
		input.Query.ClientModel = "chat-tools-model"

		_, err := Select(input)
		if !errors.Is(err, ErrUnsupportedCapability) {
			t.Fatalf("error = %v, want ErrUnsupportedCapability", err)
		}
	})
}
