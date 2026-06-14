package admin

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAdminApplicationDoesNotImportSiblingApplicationPackages(
	t *testing.T,
) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read admin package directory: %v", err)
	}

	const applicationPrefix = "github.com/bogachenko/tokenio-gateway/internal/application/"

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() ||
			!strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(
			token.NewFileSet(),
			filepath.Clean(name),
			nil,
			parser.ImportsOnly,
		)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		for _, imported := range file.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf(
					"decode import %s in %s: %v",
					imported.Path.Value,
					name,
					err,
				)
			}
			if strings.HasPrefix(path, applicationPrefix) {
				t.Fatalf(
					"%s imports sibling application package %s",
					name,
					path,
				)
			}
		}
	}
}
