#!/usr/bin/env python3
from __future__ import annotations

import difflib
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Dict, Iterable, List, Optional, Tuple

TARGET_MODULE = "github.com/bogachenko/tokenio-gateway"
OLD_MODULE = "github.com/lumiforge/hydra-billing-proxy"

DEFAULT_SOURCE_DIR = Path.home() / "src/github.com/bogachenko/hydra-billing-proxy"
DEFAULT_TARGET_DIR = Path.home() / "src/github.com/bogachenko/tokenio-gateway"

SOURCE_DIR = Path(os.environ.get("TOKENIO_SOURCE_HYDRA_PROXY", str(DEFAULT_SOURCE_DIR))).expanduser().resolve()
TARGET_DIR = Path(os.environ.get("TOKENIO_TARGET_GATEWAY", str(DEFAULT_TARGET_DIR))).expanduser().resolve()


class PatchError(RuntimeError):
    pass


class ChangeTracker:
    def __init__(self) -> None:
        self.before: Dict[Path, Optional[str]] = {}
        self.touched: List[Path] = []

    def remember(self, path: Path) -> None:
        path = path.resolve()
        if path not in self.before:
            self.before[path] = path.read_text(encoding="utf-8") if path.exists() else None
            self.touched.append(path)

    def write(self, path: Path, content: str) -> None:
        path = path.resolve()
        self.remember(path)
        path.parent.mkdir(parents=True, exist_ok=True)
        if path.exists() and path.read_text(encoding="utf-8") == content:
            return
        path.write_text(content, encoding="utf-8")

    def print_diff(self) -> None:
        print("\n========== DIFF ==========")
        any_diff = False

        for path in self.touched:
            old = self.before[path]
            new = path.read_text(encoding="utf-8") if path.exists() else None

            if old == new:
                continue

            any_diff = True
            rel = path.relative_to(TARGET_DIR)
            old_lines = [] if old is None else old.splitlines(keepends=True)
            new_lines = [] if new is None else new.splitlines(keepends=True)

            fromfile = f"a/{rel}" if old is not None else "/dev/null"
            tofile = f"b/{rel}" if new is not None else "/dev/null"

            for line in difflib.unified_diff(old_lines, new_lines, fromfile=fromfile, tofile=tofile):
                print(line, end="")

        if not any_diff:
            print("No changes.")


tracker = ChangeTracker()


def run(cmd: List[str], cwd: Path = TARGET_DIR, check: bool = True) -> subprocess.CompletedProcess[str]:
    print(f"\n$ {' '.join(cmd)}")
    result = subprocess.run(
        cmd,
        cwd=str(cwd),
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
    )
    print(result.stdout, end="")
    if check and result.returncode != 0:
        raise PatchError(f"command failed: {' '.join(cmd)}")
    return result


def require_file(path: Path) -> str:
    if not path.exists():
        raise PatchError(f"required file not found: {path}")
    return path.read_text(encoding="utf-8")


def validate_repositories() -> None:
    source_go_mod = SOURCE_DIR / "go.mod"
    target_go_mod = TARGET_DIR / "go.mod"

    if not SOURCE_DIR.exists():
        raise PatchError(f"source repo not found: {SOURCE_DIR}")

    if not TARGET_DIR.exists():
        raise PatchError(f"target repo not found: {TARGET_DIR}")

    source_mod = require_file(source_go_mod)
    target_mod = require_file(target_go_mod)

    if f"module {OLD_MODULE}" not in source_mod:
        raise PatchError(f"source repo is not {OLD_MODULE}: {SOURCE_DIR}")

    if f"module {TARGET_MODULE}" not in target_mod:
        raise PatchError(f"target repo is not {TARGET_MODULE}: {TARGET_DIR}")

    print(f"OK source repo: {SOURCE_DIR}")
    print(f"OK target repo: {TARGET_DIR}")


def copy_transformed(src_rel: str, dst_rel: str, replacements: Iterable[Tuple[str, str]]) -> None:
    src = SOURCE_DIR / src_rel
    dst = TARGET_DIR / dst_rel

    content = require_file(src)
    content = content.replace(OLD_MODULE, TARGET_MODULE)

    for old, new in replacements:
        content = content.replace(old, new)

    tracker.write(dst, content)


def ensure_go_require(module: str, version: str) -> None:
    go_mod_path = TARGET_DIR / "go.mod"
    content = require_file(go_mod_path)

    if re.search(rf"^\s*{re.escape(module)}\s+", content, flags=re.MULTILINE):
        return

    tracker.remember(go_mod_path)

    if "\nrequire (\n" in content:
        content = content.replace("\nrequire (\n", f"\nrequire (\n\t{module} {version}\n", 1)
    else:
        content = content.rstrip() + f"\n\nrequire {module} {version}\n"

    go_mod_path.write_text(content, encoding="utf-8")


def write_domain() -> None:
    tracker.write(
        TARGET_DIR / "internal/domain/domain.go",
        '''package domain

import "time"

type ProviderType string

const (
	ProviderOpenAI     ProviderType = "openai"
	ProviderOpenRouter ProviderType = "openrouter"
	ProviderTogether   ProviderType = "together"
	ProviderGroq       ProviderType = "groq"
	ProviderOllama     ProviderType = "ollama"
	ProviderLMStudio   ProviderType = "lmstudio"
	ProviderVLLM       ProviderType = "vllm"
	ProviderGemini     ProviderType = "gemini"
	ProviderAnthropic  ProviderType = "anthropic"
	ProviderHydra      ProviderType = "hydra"
)

type APIFamily string

const (
	APIFamilyOpenAICompatible APIFamily = "openai_compatible"
	APIFamilyGeminiNative     APIFamily = "gemini_native"
	APIFamilyAnthropicNative  APIFamily = "anthropic_native"
	APIFamilyOllamaNative     APIFamily = "ollama_native"
)

type EndpointKind string

const (
	EndpointChat             EndpointKind = "chat"
	EndpointEmbeddings       EndpointKind = "embeddings"
	EndpointImagesGeneration EndpointKind = "images_generation"
)

type CapabilitySet struct {
	Chat             bool `json:"chat"`
	Embeddings       bool `json:"embeddings"`
	ImagesGeneration bool `json:"images_generation"`
	Tools            bool `json:"tools"`
	ToolChoice       bool `json:"tool_choice"`
	ResponseFormat   bool `json:"response_format"`
	JSONSchema       bool `json:"json_schema"`
	ImageInput       bool `json:"image_input"`
	AudioInput       bool `json:"audio_input"`
	FileInput        bool `json:"file_input"`
	VideoInput       bool `json:"video_input"`
	Reasoning        bool `json:"reasoning"`
}

type Reseller struct {
	ID                  string       `json:"id"`
	Name                string       `json:"name"`
	ProviderType        ProviderType `json:"provider_type"`
	BaseURL             string       `json:"base_url"`
	APIKeyEnv           string       `json:"api_key_env"`
	Enabled             bool         `json:"enabled"`
	BalanceCents        int64        `json:"balance_cents"`
	ReservedCents       int64        `json:"reserved_cents"`
	MinimumBalanceCents int64        `json:"minimum_balance_cents"`
	CreatedAt           time.Time    `json:"created_at"`
	UpdatedAt           time.Time    `json:"updated_at"`
}

type Route struct {
	ID                     string        `json:"id"`
	ResellerID             string        `json:"reseller_id"`
	ProviderType           ProviderType  `json:"provider_type"`
	APIFamily              APIFamily     `json:"api_family"`
	EndpointKind           EndpointKind  `json:"endpoint_kind"`
	ClientModel            string        `json:"client_model"`
	ProviderModel          string        `json:"provider_model"`
	Enabled                bool          `json:"enabled"`
	Priority               int           `json:"priority"`
	RequestsPerMinute      int           `json:"requests_per_minute"`
	TokensPerMinute        int           `json:"tokens_per_minute"`
	ConcurrentRequests     int           `json:"concurrent_requests"`
	DefaultMaxOutputTokens int64         `json:"default_max_output_tokens"`
	Capabilities           CapabilitySet `json:"capabilities"`
	CooldownUntil          *time.Time    `json:"cooldown_until,omitempty"`
	CooldownReason         string        `json:"cooldown_reason,omitempty"`
	CreatedAt              time.Time     `json:"created_at"`
	UpdatedAt              time.Time     `json:"updated_at"`
}

type TokenUsage struct {
	InputTokens        int64 `json:"input_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
	OutputTokens       int64 `json:"output_tokens"`
	ReasoningTokens    int64 `json:"reasoning_tokens"`
	ImageInputTokens   int64 `json:"image_input_tokens"`
	AudioInputTokens   int64 `json:"audio_input_tokens"`
	AudioOutputTokens  int64 `json:"audio_output_tokens"`
	FileInputTokens    int64 `json:"file_input_tokens"`
	VideoInputTokens   int64 `json:"video_input_tokens"`
}

type RoutePrice struct {
	RouteID                              string  `json:"route_id"`
	Currency                             string  `json:"currency"`
	InputPricePer1MTokensCents           int64   `json:"input_price_per_1m_tokens_cents"`
	CachedInputPricePer1MTokensCents     int64   `json:"cached_input_price_per_1m_tokens_cents"`
	OutputPricePer1MTokensCents          int64   `json:"output_price_per_1m_tokens_cents"`
	ReasoningOutputPricePer1MTokensCents int64   `json:"reasoning_output_price_per_1m_tokens_cents"`
	ImageInputPricePer1MTokensCents      int64   `json:"image_input_price_per_1m_tokens_cents"`
	AudioInputPricePer1MTokensCents      int64   `json:"audio_input_price_per_1m_tokens_cents"`
	AudioOutputPricePer1MTokensCents     int64   `json:"audio_output_price_per_1m_tokens_cents"`
	FileInputPricePer1MTokensCents       int64   `json:"file_input_price_per_1m_tokens_cents"`
	VideoInputPricePer1MTokensCents      int64   `json:"video_input_price_per_1m_tokens_cents"`
	MarkupCoefficient                    float64 `json:"markup_coefficient"`
}

type UsageStatus string

const (
	UsageStatusReserved UsageStatus = "reserved"
	UsageStatusReleased UsageStatus = "released"
	UsageStatusBillable UsageStatus = "billable"
	UsageStatusCharged  UsageStatus = "charged"
	UsageStatusFailed   UsageStatus = "failed"
)

type UsageRecord struct {
	LocalRequestID             string       `json:"local_request_id"`
	IdempotencyKey             string       `json:"idempotency_key,omitempty"`
	UserID                     string       `json:"user_id"`
	ClientModel                string       `json:"client_model"`
	BillingModel               string       `json:"billing_model"`
	SelectedResellerID         string       `json:"selected_reseller_id"`
	SelectedRouteID            string       `json:"selected_route_id"`
	ProviderType               ProviderType `json:"provider_type"`
	APIFamily                  APIFamily    `json:"api_family"`
	EndpointKind               EndpointKind `json:"endpoint_kind"`
	ProviderModel              string       `json:"provider_model"`
	Usage                      TokenUsage   `json:"usage"`
	EstimatedClientAmountCents int64        `json:"estimated_client_amount_cents"`
	ClientAmountCents          int64        `json:"client_amount_cents"`
	EstimatedUpstreamCostCents int64        `json:"estimated_upstream_cost_cents"`
	ActualUpstreamCostCents    int64        `json:"actual_upstream_cost_cents"`
	Currency                   string       `json:"currency"`
	Status                     UsageStatus  `json:"status"`
	FailureReason              string       `json:"failure_reason,omitempty"`
	BillingChargeRequestID     string       `json:"billing_charge_request_id,omitempty"`
	CreatedAt                  time.Time    `json:"created_at"`
	BillableAt                 *time.Time   `json:"billable_at,omitempty"`
	ChargedAt                  *time.Time   `json:"charged_at,omitempty"`
}
''',
    )


def write_token_pricing() -> None:
    tracker.write(
        TARGET_DIR / "internal/pricing/token_pricing.go",
        '''package pricing

import (
	"fmt"
	"math"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type TokenCalculator struct {
	Currency                    string
	TokenEstimationSafetyFactor float64
	CostEstimationSafetyFactor  float64
}

type AmountInput struct {
	Usage              domain.TokenUsage
	Price              domain.RoutePrice
	Estimated          bool
	UseMarkup          bool
	UseSafetyFactor    bool
	MultimodalMaxInput bool
}

func (c TokenCalculator) AmountCents(input AmountInput) (int64, error) {
	if input.Price.Currency != "RUB" {
		return 0, fmt.Errorf("unsupported currency: %s", input.Price.Currency)
	}

	markup := input.Price.MarkupCoefficient
	if !input.UseMarkup {
		markup = 1
	}
	if markup <= 0 {
		return 0, fmt.Errorf("markup coefficient must be positive")
	}

	safety := 1.0
	if input.UseSafetyFactor {
		safety = c.CostEstimationSafetyFactor
		if safety <= 0 {
			safety = 1
		}
	}

	usage := input.Usage
	if input.Estimated && c.TokenEstimationSafetyFactor > 1 {
		usage = multiplyUsage(usage, c.TokenEstimationSafetyFactor)
	}

	raw := int64(0)

	if input.MultimodalMaxInput {
		maxInputPrice := maxInt64(
			input.Price.InputPricePer1MTokensCents,
			input.Price.ImageInputPricePer1MTokensCents,
			input.Price.AudioInputPricePer1MTokensCents,
			input.Price.FileInputPricePer1MTokensCents,
			input.Price.VideoInputPricePer1MTokensCents,
		)
		totalInput := usage.InputTokens +
			usage.CachedInputTokens +
			usage.ImageInputTokens +
			usage.AudioInputTokens +
			usage.FileInputTokens +
			usage.VideoInputTokens

		raw += totalInput * maxInputPrice
	} else {
		raw += usage.InputTokens * input.Price.InputPricePer1MTokensCents
		raw += usage.CachedInputTokens * input.Price.CachedInputPricePer1MTokensCents
		raw += usage.ImageInputTokens * input.Price.ImageInputPricePer1MTokensCents
		raw += usage.AudioInputTokens * input.Price.AudioInputPricePer1MTokensCents
		raw += usage.FileInputTokens * input.Price.FileInputPricePer1MTokensCents
		raw += usage.VideoInputTokens * input.Price.VideoInputPricePer1MTokensCents
	}

	raw += usage.OutputTokens * input.Price.OutputPricePer1MTokensCents
	raw += usage.ReasoningTokens * input.Price.ReasoningOutputPricePer1MTokensCents
	raw += usage.AudioOutputTokens * input.Price.AudioOutputPricePer1MTokensCents

	if raw <= 0 {
		return 0, nil
	}

	return int64(math.Ceil((float64(raw) / 1_000_000.0) * markup * safety)), nil
}

func multiplyUsage(usage domain.TokenUsage, factor float64) domain.TokenUsage {
	return domain.TokenUsage{
		InputTokens:        ceilMul(usage.InputTokens, factor),
		CachedInputTokens: ceilMul(usage.CachedInputTokens, factor),
		OutputTokens:       ceilMul(usage.OutputTokens, factor),
		ReasoningTokens:    ceilMul(usage.ReasoningTokens, factor),
		ImageInputTokens:   ceilMul(usage.ImageInputTokens, factor),
		AudioInputTokens:   ceilMul(usage.AudioInputTokens, factor),
		AudioOutputTokens:  ceilMul(usage.AudioOutputTokens, factor),
		FileInputTokens:    ceilMul(usage.FileInputTokens, factor),
		VideoInputTokens:   ceilMul(usage.VideoInputTokens, factor),
	}
}

func ceilMul(value int64, factor float64) int64 {
	if value <= 0 {
		return 0
	}
	return int64(math.Ceil(float64(value) * factor))
}

func maxInt64(values ...int64) int64 {
	max := int64(0)
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}
''',
    )


def write_config() -> None:
    tracker.write(
        TARGET_DIR / "internal/config/config.go",
        '''package config

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
''',
    )


def write_httpapi_foundation() -> None:
    tracker.write(
        TARGET_DIR / "internal/httpapi/foundation.go",
        '''package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

type BillingHeaders struct {
	LocalRequestID              string
	ProviderType                domain.ProviderType
	ClientModel                 string
	BillingModel                string
	InputTokens                 int64
	CachedInputTokens           int64
	OutputTokens                int64
	ReasoningTokens             int64
	ImageInputTokens            int64
	AudioInputTokens            int64
	AudioOutputTokens           int64
	FileInputTokens             int64
	VideoInputTokens            int64
	ClientAmountCents           int64
	Currency                    string
	WalletBalanceCents          int64
	WalletEffectiveBalanceCents int64
	BillingPendingCents         int64
}

func ReadAllLimited(r *http.Request, limit int64) ([]byte, error) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("request body is too large")
	}
	return body, nil
}

func WriteError(w http.ResponseWriter, status int, code string, message string, requestID string) {
	WriteJSON(w, status, ErrorResponse{
		Error: ErrorBody{
			Code:      code,
			Message:   message,
			RequestID: requestID,
		},
	})
}

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func WriteRawResponse(w http.ResponseWriter, status int, header http.Header, body []byte) {
	CopyResponseHeaders(w.Header(), header)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func CopyResponseHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		lower := strings.ToLower(key)
		switch lower {
		case "content-length", "connection", "transfer-encoding", "upgrade", "proxy-authenticate", "proxy-authorization", "te", "trailer":
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func SetBillingHeaders(h http.Header, headers BillingHeaders) {
	setHeader(h, "X-Local-Request-ID", headers.LocalRequestID)
	setHeader(h, "X-Billing-Provider-Type", string(headers.ProviderType))
	setHeader(h, "X-Billing-Client-Model", headers.ClientModel)
	setHeader(h, "X-Billing-Model", headers.BillingModel)
	setHeaderInt(h, "X-Billing-Input-Tokens", headers.InputTokens)
	setHeaderInt(h, "X-Billing-Cached-Input-Tokens", headers.CachedInputTokens)
	setHeaderInt(h, "X-Billing-Output-Tokens", headers.OutputTokens)
	setHeaderInt(h, "X-Billing-Reasoning-Tokens", headers.ReasoningTokens)
	setHeaderInt(h, "X-Billing-Image-Input-Tokens", headers.ImageInputTokens)
	setHeaderInt(h, "X-Billing-Audio-Input-Tokens", headers.AudioInputTokens)
	setHeaderInt(h, "X-Billing-Audio-Output-Tokens", headers.AudioOutputTokens)
	setHeaderInt(h, "X-Billing-File-Input-Tokens", headers.FileInputTokens)
	setHeaderInt(h, "X-Billing-Video-Input-Tokens", headers.VideoInputTokens)
	setHeaderInt(h, "X-Billing-Amount-Cents", headers.ClientAmountCents)
	setHeader(h, "X-Billing-Currency", headers.Currency)
	setHeaderInt(h, "X-Wallet-Balance-Cents", headers.WalletBalanceCents)
	setHeaderInt(h, "X-Wallet-Effective-Balance-Cents", headers.WalletEffectiveBalanceCents)
	setHeaderInt(h, "X-Billing-Pending-Cents", headers.BillingPendingCents)
}

func setHeader(h http.Header, key string, value string) {
	if strings.TrimSpace(value) != "" {
		h.Set(key, value)
	}
}

func setHeaderInt(h http.Header, key string, value int64) {
	h.Set(key, fmt.Sprintf("%d", value))
}
''',
    )


def write_auth_foundation() -> None:
    tracker.write(
        TARGET_DIR / "internal/auth/apikey.go",
        '''package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
)

type APIKeyPrincipal struct {
	UserID     string
	APIKeyID   string
	BillingJWT string
}

type APIKeyAuthenticator interface {
	ValidateAPIKey(rawAPIKey string) (*APIKeyPrincipal, error)
}

func ExtractBearerAPIKey(header string) (string, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", fmt.Errorf("authorization header is required")
	}

	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", fmt.Errorf("authorization header format must be Bearer {api_key}")
	}

	key := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if key == "" {
		return "", fmt.Errorf("bearer api key is empty")
	}
	if !strings.HasPrefix(key, "sk_") {
		return "", fmt.Errorf("api key must start with sk_")
	}

	return key, nil
}

func HashAPIKey(rawAPIKey string) string {
	sum := sha256.Sum256([]byte(rawAPIKey))
	return hex.EncodeToString(sum[:])
}

func ConstantTimeEqualHash(expectedHash string, rawAPIKey string) bool {
	actual := HashAPIKey(rawAPIKey)
	return subtle.ConstantTimeCompare([]byte(expectedHash), []byte(actual)) == 1
}
''',
    )


def write_main() -> None:
    tracker.write(
        TARGET_DIR / "cmd/gateway/main.go",
        '''package main

import (
	"log"
	"net/http"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/config"
	"github.com/bogachenko/tokenio-gateway/internal/httpapi"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpapi.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed", "")
			return
		}
		_, _ = w.Write([]byte("OK"))
	})

	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpapi.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed", "")
			return
		}
		httpapi.WriteJSON(w, http.StatusNotImplemented, map[string]any{
			"error": map[string]any{
				"code":    "not_implemented",
				"message": "model registry is not wired yet",
			},
		})
	})

	server := &http.Server{
		Addr:              cfg.GatewayAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("tokenio-gateway listening on %s", cfg.GatewayAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
''',
    )


def write_readme_and_makefile() -> None:
    tracker.write(
        TARGET_DIR / "README.md",
        '''# tokenio-gateway

LLM billing and reseller routing gateway.

## Role

Tokenio Gateway is a single LLM base URL for clients and agents.

Clients send requests to Tokenio in the API format they already use.
Tokenio does not convert request bodies and does not convert response bodies.
Tokenio authenticates users with API keys, selects the cheapest available compatible reseller route, forwards the request, accounts tokens and cost, stores usage locally, and charges the external billing service automatically.

## External client auth

```http
Authorization: Bearer sk_...
```

The API key is validated by Tokenio and mapped to:

```text
user_id
billing_jwt
```

The billing JWT is internal and is used only for Tokenio -> billing service calls.

## First endpoint surface

```text
GET  /health
GET  /v1/models
POST /v1/chat/completions
POST /v1/embeddings
POST /v1/images/generations
```

## Routing invariant

```text
api_family + endpoint_kind + client_model -> compatible reseller routes
```

Fallback is allowed only between routes with the same API family and endpoint kind.

## Reseller route model

```text
provider_type = openai | openrouter | together | groq | ollama | lmstudio | vllm | gemini | anthropic | hydra
reseller      = concrete account/base_url/api_key_env/balance/limits
route         = reseller sells concrete model through concrete API family
```

## Pricing invariant

Client amount is calculated from the selected route price multiplied by route markup coefficient.

If upstream returns only total input tokens and request contains image/audio/file/video input, Tokenio bills total input tokens using the most expensive applicable input category.

## Billing invariant

Public `/billing/flush` is removed.
Tokenio keeps local pending usage and automatically charges billing when pending amount reaches `TOKENIO_AUTO_CHARGE_THRESHOLD_CENTS`.

## Configuration

```bash
export TOKENIO_GATEWAY_ADDR=':8880'
export TOKENIO_DATABASE_DSN='host=localhost user=postgres password=postgres dbname=tokenio_gateway port=5432 sslmode=disable TimeZone=UTC'
export TOKENIO_BILLING_BASE_URL='https://billing.example.com'
export TOKENIO_BILLING_SERVICE_TOKEN='internal-service-token'
export TOKENIO_BILLING_JWT_SIGNING_KEY='billing-jwt-signing-key'
export TOKENIO_COST_CURRENCY='RUB'
export TOKENIO_AUTO_CHARGE_THRESHOLD_CENTS='1000'
export TOKENIO_MIN_CHARGE_AMOUNT_CENTS='100'
export TOKENIO_MIN_REQUEST_BALANCE_CENTS='500'
export TOKENIO_TOKEN_ESTIMATION_SAFETY_FACTOR='1.25'
export TOKENIO_COST_ESTIMATION_SAFETY_FACTOR='1.10'
export TOKENIO_REQUEST_BODY_MAX_BYTES='67108864'
export TOKENIO_RESELLER_BALANCE_ALERT_CENTS='10000'
export TOKENIO_ROUTE_COOLDOWN_RATE_LIMIT='60s'
export TOKENIO_ROUTE_COOLDOWN_QUOTA_EXCEEDED='24h'
export TOKENIO_ROUTE_COOLDOWN_5XX='30s'
export TOKENIO_ROUTE_COOLDOWN_TIMEOUT='30s'
export TOKENIO_ROUTE_COOLDOWN_AUTH_ERROR='24h'
export TOKENIO_BILLING_TIMEOUT='30s'
export TOKENIO_UPSTREAM_TIMEOUT='90s'
```
''',
    )

    tracker.write(
        TARGET_DIR / "Makefile",
        '''run:
	go run ./cmd/gateway

fmt:
	gofmt -w .

test:
	go test ./...

git:
	git add .
	git commit -a -m "$m"
	git push -u origin main
''',
    )


def copy_reusable_layers() -> None:
    copy_transformed("internal/billing/client.go", "internal/billing/client.go", [])

    copy_transformed("internal/pricing/pricing.go", "internal/pricing/cost_request_pricing.go", [])

    copy_transformed(
        "internal/hydra/client.go",
        "internal/forwarding/client.go",
        [
            ("package hydra", "package forwarding"),
            ("hydra error:", "upstream error:"),
            ("hydra error", "upstream error"),
        ],
    )

    copy_transformed(
        "internal/hydra/ratelimiter.go",
        "internal/forwarding/ratelimiter.go",
        [
            ("package hydra", "package forwarding"),
            (
                "// PURPOSE: local in-memory per-model outbound Hydra limiter with bounded blocking waits.",
                "// PURPOSE: local in-memory per-model outbound route limiter with bounded blocking waits.",
            ),
            ("hydra rate limit wait timeout", "upstream route rate limit wait timeout"),
        ],
    )


def remove_old_hydra_runtime_files_if_present() -> None:
    old_paths = [
        TARGET_DIR / "cmd/proxy",
        TARGET_DIR / "internal/hydra",
        TARGET_DIR / "internal/store/postgres/store.go",
    ]

    for path in old_paths:
        if not path.exists():
            continue
        if path.is_dir():
            for child in sorted(path.rglob("*"), reverse=True):
                tracker.remember(child)
            tracker.remember(path)
            shutil.rmtree(path)
        else:
            tracker.remember(path)
            path.unlink()


def run_gofmt() -> None:
    go_files = [
        str(path.relative_to(TARGET_DIR))
        for path in tracker.touched
        if path.exists() and path.suffix == ".go"
    ]
    if go_files:
        run(["gofmt", "-w", *go_files])


def verification() -> None:
    print("\n========== VERIFICATION GREP ==========")

    run(
        [
            "bash",
            "-lc",
            "grep -R \"github.com/lumiforge/hydra-billing-proxy\\|HYDRA_AI_BASE_URL\\|HYDRA_AI_API_KEY\\|HydraClient\\|/billing/flush\" -n cmd internal README.md go.mod || true",
        ],
        check=False,
    )

    run(
        [
            "bash",
            "-lc",
            "grep -R \"ProviderType\\|APIFamily\\|TOKENIO_AUTO_CHARGE_THRESHOLD_CENTS\\|TOKENIO_TOKEN_ESTIMATION_SAFETY_FACTOR\\|ExtractBearerAPIKey\\|BillingHeaders\" -n internal README.md",
        ],
        check=False,
    )

    print("\n========== GO TEST ==========")
    run(["go", "test", "./..."], check=False)


def main() -> int:
    try:
        validate_repositories()

        copy_reusable_layers()
        write_domain()
        write_token_pricing()
        write_config()
        write_auth_foundation()
        write_httpapi_foundation()
        write_main()
        write_readme_and_makefile()
        remove_old_hydra_runtime_files_if_present()
        ensure_go_require("golang.org/x/time", "v0.10.0")

        run_gofmt()
        tracker.print_diff()
        verification()

        print("\nOK: tokenio-gateway foundation patched.")
        return 0
    except PatchError as err:
        print(f"ERROR: {err}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
