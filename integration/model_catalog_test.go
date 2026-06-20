//go:build integration

package integration_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelCatalogEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := modelCatalogRepoRoot(t)
	files := readModelCatalogRepositoryTextFiles(t, repoRoot)

	assertModelCatalogEvidence(t, files, "public model catalog route", []string{
		"/v1/models",
	})
	assertModelCatalogEvidence(t, files, "model catalog implementation", []string{
		"ModelCatalog",
		"model catalog",
		"model_catalog",
	})
	assertModelCatalogEvidence(t, files, "model capabilities", []string{
		"capabilities",
		"supports_chat",
		"supports_embeddings",
		"supports_images",
	})
	assertModelCatalogEvidence(t, files, "public pricing", []string{
		"pricing",
		"input_token",
		"output_token",
	})
	assertModelCatalogEvidence(t, files, "model catalog automated coverage", []string{
		"TestModel",
		"model catalog",
		"/v1/models",
	})
}

func modelCatalogRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, ".."))
}

func readModelCatalogRepositoryTextFiles(t *testing.T, repoRoot string) map[string]string {
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

		ext := filepath.Ext(path)
		if ext != ".go" && ext != ".md" && ext != ".sql" {
			return nil
		}

		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "integration/fakes/") {
			return nil
		}
		if rel == "integration/model_catalog_test.go" {
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

func assertModelCatalogEvidence(t *testing.T, files map[string]string, label string, needles []string) {
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
