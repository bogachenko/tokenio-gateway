package postgres

import (
	"context"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

type chargeSuccessTxResult struct {
	Before domain.BillingChargeBatch
	After  domain.BillingChargeBatch
	Replay bool
}

func applyChargeSuccessInTx(
	ctx context.Context,
	tx pgx.Tx,
	success ports.UsageChargeSuccess,
	requireFailed bool,
) (chargeSuccessTxResult, error) {
	persisted, err := loadBillingChargeSnapshot(
		ctx,
		tx,
		success.BatchID,
		true,
	)
	if err != nil {
		return chargeSuccessTxResult{}, err
	}
	if err := compareChargeSuccessMetadata(
		success,
		persisted,
	); err != nil {
		return chargeSuccessTxResult{}, err
	}

	if persisted.Batch.Status ==
		domain.BillingChargeStatusSucceeded {
		if persisted.Batch.ChargedAt == nil ||
			!persisted.Batch.ChargedAt.Equal(
				success.ChargedAt,
			) ||
			!sameOptionalInt64(
				persisted.Batch.BillingResponseBalanceCents,
				success.BillingBalanceCents,
			) {
			return chargeSuccessTxResult{}, ports.ErrStoreConflict
		}
		return chargeSuccessTxResult{
			Before: persisted.Batch,
			After:  persisted.Batch,
			Replay: true,
		}, nil
	}

	if requireFailed {
		if persisted.Batch.Status !=
			domain.BillingChargeStatusFailed {
			return chargeSuccessTxResult{},
				ports.ErrStoreConflict
		}
	} else if persisted.Batch.Status !=
		domain.BillingChargeStatusPending &&
		persisted.Batch.Status !=
			domain.BillingChargeStatusFailed {
		return chargeSuccessTxResult{}, ports.ErrStoreConflict
	}

	currentByID, err := lockUsageRecordsForCharge(
		ctx,
		tx,
		persisted.ExpectedRecords,
	)
	if err != nil {
		return chargeSuccessTxResult{}, err
	}

	for index, expected := range persisted.ExpectedRecords {
		current, ok := currentByID[expected.LocalRequestID]
		if !ok || !sameUsageRecord(current, expected) {
			return chargeSuccessTxResult{},
				ports.ErrStoreConflict
		}
		allocation := persisted.Allocations[index]
		if allocation.ChargedAmountCents >
			current.RemainingAmountCents ||
			allocation.RemainingAmountCents !=
				current.RemainingAmountCents-
					allocation.ChargedAmountCents {
			return chargeSuccessTxResult{},
				ports.ErrStoreContractViolation
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
		next.UpdatedAt = canonicalTime(success.ChargedAt)

		args := usageRecordNamedArgs(next)
		args["lookup_local_request_id"] =
			current.LocalRequestID
		args["expected_status"] = string(current.Status)

		tag, err := tx.Exec(
			ctx,
			updateUsageRecordCASQL,
			args,
		)
		if err != nil {
			return chargeSuccessTxResult{},
				NormalizeError(err)
		}
		if tag.RowsAffected() != 1 {
			return chargeSuccessTxResult{},
				ports.ErrStoreConflict
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
		return chargeSuccessTxResult{}, NormalizeError(err)
	}
	if tag.RowsAffected() != 1 {
		return chargeSuccessTxResult{},
			ports.ErrStoreConflict
	}

	updated, err := scanBillingChargeBatch(
		tx.QueryRow(
			ctx,
			findBillingChargeBatchForUpdateSQL,
			success.BatchID,
		),
	)
	if err != nil {
		return chargeSuccessTxResult{}, err
	}
	return chargeSuccessTxResult{
		Before: persisted.Batch,
		After:  updated,
	}, nil
}
