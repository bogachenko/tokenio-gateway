package postgres

import "testing"

func TestLoadMigrationsReturnsCanonicalSequence(t *testing.T) {
	items, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}

	expectedNames := []string{
		"registry",
		"ledger_and_charge_command",
		"operational_admin_provisioning",
		"route_endpoint_contracts",
		"route_price_endpoint_contracts",
		"route_reseller_provider_contract",
		"forwarding_attempts",
		"telegram_delivery_attempts",
	}
	if len(items) != len(expectedNames) {
		t.Fatalf("migration count = %d, want %d", len(items), len(expectedNames))
	}
	for index, expectedName := range expectedNames {
		item := items[index]
		if item.Version != int64(index+1) {
			t.Fatalf(
				"migration[%d].Version = %d, want %d",
				index,
				item.Version,
				index+1,
			)
		}
		if item.Name != expectedName {
			t.Fatalf(
				"migration[%d].Name = %q, want %q",
				index,
				item.Name,
				expectedName,
			)
		}
		if item.Filename == "" || item.SQL == "" || len(item.Checksum) != 64 {
			t.Fatalf("migration[%d] is incomplete: %+v", index, item)
		}
	}
}
