package config

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment string

	GatewayAddr string
	DatabaseDSN string

	BillingBaseURL           string
	BillingServiceToken      string
	BillingJWTSigningKey     string
	BillingJWTTTL            time.Duration
	BillingTimeout           time.Duration
	BillingRecoveryInterval  time.Duration
	BillingRecoveryBatchSize int

	AdminToken            string
	APIKeyHashSecret      string
	APIKeyLastUsedTimeout time.Duration

	ProvisioningServiceToken              string
	APIKeyProvisioningEncryptionKey       []byte
	APIKeyProvisioningKeyVersion          string
	APIKeyProvisioningTTL                 time.Duration
	APIKeyProvisioningExpirationInterval  time.Duration
	APIKeyProvisioningExpirationBatchSize int

	ForwardingAttemptRecoveryStaleAfter time.Duration
	ForwardingAttemptRecoveryInterval   time.Duration
	ForwardingAttemptRecoveryBatchSize  int

	TelegramStaleAttemptRecoveryStaleAfter time.Duration
	TelegramStaleAttemptRecoveryInterval   time.Duration
	TelegramStaleAttemptRecoveryBatchSize  int
	TelegramDeliveryInterval               time.Duration
	TelegramDeliveryBatchSize              int

	CostCurrency                 string
	AutoChargeThresholdCents     int64
	MinChargeAmountCents         int64
	MinRequestBalanceCents       int64
	TokenEstimationSafetyFactor  float64
	CostEstimationSafetyFactor   float64
	RequestBodyMaxBytes          int64
	UpstreamResponseBodyMaxBytes int64

	TelegramBotToken          string
	TelegramChatID            string
	TelegramAlertDedupePeriod time.Duration
	ResellerBalanceAlertCents int64

	CooldownRateLimit     time.Duration
	CooldownQuotaExceeded time.Duration
	Cooldown5XX           time.Duration
	CooldownTimeout       time.Duration
	CooldownAuthError     time.Duration

	UpstreamTimeout     time.Duration
	UpstreamMaxAttempts int
	UpstreamMaxBackoff  time.Duration

	RateLimitMaxWait time.Duration

	HTTPReadHeaderTimeout time.Duration
	HTTPReadTimeout       time.Duration
	HTTPWriteTimeout      time.Duration
	HTTPIdleTimeout       time.Duration
	HTTPShutdownTimeout   time.Duration

	LogLevel  string
	LogFormat string
	LogBodies bool
}

func Load() (Config, error) {
	l := envLoader{}

	cfg := Config{
		Environment: env("TOKENIO_ENV", "production"),

		GatewayAddr: env("TOKENIO_GATEWAY_ADDR", ":8880"),
		DatabaseDSN: l.required("TOKENIO_DATABASE_DSN"),

		BillingBaseURL:       l.requiredAbsoluteURL("TOKENIO_BILLING_BASE_URL"),
		BillingServiceToken:  l.required("TOKENIO_BILLING_SERVICE_TOKEN"),
		BillingJWTSigningKey: l.required("TOKENIO_BILLING_JWT_SIGNING_KEY"),
		BillingJWTTTL:        l.duration("TOKENIO_BILLING_JWT_TTL", 15*time.Minute),
		BillingTimeout:       l.duration("TOKENIO_BILLING_TIMEOUT", 30*time.Second),
		BillingRecoveryInterval: l.duration(
			"TOKENIO_BILLING_RECOVERY_INTERVAL",
			time.Minute,
		),
		BillingRecoveryBatchSize: l.int(
			"TOKENIO_BILLING_RECOVERY_BATCH_SIZE",
			100,
		),

		AdminToken:            l.required("TOKENIO_ADMIN_TOKEN"),
		APIKeyHashSecret:      l.required("TOKENIO_API_KEY_HASH_SECRET"),
		APIKeyLastUsedTimeout: l.duration("TOKENIO_API_KEY_LAST_USED_TIMEOUT", 250*time.Millisecond),

		ProvisioningServiceToken:        env("TOKENIO_PROVISIONING_SERVICE_TOKEN", ""),
		APIKeyProvisioningEncryptionKey: l.optionalBase64Bytes("TOKENIO_API_KEY_PROVISIONING_ENCRYPTION_KEY", 32),
		APIKeyProvisioningKeyVersion:    env("TOKENIO_API_KEY_PROVISIONING_KEY_VERSION", "v1"),
		APIKeyProvisioningTTL:           l.duration("TOKENIO_API_KEY_PROVISIONING_TTL", 24*time.Hour),
		APIKeyProvisioningExpirationInterval: l.duration(
			"TOKENIO_API_KEY_PROVISIONING_EXPIRATION_INTERVAL",
			time.Minute,
		),
		APIKeyProvisioningExpirationBatchSize: l.int(
			"TOKENIO_API_KEY_PROVISIONING_EXPIRATION_BATCH_SIZE",
			100,
		),

		ForwardingAttemptRecoveryStaleAfter: l.duration(
			"TOKENIO_FORWARDING_ATTEMPT_RECOVERY_STALE_AFTER",
			5*time.Minute,
		),
		ForwardingAttemptRecoveryInterval: l.duration(
			"TOKENIO_FORWARDING_ATTEMPT_RECOVERY_INTERVAL",
			time.Minute,
		),
		ForwardingAttemptRecoveryBatchSize: l.int(
			"TOKENIO_FORWARDING_ATTEMPT_RECOVERY_BATCH_SIZE",
			100,
		),
		TelegramStaleAttemptRecoveryStaleAfter: l.duration(
			"TOKENIO_TELEGRAM_STALE_ATTEMPT_RECOVERY_STALE_AFTER",
			5*time.Minute,
		),
		TelegramStaleAttemptRecoveryInterval: l.duration(
			"TOKENIO_TELEGRAM_STALE_ATTEMPT_RECOVERY_INTERVAL",
			time.Minute,
		),
		TelegramStaleAttemptRecoveryBatchSize: l.int(
			"TOKENIO_TELEGRAM_STALE_ATTEMPT_RECOVERY_BATCH_SIZE",
			100,
		),
		TelegramDeliveryInterval: l.duration(
			"TOKENIO_TELEGRAM_DELIVERY_INTERVAL",
			time.Minute,
		),
		TelegramDeliveryBatchSize: l.int(
			"TOKENIO_TELEGRAM_DELIVERY_BATCH_SIZE",
			100,
		),

		CostCurrency:                env("TOKENIO_COST_CURRENCY", "RUB"),
		AutoChargeThresholdCents:    l.int64("TOKENIO_AUTO_CHARGE_THRESHOLD_CENTS", 1000),
		MinChargeAmountCents:        l.int64("TOKENIO_MIN_CHARGE_AMOUNT_CENTS", 100),
		MinRequestBalanceCents:      l.int64("TOKENIO_MIN_REQUEST_BALANCE_CENTS", 500),
		TokenEstimationSafetyFactor: l.float64("TOKENIO_TOKEN_ESTIMATION_SAFETY_FACTOR", 1.25),
		CostEstimationSafetyFactor:  l.float64("TOKENIO_COST_ESTIMATION_SAFETY_FACTOR", 1.10),
		RequestBodyMaxBytes: l.int64(
			"TOKENIO_REQUEST_BODY_MAX_BYTES",
			64<<20,
		),
		UpstreamResponseBodyMaxBytes: l.int64(
			"TOKENIO_UPSTREAM_RESPONSE_BODY_MAX_BYTES",
			64<<20,
		),

		TelegramBotToken:          env("TOKENIO_TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID:            env("TOKENIO_TELEGRAM_CHAT_ID", ""),
		TelegramAlertDedupePeriod: l.duration("TOKENIO_TELEGRAM_ALERT_DEDUPE_INTERVAL", time.Hour),
		ResellerBalanceAlertCents: l.int64("TOKENIO_RESELLER_BALANCE_ALERT_CENTS", 10000),

		CooldownRateLimit:     l.duration("TOKENIO_ROUTE_COOLDOWN_RATE_LIMIT", time.Minute),
		CooldownQuotaExceeded: l.duration("TOKENIO_ROUTE_COOLDOWN_QUOTA_EXCEEDED", 24*time.Hour),
		Cooldown5XX:           l.duration("TOKENIO_ROUTE_COOLDOWN_5XX", 30*time.Second),
		CooldownTimeout:       l.duration("TOKENIO_ROUTE_COOLDOWN_TIMEOUT", 30*time.Second),
		CooldownAuthError:     l.duration("TOKENIO_ROUTE_COOLDOWN_AUTH_ERROR", 24*time.Hour),

		UpstreamTimeout:     l.duration("TOKENIO_UPSTREAM_TIMEOUT", 90*time.Second),
		UpstreamMaxAttempts: l.int("TOKENIO_UPSTREAM_MAX_ATTEMPTS", 3),
		UpstreamMaxBackoff:  l.duration("TOKENIO_UPSTREAM_MAX_BACKOFF", 2*time.Second),

		RateLimitMaxWait: l.duration("TOKENIO_RATE_LIMIT_MAX_WAIT", 5*time.Second),

		HTTPReadHeaderTimeout: l.duration("TOKENIO_HTTP_READ_HEADER_TIMEOUT", 10*time.Second),
		HTTPReadTimeout:       l.duration("TOKENIO_HTTP_READ_TIMEOUT", 120*time.Second),
		HTTPWriteTimeout:      l.duration("TOKENIO_HTTP_WRITE_TIMEOUT", 120*time.Second),
		HTTPIdleTimeout:       l.duration("TOKENIO_HTTP_IDLE_TIMEOUT", 60*time.Second),
		HTTPShutdownTimeout:   l.duration("TOKENIO_HTTP_SHUTDOWN_TIMEOUT", 30*time.Second),

		LogLevel:  env("TOKENIO_LOG_LEVEL", "info"),
		LogFormat: env("TOKENIO_LOG_FORMAT", "text"),
		LogBodies: l.bool("TOKENIO_LOG_BODIES", false),
	}

	if len(l.errs) > 0 {
		return Config{}, fmt.Errorf("invalid config: %s", strings.Join(l.errs, "; "))
	}
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validate(cfg Config) error {
	if !oneOf(cfg.Environment, "production", "development", "test") {
		return fmt.Errorf("TOKENIO_ENV must be one of production, development, test")
	}
	if strings.TrimSpace(cfg.GatewayAddr) == "" {
		return fmt.Errorf("TOKENIO_GATEWAY_ADDR must be non-empty")
	}
	if cfg.CostCurrency != "RUB" {
		return fmt.Errorf("TOKENIO_COST_CURRENCY must be RUB")
	}
	if cfg.AutoChargeThresholdCents <= 0 {
		return fmt.Errorf("TOKENIO_AUTO_CHARGE_THRESHOLD_CENTS must be positive")
	}
	if cfg.MinChargeAmountCents < 0 {
		return fmt.Errorf("TOKENIO_MIN_CHARGE_AMOUNT_CENTS must be non-negative")
	}
	if cfg.MinRequestBalanceCents < 0 {
		return fmt.Errorf("TOKENIO_MIN_REQUEST_BALANCE_CENTS must be non-negative")
	}
	if cfg.TokenEstimationSafetyFactor < 1 {
		return fmt.Errorf("TOKENIO_TOKEN_ESTIMATION_SAFETY_FACTOR must be >= 1")
	}
	if cfg.CostEstimationSafetyFactor < 1 {
		return fmt.Errorf("TOKENIO_COST_ESTIMATION_SAFETY_FACTOR must be >= 1")
	}
	if cfg.RequestBodyMaxBytes <= 0 {
		return fmt.Errorf("TOKENIO_REQUEST_BODY_MAX_BYTES must be positive")
	}
	if cfg.UpstreamResponseBodyMaxBytes <= 0 {
		return fmt.Errorf(
			"TOKENIO_UPSTREAM_RESPONSE_BODY_MAX_BYTES must be positive",
		)
	}
	if cfg.BillingJWTTTL <= 0 {
		return fmt.Errorf("TOKENIO_BILLING_JWT_TTL must be positive")
	}
	if cfg.BillingTimeout <= 0 {
		return fmt.Errorf("TOKENIO_BILLING_TIMEOUT must be positive")
	}
	if cfg.BillingRecoveryInterval <= 0 {
		return fmt.Errorf(
			"TOKENIO_BILLING_RECOVERY_INTERVAL must be positive",
		)
	}
	if cfg.BillingRecoveryBatchSize < 1 {
		return fmt.Errorf(
			"TOKENIO_BILLING_RECOVERY_BATCH_SIZE must be >= 1",
		)
	}
	if cfg.UpstreamTimeout <= 0 {
		return fmt.Errorf("TOKENIO_UPSTREAM_TIMEOUT must be positive")
	}
	if cfg.UpstreamMaxAttempts < 1 {
		return fmt.Errorf("TOKENIO_UPSTREAM_MAX_ATTEMPTS must be >= 1")
	}
	if cfg.UpstreamMaxBackoff <= 0 {
		return fmt.Errorf("TOKENIO_UPSTREAM_MAX_BACKOFF must be positive")
	}
	if cfg.RateLimitMaxWait <= 0 {
		return fmt.Errorf("TOKENIO_RATE_LIMIT_MAX_WAIT must be positive")
	}
	if cfg.CooldownRateLimit <= 0 {
		return fmt.Errorf("TOKENIO_ROUTE_COOLDOWN_RATE_LIMIT must be positive")
	}
	if cfg.CooldownQuotaExceeded <= 0 {
		return fmt.Errorf("TOKENIO_ROUTE_COOLDOWN_QUOTA_EXCEEDED must be positive")
	}
	if cfg.Cooldown5XX <= 0 {
		return fmt.Errorf("TOKENIO_ROUTE_COOLDOWN_5XX must be positive")
	}
	if cfg.CooldownTimeout <= 0 {
		return fmt.Errorf("TOKENIO_ROUTE_COOLDOWN_TIMEOUT must be positive")
	}
	if cfg.CooldownAuthError <= 0 {
		return fmt.Errorf("TOKENIO_ROUTE_COOLDOWN_AUTH_ERROR must be positive")
	}
	if cfg.TelegramAlertDedupePeriod <= 0 {
		return fmt.Errorf("TOKENIO_TELEGRAM_ALERT_DEDUPE_INTERVAL must be positive")
	}
	if cfg.ResellerBalanceAlertCents < 0 {
		return fmt.Errorf("TOKENIO_RESELLER_BALANCE_ALERT_CENTS must be non-negative")
	}
	if (cfg.TelegramBotToken == "") != (cfg.TelegramChatID == "") {
		return fmt.Errorf("TOKENIO_TELEGRAM_BOT_TOKEN and TOKENIO_TELEGRAM_CHAT_ID must be set together")
	}
	if cfg.HTTPReadHeaderTimeout <= 0 {
		return fmt.Errorf("TOKENIO_HTTP_READ_HEADER_TIMEOUT must be positive")
	}
	if cfg.HTTPReadTimeout <= 0 {
		return fmt.Errorf("TOKENIO_HTTP_READ_TIMEOUT must be positive")
	}
	if cfg.HTTPWriteTimeout <= 0 {
		return fmt.Errorf("TOKENIO_HTTP_WRITE_TIMEOUT must be positive")
	}
	if cfg.HTTPIdleTimeout <= 0 {
		return fmt.Errorf("TOKENIO_HTTP_IDLE_TIMEOUT must be positive")
	}
	if cfg.HTTPShutdownTimeout <= 0 {
		return fmt.Errorf("TOKENIO_HTTP_SHUTDOWN_TIMEOUT must be positive")
	}
	if !oneOf(cfg.LogLevel, "debug", "info", "warn", "error") {
		return fmt.Errorf("TOKENIO_LOG_LEVEL must be one of debug, info, warn, error")
	}
	if !oneOf(cfg.LogFormat, "text", "json") {
		return fmt.Errorf("TOKENIO_LOG_FORMAT must be text or json")
	}
	if strings.TrimSpace(cfg.APIKeyHashSecret) == "" {
		return fmt.Errorf("TOKENIO_API_KEY_HASH_SECRET is required")
	}
	if cfg.APIKeyLastUsedTimeout <= 0 {
		return fmt.Errorf("TOKENIO_API_KEY_LAST_USED_TIMEOUT must be positive")
	}
	if strings.TrimSpace(cfg.APIKeyProvisioningKeyVersion) == "" {
		return fmt.Errorf("TOKENIO_API_KEY_PROVISIONING_KEY_VERSION must be non-empty")
	}
	if cfg.APIKeyProvisioningTTL <= 0 {
		return fmt.Errorf("TOKENIO_API_KEY_PROVISIONING_TTL must be positive")
	}
	if cfg.APIKeyProvisioningExpirationInterval <= 0 {
		return fmt.Errorf(
			"TOKENIO_API_KEY_PROVISIONING_EXPIRATION_INTERVAL must be positive",
		)
	}
	if cfg.APIKeyProvisioningExpirationBatchSize < 1 {
		return fmt.Errorf(
			"TOKENIO_API_KEY_PROVISIONING_EXPIRATION_BATCH_SIZE must be >= 1",
		)
	}
	if cfg.ForwardingAttemptRecoveryStaleAfter <= 0 {
		return fmt.Errorf(
			"TOKENIO_FORWARDING_ATTEMPT_RECOVERY_STALE_AFTER must be positive",
		)
	}
	if cfg.ForwardingAttemptRecoveryInterval <= 0 {
		return fmt.Errorf(
			"TOKENIO_FORWARDING_ATTEMPT_RECOVERY_INTERVAL must be positive",
		)
	}
	if cfg.ForwardingAttemptRecoveryBatchSize < 1 {
		return fmt.Errorf(
			"TOKENIO_FORWARDING_ATTEMPT_RECOVERY_BATCH_SIZE must be >= 1",
		)
	}
	if cfg.TelegramStaleAttemptRecoveryStaleAfter <= 0 {
		return fmt.Errorf(
			"TOKENIO_TELEGRAM_STALE_ATTEMPT_RECOVERY_STALE_AFTER must be positive",
		)
	}
	if cfg.TelegramStaleAttemptRecoveryInterval <= 0 {
		return fmt.Errorf(
			"TOKENIO_TELEGRAM_STALE_ATTEMPT_RECOVERY_INTERVAL must be positive",
		)
	}
	if cfg.TelegramStaleAttemptRecoveryBatchSize < 1 {
		return fmt.Errorf(
			"TOKENIO_TELEGRAM_STALE_ATTEMPT_RECOVERY_BATCH_SIZE must be >= 1",
		)
	}
	if cfg.TelegramDeliveryInterval <= 0 {
		return fmt.Errorf(
			"TOKENIO_TELEGRAM_DELIVERY_INTERVAL must be positive",
		)
	}
	if cfg.TelegramDeliveryBatchSize < 1 {
		return fmt.Errorf(
			"TOKENIO_TELEGRAM_DELIVERY_BATCH_SIZE must be >= 1",
		)
	}
	if keyLength := len(cfg.APIKeyProvisioningEncryptionKey); keyLength != 0 && keyLength != 32 {
		return fmt.Errorf("TOKENIO_API_KEY_PROVISIONING_ENCRYPTION_KEY must decode to exactly 32 bytes")
	}
	if len(cfg.APIKeyProvisioningEncryptionKey) > 0 &&
		bytes.Equal(cfg.APIKeyProvisioningEncryptionKey, []byte(cfg.APIKeyHashSecret)) {
		return fmt.Errorf("TOKENIO_API_KEY_PROVISIONING_ENCRYPTION_KEY and TOKENIO_API_KEY_HASH_SECRET must use different key material")
	}
	if cfg.Environment == "production" {
		if strings.TrimSpace(cfg.ProvisioningServiceToken) == "" {
			return fmt.Errorf("TOKENIO_PROVISIONING_SERVICE_TOKEN is required in production")
		}
		if len(cfg.APIKeyProvisioningEncryptionKey) == 0 {
			return fmt.Errorf("TOKENIO_API_KEY_PROVISIONING_ENCRYPTION_KEY is required in production")
		}
		if len(cfg.AdminToken) < 32 {
			return fmt.Errorf("TOKENIO_ADMIN_TOKEN must be at least 32 characters in production")
		}
		if cfg.LogBodies {
			return fmt.Errorf("TOKENIO_LOG_BODIES=true is forbidden in production")
		}
	}
	return nil
}

type envLoader struct {
	errs []string
}

func (l *envLoader) required(key string) string {
	value := env(key, "")
	if value == "" {
		l.errs = append(l.errs, fmt.Sprintf("%s is required", key))
	}
	return value
}

func (l *envLoader) requiredAbsoluteURL(key string) string {
	rawValue, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(rawValue) == "" {
		l.errs = append(l.errs, fmt.Sprintf("%s is required", key))
		return ""
	}

	value := strings.TrimSpace(rawValue)
	if value != rawValue {
		l.errs = append(l.errs, fmt.Sprintf("%s must not contain leading or trailing whitespace", key))
	}

	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		l.errs = append(l.errs, fmt.Sprintf("%s must be a valid absolute URL", key))
	}
	return value
}

func (l *envLoader) optionalBase64Bytes(key string, expectedLength int) []byte {
	value := env(key, "")
	if value == "" {
		return nil
	}

	decoded, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil {
		l.errs = append(l.errs, fmt.Sprintf("%s must be valid base64", key))
		return nil
	}
	if len(decoded) != expectedLength {
		l.errs = append(
			l.errs,
			fmt.Sprintf("%s must decode to exactly %d bytes", key, expectedLength),
		)
		return nil
	}
	return decoded
}

func (l *envLoader) int(key string, fallback int) int {
	value := env(key, "")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		l.errs = append(l.errs, fmt.Sprintf("%s must be int: %v", key, err))
		return fallback
	}
	return parsed
}

func (l *envLoader) int64(key string, fallback int64) int64 {
	value := env(key, "")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		l.errs = append(l.errs, fmt.Sprintf("%s must be int64: %v", key, err))
		return fallback
	}
	return parsed
}

func (l *envLoader) float64(key string, fallback float64) float64 {
	value := env(key, "")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		l.errs = append(l.errs, fmt.Sprintf("%s must be float64: %v", key, err))
		return fallback
	}
	return parsed
}

func (l *envLoader) duration(key string, fallback time.Duration) time.Duration {
	value := env(key, "")
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		l.errs = append(l.errs, fmt.Sprintf("%s must be duration: %v", key, err))
		return fallback
	}
	return parsed
}

func (l *envLoader) bool(key string, fallback bool) bool {
	value := env(key, "")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		l.errs = append(l.errs, fmt.Sprintf("%s must be bool: %v", key, err))
		return fallback
	}
	return parsed
}

func env(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}
