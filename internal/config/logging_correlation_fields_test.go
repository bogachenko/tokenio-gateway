package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoggingCorrelationFieldsEvidence(t *testing.T) {
	repoRoot := loggingCorrelationRepoRoot(t)
	files := loggingCorrelationTextFiles(t, repoRoot)

	expected := []string{
		"local_request_id",
		"user_id",
		"api_key_id",
		"route_id",
		"reseller_id",
		"forwarding_attempt_id",
		"billing_batch_id",
	}
	for _, field := range expected {
		t.Run(field, func(t *testing.T) {
			for path, content := range files {
				if strings.Contains(content, field) {
					t.Logf("correlation field evidence: %s contains %q", path, field)
					return
				}
			}
			t.Fatalf("missing logging/audit correlation field evidence for %q", field)
		})
	}
}

func loggingCorrelationRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "../.."))
}

func loggingCorrelationTextFiles(t *testing.T, repoRoot string) map[string]string {
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
		if ext != ".go" && ext != ".md" && ext != ".sql" {
			return nil
		}

		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "internal/config/logging_correlation_fields_test.go" {
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
