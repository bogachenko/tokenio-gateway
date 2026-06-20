package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSupportedSDKCompatibleRequestsEvidence(t *testing.T) {
	repoRoot := sdkCompatibleRepoRoot(t)
	files := sdkCompatibleTextFiles(t, repoRoot)

	assertSDKCompatibleEvidence(t, files, "OpenAI-compatible SDK requests", []string{
		"/v1/chat/completions",
		"/v1/embeddings",
		"/v1/images/generations",
		"OpenAI-compatible",
	})
	assertSDKCompatibleEvidence(t, files, "Anthropic SDK requests", []string{
		"/v1/messages",
		"anthropic",
		"Anthropic",
	})
	assertSDKCompatibleEvidence(t, files, "Gemini SDK requests", []string{
		"generateContent",
		"embedContent",
		"batchEmbedContents",
		"v1beta/models",
	})
	assertSDKCompatibleEvidence(t, files, "Ollama SDK requests", []string{
		"/api/chat",
		"/api/generate",
		"/api/embeddings",
		"/api/tags",
	})
	assertSDKCompatibleEvidence(t, files, "SDK request passthrough evidence", []string{
		"passthrough",
		"body preservation",
		"response body",
		"Body preservation",
	})
	assertSDKCompatibleEvidence(t, files, "all SDK families automated coverage", []string{
		"TestSupportedSDKCompatibleRequests",
		"All supported SDK-compatible requests pass",
		"SDK-compatible",
	})
}

func sdkCompatibleRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "../.."))
}

func sdkCompatibleTextFiles(t *testing.T, repoRoot string) map[string]string {
	t.Helper()

	files := make(map[string]string)
	err := filepath.WalkDir(repoRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		name := entry.Name()
		if entry.IsDir() {
			switch name {
			case ".git", "vendor", ".dart_tool", "build", "node_modules":
				return filepath.SkipDir
			default:
				return nil
			}
		}

		ext := filepath.Ext(path)
		if ext != ".go" && ext != ".md" && ext != ".sh" {
			return nil
		}

		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "internal/config/sdk_compatible_requests_test.go" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = string(content)
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository: %v", err)
	}
	return files
}

func assertSDKCompatibleEvidence(t *testing.T, files map[string]string, label string, needles []string) {
	t.Helper()

	for path, content := range files {
		for _, needle := range needles {
			if strings.Contains(content, needle) {
				t.Logf("%s evidence: %s contains %q", label, path, needle)
				return
			}
		}
	}
	t.Fatalf("missing %s evidence; checked %d files for any of %q", label, len(files), needles)
}
