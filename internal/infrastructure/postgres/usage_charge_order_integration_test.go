package postgres

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestUsageLedgerChargeCommandOrderedImmutableIntegration(t *testing.T) {
	ctx := t.Context()
	db := openIsolatedPostgresIntegrationDB(t)

	ledger, err := NewUsageLedger(db)
	if err != nil {
		t.Fatalf("NewUsageLedger: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	userID := "charge-order-user-" + suffix
	keyID := "charge-order-key-" + suffix
	resellerID := "charge-order-reseller-" + suffix
	routeID := "charge-order-route-" + suffix
	batchID := "charge-order-batch-" + suffix
	model := "charge-order-model-" + suffix
	now := time.Now().UTC().Truncate(time.Microsecond)

	requestIDs := []string{
		"charge-order-request-z-" + suffix,
		"charge-order-request-a-" + suffix,
		"charge-order-request-m-" + suffix,
	}
	allocationIDs := []string{
		"charge-order-allocation-z-" + suffix,
		"charge-order-allocation-a-" + suffix,
		"charge-order-allocation-m-" + suffix,
	}
	chargedAmounts := []int64{30, 20, 40}

	t.Cleanup(func() {
		for _, statement := range []struct {
			sql  string
			args []any
		}{
			{"DELETE FROM tokenio_billing_charge_expected_records WHERE batch_id = $1", []any{batchID}},
			{"DELETE FROM tokenio_billing_charge_allocations WHERE batch_id = $1", []any{batchID}},
			{"DELETE FROM tokenio_billing_charge_batches WHERE id = $1", []any{batchID}},
			{"DELETE FROM tokenio_usage_records WHERE user_id = $1", []any{userID}},
			{"DELETE FROM tokenio_routes WHERE id = $1", []any{routeID}},
			{"DELETE FROM tokenio_resellers WHERE id = $1", []any{resellerID}},
			{"DELETE FROM tokenio_api_keys WHERE id = $1", []any{keyID}},
			{"DELETE FROM tokenio_users WHERE id = $1", []any{userID}},
		} {
			_, _ = db.Exec(context.Background(), statement.sql, statement.args...)
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

	records := make([]domain.UsageRecord, len(requestIDs))
	allocations := make([]domain.BillingChargeAllocation, len(requestIDs))
	for index := range requestIDs {
		reservedAt := now.Add(-time.Minute)
		billableAt := now.Add(-time.Duration(index+1) * time.Second)
		records[index] = domain.UsageRecord{
			LocalRequestID:             requestIDs[index],
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
			EstimatedUsage:             domain.TokenUsage{InputTokens: int64(index + 1)},
			Usage:                      domain.TokenUsage{InputTokens: int64((index + 1) * 10), OutputTokens: int64(index + 1)},
			EstimatedClientAmountCents: chargedAmounts[index],
			EstimatedUpstreamCostCents: int64(index + 1),
			ClientAmountCents:          chargedAmounts[index],
			RemainingAmountCents:       chargedAmounts[index],
			ActualUpstreamCostCents:    int64(index + 1),
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
			usageRecordNamedArgs(records[index]),
		); err != nil {
			t.Fatalf("insert usage[%d]: %v", index, err)
		}

		allocations[index] = domain.BillingChargeAllocation{
			ID:                   allocationIDs[index],
			BatchID:              batchID,
			LocalRequestID:       requestIDs[index],
			ChargedAmountCents:   chargedAmounts[index],
			RemainingAmountCents: 0,
			CreatedAt:            now,
		}
	}

	batch := domain.BillingChargeBatch{
		ID:                   batchID,
		UserID:               userID,
		BillingSubjectUserID: "billing-" + suffix,
		ProviderType:         domain.ProviderOpenAI,
		ClientModel:          model,
		BillingModel:         "openai:" + model,
		InputTokens:          60,
		OutputTokens:         6,
		AmountCents:          90,
		Currency:             "RUB",
		Status:               domain.BillingChargeStatusPending,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	plan := ports.UsageChargeBatchPlan{
		Batch:           batch,
		Allocations:     allocations,
		ExpectedRecords: records,
	}

	prepared, err := ledger.PrepareChargeBatch(ctx, plan)
	if err != nil {
		t.Fatalf("PrepareChargeBatch: %v", err)
	}
	assertChargeSnapshotOrder(t, prepared, requestIDs)

	persisted, err := loadBillingChargeSnapshot(ctx, db, batchID, false)
	if err != nil {
		t.Fatalf("loadBillingChargeSnapshot: %v", err)
	}
	assertChargeSnapshotOrder(t, persisted, requestIDs)

	assertDurableChargeForeignKeysProtectSnapshot(
		t,
		ctx,
		db,
		batchID,
		requestIDs[0],
		persisted,
	)

	replay := cloneUsageChargeBatchPlan(plan)
	replay.Batch.CreatedAt = now.Add(time.Hour)
	replay.Batch.UpdatedAt = replay.Batch.CreatedAt
	for index := range replay.Allocations {
		replay.Allocations[index].CreatedAt = replay.Batch.CreatedAt
	}
	replayed, err := ledger.PrepareChargeBatch(ctx, replay)
	if err != nil {
		t.Fatalf("PrepareChargeBatch replay: %v", err)
	}
	if !reflect.DeepEqual(replayed, persisted) {
		t.Fatalf("replay changed persisted snapshot:\n got=%+v\nwant=%+v", replayed, persisted)
	}

	reordered := cloneUsageChargeBatchPlan(plan)
	reordered.Allocations[0], reordered.Allocations[1] =
		reordered.Allocations[1], reordered.Allocations[0]
	reordered.ExpectedRecords[0], reordered.ExpectedRecords[1] =
		reordered.ExpectedRecords[1], reordered.ExpectedRecords[0]
	if _, err := ledger.PrepareChargeBatch(ctx, reordered); !errors.Is(err, ports.ErrStoreConflict) {
		t.Fatalf("reordered replay err=%v, want ErrStoreConflict", err)
	}
	assertPersistedChargeSnapshotUnchanged(t, ctx, db, batchID, persisted)

	changedAmount := cloneUsageChargeBatchPlan(plan)
	changedAmount.Batch.AmountCents++
	if _, err := ledger.PrepareChargeBatch(ctx, changedAmount); !errors.Is(err, ports.ErrStoreConflict) {
		t.Fatalf("amount replay err=%v, want ErrStoreConflict", err)
	}
	assertPersistedChargeSnapshotUnchanged(t, ctx, db, batchID, persisted)

	open, err := ledger.LoadOpenChargeBatches(
		ctx,
		userID,
		"billing-"+suffix,
		"RUB",
	)
	if err != nil {
		t.Fatalf("LoadOpenChargeBatches: %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("open batch count=%d, want 1", len(open))
	}
	assertChargeSnapshotOrder(t, open[0], requestIDs)
	if !reflect.DeepEqual(open[0], persisted) {
		t.Fatalf("open snapshot differs:\n got=%+v\nwant=%+v", open[0], persisted)
	}
}

func assertDurableChargeForeignKeysProtectSnapshot(
	t *testing.T,
	ctx context.Context,
	db *DB,
	batchID string,
	localRequestID string,
	want ports.BillingChargeBatchSnapshot,
) {
	t.Helper()

	if _, err := db.Exec(
		ctx,
		"DELETE FROM tokenio_billing_charge_batches WHERE id = $1",
		batchID,
	); err == nil {
		t.Fatal("deleting batch with durable children unexpectedly succeeded")
	}
	assertPersistedChargeSnapshotUnchanged(t, ctx, db, batchID, want)

	if _, err := db.Exec(
		ctx,
		"DELETE FROM tokenio_usage_records WHERE local_request_id = $1",
		localRequestID,
	); err == nil {
		t.Fatal("deleting usage record referenced by durable command unexpectedly succeeded")
	}
	assertPersistedChargeSnapshotUnchanged(t, ctx, db, batchID, want)
}

func cloneUsageChargeBatchPlan(
	value ports.UsageChargeBatchPlan,
) ports.UsageChargeBatchPlan {
	value.Allocations = append(
		[]domain.BillingChargeAllocation(nil),
		value.Allocations...,
	)
	value.ExpectedRecords = append(
		[]domain.UsageRecord(nil),
		value.ExpectedRecords...,
	)
	return value
}

func assertChargeSnapshotOrder(
	t *testing.T,
	snapshot ports.BillingChargeBatchSnapshot,
	requestIDs []string,
) {
	t.Helper()

	if len(snapshot.Allocations) != len(requestIDs) ||
		len(snapshot.ExpectedRecords) != len(requestIDs) {
		t.Fatalf(
			"snapshot lengths allocations=%d expected=%d want=%d",
			len(snapshot.Allocations),
			len(snapshot.ExpectedRecords),
			len(requestIDs),
		)
	}
	for index, requestID := range requestIDs {
		if snapshot.Allocations[index].LocalRequestID != requestID ||
			snapshot.ExpectedRecords[index].LocalRequestID != requestID ||
			snapshot.ExpectedRecords[index].BillingChargeRequestID != snapshot.Batch.ID {
			t.Fatalf(
				"snapshot position %d allocation=%q expected=%q claim=%q want request=%q batch=%q",
				index,
				snapshot.Allocations[index].LocalRequestID,
				snapshot.ExpectedRecords[index].LocalRequestID,
				snapshot.ExpectedRecords[index].BillingChargeRequestID,
				requestID,
				snapshot.Batch.ID,
			)
		}
	}
}

func assertPersistedChargeSnapshotUnchanged(
	t *testing.T,
	ctx context.Context,
	db *DB,
	batchID string,
	want ports.BillingChargeBatchSnapshot,
) {
	t.Helper()

	got, err := loadBillingChargeSnapshot(ctx, db, batchID, false)
	if err != nil {
		t.Fatalf("load persisted charge snapshot: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("persisted snapshot mutated:\n got=%+v\nwant=%+v", got, want)
	}
}
