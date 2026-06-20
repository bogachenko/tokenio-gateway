//go:build integration

package integration_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	foc "github.com/bogachenko/tokenio-gateway/integration/fakes/openaicompat"
)

func TestOpenAIEmbeddingsScenario(t *testing.T) {
	t.Parallel()

	server := foc.New()
	defer server.Close()

	requestBody := `{"model":"text-embedding-test","input":"hello"}`
	response, err := http.Post(
		server.URL()+"/v1/embeddings",
		"application/json",
		strings.NewReader(requestBody),
	)
	if err != nil {
		t.Fatalf("post embeddings: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, string(body))
	}
	for _, want := range []string{`"object"`, `"list"`, `"data"`, `"embedding"`, `"usage"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("body=%s does not contain %s", string(body), want)
		}
	}

	requests := server.Requests()
	if len(requests) != 1 {
		t.Fatalf("requests=%d", len(requests))
	}
	if requests[0].Method != http.MethodPost || requests[0].Path != "/v1/embeddings" {
		t.Fatalf("request=%s %s", requests[0].Method, requests[0].Path)
	}
	if string(requests[0].Body) != requestBody {
		t.Fatalf("request body=%s", string(requests[0].Body))
	}
}

func TestOpenAIEmbeddingsRepositoryEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := scenarioRepoRoot(t)
	files := scenarioRepositoryTextFiles(t, repoRoot, "integration/openai_embeddings_test.go")

	assertScenarioEvidence(t, files, "OpenAI embeddings public route", []string{
		"/v1/embeddings",
	})
	assertScenarioEvidence(t, files, "embeddings forwarding", []string{
		"embeddings",
		"Embeddings",
		"embedding",
	})
	assertScenarioEvidence(t, files, "embeddings usage handling", []string{
		"prompt_tokens",
		"total_tokens",
		"usage",
	})
	assertScenarioEvidence(t, files, "embeddings response passthrough", []string{
		"passthrough",
		"response passthrough",
		"body preservation",
	})
}
