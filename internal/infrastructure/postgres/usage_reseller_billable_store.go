package postgres

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

const updateResellerBalanceReconcileSQL = `
UPDATE tokenio_resellers
SET
    balance_cents = $2,
    reserved_cents = $3,
    updated_at = GREATEST(updated_at, $4)
WHERE id = $1
RETURNING
` + resellerBalanceColumns

var _ ports.UsageResellerBillableStore = (*UsageLedger)(nil)

func (r *UsageLedger) CommitReservedUsageAndReconcileReseller(
	ctx context.Context,
	localRequestID string,
	next domain.UsageRecord,
) (ports.UsageResellerBillableResult, error) {
	if err := validateUsageResellerBillableInput(
		localRequestID,
		next,
	); err != nil {
		return ports.UsageResellerBillableResult{}, err
	}
	canonicalNext := canonicalUsageBillableRecord(next)

	var result ports.UsageResellerBillableResult
	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{},
		func(tx pgx.Tx) error {
			current, err := scanUsageRecord(tx.QueryRow(
				ctx,
				findUsageByLocalRequestIDForUpdateSQL,
				localRequestID,
			))
			if err != nil {
				return err
			}

			if current.Status != domain.UsageStatusReserved {
				result = ports.UsageResellerBillableResult{
					Applied: false,
					Usage:   current,
				}
				return nil
			}
			if err := validatePersistedReservedUsageForReconciliation(
				current,
			); err != nil {
				return err
			}
			if !sameUsageReservationIdentity(current, canonicalNext) ||
				canonicalNext.UpdatedAt.Before(current.UpdatedAt) {
				return ports.ErrStoreContractViolation
			}

			reseller, err := scanReseller(tx.QueryRow(
				ctx,
				findResellerBalanceForUpdateSQL,
				current.SelectedResellerID,
			))
			if err != nil {
				return err
			}
			if reseller.ReservedCents <
				current.EstimatedUpstreamCostCents {
				return ports.ErrStoreContractViolation
			}
			if canonicalNext.ActualUpstreamCostCents > 0 &&
				reseller.BalanceCents < math.MinInt64+
					canonicalNext.ActualUpstreamCostCents {
				return ports.ErrStoreContractViolation
			}

			expectedReseller := reseller
			expectedReseller.ReservedCents -=
				current.EstimatedUpstreamCostCents
			expectedReseller.BalanceCents -=
				canonicalNext.ActualUpstreamCostCents
			if canonicalNext.UpdatedAt.After(
				expectedReseller.UpdatedAt,
			) {
				expectedReseller.UpdatedAt = canonicalNext.UpdatedAt
			}

			persistedReseller, err := scanReseller(tx.QueryRow(
				ctx,
				updateResellerBalanceReconcileSQL,
				current.SelectedResellerID,
				expectedReseller.BalanceCents,
				expectedReseller.ReservedCents,
				canonicalNext.UpdatedAt,
			))
			if err != nil {
				return err
			}
			if !sameResellerBalanceSnapshot(
				persistedReseller,
				expectedReseller,
			) {
				return ports.ErrStoreContractViolation
			}

			args := usageRecordNamedArgs(canonicalNext)
			args["lookup_local_request_id"] = localRequestID
			args["expected_status"] = string(
				domain.UsageStatusReserved,
			)

			tag, err := tx.Exec(ctx, updateUsageRecordCASQL, args)
			if err != nil {
				return NormalizeError(err)
			}
			if tag.RowsAffected() != 1 {
				return ports.ErrStoreConflict
			}

			result = ports.UsageResellerBillableResult{
				Applied:  true,
				Usage:    canonicalNext,
				Reseller: persistedReseller,
			}
			return nil
		},
	)
	if err != nil {
		return ports.UsageResellerBillableResult{}, err
	}
	return result, nil
}

func validateUsageResellerBillableInput(
	localRequestID string,
	next domain.UsageRecord,
) error {
	if strings.TrimSpace(localRequestID) == "" ||
		next.LocalRequestID != localRequestID ||
		next.Status != domain.UsageStatusBillable ||
		next.Currency != "RUB" ||
		next.BillableAt == nil ||
		next.CreatedAt.IsZero() ||
		next.UpdatedAt.IsZero() ||
		next.ReservedAt == nil ||
		next.CreatedAt.Location() != time.UTC ||
		next.UpdatedAt.Location() != time.UTC ||
		next.ReservedAt.Location() != time.UTC ||
		next.BillableAt.Location() != time.UTC ||
		!next.BillableAt.Equal(next.UpdatedAt) ||
		next.UpdatedAt.Before(next.CreatedAt) ||
		next.ReleasedAt != nil ||
		next.ChargedAt != nil ||
		next.FailedAt != nil ||
		strings.TrimSpace(next.FailureReason) != "" ||
		strings.TrimSpace(next.BillingChargeRequestID) != "" ||
		next.ClientAmountCents < 0 ||
		next.ChargedAmountCents != 0 ||
		next.RemainingAmountCents != next.ClientAmountCents ||
		next.ActualUpstreamCostCents < 0 ||
		!validBillableUsageCompleteness(next.UsageCompleteness) ||
		!nonNegativeTokenUsage(next.Usage) {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func validatePersistedReservedUsageForReconciliation(
	current domain.UsageRecord,
) error {
	if current.Status != domain.UsageStatusReserved ||
		strings.TrimSpace(current.SelectedResellerID) == "" ||
		current.EstimatedUpstreamCostCents < 0 ||
		current.ReservedAt == nil ||
		current.BillableAt != nil ||
		current.ReleasedAt != nil ||
		current.ChargedAt != nil ||
		current.FailedAt != nil ||
		!nonNegativeTokenUsage(current.EstimatedUsage) ||
		current.ClientAmountCents != 0 ||
		current.ChargedAmountCents != 0 ||
		current.RemainingAmountCents != 0 ||
		current.ActualUpstreamCostCents != 0 ||
		current.Usage != (domain.TokenUsage{}) ||
		current.ProviderRequestID != "" ||
		current.ProviderResponseModel != "" ||
		current.FailureReason != "" ||
		current.BillingChargeRequestID != "" {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func sameUsageReservationIdentity(
	current domain.UsageRecord,
	next domain.UsageRecord,
) bool {
	return current.LocalRequestID == next.LocalRequestID &&
		current.IdempotencyKey == next.IdempotencyKey &&
		current.UserID == next.UserID &&
		current.APIKeyID == next.APIKeyID &&
		current.APIFamily == next.APIFamily &&
		current.EndpointKind == next.EndpointKind &&
		current.ClientModel == next.ClientModel &&
		current.BillingModel == next.BillingModel &&
		current.SelectedResellerID == next.SelectedResellerID &&
		current.SelectedRouteID == next.SelectedRouteID &&
		current.ProviderType == next.ProviderType &&
		current.ProviderModel == next.ProviderModel &&
		current.EstimatedUsage == next.EstimatedUsage &&
		current.EstimatedClientAmountCents ==
			next.EstimatedClientAmountCents &&
		current.EstimatedUpstreamCostCents ==
			next.EstimatedUpstreamCostCents &&
		current.Currency == next.Currency &&
		current.CreatedAt.Equal(next.CreatedAt) &&
		sameUsageBillableTimePointer(
			current.ReservedAt,
			next.ReservedAt,
		)
}

func canonicalUsageBillableRecord(
	value domain.UsageRecord,
) domain.UsageRecord {
	result := value
	result.CreatedAt = postgresUsageTime(value.CreatedAt)
	result.UpdatedAt = postgresUsageTime(value.UpdatedAt)
	result.ReservedAt = postgresUsageTimePointer(value.ReservedAt)
	result.ReleasedAt = postgresUsageTimePointer(value.ReleasedAt)
	result.BillableAt = postgresUsageTimePointer(value.BillableAt)
	result.ChargedAt = postgresUsageTimePointer(value.ChargedAt)
	result.FailedAt = postgresUsageTimePointer(value.FailedAt)
	return result
}

func postgresUsageTime(value time.Time) time.Time {
	if value.IsZero() {
		return value
	}
	return value.UTC().Truncate(time.Microsecond)
}

func postgresUsageTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	canonical := postgresUsageTime(*value)
	return &canonical
}

func sameUsageBillableTimePointer(
	left *time.Time,
	right *time.Time,
) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return left.Equal(*right)
	}
}

func validBillableUsageCompleteness(value string) bool {
	switch value {
	case "detailed", "aggregate", "estimated":
		return true
	default:
		return false
	}
}

func nonNegativeTokenUsage(value domain.TokenUsage) bool {
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
