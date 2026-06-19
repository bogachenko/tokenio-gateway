package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResellerSecretsResolveOnlyThroughConfiguredResolver(t *testing.T) {
	root := repositoryRoot(t)

	var resolverEvidence []string
	var violations []string

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel := filepath.ToSlash(mustRel(t, root, path))
		body := string(mustReadFile(t, path))
		lower := strings.ToLower(body)

		if isResellerSecretResolverEvidence(rel, lower) {
			resolverEvidence = append(resolverEvidence, rel)
		}

		if isDirectResellerSecretViolation(rel, lower) {
			violations = append(violations, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(resolverEvidence) == 0 {
		t.Fatalf(
			"expected reseller credential resolution to be wired through a configured secret resolver; no evidence found",
		)
	}
	if len(violations) > 0 {
		t.Fatalf(
			"reseller credentials appear to bypass the configured secret resolver: %s",
			strings.Join(violations, ", "),
		)
	}
}

func isResellerSecretResolverEvidence(path string, lower string) bool {
	if !strings.HasPrefix(path, "internal/app/") &&
		!strings.HasPrefix(path, "internal/infrastructure/secrets/") &&
		!strings.HasPrefix(path, "internal/ports/") {
		return false
	}
	return strings.Contains(lower, "reseller") &&
		strings.Contains(lower, "secret") &&
		strings.Contains(lower, "resolver")
}

func isDirectResellerSecretViolation(path string, lower string) bool {
	if !strings.Contains(lower, "reseller") {
		return false
	}

	// Config may define and validate non-secret reseller settings such as
	// TOKENIO_RESELLER_BALANCE_ALERT_CENTS. Secret resolver implementation and
	// ports define the approved credential boundary.
	if strings.HasPrefix(path, "internal/config/") ||
		strings.HasPrefix(path, "internal/ports/") ||
		strings.HasPrefix(path, "internal/infrastructure/secrets/") {
		return false
	}

	if isResellerSecretResolverEvidence(path, lower) {
		return false
	}

	if !hasResellerCredentialContext(lower) {
		return false
	}

	// The forbidden pattern is direct secret/env resolution in production code
	// that is not the configured resolver boundary. Domain fields, audit event
	// structs and route metadata may mention credentials/secret references, but
	// they must not read env or resolve raw credential material directly.
	return strings.Contains(lower, "os.getenv(") ||
		strings.Contains(lower, "os.lookupenv(") ||
		strings.Contains(lower, "lookupenv(") ||
		strings.Contains(lower, "env(") ||
		strings.Contains(lower, "os.environ") ||
		strings.Contains(lower, "getenv(")
}

func hasResellerCredentialContext(lower string) bool {
	credentialTerms := []string{
		"providerapikey",
		"provider_api_key",
		"api key",
		"apikey",
		"credential",
		"credentials",
		"secret",
		"secretreference",
		"secret_reference",
		"token",
	}
	for _, term := range credentialTerms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func repositoryRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func mustRel(t *testing.T, base string, path string) string {
	t.Helper()

	rel, err := filepath.Rel(base, path)
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
