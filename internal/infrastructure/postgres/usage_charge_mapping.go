package postgres

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5/pgtype"
)

const billingChargeBatchColumns = `
    id,
    user_id,
    billing_subject_user_id,
    provider_type,
    client_model,
    billing_model,
    input_tokens,
    output_tokens,
    amount_cents,
    currency,
    billing_status,
    billing_response_balance_cents,
    billing_error_code,
    created_at,
    charged_at,
    failed_at,
    updated_at
`

type positionedAllocation struct {
	Position   int
	Allocation domain.BillingChargeAllocation
}

type positionedExpectedRecord struct {
	Position       int
	LocalRequestID string
	Record         domain.UsageRecord
	CreatedAt      time.Time
}

func scanBillingChargeBatch(row rowScanner) (domain.BillingChargeBatch, error) {
	var value domain.BillingChargeBatch
	var providerType string
	var status string
	var responseBalance pgtype.Int8
	var chargedAt pgtype.Timestamptz
	var failedAt pgtype.Timestamptz

	if err := row.Scan(
		&value.ID,
		&value.UserID,
		&value.BillingSubjectUserID,
		&providerType,
		&value.ClientModel,
		&value.BillingModel,
		&value.InputTokens,
		&value.OutputTokens,
		&value.AmountCents,
		&value.Currency,
		&status,
		&responseBalance,
		&value.BillingErrorCode,
		&value.CreatedAt,
		&chargedAt,
		&failedAt,
		&value.UpdatedAt,
	); err != nil {
		return domain.BillingChargeBatch{}, normalizeRegistryReadError(err)
	}

	value.ProviderType = domain.ProviderType(providerType)
	value.Status = domain.BillingChargeStatus(status)
	if responseBalance.Valid {
		balance := responseBalance.Int64
		value.BillingResponseBalanceCents = &balance
	}
	value.CreatedAt = canonicalTime(value.CreatedAt)
	value.ChargedAt = optionalTime(chargedAt)
	value.FailedAt = optionalTime(failedAt)
	value.UpdatedAt = canonicalTime(value.UpdatedAt)

	if err := validatePersistedBatch(value); err != nil {
		return domain.BillingChargeBatch{}, err
	}
	return value, nil
}

func scanPositionedAllocation(
	row rowScanner,
) (positionedAllocation, error) {
	var value positionedAllocation
	if err := row.Scan(
		&value.Allocation.ID,
		&value.Allocation.BatchID,
		&value.Allocation.LocalRequestID,
		&value.Position,
		&value.Allocation.ChargedAmountCents,
		&value.Allocation.RemainingAmountCents,
		&value.Allocation.CreatedAt,
	); err != nil {
		return positionedAllocation{}, normalizeRegistryReadError(err)
	}

	value.Allocation.CreatedAt =
		canonicalTime(value.Allocation.CreatedAt)
	if value.Position < 0 ||
		value.Allocation.ID == "" ||
		value.Allocation.BatchID == "" ||
		value.Allocation.LocalRequestID == "" ||
		value.Allocation.ChargedAmountCents <= 0 ||
		value.Allocation.RemainingAmountCents < 0 ||
		value.Allocation.CreatedAt.IsZero() {
		return positionedAllocation{}, ports.ErrStoreContractViolation
	}
	return value, nil
}

func scanPositionedExpectedRecord(
	row rowScanner,
) (positionedExpectedRecord, error) {
	var value positionedExpectedRecord
	var raw []byte

	if err := row.Scan(
		&value.LocalRequestID,
		&value.Position,
		&raw,
		&value.CreatedAt,
	); err != nil {
		return positionedExpectedRecord{}, normalizeRegistryReadError(err)
	}

	record, err := decodeExpectedRecord(raw)
	if err != nil {
		return positionedExpectedRecord{}, err
	}
	value.Record = record
	value.CreatedAt = canonicalTime(value.CreatedAt)

	if value.Position < 0 ||
		value.LocalRequestID == "" ||
		value.CreatedAt.IsZero() ||
		value.Record.LocalRequestID != value.LocalRequestID {
		return positionedExpectedRecord{}, ports.ErrStoreContractViolation
	}
	return value, nil
}

func validatePersistedBatch(value domain.BillingChargeBatch) error {
	if value.ID == "" ||
		value.UserID == "" ||
		value.BillingSubjectUserID == "" ||
		!validChargeProviderType(value.ProviderType) ||
		value.ClientModel == "" ||
		value.BillingModel == "" ||
		value.InputTokens < 0 ||
		value.OutputTokens < 0 ||
		value.AmountCents <= 0 ||
		value.Currency != "RUB" ||
		value.CreatedAt.IsZero() ||
		value.UpdatedAt.IsZero() {
		return ports.ErrStoreContractViolation
	}
	if value.BillingResponseBalanceCents != nil &&
		*value.BillingResponseBalanceCents < 0 {
		return ports.ErrStoreContractViolation
	}

	switch value.Status {
	case domain.BillingChargeStatusPending:
		if value.ChargedAt != nil ||
			value.FailedAt != nil ||
			value.BillingErrorCode != "" {
			return ports.ErrStoreContractViolation
		}
	case domain.BillingChargeStatusFailed:
		if value.ChargedAt != nil ||
			value.FailedAt == nil ||
			value.BillingErrorCode == "" {
			return ports.ErrStoreContractViolation
		}
	case domain.BillingChargeStatusSucceeded:
		if value.ChargedAt == nil ||
			value.FailedAt != nil ||
			value.BillingErrorCode != "" {
			return ports.ErrStoreContractViolation
		}
	default:
		return ports.ErrStoreContractViolation
	}
	return nil
}

func canonicalUsageRecord(value domain.UsageRecord) domain.UsageRecord {
	result := value
	result.CreatedAt = canonicalTime(result.CreatedAt)
	result.ReservedAt = cloneCanonicalTime(result.ReservedAt)
	result.ReleasedAt = cloneCanonicalTime(result.ReleasedAt)
	result.BillableAt = cloneCanonicalTime(result.BillableAt)
	result.ChargedAt = cloneCanonicalTime(result.ChargedAt)
	result.FailedAt = cloneCanonicalTime(result.FailedAt)
	result.UpdatedAt = canonicalTime(result.UpdatedAt)
	return result
}

func isCanonicalUTCTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC
}

func cloneCanonicalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	canonical := canonicalTime(*value)
	return &canonical
}

func postClaimRecord(
	value domain.UsageRecord,
	batchID string,
) domain.UsageRecord {
	result := canonicalUsageRecord(value)
	result.BillingChargeRequestID = batchID
	return result
}

func sameUsageRecord(
	left domain.UsageRecord,
	right domain.UsageRecord,
) bool {
	return reflect.DeepEqual(
		canonicalUsageRecord(left),
		canonicalUsageRecord(right),
	)
}

func sameBatchCommand(
	left domain.BillingChargeBatch,
	right domain.BillingChargeBatch,
) bool {
	return left.ID == right.ID &&
		left.UserID == right.UserID &&
		left.BillingSubjectUserID == right.BillingSubjectUserID &&
		left.ProviderType == right.ProviderType &&
		left.ClientModel == right.ClientModel &&
		left.BillingModel == right.BillingModel &&
		left.InputTokens == right.InputTokens &&
		left.OutputTokens == right.OutputTokens &&
		left.AmountCents == right.AmountCents &&
		left.Currency == right.Currency
}

func sameAllocationCommand(
	left domain.BillingChargeAllocation,
	right domain.BillingChargeAllocation,
) bool {
	return left.ID == right.ID &&
		left.BatchID == right.BatchID &&
		left.LocalRequestID == right.LocalRequestID &&
		left.ChargedAmountCents == right.ChargedAmountCents &&
		left.RemainingAmountCents == right.RemainingAmountCents
}

func sameOptionalInt64(left *int64, right *int64) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return *left == *right
	}
}

type tokenUsageJSON struct {
	InputTokens          *int64 `json:"input_tokens"`
	CachedInputTokens    *int64 `json:"cached_input_tokens"`
	OutputTokens         *int64 `json:"output_tokens"`
	ReasoningTokens      *int64 `json:"reasoning_tokens"`
	ImageInputTokens     *int64 `json:"image_input_tokens"`
	AudioInputTokens     *int64 `json:"audio_input_tokens"`
	AudioOutputTokens    *int64 `json:"audio_output_tokens"`
	FileInputTokens      *int64 `json:"file_input_tokens"`
	VideoInputTokens     *int64 `json:"video_input_tokens"`
	ImageGenerationUnits *int64 `json:"image_generation_units"`
}

type usageRecordJSON struct {
	LocalRequestID *string `json:"local_request_id"`
	IdempotencyKey *string `json:"idempotency_key,omitempty"`

	UserID   *string `json:"user_id"`
	APIKeyID *string `json:"api_key_id"`

	APIFamily    *domain.APIFamily    `json:"api_family"`
	EndpointKind *domain.EndpointKind `json:"endpoint_kind"`

	ClientModel  *string `json:"client_model"`
	BillingModel *string `json:"billing_model"`

	SelectedRouteID    *string `json:"selected_route_id"`
	SelectedResellerID *string `json:"selected_reseller_id"`

	ProviderType  *domain.ProviderType `json:"provider_type"`
	ProviderModel *string              `json:"provider_model"`

	ProviderRequestID     *string `json:"provider_request_id,omitempty"`
	ProviderResponseModel *string `json:"provider_response_model,omitempty"`

	EstimatedUsage *tokenUsageJSON `json:"estimated_usage"`
	Usage          *tokenUsageJSON `json:"usage"`

	EstimatedClientAmountCents *int64 `json:"estimated_client_amount_cents"`
	EstimatedUpstreamCostCents *int64 `json:"estimated_upstream_cost_cents"`

	ClientAmountCents       *int64 `json:"client_amount_cents"`
	ChargedAmountCents      *int64 `json:"charged_amount_cents"`
	RemainingAmountCents    *int64 `json:"remaining_amount_cents"`
	ActualUpstreamCostCents *int64 `json:"actual_upstream_cost_cents"`

	Currency          *string             `json:"currency"`
	UsageCompleteness *string             `json:"usage_completeness"`
	Status            *domain.UsageStatus `json:"status"`

	FailureReason          *string `json:"failure_reason,omitempty"`
	BillingChargeRequestID *string `json:"billing_charge_request_id,omitempty"`

	CreatedAt  *time.Time `json:"created_at"`
	ReservedAt *time.Time `json:"reserved_at,omitempty"`
	ReleasedAt *time.Time `json:"released_at,omitempty"`
	BillableAt *time.Time `json:"billable_at,omitempty"`
	ChargedAt  *time.Time `json:"charged_at,omitempty"`
	FailedAt   *time.Time `json:"failed_at,omitempty"`
	UpdatedAt  *time.Time `json:"updated_at"`
}

func encodeExpectedRecord(
	value domain.UsageRecord,
) ([]byte, error) {
	body, err := json.Marshal(canonicalUsageRecord(value))
	if err != nil {
		return nil, ports.ErrStoreContractViolation
	}
	if _, err := decodeExpectedRecord(body); err != nil {
		return nil, err
	}
	return body, nil
}

func decodeExpectedRecord(raw []byte) (domain.UsageRecord, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) < 2 ||
		trimmed[0] != '{' ||
		trimmed[len(trimmed)-1] != '}' {
		return domain.UsageRecord{}, ports.ErrStoreContractViolation
	}

	var dto usageRecordJSON
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&dto); err != nil {
		return domain.UsageRecord{}, ports.ErrStoreContractViolation
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return domain.UsageRecord{}, ports.ErrStoreContractViolation
	}
	return dto.domainRecord()
}

func (dto usageRecordJSON) domainRecord() (domain.UsageRecord, error) {
	required := []bool{
		dto.LocalRequestID != nil,
		dto.UserID != nil,
		dto.APIKeyID != nil,
		dto.APIFamily != nil,
		dto.EndpointKind != nil,
		dto.ClientModel != nil,
		dto.BillingModel != nil,
		dto.SelectedRouteID != nil,
		dto.SelectedResellerID != nil,
		dto.ProviderType != nil,
		dto.ProviderModel != nil,
		dto.EstimatedUsage != nil,
		dto.Usage != nil,
		dto.EstimatedClientAmountCents != nil,
		dto.EstimatedUpstreamCostCents != nil,
		dto.ClientAmountCents != nil,
		dto.ChargedAmountCents != nil,
		dto.RemainingAmountCents != nil,
		dto.ActualUpstreamCostCents != nil,
		dto.Currency != nil,
		dto.UsageCompleteness != nil,
		dto.Status != nil,
		dto.BillingChargeRequestID != nil,
		dto.CreatedAt != nil,
		dto.UpdatedAt != nil,
	}
	for _, present := range required {
		if !present {
			return domain.UsageRecord{}, ports.ErrStoreContractViolation
		}
	}

	estimated, err := dto.EstimatedUsage.domainUsage()
	if err != nil {
		return domain.UsageRecord{}, err
	}
	actual, err := dto.Usage.domainUsage()
	if err != nil {
		return domain.UsageRecord{}, err
	}

	value := domain.UsageRecord{
		LocalRequestID:             *dto.LocalRequestID,
		UserID:                     *dto.UserID,
		APIKeyID:                   *dto.APIKeyID,
		APIFamily:                  *dto.APIFamily,
		EndpointKind:               *dto.EndpointKind,
		ClientModel:                *dto.ClientModel,
		BillingModel:               *dto.BillingModel,
		SelectedRouteID:            *dto.SelectedRouteID,
		SelectedResellerID:         *dto.SelectedResellerID,
		ProviderType:               *dto.ProviderType,
		ProviderModel:              *dto.ProviderModel,
		EstimatedUsage:             estimated,
		Usage:                      actual,
		EstimatedClientAmountCents: *dto.EstimatedClientAmountCents,
		EstimatedUpstreamCostCents: *dto.EstimatedUpstreamCostCents,
		ClientAmountCents:          *dto.ClientAmountCents,
		ChargedAmountCents:         *dto.ChargedAmountCents,
		RemainingAmountCents:       *dto.RemainingAmountCents,
		ActualUpstreamCostCents:    *dto.ActualUpstreamCostCents,
		Currency:                   *dto.Currency,
		UsageCompleteness:          *dto.UsageCompleteness,
		Status:                     *dto.Status,
		BillingChargeRequestID:     *dto.BillingChargeRequestID,
		CreatedAt:                  canonicalTime(*dto.CreatedAt),
		ReservedAt:                 cloneCanonicalTime(dto.ReservedAt),
		ReleasedAt:                 cloneCanonicalTime(dto.ReleasedAt),
		BillableAt:                 cloneCanonicalTime(dto.BillableAt),
		ChargedAt:                  cloneCanonicalTime(dto.ChargedAt),
		FailedAt:                   cloneCanonicalTime(dto.FailedAt),
		UpdatedAt:                  canonicalTime(*dto.UpdatedAt),
	}
	if dto.IdempotencyKey != nil {
		value.IdempotencyKey = *dto.IdempotencyKey
	}
	if dto.ProviderRequestID != nil {
		value.ProviderRequestID = *dto.ProviderRequestID
	}
	if dto.ProviderResponseModel != nil {
		value.ProviderResponseModel = *dto.ProviderResponseModel
	}
	if dto.FailureReason != nil {
		value.FailureReason = *dto.FailureReason
	}

	if err := validateExpectedRecordPersistence(value); err != nil {
		return domain.UsageRecord{}, err
	}
	return value, nil
}

func (dto tokenUsageJSON) domainUsage() (domain.TokenUsage, error) {
	required := []bool{
		dto.InputTokens != nil,
		dto.CachedInputTokens != nil,
		dto.OutputTokens != nil,
		dto.ReasoningTokens != nil,
		dto.ImageInputTokens != nil,
		dto.AudioInputTokens != nil,
		dto.AudioOutputTokens != nil,
		dto.FileInputTokens != nil,
		dto.VideoInputTokens != nil,
		dto.ImageGenerationUnits != nil,
	}
	for _, present := range required {
		if !present {
			return domain.TokenUsage{}, ports.ErrStoreContractViolation
		}
	}

	value := domain.TokenUsage{
		InputTokens:          *dto.InputTokens,
		CachedInputTokens:    *dto.CachedInputTokens,
		OutputTokens:         *dto.OutputTokens,
		ReasoningTokens:      *dto.ReasoningTokens,
		ImageInputTokens:     *dto.ImageInputTokens,
		AudioInputTokens:     *dto.AudioInputTokens,
		AudioOutputTokens:    *dto.AudioOutputTokens,
		FileInputTokens:      *dto.FileInputTokens,
		VideoInputTokens:     *dto.VideoInputTokens,
		ImageGenerationUnits: *dto.ImageGenerationUnits,
	}
	if !nonNegativeUsage(value) {
		return domain.TokenUsage{}, ports.ErrStoreContractViolation
	}
	return value, nil
}

func validateExpectedRecordPersistence(
	value domain.UsageRecord,
) error {
	if value.LocalRequestID == "" ||
		value.UserID == "" ||
		value.APIKeyID == "" ||
		!validChargeAPIFamily(value.APIFamily) ||
		!validChargeEndpointKind(value.EndpointKind) ||
		value.ClientModel == "" ||
		value.BillingModel == "" ||
		value.SelectedRouteID == "" ||
		value.SelectedResellerID == "" ||
		!validChargeProviderType(value.ProviderType) ||
		value.ProviderModel == "" ||
		value.Currency != "RUB" ||
		!validUsageCompleteness(value.UsageCompleteness) ||
		value.BillingChargeRequestID == "" ||
		value.CreatedAt.IsZero() ||
		value.UpdatedAt.IsZero() ||
		!nonNegativeUsage(value.EstimatedUsage) ||
		!nonNegativeUsage(value.Usage) ||
		value.EstimatedClientAmountCents < 0 ||
		value.EstimatedUpstreamCostCents < 0 ||
		value.ClientAmountCents <= 0 ||
		value.ChargedAmountCents < 0 ||
		value.RemainingAmountCents <= 0 ||
		value.ChargedAmountCents > value.ClientAmountCents ||
		value.RemainingAmountCents !=
			value.ClientAmountCents-value.ChargedAmountCents ||
		value.ActualUpstreamCostCents < 0 {
		return ports.ErrStoreContractViolation
	}

	switch value.Status {
	case
		domain.UsageStatusBillable,
		domain.UsageStatusPartiallyCharged:
	default:
		return ports.ErrStoreContractViolation
	}
	return nil
}

func validChargeProviderType(value domain.ProviderType) bool {
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

func validChargeAPIFamily(value domain.APIFamily) bool {
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

func validChargeEndpointKind(value domain.EndpointKind) bool {
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

func validUsageCompleteness(value string) bool {
	switch value {
	case "detailed", "aggregate", "estimated", "missing", "failed":
		return true
	default:
		return false
	}
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nonNegativeUsage(value domain.TokenUsage) bool {
	return value.InputTokens >= 0 &&
		value.CachedInputTokens >= 0 &&
		value.OutputTokens >= 0 &&
		value.ReasoningTokens >= 0 &&
		value.ImageInputTokens >= 0 &&
		value.AudioInputTokens >= 0 &&
		value.AudioOutputTokens >= 0 &&
		value.FileInputTokens >= 0 &&
		value.VideoInputTokens >= 0 &&
		value.ImageGenerationUnits >= 0
}
