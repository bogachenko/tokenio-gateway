package architecture_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAppDoesNotContainLLMRequestPipelineFiles(t *testing.T) {
	root := repositoryRoot(t)
	var violations []string

	walkProductionGoFiles(t, root, func(_ string, relative string) error {
		if strings.HasPrefix(relative, "internal/app/llmrequest_") &&
			strings.HasSuffix(filepath.Base(relative), ".go") {
			violations = append(violations, relative)
		}
		return nil
	})

	if len(violations) != 0 {
		t.Fatalf(
			"LLM request pipeline implementation files must not live in internal/app:\n%s",
			strings.Join(violations, "\n"),
		)
	}
}
