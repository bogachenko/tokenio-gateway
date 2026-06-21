package publicapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestLLMRouterPreservesAllowedRawQueryInUpstreamPath(t *testing.T) {
	body := []byte(`{"contents":[{"parts":[{"text":"hello"}]}]}`)
	requests := &testLLMRequests{
		result: successfulLLMResult(
			"llmreq_gemini_query",
			domain.EndpointChat,
			[]byte(`{"ok":true}`),
		),
	}
	router, err := NewLLMRouter(
		requests,
		&testRequestIDs{local: "llmreq_gemini_query"},
		1024,
	)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/v1beta/models/gemini-1.5-pro:generateContent?alt=sse",
		strings.NewReader(string(body)),
	)
	request.Header.Set("x-goog-api-key", "sk_live_gemini")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if requests.calls != 1 {
		t.Fatalf("calls=%d", requests.calls)
	}
	if requests.input.UpstreamPath != "/v1beta/models/gemini-1.5-pro:generateContent?alt=sse" ||
		requests.input.APIFamily != domain.APIFamilyGeminiNative ||
		requests.input.EndpointKind != domain.EndpointChat ||
		requests.input.PathModel != "gemini-1.5-pro" ||
		string(requests.input.Payload) != string(body) {
		t.Fatalf("input=%+v", requests.input)
	}
}

func TestLLMRouterRejectsQueryCredentialBeforeApplicationRequest(t *testing.T) {
	requests := &testLLMRequests{}
	router, err := NewLLMRouter(
		requests,
		&testRequestIDs{local: "llmreq_gemini_query_credential"},
		1024,
	)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/v1beta/models/gemini-1.5-pro:generateContent?alt=sse&key=sk_query_secret",
		strings.NewReader(`{"contents":[]}`),
	)
	request.Header.Set("x-goog-api-key", "sk_live_gemini")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized || requests.calls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, requests.calls, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "sk_") {
		t.Fatalf("error leaked credential: %s", response.Body.String())
	}
}
