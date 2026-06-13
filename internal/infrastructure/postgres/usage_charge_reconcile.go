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
			_, err := applyChargeSuccessInTx(
				ctx,
				tx,
				success,
				false,
			)
			return err
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
