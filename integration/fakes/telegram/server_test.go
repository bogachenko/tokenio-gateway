//go:build integration

package faketelegram

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFakeTelegramSendMessageDefault(t *testing.T) {
	server := New()
	defer server.Close()

	response, err := http.Post(
		server.BotAPIURL("TEST_TOKEN")+"/sendMessage",
		"application/json",
		strings.NewReader(`{"chat_id":12345,"text":"hello"}`),
	)
	if err != nil {
		t.Fatalf("post sendMessage: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, string(body))
	}
	if !strings.Contains(string(body), `"message_id":1`) {
		t.Fatalf("body=%s", string(body))
	}

	requests := server.Requests()
	if len(requests) != 1 {
		t.Fatalf("requests=%d", len(requests))
	}
	if requests[0].Method != http.MethodPost ||
		requests[0].Path != "/botTEST_TOKEN/sendMessage" {
		t.Fatalf("request=%s %s", requests[0].Method, requests[0].Path)
	}
}

func TestFakeTelegramProgrammableFailure(t *testing.T) {
	server := New()
	defer server.Close()

	server.SetResponse(http.MethodPost, "/botTEST_TOKEN/sendMessage", Response{
		Status: http.StatusTooManyRequests,
		Header: jsonHeader(),
		Body:   []byte(`{"ok":false,"error_code":429,"description":"Too Many Requests"}`),
	})

	response, err := http.Post(
		server.BotAPIURL("TEST_TOKEN")+"/sendMessage",
		"application/json",
		strings.NewReader(`{"chat_id":12345,"text":"hello"}`),
	)
	if err != nil {
		t.Fatalf("post sendMessage: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status=%d", response.StatusCode)
	}
}
