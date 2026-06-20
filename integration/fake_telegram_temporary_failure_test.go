//go:build integration

package integration_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	ft "github.com/bogachenko/tokenio-gateway/integration/fakes/telegram"
)

func TestFakeTelegramTemporaryFailureScenario(t *testing.T) {
	t.Parallel()

	server := ft.New()
	defer server.Close()

	server.SetResponse(http.MethodPost, "/botTEST_TOKEN/sendMessage", ft.Response{
		Status: http.StatusTooManyRequests,
		Header: telegramTemporaryFailureJSONHeader(),
		Body: []byte(`{
			"ok": false,
			"error_code": 429,
			"description": "Too Many Requests: retry later",
			"parameters": {
				"retry_after": 3
			}
		}`),
	})

	requestBody := `{"chat_id":123,"text":"hello"}`
	response, err := http.Post(
		server.BotAPIURL("TEST_TOKEN")+"/sendMessage",
		"application/json",
		strings.NewReader(requestBody),
	)
	if err != nil {
		t.Fatalf("post telegram temporary failure: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", response.StatusCode, string(body))
	}
	compact := compactTelegramTemporaryFailureJSON(string(body))
	for _, want := range []string{`"ok":false`, `"error_code":429`, `"retry_after":3`} {
		if !strings.Contains(compact, want) {
			t.Fatalf("body=%s does not contain %s", string(body), want)
		}
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

func telegramTemporaryFailureJSONHeader() http.Header {
	return http.Header{
		"Content-Type": []string{"application/json"},
	}
}

func compactTelegramTemporaryFailureJSON(value string) string {
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, "\n", "")
	value = strings.ReplaceAll(value, "\t", "")
	return value
}
