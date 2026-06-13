package domain

import "time"

type APIKeyRecord struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	Name       string     `json:"name"`
	KeyHash    string     `json:"key_hash"`
	KeyPrefix  string     `json:"key_prefix"`
	Enabled    bool       `json:"enabled"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

type User struct {
	ID                    string     `json:"id"`
	ExternalBillingUserID string     `json:"external_billing_user_id"`
	Email                 string     `json:"email,omitempty"`
	Name                  string     `json:"name,omitempty"`
	Enabled               bool       `json:"enabled"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	DisabledAt            *time.Time `json:"disabled_at,omitempty"`
}

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

type ModelRewritePolicy string

const (
	ModelRewritePolicyNone          ModelRewritePolicy = "none"
	ModelRewritePolicyProviderModel ModelRewritePolicy = "provider_model"
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
	DisabledAt          *time.Time   `json:"-"`
}

type Route struct {
	ID                     string             `json:"id"`
	ResellerID             string             `json:"reseller_id"`
	ProviderType           ProviderType       `json:"provider_type"`
	APIFamily              APIFamily          `json:"api_family"`
	EndpointKind           EndpointKind       `json:"endpoint_kind"`
	ClientModel            string             `json:"client_model"`
	ProviderModel          string             `json:"provider_model"`
	ModelRewritePolicy     ModelRewritePolicy `json:"model_rewrite_policy"`
	Enabled                bool               `json:"enabled"`
	Priority               int                `json:"priority"`
	RequestsPerMinute      int                `json:"requests_per_minute"`
	TokensPerMinute        int                `json:"tokens_per_minute"`
	ConcurrentRequests     int                `json:"concurrent_requests"`
	DefaultMaxOutputTokens int64              `json:"default_max_output_tokens"`
	Capabilities           CapabilitySet      `json:"capabilities"`
	CooldownUntil          *time.Time         `json:"cooldown_until,omitempty"`
	CooldownReason         string             `json:"cooldown_reason,omitempty"`
	LastErrorCode          string             `json:"-"`
	LastErrorAt            *time.Time         `json:"-"`
	CreatedAt              time.Time          `json:"created_at"`
	UpdatedAt              time.Time          `json:"updated_at"`
	DisabledAt             *time.Time         `json:"-"`
}

type TokenUsage struct {
	InputTokens          int64 `json:"input_tokens"`
	CachedInputTokens    int64 `json:"cached_input_tokens"`
	OutputTokens         int64 `json:"output_tokens"`
	ReasoningTokens      int64 `json:"reasoning_tokens"`
	ImageInputTokens     int64 `json:"image_input_tokens"`
	AudioInputTokens     int64 `json:"audio_input_tokens"`
	AudioOutputTokens    int64 `json:"audio_output_tokens"`
	FileInputTokens      int64 `json:"file_input_tokens"`
	VideoInputTokens     int64 `json:"video_input_tokens"`
	ImageGenerationUnits int64 `json:"image_generation_units"`
}

type ImageGenerationUnitKind string

const (
	ImageGenerationUnitKindNone           ImageGenerationUnitKind = "none"
	ImageGenerationUnitKindGeneratedImage ImageGenerationUnitKind = "generated_image"
)

type RoutePrice struct {
	RouteID                              string                  `json:"route_id"`
	Currency                             string                  `json:"currency"`
	InputPricePer1MTokensCents           int64                   `json:"input_price_per_1m_tokens_cents"`
	CachedInputPricePer1MTokensCents     int64                   `json:"cached_input_price_per_1m_tokens_cents"`
	OutputPricePer1MTokensCents          int64                   `json:"output_price_per_1m_tokens_cents"`
	ReasoningOutputPricePer1MTokensCents int64                   `json:"reasoning_output_price_per_1m_tokens_cents"`
	ImageInputPricePer1MTokensCents      int64                   `json:"image_input_price_per_1m_tokens_cents"`
	AudioInputPricePer1MTokensCents      int64                   `json:"audio_input_price_per_1m_tokens_cents"`
	AudioOutputPricePer1MTokensCents     int64                   `json:"audio_output_price_per_1m_tokens_cents"`
	FileInputPricePer1MTokensCents       int64                   `json:"file_input_price_per_1m_tokens_cents"`
	VideoInputPricePer1MTokensCents      int64                   `json:"video_input_price_per_1m_tokens_cents"`
	ImageGenerationPricePerUnitCents     int64                   `json:"image_generation_price_per_unit_cents"`
	ImageGenerationUnitKind              ImageGenerationUnitKind `json:"image_generation_unit_kind"`
	MarkupCoefficient                    float64                 `json:"markup_coefficient"`
	Enabled                              bool                    `json:"enabled"`
	CreatedAt                            time.Time               `json:"-"`
	UpdatedAt                            time.Time               `json:"-"`
}

type UsageStatus string

const (
	UsageStatusReserved         UsageStatus = "reserved"
	UsageStatusReleased         UsageStatus = "released"
	UsageStatusBillable         UsageStatus = "billable"
	UsageStatusPartiallyCharged UsageStatus = "partially_charged"
	UsageStatusCharged          UsageStatus = "charged"
	UsageStatusFailed           UsageStatus = "failed"
	UsageStatusPricingFailed    UsageStatus = "pricing_failed"
)

type BillingChargeStatus string

const (
	BillingChargeStatusPending   BillingChargeStatus = "pending"
	BillingChargeStatusSucceeded BillingChargeStatus = "succeeded"
	BillingChargeStatusFailed    BillingChargeStatus = "failed"
)

type ErrorCode string

const (
	ErrorCodeUnauthorized                  ErrorCode = "unauthorized"
	ErrorCodeInvalidAPIKey                 ErrorCode = "invalid_api_key"
	ErrorCodeUserDisabled                  ErrorCode = "user_disabled"
	ErrorCodeInvalidJSON                   ErrorCode = "invalid_json"
	ErrorCodeRequestBodyTooLarge           ErrorCode = "request_body_too_large"
	ErrorCodeUnsupportedContentType        ErrorCode = "unsupported_content_type"
	ErrorCodeModelRequired                 ErrorCode = "model_required"
	ErrorCodeStreamingUnsupported          ErrorCode = "streaming_unsupported"
	ErrorCodeUnknownModel                  ErrorCode = "unknown_model"
	ErrorCodeUnsupportedCapability         ErrorCode = "unsupported_capability"
	ErrorCodeNoRouteAvailable              ErrorCode = "no_route_available"
	ErrorCodeRouteUnavailable              ErrorCode = "route_unavailable"
	ErrorCodeInsufficientFunds             ErrorCode = "insufficient_funds"
	ErrorCodeBillingUnavailable            ErrorCode = "billing_unavailable"
	ErrorCodePricingUnavailable            ErrorCode = "pricing_unavailable"
	ErrorCodeUnresolvedUsage               ErrorCode = "unresolved_usage"
	ErrorCodeRequestInProgress             ErrorCode = "request_in_progress"
	ErrorCodeIdempotencyReplayNotAvailable ErrorCode = "idempotency_replay_not_available"
	ErrorCodeIdempotencyKeyReused          ErrorCode = "idempotency_key_reused"
	ErrorCodeUsageStoreUnavailable         ErrorCode = "usage_store_unavailable"
	ErrorCodeStoreUnavailable              ErrorCode = "store_unavailable"
	ErrorCodeUpstreamRequestError          ErrorCode = "upstream_request_error"
	ErrorCodeUpstreamUnavailable           ErrorCode = "upstream_unavailable"
	ErrorCodeConfigurationError            ErrorCode = "configuration_error"
	ErrorCodeMethodNotAllowed              ErrorCode = "method_not_allowed"
	ErrorCodeNotFound                      ErrorCode = "not_found"
	ErrorCodeInternalError                 ErrorCode = "internal_error"
	ErrorCodeProvisioningUnauthorized      ErrorCode = "provisioning_unauthorized"
	ErrorCodeProvisioningInvalidRequest    ErrorCode = "provisioning_invalid_request"
	ErrorCodeProvisioningConflict          ErrorCode = "provisioning_conflict"
	ErrorCodeProvisioningExpired           ErrorCode = "provisioning_expired"
	ErrorCodeProvisioningStoreUnavailable  ErrorCode = "provisioning_store_unavailable"
	ErrorCodeProvisioningCryptoUnavailable ErrorCode = "provisioning_crypto_unavailable"
	ErrorCodeAdminUnauthorized             ErrorCode = "admin_unauthorized"
	ErrorCodeAdminForbidden                ErrorCode = "admin_forbidden"
	ErrorCodeAdminValidationError          ErrorCode = "admin_validation_error"
	ErrorCodeAdminNotFound                 ErrorCode = "admin_not_found"
	ErrorCodeAdminConflict                 ErrorCode = "admin_conflict"
	ErrorCodeAdminStateConflict            ErrorCode = "admin_state_conflict"
	ErrorCodeAdminSecretNotAvailable       ErrorCode = "admin_secret_not_available"
)

type UsageRecord struct {
	LocalRequestID string `json:"local_request_id"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`

	UserID   string `json:"user_id"`
	APIKeyID string `json:"api_key_id"`

	APIFamily    APIFamily    `json:"api_family"`
	EndpointKind EndpointKind `json:"endpoint_kind"`

	ClientModel  string `json:"client_model"`
	BillingModel string `json:"billing_model"`

	SelectedRouteID    string `json:"selected_route_id"`
	SelectedResellerID string `json:"selected_reseller_id"`

	ProviderType  ProviderType `json:"provider_type"`
	ProviderModel string       `json:"provider_model"`

	ProviderRequestID     string `json:"provider_request_id,omitempty"`
	ProviderResponseModel string `json:"provider_response_model,omitempty"`

	EstimatedUsage TokenUsage `json:"estimated_usage"`
	Usage          TokenUsage `json:"usage"`

	EstimatedClientAmountCents int64 `json:"estimated_client_amount_cents"`
	EstimatedUpstreamCostCents int64 `json:"estimated_upstream_cost_cents"`

	ClientAmountCents       int64 `json:"client_amount_cents"`
	ChargedAmountCents      int64 `json:"charged_amount_cents"`
	RemainingAmountCents    int64 `json:"remaining_amount_cents"`
	ActualUpstreamCostCents int64 `json:"actual_upstream_cost_cents"`

	Currency          string      `json:"currency"`
	UsageCompleteness string      `json:"usage_completeness"`
	Status            UsageStatus `json:"status"`

	FailureReason          string `json:"failure_reason,omitempty"`
	BillingChargeRequestID string `json:"billing_charge_request_id,omitempty"`

	CreatedAt  time.Time  `json:"created_at"`
	ReservedAt *time.Time `json:"reserved_at,omitempty"`
	ReleasedAt *time.Time `json:"released_at,omitempty"`
	BillableAt *time.Time `json:"billable_at,omitempty"`
	ChargedAt  *time.Time `json:"charged_at,omitempty"`
	FailedAt   *time.Time `json:"failed_at,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type BillingChargeBatch struct {
	ID string `json:"id"`

	UserID               string `json:"user_id"`
	BillingSubjectUserID string `json:"billing_subject_user_id"`

	ProviderType ProviderType `json:"provider_type"`
	ClientModel  string       `json:"client_model"`
	BillingModel string       `json:"billing_model"`

	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`

	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`

	Status BillingChargeStatus `json:"status"`

	BillingResponseBalanceCents *int64 `json:"billing_response_balance_cents,omitempty"`
	BillingErrorCode            string `json:"billing_error_code,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	ChargedAt *time.Time `json:"charged_at,omitempty"`
	FailedAt  *time.Time `json:"failed_at,omitempty"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type BillingChargeAllocation struct {
	ID string `json:"id"`

	BatchID        string `json:"batch_id"`
	LocalRequestID string `json:"local_request_id"`

	ChargedAmountCents   int64 `json:"charged_amount_cents"`
	RemainingAmountCents int64 `json:"remaining_amount_cents"`

	CreatedAt time.Time `json:"created_at"`
}
