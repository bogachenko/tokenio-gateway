package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

type AdminUsageLedger struct {
	*UsageLedger
}

var _ ports.AdminUsageLedger = (*AdminUsageLedger)(nil)

func NewAdminUsageLedger(db *DB) (*AdminUsageLedger, error) {
	ledger, err := NewUsageLedger(db)
	if err != nil {
		return nil, err
	}
	return &AdminUsageLedger{UsageLedger: ledger}, nil
}

func (r *AdminUsageLedger) ListUsageRecords(
	ctx context.Context,
	filter ports.UsageListFilter,
) (ports.Page[domain.UsageRecord], error) {
	if err := validateAdminPage(filter.Page); err != nil {
		return ports.Page[domain.UsageRecord]{}, err
	}
	if err := validateAdminUsageWindow(
		filter.CreatedFrom,
		filter.CreatedTo,
	); err != nil {
		return ports.Page[domain.UsageRecord]{}, err
	}

	where, args := buildAdminUsageFilter(filter)
	var result ports.Page[domain.UsageRecord]

	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadOnly,
		},
		func(tx pgx.Tx) error {
			countSQL := "SELECT COUNT(*) FROM tokenio_usage_records" +
				where
			if err := tx.QueryRow(
				ctx,
				countSQL,
				args...,
			).Scan(&result.Total); err != nil {
				return normalizeRegistryReadError(err)
			}

			listArgs := append([]any(nil), args...)
			limitPosition := len(listArgs) + 1
			listArgs = append(listArgs, filter.Page.Limit)
			offsetPosition := len(listArgs) + 1
			listArgs = append(listArgs, filter.Page.Offset)

			listSQL := `
SELECT
` + usageRecordColumns + `
FROM tokenio_usage_records` + where + fmt.Sprintf(`
ORDER BY created_at DESC, local_request_id ASC
LIMIT $%d OFFSET $%d
`, limitPosition, offsetPosition)

			rows, err := tx.Query(ctx, listSQL, listArgs...)
			if err != nil {
				return normalizeRegistryReadError(err)
			}
			defer rows.Close()

			result.Items = make([]domain.UsageRecord, 0)
			for rows.Next() {
				record, err := scanUsageRecord(rows)
				if err != nil {
					return err
				}
				result.Items = append(result.Items, record)
			}
			if err := rows.Err(); err != nil {
				return normalizeRegistryReadError(err)
			}
			return nil
		},
	)
	if err != nil {
		return ports.Page[domain.UsageRecord]{}, err
	}
	return result, nil
}

func (r *AdminUsageLedger) ResolvePricingFailedWithAudit(
	ctx context.Context,
	expected domain.UsageRecord,
	next domain.UsageRecord,
	audit domain.AuditContext,
) (ports.UsageTransitionResult, error) {
	if err := validateAdminUsageResolution(
		expected,
		next,
		audit.Action,
	); err != nil {
		return ports.UsageTransitionResult{}, err
	}
	if err := validateAuditForEntity(
		audit,
		audit.Action,
		"usage_record",
		next.LocalRequestID,
		adminUsageApplicationState(expected),
		adminUsageApplicationState(next),
		next.UpdatedAt,
	); err != nil {
		return ports.UsageTransitionResult{}, err
	}

	var result ports.UsageTransitionResult
	persistedNext := canonicalAdminUsageRecord(next)

	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			current, err := scanUsageRecord(
				tx.QueryRow(
					ctx,
					findUsageByLocalRequestIDForUpdateSQL,
					expected.LocalRequestID,
				),
			)
			if err != nil {
				return err
			}
			if !sameUsageRecord(current, expected) {
				result = ports.UsageTransitionResult{
					Applied: false,
					Current: &current,
				}
				return nil
			}

			args := usageRecordNamedArgs(persistedNext)
			args["lookup_local_request_id"] =
				expected.LocalRequestID
			args["expected_status"] =
				string(domain.UsageStatusPricingFailed)

			tag, err := tx.Exec(
				ctx,
				updateUsageRecordCASQL,
				args,
			)
			if err != nil {
				return normalizeAdminWriteError(err)
			}
			if tag.RowsAffected() != 1 {
				return ports.ErrAdminStateConflict
			}

			persistedAudit := canonicalUsageAudit(
				audit,
				adminUsageState(current),
				adminUsageState(persistedNext),
				persistedNext.UpdatedAt,
			)
			if err := insertAdminAudit(
				ctx,
				tx,
				persistedAudit,
			); err != nil {
				return err
			}
			result = ports.UsageTransitionResult{Applied: true}
			return nil
		},
	)
	if err != nil {
		if errors.Is(err, ports.ErrStoreConflict) {
			return ports.UsageTransitionResult{},
				ports.ErrAdminStateConflict
		}
		return ports.UsageTransitionResult{}, err
	}
	return result, nil
}

func (r *AdminUsageLedger) ListBillingChargeBatches(
	ctx context.Context,
	filter ports.BillingChargeBatchListFilter,
) (ports.Page[domain.BillingChargeBatch], error) {
	if err := validateAdminPage(filter.Page); err != nil {
		return ports.Page[domain.BillingChargeBatch]{}, err
	}
	if err := validateAdminUsageWindow(
		filter.CreatedFrom,
		filter.CreatedTo,
	); err != nil {
		return ports.Page[domain.BillingChargeBatch]{}, err
	}

	where, args := buildAdminBillingBatchFilter(filter)
	var result ports.Page[domain.BillingChargeBatch]

	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadOnly,
		},
		func(tx pgx.Tx) error {
			countSQL := "SELECT COUNT(*) FROM tokenio_billing_charge_batches" +
				where
			if err := tx.QueryRow(
				ctx,
				countSQL,
				args...,
			).Scan(&result.Total); err != nil {
				return normalizeRegistryReadError(err)
			}

			listArgs := append([]any(nil), args...)
			limitPosition := len(listArgs) + 1
			listArgs = append(listArgs, filter.Page.Limit)
			offsetPosition := len(listArgs) + 1
			listArgs = append(listArgs, filter.Page.Offset)

			listSQL := `
SELECT
` + billingChargeBatchColumns + `
FROM tokenio_billing_charge_batches` + where + fmt.Sprintf(`
ORDER BY created_at DESC, id ASC
LIMIT $%d OFFSET $%d
`, limitPosition, offsetPosition)

			rows, err := tx.Query(ctx, listSQL, listArgs...)
			if err != nil {
				return normalizeRegistryReadError(err)
			}
			defer rows.Close()

			result.Items =
				make([]domain.BillingChargeBatch, 0)
			for rows.Next() {
				batch, err := scanBillingChargeBatch(rows)
				if err != nil {
					return err
				}
				result.Items = append(
					result.Items,
					batch,
				)
			}
			if err := rows.Err(); err != nil {
				return normalizeRegistryReadError(err)
			}
			return nil
		},
	)
	if err != nil {
		return ports.Page[domain.BillingChargeBatch]{}, err
	}
	return result, nil
}

func (r *AdminUsageLedger) LoadChargeBatchByID(
	ctx context.Context,
	batchID string,
) (ports.BillingChargeBatchSnapshot, error) {
	var result ports.BillingChargeBatchSnapshot
	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadOnly,
		},
		func(tx pgx.Tx) error {
			snapshot, err := loadBillingChargeSnapshot(
				ctx,
				tx,
				batchID,
				false,
			)
			if err != nil {
				return err
			}
			result = snapshot
			return nil
		},
	)
	if err != nil {
		return ports.BillingChargeBatchSnapshot{}, err
	}
	return result, nil
}

func (r *AdminUsageLedger) RecordChargeRetryAttemptWithAudit(
	ctx context.Context,
	expected ports.BillingChargeBatchSnapshot,
	audit domain.AuditContext,
) error {
	if expected.Batch.ID == "" ||
		expected.Batch.Status !=
			domain.BillingChargeStatusFailed {
		return ports.ErrStoreContractViolation
	}
	if err := validateAuditForEntity(
		audit,
		domain.AuditActionBillingChargeRetry,
		"billing_charge_batch",
		expected.Batch.ID,
		adminBillingBatchApplicationState(expected.Batch),
		adminBillingBatchApplicationState(expected.Batch),
		audit.CreatedAt,
	); err != nil {
		return err
	}

	return InTx(
		ctx,
		r.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			current, err := loadBillingChargeSnapshot(
				ctx,
				tx,
				expected.Batch.ID,
				true,
			)
			if err != nil {
				return err
			}
			if current.Batch.Status !=
				domain.BillingChargeStatusFailed ||
				!sameAdminChargeSnapshot(current, expected) {
				return ports.ErrAdminStateConflict
			}

			persistedAudit := canonicalBillingRetryAudit(
				audit,
				current.Batch,
				current.Batch,
			)
			return insertAdminAudit(ctx, tx, persistedAudit)
		},
	)
}

func (r *AdminUsageLedger) ApplyChargeRetrySuccessWithAudit(
	ctx context.Context,
	success ports.UsageChargeSuccess,
	audit domain.AuditContext,
) error {
	if err := validateChargeSuccessPersistence(success); err != nil {
		return err
	}
	if err := validateAuditContext(audit); err != nil {
		return err
	}
	if audit.Action != domain.AuditActionBillingChargeRetry ||
		audit.EntityType != "billing_charge_batch" ||
		audit.EntityID != success.BatchID {
		return ports.ErrStoreContractViolation
	}

	return InTx(
		ctx,
		r.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			outcome, err := applyChargeSuccessInTx(
				ctx,
				tx,
				success,
				true,
			)
			if err != nil {
				return err
			}

			if outcome.Replay {
				return verifyAdminAuditReplay(
					ctx,
					tx,
					canonicalBillingRetryAuditInput(audit),
				)
			}

			expectedAfter := billingRetryAfterSuccess(
				outcome.Before,
				success,
			)
			if err := validateAuditForEntity(
				audit,
				domain.AuditActionBillingChargeRetry,
				"billing_charge_batch",
				success.BatchID,
				adminBillingBatchApplicationState(
					outcome.Before,
				),
				adminBillingBatchApplicationState(
					expectedAfter,
				),
				success.ChargedAt,
			); err != nil {
				return err
			}
			if !sameAdminBillingBatch(
				outcome.After,
				expectedAfter,
			) {
				return ports.ErrStoreContractViolation
			}

			persistedAudit := canonicalBillingRetryAudit(
				audit,
				outcome.Before,
				outcome.After,
			)
			return insertAdminAudit(ctx, tx, persistedAudit)
		},
	)
}

func (r *AdminUsageLedger) MarkChargeRetryFailedWithAudit(
	ctx context.Context,
	batchID string,
	expectedStatus domain.BillingChargeStatus,
	billingErrorCode string,
	failedAt time.Time,
	audit domain.AuditContext,
) error {
	if err := validateAuditContext(audit); err != nil {
		return err
	}
	if batchID == "" ||
		expectedStatus != domain.BillingChargeStatusFailed ||
		billingErrorCode == "" ||
		!isAdminUTCTime(failedAt) ||
		audit.Action != domain.AuditActionBillingChargeRetry ||
		audit.EntityType != "billing_charge_batch" ||
		audit.EntityID != batchID {
		return ports.ErrStoreContractViolation
	}

	return InTx(
		ctx,
		r.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			current, err := scanBillingChargeBatch(
				tx.QueryRow(
					ctx,
					findBillingChargeBatchForUpdateSQL,
					batchID,
				),
			)
			if err != nil {
				return err
			}
			if current.Status != expectedStatus {
				return ports.ErrAdminStateConflict
			}

			expectedAfter := billingRetryAfterFailure(
				current,
				billingErrorCode,
				failedAt,
			)
			if err := validateAuditForEntity(
				audit,
				domain.AuditActionBillingChargeRetry,
				"billing_charge_batch",
				batchID,
				adminBillingBatchApplicationState(current),
				adminBillingBatchApplicationState(
					expectedAfter,
				),
				failedAt,
			); err != nil {
				return err
			}

			tag, err := tx.Exec(
				ctx,
				`
UPDATE tokenio_billing_charge_batches
SET billing_status = 'failed',
    billing_response_balance_cents = NULL,
    billing_error_code = $2,
    charged_at = NULL,
    failed_at = $3,
    updated_at = $3
WHERE id = $1
  AND billing_status = 'failed'
`,
				batchID,
				billingErrorCode,
				postgresAdminTime(failedAt),
			)
			if err != nil {
				return normalizeAdminWriteError(err)
			}
			if tag.RowsAffected() != 1 {
				return ports.ErrAdminStateConflict
			}

			updated, err := scanBillingChargeBatch(
				tx.QueryRow(
					ctx,
					findBillingChargeBatchForUpdateSQL,
					batchID,
				),
			)
			if err != nil {
				return err
			}
			if !sameAdminBillingBatch(
				updated,
				expectedAfter,
			) {
				return ports.ErrStoreContractViolation
			}

			persistedAudit := canonicalBillingRetryAudit(
				audit,
				current,
				updated,
			)
			return insertAdminAudit(ctx, tx, persistedAudit)
		},
	)
}

func verifyAdminAuditReplay(
	ctx context.Context,
	tx pgx.Tx,
	expected domain.AuditContext,
) error {
	entry, err := scanAdminAuditEntry(
		tx.QueryRow(
			ctx,
			`
SELECT
`+adminAuditColumns+`
FROM tokenio_admin_audit_log
WHERE id = $1
`,
			expected.ID,
		),
	)
	if err != nil {
		return ports.ErrAdminStateConflict
	}
	expectedEntry := domain.AdminAuditEntry{
		ID:           expected.ID,
		AdminSubject: expected.AdminSubject,
		Action:       expected.Action,
		EntityType:   expected.EntityType,
		EntityID:     expected.EntityID,
		BeforeState:  expected.BeforeState,
		AfterState:   expected.AfterState,
		RequestID:    expected.RequestID,
		CreatedAt:    expected.CreatedAt,
	}
	if !sameAuditEntry(entry, expectedEntry) {
		return ports.ErrAdminStateConflict
	}
	return nil
}

func validateAdminUsageWindow(
	from *time.Time,
	to *time.Time,
) error {
	if from != nil && !isAdminUTCTime(*from) {
		return ports.ErrStoreContractViolation
	}
	if to != nil && !isAdminUTCTime(*to) {
		return ports.ErrStoreContractViolation
	}
	if from != nil && to != nil && !from.Before(*to) {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func buildAdminUsageFilter(
	filter ports.UsageListFilter,
) (string, []any) {
	var clauses []string
	var args []any

	add := func(expression string, value any) {
		args = append(args, value)
		clauses = append(
			clauses,
			fmt.Sprintf(expression, len(args)),
		)
	}

	if filter.UserID != "" {
		add("user_id = $%d", filter.UserID)
	}
	if filter.Status != "" {
		add("status = $%d", string(filter.Status))
	}
	if filter.ProviderType != "" {
		add("provider_type = $%d", string(filter.ProviderType))
	}
	if filter.ClientModel != "" {
		add("client_model = $%d", filter.ClientModel)
	}
	if filter.SelectedRouteID != "" {
		add("selected_route_id = $%d", filter.SelectedRouteID)
	}
	if filter.SelectedResellerID != "" {
		add(
			"selected_reseller_id = $%d",
			filter.SelectedResellerID,
		)
	}
	if filter.CreatedFrom != nil {
		add("created_at >= $%d", filter.CreatedFrom.UTC())
	}
	if filter.CreatedTo != nil {
		add("created_at < $%d", filter.CreatedTo.UTC())
	}

	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func buildAdminBillingBatchFilter(
	filter ports.BillingChargeBatchListFilter,
) (string, []any) {
	var clauses []string
	var args []any

	add := func(expression string, value any) {
		args = append(args, value)
		clauses = append(
			clauses,
			fmt.Sprintf(expression, len(args)),
		)
	}

	if filter.UserID != "" {
		add("user_id = $%d", filter.UserID)
	}
	if filter.ProviderType != "" {
		add("provider_type = $%d", string(filter.ProviderType))
	}
	if filter.ClientModel != "" {
		add("client_model = $%d", filter.ClientModel)
	}
	if filter.Status != "" {
		add("billing_status = $%d", string(filter.Status))
	}
	if filter.CreatedFrom != nil {
		add("created_at >= $%d", filter.CreatedFrom.UTC())
	}
	if filter.CreatedTo != nil {
		add("created_at < $%d", filter.CreatedTo.UTC())
	}

	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}
