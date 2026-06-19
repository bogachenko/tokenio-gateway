//go:build integration

package fakeopenaicompat

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFakeOpenAICompatibleUpstreamDefaults(t *testing.T) {
	server := New()
	defer server.Close()

	response, err := http.Get(server.URL() + "/v1/models")
	if err != nil {
		t.Fatalf("get models: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, string(body))
	}
	if !strings.Contains(string(body), `"gpt-test"`) {
		t.Fatalf("body=%s", string(body))
	}

	requests := server.Requests()
	if len(requests) != 1 {
		t.Fatalf("requests=%d", len(requests))
	}
	if requests[0].Method != http.MethodGet || requests[0].Path != "/v1/models" {
		t.Fatalf("request=%s %s", requests[0].Method, requests[0].Path)
	}
}

func TestFakeOpenAICompatibleUpstreamProgrammableResponse(t *testing.T) {
	server := New()
	defer server.Close()

	server.SetResponse(http.MethodPost, "/v1/chat/completions", Response{
		Status: http.StatusTooManyRequests,
		Header: jsonHeader(),
		Body:   []byte(`{"error":{"message":"rate limit"}}`),
	})

	response, err := http.Post(
		server.URL()+"/v1/chat/completions",
		"application/json",
		strings.NewReader(`{"model":"gpt-test"}`),
	)
	if err != nil {
		t.Fatalf("post chat: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status=%d", response.StatusCode)
	}
}
