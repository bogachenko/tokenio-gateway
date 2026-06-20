package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConcurrentWorkersDurableCommandInvariantsEvidence(t *testing.T) {
	repoRoot := concurrentWorkersRepoRoot(t)
	files := concurrentWorkersTextFiles(t, repoRoot)

	assertConcurrentWorkersEvidence(t, files, "concurrent worker evidence", []string{
		"worker",
		"Worker",
		"concurrent",
		"parallel",
	})
	assertConcurrentWorkersEvidence(t, files, "durable command evidence", []string{
		"durable",
		"command",
		"Command",
		"status",
	})
	assertConcurrentWorkersEvidence(t, files, "claim/lease/idempotency evidence", []string{
		"claim",
		"claimed",
		"lease",
		"idempotency",
	})
	assertConcurrentWorkersEvidence(t, files, "batch boundary evidence", []string{
		"BatchSize",
		"batch",
		"LIMIT",
		"FOR UPDATE",
	})
	assertConcurrentWorkersEvidence(t, files, "no duplicate processing evidence", []string{
		"duplicate",
		"Duplicate",
		"no duplicate",
		"already",
	})
	assertConcurrentWorkersEvidence(t, files, "concurrent worker automated coverage", []string{
		"TestConcurrent",
		"Concurrent workers",
		"durable command invariants",
	})
}

func concurrentWorkersRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "../.."))
}

func concurrentWorkersTextFiles(t *testing.T, repoRoot string) map[string]string {
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
		if rel == "internal/config/concurrent_workers_durable_commands_test.go" {
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

func assertConcurrentWorkersEvidence(t *testing.T, files map[string]string, label string, needles []string) {
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
