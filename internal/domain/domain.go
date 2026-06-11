package domain

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
	EndpointModels           EndpointKind = "models"
	EndpointHealth           EndpointKind = "health"
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
	InputTokens       int64 `json:"input_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	ReasoningTokens   int64 `json:"reasoning_tokens"`
	ImageInputTokens  int64 `json:"image_input_tokens"`
	AudioInputTokens  int64 `json:"audio_input_tokens"`
	AudioOutputTokens int64 `json:"audio_output_tokens"`
	FileInputTokens   int64 `json:"file_input_tokens"`
	VideoInputTokens  int64 `json:"video_input_tokens"`
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
