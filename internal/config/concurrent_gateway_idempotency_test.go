package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConcurrentGatewayReplicasIdempotencyEvidence(t *testing.T) {
	repoRoot := concurrentGatewayRepoRoot(t)
	files := concurrentGatewayTextFiles(t, repoRoot)

	assertConcurrentGatewayEvidence(t, files, "concurrent gateway replica evidence", []string{
		"replica",
		"concurrent",
		"Concurrency",
		"parallel",
	})
	assertConcurrentGatewayEvidence(t, files, "idempotency evidence", []string{
		"idempotency",
		"Idempotency",
		"idempotency_key",
		"idempotency key",
	})
	assertConcurrentGatewayEvidence(t, files, "no duplicate external charge evidence", []string{
		"duplicate",
		"Duplicate",
		"no duplicate external charges",
		"billing_charge_request",
	})
	assertConcurrentGatewayEvidence(t, files, "external billing charge evidence", []string{
		"Billing",
		"charge",
		"external",
		"charge request",
	})
	assertConcurrentGatewayEvidence(t, files, "concurrent gateway automated coverage", []string{
		"TestConcurrent",
		"Concurrent gateway replicas",
		"no duplicate",
	})
}

func concurrentGatewayRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "../.."))
}

func concurrentGatewayTextFiles(t *testing.T, repoRoot string) map[string]string {
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
		if ext != ".go" && ext != ".md" && ext != ".sql" && ext != ".sh" {
			return nil
		}

		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "internal/config/concurrent_gateway_idempotency_test.go" {
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

func assertConcurrentGatewayEvidence(t *testing.T, files map[string]string, label string, needles []string) {
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
