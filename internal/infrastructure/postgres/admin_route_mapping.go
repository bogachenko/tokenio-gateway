package postgres

import (
	"encoding/json"
	"math"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func encodeAdminRouteCapabilities(
	value domain.CapabilitySet,
) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, ports.ErrStoreContractViolation
	}
	decoded, err := decodeCapabilities(body)
	if err != nil || decoded != value {
		return nil, ports.ErrStoreContractViolation
	}
	return body, nil
}

func canonicalAdminRoute(value domain.Route) domain.Route {
	result := value
	result.CreatedAt = postgresAdminTime(value.CreatedAt)
	result.UpdatedAt = postgresAdminTime(value.UpdatedAt)
	result.CooldownUntil = canonicalAdminRouteTimePointer(
		value.CooldownUntil,
	)
	result.LastErrorAt = canonicalAdminRouteTimePointer(
		value.LastErrorAt,
	)
	result.DisabledAt = canonicalAdminRouteTimePointer(
		value.DisabledAt,
	)
	return result
}

func canonicalAdminRouteTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	canonical := postgresAdminTime(*value)
	return &canonical
}

func adminRouteTimeArg(value *time.Time) any {
	if value == nil {
		return nil
	}
	return postgresAdminTime(*value)
}

func sameAdminRoute(left domain.Route, right domain.Route) bool {
	return left.ID == right.ID &&
		left.ResellerID == right.ResellerID &&
		left.ProviderType == right.ProviderType &&
		left.APIFamily == right.APIFamily &&
		left.EndpointKind == right.EndpointKind &&
		left.ClientModel == right.ClientModel &&
		left.ProviderModel == right.ProviderModel &&
		left.ModelRewritePolicy == right.ModelRewritePolicy &&
		left.Enabled == right.Enabled &&
		left.Priority == right.Priority &&
		left.RequestsPerMinute == right.RequestsPerMinute &&
		left.TokensPerMinute == right.TokensPerMinute &&
		left.ConcurrentRequests == right.ConcurrentRequests &&
		left.DefaultMaxOutputTokens ==
			right.DefaultMaxOutputTokens &&
		left.Capabilities == right.Capabilities &&
		sameAdminTimePointer(
			left.CooldownUntil,
			right.CooldownUntil,
		) &&
		left.CooldownReason == right.CooldownReason &&
		left.LastErrorCode == right.LastErrorCode &&
		sameAdminTimePointer(
			left.LastErrorAt,
			right.LastErrorAt,
		) &&
		postgresAdminTime(left.CreatedAt).Equal(
			postgresAdminTime(right.CreatedAt),
		) &&
		postgresAdminTime(left.UpdatedAt).Equal(
			postgresAdminTime(right.UpdatedAt),
		) &&
		sameAdminTimePointer(left.DisabledAt, right.DisabledAt)
}

func adminRouteApplicationState(
	value domain.Route,
) domain.AuditState {
	return domain.AuditState{
		"id":                        value.ID,
		"reseller_id":               value.ResellerID,
		"provider_type":             value.ProviderType,
		"api_family":                value.APIFamily,
		"endpoint_kind":             value.EndpointKind,
		"client_model":              value.ClientModel,
		"provider_model":            value.ProviderModel,
		"model_rewrite_policy":      value.ModelRewritePolicy,
		"enabled":                   value.Enabled,
		"priority":                  value.Priority,
		"requests_per_minute":       value.RequestsPerMinute,
		"tokens_per_minute":         value.TokensPerMinute,
		"concurrent_requests":       value.ConcurrentRequests,
		"default_max_output_tokens": value.DefaultMaxOutputTokens,
		"capabilities":              value.Capabilities,
		"cooldown_until":            value.CooldownUntil,
		"cooldown_reason":           value.CooldownReason,
		"last_error_code":           value.LastErrorCode,
		"last_error_at":             value.LastErrorAt,
		"created_at":                value.CreatedAt,
		"updated_at":                value.UpdatedAt,
		"disabled_at":               value.DisabledAt,
	}
}

func adminRouteState(value domain.Route) domain.AuditState {
	return adminRouteApplicationState(canonicalAdminRoute(value))
}

func validateAdminRouteRecord(value domain.Route) error {
	if value.ID == "" ||
		value.ResellerID == "" ||
		!validAdminRouteProviderType(value.ProviderType) ||
		!validAdminRouteAPIFamily(value.APIFamily) ||
		!validAdminRouteEndpointKind(value.EndpointKind) ||
		value.ClientModel == "" ||
		value.ProviderModel == "" ||
		!validAdminRouteRewritePolicy(
			value.ModelRewritePolicy,
		) ||
		value.Priority < 0 ||
		value.RequestsPerMinute < 0 ||
		value.TokensPerMinute < 0 ||
		value.ConcurrentRequests < 0 ||
		value.DefaultMaxOutputTokens < 0 ||
		!isAdminUTCTime(value.CreatedAt) ||
		!isAdminUTCTime(value.UpdatedAt) ||
		postgresAdminTime(value.UpdatedAt).Before(
			postgresAdminTime(value.CreatedAt),
		) ||
		value.CooldownUntil != nil &&
			!isAdminUTCTime(*value.CooldownUntil) ||
		value.LastErrorAt != nil &&
			!isAdminUTCTime(*value.LastErrorAt) ||
		value.DisabledAt != nil &&
			!isAdminUTCTime(*value.DisabledAt) {
		return ports.ErrStoreContractViolation
	}
	if value.CooldownUntil == nil && value.CooldownReason != "" {
		return ports.ErrStoreContractViolation
	}
	if value.CooldownUntil != nil && value.CooldownReason == "" {
		return ports.ErrStoreContractViolation
	}
	if value.LastErrorAt == nil && value.LastErrorCode != "" {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func validAdminRouteProviderType(
	value domain.ProviderType,
) bool {
	switch value {
	case
		domain.ProviderOpenAI,
		domain.ProviderOpenRouter,
		domain.ProviderTogether,
		domain.ProviderGroq,
		domain.ProviderOllama,
		domain.ProviderLMStudio,
		domain.ProviderVLLM,
		domain.ProviderGemini,
		domain.ProviderAnthropic,
		domain.ProviderHydra:
		return true
	default:
		return false
	}
}

func validAdminRouteAPIFamily(value domain.APIFamily) bool {
	switch value {
	case
		domain.APIFamilyOpenAICompatible,
		domain.APIFamilyGeminiNative,
		domain.APIFamilyAnthropicNative,
		domain.APIFamilyOllamaNative:
		return true
	default:
		return false
	}
}

func validAdminRouteEndpointKind(
	value domain.EndpointKind,
) bool {
	switch value {
	case
		domain.EndpointChat,
		domain.EndpointEmbeddings,
		domain.EndpointImagesGeneration:
		return true
	default:
		return false
	}
}

func validAdminRouteRewritePolicy(
	value domain.ModelRewritePolicy,
) bool {
	switch value {
	case
		domain.ModelRewritePolicyNone,
		domain.ModelRewritePolicyProviderModel:
		return true
	default:
		return false
	}
}

func validateAdminRouteMutation(
	expected domain.Route,
	next domain.Route,
	action domain.AuditAction,
) error {
	if expected.ID != next.ID ||
		expected.ResellerID != next.ResellerID ||
		expected.ProviderType != next.ProviderType ||
		expected.APIFamily != next.APIFamily ||
		expected.EndpointKind != next.EndpointKind ||
		expected.ClientModel != next.ClientModel ||
		!postgresAdminTime(expected.CreatedAt).Equal(
			postgresAdminTime(next.CreatedAt),
		) ||
		!postgresAdminTime(next.UpdatedAt).After(
			postgresAdminTime(expected.UpdatedAt),
		) ||
		expected.LastErrorCode != next.LastErrorCode ||
		!sameAdminTimePointer(
			expected.LastErrorAt,
			next.LastErrorAt,
		) {
		return ports.ErrStoreContractViolation
	}

	switch action {
	case domain.AuditActionRouteUpdate:
		if !sameAdminTimePointer(
			expected.CooldownUntil,
			next.CooldownUntil,
		) ||
			expected.CooldownReason != next.CooldownReason {
			return ports.ErrStoreContractViolation
		}
		return validateRouteEnabledTransition(
			expected,
			next,
			true,
		)

	case domain.AuditActionRouteEnable:
		if !sameRouteExceptEnabledState(expected, next) ||
			expected.Enabled ||
			!next.Enabled ||
			next.DisabledAt != nil {
			return ports.ErrStoreContractViolation
		}
		return nil

	case domain.AuditActionRouteDisable:
		if !sameRouteExceptEnabledState(expected, next) ||
			!expected.Enabled ||
			next.Enabled ||
			next.DisabledAt == nil ||
			!postgresAdminTime(*next.DisabledAt).Equal(
				postgresAdminTime(next.UpdatedAt),
			) {
			return ports.ErrStoreContractViolation
		}
		return nil

	case domain.AuditActionRouteCooldownSet:
		if !sameRouteExceptCooldown(expected, next) ||
			next.CooldownUntil == nil ||
			next.CooldownReason == "" ||
			!next.CooldownUntil.After(next.UpdatedAt) {
			return ports.ErrStoreContractViolation
		}
		return nil

	case domain.AuditActionRouteCooldownClear:
		if !sameRouteExceptCooldown(expected, next) ||
			expected.CooldownUntil == nil &&
				expected.CooldownReason == "" ||
			next.CooldownUntil != nil ||
			next.CooldownReason != "" {
			return ports.ErrStoreContractViolation
		}
		return nil

	default:
		return ports.ErrStoreContractViolation
	}
}

func validateRouteEnabledTransition(
	expected domain.Route,
	next domain.Route,
	allowUnchanged bool,
) error {
	if expected.Enabled == next.Enabled {
		if !allowUnchanged ||
			!sameAdminTimePointer(
				expected.DisabledAt,
				next.DisabledAt,
			) {
			return ports.ErrStoreContractViolation
		}
		return nil
	}
	if expected.Enabled && !next.Enabled {
		if next.DisabledAt == nil ||
			!postgresAdminTime(*next.DisabledAt).Equal(
				postgresAdminTime(next.UpdatedAt),
			) {
			return ports.ErrStoreContractViolation
		}
		return nil
	}
	if !expected.Enabled && next.Enabled && next.DisabledAt == nil {
		return nil
	}
	return ports.ErrStoreContractViolation
}

func sameRouteExceptEnabledState(
	expected domain.Route,
	next domain.Route,
) bool {
	left := expected
	right := next
	left.Enabled = right.Enabled
	left.DisabledAt = right.DisabledAt
	left.UpdatedAt = right.UpdatedAt
	return sameAdminRoute(left, right)
}

func sameRouteExceptCooldown(
	expected domain.Route,
	next domain.Route,
) bool {
	left := expected
	right := next
	left.CooldownUntil = right.CooldownUntil
	left.CooldownReason = right.CooldownReason
	left.UpdatedAt = right.UpdatedAt
	return sameAdminRoute(left, right)
}

func canonicalRouteAudit(
	audit domain.AuditContext,
	before domain.AuditState,
	after domain.AuditState,
	at time.Time,
) domain.AuditContext {
	result := audit
	result.BeforeState = before
	result.AfterState = after
	result.CreatedAt = postgresAdminTime(at)
	return result
}

func validateAdminRoutePriceRecord(
	value domain.RoutePrice,
) error {
	if value.RouteID == "" ||
		value.Currency != "RUB" ||
		value.InputPricePer1MTokensCents < 0 ||
		value.CachedInputPricePer1MTokensCents < 0 ||
		value.OutputPricePer1MTokensCents < 0 ||
		value.ReasoningOutputPricePer1MTokensCents < 0 ||
		value.ImageInputPricePer1MTokensCents < 0 ||
		value.AudioInputPricePer1MTokensCents < 0 ||
		value.AudioOutputPricePer1MTokensCents < 0 ||
		value.FileInputPricePer1MTokensCents < 0 ||
		value.VideoInputPricePer1MTokensCents < 0 ||
		value.ImageGenerationPricePerUnitCents < 0 ||
		!validAdminImageGenerationUnitKind(
			value.ImageGenerationUnitKind,
		) ||
		value.MarkupCoefficient <= 0 ||
		math.IsNaN(value.MarkupCoefficient) ||
		math.IsInf(value.MarkupCoefficient, 0) ||
		!isAdminUTCTime(value.CreatedAt) ||
		!isAdminUTCTime(value.UpdatedAt) ||
		postgresAdminTime(value.UpdatedAt).Before(
			postgresAdminTime(value.CreatedAt),
		) {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func validAdminImageGenerationUnitKind(
	value domain.ImageGenerationUnitKind,
) bool {
	switch value {
	case
		domain.ImageGenerationUnitKindNone,
		domain.ImageGenerationUnitKindGeneratedImage:
		return true
	default:
		return false
	}
}

func canonicalAdminRoutePrice(
	value domain.RoutePrice,
) domain.RoutePrice {
	result := value
	result.CreatedAt = postgresAdminTime(value.CreatedAt)
	result.UpdatedAt = postgresAdminTime(value.UpdatedAt)
	return result
}

func sameAdminRoutePrice(
	left domain.RoutePrice,
	right domain.RoutePrice,
) bool {
	return left.RouteID == right.RouteID &&
		left.Currency == right.Currency &&
		left.InputPricePer1MTokensCents ==
			right.InputPricePer1MTokensCents &&
		left.CachedInputPricePer1MTokensCents ==
			right.CachedInputPricePer1MTokensCents &&
		left.OutputPricePer1MTokensCents ==
			right.OutputPricePer1MTokensCents &&
		left.ReasoningOutputPricePer1MTokensCents ==
			right.ReasoningOutputPricePer1MTokensCents &&
		left.ImageInputPricePer1MTokensCents ==
			right.ImageInputPricePer1MTokensCents &&
		left.AudioInputPricePer1MTokensCents ==
			right.AudioInputPricePer1MTokensCents &&
		left.AudioOutputPricePer1MTokensCents ==
			right.AudioOutputPricePer1MTokensCents &&
		left.FileInputPricePer1MTokensCents ==
			right.FileInputPricePer1MTokensCents &&
		left.VideoInputPricePer1MTokensCents ==
			right.VideoInputPricePer1MTokensCents &&
		left.ImageGenerationPricePerUnitCents ==
			right.ImageGenerationPricePerUnitCents &&
		left.ImageGenerationUnitKind ==
			right.ImageGenerationUnitKind &&
		left.MarkupCoefficient == right.MarkupCoefficient &&
		left.Enabled == right.Enabled &&
		postgresAdminTime(left.CreatedAt).Equal(
			postgresAdminTime(right.CreatedAt),
		) &&
		postgresAdminTime(left.UpdatedAt).Equal(
			postgresAdminTime(right.UpdatedAt),
		)
}

func adminRoutePriceApplicationState(
	value domain.RoutePrice,
) domain.AuditState {
	return domain.AuditState{
		"route_id":                                   value.RouteID,
		"currency":                                   value.Currency,
		"input_price_per_1m_tokens_cents":            value.InputPricePer1MTokensCents,
		"cached_input_price_per_1m_tokens_cents":     value.CachedInputPricePer1MTokensCents,
		"output_price_per_1m_tokens_cents":           value.OutputPricePer1MTokensCents,
		"reasoning_output_price_per_1m_tokens_cents": value.ReasoningOutputPricePer1MTokensCents,
		"image_input_price_per_1m_tokens_cents":      value.ImageInputPricePer1MTokensCents,
		"audio_input_price_per_1m_tokens_cents":      value.AudioInputPricePer1MTokensCents,
		"audio_output_price_per_1m_tokens_cents":     value.AudioOutputPricePer1MTokensCents,
		"file_input_price_per_1m_tokens_cents":       value.FileInputPricePer1MTokensCents,
		"video_input_price_per_1m_tokens_cents":      value.VideoInputPricePer1MTokensCents,
		"image_generation_price_per_unit_cents":      value.ImageGenerationPricePerUnitCents,
		"image_generation_unit_kind":                 value.ImageGenerationUnitKind,
		"markup_coefficient":                         value.MarkupCoefficient,
		"enabled":                                    value.Enabled,
		"created_at":                                 value.CreatedAt,
		"updated_at":                                 value.UpdatedAt,
	}
}

func adminRoutePriceState(
	value domain.RoutePrice,
) domain.AuditState {
	return adminRoutePriceApplicationState(
		canonicalAdminRoutePrice(value),
	)
}

func canonicalRoutePriceAudit(
	audit domain.AuditContext,
	before domain.AuditState,
	after domain.AuditState,
	at time.Time,
) domain.AuditContext {
	result := audit
	result.BeforeState = before
	result.AfterState = after
	result.CreatedAt = postgresAdminTime(at)
	return result
}
