package nativeapi

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestResolveNativeFamilyContract(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		path        string
		family      domain.APIFamily
		endpoint    domain.EndpointKind
		operation   Operation
		modelSource ModelSource
		pathModel   string
		streaming   bool
	}{
		{
			name:        "openai models",
			method:      http.MethodGet,
			path:        "/v1/models",
			family:      domain.APIFamilyOpenAICompatible,
			endpoint:    domain.EndpointModels,
			operation:   OperationModels,
			modelSource: ModelSourceNone,
		},
		{
			name:        "anthropic messages",
			method:      http.MethodPost,
			path:        "/v1/messages",
			family:      domain.APIFamilyAnthropicNative,
			endpoint:    domain.EndpointChat,
			operation:   OperationAnthropicMessages,
			modelSource: ModelSourceBody,
		},
		{
			name:        "gemini generate",
			method:      http.MethodPost,
			path:        "/v1beta/models/gemini-2.5-flash:generateContent",
			family:      domain.APIFamilyGeminiNative,
			endpoint:    domain.EndpointChat,
			operation:   OperationGeminiGenerateContent,
			modelSource: ModelSourcePath,
			pathModel:   "gemini-2.5-flash",
		},
		{
			name:        "gemini streaming contract",
			method:      http.MethodPost,
			path:        "/v1beta/models/gemini-2.5-flash:streamGenerateContent",
			family:      domain.APIFamilyGeminiNative,
			endpoint:    domain.EndpointChat,
			operation:   OperationGeminiStreamGenerate,
			modelSource: ModelSourcePath,
			pathModel:   "gemini-2.5-flash",
			streaming:   true,
		},
		{
			name:        "gemini embeddings",
			method:      http.MethodPost,
			path:        "/v1beta/models/gemini-embedding-001:embedContent",
			family:      domain.APIFamilyGeminiNative,
			endpoint:    domain.EndpointEmbeddings,
			operation:   OperationGeminiEmbedContent,
			modelSource: ModelSourcePath,
			pathModel:   "gemini-embedding-001",
		},
		{
			name:        "ollama chat",
			method:      http.MethodPost,
			path:        "/api/chat",
			family:      domain.APIFamilyOllamaNative,
			endpoint:    domain.EndpointChat,
			operation:   OperationOllamaChat,
			modelSource: ModelSourceBody,
		},
		{
			name:        "ollama generate",
			method:      http.MethodPost,
			path:        "/api/generate",
			family:      domain.APIFamilyOllamaNative,
			endpoint:    domain.EndpointChat,
			operation:   OperationOllamaGenerate,
			modelSource: ModelSourceBody,
		},
		{
			name:        "ollama embeddings",
			method:      http.MethodPost,
			path:        "/api/embeddings",
			family:      domain.APIFamilyOllamaNative,
			endpoint:    domain.EndpointEmbeddings,
			operation:   OperationOllamaEmbeddings,
			modelSource: ModelSourceBody,
		},
		{
			name:        "ollama tags",
			method:      http.MethodGet,
			path:        "/api/tags",
			family:      domain.APIFamilyOllamaNative,
			endpoint:    domain.EndpointModels,
			operation:   OperationOllamaTags,
			modelSource: ModelSourceNone,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := Resolve(test.method, test.path)
			if !ok {
				t.Fatalf(
					"Resolve(%s, %s) did not match",
					test.method,
					test.path,
				)
			}
			if got.APIFamily != test.family ||
				got.EndpointKind != test.endpoint ||
				got.Operation != test.operation ||
				got.ModelSource != test.modelSource ||
				got.PathModel != test.pathModel ||
				got.Streaming != test.streaming {
				t.Fatalf("contract = %#v", got)
			}
		})
	}
}

func TestResolveRejectsUnsupportedNativePaths(t *testing.T) {
	tests := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/anthropic/v1/messages"},
		{
			method: http.MethodPost,
			path:   "/gemini/v1beta/models/gemini:generateContent",
		},
		{method: http.MethodPost, path: "/v1beta/models/:generateContent"},
		{method: http.MethodGet, path: "/v1beta/models/gemini:generateContent"},
		{method: http.MethodPost, path: "/ollama/api/chat"},
	}
	for _, test := range tests {
		if got, ok := Resolve(test.method, test.path); ok {
			t.Fatalf(
				"Resolve(%s, %s) = %#v, want no match",
				test.method,
				test.path,
				got,
			)
		}
	}
}

func TestExtractCredentialNormalizesFamilyCarriers(t *testing.T) {
	tests := []struct {
		name    string
		family  domain.APIFamily
		header  http.Header
		carrier Carrier
	}{
		{
			name:   "openai bearer",
			family: domain.APIFamilyOpenAICompatible,
			header: http.Header{
				"Authorization": {"Bearer sk_openai"},
			},
			carrier: CarrierAuthorizationBearer,
		},
		{
			name:   "anthropic x api key",
			family: domain.APIFamilyAnthropicNative,
			header: http.Header{
				"x-api-key": {"sk_anthropic"},
			},
			carrier: CarrierXAPIKey,
		},
		{
			name:   "gemini x goog api key",
			family: domain.APIFamilyGeminiNative,
			header: http.Header{
				"x-goog-api-key": {"sk_gemini"},
			},
			carrier: CarrierXGoogAPIKey,
		},
		{
			name:   "ollama bearer",
			family: domain.APIFamilyOllamaNative,
			header: http.Header{
				"Authorization": {"Bearer sk_ollama"},
			},
			carrier: CarrierAuthorizationBearer,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, failure := ExtractCredential(
				test.family,
				test.header,
				url.Values{},
			)
			if failure != nil {
				t.Fatalf("failure = %#v", failure)
			}
			if got.RawAPIKey == "" ||
				got.RawAPIKey[:3] != "sk_" ||
				got.Carrier != test.carrier {
				t.Fatalf("credential = %#v", got)
			}
		})
	}
}

func TestExtractCredentialRejectsConflictingOrUnsafeCarriers(t *testing.T) {
	tests := []struct {
		name   string
		family domain.APIFamily
		header http.Header
		query  url.Values
	}{
		{
			name:   "anthropic rejects bearer",
			family: domain.APIFamilyAnthropicNative,
			header: http.Header{"Authorization": {"Bearer sk_wrong"}},
		},
		{
			name:   "gemini rejects query key",
			family: domain.APIFamilyGeminiNative,
			header: http.Header{"x-goog-api-key": {"sk_ok"}},
			query:  url.Values{"key": {"sk_query"}},
		},
		{
			name:   "openai rejects query api_key",
			family: domain.APIFamilyOpenAICompatible,
			header: http.Header{"Authorization": {"Bearer sk_ok"}},
			query:  url.Values{"api_key": {"sk_query"}},
		},
		{
			name:   "anthropic rejects query x api key",
			family: domain.APIFamilyAnthropicNative,
			header: http.Header{"x-api-key": {"sk_ok"}},
			query:  url.Values{"x-api-key": {"sk_query"}},
		},
		{
			name:   "ollama rejects query access token",
			family: domain.APIFamilyOllamaNative,
			header: http.Header{"Authorization": {"Bearer sk_ok"}},
			query:  url.Values{"access_token": {"sk_query"}},
		},
		{
			name:   "openai rejects x api key conflict",
			family: domain.APIFamilyOpenAICompatible,
			header: http.Header{
				"Authorization": {"Bearer sk_ok"},
				"x-api-key":     {"sk_conflict"},
			},
		},
		{
			name:   "gemini rejects wrong prefix",
			family: domain.APIFamilyGeminiNative,
			header: http.Header{"x-goog-api-key": {"token"}},
		},
		{
			name:   "ollama rejects malformed bearer",
			family: domain.APIFamilyOllamaNative,
			header: http.Header{"Authorization": {"Bearer  sk_bad"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, failure := ExtractCredential(
				test.family,
				test.header,
				test.query,
			); failure == nil {
				t.Fatalf("credential = %#v, want failure", got)
			}
		})
	}
}
