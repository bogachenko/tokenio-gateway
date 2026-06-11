#!/usr/bin/env python3
from __future__ import annotations

import argparse
import difflib
import subprocess
from pathlib import Path

EXPECTED_MODULE = "module github.com/bogachenko/tokenio-gateway"

PATCH_PATHS = [
    "AGENTS.md",
    "docs/spec/070-database-schema.ru.md",
    "internal/domain/domain.go",
    "internal/config/config.go",
    "internal/forwarding/ratelimiter.go",
    "cmd/gateway/main.go",
]

CONFIG_GO = r'''package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment string

	GatewayAddr string
	DatabaseDSN string

	BillingBaseURL       string
	BillingServiceToken  string
	BillingJWTSigningKey string
	BillingJWTTTL        time.Duration
	BillingTimeout       time.Duration

	AdminToken       string
	APIKeyHashSecret string

	CostCurrency                string
	AutoChargeThresholdCents    int64
	MinChargeAmountCents        int64
	MinRequestBalanceCents      int64
	TokenEstimationSafetyFactor float64
	CostEstimationSafetyFactor  float64
	RequestBodyMaxBytes         int64

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

		BillingBaseURL:       l.required("TOKENIO_BILLING_BASE_URL"),
		BillingServiceToken:  l.required("TOKENIO_BILLING_SERVICE_TOKEN"),
		BillingJWTSigningKey: l.required("TOKENIO_BILLING_JWT_SIGNING_KEY"),
		BillingJWTTTL:        l.duration("TOKENIO_BILLING_JWT_TTL", 15*time.Minute),
		BillingTimeout:       l.duration("TOKENIO_BILLING_TIMEOUT", 30*time.Second),

		AdminToken:       l.required("TOKENIO_ADMIN_TOKEN"),
		APIKeyHashSecret: env("TOKENIO_API_KEY_HASH_SECRET", ""),

		CostCurrency:                env("TOKENIO_COST_CURRENCY", "RUB"),
		AutoChargeThresholdCents:    l.int64("TOKENIO_AUTO_CHARGE_THRESHOLD_CENTS", 1000),
		MinChargeAmountCents:        l.int64("TOKENIO_MIN_CHARGE_AMOUNT_CENTS", 100),
		MinRequestBalanceCents:      l.int64("TOKENIO_MIN_REQUEST_BALANCE_CENTS", 500),
		TokenEstimationSafetyFactor: l.float64("TOKENIO_TOKEN_ESTIMATION_SAFETY_FACTOR", 1.25),
		CostEstimationSafetyFactor:  l.float64("TOKENIO_COST_ESTIMATION_SAFETY_FACTOR", 1.10),
		RequestBodyMaxBytes:         l.int64("TOKENIO_REQUEST_BODY_MAX_BYTES", 64<<20),

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
	if cfg.BillingJWTTTL <= 0 {
		return fmt.Errorf("TOKENIO_BILLING_JWT_TTL must be positive")
	}
	if cfg.BillingTimeout <= 0 {
		return fmt.Errorf("TOKENIO_BILLING_TIMEOUT must be positive")
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
	if !oneOf(cfg.LogLevel, "debug", "info", "warn", "error") {
		return fmt.Errorf("TOKENIO_LOG_LEVEL must be one of debug, info, warn, error")
	}
	if !oneOf(cfg.LogFormat, "text", "json") {
		return fmt.Errorf("TOKENIO_LOG_FORMAT must be text or json")
	}
	if cfg.Environment == "production" {
		if cfg.APIKeyHashSecret == "" {
			return fmt.Errorf("TOKENIO_API_KEY_HASH_SECRET is required in production")
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
'''

def run(cmd: list[str], repo: Path, check: bool = True) -> int:
    print("$ " + " ".join(cmd))
    proc = subprocess.run(cmd, cwd=repo)
    if check and proc.returncode != 0:
        raise SystemExit(proc.returncode)
    return proc.returncode

def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")

def write_if_changed(path: Path, content: str) -> bool:
    old = read(path) if path.exists() else ""
    if old == content:
        return False
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")
    return True

def unified_diff(path: Path, old: str, new: str) -> str:
    return "".join(difflib.unified_diff(
        old.splitlines(keepends=True),
        new.splitlines(keepends=True),
        fromfile=str(path),
        tofile=str(path),
    ))

def require_repo(repo: Path) -> None:
    go_mod = repo / "go.mod"
    if not go_mod.exists():
        raise SystemExit(f"ERROR: go.mod not found in {repo}")
    if EXPECTED_MODULE not in read(go_mod):
        raise SystemExit(f"ERROR: expected {EXPECTED_MODULE!r} in {go_mod}")

def patch_agents(repo: Path) -> tuple[Path, str, str]:
    path = repo / "AGENTS.md"
    old = read(path)
    new = old
    hydra_line = "- Hydra is not the core architecture. Hydra is only one possible `provider_type` / reseller route."
    replacement = (
        "- Provider-specific behavior must not be implemented in generic gateway layers.\n"
        "- Every upstream provider is represented through `provider_type`, `reseller`, `route`, "
        "`api_family`, `endpoint_kind` and `capabilities`."
    )
    if hydra_line in new:
        new = new.replace(hydra_line, replacement)
    if "Provider-specific behavior must not be implemented in generic gateway layers." not in new:
        marker = "## Non-negotiable invariants\n\n"
        if marker not in new:
            raise SystemExit("ERROR: AGENTS.md does not contain expected Non-negotiable invariants section")
        new = new.replace(marker, marker + replacement + "\n", 1)
    if new.count("```") % 2 != 0:
        new = new.rstrip() + "\n```\n"
    if not new.endswith("\n"):
        new += "\n"
    write_if_changed(path, new)
    return path, old, new

def patch_schema(repo: Path) -> tuple[Path, str, str]:
    path = repo / "docs/spec/070-database-schema.ru.md"
    old = read(path)
    new = old.replace("    CHECK (balance_cents >= 0 OR balance_cents < 0),\n", "")
    if not new.endswith("\n"):
        new += "\n"
    write_if_changed(path, new)
    return path, old, new

def patch_domain(repo: Path) -> tuple[Path, str, str]:
    path = repo / "internal/domain/domain.go"
    old = read(path)
    new = old
    if "EndpointModels" not in new:
        needle = (
            'const (\n'
            '\tEndpointChat             EndpointKind = "chat"\n'
            '\tEndpointEmbeddings       EndpointKind = "embeddings"\n'
            '\tEndpointImagesGeneration EndpointKind = "images_generation"\n'
            ')'
        )
        replacement = (
            'const (\n'
            '\tEndpointChat             EndpointKind = "chat"\n'
            '\tEndpointEmbeddings       EndpointKind = "embeddings"\n'
            '\tEndpointImagesGeneration EndpointKind = "images_generation"\n'
            '\tEndpointModels           EndpointKind = "models"\n'
            '\tEndpointHealth           EndpointKind = "health"\n'
            ')'
        )
        if needle not in new:
            raise SystemExit("ERROR: endpoint const block not found in internal/domain/domain.go")
        new = new.replace(needle, replacement, 1)
    write_if_changed(path, new)
    return path, old, new

def patch_config(repo: Path) -> tuple[Path, str, str]:
    path = repo / "internal/config/config.go"
    old = read(path)
    new = CONFIG_GO
    write_if_changed(path, new)
    return path, old, new

def patch_ratelimiter(repo: Path) -> tuple[Path, str, str]:
    path = repo / "internal/forwarding/ratelimiter.go"
    old = read(path)
    new = old
    if "disabled bool" not in new:
        new = new.replace(
            "\tmaxWait time.Duration\n\tlimit   rate.Limit\n\tburst   int\n",
            "\tmaxWait  time.Duration\n\tlimit    rate.Limit\n\tburst    int\n\tdisabled bool\n",
            1,
        )
    old_ctor = '''func NewRateLimiter(rpm int, burst int, maxWait time.Duration) *RateLimiter {
	return &RateLimiter{
		maxWait:  maxWait,
		limit:    rate.Every(time.Minute / time.Duration(rpm)),
		burst:    burst,
		limiters: map[string]*rate.Limiter{},
	}
}
'''
    new_ctor = '''func NewRateLimiter(rpm int, burst int, maxWait time.Duration) *RateLimiter {
	if rpm <= 0 || burst <= 0 {
		return &RateLimiter{
			maxWait:  maxWait,
			disabled: true,
			limiters: map[string]*rate.Limiter{},
		}
	}
	return &RateLimiter{
		maxWait:  maxWait,
		limit:    rate.Every(time.Minute / time.Duration(rpm)),
		burst:    burst,
		limiters: map[string]*rate.Limiter{},
	}
}
'''
    if old_ctor in new:
        new = new.replace(old_ctor, new_ctor, 1)
    if "if r == nil || r.disabled" not in new:
        new = new.replace(
            "func (r *RateLimiter) Wait(ctx context.Context, model string) error {\n",
            "func (r *RateLimiter) Wait(ctx context.Context, model string) error {\n\tif r == nil || r.disabled {\n\t\treturn nil\n\t}\n",
            1,
        )
    write_if_changed(path, new)
    return path, old, new

def patch_main(repo: Path) -> tuple[Path, str, str]:
    path = repo / "cmd/gateway/main.go"
    old = read(path)
    new = old
    old_server = '''server := &http.Server{
		Addr:              cfg.GatewayAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
'''
    new_server = '''server := &http.Server{
		Addr:              cfg.GatewayAddr,
		Handler:           mux,
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		ReadTimeout:       cfg.HTTPReadTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
	}
'''
    if old_server in new:
        new = new.replace(old_server, new_server, 1)
        new = new.replace('\n\t"time"\n', "\n", 1)
    write_if_changed(path, new)
    return path, old, new

def print_patch_preview(changes: list[tuple[Path, str, str]], repo: Path) -> None:
    print("\n=== patch preview ===")
    any_change = False
    for path, old, new in changes:
        if old != new:
            any_change = True
            print(unified_diff(path.relative_to(repo), old, new))
    if not any_change:
        print("No file content changes needed.")

def verification(repo: Path) -> None:
    print("\n=== verification grep ===")
    commands = [
        ["grep", "-R", "Hydra is not the core architecture", "-n", "AGENTS.md"],
        ["grep", "-R", "Provider-specific behavior must not be implemented", "-n", "AGENTS.md"],
        ["grep", "-R", "CHECK (balance_cents >= 0 OR balance_cents < 0)", "-n", "docs/spec/070-database-schema.ru.md"],
        ["grep", "-R", "EndpointModels\\|EndpointHealth", "-n", "internal/domain/domain.go"],
        ["grep", "-R", "panic(", "-n", "internal/config/config.go"],
        ["grep", "-R", "TOKENIO_ADMIN_TOKEN\\|TOKENIO_ENV\\|TOKENIO_HTTP_READ_TIMEOUT", "-n", "internal/config/config.go"],
        ["grep", "-R", "disabled bool\\|rpm <= 0\\|r.disabled", "-n", "internal/forwarding/ratelimiter.go"],
        ["grep", "-R", "ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout", "-n", "cmd/gateway/main.go"],
    ]
    for cmd in commands:
        print("$ " + " ".join(cmd) + " || true")
        subprocess.run(cmd, cwd=repo)

def main() -> None:
    parser = argparse.ArgumentParser(description="Align tokenio-gateway foundation code/spec with specs.")
    parser.add_argument("--repo", default=".", help="Path to tokenio-gateway repo. Default: current directory.")
    parser.add_argument("--skip-tests", action="store_true", help="Skip go test ./...")
    args = parser.parse_args()

    repo = Path(args.repo).expanduser().resolve()
    require_repo(repo)

    before = {p: read(repo / p) for p in PATCH_PATHS if (repo / p).exists()}

    changes = [
        patch_agents(repo),
        patch_schema(repo),
        patch_domain(repo),
        patch_config(repo),
        patch_ratelimiter(repo),
        patch_main(repo),
    ]

    print_patch_preview(changes, repo)

    run(["gofmt", "-w", "cmd/gateway/main.go", "internal/domain/domain.go", "internal/config/config.go", "internal/forwarding/ratelimiter.go"], repo)

    print("\n=== git diff ===")
    run(["git", "diff", "--", *PATCH_PATHS], repo, check=False)

    verification(repo)

    if not args.skip_tests:
        print("\n=== tests ===")
        run(["go", "test", "./..."], repo)

    after = {p: read(repo / p) for p in PATCH_PATHS if (repo / p).exists()}
    touched = [p for p in PATCH_PATHS if before.get(p) != after.get(p)]
    print("\n=== touched files ===")
    if touched:
        for p in touched:
            print(p)
    else:
        print("No changes.")

if __name__ == "__main__":
    main()
