package gemininative

import (
	"errors"
	"testing"
)

func TestExtractUsageReadsGeminiUsageMetadata(t *testing.T) {
	usage, err := ExtractUsage([]byte(`{
		"candidates":[{"content":{"parts":[{"text":"ok"}]}}],
		"usageMetadata":{
			"promptTokenCount":17,
			"candidatesTokenCount":5,
			"totalTokenCount":22
		}
	}`))
	if err != nil {
		t.Fatalf("ExtractUsage: %v", err)
	}
	if usage.InputTokens != 17 || usage.OutputTokens != 5 {
		t.Fatalf("usage=%+v", usage)
	}
}

func TestExtractUsageAllowsInputOnlyUsageMetadata(t *testing.T) {
	usage, err := ExtractUsage([]byte(`{
		"embedding":{"values":[1,2,3]},
		"usageMetadata":{"promptTokenCount":9,"totalTokenCount":9}
	}`))
	if err != nil {
		t.Fatalf("ExtractUsage: %v", err)
	}
	if usage.InputTokens != 9 || usage.OutputTokens != 0 {
		t.Fatalf("usage=%+v", usage)
	}
}

func TestExtractUsageReportsMissingUsage(t *testing.T) {
	_, err := ExtractUsage([]byte(`{"candidates":[]}`))
	if !errors.Is(err, ErrUsageNotFound) {
		t.Fatalf("error=%v, want usage not found", err)
	}
}

func TestExtractUsageRejectsInvalidUsageMetadata(t *testing.T) {
	tests := []string{
		`not json`,
		`{"usageMetadata":null}`,
		`{"usageMetadata":{}}`,
		`{"usageMetadata":{"promptTokenCount":"17"}}`,
		`{"usageMetadata":{"promptTokenCount":17.5}}`,
		`{"usageMetadata":{"promptTokenCount":-1}}`,
		`{"usageMetadata":{"promptTokenCount":17,"candidatesTokenCount":"5"}}`,
		`{"usageMetadata":{"promptTokenCount":17,"totalTokenCount":16}}`,
	}

	for _, body := range tests {
		_, err := ExtractUsage([]byte(body))
		if !errors.Is(err, ErrInvalidUsage) {
			t.Fatalf("body=%s error=%v, want invalid usage", body, err)
		}
	}
}
