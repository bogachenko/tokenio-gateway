package postgres

import (
	"strings"
	"testing"
)

func TestRouteEndpointContractsMigrationIsCanonical(
	t *testing.T,
) {
	items, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("migration count=%d want=4", len(items))
	}

	migration := items[3]
	if migration.Version != 4 ||
		migration.Name != "route_endpoint_contracts" {
		t.Fatalf("migration=%+v", migration)
	}

	for _, needle := range []string{
		"tokenio_routes_capabilities_object_chk",
		"tokenio_routes_endpoint_configuration_chk",
		"jsonb_typeof(capabilities) = 'object'",
		"endpoint_kind = 'chat'",
		"default_max_output_tokens > 0",
		"endpoint_kind = 'embeddings'",
		"endpoint_kind = 'images_generation'",
		"default_max_output_tokens = 0",
		"capabilities -> 'tool_choice'",
		"capabilities -> 'tools'",
		"capabilities -> 'json_schema'",
		"capabilities -> 'response_format'",
		"cannot enforce route endpoint contracts",
	} {
		if !strings.Contains(migration.SQL, needle) {
			t.Fatalf("migration missing %q", needle)
		}
	}
}
