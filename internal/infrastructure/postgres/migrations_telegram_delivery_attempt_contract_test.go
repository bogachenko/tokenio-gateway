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

func TestTelegramDeliveryMessageIDMigrationContainsCanonicalContract(
	t *testing.T,
) {
	up := migrationFile(
		t,
		"000010_telegram_delivery_message_id.up.sql",
	)

	required := []string{
		"ADD COLUMN telegram_message_id TEXT",
		"tokenio_telegram_delivery_attempts_message_id_shape_chk",
		"status = 'succeeded'",
		"telegram_message_id IS NULL",
		"btrim(telegram_message_id) <> ''",
		"status <> 'succeeded'",
	}
	for _, fragment := range required {
		if !strings.Contains(up, fragment) {
			t.Errorf(
				"Telegram message id migration missing %q",
				fragment,
			)
		}
	}

	down := migrationFile(
		t,
		"000010_telegram_delivery_message_id.down.sql",
	)
	if !strings.Contains(down, "DROP COLUMN IF EXISTS telegram_message_id") {
		t.Fatal("down migration does not drop Telegram message id column")
	}
}
