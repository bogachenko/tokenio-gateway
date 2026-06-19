package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUserAPIKeyHashingUsesOnlyHMACSHA256(t *testing.T) {
	repo := repositoryRoot(t)

	apiKeyHashFiles := productionGoFilesContainingAny(t, repo, []string{
		"api key hash",
		"api_key_hash",
		"apikeyhash",
		"api key digest",
		"api_key_digest",
		"tokenio_api_key_hash_secret",
	})
	if len(apiKeyHashFiles) == 0 {
		t.Fatalf("no production Go files found for user API key hash implementation")
	}

	var hmacSHA256Files []string
	var forbidden []string
	for _, file := range apiKeyHashFiles {
		body := mustReadFile(t, filepath.Join(repo, file))
		lower := strings.ToLower(body)
		if strings.Contains(body, "crypto/hmac") && strings.Contains(body, "crypto/sha256") {
			hmacSHA256Files = append(hmacSHA256Files, file)
		}

		for _, pattern := range []string{
			"sha256.sum256(",
			"crypto/sha1",
			"sha1.",
			"crypto/md5",
			"md5.",
			"bcrypt.",
			"scrypt.",
			"argon2.",
		} {
			if strings.Contains(lower, pattern) {
				forbidden = append(forbidden, file+": "+pattern)
			}
		}
	}

	if len(hmacSHA256Files) == 0 {
		t.Fatalf("user API key hash implementation does not import both crypto/hmac and crypto/sha256; files=%v", apiKeyHashFiles)
	}
	if len(forbidden) > 0 {
		t.Fatalf("user API key hash implementation must not use non-HMAC/raw hash alternatives: %v", forbidden)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("resolve caller")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", file)
		}
		dir = parent
	}
}

func productionGoFilesContainingAny(t *testing.T, repo string, needles []string) []string {
	t.Helper()
	var matches []string
	err := filepath.WalkDir(repo, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "vendor", ".cache", "tmp", "build", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body := strings.ToLower(mustReadFile(t, path))
		for _, needle := range needles {
			if strings.Contains(body, strings.ToLower(needle)) {
				rel, relErr := filepath.Rel(repo, path)
				if relErr != nil {
					return relErr
				}
				matches = append(matches, filepath.ToSlash(rel))
				return nil
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository: %v", err)
	}
	return matches
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}
