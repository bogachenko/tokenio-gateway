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

	assertGatewayStartupFilesDoNotCallMigrations(t, repoRoot)
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

func assertGatewayStartupFilesDoNotCallMigrations(t *testing.T, repoRoot string) {
	t.Helper()

	startupFiles := []string{
		filepath.Join(repoRoot, "cmd/gateway/main.go"),
		findGatewayMainFile(t, repoRoot),
	}

	for _, path := range startupFiles {
		assertFileDoesNotCallFunctions(t, path, map[string]struct{}{
			"MigrateMain":     {},
			"ApplyMigration":  {},
			"RunMigrations":   {},
			"RunMigration":    {},
			"ApplyMigrations": {},
		})
	}
}

func assertFileDoesNotCallFunctions(t *testing.T, path string, forbidden map[string]struct{}) {
	t.Helper()

	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	checked := false
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if path != filepath.Join(gatewayNoSchemaMutationRepoRoot(t), "cmd/gateway/main.go") &&
			function.Name.Name != "GatewayMain" {
			continue
		}
		checked = true
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}

			var name string
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				name = fun.Name
			case *ast.SelectorExpr:
				name = fun.Sel.Name
			default:
				return true
			}

			if _, exists := forbidden[name]; exists {
				position := fileSet.Position(call.Pos())
				t.Fatalf("%s calls migration function %s at %s", path, name, position)
			}
			return true
		})
	}
	if !checked {
		t.Fatalf("no gateway startup function checked in %s", path)
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
