//go:build integration

package integration_test

import (
	"net/http"
	"strings"
	"testing"

	fa "github.com/bogachenko/tokenio-gateway/integration/fakes/anthropic"
	fb "github.com/bogachenko/tokenio-gateway/integration/fakes/billing"
	fg "github.com/bogachenko/tokenio-gateway/integration/fakes/gemini"
	fo "github.com/bogachenko/tokenio-gateway/integration/fakes/ollama"
	foc "github.com/bogachenko/tokenio-gateway/integration/fakes/openaicompat"
	ft "github.com/bogachenko/tokenio-gateway/integration/fakes/telegram"
)

func TestFakeServicesSuccessScenario(t *testing.T) {
	t.Parallel()

	t.Run("billing", func(t *testing.T) {
		server := fb.New()
		defer server.Close()

		response, err := http.Post(server.URL()+"/v1/charges", "application/json", strings.NewReader(`{"amount":1}`))
		assertStatus(t, response, err, http.StatusOK)
	})

	t.Run("openai compatible", func(t *testing.T) {
		server := foc.New()
		defer server.Close()

		response, err := http.Get(server.URL() + "/v1/models")
		assertStatus(t, response, err, http.StatusOK)

		response, err = http.Post(server.URL()+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"gpt-test"}`))
		assertStatus(t, response, err, http.StatusOK)

		response, err = http.Post(server.URL()+"/v1/embeddings", "application/json", strings.NewReader(`{"model":"embed-test","input":"hi"}`))
		assertStatus(t, response, err, http.StatusOK)

		response, err = http.Post(server.URL()+"/v1/images/generations", "application/json", strings.NewReader(`{"model":"image-test","prompt":"hi"}`))
		assertStatus(t, response, err, http.StatusOK)
	})

	t.Run("anthropic", func(t *testing.T) {
		server := fa.New()
		defer server.Close()

		response, err := http.Post(server.URL()+"/v1/messages", "application/json", strings.NewReader(`{"model":"claude-test"}`))
		assertStatus(t, response, err, http.StatusOK)
	})

	t.Run("gemini", func(t *testing.T) {
		server := fg.New()
		defer server.Close()

		response, err := http.Get(server.URL() + "/v1beta/models")
		assertStatus(t, response, err, http.StatusOK)

		response, err = http.Post(server.URL()+"/v1beta/models/gemini-test:generateContent", "application/json", strings.NewReader(`{"contents":[]}`))
		assertStatus(t, response, err, http.StatusOK)

		response, err = http.Post(server.URL()+"/v1beta/models/gemini-test:embedContent", "application/json", strings.NewReader(`{"content":{}}`))
		assertStatus(t, response, err, http.StatusOK)

		response, err = http.Post(server.URL()+"/v1beta/models/gemini-test:batchEmbedContents", "application/json", strings.NewReader(`{"requests":[]}`))
		assertStatus(t, response, err, http.StatusOK)
	})

	t.Run("ollama", func(t *testing.T) {
		server := fo.New()
		defer server.Close()

		response, err := http.Get(server.URL() + "/api/tags")
		assertStatus(t, response, err, http.StatusOK)

		response, err = http.Post(server.URL()+"/api/chat", "application/json", strings.NewReader(`{"model":"ollama-test"}`))
		assertStatus(t, response, err, http.StatusOK)

		response, err = http.Post(server.URL()+"/api/generate", "application/json", strings.NewReader(`{"model":"ollama-test"}`))
		assertStatus(t, response, err, http.StatusOK)

		response, err = http.Post(server.URL()+"/api/embeddings", "application/json", strings.NewReader(`{"model":"ollama-test"}`))
		assertStatus(t, response, err, http.StatusOK)
	})

	t.Run("telegram", func(t *testing.T) {
		server := ft.New()
		defer server.Close()

		response, err := http.Post(server.BotAPIURL("TEST_TOKEN")+"/sendMessage", "application/json", strings.NewReader(`{"chat_id":1,"text":"ok"}`))
		assertStatus(t, response, err, http.StatusOK)
	})
}

func assertStatus(t *testing.T, response *http.Response, err error, want int) {
	t.Helper()

	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != want {
		t.Fatalf("status=%d want=%d", response.StatusCode, want)
	}
}
