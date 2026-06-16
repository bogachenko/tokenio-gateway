package application_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestApplicationPackagesDoNotImportSiblingApplicationPackages(t *testing.T) {
	const prefix = "github.com/bogachenko/tokenio-gateway/internal/application/"
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		current := strings.Split(filepath.ToSlash(path), "/")[0]
		for _, imported := range file.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(importPath, prefix) {
				continue
			}
			target := strings.Split(strings.TrimPrefix(importPath, prefix), "/")[0]
			if target != current {
				t.Errorf("%s imports sibling application package %s", path, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk application tree: %v", err)
	}
}
