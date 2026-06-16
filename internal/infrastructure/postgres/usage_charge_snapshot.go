package postgres

import (
	"context"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

const findBillingChargeBatchSQL = `
SELECT
` + billingChargeBatchColumns + `
FROM tokenio_billing_charge_batches
WHERE id = $1
`

const findBillingChargeBatchForUpdateSQL = `
SELECT
` + billingChargeBatchColumns + `
FROM tokenio_billing_charge_batches
WHERE id = $1
FOR UPDATE
`

const loadBillingChargeAllocationsSQL = `
SELECT
    id,
    batch_id,
    local_request_id,
    position,
    charged_amount_cents,
    remaining_amount_cents,
    created_at
FROM tokenio_billing_charge_allocations
WHERE batch_id = $1
ORDER BY position ASC
`

const loadBillingChargeExpectedRecordsSQL = `
SELECT
    local_request_id,
    position,
    expected_record,
    created_at
FROM tokenio_billing_charge_expected_records
WHERE batch_id = $1
ORDER BY position ASC
`

const loadOpenChargeBatchIDsSQL = `
SELECT id
FROM tokenio_billing_charge_batches
WHERE user_id = $1
  AND billing_subject_user_id = $2
  AND currency = $3
  AND billing_status IN ('pending', 'failed')
ORDER BY created_at ASC, id ASC
`

const loadRecoveryChargeBatchIDsSQL = `
SELECT id
FROM tokenio_billing_charge_batches
WHERE billing_status IN ('pending', 'failed')
ORDER BY created_at ASC, id ASC
LIMIT $1
`

func loadBillingChargeSnapshot(
	ctx context.Context,
	db DBTX,
	batchID string,
	lockBatch bool,
) (ports.BillingChargeBatchSnapshot, error) {
	batchQuery := findBillingChargeBatchSQL
	if lockBatch {
		batchQuery = findBillingChargeBatchForUpdateSQL
	}

	batch, err := scanBillingChargeBatch(
		db.QueryRow(ctx, batchQuery, batchID),
	)
	if err != nil {
		return ports.BillingChargeBatchSnapshot{}, err
	}

	allocationRows, err := db.Query(
		ctx,
		loadBillingChargeAllocationsSQL,
		batchID,
	)
	if err != nil {
		return ports.BillingChargeBatchSnapshot{},
			normalizeRegistryReadError(err)
	}
	defer allocationRows.Close()

	positionedAllocations := make([]positionedAllocation, 0)
	for allocationRows.Next() {
		value, err := scanPositionedAllocation(allocationRows)
		if err != nil {
			return ports.BillingChargeBatchSnapshot{}, err
		}
		positionedAllocations = append(positionedAllocations, value)
	}
	if err := allocationRows.Err(); err != nil {
		return ports.BillingChargeBatchSnapshot{},
			normalizeRegistryReadError(err)
	}

	expectedRows, err := db.Query(
		ctx,
		loadBillingChargeExpectedRecordsSQL,
		batchID,
	)
	if err != nil {
		return ports.BillingChargeBatchSnapshot{},
			normalizeRegistryReadError(err)
	}
	defer expectedRows.Close()

	positionedExpected := make([]positionedExpectedRecord, 0)
	for expectedRows.Next() {
		value, err := scanPositionedExpectedRecord(expectedRows)
		if err != nil {
			return ports.BillingChargeBatchSnapshot{}, err
		}
		positionedExpected = append(positionedExpected, value)
	}
	if err := expectedRows.Err(); err != nil {
		return ports.BillingChargeBatchSnapshot{},
			normalizeRegistryReadError(err)
	}

	if len(positionedAllocations) == 0 ||
		len(positionedAllocations) != len(positionedExpected) {
		return ports.BillingChargeBatchSnapshot{},
			ports.ErrStoreContractViolation
	}

	snapshot := ports.BillingChargeBatchSnapshot{
		Batch:           batch,
		Allocations:     make([]domain.BillingChargeAllocation, len(positionedAllocations)),
		ExpectedRecords: make([]domain.UsageRecord, len(positionedExpected)),
	}
	for index := range positionedAllocations {
		allocation := positionedAllocations[index]
		expected := positionedExpected[index]
		if allocation.Position != index ||
			expected.Position != index ||
			allocation.Allocation.BatchID != batch.ID ||
			allocation.Allocation.LocalRequestID !=
				expected.LocalRequestID {
			return ports.BillingChargeBatchSnapshot{},
				ports.ErrStoreContractViolation
		}
		snapshot.Allocations[index] = allocation.Allocation
		snapshot.ExpectedRecords[index] = expected.Record
	}
	return snapshot, nil
}

func (r *UsageLedger) ListOpenChargeBatchesForRecovery(
	ctx context.Context,
	limit int,
) ([]ports.BillingChargeBatchSnapshot, error) {
	if ctx == nil || limit < 1 {
		return nil, ports.ErrStoreContractViolation
	}

	result := make([]ports.BillingChargeBatchSnapshot, 0, limit)
	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadOnly,
		},
		func(tx pgx.Tx) error {
			rows, err := tx.Query(
				ctx,
				loadRecoveryChargeBatchIDsSQL,
				limit,
			)
			if err != nil {
				return normalizeRegistryReadError(err)
			}
			defer rows.Close()

			batchIDs := make([]string, 0, limit)
			for rows.Next() {
				var batchID string
				if err := rows.Scan(&batchID); err != nil {
					return normalizeRegistryReadError(err)
				}
				batchIDs = append(batchIDs, batchID)
			}
			if err := rows.Err(); err != nil {
				return normalizeRegistryReadError(err)
			}
			rows.Close()

			if len(batchIDs) > limit {
				return ports.ErrStoreContractViolation
			}
			for _, batchID := range batchIDs {
				snapshot, err := loadBillingChargeSnapshot(
					ctx,
					tx,
					batchID,
					false,
				)
				if err != nil {
					return err
				}
				if snapshot.Batch.Status !=
					domain.BillingChargeStatusPending &&
					snapshot.Batch.Status !=
						domain.BillingChargeStatusFailed {
					return ports.ErrStoreContractViolation
				}
				result = append(result, snapshot)
			}
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *UsageLedger) LoadOpenChargeBatches(
	ctx context.Context,
	userID string,
	billingSubjectUserID string,
	currency string,
) ([]ports.BillingChargeBatchSnapshot, error) {
	result := make([]ports.BillingChargeBatchSnapshot, 0)

	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadOnly,
		},
		func(tx pgx.Tx) error {
			rows, err := tx.Query(
				ctx,
				loadOpenChargeBatchIDsSQL,
				userID,
				billingSubjectUserID,
				currency,
			)
			if err != nil {
				return normalizeRegistryReadError(err)
			}
			defer rows.Close()

			batchIDs := make([]string, 0)
			for rows.Next() {
				var batchID string
				if err := rows.Scan(&batchID); err != nil {
					return normalizeRegistryReadError(err)
				}
				batchIDs = append(batchIDs, batchID)
			}
			if err := rows.Err(); err != nil {
				return normalizeRegistryReadError(err)
			}
			rows.Close()

			for _, batchID := range batchIDs {
				snapshot, err := loadBillingChargeSnapshot(
					ctx,
					tx,
					batchID,
					false,
				)
				if err != nil {
					return err
				}
				if snapshot.Batch.Status !=
					domain.BillingChargeStatusPending &&
					snapshot.Batch.Status !=
						domain.BillingChargeStatusFailed {
					return ports.ErrStoreContractViolation
				}
				result = append(result, snapshot)
			}
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return result, nil
}
