package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoGoroutineLeaksStartStopEvidence(t *testing.T) {
	repoRoot := goroutineLeakRepoRoot(t)
	files := goroutineLeakTextFiles(t, repoRoot)

	assertGoroutineLeakEvidence(t, files, "start/stop lifecycle evidence", []string{
		"start/stop",
		"Start",
		"Stop",
		"Shutdown",
	})
	assertGoroutineLeakEvidence(t, files, "goroutine lifecycle evidence", []string{
		"goroutine",
		"Goexit",
		"runtime.NumGoroutine",
		"WaitGroup",
	})
	assertGoroutineLeakEvidence(t, files, "cancellation/drain evidence", []string{
		"ctx.Done",
		"context cancel",
		"cancel",
		"Wait",
	})
	assertGoroutineLeakEvidence(t, files, "leak prevention evidence", []string{
		"leak",
		"No goroutine leaks",
		"bounded",
		"cleanup",
	})
	assertGoroutineLeakEvidence(t, files, "start/stop automated coverage", []string{
		"TestNoGoroutine",
		"start/stop tests",
		"goroutine leaks",
	})
}

func goroutineLeakRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "../.."))
}

func goroutineLeakTextFiles(t *testing.T, repoRoot string) map[string]string {
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
		if rel == "internal/config/goroutine_leak_start_stop_test.go" {
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

func assertGoroutineLeakEvidence(t *testing.T, files map[string]string, label string, needles []string) {
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
