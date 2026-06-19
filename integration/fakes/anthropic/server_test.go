//go:build integration

package fakeanthropic

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFakeAnthropicUpstreamDefaults(t *testing.T) {
	server := New()
	defer server.Close()

	response, err := http.Post(
		server.URL()+"/v1/messages",
		"application/json",
		strings.NewReader(`{"model":"claude-test","messages":[{"role":"user","content":"hi"}]}`),
	)
	if err != nil {
		t.Fatalf("post messages: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, string(body))
	}
	if !strings.Contains(string(body), `"msg_test"`) {
		t.Fatalf("body=%s", string(body))
	}

	requests := server.Requests()
	if len(requests) != 1 {
		t.Fatalf("requests=%d", len(requests))
	}
	if requests[0].Method != http.MethodPost || requests[0].Path != "/v1/messages" {
		t.Fatalf("request=%s %s", requests[0].Method, requests[0].Path)
	}
}

func TestFakeAnthropicUpstreamProgrammableResponse(t *testing.T) {
	server := New()
	defer server.Close()

	server.SetResponse(http.MethodPost, "/v1/messages", Response{
		Status: http.StatusServiceUnavailable,
		Header: jsonHeader(),
		Body:   []byte(`{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`),
	})

	response, err := http.Post(
		server.URL()+"/v1/messages",
		"application/json",
		strings.NewReader(`{"model":"claude-test"}`),
	)
	if err != nil {
		t.Fatalf("post messages: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", response.StatusCode)
	}
}
