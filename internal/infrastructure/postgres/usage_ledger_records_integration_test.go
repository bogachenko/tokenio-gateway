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

func assertUsageRecordReservedTimestamps(
	t *testing.T,
	record domain.UsageRecord,
	createdAt time.Time,
	reservedAt time.Time,
) {
	t.Helper()

	assertUsageTime(t, "created_at", record.CreatedAt, createdAt)
	assertUsageTimePointer(t, "reserved_at", record.ReservedAt, &reservedAt)
	assertUsageTimePointer(t, "released_at", record.ReleasedAt, nil)
	assertUsageTimePointer(t, "billable_at", record.BillableAt, nil)
	assertUsageTimePointer(t, "charged_at", record.ChargedAt, nil)
	assertUsageTimePointer(t, "failed_at", record.FailedAt, nil)
	assertUsageTime(t, "updated_at", record.UpdatedAt, createdAt)
}

func assertUsageRecordBillableTimestamps(
	t *testing.T,
	record domain.UsageRecord,
	createdAt time.Time,
	reservedAt time.Time,
	billableAt time.Time,
) {
	t.Helper()

	assertUsageTime(t, "created_at", record.CreatedAt, createdAt)
	assertUsageTimePointer(t, "reserved_at", record.ReservedAt, &reservedAt)
	assertUsageTimePointer(t, "released_at", record.ReleasedAt, nil)
	assertUsageTimePointer(t, "billable_at", record.BillableAt, &billableAt)
	assertUsageTimePointer(t, "charged_at", record.ChargedAt, nil)
	assertUsageTimePointer(t, "failed_at", record.FailedAt, nil)
	assertUsageTime(t, "updated_at", record.UpdatedAt, billableAt)
}

func assertUsageTime(
	t *testing.T,
	name string,
	got time.Time,
	want time.Time,
) {
	t.Helper()

	if got.Location() != time.UTC {
		t.Fatalf("%s location=%v, want UTC", name, got.Location())
	}
	if !got.Equal(want) {
		t.Fatalf("%s=%s, want %s", name, got, want)
	}
}

func assertUsageTimePointer(
	t *testing.T,
	name string,
	got *time.Time,
	want *time.Time,
) {
	t.Helper()

	switch {
	case got == nil && want == nil:
		return
	case got == nil || want == nil:
		t.Fatalf("%s got=%v want=%v", name, got, want)
	default:
		assertUsageTime(t, name, *got, *want)
	}
}

func assertUsageRecordReleasedTimestamps(
	t *testing.T,
	record domain.UsageRecord,
	createdAt time.Time,
	reservedAt time.Time,
	releasedAt time.Time,
) {
	t.Helper()

	assertUsageTime(t, "released.created_at", record.CreatedAt, createdAt)
	assertUsageTimePointer(t, "released.reserved_at", record.ReservedAt, &reservedAt)
	assertUsageTimePointer(t, "released.released_at", record.ReleasedAt, &releasedAt)
	assertUsageTimePointer(t, "released.billable_at", record.BillableAt, nil)
	assertUsageTimePointer(t, "released.charged_at", record.ChargedAt, nil)
	assertUsageTimePointer(t, "released.failed_at", record.FailedAt, nil)
	assertUsageTime(t, "released.updated_at", record.UpdatedAt, releasedAt)
}

func assertUsageRecordFailedTimestamps(
	t *testing.T,
	record domain.UsageRecord,
	createdAt time.Time,
	reservedAt time.Time,
	failedAt time.Time,
) {
	t.Helper()

	assertUsageTime(t, "failed.created_at", record.CreatedAt, createdAt)
	assertUsageTimePointer(t, "failed.reserved_at", record.ReservedAt, &reservedAt)
	assertUsageTimePointer(t, "failed.released_at", record.ReleasedAt, nil)
	assertUsageTimePointer(t, "failed.billable_at", record.BillableAt, nil)
	assertUsageTimePointer(t, "failed.charged_at", record.ChargedAt, nil)
	assertUsageTimePointer(t, "failed.failed_at", record.FailedAt, &failedAt)
	assertUsageTime(t, "failed.updated_at", record.UpdatedAt, failedAt)
}

func TestUsageLedgerRecordLifecycleIntegration(t *testing.T) {
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
	userID := "usage-user-" + suffix
	keyID := "usage-key-" + suffix
	resellerID := "usage-reseller-" + suffix
	routeID := "usage-route-" + suffix
	localRequestID := "usage-request-" + suffix
	now := time.Now().UTC().Truncate(time.Microsecond)

	t.Cleanup(func() {
		for _, statement := range []struct {
			sql string
			arg string
		}{
			{"DELETE FROM tokenio_usage_records WHERE user_id = $1", userID},
			{"DELETE FROM tokenio_routes WHERE id = $1", routeID},
			{"DELETE FROM tokenio_resellers WHERE id = $1", resellerID},
			{"DELETE FROM tokenio_api_keys WHERE id = $1", keyID},
			{"DELETE FROM tokenio_users WHERE id = $1", userID},
		} {
			_, _ = db.Exec(context.Background(), statement.sql, statement.arg)
		}
	})

	if _, err := db.Exec(
		ctx,
		`
INSERT INTO tokenio_users (
    id,
    external_billing_user_id,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $3)
`,
		userID,
		"billing-"+suffix,
		now,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.Exec(
		ctx,
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
VALUES ($1, $2, 'usage-test', $3, 'sk_test', $4, $4)
`,
		keyID,
		userID,
		"usage-hash-"+suffix,
		now,
	); err != nil {
		t.Fatalf("insert api key: %v", err)
	}
	if _, err := db.Exec(
		ctx,
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
VALUES ($1, 'usage-test', 'openai', 'https://example.test', $2, 100000, $3, $3)
`,
		resellerID,
		"USAGE_TEST_KEY_"+suffix,
		now,
	); err != nil {
		t.Fatalf("insert reseller: %v", err)
	}
	if _, err := db.Exec(
		ctx,
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
		routeID,
		resellerID,
		"usage-model-"+suffix,
		now,
	); err != nil {
		t.Fatalf("insert route: %v", err)
	}

	reservedAt := now
	record := domain.UsageRecord{
		LocalRequestID:     localRequestID,
		IdempotencyKey:     "usage-idempotency-" + suffix,
		UserID:             userID,
		APIKeyID:           keyID,
		APIFamily:          domain.APIFamilyOpenAICompatible,
		EndpointKind:       domain.EndpointChat,
		ClientModel:        "usage-model-" + suffix,
		BillingModel:       "openai:usage-model-" + suffix,
		SelectedRouteID:    routeID,
		SelectedResellerID: resellerID,
		ProviderType:       domain.ProviderOpenAI,
		ProviderModel:      "usage-model-" + suffix,
		EstimatedUsage: domain.TokenUsage{
			InputTokens:          1,
			CachedInputTokens:    2,
			OutputTokens:         3,
			ReasoningTokens:      4,
			ImageInputTokens:     5,
			AudioInputTokens:     6,
			AudioOutputTokens:    7,
			FileInputTokens:      8,
			VideoInputTokens:     9,
			ImageGenerationUnits: 10,
		},
		EstimatedClientAmountCents: 100,
		EstimatedUpstreamCostCents: 40,
		Currency:                   "RUB",
		UsageCompleteness:          "missing",
		Status:                     domain.UsageStatusReserved,
		CreatedAt:                  now,
		ReservedAt:                 &reservedAt,
		UpdatedAt:                  now,
	}

	created, err := ledger.CreateReserved(ctx, record)
	if err != nil {
		t.Fatalf("CreateReserved: %v", err)
	}
	if created.Outcome != ports.UsageReserveOutcomeCreated ||
		created.Existing != nil {
		t.Fatalf("created result = %+v", created)
	}

	replayed, err := ledger.CreateReserved(ctx, record)
	if err != nil {
		t.Fatalf("CreateReserved replay: %v", err)
	}
	if replayed.Outcome != ports.UsageReserveOutcomeLocalRequestExists ||
		replayed.Existing == nil ||
		replayed.Existing.LocalRequestID != localRequestID {
		t.Fatalf("replayed result = %+v", replayed)
	}

	found, err := ledger.FindByLocalRequestID(ctx, localRequestID)
	if err != nil {
		t.Fatalf("FindByLocalRequestID: %v", err)
	}
	if found.EstimatedUsage != record.EstimatedUsage {
		t.Fatalf("found record = %+v", found)
	}
	assertUsageRecordReservedTimestamps(
		t,
		*found,
		now,
		reservedAt,
	)

	billableAt := now.Add(time.Second)
	next := *found
	next.Status = domain.UsageStatusBillable
	next.Usage = domain.TokenUsage{
		InputTokens:          11,
		CachedInputTokens:    12,
		OutputTokens:         13,
		ReasoningTokens:      14,
		ImageInputTokens:     15,
		AudioInputTokens:     16,
		AudioOutputTokens:    17,
		FileInputTokens:      18,
		VideoInputTokens:     19,
		ImageGenerationUnits: 20,
	}
	next.UsageCompleteness = "detailed"
	next.ClientAmountCents = 90
	next.RemainingAmountCents = 90
	next.ActualUpstreamCostCents = 35
	next.BillableAt = &billableAt
	next.UpdatedAt = billableAt

	transition, err := ledger.CompareAndSwap(
		ctx,
		localRequestID,
		domain.UsageStatusReserved,
		next,
	)
	if err != nil {
		t.Fatalf("CompareAndSwap: %v", err)
	}
	if !transition.Applied || transition.Current != nil {
		t.Fatalf("transition = %+v", transition)
	}

	notApplied, err := ledger.CompareAndSwap(
		ctx,
		localRequestID,
		domain.UsageStatusReserved,
		next,
	)
	if err != nil {
		t.Fatalf("CompareAndSwap mismatch: %v", err)
	}
	if notApplied.Applied ||
		notApplied.Current == nil ||
		notApplied.Current.Status != domain.UsageStatusBillable {
		t.Fatalf("not-applied transition = %+v", notApplied)
	}

	exposure, err := ledger.LoadExposure(ctx, userID, "RUB")
	if err != nil {
		t.Fatalf("LoadExposure: %v", err)
	}
	if exposure.BillableRemainingAmountCents != 90 {
		t.Fatalf("exposure = %+v", exposure)
	}

	candidates, err := ledger.LoadChargeCandidates(ctx, userID, "RUB")
	if err != nil {
		t.Fatalf("LoadChargeCandidates: %v", err)
	}
	if len(candidates) != 1 ||
		candidates[0].LocalRequestID != localRequestID ||
		candidates[0].EstimatedUsage != record.EstimatedUsage ||
		candidates[0].Usage != next.Usage {
		t.Fatalf("candidates = %+v", candidates)
	}
	assertUsageRecordBillableTimestamps(
		t,
		candidates[0],
		now,
		reservedAt,
		billableAt,
	)

	releasedCreatedAt := now.Add(10 * time.Second)
	releasedReservedAt := releasedCreatedAt
	releasedRecord := record
	releasedRecord.LocalRequestID = "usage-released-" + suffix
	releasedRecord.IdempotencyKey = "usage-released-idempotency-" + suffix
	releasedRecord.CreatedAt = releasedCreatedAt
	releasedRecord.ReservedAt = &releasedReservedAt
	releasedRecord.UpdatedAt = releasedCreatedAt
	releasedRecord.BillableAt = nil

	releasedCreate, err := ledger.CreateReserved(ctx, releasedRecord)
	if err != nil {
		t.Fatalf("CreateReserved released fixture: %v", err)
	}
	if releasedCreate.Outcome != ports.UsageReserveOutcomeCreated {
		t.Fatalf("released fixture create = %+v", releasedCreate)
	}

	releasedCurrent, err := ledger.FindByLocalRequestID(
		ctx,
		releasedRecord.LocalRequestID,
	)
	if err != nil {
		t.Fatalf("FindByLocalRequestID released fixture: %v", err)
	}
	releasedAt := releasedCreatedAt.Add(time.Second)
	releasedNext := *releasedCurrent
	releasedNext.Status = domain.UsageStatusReleased
	releasedNext.ReleasedAt = &releasedAt
	releasedNext.UpdatedAt = releasedAt

	releasedTransition, err := ledger.CompareAndSwap(
		ctx,
		releasedRecord.LocalRequestID,
		domain.UsageStatusReserved,
		releasedNext,
	)
	if err != nil {
		t.Fatalf("CompareAndSwap released: %v", err)
	}
	if !releasedTransition.Applied {
		t.Fatalf("released transition = %+v", releasedTransition)
	}

	releasedFound, err := ledger.FindByLocalRequestID(
		ctx,
		releasedRecord.LocalRequestID,
	)
	if err != nil {
		t.Fatalf("FindByLocalRequestID released: %v", err)
	}
	if releasedFound.Status != domain.UsageStatusReleased {
		t.Fatalf("released record = %+v", releasedFound)
	}
	assertUsageRecordReleasedTimestamps(
		t,
		*releasedFound,
		releasedCreatedAt,
		releasedReservedAt,
		releasedAt,
	)

	failedCreatedAt := now.Add(20 * time.Second)
	failedReservedAt := failedCreatedAt
	failedRecord := record
	failedRecord.LocalRequestID = "usage-failed-" + suffix
	failedRecord.IdempotencyKey = "usage-failed-idempotency-" + suffix
	failedRecord.CreatedAt = failedCreatedAt
	failedRecord.ReservedAt = &failedReservedAt
	failedRecord.UpdatedAt = failedCreatedAt
	failedRecord.BillableAt = nil

	failedCreate, err := ledger.CreateReserved(ctx, failedRecord)
	if err != nil {
		t.Fatalf("CreateReserved failed fixture: %v", err)
	}
	if failedCreate.Outcome != ports.UsageReserveOutcomeCreated {
		t.Fatalf("failed fixture create = %+v", failedCreate)
	}

	failedCurrent, err := ledger.FindByLocalRequestID(
		ctx,
		failedRecord.LocalRequestID,
	)
	if err != nil {
		t.Fatalf("FindByLocalRequestID failed fixture: %v", err)
	}
	failedAt := failedCreatedAt.Add(time.Second)
	failedNext := *failedCurrent
	failedNext.Status = domain.UsageStatusFailed
	failedNext.UsageCompleteness = "failed"
	failedNext.FailureReason = "upstream_failed"
	failedNext.FailedAt = &failedAt
	failedNext.UpdatedAt = failedAt

	failedTransition, err := ledger.CompareAndSwap(
		ctx,
		failedRecord.LocalRequestID,
		domain.UsageStatusReserved,
		failedNext,
	)
	if err != nil {
		t.Fatalf("CompareAndSwap failed: %v", err)
	}
	if !failedTransition.Applied {
		t.Fatalf("failed transition = %+v", failedTransition)
	}

	failedFound, err := ledger.FindByLocalRequestID(
		ctx,
		failedRecord.LocalRequestID,
	)
	if err != nil {
		t.Fatalf("FindByLocalRequestID failed: %v", err)
	}
	if failedFound.Status != domain.UsageStatusFailed ||
		failedFound.FailureReason != "upstream_failed" {
		t.Fatalf("failed record = %+v", failedFound)
	}
	assertUsageRecordFailedTimestamps(
		t,
		*failedFound,
		failedCreatedAt,
		failedReservedAt,
		failedAt,
	)

	_, err = ledger.FindByLocalRequestID(ctx, "missing-"+suffix)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("missing error = %v, want ErrNotFound", err)
	}
}
