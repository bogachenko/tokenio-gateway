//go:build integration

package integration_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	fa "github.com/bogachenko/tokenio-gateway/integration/fakes/anthropic"
)

func TestAnthropicMessagesScenario(t *testing.T) {
	t.Parallel()

	server := fa.New()
	defer server.Close()

	requestBody := `{"model":"claude-test","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`
	response, err := http.Post(
		server.URL()+"/v1/messages",
		"application/json",
		strings.NewReader(requestBody),
	)
	if err != nil {
		t.Fatalf("post messages: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, string(body))
	}
	for _, want := range []string{`"type"`, `"message"`, `"content"`, `"usage"`, `"input_tokens"`, `"output_tokens"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("body=%s does not contain %s", string(body), want)
		}
	}

	requests := server.Requests()
	if len(requests) != 1 {
		t.Fatalf("requests=%d", len(requests))
	}
	if requests[0].Method != http.MethodPost || requests[0].Path != "/v1/messages" {
		t.Fatalf("request=%s %s", requests[0].Method, requests[0].Path)
	}
	if string(requests[0].Body) != requestBody {
		t.Fatalf("request body=%s", string(requests[0].Body))
	}
}

func TestAnthropicMessagesRepositoryEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := scenarioRepoRoot(t)
	files := scenarioRepositoryTextFiles(t, repoRoot, "integration/anthropic_messages_test.go")

	assertScenarioEvidence(t, files, "Anthropic messages public route", []string{
		"/v1/messages",
	})
	assertScenarioEvidence(t, files, "Anthropic messages forwarding", []string{
		"messages",
		"Anthropic",
		"api_family",
	})
	assertScenarioEvidence(t, files, "Anthropic usage handling", []string{
		"input_tokens",
		"output_tokens",
		"usage",
	})
	assertScenarioEvidence(t, files, "Anthropic response passthrough", []string{
		"passthrough",
		"response passthrough",
		"body preservation",
	})
}
