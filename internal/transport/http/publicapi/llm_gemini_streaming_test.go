package publicapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestLLMRouterRejectsGeminiNativeStreamingBeforeApplicationCall(t *testing.T) {
	requests := &testLLMRequests{}
	router, err := NewLLMRouter(
		requests,
		&testRequestIDs{local: "llmreq_gemini_streaming"},
		1024,
	)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/v1beta/models/gemini-2.5-flash:streamGenerateContent",
		strings.NewReader(`{"contents":[{"parts":[{"text":"hello"}]}]}`),
	)
	request.Header.Set("x-goog-api-key", "sk_live_gemini")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest || requests.calls != 0 {
		t.Fatalf(
			"status=%d calls=%d body=%s",
			response.Code,
			requests.calls,
			response.Body.String(),
		)
	}
	if response.Header().Get(localRequestIDHeader) != "llmreq_gemini_streaming" ||
		!strings.Contains(response.Body.String(), `"code":"`+string(domain.ErrorCodeStreamingUnsupported)+`"`) ||
		!strings.Contains(response.Body.String(), `"request_id":"llmreq_gemini_streaming"`) {
		t.Fatalf("headers=%v body=%s", response.Header(), response.Body.String())
	}
}
