//go:build integration

package integration_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	fo "github.com/bogachenko/tokenio-gateway/integration/fakes/ollama"
)

func TestOllamaChatGenerateEmbeddingsTagsScenario(t *testing.T) {
	t.Parallel()

	server := fo.New()
	defer server.Close()

	assertOllamaRequest(t, server, http.MethodGet, "/api/tags", "", []string{
		`"models"`,
		`"ollama-test"`,
	})

	chatBody := `{"model":"ollama-test","messages":[{"role":"user","content":"hello"}],"stream":false}`
	assertOllamaRequest(t, server, http.MethodPost, "/api/chat", chatBody, []string{
		`"message"`,
		`"prompt_eval_count"`,
		`"eval_count"`,
	})

	generateBody := `{"model":"ollama-test","prompt":"hello","stream":false}`
	assertOllamaRequest(t, server, http.MethodPost, "/api/generate", generateBody, []string{
		`"response"`,
		`"prompt_eval_count"`,
		`"eval_count"`,
	})

	embeddingsBody := `{"model":"ollama-test","prompt":"hello"}`
	assertOllamaRequest(t, server, http.MethodPost, "/api/embeddings", embeddingsBody, []string{
		`"embedding"`,
	})

	requests := server.Requests()
	if len(requests) != 4 {
		t.Fatalf("requests=%d", len(requests))
	}
	assertOllamaRecordedRequest(t, requests[0], http.MethodGet, "/api/tags", "")
	assertOllamaRecordedRequest(t, requests[1], http.MethodPost, "/api/chat", chatBody)
	assertOllamaRecordedRequest(t, requests[2], http.MethodPost, "/api/generate", generateBody)
	assertOllamaRecordedRequest(t, requests[3], http.MethodPost, "/api/embeddings", embeddingsBody)
}

func TestOllamaChatGenerateEmbeddingsTagsRepositoryEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := scenarioRepoRoot(t)
	files := scenarioRepositoryTextFiles(t, repoRoot, "integration/ollama_chat_generate_embeddings_tags_test.go")

	assertScenarioEvidence(t, files, "Ollama tags public route", []string{
		"/api/tags",
	})
	assertScenarioEvidence(t, files, "Ollama chat route", []string{
		"/api/chat",
	})
	assertScenarioEvidence(t, files, "Ollama generate route", []string{
		"/api/generate",
	})
	assertScenarioEvidence(t, files, "Ollama embeddings route", []string{
		"/api/embeddings",
	})
	assertScenarioEvidence(t, files, "Ollama usage handling", []string{
		"prompt_eval_count",
		"eval_count",
	})
	assertScenarioEvidence(t, files, "Ollama response passthrough", []string{
		"passthrough",
		"response passthrough",
		"body preservation",
	})
}

func assertOllamaRequest(t *testing.T, server *fo.Server, method string, path string, body string, wants []string) {
	t.Helper()

	request, err := http.NewRequest(method, server.URL()+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, string(responseBody))
	}
	for _, want := range wants {
		if !strings.Contains(string(responseBody), want) {
			t.Fatalf("body=%s does not contain %s", string(responseBody), want)
		}
	}
}

func assertOllamaRecordedRequest(t *testing.T, request fo.Request, method string, path string, body string) {
	t.Helper()

	if request.Method != method || request.Path != path {
		t.Fatalf("request=%s %s want=%s %s", request.Method, request.Path, method, path)
	}
	if string(request.Body) != body {
		t.Fatalf("request body=%s want=%s", string(request.Body), body)
	}
}
