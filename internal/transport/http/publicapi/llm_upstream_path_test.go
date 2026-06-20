package publicapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestLLMRouterPreservesNativeUpstreamPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		keyHeader string
		keyValue string
		family domain.APIFamily
		kind domain.EndpointKind
	}{
		{name: "gemini generateContent", path: "/v1beta/models/gemini-client:generateContent", keyHeader: "x-goog-api-key", keyValue: "sk_gemini", family: domain.APIFamilyGeminiNative, kind: domain.EndpointChat},
		{name: "gemini streamGenerateContent", path: "/v1beta/models/gemini-client:streamGenerateContent", keyHeader: "x-goog-api-key", keyValue: "sk_gemini", family: domain.APIFamilyGeminiNative, kind: domain.EndpointChat},
		{name: "gemini embedContent", path: "/v1beta/models/gemini-client:embedContent", keyHeader: "x-goog-api-key", keyValue: "sk_gemini", family: domain.APIFamilyGeminiNative, kind: domain.EndpointEmbeddings},
		{name: "gemini batchEmbedContents", path: "/v1beta/models/gemini-client:batchEmbedContents", keyHeader: "x-goog-api-key", keyValue: "sk_gemini", family: domain.APIFamilyGeminiNative, kind: domain.EndpointEmbeddings},
		{name: "ollama chat", path: "/api/chat", keyHeader: "Authorization", keyValue: "Bearer sk_ollama", family: domain.APIFamilyOllamaNative, kind: domain.EndpointChat},
		{name: "ollama generate", path: "/api/generate", keyHeader: "Authorization", keyValue: "Bearer sk_ollama", family: domain.APIFamilyOllamaNative, kind: domain.EndpointChat},
		{name: "ollama embeddings", path: "/api/embeddings", keyHeader: "Authorization", keyValue: "Bearer sk_ollama", family: domain.APIFamilyOllamaNative, kind: domain.EndpointEmbeddings},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := &testLLMRequests{result: successfulLLMResult("llmreq_upstream_path", test.kind, []byte(`{"ok":true}`))}
			router, err := NewLLMRouter(requests, &testRequestIDs{local: "llmreq_upstream_path"}, 1024)
			if err != nil { t.Fatal(err) }
			req := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(`{"model":"gemini-client"}`))
			req.Header.Set(test.keyHeader, test.keyValue)
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != http.StatusCreated { t.Fatalf("status=%d body=%s input=%+v", res.Code, res.Body.String(), requests.input) }
			wantKey := strings.TrimPrefix(test.keyValue, "Bearer ")
			if requests.calls != 1 || requests.input.RawAPIKey != wantKey || requests.input.APIFamily != test.family || requests.input.EndpointKind != test.kind || requests.input.UpstreamPath != test.path {
				t.Fatalf("calls=%d input=%+v", requests.calls, requests.input)
			}
			_ = llmrequest.Input{}
		})
	}
}
