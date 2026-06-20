package publicapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	modelcatalogapp "github.com/bogachenko/tokenio-gateway/internal/application/modelcatalog"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestGeminiModelsEndpointAuthenticatesAndUsesGeminiNativeCatalog(t *testing.T) {
	authentication := successfulAuthentication()
	models := &testModelCatalog{
		result: modelcatalogapp.Catalog{
			Object: "list",
			Data: []modelcatalogapp.Model{
				{ID: "gemini-1.5-pro", Object: "model", OwnedBy: "tokenio", Type: "chat", Active: true},
			},
		},
	}
	router, err := NewRouter(authentication, models, &testRequestIDs{local: "llmreq_gemini_models"})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, geminiModelsPath, nil)
	request.Header.Set("Authorization", "Bearer sk_live_gemini")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if authentication.calls != 1 || authentication.rawKey != "sk_live_gemini" || models.calls != 1 || models.family != domain.APIFamilyGeminiNative {
		t.Fatalf("auth calls=%d raw=%q models calls=%d family=%q", authentication.calls, authentication.rawKey, models.calls, models.family)
	}
}

func TestModelCatalogFamilyMappings(t *testing.T) {
	tests := []struct {
		path string
		want domain.APIFamily
	}{
		{path: modelsPath, want: domain.APIFamilyOpenAICompatible},
		{path: geminiModelsPath, want: domain.APIFamilyGeminiNative},
		{path: ollamaTagsPath, want: domain.APIFamilyOllamaNative},
	}
	for _, test := range tests {
		if got := modelCatalogFamily(test.path); got != test.want {
			t.Fatalf("path=%s family=%q want %q", test.path, got, test.want)
		}
	}
}
