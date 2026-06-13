package postgres

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5/pgtype"
)

type operationalRowScanner interface {
	Scan(dest ...any) error
}

func operationalTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

func operationalTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	canonical := operationalTime(*value)
	return &canonical
}

func operationalTimeArg(value *time.Time) any {
	if value == nil {
		return nil
	}
	return operationalTime(*value)
}

func sameOperationalTimePointer(
	left *time.Time,
	right *time.Time,
) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return operationalTime(*left).Equal(
			operationalTime(*right),
		)
	}
}

func isOperationalUTCTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC
}

func validateOperationalPage(page ports.PageRequest) error {
	if page.Limit <= 0 || page.Offset < 0 {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func validateOperationalWindow(
	from *time.Time,
	to *time.Time,
) error {
	if from != nil && !isOperationalUTCTime(*from) {
		return ports.ErrStoreContractViolation
	}
	if to != nil && !isOperationalUTCTime(*to) {
		return ports.ErrStoreContractViolation
	}
	if from != nil && to != nil && !from.Before(*to) {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func scanBillingSession(
	row operationalRowScanner,
) (domain.BillingSession, error) {
	var value domain.BillingSession
	if err := row.Scan(
		&value.UserID,
		&value.BillingSubjectUserID,
		&value.RemoteBalanceCents,
		&value.PendingAmountCentsCached,
		&value.Currency,
		&value.FetchedAt,
		&value.CreatedAt,
		&value.UpdatedAt,
	); err != nil {
		return domain.BillingSession{},
			normalizeRegistryReadError(err)
	}

	value.FetchedAt = operationalTime(value.FetchedAt)
	value.CreatedAt = operationalTime(value.CreatedAt)
	value.UpdatedAt = operationalTime(value.UpdatedAt)
	if err := validateBillingSessionPersistence(value); err != nil {
		return domain.BillingSession{}, err
	}
	return value, nil
}

func validateBillingSessionPersistence(
	value domain.BillingSession,
) error {
	if value.UserID == "" ||
		value.BillingSubjectUserID == "" ||
		value.RemoteBalanceCents < 0 ||
		value.PendingAmountCentsCached < 0 ||
		value.Currency != "RUB" ||
		!isOperationalUTCTime(value.FetchedAt) ||
		!isOperationalUTCTime(value.CreatedAt) ||
		!isOperationalUTCTime(value.UpdatedAt) ||
		operationalTime(value.FetchedAt).Before(
			operationalTime(value.CreatedAt),
		) ||
		operationalTime(value.UpdatedAt).Before(
			operationalTime(value.FetchedAt),
		) {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func canonicalBillingSession(
	value domain.BillingSession,
) domain.BillingSession {
	result := value
	result.FetchedAt = operationalTime(value.FetchedAt)
	result.CreatedAt = operationalTime(value.CreatedAt)
	result.UpdatedAt = operationalTime(value.UpdatedAt)
	return result
}

func sameBillingSession(
	left domain.BillingSession,
	right domain.BillingSession,
) bool {
	return left.UserID == right.UserID &&
		left.BillingSubjectUserID ==
			right.BillingSubjectUserID &&
		left.RemoteBalanceCents == right.RemoteBalanceCents &&
		left.PendingAmountCentsCached ==
			right.PendingAmountCentsCached &&
		left.Currency == right.Currency &&
		operationalTime(left.FetchedAt).Equal(
			operationalTime(right.FetchedAt),
		) &&
		operationalTime(left.CreatedAt).Equal(
			operationalTime(right.CreatedAt),
		) &&
		operationalTime(left.UpdatedAt).Equal(
			operationalTime(right.UpdatedAt),
		)
}

func encodeRouteEventMetadata(
	metadata domain.RouteEventMetadata,
) ([]byte, error) {
	if metadata == nil {
		metadata = domain.RouteEventMetadata{}
	}
	if containsOperationalSecret(metadata) {
		return nil, ports.ErrStoreContractViolation
	}
	body, err := json.Marshal(metadata)
	if err != nil {
		return nil, ports.ErrStoreContractViolation
	}
	if _, err := decodeRouteEventMetadata(body); err != nil {
		return nil, err
	}
	return body, nil
}

func decodeRouteEventMetadata(
	raw []byte,
) (domain.RouteEventMetadata, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) < 2 ||
		trimmed[0] != '{' ||
		trimmed[len(trimmed)-1] != '}' {
		return nil, ports.ErrStoreContractViolation
	}

	var value domain.RouteEventMetadata
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, ports.ErrStoreContractViolation
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, ports.ErrStoreContractViolation
	}
	if value == nil {
		value = domain.RouteEventMetadata{}
	}
	if containsOperationalSecret(value) {
		return nil, ports.ErrStoreContractViolation
	}
	return value, nil
}

func containsOperationalSecret(value any) bool {
	switch typed := value.(type) {
	case domain.RouteEventMetadata:
		return containsOperationalSecret(map[string]any(typed))
	case map[string]any:
		for key, child := range typed {
			switch strings.ToLower(key) {
			case
				"authorization",
				"api_key",
				"raw_api_key",
				"key_hash",
				"billing_jwt",
				"billing_service_token",
				"admin_token",
				"request_body",
				"response_body",
				"raw_body",
				"encrypted_raw_key",
				"encryption_nonce":
				return true
			}
			if containsOperationalSecret(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsOperationalSecret(child) {
				return true
			}
		}
	}
	return false
}

func validRouteEventType(value domain.RouteEventType) bool {
	switch value {
	case
		domain.RouteEventTypeSelected,
		domain.RouteEventTypeSkipped,
		domain.RouteEventTypeCooldownSet,
		domain.RouteEventTypeCooldownExpired,
		domain.RouteEventTypeRetry,
		domain.RouteEventTypeFailure,
		domain.RouteEventTypeSuccess,
		domain.RouteEventTypeHealthcheckFailed,
		domain.RouteEventTypeHealthcheckRecovered,
		domain.RouteEventTypeBalanceLow:
		return true
	default:
		return false
	}
}

func validOperationalProviderType(
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

func validOperationalAPIFamily(value domain.APIFamily) bool {
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

func validOperationalEndpointKind(
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

func validateRouteEventPersistence(
	value domain.RouteEvent,
) error {
	if value.ID == "" ||
		!validRouteEventType(value.EventType) ||
		!isOperationalUTCTime(value.CreatedAt) {
		return ports.ErrStoreContractViolation
	}
	if value.ProviderType != "" &&
		!validOperationalProviderType(value.ProviderType) {
		return ports.ErrStoreContractViolation
	}
	if value.APIFamily != "" &&
		!validOperationalAPIFamily(value.APIFamily) {
		return ports.ErrStoreContractViolation
	}
	if value.EndpointKind != "" &&
		!validOperationalEndpointKind(value.EndpointKind) {
		return ports.ErrStoreContractViolation
	}
	if _, err := encodeRouteEventMetadata(value.Metadata); err != nil {
		return err
	}
	return nil
}

func canonicalRouteEvent(value domain.RouteEvent) domain.RouteEvent {
	result := value
	result.CreatedAt = operationalTime(value.CreatedAt)
	if result.Metadata == nil {
		result.Metadata = domain.RouteEventMetadata{}
	}
	return result
}

func scanRouteEvent(
	row operationalRowScanner,
) (domain.RouteEvent, error) {
	var value domain.RouteEvent
	var routeID pgtype.Text
	var resellerID pgtype.Text
	var providerType pgtype.Text
	var apiFamily pgtype.Text
	var endpointKind pgtype.Text
	var clientModel pgtype.Text
	var eventType string
	var reason pgtype.Text
	var localRequestID pgtype.Text
	var metadataRaw []byte

	if err := row.Scan(
		&value.ID,
		&routeID,
		&resellerID,
		&providerType,
		&apiFamily,
		&endpointKind,
		&clientModel,
		&eventType,
		&reason,
		&localRequestID,
		&metadataRaw,
		&value.CreatedAt,
	); err != nil {
		return domain.RouteEvent{},
			normalizeRegistryReadError(err)
	}

	metadata, err := decodeRouteEventMetadata(metadataRaw)
	if err != nil {
		return domain.RouteEvent{}, err
	}

	value.RouteID = optionalText(routeID)
	value.ResellerID = optionalText(resellerID)
	value.ProviderType =
		domain.ProviderType(optionalText(providerType))
	value.APIFamily = domain.APIFamily(optionalText(apiFamily))
	value.EndpointKind =
		domain.EndpointKind(optionalText(endpointKind))
	value.ClientModel = optionalText(clientModel)
	value.EventType = domain.RouteEventType(eventType)
	value.Reason = optionalText(reason)
	value.LocalRequestID = optionalText(localRequestID)
	value.Metadata = metadata
	value.CreatedAt = operationalTime(value.CreatedAt)

	if err := validateRouteEventPersistence(value); err != nil {
		return domain.RouteEvent{}, err
	}
	return value, nil
}

func sameRouteEvent(
	left domain.RouteEvent,
	right domain.RouteEvent,
) bool {
	leftBody, leftErr := encodeRouteEventMetadata(left.Metadata)
	rightBody, rightErr := encodeRouteEventMetadata(right.Metadata)
	return leftErr == nil &&
		rightErr == nil &&
		bytes.Equal(leftBody, rightBody) &&
		left.ID == right.ID &&
		left.RouteID == right.RouteID &&
		left.ResellerID == right.ResellerID &&
		left.ProviderType == right.ProviderType &&
		left.APIFamily == right.APIFamily &&
		left.EndpointKind == right.EndpointKind &&
		left.ClientModel == right.ClientModel &&
		left.EventType == right.EventType &&
		left.Reason == right.Reason &&
		left.LocalRequestID == right.LocalRequestID &&
		operationalTime(left.CreatedAt).Equal(
			operationalTime(right.CreatedAt),
		)
}

func validTelegramAlertStatus(
	value domain.TelegramAlertStatus,
) bool {
	switch value {
	case
		domain.TelegramAlertStatusPending,
		domain.TelegramAlertStatusSent,
		domain.TelegramAlertStatusFailed,
		domain.TelegramAlertStatusSuppressed:
		return true
	default:
		return false
	}
}

func validateTelegramAlertPersistence(
	value domain.TelegramAlert,
) error {
	if value.ID == "" ||
		value.AlertType == "" ||
		value.DedupeKey == "" ||
		value.Message == "" ||
		!validTelegramAlertStatus(value.Status) ||
		!isOperationalUTCTime(value.CreatedAt) ||
		value.SentAt != nil &&
			!isOperationalUTCTime(*value.SentAt) {
		return ports.ErrStoreContractViolation
	}

	switch value.Status {
	case domain.TelegramAlertStatusPending:
		if value.Error != "" || value.SentAt != nil {
			return ports.ErrStoreContractViolation
		}
	case domain.TelegramAlertStatusSent:
		if value.Error != "" ||
			value.SentAt == nil ||
			value.SentAt.Before(value.CreatedAt) {
			return ports.ErrStoreContractViolation
		}
	case domain.TelegramAlertStatusFailed:
		if value.Error == "" || value.SentAt != nil {
			return ports.ErrStoreContractViolation
		}
	case domain.TelegramAlertStatusSuppressed:
		if value.Error != "" || value.SentAt != nil {
			return ports.ErrStoreContractViolation
		}
	}
	return nil
}

func canonicalTelegramAlert(
	value domain.TelegramAlert,
) domain.TelegramAlert {
	result := value
	result.CreatedAt = operationalTime(value.CreatedAt)
	result.SentAt = operationalTimePointer(value.SentAt)
	return result
}

func scanTelegramAlert(
	row operationalRowScanner,
) (domain.TelegramAlert, error) {
	var value domain.TelegramAlert
	var resellerID pgtype.Text
	var routeID pgtype.Text
	var status string
	var errorText pgtype.Text
	var sentAt pgtype.Timestamptz

	if err := row.Scan(
		&value.ID,
		&value.AlertType,
		&value.DedupeKey,
		&resellerID,
		&routeID,
		&value.Message,
		&status,
		&errorText,
		&value.CreatedAt,
		&sentAt,
	); err != nil {
		return domain.TelegramAlert{},
			normalizeRegistryReadError(err)
	}

	value.ResellerID = optionalText(resellerID)
	value.RouteID = optionalText(routeID)
	value.Status = domain.TelegramAlertStatus(status)
	value.Error = optionalText(errorText)
	value.CreatedAt = operationalTime(value.CreatedAt)
	value.SentAt = optionalTime(sentAt)

	if err := validateTelegramAlertPersistence(value); err != nil {
		return domain.TelegramAlert{}, err
	}
	return value, nil
}

func sameTelegramAlert(
	left domain.TelegramAlert,
	right domain.TelegramAlert,
) bool {
	return left.ID == right.ID &&
		left.AlertType == right.AlertType &&
		left.DedupeKey == right.DedupeKey &&
		left.ResellerID == right.ResellerID &&
		left.RouteID == right.RouteID &&
		left.Message == right.Message &&
		left.Status == right.Status &&
		left.Error == right.Error &&
		operationalTime(left.CreatedAt).Equal(
			operationalTime(right.CreatedAt),
		) &&
		sameOperationalTimePointer(left.SentAt, right.SentAt)
}

func sameTelegramAlertIdentity(
	left domain.TelegramAlert,
	right domain.TelegramAlert,
) bool {
	return left.ID == right.ID &&
		left.AlertType == right.AlertType &&
		left.DedupeKey == right.DedupeKey &&
		left.ResellerID == right.ResellerID &&
		left.RouteID == right.RouteID &&
		left.Message == right.Message &&
		operationalTime(left.CreatedAt).Equal(
			operationalTime(right.CreatedAt),
		)
}
