package postgres

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestAdminUsageLedgerIntegration(t *testing.T) {
	dsn := os.Getenv("TOKENIO_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TOKENIO_TEST_DATABASE_DSN is not set")
	}

	ctx := t.Context()
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := db.ApplyMigrations(ctx); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	adminLedger, err := NewAdminUsageLedger(db)
	if err != nil {
		t.Fatal(err)
	}
	audits, err := NewAdminAuditStore(db)
	if err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	userID := "admin-usage-user-" + suffix
	keyID := "admin-usage-key-" + suffix
	resellerID := "admin-usage-reseller-" + suffix
	routeID := "admin-usage-route-" + suffix
	pricingFailedID := "admin-pricing-failed-" + suffix
	billableID := "admin-billable-" + suffix
	model := "admin-usage-model-" + suffix
	adminSubject := "admin-usage-subject-" + suffix
	now := time.Now().UTC().Truncate(time.Microsecond)

	insertChargeTestRegistry(
		t,
		db,
		userID,
		keyID,
		resellerID,
		routeID,
		model,
		suffix,
		now,
	)

	t.Cleanup(func() {
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_billing_charge_expected_records WHERE local_request_id IN ($1, $2)",
			pricingFailedID,
			billableID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_billing_charge_allocations WHERE local_request_id IN ($1, $2)",
			pricingFailedID,
			billableID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_billing_charge_batches WHERE user_id = $1",
			userID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_usage_records WHERE user_id = $1",
			userID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_routes WHERE id = $1",
			routeID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_resellers WHERE id = $1",
			resellerID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_api_keys WHERE id = $1",
			keyID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_users WHERE id = $1",
			userID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_admin_audit_log WHERE request_id LIKE $1",
			"admreq-admin-usage-"+suffix+"%",
		)
	})

	pricingFailed := adminPricingFailedRecord()
	pricingFailed.LocalRequestID = pricingFailedID
	pricingFailed.UserID = userID
	pricingFailed.APIKeyID = keyID
	pricingFailed.ClientModel = model
	pricingFailed.BillingModel = "openai:" + model
	pricingFailed.SelectedRouteID = routeID
	pricingFailed.SelectedResellerID = resellerID
	pricingFailed.ProviderModel = model
	pricingFailed.CreatedAt = now.Add(-time.Minute)
	pricingFailed.ReservedAt =
		timePointerForAdminUsage(pricingFailed.CreatedAt)
	pricingFailed.FailedAt = timePointerForAdminUsage(now)
	pricingFailed.UpdatedAt = now

	if _, err := db.Exec(
		ctx,
		insertUsageRecordSQL,
		usageRecordNamedArgs(pricingFailed),
	); err != nil {
		t.Fatalf("insert pricing_failed: %v", err)
	}

	resolvedAt := now.Add(time.Second)
	resolved := pricingFailed
	resolved.Status = domain.UsageStatusBillable
	resolved.Usage = domain.TokenUsage{
		InputTokens:  10,
		OutputTokens: 5,
	}
	resolved.UsageCompleteness = "estimated"
	resolved.ClientAmountCents = 100
	resolved.ChargedAmountCents = 0
	resolved.RemainingAmountCents = 100
	resolved.ActualUpstreamCostCents = 40
	resolved.FailureReason = ""
	resolved.BillableAt = &resolvedAt
	resolved.FailedAt = nil
	resolved.UpdatedAt = resolvedAt

	resolveAudit := domain.AuditContext{
		ID:           "audit-admin-usage-resolve-" + suffix,
		AdminSubject: adminSubject,
		Action:       domain.AuditActionUsageResolveBillable,
		EntityType:   "usage_record",
		EntityID:     pricingFailedID,
		BeforeState:  adminUsageApplicationState(pricingFailed),
		AfterState:   adminUsageApplicationState(resolved),
		RequestID:    "admreq-admin-usage-" + suffix + "-resolve",
		CreatedAt:    resolvedAt,
	}
	transition, err := adminLedger.ResolvePricingFailedWithAudit(
		ctx,
		pricingFailed,
		resolved,
		resolveAudit,
	)
	if err != nil {
		t.Fatalf("ResolvePricingFailedWithAudit: %v", err)
	}
	if !transition.Applied {
		t.Fatalf("transition = %+v", transition)
	}

	billable := resolved
	billable.LocalRequestID = billableID
	billable.CreatedAt = now.Add(2 * time.Second)
	billable.ReservedAt =
		timePointerForAdminUsage(billable.CreatedAt)
	billable.BillableAt =
		timePointerForAdminUsage(billable.CreatedAt)
	billable.UpdatedAt = billable.CreatedAt
	if _, err := db.Exec(
		ctx,
		insertUsageRecordSQL,
		usageRecordNamedArgs(billable),
	); err != nil {
		t.Fatalf("insert billable: %v", err)
	}

	plan, err := buildAdminUsageChargePlan(
		billable,
		"billing-"+suffix,
		now.Add(3*time.Second),
	)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	prepared, err := adminLedger.PrepareChargeBatch(ctx, plan)
	if err != nil {
		t.Fatalf("PrepareChargeBatch: %v", err)
	}
	failedAt := now.Add(4 * time.Second)
	if err := adminLedger.MarkChargeBatchFailed(
		ctx,
		prepared.Batch.ID,
		domain.BillingChargeStatusPending,
		"billing_unavailable",
		failedAt,
	); err != nil {
		t.Fatalf("MarkChargeBatchFailed: %v", err)
	}

	failedSnapshot, err := adminLedger.LoadChargeBatchByID(
		ctx,
		prepared.Batch.ID,
	)
	if err != nil {
		t.Fatalf("LoadChargeBatchByID: %v", err)
	}

	attemptAt := now.Add(5 * time.Second)
	attemptAudit := domain.AuditContext{
		ID:           "audit-admin-usage-attempt-" + suffix,
		AdminSubject: adminSubject,
		Action:       domain.AuditActionBillingChargeRetry,
		EntityType:   "billing_charge_batch",
		EntityID:     failedSnapshot.Batch.ID,
		BeforeState: adminBillingBatchApplicationState(
			failedSnapshot.Batch,
		),
		AfterState: adminBillingBatchApplicationState(
			failedSnapshot.Batch,
		),
		RequestID: "admreq-admin-usage-" + suffix + "-attempt",
		CreatedAt: attemptAt,
	}
	if err := adminLedger.RecordChargeRetryAttemptWithAudit(
		ctx,
		failedSnapshot,
		attemptAudit,
	); err != nil {
		t.Fatalf("RecordChargeRetryAttemptWithAudit: %v", err)
	}

	staleSnapshot := failedSnapshot
	staleSnapshot.Batch.BillingErrorCode = "different"
	staleAudit := attemptAudit
	staleAudit.ID = "audit-admin-usage-stale-" + suffix
	staleAudit.RequestID =
		"admreq-admin-usage-" + suffix + "-stale"
	if err := adminLedger.RecordChargeRetryAttemptWithAudit(
		ctx,
		staleSnapshot,
		staleAudit,
	); !errors.Is(err, ports.ErrAdminStateConflict) {
		t.Fatalf("stale attempt error = %v", err)
	}

	chargedAt := now.Add(6 * time.Second)
	balance := int64(900)
	success := ports.UsageChargeSuccess{
		BatchID:             failedSnapshot.Batch.ID,
		BillingBalanceCents: &balance,
		ChargedAt:           chargedAt,
		Allocations:         failedSnapshot.Allocations,
		ExpectedRecords:     failedSnapshot.ExpectedRecords,
	}
	succeededBatch := billingRetryAfterSuccess(
		failedSnapshot.Batch,
		success,
	)
	successAudit := domain.AuditContext{
		ID:           "audit-admin-usage-success-" + suffix,
		AdminSubject: adminSubject,
		Action:       domain.AuditActionBillingChargeRetry,
		EntityType:   "billing_charge_batch",
		EntityID:     failedSnapshot.Batch.ID,
		BeforeState: adminBillingBatchApplicationState(
			failedSnapshot.Batch,
		),
		AfterState: adminBillingBatchApplicationState(
			succeededBatch,
		),
		RequestID: "admreq-admin-usage-" + suffix + "-success",
		CreatedAt: chargedAt,
	}
	if err := adminLedger.ApplyChargeRetrySuccessWithAudit(
		ctx,
		success,
		successAudit,
	); err != nil {
		t.Fatalf("ApplyChargeRetrySuccessWithAudit: %v", err)
	}

	charged, err := adminLedger.FindByLocalRequestID(
		ctx,
		billableID,
	)
	if err != nil {
		t.Fatalf("FindByLocalRequestID: %v", err)
	}
	if charged.Status != domain.UsageStatusCharged ||
		charged.RemainingAmountCents != 0 {
		t.Fatalf("charged usage = %+v", charged)
	}

	batchPage, err := adminLedger.ListBillingChargeBatches(
		ctx,
		ports.BillingChargeBatchListFilter{
			UserID: userID,
			Status: domain.BillingChargeStatusSucceeded,
			Page:   ports.PageRequest{Limit: 10},
		},
	)
	if err != nil {
		t.Fatalf("ListBillingChargeBatches: %v", err)
	}
	if batchPage.Total != 1 ||
		len(batchPage.Items) != 1 {
		t.Fatalf("batch page = %+v", batchPage)
	}

	usagePage, err := adminLedger.ListUsageRecords(
		ctx,
		ports.UsageListFilter{
			UserID: userID,
			Page:   ports.PageRequest{Limit: 10},
		},
	)
	if err != nil {
		t.Fatalf("ListUsageRecords: %v", err)
	}
	if usagePage.Total != 2 ||
		len(usagePage.Items) != 2 {
		t.Fatalf("usage page = %+v", usagePage)
	}

	auditPage, err := audits.ListAuditEntries(
		ctx,
		ports.AuditListFilter{
			AdminSubject: adminSubject,
			Page:         ports.PageRequest{Limit: 20},
		},
	)
	if err != nil {
		t.Fatalf("ListAuditEntries: %v", err)
	}
	if auditPage.Total != 3 ||
		len(auditPage.Items) != 3 {
		t.Fatalf("audit page = %+v", auditPage)
	}
}

func buildAdminUsageChargePlan(
	record domain.UsageRecord,
	billingSubjectUserID string,
	at time.Time,
) (ports.UsageChargeBatchPlan, error) {
	batchID := "billchg_admin_" + record.LocalRequestID
	allocationID := "allocation_admin_" + record.LocalRequestID
	batch := domain.BillingChargeBatch{
		ID:                   batchID,
		UserID:               record.UserID,
		BillingSubjectUserID: billingSubjectUserID,
		ProviderType:         record.ProviderType,
		ClientModel:          record.ClientModel,
		BillingModel:         record.BillingModel,
		InputTokens:          record.Usage.InputTokens,
		OutputTokens:         record.Usage.OutputTokens,
		AmountCents:          record.RemainingAmountCents,
		Currency:             record.Currency,
		Status:               domain.BillingChargeStatusPending,
		CreatedAt:            at,
		UpdatedAt:            at,
	}
	allocation := domain.BillingChargeAllocation{
		ID:                   allocationID,
		BatchID:              batchID,
		LocalRequestID:       record.LocalRequestID,
		ChargedAmountCents:   record.RemainingAmountCents,
		RemainingAmountCents: 0,
		CreatedAt:            at,
	}
	return ports.UsageChargeBatchPlan{
		Batch:           batch,
		Allocations:     []domain.BillingChargeAllocation{allocation},
		ExpectedRecords: []domain.UsageRecord{record},
	}, nil
}
