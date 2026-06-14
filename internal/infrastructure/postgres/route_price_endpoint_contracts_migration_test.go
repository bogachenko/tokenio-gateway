package postgres

import (
	"strings"
	"testing"
)

func TestRoutePriceEndpointContractsMigrationIsCanonical(
	t *testing.T,
) {
	items, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(items) < 5 {
		t.Fatalf("migration count=%d want at least 5", len(items))
	}

	migration := items[4]
	if migration.Version != 5 ||
		migration.Name != "route_price_endpoint_contracts" {
		t.Fatalf("migration=%+v", migration)
	}

	for _, needle := range []string{
		"cannot enforce route price endpoint contracts",
		"tokenio_validate_route_price_endpoint_contract",
		"tokenio_enforce_route_price_endpoint_contract",
		"pg_advisory_xact_lock",
		"hashtextextended",
		"CREATE CONSTRAINT TRIGGER tokenio_route_prices_endpoint_contract_trg",
		"CREATE CONSTRAINT TRIGGER tokenio_routes_price_endpoint_contract_trg",
		"DEFERRABLE INITIALLY DEFERRED",
		"AFTER INSERT OR UPDATE ON tokenio_route_prices",
		"AFTER UPDATE OF endpoint_kind ON tokenio_routes",
		"route_endpoint_kind = 'chat'",
		"route_endpoint_kind = 'embeddings'",
		"route_endpoint_kind = 'images_generation'",
		"image_generation_unit_kind <> 'generated_image'",
	} {
		if !strings.Contains(migration.SQL, needle) {
			t.Fatalf("migration missing %q", needle)
		}
	}
}
