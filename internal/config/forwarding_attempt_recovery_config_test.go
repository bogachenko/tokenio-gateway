package config

import (
	"testing"
	"time"
)

func TestLoadForwardingAttemptRecoveryDefaults(
	t *testing.T,
) {
	setValidRequiredEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ForwardingAttemptRecoveryStaleAfter !=
		5*time.Minute {
		t.Fatalf(
			"stale after = %s",
			cfg.ForwardingAttemptRecoveryStaleAfter,
		)
	}
	if cfg.ForwardingAttemptRecoveryInterval !=
		time.Minute {
		t.Fatalf(
			"interval = %s",
			cfg.ForwardingAttemptRecoveryInterval,
		)
	}
	if cfg.ForwardingAttemptRecoveryBatchSize != 100 {
		t.Fatalf(
			"batch size = %d",
			cfg.ForwardingAttemptRecoveryBatchSize,
		)
	}
}
