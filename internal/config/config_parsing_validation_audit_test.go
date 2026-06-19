package config

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

type configEnvKeyEvidence struct {
	field      string
	parser     string
	validation string
	behavior   string
}

func TestConsumedConfigEnvKeysHaveParsingValidationAndBehaviorEvidence(t *testing.T) {
	consumed := consumedConfigEnvKeys(t)
	evidence := configEnvKeyEvidenceMap()

	for _, key := range consumed {
		item, ok := evidence[key]
		if !ok {
			t.Fatalf("consumed env key %s has no audit evidence", key)
		}
		if strings.TrimSpace(item.field) == "" {
			t.Fatalf("consumed env key %s has no typed Config field evidence", key)
		}
		if strings.TrimSpace(item.parser) == "" {
			t.Fatalf("consumed env key %s has no parser evidence", key)
		}
		if strings.TrimSpace(item.validation) == "" {
			t.Fatalf("consumed env key %s has no validation/default evidence", key)
		}
		if strings.TrimSpace(item.behavior) == "" {
			t.Fatalf("consumed env key %s has no behavioral test evidence", key)
		}
	}

	consumedSet := make(map[string]struct{}, len(consumed))
	for _, key := range consumed {
		consumedSet[key] = struct{}{}
	}
	for key := range evidence {
		if _, ok := consumedSet[key]; !ok {
			t.Fatalf("audit evidence references non-consumed env key %s", key)
		}
	}
}

func consumedConfigEnvKeys(t *testing.T) []string {
	t.Helper()

	content, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatalf("read config.go: %v", err)
	}

	re := regexp.MustCompile(`TOKENIO_[A-Z0-9_]+`)
	seen := map[string]struct{}{}
	for _, match := range re.FindAllString(string(content), -1) {
		seen[match] = struct{}{}
	}

	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func configEnvKeyEvidenceMap() map[string]configEnvKeyEvidence {
	return map[string]configEnvKeyEvidence{
		"TOKENIO_ADMIN_TOKEN": {
			field:      "Config.AdminToken",
			parser:     "required string via envLoader.required",
			validation: "required; production minimum length",
			behavior:   "config_test.go: setValidRequiredEnv, production strictness",
		},
		"TOKENIO_API_KEY_HASH_SECRET": {
			field:      "Config.APIKeyHashSecret",
			parser:     "required string via envLoader.required",
			validation: "required; secret material differs from provisioning encryption key",
			behavior:   "config_test.go: TestLoadAPIKeyHashSecret, TestLoadRequiresAPIKeyHashSecret, secret-safe errors",
		},
		"TOKENIO_API_KEY_LAST_USED_TIMEOUT": {
			field:      "Config.APIKeyLastUsedTimeout",
			parser:     "duration with default 250ms",
			validation: "positive duration",
			behavior:   "config_test.go: config defaults and invalid duration coverage",
		},
		"TOKENIO_API_KEY_PROVISIONING_ENCRYPTION_KEY": {
			field:      "Config.APIKeyProvisioningEncryptionKey",
			parser:     "optional strict base64 decoded bytes",
			validation: "optional outside production; required in production; exactly 32 bytes; differs from API key HMAC secret",
			behavior:   "config_test.go: TestLoadProvisioningConfig, invalid key, production requiredness, key material separation",
		},
		"TOKENIO_API_KEY_PROVISIONING_EXPIRATION_BATCH_SIZE": {
			field:      "Config.APIKeyProvisioningExpirationBatchSize",
			parser:     "int with default 100",
			validation: "must be >= 1",
			behavior:   "config_test.go: TestLoadProvisioningConfig, TestLoadRejectsInvalidProvisioningExpirationWorkerConfig",
		},
		"TOKENIO_API_KEY_PROVISIONING_EXPIRATION_INTERVAL": {
			field:      "Config.APIKeyProvisioningExpirationInterval",
			parser:     "duration with default 1m",
			validation: "positive duration",
			behavior:   "config_test.go: TestLoadProvisioningConfig, TestLoadRejectsInvalidProvisioningExpirationWorkerConfig",
		},
		"TOKENIO_API_KEY_PROVISIONING_KEY_VERSION": {
			field:      "Config.APIKeyProvisioningKeyVersion",
			parser:     "string with default v1",
			validation: "non-empty",
			behavior:   "config_test.go: TestLoadProvisioningConfig",
		},
		"TOKENIO_API_KEY_PROVISIONING_TTL": {
			field:      "Config.APIKeyProvisioningTTL",
			parser:     "duration with default 24h",
			validation: "positive duration",
			behavior:   "config_test.go: TestLoadProvisioningConfig, TestLoadRejectsNonPositiveProvisioningTTL",
		},
		"TOKENIO_AUTO_CHARGE_THRESHOLD_CENTS": {
			field:      "Config.AutoChargeThresholdCents",
			parser:     "int64 with default 1000",
			validation: "positive",
			behavior:   "config_test.go: numeric defaults and invalid range coverage",
		},
		"TOKENIO_BILLING_BASE_URL": {
			field:      "Config.BillingBaseURL",
			parser:     "required absolute URL",
			validation: "required, absolute URL, no leading/trailing whitespace",
			behavior:   "config_test.go: setValidRequiredEnv, TestLoadRejectsInvalidBillingBaseURL",
		},
		"TOKENIO_BILLING_JWT_SIGNING_KEY": {
			field:      "Config.BillingJWTSigningKey",
			parser:     "required string via envLoader.required",
			validation: "required",
			behavior:   "config_test.go: setValidRequiredEnv required config path",
		},
		"TOKENIO_BILLING_JWT_TTL": {
			field:      "Config.BillingJWTTTL",
			parser:     "duration with default 15m",
			validation: "positive duration",
			behavior:   "config_test.go: duration defaults and invalid duration coverage",
		},
		"TOKENIO_BILLING_RECOVERY_BATCH_SIZE": {
			field:      "Config.BillingRecoveryBatchSize",
			parser:     "int with default 100",
			validation: "must be >= 1",
			behavior:   "config_test.go: numeric defaults and invalid range coverage",
		},
		"TOKENIO_BILLING_RECOVERY_INTERVAL": {
			field:      "Config.BillingRecoveryInterval",
			parser:     "duration with default 1m",
			validation: "positive duration",
			behavior:   "config_test.go: duration defaults and invalid duration coverage",
		},
		"TOKENIO_BILLING_SERVICE_TOKEN": {
			field:      "Config.BillingServiceToken",
			parser:     "required string via envLoader.required",
			validation: "required",
			behavior:   "config_test.go: setValidRequiredEnv required config path",
		},
		"TOKENIO_BILLING_TIMEOUT": {
			field:      "Config.BillingTimeout",
			parser:     "duration with default 30s",
			validation: "positive duration",
			behavior:   "config_test.go: duration defaults and invalid duration coverage",
		},
		"TOKENIO_COST_CURRENCY": {
			field:      "Config.CostCurrency",
			parser:     "string with default RUB",
			validation: "must be RUB",
			behavior:   "config_test.go: TestLoadAPIKeyHashSecretErrorDoesNotContainSecretValue exercises invalid currency",
		},
		"TOKENIO_COST_ESTIMATION_SAFETY_FACTOR": {
			field:      "Config.CostEstimationSafetyFactor",
			parser:     "float64 with default 1.10",
			validation: "must be >= 1",
			behavior:   "config_test.go: numeric defaults and invalid range coverage",
		},
		"TOKENIO_DATABASE_DSN": {
			field:      "Config.DatabaseDSN",
			parser:     "required string via envLoader.required",
			validation: "required; DB connectivity is validated by runtime startup layer",
			behavior:   "config_test.go: setValidRequiredEnv required config path; app runtime tests cover DB startup boundary",
		},
		"TOKENIO_ENV": {
			field:      "Config.Environment",
			parser:     "string with default production",
			validation: "one of production, development, test",
			behavior:   "config_test.go: production strictness and test-mode fixtures",
		},
		"TOKENIO_FORWARDING_ATTEMPT_RECOVERY_BATCH_SIZE": {
			field:      "Config.ForwardingAttemptRecoveryBatchSize",
			parser:     "int with default 100",
			validation: "must be >= 1",
			behavior:   "worker/app recovery tests plus config audit evidence",
		},
		"TOKENIO_FORWARDING_ATTEMPT_RECOVERY_INTERVAL": {
			field:      "Config.ForwardingAttemptRecoveryInterval",
			parser:     "duration with default 1m",
			validation: "positive duration",
			behavior:   "worker/app recovery tests plus config audit evidence",
		},
		"TOKENIO_FORWARDING_ATTEMPT_RECOVERY_STALE_AFTER": {
			field:      "Config.ForwardingAttemptRecoveryStaleAfter",
			parser:     "duration with default 5m",
			validation: "positive duration",
			behavior:   "worker/app recovery tests plus config audit evidence",
		},
		"TOKENIO_GATEWAY_ADDR": {
			field:      "Config.GatewayAddr",
			parser:     "string with default :8880",
			validation: "non-empty",
			behavior:   "config_test.go: default config load path",
		},
		"TOKENIO_HTTP_IDLE_TIMEOUT": {
			field:      "Config.HTTPIdleTimeout",
			parser:     "duration with default 60s",
			validation: "positive duration",
			behavior:   "config_test.go: HTTP timeout default/validation coverage",
		},
		"TOKENIO_HTTP_READ_HEADER_TIMEOUT": {
			field:      "Config.HTTPReadHeaderTimeout",
			parser:     "duration with default 10s",
			validation: "positive duration",
			behavior:   "config_test.go: HTTP timeout default/validation coverage",
		},
		"TOKENIO_HTTP_READ_TIMEOUT": {
			field:      "Config.HTTPReadTimeout",
			parser:     "duration with default 120s",
			validation: "positive duration",
			behavior:   "config_test.go: HTTP timeout default/validation coverage",
		},
		"TOKENIO_HTTP_SHUTDOWN_TIMEOUT": {
			field:      "Config.HTTPShutdownTimeout",
			parser:     "duration with default 30s",
			validation: "positive duration",
			behavior:   "config_test.go: HTTP timeout default/validation coverage",
		},
		"TOKENIO_HTTP_WRITE_TIMEOUT": {
			field:      "Config.HTTPWriteTimeout",
			parser:     "duration with default 120s",
			validation: "positive duration",
			behavior:   "config_test.go: HTTP timeout default/validation coverage",
		},
		"TOKENIO_LOG_BODIES": {
			field:      "Config.LogBodies",
			parser:     "bool with default false",
			validation: "forbidden in production when true",
			behavior:   "config_test.go: production strictness coverage",
		},
		"TOKENIO_LOG_FORMAT": {
			field:      "Config.LogFormat",
			parser:     "string with default text",
			validation: "text or json",
			behavior:   "config_test.go: logging default/validation coverage",
		},
		"TOKENIO_LOG_LEVEL": {
			field:      "Config.LogLevel",
			parser:     "string with default info",
			validation: "debug, info, warn, or error",
			behavior:   "config_test.go: logging default/validation coverage",
		},
		"TOKENIO_MIN_CHARGE_AMOUNT_CENTS": {
			field:      "Config.MinChargeAmountCents",
			parser:     "int64 with default 100",
			validation: "non-negative",
			behavior:   "config_test.go: numeric defaults and invalid range coverage",
		},
		"TOKENIO_MIN_REQUEST_BALANCE_CENTS": {
			field:      "Config.MinRequestBalanceCents",
			parser:     "int64 with default 500",
			validation: "non-negative",
			behavior:   "config_test.go: numeric defaults and invalid range coverage",
		},
		"TOKENIO_PROVISIONING_SERVICE_TOKEN": {
			field:      "Config.ProvisioningServiceToken",
			parser:     "optional string outside production",
			validation: "required in production",
			behavior:   "config_test.go: TestLoadRequiresProvisioningSecretsInProduction, TestLoadAllowsMissingProvisioningSecretsOutsideProduction",
		},
		"TOKENIO_RATE_LIMIT_MAX_WAIT": {
			field:      "Config.RateLimitMaxWait",
			parser:     "duration with default 5s",
			validation: "positive duration",
			behavior:   "config_test.go: duration defaults and invalid duration coverage",
		},
		"TOKENIO_REQUEST_BODY_MAX_BYTES": {
			field:      "Config.RequestBodyMaxBytes",
			parser:     "int64 with default 64MiB",
			validation: "positive",
			behavior:   "config_test.go: request body limit default/override/invalid coverage",
		},
		"TOKENIO_RESELLER_BALANCE_ALERT_CENTS": {
			field:      "Config.ResellerBalanceAlertCents",
			parser:     "int64 with default 10000",
			validation: "non-negative",
			behavior:   "config_test.go: Telegram alert config coverage",
		},
		"TOKENIO_ROUTE_COOLDOWN_5XX": {
			field:      "Config.Cooldown5XX",
			parser:     "duration with default 30s",
			validation: "positive duration",
			behavior:   "config_test.go: cooldown default/validation coverage",
		},
		"TOKENIO_ROUTE_COOLDOWN_AUTH_ERROR": {
			field:      "Config.CooldownAuthError",
			parser:     "duration with default 24h",
			validation: "positive duration",
			behavior:   "config_test.go: cooldown default/validation coverage",
		},
		"TOKENIO_ROUTE_COOLDOWN_QUOTA_EXCEEDED": {
			field:      "Config.CooldownQuotaExceeded",
			parser:     "duration with default 24h",
			validation: "positive duration",
			behavior:   "config_test.go: cooldown default/validation coverage",
		},
		"TOKENIO_ROUTE_COOLDOWN_RATE_LIMIT": {
			field:      "Config.CooldownRateLimit",
			parser:     "duration with default 60s",
			validation: "positive duration",
			behavior:   "config_test.go: cooldown default/validation coverage",
		},
		"TOKENIO_ROUTE_COOLDOWN_TIMEOUT": {
			field:      "Config.CooldownTimeout",
			parser:     "duration with default 30s",
			validation: "positive duration",
			behavior:   "config_test.go: cooldown default/validation coverage",
		},
		"TOKENIO_TELEGRAM_ALERT_DEDUPE_INTERVAL": {
			field:      "Config.TelegramAlertDedupePeriod",
			parser:     "duration with default 1h",
			validation: "positive duration",
			behavior:   "config_test.go: Telegram alert config coverage",
		},
		"TOKENIO_TELEGRAM_BALANCE_SCAN_BATCH_SIZE": {
			field:      "Config.TelegramBalanceScanBatchSize",
			parser:     "int with default 100",
			validation: "must be >= 1",
			behavior:   "worker Telegram balance-scan tests plus config audit evidence",
		},
		"TOKENIO_TELEGRAM_BALANCE_SCAN_INTERVAL": {
			field:      "Config.TelegramBalanceScanInterval",
			parser:     "duration with default 1m",
			validation: "positive duration",
			behavior:   "worker Telegram balance-scan tests plus config audit evidence",
		},
		"TOKENIO_TELEGRAM_BOT_TOKEN": {
			field:      "Config.TelegramBotToken",
			parser:     "optional string",
			validation: "all-or-none with TOKENIO_TELEGRAM_CHAT_ID",
			behavior:   "config_test.go: Telegram all-or-none coverage",
		},
		"TOKENIO_TELEGRAM_CHAT_ID": {
			field:      "Config.TelegramChatID",
			parser:     "optional string",
			validation: "all-or-none with TOKENIO_TELEGRAM_BOT_TOKEN",
			behavior:   "config_test.go: Telegram all-or-none coverage",
		},
		"TOKENIO_TELEGRAM_DELIVERY_BATCH_SIZE": {
			field:      "Config.TelegramDeliveryBatchSize",
			parser:     "int with default 100",
			validation: "must be >= 1",
			behavior:   "worker Telegram delivery tests plus config audit evidence",
		},
		"TOKENIO_TELEGRAM_DELIVERY_INTERVAL": {
			field:      "Config.TelegramDeliveryInterval",
			parser:     "duration with default 1m",
			validation: "positive duration",
			behavior:   "worker Telegram delivery tests plus config audit evidence",
		},
		"TOKENIO_TELEGRAM_FAILED_RETRY_BATCH_SIZE": {
			field:      "Config.TelegramFailedRetryBatchSize",
			parser:     "int with default 100",
			validation: "must be >= 1",
			behavior:   "worker Telegram failed-retry tests plus config audit evidence",
		},
		"TOKENIO_TELEGRAM_FAILED_RETRY_INTERVAL": {
			field:      "Config.TelegramFailedRetryInterval",
			parser:     "duration with default 1m",
			validation: "positive duration",
			behavior:   "worker Telegram failed-retry tests plus config audit evidence",
		},
		"TOKENIO_TELEGRAM_STALE_ATTEMPT_RECOVERY_BATCH_SIZE": {
			field:      "Config.TelegramStaleAttemptRecoveryBatchSize",
			parser:     "int with default 100",
			validation: "must be >= 1",
			behavior:   "worker Telegram stale recovery tests plus config audit evidence",
		},
		"TOKENIO_TELEGRAM_STALE_ATTEMPT_RECOVERY_INTERVAL": {
			field:      "Config.TelegramStaleAttemptRecoveryInterval",
			parser:     "duration with default 1m",
			validation: "positive duration",
			behavior:   "worker Telegram stale recovery tests plus config audit evidence",
		},
		"TOKENIO_TELEGRAM_STALE_ATTEMPT_RECOVERY_STALE_AFTER": {
			field:      "Config.TelegramStaleAttemptRecoveryStaleAfter",
			parser:     "duration with default 5m",
			validation: "positive duration",
			behavior:   "worker Telegram stale recovery tests plus config audit evidence",
		},
		"TOKENIO_TOKEN_ESTIMATION_SAFETY_FACTOR": {
			field:      "Config.TokenEstimationSafetyFactor",
			parser:     "float64 with default 1.25",
			validation: "must be >= 1",
			behavior:   "config_test.go: numeric defaults and invalid range coverage",
		},
		"TOKENIO_UPSTREAM_MAX_ATTEMPTS": {
			field:      "Config.UpstreamMaxAttempts",
			parser:     "int with default 3",
			validation: "must be >= 1",
			behavior:   "config_test.go: upstream retry config coverage",
		},
		"TOKENIO_UPSTREAM_MAX_BACKOFF": {
			field:      "Config.UpstreamMaxBackoff",
			parser:     "duration with default 2s",
			validation: "positive duration",
			behavior:   "config_test.go: upstream retry config coverage",
		},
		"TOKENIO_UPSTREAM_RESPONSE_BODY_MAX_BYTES": {
			field:      "Config.UpstreamResponseBodyMaxBytes",
			parser:     "int64 with default 64MiB",
			validation: "positive",
			behavior:   "config_test.go: TestLoadUpstreamResponseBodyMaxBytes, TestLoadRejectsInvalidUpstreamResponseBodyMaxBytes",
		},
		"TOKENIO_UPSTREAM_TIMEOUT": {
			field:      "Config.UpstreamTimeout",
			parser:     "duration with default 90s",
			validation: "positive duration",
			behavior:   "config_test.go: upstream timeout coverage",
		},
	}
}
