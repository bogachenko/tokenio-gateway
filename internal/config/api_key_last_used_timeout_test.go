package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadAPIKeyLastUsedTimeout(
	t *testing.T,
) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{
			name: "default",
			want: 250 * time.Millisecond,
		},
		{
			name:  "explicit",
			value: "750ms",
			want:  750 * time.Millisecond,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setValidRequiredEnv(t)
			t.Setenv(
				"TOKENIO_API_KEY_LAST_USED_TIMEOUT",
				test.value,
			)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.APIKeyLastUsedTimeout != test.want {
				t.Fatalf(
					"APIKeyLastUsedTimeout=%s want=%s",
					cfg.APIKeyLastUsedTimeout,
					test.want,
				)
			}
		})
	}
}

func TestLoadRejectsInvalidAPIKeyLastUsedTimeout(
	t *testing.T,
) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "zero",
			value: "0s",
			want:  "TOKENIO_API_KEY_LAST_USED_TIMEOUT must be positive",
		},
		{
			name:  "negative",
			value: "-1ms",
			want:  "TOKENIO_API_KEY_LAST_USED_TIMEOUT must be positive",
		},
		{
			name:  "invalid duration",
			value: "invalid",
			want:  "TOKENIO_API_KEY_LAST_USED_TIMEOUT must be duration",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setValidRequiredEnv(t)
			t.Setenv(
				"TOKENIO_API_KEY_LAST_USED_TIMEOUT",
				test.value,
			)

			_, err := Load()
			if err == nil {
				t.Fatal("expected Load error")
			}
			if !strings.Contains(
				err.Error(),
				test.want,
			) {
				t.Fatalf(
					"error=%v want substring %q",
					err,
					test.want,
				)
			}
		})
	}
}
