package postgres_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func migrationFile(t *testing.T, name string) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}

	path := filepath.Join(
		filepath.Dir(currentFile),
		"..",
		"..",
		"..",
		"db",
		"migrations",
		name,
	)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration %s: %v", name, err)
	}
	return string(content)
}

func TestRegistryMigrationContainsCanonicalTablesAndConstraints(t *testing.T) {
	up := migrationFile(t, "000001_registry.up.sql")

	required := []string{
		"CREATE TABLE tokenio_users",
		"CREATE TABLE tokenio_api_keys",
		"CREATE TABLE tokenio_resellers",
		"CREATE TABLE tokenio_routes",
		"CREATE TABLE tokenio_route_prices",
		"tokenio_users_external_billing_user_id_uq",
		"tokenio_api_keys_key_hash_uq",
		"tokenio_resellers_provider_type_chk",
		"tokenio_routes_api_family_chk",
		"tokenio_routes_endpoint_kind_chk",
		"tokenio_routes_model_rewrite_policy_chk",
		"tokenio_routes_unique_provider_model_route_uq",
		"tokenio_routes_lookup_idx",
		"markup_coefficient DOUBLE PRECISION",
		"markup_coefficient < 'Infinity'::double precision",
	}
	for _, fragment := range required {
		if !strings.Contains(up, fragment) {
			t.Errorf("up migration missing %q", fragment)
		}
	}

	forbidden := []string{
		"raw_api_key",
		"billing_jwt",
		"billing_service_token",
		"admin_token",
		"NUMERIC(12, 6)",
		"ON DELETE CASCADE",
	}
	for _, fragment := range forbidden {
		if strings.Contains(up, fragment) {
			t.Errorf("up migration contains forbidden fragment %q", fragment)
		}
	}
}

func TestRegistryDownMigrationDropsTablesInDependencyOrder(t *testing.T) {
	down := migrationFile(t, "000001_registry.down.sql")
	expected := []string{
		"DROP TABLE tokenio_route_prices;",
		"DROP TABLE tokenio_routes;",
		"DROP TABLE tokenio_resellers;",
		"DROP TABLE tokenio_api_keys;",
		"DROP TABLE tokenio_users;",
	}

	position := -1
	for _, statement := range expected {
		next := strings.Index(down, statement)
		if next < 0 {
			t.Fatalf("down migration missing %q", statement)
		}
		if next <= position {
			t.Fatalf("down migration dependency order is invalid at %q", statement)
		}
		position = next
	}
}
