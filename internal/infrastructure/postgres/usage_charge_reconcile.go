package postgres

import (
	"context"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

func (r *UsageLedger) MarkChargeBatchFailed(
	ctx context.Context,
	batchID string,
	expectedStatus domain.BillingChargeStatus,
	billingErrorCode string,
	failedAt time.Time,
) error {
	if batchID == "" ||
		billingErrorCode == "" ||
		!isCanonicalUTCTime(failedAt) {
		return ports.ErrStoreContractViolation
	}
	failedAt = canonicalTime(failedAt)

	return InTx(
		ctx,
		r.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			batch, err := scanBillingChargeBatch(
				tx.QueryRow(
					ctx,
					findBillingChargeBatchForUpdateSQL,
					batchID,
				),
			)
			if err != nil {
				return err
			}

			switch {
			case batch.Status ==
				domain.BillingChargeStatusPending &&
				expectedStatus ==
					domain.BillingChargeStatusPending:
				tag, err := tx.Exec(
					ctx,
					`
UPDATE tokenio_billing_charge_batches
SET billing_status = 'failed',
    billing_error_code = $2,
    failed_at = $3,
    updated_at = $3
WHERE id = $1
  AND billing_status = 'pending'
`,
					batchID,
					billingErrorCode,
					failedAt,
				)
				if err != nil {
					return NormalizeError(err)
				}
				if tag.RowsAffected() != 1 {
					return ports.ErrStoreConflict
				}
				return nil

			case batch.Status ==
				domain.BillingChargeStatusFailed &&
				expectedStatus ==
					domain.BillingChargeStatusFailed &&
				batch.BillingErrorCode == billingErrorCode:
				return nil

			default:
				return ports.ErrStoreConflict
			}
		},
	)
}

func (r *UsageLedger) ApplyChargeSuccess(
	ctx context.Context,
	success ports.UsageChargeSuccess,
) error {
	if err := validateChargeSuccessPersistence(success); err != nil {
		return err
	}

	return InTx(
		ctx,
		r.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			persisted, err := loadBillingChargeSnapshot(
				ctx,
				tx,
				success.BatchID,
				true,
			)
			if err != nil {
				return err
			}
			if err := compareChargeSuccessMetadata(
				success,
				persisted,
			); err != nil {
				return err
			}

			if persisted.Batch.Status ==
				domain.BillingChargeStatusSucceeded {
				if persisted.Batch.ChargedAt == nil ||
					!persisted.Batch.ChargedAt.Equal(
						success.ChargedAt,
					) ||
					!sameOptionalInt64(
						persisted.Batch.
							BillingResponseBalanceCents,
						success.BillingBalanceCents,
					) {
					return ports.ErrStoreConflict
				}
				return nil
			}
			if persisted.Batch.Status !=
				domain.BillingChargeStatusPending &&
				persisted.Batch.Status !=
					domain.BillingChargeStatusFailed {
				return ports.ErrStoreConflict
			}

			currentByID, err := lockUsageRecordsForCharge(
				ctx,
				tx,
				persisted.ExpectedRecords,
			)
			if err != nil {
				return err
			}

			for index, expected := range persisted.ExpectedRecords {
				current, ok := currentByID[expected.LocalRequestID]
				if !ok || !sameUsageRecord(current, expected) {
					return ports.ErrStoreConflict
				}
				allocation := persisted.Allocations[index]
				if allocation.ChargedAmountCents >
					current.RemainingAmountCents ||
					allocation.RemainingAmountCents !=
						current.RemainingAmountCents-
							allocation.ChargedAmountCents {
					return ports.ErrStoreContractViolation
				}

				next := current
				next.ChargedAmountCents +=
					allocation.ChargedAmountCents
				next.RemainingAmountCents =
					allocation.RemainingAmountCents
				if next.RemainingAmountCents == 0 {
					next.Status = domain.UsageStatusCharged
				} else {
					next.Status =
						domain.UsageStatusPartiallyCharged
				}
				next.ChargedAt =
					cloneCanonicalTime(&success.ChargedAt)
				next.FailureReason = ""
				next.FailedAt = nil
				next.BillingChargeRequestID =
					persisted.Batch.ID
				next.UpdatedAt =
					canonicalTime(success.ChargedAt)

				args := usageRecordNamedArgs(next)
				args["lookup_local_request_id"] =
					current.LocalRequestID
				args["expected_status"] =
					string(current.Status)

				tag, err := tx.Exec(
					ctx,
					updateUsageRecordCASQL,
					args,
				)
				if err != nil {
					return NormalizeError(err)
				}
				if tag.RowsAffected() != 1 {
					return ports.ErrStoreConflict
				}
			}

			tag, err := tx.Exec(
				ctx,
				`
UPDATE tokenio_billing_charge_batches
SET billing_status = 'succeeded',
    billing_response_balance_cents = $2,
    billing_error_code = '',
    charged_at = $3,
    failed_at = NULL,
    updated_at = $3
WHERE id = $1
  AND billing_status = $4
`,
				success.BatchID,
				nullableInt64(success.BillingBalanceCents),
				canonicalTime(success.ChargedAt),
				string(persisted.Batch.Status),
			)
			if err != nil {
				return NormalizeError(err)
			}
			if tag.RowsAffected() != 1 {
				return ports.ErrStoreConflict
			}
			return nil
		},
	)
}

func validateChargeSuccessPersistence(
	success ports.UsageChargeSuccess,
) error {
	if success.BatchID == "" ||
		!isCanonicalUTCTime(success.ChargedAt) ||
		len(success.Allocations) == 0 ||
		len(success.Allocations) !=
			len(success.ExpectedRecords) ||
		success.BillingBalanceCents != nil &&
			*success.BillingBalanceCents < 0 {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func compareChargeSuccessMetadata(
	success ports.UsageChargeSuccess,
	persisted ports.BillingChargeBatchSnapshot,
) error {
	if success.BatchID != persisted.Batch.ID ||
		len(success.Allocations) != len(persisted.Allocations) ||
		len(success.ExpectedRecords) !=
			len(persisted.ExpectedRecords) {
		return ports.ErrStoreConflict
	}

	for index := range success.Allocations {
		if !sameAllocationCommand(
			success.Allocations[index],
			persisted.Allocations[index],
		) ||
			!sameUsageRecord(
				success.ExpectedRecords[index],
				persisted.ExpectedRecords[index],
			) {
			return ports.ErrStoreConflict
		}
	}
	return nil
}
