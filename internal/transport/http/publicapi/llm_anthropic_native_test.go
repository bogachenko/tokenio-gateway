package publicapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestLLMRouterDispatchesAnthropicMessagesAsNativeChat(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"max_tokens":32}`)
	requests := &testLLMRequests{
		result: successfulLLMResult(
			"llmreq_anthropic_messages_1",
			domain.EndpointChat,
			[]byte(`{"id":"msg_1"}`),
		),
	}
	router, err := NewLLMRouter(
		requests,
		&testRequestIDs{local: "llmreq_anthropic_messages_1"},
		1024,
	)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		anthropicMessagesPath,
		strings.NewReader(string(body)),
	)
	request.Header.Set("x-api-key", "sk_live_test")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	if requests.calls != 1 ||
		requests.input.RawAPIKey != "sk_live_test" ||
		requests.input.APIFamily != domain.APIFamilyAnthropicNative ||
		requests.input.EndpointKind != domain.EndpointChat ||
		requests.input.PathModel != "" ||
		string(requests.input.Payload) != string(body) {
		t.Fatalf("calls = %d input = %+v", requests.calls, requests.input)
	}
}
