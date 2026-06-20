//go:build integration

package integration_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	fg "github.com/bogachenko/tokenio-gateway/integration/fakes/gemini"
)

func TestGeminiGenerateEmbedModelsScenario(t *testing.T) {
	t.Parallel()

	server := fg.New()
	defer server.Close()

	assertGeminiRequest(t, server, http.MethodGet, "/v1beta/models", "", []string{
		`"models"`,
		`"models/gemini-test"`,
	})

	generateBody := `{"contents":[{"parts":[{"text":"hello"}]}]}`
	assertGeminiRequest(t, server, http.MethodPost, "/v1beta/models/gemini-test:generateContent", generateBody, []string{
		`"candidates"`,
		`"usageMetadata"`,
		`"totalTokenCount"`,
	})

	embedBody := `{"content":{"parts":[{"text":"hello"}]}}`
	assertGeminiRequest(t, server, http.MethodPost, "/v1beta/models/gemini-test:embedContent", embedBody, []string{
		`"embedding"`,
		`"values"`,
	})

	batchEmbedBody := `{"requests":[{"content":{"parts":[{"text":"hello"}]}}]}`
	assertGeminiRequest(t, server, http.MethodPost, "/v1beta/models/gemini-test:batchEmbedContents", batchEmbedBody, []string{
		`"embeddings"`,
		`"values"`,
	})

	requests := server.Requests()
	if len(requests) != 4 {
		t.Fatalf("requests=%d", len(requests))
	}
	assertGeminiRecordedRequest(t, requests[0], http.MethodGet, "/v1beta/models", "")
	assertGeminiRecordedRequest(t, requests[1], http.MethodPost, "/v1beta/models/gemini-test:generateContent", generateBody)
	assertGeminiRecordedRequest(t, requests[2], http.MethodPost, "/v1beta/models/gemini-test:embedContent", embedBody)
	assertGeminiRecordedRequest(t, requests[3], http.MethodPost, "/v1beta/models/gemini-test:batchEmbedContents", batchEmbedBody)
}

func TestGeminiGenerateEmbedModelsRepositoryEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := scenarioRepoRoot(t)
	files := scenarioRepositoryTextFiles(t, repoRoot, "integration/gemini_generate_embed_models_test.go")

	assertScenarioEvidence(t, files, "Gemini models public route", []string{
		"/v1beta/models",
	})
	assertScenarioEvidence(t, files, "Gemini generateContent route", []string{
		":generateContent",
		"generateContent",
	})
	assertScenarioEvidence(t, files, "Gemini embedContent routes", []string{
		":embedContent",
		":batchEmbedContents",
		"embedContent",
	})
	assertScenarioEvidence(t, files, "Gemini usage handling", []string{
		"usageMetadata",
		"promptTokenCount",
		"totalTokenCount",
	})
	assertScenarioEvidence(t, files, "Gemini response passthrough", []string{
		"passthrough",
		"response passthrough",
		"body preservation",
	})
}

func assertGeminiRequest(t *testing.T, server *fg.Server, method string, path string, body string, wants []string) {
	t.Helper()

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}

	request, err := http.NewRequest(method, server.URL()+path, reader)
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

func assertGeminiRecordedRequest(t *testing.T, request fg.Request, method string, path string, body string) {
	t.Helper()

	if request.Method != method || request.Path != path {
		t.Fatalf("request=%s %s want=%s %s", request.Method, request.Path, method, path)
	}
	if string(request.Body) != body {
		t.Fatalf("request body=%s want=%s", string(request.Body), body)
	}
}
