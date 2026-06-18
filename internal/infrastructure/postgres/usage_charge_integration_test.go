package postgres

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestUsageLedgerChargeCommandIntegration(t *testing.T) {
	ctx := t.Context()
	db := openIsolatedPostgresIntegrationDB(t)

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

	failureAt := now.Add(time.Second)
	if err := ledger.MarkChargeBatchFailed(
		ctx,
		batchID,
		domain.BillingChargeStatusPending,
		"billing_unavailable",
		failureAt,
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
	assertFailedBatchTimestamps(t, open[0].Batch, now, failureAt)

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
	assertChargedUsageTimestamps(
		t,
		*found,
		record.CreatedAt,
		reservedAt,
		billableAt,
		chargedAt,
	)

	completed, err := loadBillingChargeSnapshot(ctx, db, batchID, false)
	if err != nil {
		t.Fatalf("load completed charge snapshot: %v", err)
	}
	if completed.Batch.Status != domain.BillingChargeStatusSucceeded {
		t.Fatalf("completed batch = %+v", completed.Batch)
	}
	assertSucceededBatchTimestamps(t, completed.Batch, now, chargedAt)

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

func assertFailedBatchTimestamps(
	t *testing.T,
	batch domain.BillingChargeBatch,
	createdAt time.Time,
	failedAt time.Time,
) {
	t.Helper()

	assertUsageTime(t, "batch.created_at", batch.CreatedAt, createdAt)
	assertUsageTimePointer(t, "batch.charged_at", batch.ChargedAt, nil)
	assertUsageTimePointer(t, "batch.failed_at", batch.FailedAt, &failedAt)
	assertUsageTime(t, "batch.updated_at", batch.UpdatedAt, failedAt)
}

func assertSucceededBatchTimestamps(
	t *testing.T,
	batch domain.BillingChargeBatch,
	createdAt time.Time,
	chargedAt time.Time,
) {
	t.Helper()

	assertUsageTime(t, "batch.created_at", batch.CreatedAt, createdAt)
	assertUsageTimePointer(t, "batch.charged_at", batch.ChargedAt, &chargedAt)
	assertUsageTimePointer(t, "batch.failed_at", batch.FailedAt, nil)
	assertUsageTime(t, "batch.updated_at", batch.UpdatedAt, chargedAt)
}

func assertChargedUsageTimestamps(
	t *testing.T,
	record domain.UsageRecord,
	createdAt time.Time,
	reservedAt time.Time,
	billableAt time.Time,
	chargedAt time.Time,
) {
	t.Helper()

	assertUsageTime(t, "usage.created_at", record.CreatedAt, createdAt)
	assertUsageTimePointer(t, "usage.reserved_at", record.ReservedAt, &reservedAt)
	assertUsageTimePointer(t, "usage.released_at", record.ReleasedAt, nil)
	assertUsageTimePointer(t, "usage.billable_at", record.BillableAt, &billableAt)
	assertUsageTimePointer(t, "usage.charged_at", record.ChargedAt, &chargedAt)
	assertUsageTimePointer(t, "usage.failed_at", record.FailedAt, nil)
	assertUsageTime(t, "usage.updated_at", record.UpdatedAt, chargedAt)
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
    default_max_output_tokens,
    capabilities,
    created_at,
    updated_at
)
VALUES (
    $1,
    $2,
    'openai',
    'openai_compatible',
    'chat',
    $3,
    $3,
    1024,
    '{"chat":true}'::jsonb,
    $4,
    $4
)
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

func TestUsageChargeSubjectDiscoveryIncludesPartialAndExcludesActiveClaim(
	t *testing.T,
) {
	ctx := t.Context()
	db := openIsolatedPostgresIntegrationDB(t)
	ledger, err := NewUsageLedger(db)
	if err != nil {
		t.Fatalf("NewUsageLedger: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	userID := "discovery-user-" + suffix
	keyID := "discovery-key-" + suffix
	resellerID := "discovery-reseller-" + suffix
	routeID := "discovery-route-" + suffix
	model := "discovery-model-" + suffix
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

	newRecord := func(
		requestID string,
		createdAt time.Time,
	) domain.UsageRecord {
		reservedAt := createdAt.Add(time.Second)
		billableAt := reservedAt.Add(time.Second)
		return domain.UsageRecord{
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
			EstimatedUsage:             domain.TokenUsage{InputTokens: 10},
			Usage:                      domain.TokenUsage{InputTokens: 10},
			EstimatedClientAmountCents: 100,
			EstimatedUpstreamCostCents: 40,
			ClientAmountCents:          100,
			RemainingAmountCents:       100,
			ActualUpstreamCostCents:    40,
			Currency:                   "RUB",
			UsageCompleteness:          "detailed",
			Status:                     domain.UsageStatusBillable,
			CreatedAt:                  createdAt,
			ReservedAt:                 &reservedAt,
			BillableAt:                 &billableAt,
			UpdatedAt:                  billableAt,
		}
	}

	unclaimed := newRecord(
		"discovery-unclaimed-"+suffix,
		now.Add(-3*time.Hour),
	)
	active := newRecord(
		"discovery-active-"+suffix,
		now.Add(-2*time.Hour),
	)
	partial := newRecord(
		"discovery-partial-"+suffix,
		now.Add(-time.Hour),
	)
	for _, record := range []domain.UsageRecord{
		unclaimed,
		active,
		partial,
	} {
		if _, err := db.Exec(
			ctx,
			insertUsageRecordSQL,
			usageRecordNamedArgs(record),
		); err != nil {
			t.Fatalf(
				"insert usage %s: %v",
				record.LocalRequestID,
				err,
			)
		}
	}

	activeBatchID := "discovery-active-batch-" + suffix
	activePlan := ports.UsageChargeBatchPlan{
		Batch: domain.BillingChargeBatch{
			ID:                   activeBatchID,
			UserID:               userID,
			BillingSubjectUserID: "billing-" + suffix,
			ProviderType:         domain.ProviderOpenAI,
			ClientModel:          model,
			BillingModel:         "openai:" + model,
			InputTokens:          10,
			AmountCents:          100,
			Currency:             "RUB",
			Status:               domain.BillingChargeStatusPending,
			CreatedAt:            now,
			UpdatedAt:            now,
		},
		Allocations: []domain.BillingChargeAllocation{
			{
				ID:                   "discovery-active-allocation-" + suffix,
				BatchID:              activeBatchID,
				LocalRequestID:       active.LocalRequestID,
				ChargedAmountCents:   100,
				RemainingAmountCents: 0,
				CreatedAt:            now,
			},
		},
		ExpectedRecords: []domain.UsageRecord{active},
	}
	if _, err := ledger.PrepareChargeBatch(ctx, activePlan); err != nil {
		t.Fatalf("prepare active batch: %v", err)
	}

	partialBatchID := "discovery-partial-batch-" + suffix
	partialPlan := ports.UsageChargeBatchPlan{
		Batch: domain.BillingChargeBatch{
			ID:                   partialBatchID,
			UserID:               userID,
			BillingSubjectUserID: "billing-" + suffix,
			ProviderType:         domain.ProviderOpenAI,
			ClientModel:          model,
			BillingModel:         "openai:" + model,
			InputTokens:          5,
			AmountCents:          50,
			Currency:             "RUB",
			Status:               domain.BillingChargeStatusPending,
			CreatedAt:            now.Add(time.Second),
			UpdatedAt:            now.Add(time.Second),
		},
		Allocations: []domain.BillingChargeAllocation{
			{
				ID:                   "discovery-partial-allocation-" + suffix,
				BatchID:              partialBatchID,
				LocalRequestID:       partial.LocalRequestID,
				ChargedAmountCents:   50,
				RemainingAmountCents: 50,
				CreatedAt:            now.Add(time.Second),
			},
		},
		ExpectedRecords: []domain.UsageRecord{partial},
	}
	preparedPartial, err := ledger.PrepareChargeBatch(
		ctx,
		partialPlan,
	)
	if err != nil {
		t.Fatalf("prepare partial batch: %v", err)
	}
	chargedAt := now.Add(2 * time.Second)
	if err := ledger.ApplyChargeSuccess(
		ctx,
		ports.UsageChargeSuccess{
			BatchID:         partialBatchID,
			ChargedAt:       chargedAt,
			Allocations:     preparedPartial.Allocations,
			ExpectedRecords: preparedPartial.ExpectedRecords,
		},
	); err != nil {
		t.Fatalf("apply partial success: %v", err)
	}

	subjects, err := ledger.ListChargeableBillingSubjects(ctx, 10)
	if err != nil {
		t.Fatalf("ListChargeableBillingSubjects: %v", err)
	}
	if len(subjects) != 1 {
		t.Fatalf("subjects=%+v, want one", subjects)
	}
	if subjects[0].UserID != userID ||
		subjects[0].BillingSubjectUserID !=
			"billing-"+suffix ||
		subjects[0].Currency != "RUB" ||
		!subjects[0].OldestChargeableAt.Equal(
			unclaimed.CreatedAt,
		) {
		t.Fatalf("subject=%+v", subjects[0])
	}

	candidates, err := ledger.LoadChargeCandidates(
		ctx,
		userID,
		"RUB",
	)
	if err != nil {
		t.Fatalf("LoadChargeCandidates: %v", err)
	}
	got := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		got = append(got, candidate.LocalRequestID)
	}
	want := []string{
		unclaimed.LocalRequestID,
		partial.LocalRequestID,
	}
	if len(got) != len(want) {
		t.Fatalf("candidate IDs=%v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("candidate IDs=%v, want %v", got, want)
		}
	}
}
