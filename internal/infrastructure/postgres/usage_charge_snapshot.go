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

const listChargeableBillingSubjectsSQL = `
SELECT usage.user_id, users.external_billing_user_id, usage.currency,
       MIN(usage.created_at) AS oldest_chargeable_at
FROM tokenio_usage_records AS usage
JOIN tokenio_users AS users ON users.id = usage.user_id
WHERE usage.remaining_amount_cents > 0
  AND ((usage.status = 'billable' AND usage.billing_charge_request_id IS NULL)
       OR (usage.status = 'partially_charged' AND EXISTS (
            SELECT 1 FROM tokenio_billing_charge_batches AS historical_batch
            WHERE historical_batch.id = usage.billing_charge_request_id
              AND historical_batch.billing_status = 'succeeded')))
  AND NOT EXISTS (
        SELECT 1
        FROM tokenio_billing_charge_allocations AS active_allocation
        JOIN tokenio_billing_charge_batches AS active_batch
          ON active_batch.id = active_allocation.batch_id
        WHERE active_allocation.local_request_id = usage.local_request_id
          AND active_batch.billing_status IN ('pending', 'failed'))
GROUP BY usage.user_id, users.external_billing_user_id, usage.currency
ORDER BY oldest_chargeable_at ASC, usage.user_id ASC,
         users.external_billing_user_id ASC, usage.currency ASC
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

func (r *UsageLedger) ListChargeableBillingSubjects(
	ctx context.Context,
	limit int,
) ([]ports.BillingChargeSubject, error) {
	if ctx == nil || limit < 1 {
		return nil, ports.ErrStoreContractViolation
	}
	rows, err := r.db.Query(ctx, listChargeableBillingSubjectsSQL, limit)
	if err != nil {
		return nil, normalizeRegistryReadError(err)
	}
	defer rows.Close()

	result := make([]ports.BillingChargeSubject, 0, limit)
	seen := make(map[string]struct{}, limit)
	for rows.Next() {
		var subject ports.BillingChargeSubject
		if err := rows.Scan(&subject.UserID, &subject.BillingSubjectUserID, &subject.Currency, &subject.OldestChargeableAt); err != nil {
			return nil, normalizeRegistryReadError(err)
		}
		key := subject.UserID + "\x00" + subject.BillingSubjectUserID + "\x00" + subject.Currency
		if _, exists := seen[key]; exists {
			return nil, ports.ErrStoreContractViolation
		}
		seen[key] = struct{}{}
		subject.OldestChargeableAt = subject.OldestChargeableAt.UTC()
		result = append(result, subject)
	}
	if err := rows.Err(); err != nil {
		return nil, normalizeRegistryReadError(err)
	}
	if len(result) > limit {
		return nil, ports.ErrStoreContractViolation
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
