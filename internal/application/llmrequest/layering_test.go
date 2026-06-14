package llmrequest_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/bogachenko/tokenio-gateway"

func TestProductionFilesDoNotImportForbiddenLayers(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	forbiddenExact := map[string]struct{}{
		"net/http": {},
		"os":       {},
	}
	forbiddenPrefixes := []string{
		modulePath + "/internal/application/",
		modulePath + "/internal/config",
		modulePath + "/internal/infrastructure",
		modulePath + "/internal/transport",
	}

	files := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() ||
			!strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") {
			continue
		}

		parsed, err := parser.ParseFile(
			files,
			filepath.Clean(name),
			nil,
			parser.ImportsOnly,
		)
		if err != nil {
			t.Fatalf("ParseFile %s: %v", name, err)
		}
		for _, imported := range parsed.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf(
					"unquote import in %s: %v",
					name,
					err,
				)
			}
			if _, forbidden := forbiddenExact[path]; forbidden {
				t.Errorf(
					"%s imports forbidden package %q",
					name,
					path,
				)
			}
			for _, prefix := range forbiddenPrefixes {
				if strings.HasPrefix(path, prefix) {
					t.Errorf(
						"%s imports forbidden layer %q",
						name,
						path,
					)
				}
			}
		}
	}
}
