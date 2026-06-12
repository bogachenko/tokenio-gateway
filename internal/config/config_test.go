package config

import (
	"strings"
	"testing"
)

func setValidRequiredEnv(t *testing.T) {
	t.Helper()

	t.Setenv("TOKENIO_ENV", "test")
	t.Setenv("TOKENIO_DATABASE_DSN", "postgres://tokenio:tokenio@localhost:5432/tokenio?sslmode=disable")
	t.Setenv("TOKENIO_BILLING_BASE_URL", "https://billing.example.test")
	t.Setenv("TOKENIO_BILLING_SERVICE_TOKEN", "billing-service-token")
	t.Setenv("TOKENIO_BILLING_JWT_SIGNING_KEY", "billing-jwt-signing-key")
	t.Setenv("TOKENIO_ADMIN_TOKEN", "admin-token")
	t.Setenv("TOKENIO_API_KEY_HASH_SECRET", "api-key-hash-secret")
}

func TestLoadAPIKeyHashSecret(t *testing.T) {
	setValidRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.APIKeyHashSecret != "api-key-hash-secret" {
		t.Fatalf("expected APIKeyHashSecret to be loaded")
	}
}

func TestLoadRequiresAPIKeyHashSecret(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "missing", value: ""},
		{name: "whitespace", value: " \t\n "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidRequiredEnv(t)
			t.Setenv("TOKENIO_API_KEY_HASH_SECRET", tt.value)

			_, err := Load()
			if err == nil {
				t.Fatalf("expected Load() error")
			}
			if !strings.Contains(err.Error(), "TOKENIO_API_KEY_HASH_SECRET is required") {
				t.Fatalf("expected required secret error, got %v", err)
			}
			if strings.Contains(err.Error(), strings.TrimSpace(tt.value)) && strings.TrimSpace(tt.value) != "" {
				t.Fatalf("error must not contain secret value")
			}
		})
	}
}

func TestLoadAPIKeyHashSecretErrorDoesNotContainSecretValue(t *testing.T) {
	setValidRequiredEnv(t)
	secret := "sensitive-api-key-hash-secret"
	t.Setenv("TOKENIO_API_KEY_HASH_SECRET", secret)
	t.Setenv("TOKENIO_COST_CURRENCY", "USD")

	_, err := Load()
	if err == nil {
		t.Fatalf("expected Load() error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error must not contain secret value")
	}
}
