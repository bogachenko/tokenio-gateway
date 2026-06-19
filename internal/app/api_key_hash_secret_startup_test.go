package app

import (
	"strings"
	"testing"
)

const startupTestProvisioningEncryptionKeyBase64 = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

func setStartupTestRequiredEnv(t *testing.T) {
	t.Helper()

	t.Setenv("TOKENIO_ENV", "test")
	t.Setenv("TOKENIO_DATABASE_DSN", "postgres://tokenio:tokenio@localhost:5432/tokenio?sslmode=disable")
	t.Setenv("TOKENIO_BILLING_BASE_URL", "https://billing.example.test")
	t.Setenv("TOKENIO_BILLING_SERVICE_TOKEN", "billing-service-token")
	t.Setenv("TOKENIO_BILLING_JWT_SIGNING_KEY", "billing-jwt-signing-key")
	t.Setenv("TOKENIO_ADMIN_TOKEN", "admin-token")
	t.Setenv("TOKENIO_API_KEY_HASH_SECRET", "api-key-hash-secret")
	t.Setenv("TOKENIO_PROVISIONING_SERVICE_TOKEN", "provisioning-service-token")
	t.Setenv("TOKENIO_API_KEY_PROVISIONING_ENCRYPTION_KEY", startupTestProvisioningEncryptionKeyBase64)
}

func TestRunFailsBeforeRuntimeWithoutAPIKeyHashSecret(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "missing", value: ""},
		{name: "whitespace", value: " \t\n "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setStartupTestRequiredEnv(t)
			t.Setenv("TOKENIO_API_KEY_HASH_SECRET", tt.value)

			err := Run()
			if err == nil {
				t.Fatalf("expected Run() to fail before runtime startup")
			}
			if !strings.Contains(err.Error(), "TOKENIO_API_KEY_HASH_SECRET is required") {
				t.Fatalf("expected API key hash secret startup error, got %v", err)
			}
			if strings.Contains(err.Error(), "api-key-hash-secret") {
				t.Fatalf("startup error must not contain API key hash secret value")
			}
		})
	}
}
