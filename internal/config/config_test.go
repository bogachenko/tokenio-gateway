package config

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

const (
	validProvisioningEncryptionKeyBase64 = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	validProvisioningEncryptionKeyRaw    = "0123456789abcdef0123456789abcdef"
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
	t.Setenv("TOKENIO_PROVISIONING_SERVICE_TOKEN", "provisioning-service-token")
	t.Setenv("TOKENIO_API_KEY_PROVISIONING_ENCRYPTION_KEY", validProvisioningEncryptionKeyBase64)
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

func TestLoadProvisioningConfig(t *testing.T) {
	setValidRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ProvisioningServiceToken != "provisioning-service-token" {
		t.Fatalf("expected ProvisioningServiceToken to be loaded")
	}
	if !bytes.Equal(
		cfg.APIKeyProvisioningEncryptionKey,
		[]byte(validProvisioningEncryptionKeyRaw),
	) {
		t.Fatalf("expected APIKeyProvisioningEncryptionKey to be decoded")
	}
	if cfg.APIKeyProvisioningKeyVersion != "v1" {
		t.Fatalf(
			"APIKeyProvisioningKeyVersion = %q, want v1",
			cfg.APIKeyProvisioningKeyVersion,
		)
	}
	if cfg.APIKeyProvisioningTTL != 24*time.Hour {
		t.Fatalf(
			"APIKeyProvisioningTTL = %s, want %s",
			cfg.APIKeyProvisioningTTL,
			24*time.Hour,
		)
	}
	if cfg.APIKeyProvisioningExpirationInterval != time.Minute {
		t.Fatalf(
			"APIKeyProvisioningExpirationInterval = %s, want %s",
			cfg.APIKeyProvisioningExpirationInterval,
			time.Minute,
		)
	}
	if cfg.APIKeyProvisioningExpirationBatchSize != 100 {
		t.Fatalf(
			"APIKeyProvisioningExpirationBatchSize = %d, want 100",
			cfg.APIKeyProvisioningExpirationBatchSize,
		)
	}
}

func TestLoadRejectsInvalidProvisioningExpirationWorkerConfig(
	t *testing.T,
) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{
			name:  "non-positive interval",
			key:   "TOKENIO_API_KEY_PROVISIONING_EXPIRATION_INTERVAL",
			value: "0s",
			want:  "TOKENIO_API_KEY_PROVISIONING_EXPIRATION_INTERVAL must be positive",
		},
		{
			name:  "non-positive batch size",
			key:   "TOKENIO_API_KEY_PROVISIONING_EXPIRATION_BATCH_SIZE",
			value: "0",
			want:  "TOKENIO_API_KEY_PROVISIONING_EXPIRATION_BATCH_SIZE must be >= 1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setValidRequiredEnv(t)
			t.Setenv(test.key, test.value)

			_, err := Load()
			if err == nil {
				t.Fatal("expected Load() error")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf(
					"expected %q, got %v",
					test.want,
					err,
				)
			}
		})
	}
}

func TestLoadRejectsInvalidBillingBaseURL(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "relative URL",
			value: "billing.example.test",
			want:  "TOKENIO_BILLING_BASE_URL must be a valid absolute URL",
		},
		{
			name:  "surrounding whitespace",
			value: "https://billing.example.test ",
			want:  "TOKENIO_BILLING_BASE_URL must not contain leading or trailing whitespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidRequiredEnv(t)
			t.Setenv("TOKENIO_BILLING_BASE_URL", tt.value)

			_, err := Load()
			if err == nil {
				t.Fatalf("expected Load() error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q, got %v", tt.want, err)
			}
		})
	}
}

func TestLoadRejectsInvalidProvisioningEncryptionKey(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "invalid base64",
			value: "not-base64",
			want:  "TOKENIO_API_KEY_PROVISIONING_ENCRYPTION_KEY must be valid base64",
		},
		{
			name:  "wrong decoded length",
			value: "c2hvcnQ=",
			want:  "TOKENIO_API_KEY_PROVISIONING_ENCRYPTION_KEY must decode to exactly 32 bytes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidRequiredEnv(t)
			t.Setenv("TOKENIO_API_KEY_PROVISIONING_ENCRYPTION_KEY", tt.value)

			_, err := Load()
			if err == nil {
				t.Fatalf("expected Load() error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q, got %v", tt.want, err)
			}
			if strings.Contains(err.Error(), tt.value) {
				t.Fatalf("error must not contain provisioning encryption key value")
			}
		})
	}
}

func TestLoadRequiresProvisioningSecretsInProduction(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{
			name: "missing service token",
			key:  "TOKENIO_PROVISIONING_SERVICE_TOKEN",
			want: "TOKENIO_PROVISIONING_SERVICE_TOKEN is required in production",
		},
		{
			name: "missing encryption key",
			key:  "TOKENIO_API_KEY_PROVISIONING_ENCRYPTION_KEY",
			want: "TOKENIO_API_KEY_PROVISIONING_ENCRYPTION_KEY is required in production",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidRequiredEnv(t)
			t.Setenv("TOKENIO_ENV", "production")
			t.Setenv("TOKENIO_ADMIN_TOKEN", strings.Repeat("a", 32))
			t.Setenv(tt.key, "")

			_, err := Load()
			if err == nil {
				t.Fatalf("expected Load() error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q, got %v", tt.want, err)
			}
		})
	}
}

func TestLoadAllowsMissingProvisioningSecretsOutsideProduction(t *testing.T) {
	setValidRequiredEnv(t)
	t.Setenv("TOKENIO_PROVISIONING_SERVICE_TOKEN", "")
	t.Setenv("TOKENIO_API_KEY_PROVISIONING_ENCRYPTION_KEY", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ProvisioningServiceToken != "" {
		t.Fatalf("expected empty ProvisioningServiceToken")
	}
	if cfg.APIKeyProvisioningEncryptionKey != nil {
		t.Fatalf("expected nil APIKeyProvisioningEncryptionKey")
	}
}

func TestLoadRejectsSharedAPIKeyAndProvisioningEncryptionMaterial(t *testing.T) {
	setValidRequiredEnv(t)
	t.Setenv("TOKENIO_API_KEY_HASH_SECRET", validProvisioningEncryptionKeyRaw)

	_, err := Load()
	if err == nil {
		t.Fatalf("expected Load() error")
	}
	want := "must use different key material"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected %q, got %v", want, err)
	}
	if strings.Contains(err.Error(), validProvisioningEncryptionKeyRaw) {
		t.Fatalf("error must not contain secret value")
	}
}

func TestLoadRejectsNonPositiveProvisioningTTL(t *testing.T) {
	setValidRequiredEnv(t)
	t.Setenv("TOKENIO_API_KEY_PROVISIONING_TTL", "0s")

	_, err := Load()
	if err == nil {
		t.Fatalf("expected Load() error")
	}
	want := "TOKENIO_API_KEY_PROVISIONING_TTL must be positive"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected %q, got %v", want, err)
	}
}

func TestLoadUpstreamResponseBodyMaxBytes(t *testing.T) {
	setValidRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.UpstreamResponseBodyMaxBytes != 64<<20 {
		t.Fatalf(
			"UpstreamResponseBodyMaxBytes = %d, want %d",
			cfg.UpstreamResponseBodyMaxBytes,
			int64(64<<20),
		)
	}

	t.Setenv(
		"TOKENIO_UPSTREAM_RESPONSE_BODY_MAX_BYTES",
		"2048",
	)
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() with override error = %v", err)
	}
	if cfg.UpstreamResponseBodyMaxBytes != 2048 {
		t.Fatalf(
			"UpstreamResponseBodyMaxBytes = %d, want 2048",
			cfg.UpstreamResponseBodyMaxBytes,
		)
	}
}

func TestLoadRejectsInvalidUpstreamResponseBodyMaxBytes(
	t *testing.T,
) {
	setValidRequiredEnv(t)
	t.Setenv(
		"TOKENIO_UPSTREAM_RESPONSE_BODY_MAX_BYTES",
		"0",
	)

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load() error")
	}
	if !strings.Contains(
		err.Error(),
		"TOKENIO_UPSTREAM_RESPONSE_BODY_MAX_BYTES must be positive",
	) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadAppliesBehavioralDefaults(t *testing.T) {
	setValidRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.GatewayAddr != ":8880" {
		t.Fatalf("GatewayAddr = %q, want :8880", cfg.GatewayAddr)
	}
	if cfg.BillingJWTTTL != 15*time.Minute {
		t.Fatalf("BillingJWTTTL = %s, want %s", cfg.BillingJWTTTL, 15*time.Minute)
	}
	if cfg.BillingTimeout != 30*time.Second {
		t.Fatalf("BillingTimeout = %s, want %s", cfg.BillingTimeout, 30*time.Second)
	}
	if cfg.CostCurrency != "RUB" {
		t.Fatalf("CostCurrency = %q, want RUB", cfg.CostCurrency)
	}
	if cfg.AutoChargeThresholdCents != 1000 {
		t.Fatalf("AutoChargeThresholdCents = %d, want 1000", cfg.AutoChargeThresholdCents)
	}
	if cfg.MinChargeAmountCents != 100 {
		t.Fatalf("MinChargeAmountCents = %d, want 100", cfg.MinChargeAmountCents)
	}
	if cfg.MinRequestBalanceCents != 500 {
		t.Fatalf("MinRequestBalanceCents = %d, want 500", cfg.MinRequestBalanceCents)
	}
	if cfg.TokenEstimationSafetyFactor != 1.25 {
		t.Fatalf("TokenEstimationSafetyFactor = %f, want 1.25", cfg.TokenEstimationSafetyFactor)
	}
	if cfg.CostEstimationSafetyFactor != 1.10 {
		t.Fatalf("CostEstimationSafetyFactor = %f, want 1.10", cfg.CostEstimationSafetyFactor)
	}
	if cfg.RequestBodyMaxBytes != 64<<20 {
		t.Fatalf("RequestBodyMaxBytes = %d, want %d", cfg.RequestBodyMaxBytes, int64(64<<20))
	}
	if cfg.UpstreamTimeout != 90*time.Second {
		t.Fatalf("UpstreamTimeout = %s, want %s", cfg.UpstreamTimeout, 90*time.Second)
	}
	if cfg.UpstreamMaxAttempts != 3 {
		t.Fatalf("UpstreamMaxAttempts = %d, want 3", cfg.UpstreamMaxAttempts)
	}
	if cfg.UpstreamMaxBackoff != 2*time.Second {
		t.Fatalf("UpstreamMaxBackoff = %s, want %s", cfg.UpstreamMaxBackoff, 2*time.Second)
	}
	if cfg.RateLimitMaxWait != 5*time.Second {
		t.Fatalf("RateLimitMaxWait = %s, want %s", cfg.RateLimitMaxWait, 5*time.Second)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("LogLevel = %q, want info", cfg.LogLevel)
	}
	if cfg.LogFormat != "text" {
		t.Fatalf("LogFormat = %q, want text", cfg.LogFormat)
	}
	if cfg.LogBodies {
		t.Fatalf("LogBodies = true, want false")
	}
}

func TestLoadRejectsMissingRequiredConfig(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{
			name: "database dsn",
			key:  "TOKENIO_DATABASE_DSN",
			want: "TOKENIO_DATABASE_DSN is required",
		},
		{
			name: "billing base url",
			key:  "TOKENIO_BILLING_BASE_URL",
			want: "TOKENIO_BILLING_BASE_URL is required",
		},
		{
			name: "billing service token",
			key:  "TOKENIO_BILLING_SERVICE_TOKEN",
			want: "TOKENIO_BILLING_SERVICE_TOKEN is required",
		},
		{
			name: "billing jwt signing key",
			key:  "TOKENIO_BILLING_JWT_SIGNING_KEY",
			want: "TOKENIO_BILLING_JWT_SIGNING_KEY is required",
		},
		{
			name: "admin token",
			key:  "TOKENIO_ADMIN_TOKEN",
			want: "TOKENIO_ADMIN_TOKEN is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setValidRequiredEnv(t)
			t.Setenv(test.key, "")

			_, err := Load()
			if err == nil {
				t.Fatal("expected Load() error")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}

func TestLoadRejectsInvalidBehavioralConfigValues(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{
			name:  "environment",
			key:   "TOKENIO_ENV",
			value: "staging",
			want:  "TOKENIO_ENV must be one of production, development, test",
		},
		{
			name:  "log level",
			key:   "TOKENIO_LOG_LEVEL",
			value: "trace",
			want:  "TOKENIO_LOG_LEVEL must be one of debug, info, warn, error",
		},
		{
			name:  "log format",
			key:   "TOKENIO_LOG_FORMAT",
			value: "console",
			want:  "TOKENIO_LOG_FORMAT must be text or json",
		},
		{
			name:  "upstream attempts",
			key:   "TOKENIO_UPSTREAM_MAX_ATTEMPTS",
			value: "0",
			want:  "TOKENIO_UPSTREAM_MAX_ATTEMPTS must be >= 1",
		},
		{
			name:  "token estimation factor",
			key:   "TOKENIO_TOKEN_ESTIMATION_SAFETY_FACTOR",
			value: "0.99",
			want:  "TOKENIO_TOKEN_ESTIMATION_SAFETY_FACTOR must be >= 1",
		},
		{
			name:  "request body max bytes",
			key:   "TOKENIO_REQUEST_BODY_MAX_BYTES",
			value: "0",
			want:  "TOKENIO_REQUEST_BODY_MAX_BYTES must be positive",
		},
		{
			name:  "invalid bool",
			key:   "TOKENIO_LOG_BODIES",
			value: "sometimes",
			want:  "TOKENIO_LOG_BODIES must be bool",
		},
		{
			name:  "invalid duration",
			key:   "TOKENIO_UPSTREAM_TIMEOUT",
			value: "soon",
			want:  "TOKENIO_UPSTREAM_TIMEOUT must be duration",
		},
		{
			name:  "invalid int",
			key:   "TOKENIO_UPSTREAM_MAX_ATTEMPTS",
			value: "many",
			want:  "TOKENIO_UPSTREAM_MAX_ATTEMPTS must be int",
		},
		{
			name:  "invalid float",
			key:   "TOKENIO_COST_ESTIMATION_SAFETY_FACTOR",
			value: "safe",
			want:  "TOKENIO_COST_ESTIMATION_SAFETY_FACTOR must be float64",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setValidRequiredEnv(t)
			t.Setenv(test.key, test.value)

			_, err := Load()
			if err == nil {
				t.Fatal("expected Load() error")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}
