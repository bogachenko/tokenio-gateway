//go:build integration

package integration_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	fa "github.com/bogachenko/tokenio-gateway/integration/fakes/anthropic"
	fg "github.com/bogachenko/tokenio-gateway/integration/fakes/gemini"
	fo "github.com/bogachenko/tokenio-gateway/integration/fakes/ollama"
	foc "github.com/bogachenko/tokenio-gateway/integration/fakes/openaicompat"
)

func TestFakeServicesMissingUsageScenario(t *testing.T) {
	t.Parallel()

	t.Run("openai compatible chat", func(t *testing.T) {
		server := foc.New()
		defer server.Close()

		server.SetResponse(http.MethodPost, "/v1/chat/completions", foc.Response{
			Status: http.StatusOK,
			Header: missingUsageJSONHeader(),
			Body:   []byte(`{"id":"chatcmpl_test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`),
		})
		response, err := http.Post(server.URL()+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"gpt-test"}`))
		assertMissingUsageResponse(t, response, err, "chatcmpl_test", "usage")
	})

	t.Run("openai compatible embeddings", func(t *testing.T) {
		server := foc.New()
		defer server.Close()

		server.SetResponse(http.MethodPost, "/v1/embeddings", foc.Response{
			Status: http.StatusOK,
			Header: missingUsageJSONHeader(),
			Body:   []byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}]}`),
		})
		response, err := http.Post(server.URL()+"/v1/embeddings", "application/json", strings.NewReader(`{"model":"embed-test","input":"hi"}`))
		assertMissingUsageResponse(t, response, err, "embedding", "usage")
	})

	t.Run("anthropic messages", func(t *testing.T) {
		server := fa.New()
		defer server.Close()

		server.SetResponse(http.MethodPost, "/v1/messages", fa.Response{
			Status: http.StatusOK,
			Header: missingUsageJSONHeader(),
			Body:   []byte(`{"id":"msg_test","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`),
		})
		response, err := http.Post(server.URL()+"/v1/messages", "application/json", strings.NewReader(`{"model":"claude-test"}`))
		assertMissingUsageResponse(t, response, err, "msg_test", "usage")
	})

	t.Run("gemini generate content", func(t *testing.T) {
		server := fg.New()
		defer server.Close()

		server.SetResponse(http.MethodPost, "/v1beta/models/gemini-test:generateContent", fg.Response{
			Status: http.StatusOK,
			Header: missingUsageJSONHeader(),
			Body:   []byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}],"role":"model"},"finishReason":"STOP"}]}`),
		})
		response, err := http.Post(server.URL()+"/v1beta/models/gemini-test:generateContent", "application/json", strings.NewReader(`{"contents":[]}`))
		assertMissingUsageResponse(t, response, err, "candidates", "usageMetadata")
	})

	t.Run("ollama chat", func(t *testing.T) {
		server := fo.New()
		defer server.Close()

		server.SetResponse(http.MethodPost, "/api/chat", fo.Response{
			Status: http.StatusOK,
			Header: missingUsageJSONHeader(),
			Body:   []byte(`{"model":"ollama-test","created_at":"2026-01-01T00:00:00Z","message":{"role":"assistant","content":"ok"},"done":true}`),
		})
		response, err := http.Post(server.URL()+"/api/chat", "application/json", strings.NewReader(`{"model":"ollama-test"}`))
		assertMissingUsageResponse(t, response, err, "ollama-test", "eval_count")
	})
}

func missingUsageJSONHeader() http.Header {
	return http.Header{
		"Content-Type": []string{"application/json"},
	}
}

func assertMissingUsageResponse(t *testing.T, response *http.Response, err error, present string, absent string) {
	t.Helper()

	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, string(body))
	}
	if !strings.Contains(string(body), present) {
		t.Fatalf("body=%s does not contain %q", string(body), present)
	}
	if strings.Contains(string(body), absent) {
		t.Fatalf("body=%s unexpectedly contains %q", string(body), absent)
	}
}
