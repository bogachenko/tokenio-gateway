package postgres_test

import (
	"strings"
	"testing"
)

func TestOperationalMigrationContainsCanonicalTablesAndIndexes(t *testing.T) {
	up := migrationFile(t, "000003_operational_admin_provisioning.up.sql")

	required := []string{
		"CREATE TABLE tokenio_route_events",
		"CREATE TABLE tokenio_telegram_alerts",
		"CREATE TABLE tokenio_admin_audit_log",
		"CREATE TABLE tokenio_api_key_provisionings",
		"tokenio_route_events_route_idx",
		"tokenio_route_events_reseller_idx",
		"tokenio_route_events_request_idx",
		"tokenio_telegram_alerts_dedupe_idx",
		"tokenio_api_key_provisionings_idempotency_uq",
		"tokenio_api_key_provisionings_user_pending_uq",
		"tokenio_api_key_provisionings_external_billing_user_idx",
		"tokenio_api_key_provisionings_api_key_idx",
		"tokenio_api_key_provisionings_expiry_idx",
	}
	for _, fragment := range required {
		if !strings.Contains(up, fragment) {
			t.Errorf("operational migration missing %q", fragment)
		}
	}
}

func TestOperationalMigrationEnforcesCanonicalStates(t *testing.T) {
	up := migrationFile(t, "000003_operational_admin_provisioning.up.sql")

	required := []string{
		"'selected'",
		"'cooldown_set'",
		"'healthcheck_recovered'",
		"'balance_low'",
		"'pending'",
		"'sent'",
		"'failed'",
		"'suppressed'",
		"result_type IN ('key_created', 'already_provisioned')",
		"status IN ('pending_delivery', 'delivered', 'expired')",
		"CHECK (delivery_attempts >= 0)",
		"status = 'pending_delivery'",
		"result_type = 'key_created'",
		"encrypted_raw_key IS NOT NULL",
		"encryption_nonce IS NOT NULL",
		"encryption_key_version IS NOT NULL",
		"expires_at IS NOT NULL",
		"status IN ('delivered', 'expired')",
		"encrypted_raw_key IS NULL",
		"encryption_nonce IS NULL",
	}
	for _, fragment := range required {
		if !strings.Contains(up, fragment) {
			t.Errorf("operational migration missing state invariant %q", fragment)
		}
	}
}

func TestOperationalMigrationDoesNotPersistForbiddenSecrets(t *testing.T) {
	up := migrationFile(t, "000003_operational_admin_provisioning.up.sql")

	forbidden := []string{
		"raw_api_key TEXT",
		"reseller_api_key",
		"billing_jwt",
		"billing_service_token",
		"admin_token",
		"ON DELETE CASCADE",
	}
	for _, fragment := range forbidden {
		if strings.Contains(up, fragment) {
			t.Errorf("operational migration contains forbidden fragment %q", fragment)
		}
	}
}

func TestOperationalDownMigrationDropsTablesInDependencyOrder(t *testing.T) {
	down := migrationFile(t, "000003_operational_admin_provisioning.down.sql")
	expected := []string{
		"DROP TABLE tokenio_api_key_provisionings;",
		"DROP TABLE tokenio_admin_audit_log;",
		"DROP TABLE tokenio_telegram_alerts;",
		"DROP TABLE tokenio_route_events;",
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
