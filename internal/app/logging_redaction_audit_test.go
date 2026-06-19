package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestStructuredLoggingRedactionCurrentStateIsAudited(t *testing.T) {
	want := map[string]struct{}{}
	for _, site := range currentStdlibLoggingSitesPendingStructuredLogger() {
		want[site] = struct{}{}
	}

	gotSites := collectStdlibLoggingSites(t)
	got := map[string]struct{}{}
	for _, site := range gotSites {
		got[site] = struct{}{}
	}

	for _, site := range sortedStringKeys(got) {
		if _, ok := want[site]; !ok {
			t.Fatalf("unaudited stdlib logging site %s; route it through central logger/redactor or add explicit evidence", site)
		}
	}
	for _, site := range sortedStringKeys(want) {
		if _, ok := got[site]; !ok {
			t.Fatalf("audited stdlib logging site %s no longer exists; remove it from pending logging audit", site)
		}
	}
}

func currentStdlibLoggingSitesPendingStructuredLogger() []string {
	return []string{
		"cmd/gateway/main.go:11",
		"cmd/migrate/main.go:25",
		"internal/app/app.go:70",
		"internal/app/app.go:87",
		"internal/app/application.go:470",
	}
}

func collectStdlibLoggingSites(t *testing.T) []string {
	t.Helper()
	root := repositoryRootForAudit(t)
	patterns := []string{
		"log.Default(",
		"log.New(",
		"log.Print(",
		"log.Printf(",
		"log.Println(",
		"log.Fatal(",
		"log.Fatalf(",
		"log.Fatalln(",
		"fmt.Print(",
		"fmt.Printf(",
		"fmt.Println(",
	}
	sites := make([]string, 0)
	for _, dir := range []string{"cmd", "internal/app"} {
		base := filepath.Join(root, dir)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == ".git" || entry.Name() == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			bytes, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for index, line := range strings.Split(string(bytes), "\n") {
				trimmed := strings.TrimSpace(line)
				if trimmed == "" || strings.HasPrefix(trimmed, "//") {
					continue
				}
				for _, pattern := range patterns {
					if strings.Contains(trimmed, pattern) {
						relative, err := filepath.Rel(root, path)
						if err != nil {
							return err
						}
						sites = append(sites, filepath.ToSlash(relative)+":"+itoa(index+1))
						break
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(sites)
	return sites
}

func readAuditTree(t *testing.T, relativeDir string) string {
	t.Helper()
	root := repositoryRootForAudit(t)
	base := filepath.Join(root, "internal/app", relativeDir)
	var builder strings.Builder
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		bytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		builder.Write(bytes)
		builder.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return builder.String()
}

func repositoryRootForAudit(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func sortedStringKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := [20]byte{}
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
