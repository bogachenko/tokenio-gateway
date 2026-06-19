package postgres

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestRawSecretsAreAbsentFromAuditState(t *testing.T) {
	forbidden := []string{
		"raw_api_key",
		"api_key",
		"key_hash",
		"encrypted_raw_key",
		"encryption_nonce",
		"encryption_key",
		"billing_jwt",
		"billing_service_token",
		"admin_token",
		"authorization",
	}

	for _, key := range forbidden {
		t.Run(key, func(t *testing.T) {
			state := domain.AuditState{
				"id": "safe",
				key:  "raw-secret-value",
			}
			if !auditStateContainsSecret(state) {
				t.Fatalf("auditStateContainsSecret(%q) = false", key)
			}
			if _, err := encodeAuditState(state); err == nil {
				t.Fatalf("encodeAuditState accepted forbidden audit key %q", key)
			}
		})
	}
}

func TestAdminAPIKeyAuditStateExcludesSecretMaterial(t *testing.T) {
	state := adminAPIKeyState(domain.APIKeyRecord{
		ID:        "key-id",
		UserID:    "user-id",
		Name:      "Production key",
		KeyHash:   "raw-hash-must-not-appear",
		KeyPrefix: "sp_live",
		Enabled:   true,
	})

	for _, forbidden := range []string{
		"raw_api_key",
		"api_key",
		"key_hash",
		"encrypted_raw_key",
		"encryption_nonce",
		"encryption_key",
		"billing_jwt",
		"billing_service_token",
		"admin_token",
		"authorization",
	} {
		if _, ok := state[forbidden]; ok {
			t.Fatalf("adminAPIKeyState contains forbidden key %q: %#v", forbidden, state)
		}
	}
	if strings.Contains(strings.ToLower(strings.Join(auditStateKeys(state), ",")), "hash") {
		t.Fatalf("adminAPIKeyState keys must not expose hash material: %#v", state)
	}
}

func TestDatabaseMigrationsDoNotDefineRawSecretColumns(t *testing.T) {
	root := repositoryRootForRawSecretAudit(t)
	migrationsDir := filepath.Join(root, "db", "migrations")

	var violations []string
	err := filepath.WalkDir(migrationsDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".sql") {
			return nil
		}

		body := string(mustReadRawSecretAuditFile(t, path))
		for _, column := range rawSecretColumnDefinitions(body) {
			violations = append(
				violations,
				filepath.ToSlash(mustRelRawSecretAudit(t, root, path))+":"+column,
			)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(violations) > 0 {
		t.Fatalf(
			"database migrations define raw secret persistence columns: %s",
			strings.Join(violations, ", "),
		)
	}
}

func rawSecretColumnDefinitions(sql string) []string {
	var violations []string
	re := regexp.MustCompile(`(?im)^\s*([a-z_]+)\s+(text|bytea|varchar|jsonb|json)\b`)
	matches := re.FindAllStringSubmatch(sql, -1)
	for _, match := range matches {
		column := strings.ToLower(match[1])
		if isForbiddenRawSecretColumn(column) {
			violations = append(violations, column)
		}
	}
	return violations
}

func isForbiddenRawSecretColumn(column string) bool {
	switch column {
	case "raw_api_key",
		"plain_api_key",
		"api_key_plain",
		"provider_api_key_plain",
		"billing_jwt",
		"billing_service_token",
		"admin_token",
		"telegram_bot_token",
		"authorization_header":
		return true
	default:
		return false
	}
}

func auditStateKeys(state domain.AuditState) []string {
	keys := make([]string, 0, len(state))
	for key := range state {
		keys = append(keys, key)
	}
	return keys
}

func repositoryRootForRawSecretAudit(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func mustRelRawSecretAudit(t *testing.T, base string, path string) string {
	t.Helper()

	rel, err := filepath.Rel(base, path)
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

func mustReadRawSecretAuditFile(t *testing.T, path string) []byte {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
