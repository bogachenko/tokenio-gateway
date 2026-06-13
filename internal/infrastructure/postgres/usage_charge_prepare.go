package postgres

import (
	"context"
	"errors"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

const insertBillingChargeBatchSQL = `
INSERT INTO tokenio_billing_charge_batches (
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
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9,
    $10, $11, $12, $13, $14, $15, $16, $17
)
`

const insertBillingChargeAllocationSQL = `
INSERT INTO tokenio_billing_charge_allocations (
    id,
    batch_id,
    local_request_id,
    position,
    charged_amount_cents,
    remaining_amount_cents,
    created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
`

const insertBillingChargeExpectedRecordSQL = `
INSERT INTO tokenio_billing_charge_expected_records (
    batch_id,
    local_request_id,
    position,
    expected_record,
    created_at
)
VALUES ($1, $2, $3, $4::jsonb, $5)
`

const lockUsageRecordsForChargeSQL = `
SELECT
` + usageRecordColumns + `
FROM tokenio_usage_records
WHERE local_request_id = ANY($1::text[])
ORDER BY local_request_id ASC
FOR UPDATE
`

const activeChargeClaimExistsSQL = `
SELECT EXISTS (
    SELECT 1
    FROM tokenio_billing_charge_allocations AS allocation
    JOIN tokenio_billing_charge_batches AS batch
      ON batch.id = allocation.batch_id
    WHERE allocation.local_request_id = ANY($1::text[])
      AND batch.billing_status IN ('pending', 'failed')
)
`

func (r *UsageLedger) PrepareChargeBatch(
	ctx context.Context,
	plan ports.UsageChargeBatchPlan,
) (ports.BillingChargeBatchSnapshot, error) {
	if err := validateChargePlanPersistence(plan); err != nil {
		return ports.BillingChargeBatchSnapshot{}, err
	}

	var result ports.BillingChargeBatchSnapshot
	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			if _, err := tx.Exec(
				ctx,
				`
SELECT pg_advisory_xact_lock(
    hashtextextended('tokenio_charge_batch:' || $1, 0)
)
`,
				plan.Batch.ID,
			); err != nil {
				return NormalizeError(err)
			}

			existing, err := loadBillingChargeSnapshot(
				ctx,
				tx,
				plan.Batch.ID,
				true,
			)
			switch {
			case err == nil:
				if err := compareChargePlanReplay(
					plan,
					existing,
				); err != nil {
					return err
				}
				result = existing
				return nil
			case errors.Is(err, ports.ErrNotFound):
			default:
				return err
			}

			currentByID, err := lockUsageRecordsForCharge(
				ctx,
				tx,
				plan.ExpectedRecords,
			)
			if err != nil {
				return err
			}
			for _, expected := range plan.ExpectedRecords {
				current, ok := currentByID[expected.LocalRequestID]
				if !ok || !sameUsageRecord(current, expected) {
					return ports.ErrStoreConflict
				}
			}

			localRequestIDs := make([]string, 0, len(plan.ExpectedRecords))
			for _, expected := range plan.ExpectedRecords {
				localRequestIDs = append(
					localRequestIDs,
					expected.LocalRequestID,
				)
			}
			if err := verifyHistoricalClaims(
				ctx,
				tx,
				plan.ExpectedRecords,
			); err != nil {
				return err
			}

			var activeClaim bool
			if err := tx.QueryRow(
				ctx,
				activeChargeClaimExistsSQL,
				localRequestIDs,
			).Scan(&activeClaim); err != nil {
				return normalizeRegistryReadError(err)
			}
			if activeClaim {
				return ports.ErrStoreConflict
			}

			if err := insertChargeCommand(ctx, tx, plan); err != nil {
				return err
			}

			for index, expected := range plan.ExpectedRecords {
				tag, err := tx.Exec(
					ctx,
					`
UPDATE tokenio_usage_records
SET billing_charge_request_id = $2
WHERE local_request_id = $1
`,
					expected.LocalRequestID,
					plan.Batch.ID,
				)
				if err != nil {
					return NormalizeError(err)
				}
				if tag.RowsAffected() != 1 {
					return ports.ErrStoreConflict
				}

				postClaim := postClaimRecord(
					expected,
					plan.Batch.ID,
				)
				body, err := encodeExpectedRecord(postClaim)
				if err != nil {
					return err
				}
				if _, err := tx.Exec(
					ctx,
					insertBillingChargeExpectedRecordSQL,
					plan.Batch.ID,
					expected.LocalRequestID,
					index,
					body,
					plan.Batch.CreatedAt,
				); err != nil {
					return NormalizeError(err)
				}
			}

			result, err = loadBillingChargeSnapshot(
				ctx,
				tx,
				plan.Batch.ID,
				false,
			)
			return err
		},
	)
	if err != nil {
		return ports.BillingChargeBatchSnapshot{}, err
	}
	return result, nil
}

func validateChargePlanPersistence(
	plan ports.UsageChargeBatchPlan,
) error {
	batch := plan.Batch
	if batch.ID == "" ||
		batch.UserID == "" ||
		batch.BillingSubjectUserID == "" ||
		batch.ProviderType == "" ||
		batch.ClientModel == "" ||
		batch.BillingModel == "" ||
		batch.InputTokens < 0 ||
		batch.OutputTokens < 0 ||
		batch.AmountCents <= 0 ||
		batch.Currency != "RUB" ||
		batch.Status != domain.BillingChargeStatusPending ||
		batch.BillingResponseBalanceCents != nil ||
		batch.BillingErrorCode != "" ||
		batch.ChargedAt != nil ||
		batch.FailedAt != nil ||
		!isCanonicalUTCTime(batch.CreatedAt) ||
		!isCanonicalUTCTime(batch.UpdatedAt) ||
		!batch.CreatedAt.Equal(batch.UpdatedAt) {
		return ports.ErrStoreContractViolation
	}

	if len(plan.Allocations) == 0 ||
		len(plan.Allocations) != len(plan.ExpectedRecords) {
		return ports.ErrStoreContractViolation
	}

	seen := make(map[string]struct{}, len(plan.Allocations))
	for index := range plan.Allocations {
		allocation := plan.Allocations[index]
		expected := plan.ExpectedRecords[index]

		if allocation.ID == "" ||
			allocation.BatchID != batch.ID ||
			allocation.LocalRequestID == "" ||
			allocation.LocalRequestID != expected.LocalRequestID ||
			allocation.ChargedAmountCents <= 0 ||
			allocation.RemainingAmountCents < 0 ||
			!isCanonicalUTCTime(allocation.CreatedAt) ||
			!allocation.CreatedAt.Equal(batch.CreatedAt) ||
			expected.LocalRequestID == "" ||
			expected.Status != domain.UsageStatusBillable &&
				expected.Status !=
					domain.UsageStatusPartiallyCharged ||
			expected.Status == domain.UsageStatusBillable &&
				expected.BillingChargeRequestID != "" ||
			expected.Status ==
				domain.UsageStatusPartiallyCharged &&
				expected.BillingChargeRequestID == "" ||
			expected.RemainingAmountCents <= 0 {
			return ports.ErrStoreContractViolation
		}
		if _, exists := seen[expected.LocalRequestID]; exists {
			return ports.ErrStoreContractViolation
		}
		seen[expected.LocalRequestID] = struct{}{}

		postClaim := postClaimRecord(expected, batch.ID)
		if err := validateExpectedRecordPersistence(postClaim); err != nil {
			return err
		}
	}
	return nil
}

func lockUsageRecordsForCharge(
	ctx context.Context,
	tx pgx.Tx,
	expected []domain.UsageRecord,
) (map[string]domain.UsageRecord, error) {
	localRequestIDs := make([]string, 0, len(expected))
	for _, record := range expected {
		localRequestIDs = append(localRequestIDs, record.LocalRequestID)
	}

	rows, err := tx.Query(
		ctx,
		lockUsageRecordsForChargeSQL,
		localRequestIDs,
	)
	if err != nil {
		return nil, normalizeRegistryReadError(err)
	}
	defer rows.Close()

	result := make(map[string]domain.UsageRecord, len(expected))
	for rows.Next() {
		record, err := scanUsageRecord(rows)
		if err != nil {
			return nil, err
		}
		if _, exists := result[record.LocalRequestID]; exists {
			return nil, ports.ErrStoreContractViolation
		}
		result[record.LocalRequestID] = record
	}
	if err := rows.Err(); err != nil {
		return nil, normalizeRegistryReadError(err)
	}
	if len(result) != len(expected) {
		return nil, ports.ErrStoreConflict
	}
	return result, nil
}

func verifyHistoricalClaims(
	ctx context.Context,
	tx pgx.Tx,
	expected []domain.UsageRecord,
) error {
	for _, record := range expected {
		if record.BillingChargeRequestID == "" {
			continue
		}

		var status string
		err := tx.QueryRow(
			ctx,
			`
SELECT billing_status
FROM tokenio_billing_charge_batches
WHERE id = $1
`,
			record.BillingChargeRequestID,
		).Scan(&status)
		if err != nil {
			if errors.Is(normalizeRegistryReadError(err), ports.ErrNotFound) {
				return ports.ErrStoreConflict
			}
			return normalizeRegistryReadError(err)
		}
		if domain.BillingChargeStatus(status) !=
			domain.BillingChargeStatusSucceeded {
			return ports.ErrStoreConflict
		}
	}
	return nil
}

func insertChargeCommand(
	ctx context.Context,
	tx pgx.Tx,
	plan ports.UsageChargeBatchPlan,
) error {
	batch := plan.Batch
	if _, err := tx.Exec(
		ctx,
		insertBillingChargeBatchSQL,
		batch.ID,
		batch.UserID,
		batch.BillingSubjectUserID,
		string(batch.ProviderType),
		batch.ClientModel,
		batch.BillingModel,
		batch.InputTokens,
		batch.OutputTokens,
		batch.AmountCents,
		batch.Currency,
		string(batch.Status),
		nullableInt64(batch.BillingResponseBalanceCents),
		batch.BillingErrorCode,
		canonicalTime(batch.CreatedAt),
		canonicalTimePointer(batch.ChargedAt),
		canonicalTimePointer(batch.FailedAt),
		canonicalTime(batch.UpdatedAt),
	); err != nil {
		return NormalizeError(err)
	}

	for index, allocation := range plan.Allocations {
		if _, err := tx.Exec(
			ctx,
			insertBillingChargeAllocationSQL,
			allocation.ID,
			allocation.BatchID,
			allocation.LocalRequestID,
			index,
			allocation.ChargedAmountCents,
			allocation.RemainingAmountCents,
			canonicalTime(allocation.CreatedAt),
		); err != nil {
			return NormalizeError(err)
		}
	}
	return nil
}

func compareChargePlanReplay(
	plan ports.UsageChargeBatchPlan,
	persisted ports.BillingChargeBatchSnapshot,
) error {
	if !sameBatchCommand(plan.Batch, persisted.Batch) ||
		len(plan.Allocations) != len(persisted.Allocations) ||
		len(plan.ExpectedRecords) !=
			len(persisted.ExpectedRecords) {
		return ports.ErrStoreConflict
	}

	for index := range plan.Allocations {
		if !sameAllocationCommand(
			plan.Allocations[index],
			persisted.Allocations[index],
		) {
			return ports.ErrStoreConflict
		}
		comparison := postClaimRecord(
			plan.ExpectedRecords[index],
			plan.Batch.ID,
		)
		if !sameUsageRecord(
			comparison,
			persisted.ExpectedRecords[index],
		) {
			return ports.ErrStoreConflict
		}
		if plan.Allocations[index].LocalRequestID !=
			plan.ExpectedRecords[index].LocalRequestID {
			return ports.ErrStoreConflict
		}
	}
	return nil
}
