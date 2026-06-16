package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadBillingRecoveryDefaults(t *testing.T) {
	setValidRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BillingRecoveryInterval != time.Minute {
		t.Fatalf("interval=%s, want %s", cfg.BillingRecoveryInterval, time.Minute)
	}
	if cfg.BillingRecoveryBatchSize != 100 {
		t.Fatalf("batch size=%d, want 100", cfg.BillingRecoveryBatchSize)
	}
}

func TestLoadRejectsInvalidBillingRecoveryConfig(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{
			name:  "non-positive interval",
			key:   "TOKENIO_BILLING_RECOVERY_INTERVAL",
			value: "0s",
			want:  "TOKENIO_BILLING_RECOVERY_INTERVAL must be positive",
		},
		{
			name:  "non-positive batch size",
			key:   "TOKENIO_BILLING_RECOVERY_BATCH_SIZE",
			value: "0",
			want:  "TOKENIO_BILLING_RECOVERY_BATCH_SIZE must be >= 1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setValidRequiredEnv(t)
			t.Setenv(test.key, test.value)

			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load error=%v, want containing %q", err, test.want)
			}
		})
	}
}
