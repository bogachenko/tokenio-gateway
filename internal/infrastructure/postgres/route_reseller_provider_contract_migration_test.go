package postgres

import (
	"strings"
	"testing"
)

func TestRouteResellerProviderContractMigrationIsCanonical(
	t *testing.T,
) {
	items, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(items) < 6 {
		t.Fatalf(
			"migration count=%d want at least 6",
			len(items),
		)
	}

	migration := items[5]
	if migration.Version != 6 ||
		migration.Name != "route_reseller_provider_contract" {
		t.Fatalf("migration=%+v", migration)
	}

	for _, needle := range []string{
		"cannot enforce route reseller provider contract",
		"tokenio_validate_route_reseller_provider_contract",
		"tokenio_enforce_route_reseller_provider_contract",
		"pg_advisory_xact_lock",
		"hashtextextended",
		"route.provider_type <> reseller.provider_type",
		"CREATE CONSTRAINT TRIGGER tokenio_routes_reseller_provider_contract_trg",
		"CREATE CONSTRAINT TRIGGER tokenio_resellers_route_provider_contract_trg",
		"AFTER INSERT OR UPDATE OF reseller_id, provider_type",
		"AFTER UPDATE OF provider_type",
		"DEFERRABLE INITIALLY DEFERRED",
	} {
		if !strings.Contains(migration.SQL, needle) {
			t.Fatalf("migration missing %q", needle)
		}
	}
}
