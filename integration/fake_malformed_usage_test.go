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

func TestFakeServicesMalformedUsageScenario(t *testing.T) {
	t.Parallel()

	t.Run("openai compatible chat", func(t *testing.T) {
		server := foc.New()
		defer server.Close()

		server.SetResponse(http.MethodPost, "/v1/chat/completions", foc.Response{
			Status: http.StatusOK,
			Header: malformedUsageJSONHeader(),
			Body:   []byte(`{"id":"chatcmpl_test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":"bad","completion_tokens":1,"total_tokens":2}}`),
		})
		response, err := http.Post(server.URL()+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"gpt-test"}`))
		assertMalformedUsageResponse(t, response, err, "prompt_tokens", `"bad"`)
	})

	t.Run("openai compatible embeddings", func(t *testing.T) {
		server := foc.New()
		defer server.Close()

		server.SetResponse(http.MethodPost, "/v1/embeddings", foc.Response{
			Status: http.StatusOK,
			Header: malformedUsageJSONHeader(),
			Body:   []byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":{},"total_tokens":1}}`),
		})
		response, err := http.Post(server.URL()+"/v1/embeddings", "application/json", strings.NewReader(`{"model":"embed-test","input":"hi"}`))
		assertMalformedUsageResponse(t, response, err, "prompt_tokens", "{}")
	})

	t.Run("anthropic messages", func(t *testing.T) {
		server := fa.New()
		defer server.Close()

		server.SetResponse(http.MethodPost, "/v1/messages", fa.Response{
			Status: http.StatusOK,
			Header: malformedUsageJSONHeader(),
			Body:   []byte(`{"id":"msg_test","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":"bad","output_tokens":1}}`),
		})
		response, err := http.Post(server.URL()+"/v1/messages", "application/json", strings.NewReader(`{"model":"claude-test"}`))
		assertMalformedUsageResponse(t, response, err, "input_tokens", `"bad"`)
	})

	t.Run("gemini generate content", func(t *testing.T) {
		server := fg.New()
		defer server.Close()

		server.SetResponse(http.MethodPost, "/v1beta/models/gemini-test:generateContent", fg.Response{
			Status: http.StatusOK,
			Header: malformedUsageJSONHeader(),
			Body:   []byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":"bad","candidatesTokenCount":1,"totalTokenCount":2}}`),
		})
		response, err := http.Post(server.URL()+"/v1beta/models/gemini-test:generateContent", "application/json", strings.NewReader(`{"contents":[]}`))
		assertMalformedUsageResponse(t, response, err, "promptTokenCount", `"bad"`)
	})

	t.Run("ollama chat", func(t *testing.T) {
		server := fo.New()
		defer server.Close()

		server.SetResponse(http.MethodPost, "/api/chat", fo.Response{
			Status: http.StatusOK,
			Header: malformedUsageJSONHeader(),
			Body:   []byte(`{"model":"ollama-test","created_at":"2026-01-01T00:00:00Z","message":{"role":"assistant","content":"ok"},"done":true,"prompt_eval_count":"bad","eval_count":1}`),
		})
		response, err := http.Post(server.URL()+"/api/chat", "application/json", strings.NewReader(`{"model":"ollama-test"}`))
		assertMalformedUsageResponse(t, response, err, "prompt_eval_count", `"bad"`)
	})
}

func malformedUsageJSONHeader() http.Header {
	return http.Header{
		"Content-Type": []string{"application/json"},
	}
}

func assertMalformedUsageResponse(t *testing.T, response *http.Response, err error, field string, malformedValue string) {
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
	if !strings.Contains(string(body), field) {
		t.Fatalf("body=%s does not contain field %q", string(body), field)
	}
	if !strings.Contains(string(body), malformedValue) {
		t.Fatalf("body=%s does not contain malformed value %q", string(body), malformedValue)
	}
}
