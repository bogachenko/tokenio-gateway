//go:build integration

package fakegemini

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFakeGeminiUpstreamDefaults(t *testing.T) {
	server := New()
	defer server.Close()

	response, err := http.Post(
		server.URL()+"/v1beta/models/gemini-test:generateContent",
		"application/json",
		strings.NewReader(`{"contents":[{"parts":[{"text":"hi"}]}]}`),
	)
	if err != nil {
		t.Fatalf("post generateContent: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, string(body))
	}
	if !strings.Contains(string(body), `"usageMetadata"`) {
		t.Fatalf("body=%s", string(body))
	}

	requests := server.Requests()
	if len(requests) != 1 {
		t.Fatalf("requests=%d", len(requests))
	}
	if requests[0].Method != http.MethodPost ||
		requests[0].Path != "/v1beta/models/gemini-test:generateContent" {
		t.Fatalf("request=%s %s", requests[0].Method, requests[0].Path)
	}
}

func TestFakeGeminiUpstreamModelsAndProgrammableResponse(t *testing.T) {
	server := New()
	defer server.Close()

	server.SetResponse(http.MethodGet, "/v1beta/models", Response{
		Status: http.StatusUnauthorized,
		Header: jsonHeader(),
		Body:   []byte(`{"error":{"message":"unauthorized"}}`),
	})

	response, err := http.Get(server.URL() + "/v1beta/models")
	if err != nil {
		t.Fatalf("get models: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.StatusCode)
	}
}
