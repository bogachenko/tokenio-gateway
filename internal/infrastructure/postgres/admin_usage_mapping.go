package postgres

import (
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func canonicalAdminUsageRecord(
	value domain.UsageRecord,
) domain.UsageRecord {
	result := value
	result.CreatedAt = postgresAdminTime(value.CreatedAt)
	result.ReservedAt = canonicalAdminRouteTimePointer(
		value.ReservedAt,
	)
	result.ReleasedAt = canonicalAdminRouteTimePointer(
		value.ReleasedAt,
	)
	result.BillableAt = canonicalAdminRouteTimePointer(
		value.BillableAt,
	)
	result.ChargedAt = canonicalAdminRouteTimePointer(
		value.ChargedAt,
	)
	result.FailedAt = canonicalAdminRouteTimePointer(
		value.FailedAt,
	)
	result.UpdatedAt = postgresAdminTime(value.UpdatedAt)
	return result
}

func adminUsageApplicationState(
	value domain.UsageRecord,
) domain.AuditState {
	return domain.AuditState{
		"local_request_id":           value.LocalRequestID,
		"user_id":                    value.UserID,
		"api_family":                 value.APIFamily,
		"endpoint_kind":              value.EndpointKind,
		"client_model":               value.ClientModel,
		"billing_model":              value.BillingModel,
		"selected_reseller_id":       value.SelectedResellerID,
		"selected_route_id":          value.SelectedRouteID,
		"provider_type":              value.ProviderType,
		"provider_model":             value.ProviderModel,
		"usage":                      value.Usage,
		"usage_completeness":         value.UsageCompleteness,
		"client_amount_cents":        value.ClientAmountCents,
		"charged_amount_cents":       value.ChargedAmountCents,
		"remaining_amount_cents":     value.RemainingAmountCents,
		"actual_upstream_cost_cents": value.ActualUpstreamCostCents,
		"currency":                   value.Currency,
		"status":                     value.Status,
		"failure_reason":             value.FailureReason,
		"billing_charge_request_id":  value.BillingChargeRequestID,
		"created_at":                 value.CreatedAt,
		"reserved_at":                value.ReservedAt,
		"released_at":                value.ReleasedAt,
		"billable_at":                value.BillableAt,
		"charged_at":                 value.ChargedAt,
		"failed_at":                  value.FailedAt,
		"updated_at":                 value.UpdatedAt,
	}
}

func adminUsageState(value domain.UsageRecord) domain.AuditState {
	return adminUsageApplicationState(
		canonicalAdminUsageRecord(value),
	)
}

func validateAdminUsageResolution(
	expected domain.UsageRecord,
	next domain.UsageRecord,
	action domain.AuditAction,
) error {
	if expected.Status != domain.UsageStatusPricingFailed ||
		expected.LocalRequestID != next.LocalRequestID ||
		!sameAdminUsageImmutable(expected, next) ||
		!isAdminUTCTime(next.UpdatedAt) ||
		!postgresAdminTime(next.UpdatedAt).After(
			postgresAdminTime(expected.UpdatedAt),
		) {
		return ports.ErrStoreContractViolation
	}

	switch action {
	case domain.AuditActionUsageResolveBillable:
		if next.Status != domain.UsageStatusBillable ||
			!validResolvedUsageCompleteness(
				next.UsageCompleteness,
			) ||
			next.BillingChargeRequestID != "" ||
			next.BillableAt == nil ||
			next.ChargedAt != nil ||
			next.FailedAt != nil ||
			next.FailureReason != "" ||
			next.ChargedAmountCents != 0 ||
			next.RemainingAmountCents !=
				next.ClientAmountCents {
			return ports.ErrStoreContractViolation
		}
	case domain.AuditActionUsageResolveFailed:
		if next.Status != domain.UsageStatusFailed ||
			next.FailureReason == "" ||
			next.BillingChargeRequestID != "" ||
			next.ClientAmountCents != 0 ||
			next.ChargedAmountCents != 0 ||
			next.RemainingAmountCents != 0 ||
			next.BillableAt != nil ||
			next.ChargedAt != nil ||
			next.FailedAt == nil {
			return ports.ErrStoreContractViolation
		}
	case domain.AuditActionUsageResolveCharged:
		if next.Status != domain.UsageStatusCharged ||
			!validResolvedUsageCompleteness(
				next.UsageCompleteness,
			) ||
			next.BillingChargeRequestID == "" ||
			next.ClientAmountCents < 0 ||
			next.ChargedAmountCents !=
				next.ClientAmountCents ||
			next.RemainingAmountCents != 0 ||
			next.BillableAt == nil ||
			next.ChargedAt == nil ||
			next.FailedAt != nil ||
			next.FailureReason != "" {
			return ports.ErrStoreContractViolation
		}
	default:
		return ports.ErrStoreContractViolation
	}

	if !nonNegativeUsage(next.EstimatedUsage) ||
		!nonNegativeUsage(next.Usage) ||
		next.EstimatedClientAmountCents < 0 ||
		next.EstimatedUpstreamCostCents < 0 ||
		next.ClientAmountCents < 0 ||
		next.ChargedAmountCents < 0 ||
		next.RemainingAmountCents < 0 ||
		next.ActualUpstreamCostCents < 0 ||
		next.Currency != "RUB" ||
		!validUsageCompleteness(next.UsageCompleteness) {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func validResolvedUsageCompleteness(value string) bool {
	switch value {
	case "detailed", "aggregate", "estimated":
		return true
	default:
		return false
	}
}

func sameAdminUsageImmutable(
	left domain.UsageRecord,
	right domain.UsageRecord,
) bool {
	return left.LocalRequestID == right.LocalRequestID &&
		left.IdempotencyKey == right.IdempotencyKey &&
		left.UserID == right.UserID &&
		left.APIKeyID == right.APIKeyID &&
		left.APIFamily == right.APIFamily &&
		left.EndpointKind == right.EndpointKind &&
		left.ClientModel == right.ClientModel &&
		left.BillingModel == right.BillingModel &&
		left.SelectedRouteID == right.SelectedRouteID &&
		left.SelectedResellerID == right.SelectedResellerID &&
		left.ProviderType == right.ProviderType &&
		left.ProviderModel == right.ProviderModel &&
		left.ProviderRequestID == right.ProviderRequestID &&
		left.ProviderResponseModel ==
			right.ProviderResponseModel &&
		left.EstimatedUsage == right.EstimatedUsage &&
		left.EstimatedClientAmountCents ==
			right.EstimatedClientAmountCents &&
		left.EstimatedUpstreamCostCents ==
			right.EstimatedUpstreamCostCents &&
		postgresAdminTime(left.CreatedAt).Equal(
			postgresAdminTime(right.CreatedAt),
		) &&
		sameAdminTimePointer(
			left.ReservedAt,
			right.ReservedAt,
		) &&
		sameAdminTimePointer(
			left.ReleasedAt,
			right.ReleasedAt,
		)
}

func canonicalUsageAudit(
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

func canonicalAdminBillingBatch(
	value domain.BillingChargeBatch,
) domain.BillingChargeBatch {
	result := value
	result.CreatedAt = postgresAdminTime(value.CreatedAt)
	result.ChargedAt = canonicalAdminRouteTimePointer(
		value.ChargedAt,
	)
	result.FailedAt = canonicalAdminRouteTimePointer(
		value.FailedAt,
	)
	result.UpdatedAt = postgresAdminTime(value.UpdatedAt)
	return result
}

func adminBillingBatchApplicationState(
	value domain.BillingChargeBatch,
) domain.AuditState {
	return domain.AuditState{
		"id":                             value.ID,
		"user_id":                        value.UserID,
		"billing_subject_user_id":        value.BillingSubjectUserID,
		"provider_type":                  value.ProviderType,
		"client_model":                   value.ClientModel,
		"billing_model":                  value.BillingModel,
		"input_tokens":                   value.InputTokens,
		"output_tokens":                  value.OutputTokens,
		"amount_cents":                   value.AmountCents,
		"currency":                       value.Currency,
		"billing_status":                 value.Status,
		"billing_response_balance_cents": value.BillingResponseBalanceCents,
		"billing_error_code":             value.BillingErrorCode,
		"created_at":                     value.CreatedAt,
		"charged_at":                     value.ChargedAt,
		"failed_at":                      value.FailedAt,
		"updated_at":                     value.UpdatedAt,
	}
}

func adminBillingBatchState(
	value domain.BillingChargeBatch,
) domain.AuditState {
	return adminBillingBatchApplicationState(
		canonicalAdminBillingBatch(value),
	)
}

func sameAdminBillingBatch(
	left domain.BillingChargeBatch,
	right domain.BillingChargeBatch,
) bool {
	return left.ID == right.ID &&
		left.UserID == right.UserID &&
		left.BillingSubjectUserID ==
			right.BillingSubjectUserID &&
		left.ProviderType == right.ProviderType &&
		left.ClientModel == right.ClientModel &&
		left.BillingModel == right.BillingModel &&
		left.InputTokens == right.InputTokens &&
		left.OutputTokens == right.OutputTokens &&
		left.AmountCents == right.AmountCents &&
		left.Currency == right.Currency &&
		left.Status == right.Status &&
		sameOptionalInt64(
			left.BillingResponseBalanceCents,
			right.BillingResponseBalanceCents,
		) &&
		left.BillingErrorCode == right.BillingErrorCode &&
		postgresAdminTime(left.CreatedAt).Equal(
			postgresAdminTime(right.CreatedAt),
		) &&
		samePostgresAdminTimePointer(
			left.ChargedAt,
			right.ChargedAt,
		) &&
		samePostgresAdminTimePointer(
			left.FailedAt,
			right.FailedAt,
		) &&
		postgresAdminTime(left.UpdatedAt).Equal(
			postgresAdminTime(right.UpdatedAt),
		)
}

func samePostgresAdminTimePointer(
	left *time.Time,
	right *time.Time,
) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return postgresAdminTime(*left).Equal(
			postgresAdminTime(*right),
		)
	}
}

func sameAdminChargeSnapshot(
	left ports.BillingChargeBatchSnapshot,
	right ports.BillingChargeBatchSnapshot,
) bool {
	if !sameAdminBillingBatch(left.Batch, right.Batch) ||
		len(left.Allocations) != len(right.Allocations) ||
		len(left.ExpectedRecords) != len(right.ExpectedRecords) {
		return false
	}
	for index := range left.Allocations {
		leftAllocation := left.Allocations[index]
		rightAllocation := right.Allocations[index]
		if !sameAllocationCommand(
			leftAllocation,
			rightAllocation,
		) ||
			!postgresAdminTime(leftAllocation.CreatedAt).Equal(
				postgresAdminTime(
					rightAllocation.CreatedAt,
				),
			) ||
			!sameUsageRecord(
				left.ExpectedRecords[index],
				right.ExpectedRecords[index],
			) {
			return false
		}
	}
	return true
}

func billingRetryAfterSuccess(
	before domain.BillingChargeBatch,
	success ports.UsageChargeSuccess,
) domain.BillingChargeBatch {
	after := before
	after.Status = domain.BillingChargeStatusSucceeded
	after.BillingResponseBalanceCents =
		success.BillingBalanceCents
	after.BillingErrorCode = ""
	chargedAt := success.ChargedAt
	after.ChargedAt = &chargedAt
	after.FailedAt = nil
	after.UpdatedAt = success.ChargedAt
	return after
}

func billingRetryAfterFailure(
	before domain.BillingChargeBatch,
	errorCode string,
	failedAt time.Time,
) domain.BillingChargeBatch {
	after := before
	after.Status = domain.BillingChargeStatusFailed
	after.BillingResponseBalanceCents = nil
	after.BillingErrorCode = errorCode
	after.ChargedAt = nil
	after.FailedAt = &failedAt
	after.UpdatedAt = failedAt
	return after
}

func canonicalBillingRetryAudit(
	audit domain.AuditContext,
	before domain.BillingChargeBatch,
	after domain.BillingChargeBatch,
) domain.AuditContext {
	result := audit
	result.BeforeState = adminBillingBatchState(before)
	result.AfterState = adminBillingBatchState(after)
	result.CreatedAt = postgresAdminTime(audit.CreatedAt)
	return result
}

func canonicalBillingRetryAuditInput(
	audit domain.AuditContext,
) domain.AuditContext {
	result := audit
	result.BeforeState = canonicalBillingRetryAuditInputState(
		audit.BeforeState,
	)
	result.AfterState = canonicalBillingRetryAuditInputState(
		audit.AfterState,
	)
	result.CreatedAt = postgresAdminTime(audit.CreatedAt)
	return result
}

func canonicalBillingRetryAuditInputState(
	state domain.AuditState,
) domain.AuditState {
	result := make(domain.AuditState, len(state))
	for key, value := range state {
		switch key {
		case "created_at", "updated_at":
			if typed, ok := value.(time.Time); ok {
				result[key] = postgresAdminTime(typed)
				continue
			}
		case "charged_at", "failed_at":
			switch typed := value.(type) {
			case *time.Time:
				result[key] =
					canonicalAdminRouteTimePointer(typed)
				continue
			case time.Time:
				canonical := postgresAdminTime(typed)
				result[key] = canonical
				continue
			case nil:
				result[key] = nil
				continue
			}
		}
		result[key] = value
	}
	return result
}
