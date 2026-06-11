package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	GatewayAddr                 string
	DatabaseDSN                 string
	BillingBaseURL              string
	BillingServiceToken         string
	BillingJWTSigningKey        string
	CostCurrency                string
	AutoChargeThresholdCents    int64
	MinChargeAmountCents        int64
	MinRequestBalanceCents      int64
	TokenEstimationSafetyFactor float64
	CostEstimationSafetyFactor  float64
	RequestBodyMaxBytes         int64
	TelegramBotToken            string
	TelegramChatID              string
	ResellerBalanceAlertCents   int64
	CooldownRateLimit           time.Duration
	CooldownQuotaExceeded       time.Duration
	Cooldown5XX                 time.Duration
	CooldownTimeout             time.Duration
	CooldownAuthError           time.Duration
	BillingTimeout              time.Duration
	UpstreamTimeout             time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		GatewayAddr:                 env("TOKENIO_GATEWAY_ADDR", ":8880"),
		DatabaseDSN:                 requiredEnv("TOKENIO_DATABASE_DSN"),
		BillingBaseURL:              requiredEnv("TOKENIO_BILLING_BASE_URL"),
		BillingServiceToken:         requiredEnv("TOKENIO_BILLING_SERVICE_TOKEN"),
		BillingJWTSigningKey:        requiredEnv("TOKENIO_BILLING_JWT_SIGNING_KEY"),
		CostCurrency:                env("TOKENIO_COST_CURRENCY", "RUB"),
		AutoChargeThresholdCents:    envInt64("TOKENIO_AUTO_CHARGE_THRESHOLD_CENTS", 1000),
		MinChargeAmountCents:        envInt64("TOKENIO_MIN_CHARGE_AMOUNT_CENTS", 100),
		MinRequestBalanceCents:      envInt64("TOKENIO_MIN_REQUEST_BALANCE_CENTS", 500),
		TokenEstimationSafetyFactor: envFloat("TOKENIO_TOKEN_ESTIMATION_SAFETY_FACTOR", 1.25),
		CostEstimationSafetyFactor:  envFloat("TOKENIO_COST_ESTIMATION_SAFETY_FACTOR", 1.10),
		RequestBodyMaxBytes:         envInt64("TOKENIO_REQUEST_BODY_MAX_BYTES", 64<<20),
		TelegramBotToken:            env("TOKENIO_TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID:              env("TOKENIO_TELEGRAM_CHAT_ID", ""),
		ResellerBalanceAlertCents:   envInt64("TOKENIO_RESELLER_BALANCE_ALERT_CENTS", 10000),
		CooldownRateLimit:           envDuration("TOKENIO_ROUTE_COOLDOWN_RATE_LIMIT", time.Minute),
		CooldownQuotaExceeded:       envDuration("TOKENIO_ROUTE_COOLDOWN_QUOTA_EXCEEDED", 24*time.Hour),
		Cooldown5XX:                 envDuration("TOKENIO_ROUTE_COOLDOWN_5XX", 30*time.Second),
		CooldownTimeout:             envDuration("TOKENIO_ROUTE_COOLDOWN_TIMEOUT", 30*time.Second),
		CooldownAuthError:           envDuration("TOKENIO_ROUTE_COOLDOWN_AUTH_ERROR", 24*time.Hour),
		BillingTimeout:              envDuration("TOKENIO_BILLING_TIMEOUT", 30*time.Second),
		UpstreamTimeout:             envDuration("TOKENIO_UPSTREAM_TIMEOUT", 90*time.Second),
	}

	if cfg.CostCurrency != "RUB" {
		return Config{}, fmt.Errorf("TOKENIO_COST_CURRENCY must be RUB")
	}
	if cfg.AutoChargeThresholdCents <= 0 {
		return Config{}, fmt.Errorf("TOKENIO_AUTO_CHARGE_THRESHOLD_CENTS must be positive")
	}
	if cfg.MinChargeAmountCents < 0 {
		return Config{}, fmt.Errorf("TOKENIO_MIN_CHARGE_AMOUNT_CENTS must be non-negative")
	}
	if cfg.MinRequestBalanceCents < 0 {
		return Config{}, fmt.Errorf("TOKENIO_MIN_REQUEST_BALANCE_CENTS must be non-negative")
	}
	if cfg.TokenEstimationSafetyFactor < 1 {
		return Config{}, fmt.Errorf("TOKENIO_TOKEN_ESTIMATION_SAFETY_FACTOR must be >= 1")
	}
	if cfg.CostEstimationSafetyFactor < 1 {
		return Config{}, fmt.Errorf("TOKENIO_COST_ESTIMATION_SAFETY_FACTOR must be >= 1")
	}
	if cfg.RequestBodyMaxBytes <= 0 {
		return Config{}, fmt.Errorf("TOKENIO_REQUEST_BODY_MAX_BYTES must be positive")
	}
	if cfg.BillingTimeout <= 0 {
		return Config{}, fmt.Errorf("TOKENIO_BILLING_TIMEOUT must be positive")
	}
	if cfg.UpstreamTimeout <= 0 {
		return Config{}, fmt.Errorf("TOKENIO_UPSTREAM_TIMEOUT must be positive")
	}

	return cfg, nil
}

func requiredEnv(key string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		panic(fmt.Sprintf("%s is required", key))
	}
	return value
}

func env(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envInt64(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		panic(fmt.Sprintf("%s must be int64: %v", key, err))
	}
	return parsed
}

func envFloat(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		panic(fmt.Sprintf("%s must be float64: %v", key, err))
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		panic(fmt.Sprintf("%s must be duration: %v", key, err))
	}
	return parsed
}
