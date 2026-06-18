package postgres_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

type migrationForeignKeyManifestEntry struct {
	ChildTable   string
	ChildColumn  string
	ParentTable  string
	ParentColumn string
	DeleteAction string
}

func TestFullMigrationSchemaManifest(t *testing.T) {
	content := allUpMigrations(t)

	actualTables, actualForeignKeys := parseMigrationSchemaManifest(t, content)

	expectedTables := []string{
		"tokenio_admin_audit_log",
		"tokenio_api_key_provisionings",
		"tokenio_api_keys",
		"tokenio_billing_charge_allocations",
		"tokenio_billing_charge_batches",
		"tokenio_billing_charge_expected_records",
		"tokenio_billing_sessions",
		"tokenio_forwarding_attempts",
		"tokenio_resellers",
		"tokenio_route_events",
		"tokenio_route_prices",
		"tokenio_routes",
		"tokenio_telegram_alerts",
		"tokenio_telegram_delivery_attempts",
		"tokenio_usage_records",
		"tokenio_users",
	}
	if !reflect.DeepEqual(actualTables, expectedTables) {
		t.Fatalf("migration tables mismatch\nactual: %#v\nexpected: %#v", actualTables, expectedTables)
	}

	expectedForeignKeys := []migrationForeignKeyManifestEntry{
		{"tokenio_api_key_provisionings", "api_key_id", "tokenio_api_keys", "id", "NO ACTION"},
		{"tokenio_api_key_provisionings", "user_id", "tokenio_users", "id", "NO ACTION"},
		{"tokenio_api_keys", "user_id", "tokenio_users", "id", "NO ACTION"},
		{"tokenio_billing_charge_allocations", "batch_id", "tokenio_billing_charge_batches", "id", "NO ACTION"},
		{"tokenio_billing_charge_allocations", "local_request_id", "tokenio_usage_records", "local_request_id", "NO ACTION"},
		{"tokenio_billing_charge_batches", "user_id", "tokenio_users", "id", "NO ACTION"},
		{"tokenio_billing_charge_expected_records", "batch_id", "tokenio_billing_charge_batches", "id", "NO ACTION"},
		{"tokenio_billing_charge_expected_records", "local_request_id", "tokenio_usage_records", "local_request_id", "NO ACTION"},
		{"tokenio_billing_sessions", "user_id", "tokenio_users", "id", "NO ACTION"},
		{"tokenio_forwarding_attempts", "local_request_id", "tokenio_usage_records", "local_request_id", "CASCADE"},
		{"tokenio_forwarding_attempts", "reseller_id", "tokenio_resellers", "id", "NO ACTION"},
		{"tokenio_forwarding_attempts", "route_id", "tokenio_routes", "id", "NO ACTION"},
		{"tokenio_route_events", "reseller_id", "tokenio_resellers", "id", "NO ACTION"},
		{"tokenio_route_events", "route_id", "tokenio_routes", "id", "NO ACTION"},
		{"tokenio_route_prices", "route_id", "tokenio_routes", "id", "NO ACTION"},
		{"tokenio_routes", "reseller_id", "tokenio_resellers", "id", "NO ACTION"},
		{"tokenio_telegram_alerts", "reseller_id", "tokenio_resellers", "id", "NO ACTION"},
		{"tokenio_telegram_alerts", "route_id", "tokenio_routes", "id", "NO ACTION"},
		{"tokenio_telegram_delivery_attempts", "alert_id", "tokenio_telegram_alerts", "id", "NO ACTION"},
		{"tokenio_usage_records", "api_key_id", "tokenio_api_keys", "id", "NO ACTION"},
		{"tokenio_usage_records", "selected_reseller_id", "tokenio_resellers", "id", "NO ACTION"},
		{"tokenio_usage_records", "selected_route_id", "tokenio_routes", "id", "NO ACTION"},
		{"tokenio_usage_records", "user_id", "tokenio_users", "id", "NO ACTION"},
	}
	sortMigrationForeignKeys(expectedForeignKeys)
	if !reflect.DeepEqual(actualForeignKeys, expectedForeignKeys) {
		t.Fatalf("migration foreign keys mismatch\nactual: %#v\nexpected: %#v", actualForeignKeys, expectedForeignKeys)
	}

	cascadeCount := 0
	for _, foreignKey := range actualForeignKeys {
		if foreignKey.DeleteAction == "CASCADE" {
			cascadeCount++
			if foreignKey.ChildTable != "tokenio_forwarding_attempts" ||
				foreignKey.ChildColumn != "local_request_id" {
				t.Fatalf("unexpected cascade foreign key: %#v", foreignKey)
			}
		}
	}
	if cascadeCount != 1 {
		t.Fatalf("cascade foreign key count=%d, want 1", cascadeCount)
	}
}

func allUpMigrations(t *testing.T) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}

	pattern := filepath.Join(
		filepath.Dir(currentFile),
		"..",
		"..",
		"..",
		"db",
		"migrations",
		"*.up.sql",
	)
	paths, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(paths) != 9 {
		t.Fatalf("up migration count=%d, want 9", len(paths))
	}
	sort.Strings(paths)

	var builder strings.Builder
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration %s: %v", path, err)
		}
		builder.Write(content)
		builder.WriteByte('\n')
	}
	return builder.String()
}

func parseMigrationSchemaManifest(
	t *testing.T,
	content string,
) ([]string, []migrationForeignKeyManifestEntry) {
	t.Helper()

	createTable := regexp.MustCompile(
		`(?i)CREATE\s+TABLE\s+([a-zA-Z0-9_]+)\s*\(`,
	)
	reference := regexp.MustCompile(
		`(?is)\bREFERENCES\s+([a-zA-Z0-9_]+)\s*` +
			`\(\s*([a-zA-Z0-9_]+)\s*\)` +
			`(?:\s+ON\s+DELETE\s+` +
			`(CASCADE|RESTRICT|SET\s+NULL|SET\s+DEFAULT|NO\s+ACTION))?`,
	)
	tableLevelForeignKey := regexp.MustCompile(
		`(?is)\bFOREIGN\s+KEY\s*\(\s*([a-zA-Z0-9_]+)\s*\)`,
	)
	columnName := regexp.MustCompile(`^\s*([a-zA-Z0-9_]+)\b`)

	var tables []string
	var foreignKeys []migrationForeignKeyManifestEntry
	searchFrom := 0

	for {
		location := createTable.FindStringSubmatchIndex(content[searchFrom:])
		if location == nil {
			break
		}

		bodyStart := searchFrom + location[1]
		table := content[searchFrom+location[2] : searchFrom+location[3]]
		bodyEnd := matchingClosingParenthesis(t, content, bodyStart-1)
		body := content[bodyStart:bodyEnd]

		tables = append(tables, table)
		for _, segment := range splitTopLevelCommaSegments(body) {
			referenceMatch := reference.FindStringSubmatch(segment)
			if referenceMatch == nil {
				continue
			}

			childColumn := ""
			if tableLevel := tableLevelForeignKey.FindStringSubmatch(segment); tableLevel != nil {
				childColumn = tableLevel[1]
			} else if column := columnName.FindStringSubmatch(segment); column != nil {
				childColumn = column[1]
			}
			if childColumn == "" {
				t.Fatalf("cannot resolve child column for %s segment %q", table, segment)
			}

			deleteAction := "NO ACTION"
			if referenceMatch[3] != "" {
				deleteAction = strings.ToUpper(
					strings.Join(strings.Fields(referenceMatch[3]), " "),
				)
			}
			foreignKeys = append(foreignKeys, migrationForeignKeyManifestEntry{
				ChildTable:   table,
				ChildColumn:  childColumn,
				ParentTable:  referenceMatch[1],
				ParentColumn: referenceMatch[2],
				DeleteAction: deleteAction,
			})
		}

		searchFrom = bodyEnd + 1
	}

	sort.Strings(tables)
	sortMigrationForeignKeys(foreignKeys)
	return tables, foreignKeys
}

func matchingClosingParenthesis(
	t *testing.T,
	content string,
	openIndex int,
) int {
	t.Helper()

	depth := 0
	inSingleQuote := false
	for index := openIndex; index < len(content); index++ {
		switch content[index] {
		case '\'':
			if index+1 < len(content) && content[index+1] == '\'' {
				index++
				continue
			}
			inSingleQuote = !inSingleQuote
		case '(':
			if !inSingleQuote {
				depth++
			}
		case ')':
			if !inSingleQuote {
				depth--
				if depth == 0 {
					return index
				}
			}
		}
	}
	t.Fatal("unterminated CREATE TABLE body")
	return -1
}

func splitTopLevelCommaSegments(body string) []string {
	var result []string
	start := 0
	depth := 0
	inSingleQuote := false

	for index := 0; index < len(body); index++ {
		switch body[index] {
		case '\'':
			if index+1 < len(body) && body[index+1] == '\'' {
				index++
				continue
			}
			inSingleQuote = !inSingleQuote
		case '(':
			if !inSingleQuote {
				depth++
			}
		case ')':
			if !inSingleQuote {
				depth--
			}
		case ',':
			if !inSingleQuote && depth == 0 {
				result = append(result, body[start:index])
				start = index + 1
			}
		}
	}
	result = append(result, body[start:])
	return result
}

func sortMigrationForeignKeys(values []migrationForeignKeyManifestEntry) {
	sort.Slice(values, func(left int, right int) bool {
		leftKey := values[left].ChildTable + "\x00" +
			values[left].ChildColumn + "\x00" +
			values[left].ParentTable + "\x00" +
			values[left].ParentColumn + "\x00" +
			values[left].DeleteAction
		rightKey := values[right].ChildTable + "\x00" +
			values[right].ChildColumn + "\x00" +
			values[right].ParentTable + "\x00" +
			values[right].ParentColumn + "\x00" +
			values[right].DeleteAction
		return leftKey < rightKey
	})
}
