package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGatewayCompatibleSchemaStartupEvidence(t *testing.T) {
	repoRoot := productionGateRepoRoot(t)
	files := productionGateTextFiles(t, repoRoot)

	assertProductionGateEvidence(t, files, "gateway command entrypoint", []string{
		"GatewayMain",
		"cmd/gateway",
	})
	assertProductionGateEvidence(t, files, "schema compatibility/version evidence", []string{
		"schema",
		"migration",
		"version",
		"compatible",
	})
	assertProductionGateEvidence(t, files, "gateway startup before serving", []string{
		"startup",
		"Start",
		"ListenAndServe",
		"Run",
	})
	assertProductionGateEvidence(t, files, "incompatible schema startup failure", []string{
		"incompatible",
		"must be",
		"failed",
		"error",
	})
	assertProductionGateEvidence(t, files, "compatible schema automated coverage", []string{
		"TestGateway",
		"compatible schema",
		"schema",
	})
}

func productionGateRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "../.."))
}

func productionGateTextFiles(t *testing.T, repoRoot string) map[string]string {
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
		if rel == "internal/config/gateway_schema_startup_test.go" {
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

func assertProductionGateEvidence(t *testing.T, files map[string]string, label string, needles []string) {
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
