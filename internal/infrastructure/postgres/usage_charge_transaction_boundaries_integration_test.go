package postgres

import (
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type transactionBoundaryFixture struct {
	db      *DB
	ledger  *UsageLedger
	records []domain.UsageRecord
	suffix  string
	now     time.Time
	userID  string
	model   string
}

func newTransactionBoundaryFixture(
	t *testing.T,
	recordCount int,
) transactionBoundaryFixture {
	t.Helper()

	db := openIsolatedPostgresIntegrationDB(t)
	ledger, err := NewUsageLedger(db)
	if err != nil {
		t.Fatalf("NewUsageLedger: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	userID := "tx-boundary-user-" + suffix
	keyID := "tx-boundary-key-" + suffix
	resellerID := "tx-boundary-reseller-" + suffix
	routeID := "tx-boundary-route-" + suffix
	model := "tx-boundary-model-" + suffix
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

	records := make([]domain.UsageRecord, 0, recordCount)
	for index := range recordCount {
		createdAt := now.Add(
			time.Duration(index-recordCount) * time.Minute,
		)
		reservedAt := createdAt.Add(time.Second)
		billableAt := reservedAt.Add(time.Second)
		record := domain.UsageRecord{
			LocalRequestID: "tx-boundary-request-" +
				strconv.Itoa(index) + "-" + suffix,
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
		if _, err := db.Exec(
			t.Context(),
			insertUsageRecordSQL,
			usageRecordNamedArgs(record),
		); err != nil {
			t.Fatalf("insert usage: %v", err)
		}
		records = append(records, record)
	}

	return transactionBoundaryFixture{
		db:      db,
		ledger:  ledger,
		records: records,
		suffix:  suffix,
		now:     now,
		userID:  userID,
		model:   model,
	}
}

func (f transactionBoundaryFixture) plan(
	batchID string,
	records []domain.UsageRecord,
	allocationIDs []string,
	amounts []int64,
) ports.UsageChargeBatchPlan {
	allocations := make(
		[]domain.BillingChargeAllocation,
		0,
		len(records),
	)
	var amount int64
	var inputTokens int64
	for index, record := range records {
		charged := amounts[index]
		amount += charged
		inputTokens += record.Usage.InputTokens
		allocations = append(
			allocations,
			domain.BillingChargeAllocation{
				ID:                   allocationIDs[index],
				BatchID:              batchID,
				LocalRequestID:       record.LocalRequestID,
				ChargedAmountCents:   charged,
				RemainingAmountCents: record.RemainingAmountCents - charged,
				CreatedAt:            f.now,
			},
		)
	}
	return ports.UsageChargeBatchPlan{
		Batch: domain.BillingChargeBatch{
			ID:                   batchID,
			UserID:               f.userID,
			BillingSubjectUserID: "billing-" + f.suffix,
			ProviderType:         domain.ProviderOpenAI,
			ClientModel:          f.model,
			BillingModel:         "openai:" + f.model,
			InputTokens:          inputTokens,
			AmountCents:          amount,
			Currency:             "RUB",
			Status:               domain.BillingChargeStatusPending,
			CreatedAt:            f.now,
			UpdatedAt:            f.now,
		},
		Allocations:     allocations,
		ExpectedRecords: append([]domain.UsageRecord(nil), records...),
	}
}

func TestPrepareChargeBatchRollsBackCommandAllocationsAndClaims(
	t *testing.T,
) {
	fixture := newTransactionBoundaryFixture(t, 2)
	batchID := "tx-boundary-prepare-" + fixture.suffix
	duplicateAllocationID :=
		"tx-boundary-duplicate-allocation-" + fixture.suffix
	plan := fixture.plan(
		batchID,
		fixture.records,
		[]string{
			duplicateAllocationID,
			duplicateAllocationID,
		},
		[]int64{100, 100},
	)

	if _, err := fixture.ledger.PrepareChargeBatch(
		t.Context(),
		plan,
	); err == nil {
		t.Fatal("expected duplicate allocation failure")
	}

	if _, err := loadBillingChargeSnapshot(
		t.Context(),
		fixture.db,
		batchID,
		false,
	); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("batch after rollback error=%v", err)
	}

	for _, expected := range fixture.records {
		current, err := fixture.ledger.FindByLocalRequestID(
			t.Context(),
			expected.LocalRequestID,
		)
		if err != nil {
			t.Fatalf("FindByLocalRequestID: %v", err)
		}
		if current.BillingChargeRequestID != "" ||
			current.Status != domain.UsageStatusBillable ||
			current.ChargedAmountCents != 0 ||
			current.RemainingAmountCents != 100 {
			t.Fatalf("usage after prepare rollback=%+v", current)
		}
	}
}

func TestApplyChargeSuccessRollsBackAllUsageAndBatchMutations(
	t *testing.T,
) {
	fixture := newTransactionBoundaryFixture(t, 2)
	batchID := "tx-boundary-success-" + fixture.suffix
	plan := fixture.plan(
		batchID,
		fixture.records,
		[]string{
			"tx-boundary-success-allocation-0-" + fixture.suffix,
			"tx-boundary-success-allocation-1-" + fixture.suffix,
		},
		[]int64{100, 100},
	)
	prepared, err := fixture.ledger.PrepareChargeBatch(
		t.Context(),
		plan,
	)
	if err != nil {
		t.Fatalf("PrepareChargeBatch: %v", err)
	}

	if _, err := fixture.db.Exec(
		t.Context(),
		`
UPDATE tokenio_usage_records
SET failure_reason = 'concurrent_change'
WHERE local_request_id = $1
`,
		fixture.records[1].LocalRequestID,
	); err != nil {
		t.Fatalf("mutate second usage: %v", err)
	}

	if err := fixture.ledger.ApplyChargeSuccess(
		t.Context(),
		ports.UsageChargeSuccess{
			BatchID:         batchID,
			ChargedAt:       fixture.now.Add(time.Second),
			Allocations:     prepared.Allocations,
			ExpectedRecords: prepared.ExpectedRecords,
		},
	); !errors.Is(err, ports.ErrStoreConflict) {
		t.Fatalf("ApplyChargeSuccess error=%v", err)
	}

	first, err := fixture.ledger.FindByLocalRequestID(
		t.Context(),
		fixture.records[0].LocalRequestID,
	)
	if err != nil {
		t.Fatalf("load first usage: %v", err)
	}
	if first.Status != domain.UsageStatusBillable ||
		first.ChargedAmountCents != 0 ||
		first.RemainingAmountCents != 100 ||
		first.BillingChargeRequestID != batchID {
		t.Fatalf("first usage after rollback=%+v", first)
	}

	snapshot, err := loadBillingChargeSnapshot(
		t.Context(),
		fixture.db,
		batchID,
		false,
	)
	if err != nil {
		t.Fatalf("load batch: %v", err)
	}
	if snapshot.Batch.Status != domain.BillingChargeStatusPending ||
		snapshot.Batch.ChargedAt != nil {
		t.Fatalf("batch after success rollback=%+v", snapshot.Batch)
	}
}

func TestMarkChargeBatchFailedCASAllowsOneConcurrentTransition(
	t *testing.T,
) {
	fixture := newTransactionBoundaryFixture(t, 1)
	batchID := "tx-boundary-failure-" + fixture.suffix
	plan := fixture.plan(
		batchID,
		fixture.records,
		[]string{
			"tx-boundary-failure-allocation-" + fixture.suffix,
		},
		[]int64{100},
	)
	if _, err := fixture.ledger.PrepareChargeBatch(
		t.Context(),
		plan,
	); err != nil {
		t.Fatalf("PrepareChargeBatch: %v", err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			results <- fixture.ledger.MarkChargeBatchFailed(
				t.Context(),
				batchID,
				domain.BillingChargeStatusPending,
				"billing_unavailable",
				fixture.now.Add(time.Second),
			)
		}()
	}
	ready.Wait()
	close(start)

	successes := 0
	conflicts := 0
	for range 2 {
		callErr := <-results
		switch {
		case callErr == nil:
			successes++
		case errors.Is(callErr, ports.ErrStoreConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent failure result: %v", callErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf(
			"successes=%d conflicts=%d, want 1/1",
			successes,
			conflicts,
		)
	}

	snapshot, err := loadBillingChargeSnapshot(
		t.Context(),
		fixture.db,
		batchID,
		false,
	)
	if err != nil {
		t.Fatalf("load batch: %v", err)
	}
	if snapshot.Batch.Status != domain.BillingChargeStatusFailed {
		t.Fatalf("batch=%+v", snapshot.Batch)
	}
}
