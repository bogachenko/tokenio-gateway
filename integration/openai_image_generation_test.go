//go:build integration

package integration_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	foc "github.com/bogachenko/tokenio-gateway/integration/fakes/openaicompat"
)

func TestOpenAIImageGenerationScenario(t *testing.T) {
	t.Parallel()

	server := foc.New()
	defer server.Close()

	requestBody := `{"model":"image-test","prompt":"a blue label","size":"1024x1024"}`
	response, err := http.Post(
		server.URL()+"/v1/images/generations",
		"application/json",
		strings.NewReader(requestBody),
	)
	if err != nil {
		t.Fatalf("post image generation: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, string(body))
	}
	for _, want := range []string{`"created"`, `"data"`, `"url"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("body=%s does not contain %s", string(body), want)
		}
	}

	requests := server.Requests()
	if len(requests) != 1 {
		t.Fatalf("requests=%d", len(requests))
	}
	if requests[0].Method != http.MethodPost || requests[0].Path != "/v1/images/generations" {
		t.Fatalf("request=%s %s", requests[0].Method, requests[0].Path)
	}
	if string(requests[0].Body) != requestBody {
		t.Fatalf("request body=%s", string(requests[0].Body))
	}
}

func TestOpenAIImageGenerationRepositoryEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := scenarioRepoRoot(t)
	files := scenarioRepositoryTextFiles(t, repoRoot, "integration/openai_image_generation_test.go")

	assertScenarioEvidence(t, files, "OpenAI image generation public route", []string{
		"/v1/images/generations",
	})
	assertScenarioEvidence(t, files, "image generation forwarding", []string{
		"images/generations",
		"ImageGeneration",
		"image generation",
	})
	assertScenarioEvidence(t, files, "image generation model extraction", []string{
		"endpoint_kind",
		"images",
		"image",
	})
	assertScenarioEvidence(t, files, "image generation response passthrough", []string{
		"passthrough",
		"response passthrough",
		"body preservation",
	})
}
