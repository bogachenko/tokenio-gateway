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

func TestApplicationPackagesDoNotImportOuterLayers(t *testing.T) {
	forbiddenPrefixes := []string{
		"github.com/bogachenko/tokenio-gateway/internal/app/",
		"github.com/bogachenko/tokenio-gateway/internal/infrastructure/",
		"github.com/bogachenko/tokenio-gateway/internal/transport/",
	}

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
		for _, imported := range file.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			for _, prefix := range forbiddenPrefixes {
				if strings.HasPrefix(importPath, prefix) {
					t.Errorf("%s imports forbidden outer layer %s", path, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk application tree: %v", err)
	}
}
