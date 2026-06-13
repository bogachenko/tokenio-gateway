package openaicompat

import (
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type ModelRewriteSupport struct{}

var _ ports.ModelIdentifierRewriteSupport = (*ModelRewriteSupport)(nil)

func NewModelRewriteSupport() *ModelRewriteSupport {
	return &ModelRewriteSupport{}
}

func (*ModelRewriteSupport) SupportsModelIdentifierRewrite(
	apiFamily domain.APIFamily,
	providerType domain.ProviderType,
) bool {
	if apiFamily !=
		domain.APIFamilyOpenAICompatible {
		return false
	}

	switch providerType {
	case domain.ProviderOpenAI,
		domain.ProviderOpenRouter,
		domain.ProviderTogether,
		domain.ProviderGroq,
		domain.ProviderOllama,
		domain.ProviderLMStudio,
		domain.ProviderVLLM,
		domain.ProviderGemini,
		domain.ProviderAnthropic,
		domain.ProviderHydra:
		return true
	default:
		return false
	}
}
