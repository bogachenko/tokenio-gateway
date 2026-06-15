package postgres_test

import (
	"strings"
	"testing"
)

func TestTelegramDeliveryAttemptMigrationContainsCanonicalContract(
	t *testing.T,
) {
	up := migrationFile(
		t,
		"000008_telegram_delivery_attempts.up.sql",
	)

	required := []string{
		"CREATE TABLE tokenio_telegram_delivery_attempts",
		"REFERENCES tokenio_telegram_alerts(id)",
		"UNIQUE (alert_id, attempt_number)",
		"CHECK (attempt_number > 0)",
		"'started'",
		"'succeeded'",
		"'failed'",
		"'not_sent'",
		"'sent_no_response'",
		"'response_received'",
		"completed_at >= started_at",
		"tokenio_telegram_delivery_attempts_alert_idx",
		"tokenio_telegram_delivery_attempts_open_idx",
		"tokenio_telegram_delivery_attempts_recovery_idx",
		"WHERE status = 'started'",
		"WHERE status = 'failed'",
	}
	for _, fragment := range required {
		if !strings.Contains(up, fragment) {
			t.Errorf(
				"Telegram delivery attempt migration missing %q",
				fragment,
			)
		}
	}
}

func TestTelegramDeliveryAttemptMigrationEnforcesLifecycleShape(
	t *testing.T,
) {
	up := migrationFile(
		t,
		"000008_telegram_delivery_attempts.up.sql",
	)

	required := []string{
		"status = 'started'",
		"attempt_state IS NULL",
		"failure_code IS NULL",
		"completed_at IS NULL",
		"status = 'succeeded'",
		"attempt_state = 'response_received'",
		"completed_at IS NOT NULL",
		"status = 'failed'",
		"attempt_state IS NOT NULL",
		"failure_code IS NOT NULL",
	}
	for _, fragment := range required {
		if !strings.Contains(up, fragment) {
			t.Errorf(
				"Telegram delivery lifecycle missing %q",
				fragment,
			)
		}
	}
}

func TestTelegramDeliveryAttemptMigrationDoesNotCascadeAlertDeletion(
	t *testing.T,
) {
	up := migrationFile(
		t,
		"000008_telegram_delivery_attempts.up.sql",
	)
	if strings.Contains(up, "ON DELETE CASCADE") {
		t.Fatal(
			"Telegram delivery attempt history must not cascade on alert deletion",
		)
	}
}

func TestTelegramDeliveryAttemptDownMigrationDropsAttemptTable(
	t *testing.T,
) {
	down := migrationFile(
		t,
		"000008_telegram_delivery_attempts.down.sql",
	)
	if !strings.Contains(
		down,
		"DROP TABLE IF EXISTS tokenio_telegram_delivery_attempts;",
	) {
		t.Fatal("down migration does not drop Telegram delivery attempts")
	}
}
