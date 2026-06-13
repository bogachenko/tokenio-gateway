package postgres

import (
	"context"
	"errors"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

const findUsageByLocalRequestIDSQL = `
SELECT
` + usageRecordColumns + `
FROM tokenio_usage_records
WHERE local_request_id = $1
`

const findUsageByLocalRequestIDForUpdateSQL = `
SELECT
` + usageRecordColumns + `
FROM tokenio_usage_records
WHERE local_request_id = $1
FOR UPDATE
`

const findUsageByIdempotencySQL = `
SELECT
` + usageRecordColumns + `
FROM tokenio_usage_records
WHERE user_id = $1
  AND endpoint_kind = $2
  AND idempotency_key = $3
`

const findPricingFailedUsageSQL = `
SELECT
` + usageRecordColumns + `
FROM tokenio_usage_records
WHERE user_id = $1
  AND status = 'pricing_failed'
ORDER BY created_at ASC, local_request_id ASC
LIMIT 1
`

const loadUsageExposureSQL = `
SELECT
    COALESCE(
        SUM(
            CASE
                WHEN status = 'reserved'
                THEN estimated_client_amount_cents
                ELSE 0
            END
        ),
        0
    ),
    COALESCE(
        SUM(
            CASE
                WHEN status = 'billable'
                THEN remaining_amount_cents
                ELSE 0
            END
        ),
        0
    ),
    COALESCE(
        SUM(
            CASE
                WHEN status = 'partially_charged'
                THEN remaining_amount_cents
                ELSE 0
            END
        ),
        0
    ),
    COUNT(*) FILTER (WHERE status = 'pricing_failed')
FROM tokenio_usage_records
WHERE user_id = $1
  AND currency = $2
`

const loadChargeCandidatesSQL = `
SELECT
` + usageRecordColumns + `
FROM tokenio_usage_records AS usage
WHERE usage.user_id = $1
  AND usage.currency = $2
  AND usage.remaining_amount_cents > 0
  AND (
        (
            usage.status = 'billable'
            AND usage.billing_charge_request_id IS NULL
        )
        OR
        (
            usage.status = 'partially_charged'
            AND EXISTS (
                SELECT 1
                FROM tokenio_billing_charge_batches AS historical_batch
                WHERE historical_batch.id =
                    usage.billing_charge_request_id
                  AND historical_batch.billing_status = 'succeeded'
            )
        )
  )
  AND NOT EXISTS (
        SELECT 1
        FROM tokenio_billing_charge_allocations AS active_allocation
        JOIN tokenio_billing_charge_batches AS active_batch
          ON active_batch.id = active_allocation.batch_id
        WHERE active_allocation.local_request_id = usage.local_request_id
          AND active_batch.billing_status IN ('pending', 'failed')
  )
ORDER BY usage.created_at ASC, usage.local_request_id ASC
`

type UsageLedger struct {
	db *DB
}

func NewUsageLedger(db *DB) (*UsageLedger, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &UsageLedger{db: db}, nil
}

func (r *UsageLedger) CreateReserved(
	ctx context.Context,
	record domain.UsageRecord,
) (ports.UsageReserveResult, error) {
	var result ports.UsageReserveResult

	err := InTx(ctx, r.db, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(
			ctx,
			`
SELECT pg_advisory_xact_lock(
    hashtextextended('tokenio_usage_local_request:' || $1, 0)
)
`,
			record.LocalRequestID,
		); err != nil {
			return NormalizeError(err)
		}

		var lockedUserID string
		if err := tx.QueryRow(
			ctx,
			`
SELECT id
FROM tokenio_users
WHERE id = $1
FOR UPDATE
`,
			record.UserID,
		).Scan(&lockedUserID); err != nil {
			return normalizeRegistryReadError(err)
		}

		_, err := scanUsageRecord(
			tx.QueryRow(ctx, findPricingFailedUsageSQL, record.UserID),
		)
		switch {
		case err == nil:
			result = ports.UsageReserveResult{
				Outcome: ports.UsageReserveOutcomeUnresolvedUsage,
			}
			return nil
		case errors.Is(err, ports.ErrNotFound):
		default:
			return err
		}

		existing, err := scanUsageRecord(
			tx.QueryRow(
				ctx,
				findUsageByLocalRequestIDSQL,
				record.LocalRequestID,
			),
		)
		switch {
		case err == nil:
			result = ports.UsageReserveResult{
				Outcome:  ports.UsageReserveOutcomeLocalRequestExists,
				Existing: &existing,
			}
			return nil
		case errors.Is(err, ports.ErrNotFound):
		default:
			return err
		}

		if record.IdempotencyKey != "" {
			existing, err = scanUsageRecord(
				tx.QueryRow(
					ctx,
					findUsageByIdempotencySQL,
					record.UserID,
					string(record.EndpointKind),
					record.IdempotencyKey,
				),
			)
			switch {
			case err == nil:
				result = ports.UsageReserveResult{
					Outcome:  ports.UsageReserveOutcomeIdempotencyExists,
					Existing: &existing,
				}
				return nil
			case errors.Is(err, ports.ErrNotFound):
			default:
				return err
			}
		}

		if _, err := tx.Exec(
			ctx,
			insertUsageRecordSQL,
			usageRecordNamedArgs(record),
		); err != nil {
			return NormalizeError(err)
		}

		result = ports.UsageReserveResult{
			Outcome: ports.UsageReserveOutcomeCreated,
		}
		return nil
	})
	if err != nil {
		return ports.UsageReserveResult{}, err
	}
	return result, nil
}

func (r *UsageLedger) FindByLocalRequestID(
	ctx context.Context,
	localRequestID string,
) (*domain.UsageRecord, error) {
	record, err := scanUsageRecord(
		r.db.QueryRow(ctx, findUsageByLocalRequestIDSQL, localRequestID),
	)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (r *UsageLedger) CompareAndSwap(
	ctx context.Context,
	localRequestID string,
	expectedStatus domain.UsageStatus,
	next domain.UsageRecord,
) (ports.UsageTransitionResult, error) {
	var result ports.UsageTransitionResult

	err := InTx(ctx, r.db, pgx.TxOptions{}, func(tx pgx.Tx) error {
		current, err := scanUsageRecord(
			tx.QueryRow(
				ctx,
				findUsageByLocalRequestIDForUpdateSQL,
				localRequestID,
			),
		)
		if err != nil {
			return err
		}

		if current.Status != expectedStatus {
			result = ports.UsageTransitionResult{
				Applied: false,
				Current: &current,
			}
			return nil
		}
		if next.LocalRequestID != localRequestID {
			return ports.ErrStoreContractViolation
		}

		args := usageRecordNamedArgs(next)
		args["lookup_local_request_id"] = localRequestID
		args["expected_status"] = string(expectedStatus)

		tag, err := tx.Exec(ctx, updateUsageRecordCASQL, args)
		if err != nil {
			return NormalizeError(err)
		}
		if tag.RowsAffected() != 1 {
			return ports.ErrStoreConflict
		}

		result = ports.UsageTransitionResult{Applied: true}
		return nil
	})
	if err != nil {
		return ports.UsageTransitionResult{}, err
	}
	return result, nil
}

func (r *UsageLedger) LoadExposure(
	ctx context.Context,
	userID string,
	currency string,
) (ports.UsageExposureSnapshot, error) {
	result := ports.UsageExposureSnapshot{Currency: currency}

	err := r.db.QueryRow(
		ctx,
		loadUsageExposureSQL,
		userID,
		currency,
	).Scan(
		&result.ReservedEstimatedAmountCents,
		&result.BillableRemainingAmountCents,
		&result.PartiallyChargedRemainingAmountCents,
		&result.PricingFailedCount,
	)
	if err != nil {
		return ports.UsageExposureSnapshot{}, normalizeRegistryReadError(err)
	}
	return result, nil
}

func (r *UsageLedger) LoadChargeCandidates(
	ctx context.Context,
	userID string,
	currency string,
) ([]domain.UsageRecord, error) {
	rows, err := r.db.Query(
		ctx,
		loadChargeCandidatesSQL,
		userID,
		currency,
	)
	if err != nil {
		return nil, normalizeRegistryReadError(err)
	}
	defer rows.Close()

	result := make([]domain.UsageRecord, 0)
	seen := make(map[string]struct{})
	for rows.Next() {
		record, err := scanUsageRecord(rows)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[record.LocalRequestID]; exists {
			return nil, ports.ErrStoreContractViolation
		}
		seen[record.LocalRequestID] = struct{}{}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, normalizeRegistryReadError(err)
	}
	return result, nil
}
