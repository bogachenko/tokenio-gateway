package config

import (
	"testing"
	"time"
)

func TestLoadHTTPShutdownTimeout(t *testing.T) {
	setValidRequiredEnv(t)
	t.Setenv("TOKENIO_HTTP_SHUTDOWN_TIMEOUT", "17s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPShutdownTimeout != 17*time.Second {
		t.Fatalf(
			"HTTPShutdownTimeout = %s, want 17s",
			cfg.HTTPShutdownTimeout,
		)
	}
}

func TestLoadRejectsNonPositiveHTTPShutdownTimeout(
	t *testing.T,
) {
	setValidRequiredEnv(t)
	t.Setenv("TOKENIO_HTTP_SHUTDOWN_TIMEOUT", "0s")

	if _, err := Load(); err == nil {
		t.Fatal("expected non-positive shutdown timeout error")
	}
}
