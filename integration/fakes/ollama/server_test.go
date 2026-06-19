//go:build integration

package fakeollama

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFakeOllamaUpstreamDefaults(t *testing.T) {
	server := New()
	defer server.Close()

	response, err := http.Post(
		server.URL()+"/api/chat",
		"application/json",
		strings.NewReader(`{"model":"ollama-test","messages":[{"role":"user","content":"hi"}]}`),
	)
	if err != nil {
		t.Fatalf("post chat: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, string(body))
	}
	if !strings.Contains(string(body), `"prompt_eval_count"`) {
		t.Fatalf("body=%s", string(body))
	}

	requests := server.Requests()
	if len(requests) != 1 {
		t.Fatalf("requests=%d", len(requests))
	}
	if requests[0].Method != http.MethodPost || requests[0].Path != "/api/chat" {
		t.Fatalf("request=%s %s", requests[0].Method, requests[0].Path)
	}
}

func TestFakeOllamaUpstreamTagsAndProgrammableResponse(t *testing.T) {
	server := New()
	defer server.Close()

	server.SetResponse(http.MethodGet, "/api/tags", Response{
		Status: http.StatusInternalServerError,
		Header: jsonHeader(),
		Body:   []byte(`{"error":"broken"}`),
	})

	response, err := http.Get(server.URL() + "/api/tags")
	if err != nil {
		t.Fatalf("get tags: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d", response.StatusCode)
	}
}
