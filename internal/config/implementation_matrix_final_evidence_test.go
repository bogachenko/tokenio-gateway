package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestImplementationMatrixEvidencePathsExist(t *testing.T) {
	repoRoot := implementationMatrixRepoRoot(t)
	matrixPath := filepath.Join(repoRoot, "docs/implementation-matrix.md")

	content, err := os.ReadFile(matrixPath)
	if err != nil {
		t.Fatalf("read implementation matrix: %v", err)
	}

	paths := implementationMatrixBacktickPaths(string(content))
	if len(paths) == 0 {
		t.Fatalf("implementation matrix does not contain backticked evidence paths")
	}

	for _, path := range paths {
		if shouldSkipImplementationMatrixPath(path) {
			continue
		}
		absolute := filepath.Join(repoRoot, filepath.FromSlash(path))
		if _, err := os.Stat(absolute); err != nil {
			t.Fatalf("implementation matrix references missing evidence path %q: %v", path, err)
		}
	}
}

func TestImplementationMatrixRecordsFinalProductionGateEvidence(t *testing.T) {
	repoRoot := implementationMatrixRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "docs/implementation-matrix.md"))
	if err != nil {
		t.Fatalf("read implementation matrix: %v", err)
	}
	matrix := string(content)

	for _, want := range []string{
		"gateway_schema_startup_test.go",
		"gateway_no_schema_mutation_test.go",
		"sigterm_shutdown_test.go",
		"worker_bounded_cycles_test.go",
		"goroutine_leak_start_stop_test.go",
		"concurrent_gateway_idempotency_test.go",
		"concurrent_workers_durable_commands_test.go",
		"logs_no_secrets_test.go",
		"sdk_compatible_requests_test.go",
		"implementation_matrix_final_evidence_test.go",
	} {
		if !strings.Contains(matrix, want) {
			t.Fatalf("implementation matrix missing final evidence path %q", want)
		}
	}
}

func implementationMatrixRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "../.."))
}

func implementationMatrixBacktickPaths(content string) []string {
	re := regexp.MustCompile("`([^`]+)`")
	matches := re.FindAllStringSubmatch(content, -1)

	seen := make(map[string]struct{})
	var paths []string
	for _, match := range matches {
		value := strings.TrimSpace(match[1])
		if !looksLikeImplementationPath(value) {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		paths = append(paths, value)
	}
	return paths
}

func looksLikeImplementationPath(value string) bool {
	return strings.HasPrefix(value, "cmd/") ||
		strings.HasPrefix(value, "docs/") ||
		strings.HasPrefix(value, "internal/") ||
		strings.HasPrefix(value, "integration/") ||
		strings.HasPrefix(value, "scripts/") ||
		strings.HasSuffix(value, ".go") ||
		strings.HasSuffix(value, ".md") ||
		strings.HasSuffix(value, ".sql") ||
		strings.HasSuffix(value, ".sh")
}

func shouldSkipImplementationMatrixPath(path string) bool {
	if strings.Contains(path, "*") ||
		strings.Contains(path, "...") ||
		strings.Contains(path, " ") ||
		strings.Contains(path, ":") {
		return true
	}
	return false
}
