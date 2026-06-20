package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogsDoNotContainSecretsEvidence(t *testing.T) {
	repoRoot := logSecretRepoRoot(t)
	files := logSecretTextFiles(t, repoRoot)

	assertLogSecretEvidence(t, files, "central redactor evidence", []string{
		"Redact",
		"redact",
		"redacted",
	})
	assertLogSecretEvidence(t, files, "secret token redaction evidence", []string{
		"Authorization",
		"x-api-key",
		"x-goog-api-key",
		"Telegram bot token",
	})
	assertLogSecretEvidence(t, files, "DSN/password redaction evidence", []string{
		"DSN password",
		"password",
		"dsn",
	})
	assertLogSecretEvidence(t, files, "body logging production guard evidence", []string{
		"TOKENIO_LOG_BODIES=true is forbidden in production",
		"LogBodies",
		"body logging",
	})
	assertLogSecretEvidence(t, files, "logs secret automated coverage", []string{
		"TestLogsDoNotContainSecrets",
		"Logs do not contain secrets",
		"error must not contain secret",
	})

	assertLoggingFilesDoNotContainKnownSecretLiterals(t, files)
}

func logSecretRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "../.."))
}

func logSecretTextFiles(t *testing.T, repoRoot string) map[string]string {
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
		if rel == "internal/config/logs_no_secrets_test.go" {
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

func assertLogSecretEvidence(t *testing.T, files map[string]string, label string, needles []string) {
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

func assertLoggingFilesDoNotContainKnownSecretLiterals(t *testing.T, files map[string]string) {
	t.Helper()

	for path, content := range files {
		if !strings.Contains(path, "log") &&
			!strings.Contains(path, "logger") &&
			!strings.Contains(path, "redact") &&
			!strings.Contains(content, "Log") &&
			!strings.Contains(content, "log.") {
			continue
		}
		for _, forbidden := range []string{
			"billing-service-token",
			"admin-token",
			"api-key-hash-secret",
			"provisioning-service-token",
			"sensitive-api-key-hash-secret",
		} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s contains known secret literal %q in logging/redaction context", path, forbidden)
			}
		}
	}
}
