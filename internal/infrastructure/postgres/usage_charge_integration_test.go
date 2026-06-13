package postgres

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestUsageLedgerChargeCommandIntegration(t *testing.T) {
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

	ledger, err := NewUsageLedger(db)
	if err != nil {
		t.Fatalf("NewUsageLedger: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	userID := "charge-user-" + suffix
	keyID := "charge-key-" + suffix
	resellerID := "charge-reseller-" + suffix
	routeID := "charge-route-" + suffix
	requestID := "charge-request-" + suffix
	batchID := "charge-batch-" + suffix
	allocationID := "charge-allocation-" + suffix
	model := "charge-model-" + suffix
	now := time.Now().UTC().Truncate(time.Microsecond)

	t.Cleanup(func() {
		statements := []struct {
			sql string
			arg string
		}{
			{
				"DELETE FROM tokenio_billing_charge_expected_records WHERE batch_id = $1",
				batchID,
			},
			{
				"DELETE FROM tokenio_billing_charge_allocations WHERE batch_id = $1",
				batchID,
			},
			{
				"DELETE FROM tokenio_billing_charge_batches WHERE id = $1",
				batchID,
			},
			{
				"DELETE FROM tokenio_usage_records WHERE local_request_id = $1",
				requestID,
			},
			{"DELETE FROM tokenio_routes WHERE id = $1", routeID},
			{"DELETE FROM tokenio_resellers WHERE id = $1", resellerID},
			{"DELETE FROM tokenio_api_keys WHERE id = $1", keyID},
			{"DELETE FROM tokenio_users WHERE id = $1", userID},
		}
		for _, statement := range statements {
			_, _ = db.Exec(
				context.Background(),
				statement.sql,
				statement.arg,
			)
		}
	})

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

	reservedAt := now.Add(-time.Minute)
	billableAt := now.Add(-time.Second)
	record := domain.UsageRecord{
		LocalRequestID:             requestID,
		UserID:                     userID,
		APIKeyID:                   keyID,
		APIFamily:                  domain.APIFamilyOpenAICompatible,
		EndpointKind:               domain.EndpointChat,
		ClientModel:                model,
		BillingModel:               "openai:" + model,
		SelectedRouteID:            routeID,
		SelectedResellerID:         resellerID,
		ProviderType:               domain.ProviderOpenAI,
		ProviderModel:              model,
		EstimatedUsage:             domain.TokenUsage{InputTokens: 10, OutputTokens: 5},
		Usage:                      domain.TokenUsage{InputTokens: 8, OutputTokens: 4},
		EstimatedClientAmountCents: 100,
		EstimatedUpstreamCostCents: 40,
		ClientAmountCents:          90,
		RemainingAmountCents:       90,
		ActualUpstreamCostCents:    35,
		Currency:                   "RUB",
		UsageCompleteness:          "detailed",
		Status:                     domain.UsageStatusBillable,
		CreatedAt:                  now.Add(-time.Hour),
		ReservedAt:                 &reservedAt,
		BillableAt:                 &billableAt,
		UpdatedAt:                  billableAt,
	}
	if _, err := db.Exec(
		ctx,
		insertUsageRecordSQL,
		usageRecordNamedArgs(record),
	); err != nil {
		t.Fatalf("insert usage: %v", err)
	}

	batch := domain.BillingChargeBatch{
		ID:                   batchID,
		UserID:               userID,
		BillingSubjectUserID: "billing-" + suffix,
		ProviderType:         domain.ProviderOpenAI,
		ClientModel:          model,
		BillingModel:         "openai:" + model,
		InputTokens:          8,
		OutputTokens:         4,
		AmountCents:          90,
		Currency:             "RUB",
		Status:               domain.BillingChargeStatusPending,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	allocation := domain.BillingChargeAllocation{
		ID:                   allocationID,
		BatchID:              batchID,
		LocalRequestID:       requestID,
		ChargedAmountCents:   90,
		RemainingAmountCents: 0,
		CreatedAt:            now,
	}
	plan := ports.UsageChargeBatchPlan{
		Batch:           batch,
		Allocations:     []domain.BillingChargeAllocation{allocation},
		ExpectedRecords: []domain.UsageRecord{record},
	}

	prepared, err := ledger.PrepareChargeBatch(ctx, plan)
	if err != nil {
		t.Fatalf("PrepareChargeBatch: %v", err)
	}
	if prepared.Batch.Status != domain.BillingChargeStatusPending ||
		prepared.ExpectedRecords[0].BillingChargeRequestID != batchID {
		t.Fatalf("prepared = %+v", prepared)
	}

	replay := plan
	replay.Batch.CreatedAt = now.Add(time.Hour)
	replay.Batch.UpdatedAt = replay.Batch.CreatedAt
	replay.Allocations = append(
		[]domain.BillingChargeAllocation(nil),
		plan.Allocations...,
	)
	replay.Allocations[0].CreatedAt = replay.Batch.CreatedAt

	replayed, err := ledger.PrepareChargeBatch(ctx, replay)
	if err != nil {
		t.Fatalf("PrepareChargeBatch replay: %v", err)
	}
	if !replayed.Batch.CreatedAt.Equal(now) {
		t.Fatalf("replay replaced persisted timestamp: %+v", replayed.Batch)
	}

	if err := ledger.MarkChargeBatchFailed(
		ctx,
		batchID,
		domain.BillingChargeStatusPending,
		"billing_unavailable",
		now.Add(time.Second),
	); err != nil {
		t.Fatalf("MarkChargeBatchFailed: %v", err)
	}
	if err := ledger.MarkChargeBatchFailed(
		ctx,
		batchID,
		domain.BillingChargeStatusFailed,
		"billing_unavailable",
		now.Add(time.Hour),
	); err != nil {
		t.Fatalf("MarkChargeBatchFailed replay: %v", err)
	}

	open, err := ledger.LoadOpenChargeBatches(
		ctx,
		userID,
		"billing-"+suffix,
		"RUB",
	)
	if err != nil {
		t.Fatalf("LoadOpenChargeBatches: %v", err)
	}
	if len(open) != 1 ||
		open[0].Batch.Status != domain.BillingChargeStatusFailed {
		t.Fatalf("open = %+v", open)
	}

	chargedAt := now.Add(2 * time.Second)
	balance := int64(1000)
	success := ports.UsageChargeSuccess{
		BatchID:             batchID,
		BillingBalanceCents: &balance,
		ChargedAt:           chargedAt,
		Allocations:         prepared.Allocations,
		ExpectedRecords:     prepared.ExpectedRecords,
	}
	if err := ledger.ApplyChargeSuccess(ctx, success); err != nil {
		t.Fatalf("ApplyChargeSuccess: %v", err)
	}
	if err := ledger.ApplyChargeSuccess(ctx, success); err != nil {
		t.Fatalf("ApplyChargeSuccess replay: %v", err)
	}

	found, err := ledger.FindByLocalRequestID(ctx, requestID)
	if err != nil {
		t.Fatalf("FindByLocalRequestID: %v", err)
	}
	if found.Status != domain.UsageStatusCharged ||
		found.RemainingAmountCents != 0 ||
		found.ChargedAmountCents != 90 ||
		found.BillingChargeRequestID != batchID {
		t.Fatalf("charged usage = %+v", found)
	}

	open, err = ledger.LoadOpenChargeBatches(
		ctx,
		userID,
		"billing-"+suffix,
		"RUB",
	)
	if err != nil {
		t.Fatalf("LoadOpenChargeBatches after success: %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("open after success = %+v", open)
	}
}

func insertChargeTestRegistry(
	t *testing.T,
	db *DB,
	userID string,
	keyID string,
	resellerID string,
	routeID string,
	model string,
	suffix string,
	now time.Time,
) {
	t.Helper()
	ctx := t.Context()

	statements := []struct {
		sql  string
		args []any
	}{
		{
			`
INSERT INTO tokenio_users (
    id,
    external_billing_user_id,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $3)
`,
			[]any{userID, "billing-" + suffix, now},
		},
		{
			`
INSERT INTO tokenio_api_keys (
    id,
    user_id,
    name,
    key_hash,
    key_prefix,
    created_at,
    updated_at
)
VALUES ($1, $2, 'charge-test', $3, 'sk_test', $4, $4)
`,
			[]any{keyID, userID, "charge-hash-" + suffix, now},
		},
		{
			`
INSERT INTO tokenio_resellers (
    id,
    name,
    provider_type,
    base_url,
    api_key_env,
    balance_cents,
    created_at,
    updated_at
)
VALUES ($1, 'charge-test', 'openai', 'https://example.test', $2, 100000, $3, $3)
`,
			[]any{resellerID, "CHARGE_TEST_KEY_" + suffix, now},
		},
		{
			`
INSERT INTO tokenio_routes (
    id,
    reseller_id,
    provider_type,
    api_family,
    endpoint_kind,
    client_model,
    provider_model,
    created_at,
    updated_at
)
VALUES ($1, $2, 'openai', 'openai_compatible', 'chat', $3, $3, $4, $4)
`,
			[]any{routeID, resellerID, model, now},
		},
	}

	for _, statement := range statements {
		if _, err := db.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("insert registry dependency: %v", err)
		}
	}
}
