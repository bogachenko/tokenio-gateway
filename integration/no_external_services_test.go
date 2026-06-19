//go:build integration

package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegrationTestsDoNotReferenceExternalServices(t *testing.T) {
	t.Parallel()

	forbidden := []string{
		"api." + "openai.com",
		"api." + "anthropic.com",
		"generativelanguage." + "googleapis.com",
		"api." + "telegram.org",
		"localhost:" + "11434",
		"ollama." + "com",
		"billing" + ".",
	}

	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") &&
			!strings.HasSuffix(path, ".md") &&
			!strings.HasSuffix(path, ".sh") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := strings.ToLower(string(content))
		for _, value := range forbidden {
			if strings.Contains(text, strings.ToLower(value)) {
				t.Fatalf("%s references forbidden external service marker %q", path, value)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk integration files: %v", err)
	}
}
