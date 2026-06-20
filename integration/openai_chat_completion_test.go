//go:build integration

package integration_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	foc "github.com/bogachenko/tokenio-gateway/integration/fakes/openaicompat"
)

func TestOpenAIChatCompletionScenario(t *testing.T) {
	t.Parallel()

	server := foc.New()
	defer server.Close()

	requestBody := `{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`
	response, err := http.Post(
		server.URL()+"/v1/chat/completions",
		"application/json",
		strings.NewReader(requestBody),
	)
	if err != nil {
		t.Fatalf("post chat completion: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, string(body))
	}
	for _, want := range []string{`"object"`, `"chat.completion"`, `"choices"`, `"usage"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("body=%s does not contain %s", string(body), want)
		}
	}

	requests := server.Requests()
	if len(requests) != 1 {
		t.Fatalf("requests=%d", len(requests))
	}
	if requests[0].Method != http.MethodPost || requests[0].Path != "/v1/chat/completions" {
		t.Fatalf("request=%s %s", requests[0].Method, requests[0].Path)
	}
	if string(requests[0].Body) != requestBody {
		t.Fatalf("request body=%s", string(requests[0].Body))
	}
}

func TestOpenAIChatCompletionRepositoryEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := scenarioRepoRoot(t)
	files := scenarioRepositoryTextFiles(t, repoRoot, "integration/openai_chat_completion_test.go")

	assertScenarioEvidence(t, files, "OpenAI chat public route", []string{
		"/v1/chat/completions",
	})
	assertScenarioEvidence(t, files, "chat completion forwarding", []string{
		"chat.completions",
		"ChatCompletion",
		"chat completion",
	})
	assertScenarioEvidence(t, files, "chat completion usage handling", []string{
		"prompt_tokens",
		"completion_tokens",
		"total_tokens",
	})
	assertScenarioEvidence(t, files, "chat completion response passthrough", []string{
		"passthrough",
		"response passthrough",
		"body preservation",
	})
}
