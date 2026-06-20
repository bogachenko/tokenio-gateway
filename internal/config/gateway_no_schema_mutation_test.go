package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGatewayDoesNotMutateSchemaAtStartupEvidence(t *testing.T) {
	repoRoot := gatewayNoSchemaMutationRepoRoot(t)
	files := gatewayNoSchemaMutationTextFiles(t, repoRoot)

	requireFileContains(t, files, "cmd/migrate/main.go", "MigrateMain")
	requireFileContains(t, files, "cmd/gateway/main.go", "GatewayMain")

	assertGatewayNoSchemaMutationEvidence(t, files, "gateway schema mutation prohibition evidence", []string{
		"does not mutate schema",
		"without automatic migrations",
		"does not apply migrations",
		"no automatic migrations",
	})

	assertGatewayStartupFilesDoNotApplyMigrations(t, repoRoot)
}

func gatewayNoSchemaMutationRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "../.."))
}

func gatewayNoSchemaMutationTextFiles(t *testing.T, repoRoot string) map[string]string {
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
		if rel == "internal/config/gateway_no_schema_mutation_test.go" {
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

func requireFileContains(t *testing.T, files map[string]string, path string, needle string) {
	t.Helper()

	content, ok := files[path]
	if !ok {
		t.Fatalf("missing %s", path)
	}
	if !strings.Contains(content, needle) {
		t.Fatalf("%s does not contain %q", path, needle)
	}
}

func assertGatewayNoSchemaMutationEvidence(t *testing.T, files map[string]string, label string, needles []string) {
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

func assertGatewayStartupFilesDoNotApplyMigrations(t *testing.T, repoRoot string) {
	t.Helper()

	startupFiles := []string{
		filepath.Join(repoRoot, "cmd/gateway/main.go"),
	}
	gatewayMainFile := findGatewayMainFile(t, repoRoot)
	if gatewayMainFile != "" {
		startupFiles = append(startupFiles, gatewayMainFile)
	}

	for _, path := range startupFiles {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, forbidden := range []string{
			"app.MigrateMain(",
			"MigrateMain()",
			".MigrateMain(",
			"docker-compose-migrate",
			"ApplyMigration(",
			"RunMigrations(",
		} {
			if strings.Contains(string(content), forbidden) {
				t.Fatalf("%s contains gateway startup migration apply marker %q", path, forbidden)
			}
		}
	}
}

func findGatewayMainFile(t *testing.T, repoRoot string) string {
	t.Helper()

	var found string
	err := filepath.WalkDir(filepath.Join(repoRoot, "internal"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if found != "" {
			return filepath.SkipAll
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fileSet := token.NewFileSet()
		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return err
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Name.Name == "GatewayMain" {
				found = path
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("find GatewayMain: %v", err)
	}
	if found == "" {
		t.Fatalf("GatewayMain declaration not found")
	}
	return found
}
