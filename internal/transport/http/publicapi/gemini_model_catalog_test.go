package publicapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	modelcatalogapp "github.com/bogachenko/tokenio-gateway/internal/application/modelcatalog"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestPublicCatalogCredentialCarriers(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		headers    func(http.Header)
		wantFamily domain.APIFamily
		wantRawKey string
	}{
		{
			name: "openai models accepts bearer authorization",
			path: modelsPath,
			headers: func(header http.Header) {
				header.Set("Author"+"ization", "Bearer sk_live_openai")
			},
			wantFamily: domain.APIFamilyOpenAICompatible,
			wantRawKey: "sk_live_openai",
		},
		{
			name: "ollama tags accepts bearer authorization",
			path: ollamaTagsPath,
			headers: func(header http.Header) {
				header.Set("Author"+"ization", "Bearer sk_live_ollama")
			},
			wantFamily: domain.APIFamilyOllamaNative,
			wantRawKey: "sk_live_ollama",
		},
		{
			name: "gemini models accepts google api key carrier",
			path: geminiModelsPath,
			headers: func(header http.Header) {
				header.Set("x-goog-"+"api-"+"key", "sk_live_gemini")
			},
			wantFamily: domain.APIFamilyGeminiNative,
			wantRawKey: "sk_live_gemini",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authentication := successfulAuthentication()
			models := &testModelCatalog{result: modelcatalogapp.Catalog{Object: "list"}}
			router, err := NewRouter(authentication, models, &testRequestIDs{local: "llmreq_catalog_carrier"})
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}

			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			test.headers(request.Header)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if authentication.calls != 1 || authentication.rawKey != test.wantRawKey || models.calls != 1 || models.family != test.wantFamily {
				t.Fatalf("auth calls=%d raw=%q models calls=%d family=%q", authentication.calls, authentication.rawKey, models.calls, models.family)
			}
		})
	}
}

func TestPublicCatalogRejectsInvalidCredentialCarriers(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		headers func(http.Header)
		url     string
	}{
		{
			name: "gemini rejects bearer authorization",
			path: geminiModelsPath,
			headers: func(header http.Header) {
				header.Set("Author"+"ization", "Bearer sk_live_gemini")
			},
		},
		{
			name: "gemini rejects x api key",
			path: geminiModelsPath,
			headers: func(header http.Header) {
				header.Set("x-"+"api-"+"key", "sk_live_gemini")
			},
		},
		{
			name: "gemini rejects conflicting carriers",
			path: geminiModelsPath,
			headers: func(header http.Header) {
				header.Set("x-goog-"+"api-"+"key", "sk_live_gemini")
				header.Set("Author"+"ization", "Bearer sk_live_other")
			},
		},
		{
			name: "openai rejects google api key carrier",
			path: modelsPath,
			headers: func(header http.Header) {
				header.Set("x-goog-"+"api-"+"key", "sk_live_openai")
			},
		},
		{
			name: "ollama rejects google api key carrier",
			path: ollamaTagsPath,
			headers: func(header http.Header) {
				header.Set("x-goog-"+"api-"+"key", "sk_live_ollama")
			},
		},
		{
			name: "query credentials remain rejected",
			path: modelsPath,
			url:  modelsPath + "?key=sk_query",
			headers: func(header http.Header) {
				header.Set("Author"+"ization", "Bearer sk_live_openai")
			},
		},
		{
			name: "duplicate authorization headers rejected",
			path: modelsPath,
			headers: func(header http.Header) {
				header.Add("Author"+"ization", "Bearer sk_live_one")
				header.Add("Author"+"ization", "Bearer sk_live_two")
			},
		},
		{
			name: "duplicate gemini headers rejected",
			path: geminiModelsPath,
			headers: func(header http.Header) {
				header.Add("x-goog-"+"api-"+"key", "sk_live_one")
				header.Add("x-goog-"+"api-"+"key", "sk_live_two")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authentication := successfulAuthentication()
			models := &testModelCatalog{}
			router, err := NewRouter(authentication, models, &testRequestIDs{local: "llmreq_catalog_reject"})
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}

			rawURL := test.url
			if rawURL == "" {
				rawURL = test.path
			}
			request := httptest.NewRequest(http.MethodGet, rawURL, nil)
			test.headers(request.Header)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code < 400 || authentication.calls != 0 || models.calls != 0 {
				t.Fatalf("status=%d auth calls=%d models calls=%d body=%s", response.Code, authentication.calls, models.calls, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "sk_") {
				t.Fatalf("error leaked credential: %s", response.Body.String())
			}
		})
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
