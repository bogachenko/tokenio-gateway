package postgres_test

import (
	"strings"
	"testing"
)

func TestLedgerMigrationContainsCanonicalTables(t *testing.T) {
	up := migrationFile(t, "000002_ledger_and_charge_command.up.sql")

	required := []string{
		"CREATE TABLE tokenio_usage_records",
		"CREATE TABLE tokenio_billing_sessions",
		"CREATE TABLE tokenio_billing_charge_batches",
		"CREATE TABLE tokenio_billing_charge_allocations",
		"CREATE TABLE tokenio_billing_charge_expected_records",
		"tokenio_usage_records_status_chk",
		"tokenio_usage_records_usage_completeness_chk",
		"tokenio_usage_records_idempotency_uq",
		"tokenio_usage_records_pending_idx",
		"tokenio_usage_records_chargeable_idx",
		"tokenio_usage_records_billing_group_idx",
		"tokenio_billing_charge_batches_status_chk",
		"tokenio_billing_charge_allocations_batch_position_uq",
		"tokenio_billing_charge_expected_records_request_idx",
		"expected_record JSONB NOT NULL",
		"CHECK (amount_cents > 0)",
		"CHECK (charged_amount_cents > 0)",
		"billing_error_code TEXT NOT NULL DEFAULT ''",
		"updated_at TIMESTAMPTZ NOT NULL DEFAULT now()",
	}
	for _, fragment := range required {
		if !strings.Contains(up, fragment) {
			t.Errorf("ledger migration missing %q", fragment)
		}
	}

	forbidden := []string{
		"billing_error_body",
		"ON DELETE CASCADE",
		"NUMERIC(12, 6)",
	}
	for _, fragment := range forbidden {
		if strings.Contains(up, fragment) {
			t.Errorf("ledger migration contains forbidden fragment %q", fragment)
		}
	}
}

func TestUsageMigrationPersistsAllUsageDimensions(t *testing.T) {
	up := migrationFile(t, "000002_ledger_and_charge_command.up.sql")

	dimensions := []string{
		"input_tokens",
		"cached_input_tokens",
		"output_tokens",
		"reasoning_tokens",
		"image_input_tokens",
		"audio_input_tokens",
		"audio_output_tokens",
		"file_input_tokens",
		"video_input_tokens",
		"image_generation_units",
	}

	for _, dimension := range dimensions {
		actual := "\n    " + dimension + " BIGINT NOT NULL DEFAULT 0"
		estimated := "\n    estimated_" + dimension + " BIGINT NOT NULL DEFAULT 0"
		if !strings.Contains(up, actual) {
			t.Errorf("usage migration missing actual dimension %q", dimension)
		}
		if !strings.Contains(up, estimated) {
			t.Errorf("usage migration missing estimated dimension %q", dimension)
		}
	}
}

func TestLedgerDownMigrationDropsTablesInDependencyOrder(t *testing.T) {
	down := migrationFile(t, "000002_ledger_and_charge_command.down.sql")
	expected := []string{
		"DROP TABLE tokenio_billing_charge_expected_records;",
		"DROP TABLE tokenio_billing_charge_allocations;",
		"DROP TABLE tokenio_billing_charge_batches;",
		"DROP TABLE tokenio_billing_sessions;",
		"DROP TABLE tokenio_usage_records;",
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
