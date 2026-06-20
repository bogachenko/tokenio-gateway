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

func TestConfigFieldsAreUsedOutsideConfigPackage(t *testing.T) {
	repoRoot := configDeadKnobRepoRoot(t)
	fields := configStructFieldNames(t, filepath.Join(repoRoot, "internal/config/config.go"))
	files := goSourceFilesOutsideConfigPackage(t, repoRoot)

	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			for _, file := range files {
				content, err := os.ReadFile(file)
				if err != nil {
					t.Fatalf("read %s: %v", file, err)
				}
				if strings.Contains(string(content), "."+field) ||
					strings.Contains(string(content), field+":") {
					return
				}
			}
			t.Fatalf("Config.%s is not used outside internal/config; remove the field instead of keeping a dead knob", field)
		})
	}
}

func configDeadKnobRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "../.."))
}

func configStructFieldNames(t *testing.T, path string) []string {
	t.Helper()

	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var fields []string
	ast.Inspect(parsed, func(node ast.Node) bool {
		typeSpec, ok := node.(*ast.TypeSpec)
		if !ok || typeSpec.Name.Name != "Config" {
			return true
		}
		structType, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			t.Fatalf("Config is not a struct")
		}
		for _, field := range structType.Fields.List {
			for _, name := range field.Names {
				fields = append(fields, name.Name)
			}
		}
		return false
	})

	if len(fields) == 0 {
		t.Fatalf("Config fields not found")
	}
	return fields
}

func goSourceFilesOutsideConfigPackage(t *testing.T, repoRoot string) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir(repoRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		name := entry.Name()
		if entry.IsDir() {
			switch name {
			case ".git", "vendor", ".dart_tool", "build", "node_modules":
				return filepath.SkipDir
			}
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if rel == "internal/config" {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no Go files found outside internal/config")
	}
	return files
}
