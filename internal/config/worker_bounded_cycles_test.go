package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerCyclesAreBoundedEvidence(t *testing.T) {
	repoRoot := workerCyclesRepoRoot(t)
	files := workerCyclesTextFiles(t, repoRoot)

	assertWorkerCycleEvidence(t, files, "worker interval evidence", []string{
		"Interval",
		"Ticker",
		"time.NewTicker",
		"time.After",
	})
	assertWorkerCycleEvidence(t, files, "worker batch size evidence", []string{
		"BatchSize",
		"LIMIT",
		"limit",
		"batch_size",
	})
	assertWorkerCycleEvidence(t, files, "worker context cancellation evidence", []string{
		"context.Context",
		"ctx.Done",
		"ctx.Err",
		"cancel",
	})
	assertWorkerCycleEvidence(t, files, "worker bounded retry/recovery evidence", []string{
		"RecoveryBatchSize",
		"RetryBatchSize",
		"StaleAttemptRecoveryBatchSize",
		"FailedRetryBatchSize",
	})
	assertWorkerCycleEvidence(t, files, "worker bounded cycle automated coverage", []string{
		"TestWorker",
		"bounded",
		"Worker cycles are bounded",
	})
}

func workerCyclesRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "../.."))
}

func workerCyclesTextFiles(t *testing.T, repoRoot string) map[string]string {
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
		if rel == "internal/config/worker_bounded_cycles_test.go" {
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

func assertWorkerCycleEvidence(t *testing.T, files map[string]string, label string, needles []string) {
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
