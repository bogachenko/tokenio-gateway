package architecture_test

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/bogachenko/tokenio-gateway"

func TestInternalDependencyDirection(t *testing.T) {
	root := repositoryRoot(t)
	fileSet := token.NewFileSet()
	var violations []string

	walkProductionGoFiles(t, root, func(path string, relative string) error {
		file, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse %s: %w", relative, err)
		}

		sourcePackage := internalPackage(relative)
		for _, importSpec := range file.Imports {
			importPath, err := strconv.Unquote(importSpec.Path.Value)
			if err != nil {
				return fmt.Errorf("decode import %s in %s: %w", importSpec.Path.Value, relative, err)
			}
			targetPackage := projectInternalPackage(importPath)
			if targetPackage == "" {
				continue
			}
			if reason := forbiddenDependency(sourcePackage, targetPackage); reason != "" {
				violations = append(violations, fmt.Sprintf(
					"%s imports %s: %s",
					fileSet.Position(importSpec.Pos()),
					importPath,
					reason,
				))
			}
		}
		return nil
	})

	sort.Strings(violations)
	if len(violations) != 0 {
		t.Fatalf("forbidden internal dependency directions:\n%s", strings.Join(violations, "\n"))
	}
}

func TestDirectEnvironmentAccessBoundary(t *testing.T) {
	root := repositoryRoot(t)
	fileSet := token.NewFileSet()
	var violations []string

	walkProductionGoFiles(t, root, func(path string, relative string) error {
		file, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse imports in %s: %w", relative, err)
		}

		osImportName := ""
		for _, importSpec := range file.Imports {
			importPath, err := strconv.Unquote(importSpec.Path.Value)
			if err != nil {
				return fmt.Errorf("decode import %s in %s: %w", importSpec.Path.Value, relative, err)
			}
			if importPath != "os" {
				continue
			}
			switch {
			case importSpec.Name == nil:
				osImportName = "os"
			case importSpec.Name.Name == ".":
				violations = append(violations, fmt.Sprintf(
					"%s: dot import of os bypasses environment access enforcement",
					fileSet.Position(importSpec.Pos()),
				))
			case importSpec.Name.Name != "_":
				osImportName = importSpec.Name.Name
			}
		}
		if osImportName == "" {
			return nil
		}

		fullFile, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse declarations in %s: %w", relative, err)
		}
		ast.Inspect(fullFile, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			receiver, ok := selector.X.(*ast.Ident)
			if !ok || receiver.Name != osImportName {
				return true
			}
			if selector.Sel.Name != "Getenv" && selector.Sel.Name != "LookupEnv" {
				return true
			}
			if directEnvironmentAccessAllowed(relative) {
				return true
			}
			violations = append(violations, fmt.Sprintf(
				"%s: direct os.%s is forbidden outside approved boundaries",
				fileSet.Position(selector.Pos()),
				selector.Sel.Name,
			))
			return true
		})
		return nil
	})

	sort.Strings(violations)
	if len(violations) != 0 {
		t.Fatalf("forbidden direct environment access:\n%s", strings.Join(violations, "\n"))
	}
}

func walkProductionGoFiles(t *testing.T, root string, visit func(path string, relative string) error) {
	t.Helper()

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "vendor":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("relative path for %s: %w", path, err)
		}
		return visit(path, filepath.ToSlash(relative))
	})
	if err != nil {
		t.Fatalf("inspect repository files: %v", err)
	}
}

func directEnvironmentAccessAllowed(relative string) bool {
	relative = filepath.ToSlash(relative)
	return strings.HasPrefix(relative, "internal/app/") ||
		strings.HasPrefix(relative, "internal/config/") ||
		strings.HasPrefix(relative, "internal/infrastructure/secrets/envresolver/")
}

func forbiddenDependency(source string, target string) string {
	switch {
	case source == "domain":
		if target == "ports" || target == "infrastructure" || target == "transport" || target == "app" || strings.HasPrefix(target, "application/") {
			return "domain must not depend on outer layers"
		}
	case strings.HasPrefix(source, "application/"):
		if target != "domain" && target != "ports" {
			return "application may depend only on domain and ports"
		}
	case source == "ports":
		switch target {
		case "infrastructure", "transport", "app":
			return "ports must not depend on concrete outer layers"
		}
	case source == "transport":
		switch target {
		case "infrastructure", "app":
			return "transport may call application contracts only"
		}
	}
	return ""
}

func internalPackage(relative string) string {
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) < 2 || parts[0] != "internal" {
		return ""
	}
	if parts[1] == "application" && len(parts) >= 3 {
		return "application/" + parts[2]
	}
	return parts[1]
}

func projectInternalPackage(importPath string) string {
	prefix := modulePath + "/internal/"
	if !strings.HasPrefix(importPath, prefix) {
		return ""
	}
	relative := strings.TrimPrefix(importPath, prefix)
	parts := strings.Split(relative, "/")
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	if parts[0] == "application" && len(parts) >= 2 {
		return "application/" + parts[1]
	}
	return parts[0]
}

func repositoryRoot(t *testing.T) string {
	t.Helper()

	current, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		goMod := filepath.Join(current, "go.mod")
		file, err := os.Open(goMod)
		if err == nil {
			scanner := bufio.NewScanner(file)
			firstLine := ""
			if scanner.Scan() {
				firstLine = scanner.Text()
			}
			scanErr := scanner.Err()
			closeErr := file.Close()
			if scanErr != nil {
				t.Fatalf("read %s: %v", goMod, scanErr)
			}
			if closeErr != nil {
				t.Fatalf("close %s: %v", goMod, closeErr)
			}
			if firstLine == "module "+modulePath {
				return current
			}
		} else if !os.IsNotExist(err) {
			t.Fatalf("open %s: %v", goMod, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			t.Fatalf("repository root with module %s not found", modulePath)
		}
		current = parent
	}
}
