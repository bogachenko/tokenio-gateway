package domain

import (
	"reflect"
	"testing"
	"time"
)

func TestProviderTypeContractValues(t *testing.T) {
	values := map[string]ProviderType{
		"openai":     ProviderOpenAI,
		"openrouter": ProviderOpenRouter,
		"together":   ProviderTogether,
		"groq":       ProviderGroq,
		"ollama":     ProviderOllama,
		"lmstudio":   ProviderLMStudio,
		"vllm":       ProviderVLLM,
		"gemini":     ProviderGemini,
		"anthropic":  ProviderAnthropic,
		"hydra":      ProviderHydra,
	}

	for want, got := range values {
		if string(got) != want {
			t.Fatalf("ProviderType value mismatch: got %q, want %q", got, want)
		}
	}
}

func TestAPIFamilyContractValues(t *testing.T) {
	values := map[string]APIFamily{
		"openai_compatible": APIFamilyOpenAICompatible,
		"gemini_native":     APIFamilyGeminiNative,
		"anthropic_native":  APIFamilyAnthropicNative,
		"ollama_native":     APIFamilyOllamaNative,
	}

	for want, got := range values {
		if string(got) != want {
			t.Fatalf("APIFamily value mismatch: got %q, want %q", got, want)
		}
	}
}

func TestEndpointKindContractValues(t *testing.T) {
	values := map[string]EndpointKind{
		"chat":              EndpointChat,
		"embeddings":        EndpointEmbeddings,
		"images_generation": EndpointImagesGeneration,
		"models":            EndpointModels,
		"health":            EndpointHealth,
	}

	for want, got := range values {
		if string(got) != want {
			t.Fatalf("EndpointKind value mismatch: got %q, want %q", got, want)
		}
	}
}

func TestModelRewritePolicyContractValues(t *testing.T) {
	values := map[string]ModelRewritePolicy{
		"none":           ModelRewritePolicyNone,
		"provider_model": ModelRewritePolicyProviderModel,
	}

	for want, got := range values {
		if string(got) != want {
			t.Fatalf("ModelRewritePolicy value mismatch: got %q, want %q", got, want)
		}
	}
}

func TestImageGenerationUnitKindContractValues(t *testing.T) {
	values := map[string]ImageGenerationUnitKind{
		"none":            ImageGenerationUnitKindNone,
		"generated_image": ImageGenerationUnitKindGeneratedImage,
	}

	for want, got := range values {
		if string(got) != want {
			t.Fatalf("ImageGenerationUnitKind value mismatch: got %q, want %q", got, want)
		}
	}
}

func TestCapabilitySetContractFields(t *testing.T) {
	wantTags := []string{
		"chat",
		"embeddings",
		"images_generation",
		"tools",
		"tool_choice",
		"response_format",
		"json_schema",
		"image_input",
		"audio_input",
		"file_input",
		"video_input",
		"reasoning",
	}

	capabilitySetType := reflect.TypeOf(CapabilitySet{})
	gotTags := make(map[string]bool, capabilitySetType.NumField())
	for i := 0; i < capabilitySetType.NumField(); i++ {
		gotTags[capabilitySetType.Field(i).Tag.Get("json")] = true
	}

	for _, tag := range wantTags {
		if !gotTags[tag] {
			t.Fatalf("CapabilitySet missing json contract field %q", tag)
		}
	}
}

func TestRouteSupportsModelRewritePolicy(t *testing.T) {
	route := Route{
		ClientModel:        "client-model",
		ProviderModel:      "provider-model",
		ModelRewritePolicy: ModelRewritePolicyProviderModel,
	}

	if route.ModelRewritePolicy != ModelRewritePolicyProviderModel {
		t.Fatalf("Route ModelRewritePolicy mismatch: got %q", route.ModelRewritePolicy)
	}
}

func TestTokenUsageSupportsImageGenerationUnits(t *testing.T) {
	usage := TokenUsage{ImageGenerationUnits: 3}

	if usage.ImageGenerationUnits != 3 {
		t.Fatalf("TokenUsage ImageGenerationUnits mismatch: got %d", usage.ImageGenerationUnits)
	}
}

func TestRoutePriceSupportsImageGenerationUnitPricing(t *testing.T) {
	price := RoutePrice{
		ImageGenerationPricePerUnitCents: 25,
		ImageGenerationUnitKind:          ImageGenerationUnitKindGeneratedImage,
	}

	if price.ImageGenerationPricePerUnitCents != 25 {
		t.Fatalf("RoutePrice ImageGenerationPricePerUnitCents mismatch: got %d", price.ImageGenerationPricePerUnitCents)
	}
	if price.ImageGenerationUnitKind != ImageGenerationUnitKindGeneratedImage {
		t.Fatalf("RoutePrice ImageGenerationUnitKind mismatch: got %q", price.ImageGenerationUnitKind)
	}
}

func TestUsageStatusRequiredValues(t *testing.T) {
	values := map[string]UsageStatus{
		"reserved":          UsageStatusReserved,
		"released":          UsageStatusReleased,
		"billable":          UsageStatusBillable,
		"partially_charged": UsageStatusPartiallyCharged,
		"charged":           UsageStatusCharged,
		"failed":            UsageStatusFailed,
		"pricing_failed":    UsageStatusPricingFailed,
	}

	for want, got := range values {
		if string(got) != want {
			t.Fatalf("UsageStatus value mismatch: got %q, want %q", got, want)
		}
	}
}

func TestErrorCodeRegistryContractValues(t *testing.T) {
	values := map[string]ErrorCode{
		"unauthorized":                     ErrorCodeUnauthorized,
		"invalid_api_key":                  ErrorCodeInvalidAPIKey,
		"user_disabled":                    ErrorCodeUserDisabled,
		"invalid_json":                     ErrorCodeInvalidJSON,
		"request_body_too_large":           ErrorCodeRequestBodyTooLarge,
		"unsupported_content_type":         ErrorCodeUnsupportedContentType,
		"model_required":                   ErrorCodeModelRequired,
		"streaming_unsupported":            ErrorCodeStreamingUnsupported,
		"unknown_model":                    ErrorCodeUnknownModel,
		"unsupported_capability":           ErrorCodeUnsupportedCapability,
		"no_route_available":               ErrorCodeNoRouteAvailable,
		"route_unavailable":                ErrorCodeRouteUnavailable,
		"insufficient_funds":               ErrorCodeInsufficientFunds,
		"billing_unavailable":              ErrorCodeBillingUnavailable,
		"pricing_unavailable":              ErrorCodePricingUnavailable,
		"unresolved_usage":                 ErrorCodeUnresolvedUsage,
		"request_in_progress":              ErrorCodeRequestInProgress,
		"idempotency_replay_not_available": ErrorCodeIdempotencyReplayNotAvailable,
		"idempotency_key_reused":           ErrorCodeIdempotencyKeyReused,
		"usage_store_unavailable":          ErrorCodeUsageStoreUnavailable,
		"store_unavailable":                ErrorCodeStoreUnavailable,
		"upstream_request_error":           ErrorCodeUpstreamRequestError,
		"upstream_unavailable":             ErrorCodeUpstreamUnavailable,
		"configuration_error":              ErrorCodeConfigurationError,
		"method_not_allowed":               ErrorCodeMethodNotAllowed,
		"not_found":                        ErrorCodeNotFound,
		"internal_error":                   ErrorCodeInternalError,
		"provisioning_unauthorized":        ErrorCodeProvisioningUnauthorized,
		"provisioning_invalid_request":     ErrorCodeProvisioningInvalidRequest,
		"provisioning_conflict":            ErrorCodeProvisioningConflict,
		"provisioning_expired":             ErrorCodeProvisioningExpired,
		"provisioning_store_unavailable":   ErrorCodeProvisioningStoreUnavailable,
		"provisioning_crypto_unavailable":  ErrorCodeProvisioningCryptoUnavailable,
		"admin_unauthorized":               ErrorCodeAdminUnauthorized,
		"admin_forbidden":                  ErrorCodeAdminForbidden,
		"admin_validation_error":           ErrorCodeAdminValidationError,
		"admin_not_found":                  ErrorCodeAdminNotFound,
		"admin_conflict":                   ErrorCodeAdminConflict,
		"admin_state_conflict":             ErrorCodeAdminStateConflict,
		"admin_secret_not_available":       ErrorCodeAdminSecretNotAvailable,
	}

	for want, got := range values {
		if string(got) != want {
			t.Fatalf("ErrorCode value mismatch: got %q, want %q", got, want)
		}
	}
}

func TestUsageRecordContainsAcceptedLedgerLifecycleFields(t *testing.T) {
	typ := reflect.TypeOf(UsageRecord{})
	fields := map[string]reflect.Type{
		"LocalRequestID":             reflect.TypeOf(""),
		"IdempotencyKey":             reflect.TypeOf(""),
		"UserID":                     reflect.TypeOf(""),
		"APIKeyID":                   reflect.TypeOf(""),
		"APIFamily":                  reflect.TypeOf(APIFamily("")),
		"EndpointKind":               reflect.TypeOf(EndpointKind("")),
		"ClientModel":                reflect.TypeOf(""),
		"BillingModel":               reflect.TypeOf(""),
		"SelectedRouteID":            reflect.TypeOf(""),
		"SelectedResellerID":         reflect.TypeOf(""),
		"ProviderType":               reflect.TypeOf(ProviderType("")),
		"ProviderModel":              reflect.TypeOf(""),
		"ProviderRequestID":          reflect.TypeOf(""),
		"ProviderResponseModel":      reflect.TypeOf(""),
		"EstimatedUsage":             reflect.TypeOf(TokenUsage{}),
		"Usage":                      reflect.TypeOf(TokenUsage{}),
		"EstimatedClientAmountCents": reflect.TypeOf(int64(0)),
		"EstimatedUpstreamCostCents": reflect.TypeOf(int64(0)),
		"ClientAmountCents":          reflect.TypeOf(int64(0)),
		"ChargedAmountCents":         reflect.TypeOf(int64(0)),
		"RemainingAmountCents":       reflect.TypeOf(int64(0)),
		"ActualUpstreamCostCents":    reflect.TypeOf(int64(0)),
		"Currency":                   reflect.TypeOf(""),
		"UsageCompleteness":          reflect.TypeOf(""),
		"Status":                     reflect.TypeOf(UsageStatus("")),
		"FailureReason":              reflect.TypeOf(""),
		"BillingChargeRequestID":     reflect.TypeOf(""),
		"CreatedAt":                  reflect.TypeOf(time.Time{}),
		"ReservedAt":                 reflect.TypeOf((*time.Time)(nil)),
		"ReleasedAt":                 reflect.TypeOf((*time.Time)(nil)),
		"BillableAt":                 reflect.TypeOf((*time.Time)(nil)),
		"ChargedAt":                  reflect.TypeOf((*time.Time)(nil)),
		"FailedAt":                   reflect.TypeOf((*time.Time)(nil)),
		"UpdatedAt":                  reflect.TypeOf(time.Time{}),
	}
	for name, want := range fields {
		field, ok := typ.FieldByName(name)
		if !ok {
			t.Fatalf("UsageRecord.%s is missing", name)
		}
		if field.Type != want {
			t.Fatalf("UsageRecord.%s type = %v, want %v", name, field.Type, want)
		}
	}
}

func TestBillingChargeDomainContracts(t *testing.T) {
	if BillingChargeStatusPending != "pending" || BillingChargeStatusSucceeded != "succeeded" || BillingChargeStatusFailed != "failed" {
		t.Fatal("BillingChargeStatus values changed")
	}
	batchType := reflect.TypeOf(BillingChargeBatch{})
	for _, name := range []string{"ID", "UserID", "BillingSubjectUserID", "ProviderType", "ClientModel", "BillingModel", "InputTokens", "OutputTokens", "AmountCents", "Currency", "Status", "BillingResponseBalanceCents", "BillingErrorCode", "CreatedAt", "ChargedAt", "FailedAt", "UpdatedAt"} {
		if _, ok := batchType.FieldByName(name); !ok {
			t.Fatalf("BillingChargeBatch.%s is missing", name)
		}
	}
	allocType := reflect.TypeOf(BillingChargeAllocation{})
	for _, name := range []string{"ID", "BatchID", "LocalRequestID", "ChargedAmountCents", "RemainingAmountCents", "CreatedAt"} {
		if _, ok := allocType.FieldByName(name); !ok {
			t.Fatalf("BillingChargeAllocation.%s is missing", name)
		}
	}
	for _, forbidden := range []string{"Credential", "Token", "RawRequest", "RawResponse", "RawError", "ServiceToken", "JWT"} {
		if _, ok := batchType.FieldByName(forbidden); ok {
			t.Fatalf("BillingChargeBatch exposes forbidden field %s", forbidden)
		}
	}
}
