package postgres

import (
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const usageRecordColumns = `
    local_request_id,
    idempotency_key,
    user_id,
    api_key_id,
    api_family,
    endpoint_kind,
    client_model,
    billing_model,
    selected_reseller_id,
    selected_route_id,
    provider_type,
    provider_model,
    provider_request_id,
    provider_response_model,
    estimated_input_tokens,
    estimated_cached_input_tokens,
    estimated_output_tokens,
    estimated_reasoning_tokens,
    estimated_image_input_tokens,
    estimated_audio_input_tokens,
    estimated_audio_output_tokens,
    estimated_file_input_tokens,
    estimated_video_input_tokens,
    estimated_image_generation_units,
    estimated_client_amount_cents,
    estimated_upstream_cost_cents,
    input_tokens,
    cached_input_tokens,
    output_tokens,
    reasoning_tokens,
    image_input_tokens,
    audio_input_tokens,
    audio_output_tokens,
    file_input_tokens,
    video_input_tokens,
    image_generation_units,
    client_amount_cents,
    charged_amount_cents,
    remaining_amount_cents,
    actual_upstream_cost_cents,
    currency,
    usage_completeness,
    status,
    failure_reason,
    billing_charge_request_id,
    created_at,
    reserved_at,
    released_at,
    billable_at,
    charged_at,
    failed_at,
    updated_at
`

const insertUsageRecordSQL = `
INSERT INTO tokenio_usage_records (
` + usageRecordColumns + `
)
VALUES (
    @local_request_id,
    @idempotency_key,
    @user_id,
    @api_key_id,
    @api_family,
    @endpoint_kind,
    @client_model,
    @billing_model,
    @selected_reseller_id,
    @selected_route_id,
    @provider_type,
    @provider_model,
    @provider_request_id,
    @provider_response_model,
    @estimated_input_tokens,
    @estimated_cached_input_tokens,
    @estimated_output_tokens,
    @estimated_reasoning_tokens,
    @estimated_image_input_tokens,
    @estimated_audio_input_tokens,
    @estimated_audio_output_tokens,
    @estimated_file_input_tokens,
    @estimated_video_input_tokens,
    @estimated_image_generation_units,
    @estimated_client_amount_cents,
    @estimated_upstream_cost_cents,
    @input_tokens,
    @cached_input_tokens,
    @output_tokens,
    @reasoning_tokens,
    @image_input_tokens,
    @audio_input_tokens,
    @audio_output_tokens,
    @file_input_tokens,
    @video_input_tokens,
    @image_generation_units,
    @client_amount_cents,
    @charged_amount_cents,
    @remaining_amount_cents,
    @actual_upstream_cost_cents,
    @currency,
    @usage_completeness,
    @status,
    @failure_reason,
    @billing_charge_request_id,
    @created_at,
    @reserved_at,
    @released_at,
    @billable_at,
    @charged_at,
    @failed_at,
    @updated_at
)
`

const updateUsageRecordCASQL = `
UPDATE tokenio_usage_records
SET
    idempotency_key = @idempotency_key,
    user_id = @user_id,
    api_key_id = @api_key_id,
    api_family = @api_family,
    endpoint_kind = @endpoint_kind,
    client_model = @client_model,
    billing_model = @billing_model,
    selected_reseller_id = @selected_reseller_id,
    selected_route_id = @selected_route_id,
    provider_type = @provider_type,
    provider_model = @provider_model,
    provider_request_id = @provider_request_id,
    provider_response_model = @provider_response_model,
    estimated_input_tokens = @estimated_input_tokens,
    estimated_cached_input_tokens = @estimated_cached_input_tokens,
    estimated_output_tokens = @estimated_output_tokens,
    estimated_reasoning_tokens = @estimated_reasoning_tokens,
    estimated_image_input_tokens = @estimated_image_input_tokens,
    estimated_audio_input_tokens = @estimated_audio_input_tokens,
    estimated_audio_output_tokens = @estimated_audio_output_tokens,
    estimated_file_input_tokens = @estimated_file_input_tokens,
    estimated_video_input_tokens = @estimated_video_input_tokens,
    estimated_image_generation_units = @estimated_image_generation_units,
    estimated_client_amount_cents = @estimated_client_amount_cents,
    estimated_upstream_cost_cents = @estimated_upstream_cost_cents,
    input_tokens = @input_tokens,
    cached_input_tokens = @cached_input_tokens,
    output_tokens = @output_tokens,
    reasoning_tokens = @reasoning_tokens,
    image_input_tokens = @image_input_tokens,
    audio_input_tokens = @audio_input_tokens,
    audio_output_tokens = @audio_output_tokens,
    file_input_tokens = @file_input_tokens,
    video_input_tokens = @video_input_tokens,
    image_generation_units = @image_generation_units,
    client_amount_cents = @client_amount_cents,
    charged_amount_cents = @charged_amount_cents,
    remaining_amount_cents = @remaining_amount_cents,
    actual_upstream_cost_cents = @actual_upstream_cost_cents,
    currency = @currency,
    usage_completeness = @usage_completeness,
    status = @status,
    failure_reason = @failure_reason,
    billing_charge_request_id = @billing_charge_request_id,
    created_at = @created_at,
    reserved_at = @reserved_at,
    released_at = @released_at,
    billable_at = @billable_at,
    charged_at = @charged_at,
    failed_at = @failed_at,
    updated_at = @updated_at
WHERE local_request_id = @lookup_local_request_id
  AND status = @expected_status
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUsageRecord(row rowScanner) (domain.UsageRecord, error) {
	var value domain.UsageRecord

	var idempotencyKey pgtype.Text
	var apiKeyID pgtype.Text
	var apiFamily string
	var endpointKind string
	var selectedResellerID pgtype.Text
	var selectedRouteID pgtype.Text
	var providerType string
	var providerRequestID pgtype.Text
	var providerResponseModel pgtype.Text
	var status string
	var failureReason pgtype.Text
	var billingChargeRequestID pgtype.Text

	var reservedAt pgtype.Timestamptz
	var releasedAt pgtype.Timestamptz
	var billableAt pgtype.Timestamptz
	var chargedAt pgtype.Timestamptz
	var failedAt pgtype.Timestamptz

	if err := row.Scan(
		&value.LocalRequestID,
		&idempotencyKey,
		&value.UserID,
		&apiKeyID,
		&apiFamily,
		&endpointKind,
		&value.ClientModel,
		&value.BillingModel,
		&selectedResellerID,
		&selectedRouteID,
		&providerType,
		&value.ProviderModel,
		&providerRequestID,
		&providerResponseModel,
		&value.EstimatedUsage.InputTokens,
		&value.EstimatedUsage.CachedInputTokens,
		&value.EstimatedUsage.OutputTokens,
		&value.EstimatedUsage.ReasoningTokens,
		&value.EstimatedUsage.ImageInputTokens,
		&value.EstimatedUsage.AudioInputTokens,
		&value.EstimatedUsage.AudioOutputTokens,
		&value.EstimatedUsage.FileInputTokens,
		&value.EstimatedUsage.VideoInputTokens,
		&value.EstimatedUsage.ImageGenerationUnits,
		&value.EstimatedClientAmountCents,
		&value.EstimatedUpstreamCostCents,
		&value.Usage.InputTokens,
		&value.Usage.CachedInputTokens,
		&value.Usage.OutputTokens,
		&value.Usage.ReasoningTokens,
		&value.Usage.ImageInputTokens,
		&value.Usage.AudioInputTokens,
		&value.Usage.AudioOutputTokens,
		&value.Usage.FileInputTokens,
		&value.Usage.VideoInputTokens,
		&value.Usage.ImageGenerationUnits,
		&value.ClientAmountCents,
		&value.ChargedAmountCents,
		&value.RemainingAmountCents,
		&value.ActualUpstreamCostCents,
		&value.Currency,
		&value.UsageCompleteness,
		&status,
		&failureReason,
		&billingChargeRequestID,
		&value.CreatedAt,
		&reservedAt,
		&releasedAt,
		&billableAt,
		&chargedAt,
		&failedAt,
		&value.UpdatedAt,
	); err != nil {
		return domain.UsageRecord{}, normalizeRegistryReadError(err)
	}

	value.IdempotencyKey = optionalText(idempotencyKey)
	value.APIKeyID = optionalText(apiKeyID)
	value.APIFamily = domain.APIFamily(apiFamily)
	value.EndpointKind = domain.EndpointKind(endpointKind)
	value.SelectedResellerID = optionalText(selectedResellerID)
	value.SelectedRouteID = optionalText(selectedRouteID)
	value.ProviderType = domain.ProviderType(providerType)
	value.ProviderRequestID = optionalText(providerRequestID)
	value.ProviderResponseModel = optionalText(providerResponseModel)
	value.Status = domain.UsageStatus(status)
	value.FailureReason = optionalText(failureReason)
	value.BillingChargeRequestID = optionalText(billingChargeRequestID)

	value.CreatedAt = canonicalTime(value.CreatedAt)
	value.ReservedAt = optionalTime(reservedAt)
	value.ReleasedAt = optionalTime(releasedAt)
	value.BillableAt = optionalTime(billableAt)
	value.ChargedAt = optionalTime(chargedAt)
	value.FailedAt = optionalTime(failedAt)
	value.UpdatedAt = canonicalTime(value.UpdatedAt)

	return value, nil
}

func usageRecordNamedArgs(record domain.UsageRecord) pgx.NamedArgs {
	return pgx.NamedArgs{
		"local_request_id":                 record.LocalRequestID,
		"idempotency_key":                  nullIfEmpty(record.IdempotencyKey),
		"user_id":                          record.UserID,
		"api_key_id":                       nullIfEmpty(record.APIKeyID),
		"api_family":                       string(record.APIFamily),
		"endpoint_kind":                    string(record.EndpointKind),
		"client_model":                     record.ClientModel,
		"billing_model":                    record.BillingModel,
		"selected_reseller_id":             nullIfEmpty(record.SelectedResellerID),
		"selected_route_id":                nullIfEmpty(record.SelectedRouteID),
		"provider_type":                    string(record.ProviderType),
		"provider_model":                   record.ProviderModel,
		"provider_request_id":              nullIfEmpty(record.ProviderRequestID),
		"provider_response_model":          nullIfEmpty(record.ProviderResponseModel),
		"estimated_input_tokens":           record.EstimatedUsage.InputTokens,
		"estimated_cached_input_tokens":    record.EstimatedUsage.CachedInputTokens,
		"estimated_output_tokens":          record.EstimatedUsage.OutputTokens,
		"estimated_reasoning_tokens":       record.EstimatedUsage.ReasoningTokens,
		"estimated_image_input_tokens":     record.EstimatedUsage.ImageInputTokens,
		"estimated_audio_input_tokens":     record.EstimatedUsage.AudioInputTokens,
		"estimated_audio_output_tokens":    record.EstimatedUsage.AudioOutputTokens,
		"estimated_file_input_tokens":      record.EstimatedUsage.FileInputTokens,
		"estimated_video_input_tokens":     record.EstimatedUsage.VideoInputTokens,
		"estimated_image_generation_units": record.EstimatedUsage.ImageGenerationUnits,
		"estimated_client_amount_cents":    record.EstimatedClientAmountCents,
		"estimated_upstream_cost_cents":    record.EstimatedUpstreamCostCents,
		"input_tokens":                     record.Usage.InputTokens,
		"cached_input_tokens":              record.Usage.CachedInputTokens,
		"output_tokens":                    record.Usage.OutputTokens,
		"reasoning_tokens":                 record.Usage.ReasoningTokens,
		"image_input_tokens":               record.Usage.ImageInputTokens,
		"audio_input_tokens":               record.Usage.AudioInputTokens,
		"audio_output_tokens":              record.Usage.AudioOutputTokens,
		"file_input_tokens":                record.Usage.FileInputTokens,
		"video_input_tokens":               record.Usage.VideoInputTokens,
		"image_generation_units":           record.Usage.ImageGenerationUnits,
		"client_amount_cents":              record.ClientAmountCents,
		"charged_amount_cents":             record.ChargedAmountCents,
		"remaining_amount_cents":           record.RemainingAmountCents,
		"actual_upstream_cost_cents":       record.ActualUpstreamCostCents,
		"currency":                         record.Currency,
		"usage_completeness":               record.UsageCompleteness,
		"status":                           string(record.Status),
		"failure_reason":                   nullIfEmpty(record.FailureReason),
		"billing_charge_request_id":        nullIfEmpty(record.BillingChargeRequestID),
		"created_at":                       canonicalTime(record.CreatedAt),
		"reserved_at":                      canonicalTimePointer(record.ReservedAt),
		"released_at":                      canonicalTimePointer(record.ReleasedAt),
		"billable_at":                      canonicalTimePointer(record.BillableAt),
		"charged_at":                       canonicalTimePointer(record.ChargedAt),
		"failed_at":                        canonicalTimePointer(record.FailedAt),
		"updated_at":                       canonicalTime(record.UpdatedAt),
	}
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func canonicalTimePointer(value *time.Time) any {
	if value == nil {
		return nil
	}
	return canonicalTime(*value)
}
