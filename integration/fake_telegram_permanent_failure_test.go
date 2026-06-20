//go:build integration

package integration_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	ft "github.com/bogachenko/tokenio-gateway/integration/fakes/telegram"
)

func TestFakeTelegramPermanentFailureScenario(t *testing.T) {
	t.Parallel()

	server := ft.New()
	defer server.Close()

	server.SetResponse(http.MethodPost, "/botTEST_TOKEN/sendMessage", ft.Response{
		Status: http.StatusForbidden,
		Header: telegramPermanentFailureJSONHeader(),
		Body: []byte(`{
			"ok": false,
			"error_code": 403,
			"description": "Forbidden: bot was blocked by the user"
		}`),
	})

	requestBody := `{"chat_id":123,"text":"hello"}`
	response, err := http.Post(
		server.BotAPIURL("TEST_TOKEN")+"/sendMessage",
		"application/json",
		strings.NewReader(requestBody),
	)
	if err != nil {
		t.Fatalf("post telegram permanent failure: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.StatusCode, string(body))
	}
	compact := compactTelegramPermanentFailureJSON(string(body))
	for _, want := range []string{`"ok":false`, `"error_code":403`, `"description":"Forbidden:botwasblockedbytheuser"`} {
		if !strings.Contains(compact, want) {
			t.Fatalf("body=%s does not contain %s", string(body), want)
		}
	}
	if strings.Contains(compact, "retry_after") {
		t.Fatalf("body=%s unexpectedly contains retry_after", string(body))
	}

	requests := server.Requests()
	if len(requests) != 1 {
		t.Fatalf("requests=%d", len(requests))
	}
	if requests[0].Method != http.MethodPost || requests[0].Path != "/botTEST_TOKEN/sendMessage" {
		t.Fatalf("request=%s %s", requests[0].Method, requests[0].Path)
	}
	if string(requests[0].Body) != requestBody {
		t.Fatalf("request body=%s", string(requests[0].Body))
	}
}

func telegramPermanentFailureJSONHeader() http.Header {
	return http.Header{
		"Content-Type": []string{"application/json"},
	}
}

func compactTelegramPermanentFailureJSON(value string) string {
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, "\n", "")
	value = strings.ReplaceAll(value, "\t", "")
	return value
}
