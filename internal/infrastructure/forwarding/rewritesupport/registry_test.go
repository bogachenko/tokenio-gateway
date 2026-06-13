package rewritesupport

import (
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type testSupport struct {
	family   domain.APIFamily
	provider domain.ProviderType
	calls    int
}

func (s *testSupport) SupportsModelIdentifierRewrite(
	family domain.APIFamily,
	provider domain.ProviderType,
) bool {
	s.calls++
	return family == s.family &&
		provider == s.provider
}

func TestRegistryComposesAdapterOwnedCapabilities(
	t *testing.T,
) {
	first := &testSupport{
		family:   domain.APIFamilyGeminiNative,
		provider: domain.ProviderGemini,
	}
	second := &testSupport{
		family:   domain.APIFamilyOpenAICompatible,
		provider: domain.ProviderOpenAI,
	}

	registry, err := NewRegistry(first, second)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	if !registry.SupportsModelIdentifierRewrite(
		domain.APIFamilyOpenAICompatible,
		domain.ProviderOpenAI,
	) {
		t.Fatal("registered support was not found")
	}
	if first.calls != 1 || second.calls != 1 {
		t.Fatalf(
			"first calls=%d second calls=%d",
			first.calls,
			second.calls,
		)
	}

	if registry.SupportsModelIdentifierRewrite(
		domain.APIFamilyAnthropicNative,
		domain.ProviderAnthropic,
	) {
		t.Fatal("unregistered support was accepted")
	}
}

func TestNewRegistryValidation(t *testing.T) {
	registry, err := NewRegistry()
	if registry != nil ||
		!errors.Is(err, ErrInvalidRegistry) {
		t.Fatalf(
			"empty registry=%v error=%v",
			registry,
			err,
		)
	}

	registry, err = NewRegistry(nil)
	if registry != nil ||
		!errors.Is(err, ErrInvalidRegistry) {
		t.Fatalf(
			"nil support registry=%v error=%v",
			registry,
			err,
		)
	}
}
