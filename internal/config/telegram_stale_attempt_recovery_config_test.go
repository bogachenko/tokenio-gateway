package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadTelegramStaleAttemptRecoveryDefaults(t *testing.T) {
	setValidRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TelegramStaleAttemptRecoveryStaleAfter != 5*time.Minute {
		t.Fatalf("stale after = %s", cfg.TelegramStaleAttemptRecoveryStaleAfter)
	}
	if cfg.TelegramStaleAttemptRecoveryInterval != time.Minute {
		t.Fatalf("interval = %s", cfg.TelegramStaleAttemptRecoveryInterval)
	}
	if cfg.TelegramStaleAttemptRecoveryBatchSize != 100 {
		t.Fatalf("batch size = %d", cfg.TelegramStaleAttemptRecoveryBatchSize)
	}
}

func TestLoadRejectsInvalidTelegramStaleAttemptRecoveryConfig(t *testing.T) {
	tests := []struct {
		key   string
		value string
		want  string
	}{
		{
			key:   "TOKENIO_TELEGRAM_STALE_ATTEMPT_RECOVERY_STALE_AFTER",
			value: "0s",
			want:  "TOKENIO_TELEGRAM_STALE_ATTEMPT_RECOVERY_STALE_AFTER must be positive",
		},
		{
			key:   "TOKENIO_TELEGRAM_STALE_ATTEMPT_RECOVERY_INTERVAL",
			value: "0s",
			want:  "TOKENIO_TELEGRAM_STALE_ATTEMPT_RECOVERY_INTERVAL must be positive",
		},
		{
			key:   "TOKENIO_TELEGRAM_STALE_ATTEMPT_RECOVERY_BATCH_SIZE",
			value: "0",
			want:  "TOKENIO_TELEGRAM_STALE_ATTEMPT_RECOVERY_BATCH_SIZE must be >= 1",
		},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			setValidRequiredEnv(t)
			t.Setenv(test.key, test.value)

			_, err := Load()
			if err == nil {
				t.Fatal("expected Load error")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}
