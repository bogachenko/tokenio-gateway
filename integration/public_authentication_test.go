//go:build integration

package integration_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicAuthenticationEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := publicAuthenticationRepoRoot(t)
	files := readRepositoryTextFiles(t, repoRoot)

	assertAnyFileContains(t, files, "public OpenAI-compatible routes", []string{
		"/v1/models",
		"/v1/chat/completions",
		"/v1/embeddings",
		"/v1/images/generations",
	})
	assertAnyFileContains(t, files, "public authorization carrier", []string{
		"Authorization",
		"Bearer",
	})
	assertAnyFileContains(t, files, "public unauthenticated rejection", []string{
		"StatusUnauthorized",
		"401",
		"unauthorized",
	})
	assertAnyFileContains(t, files, "public API key rejection/validation evidence", []string{
		"api key",
		"api_key",
		"API key",
		"Bearer",
	})
}

func publicAuthenticationRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, ".."))
}

func readRepositoryTextFiles(t *testing.T, repoRoot string) map[string]string {
	t.Helper()

	files := make(map[string]string)
	err := filepath.WalkDir(repoRoot, func(path string, entry fs.DirEntry, err error) error {
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

		if filepath.Ext(path) != ".go" && filepath.Ext(path) != ".md" {
			return nil
		}

		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(rel, "integration/fakes/") {
			return nil
		}
		if rel == "integration/public_authentication_test.go" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(content)
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository: %v", err)
	}
	return files
}

func assertAnyFileContains(t *testing.T, files map[string]string, label string, needles []string) {
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
