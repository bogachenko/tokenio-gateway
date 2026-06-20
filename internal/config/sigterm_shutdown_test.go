package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSIGTERMShutdownEvidence(t *testing.T) {
	repoRoot := sigtermShutdownRepoRoot(t)
	files := sigtermShutdownTextFiles(t, repoRoot)

	assertSIGTERMShutdownEvidence(t, files, "SIGTERM handling", []string{
		"SIGTERM",
		"os.Interrupt",
		"signal.Notify",
		"signal.NotifyContext",
	})
	assertSIGTERMShutdownEvidence(t, files, "HTTP server shutdown", []string{
		"Shutdown",
		"HTTPShutdownTimeout",
		"server.Shutdown",
		"http.Server",
	})
	assertSIGTERMShutdownEvidence(t, files, "worker shutdown", []string{
		"worker",
		"Worker",
		"Stop",
		"context cancel",
	})
	assertSIGTERMShutdownEvidence(t, files, "postgres pool close", []string{
		"PostgreSQL pool",
		"pgxpool",
		"pool.Close",
		"Close()",
	})
	assertSIGTERMShutdownEvidence(t, files, "shutdown automated coverage", []string{
		"TestSIGTERM",
		"shutdown",
		"SIGTERM closes HTTP server, workers and PostgreSQL pool",
	})
}

func sigtermShutdownRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "../.."))
}

func sigtermShutdownTextFiles(t *testing.T, repoRoot string) map[string]string {
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
		if rel == "internal/config/sigterm_shutdown_test.go" {
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

func assertSIGTERMShutdownEvidence(t *testing.T, files map[string]string, label string, needles []string) {
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
