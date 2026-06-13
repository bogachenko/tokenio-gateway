package openaicompat

import (
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestModelRewriteSupportMatchesAdapterContract(
	t *testing.T,
) {
	support := NewModelRewriteSupport()

	for _, provider := range []domain.ProviderType{
		domain.ProviderOpenAI,
		domain.ProviderOpenRouter,
		domain.ProviderHydra,
		domain.ProviderGemini,
	} {
		if !support.SupportsModelIdentifierRewrite(
			domain.APIFamilyOpenAICompatible,
			provider,
		) {
			t.Fatalf(
				"OpenAI-compatible rewrite rejected for %q",
				provider,
			)
		}
	}

	if support.SupportsModelIdentifierRewrite(
		domain.APIFamilyGeminiNative,
		domain.ProviderGemini,
	) {
		t.Fatal("native Gemini family was accepted")
	}
	for _, unsupported := range []domain.ProviderType{
		"",
		domain.ProviderType("unknown"),
	} {
		if support.SupportsModelIdentifierRewrite(
			domain.APIFamilyOpenAICompatible,
			unsupported,
		) {
			t.Fatalf(
				"unsupported provider %q was accepted",
				unsupported,
			)
		}
	}
}
