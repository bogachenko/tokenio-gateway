package billing

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type idempotentChargeClient struct {
	mu       sync.Mutex
	requests []ports.BillingChargeRequest
	effects  map[string]int
	balance  int64
}

func (c *idempotentChargeClient) Charge(
	_ context.Context,
	request ports.BillingChargeRequest,
) (ports.BillingChargeResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.requests = append(c.requests, request)
	if c.effects == nil {
		c.effects = make(map[string]int)
	}
	if c.effects[request.RequestID] == 0 {
		c.effects[request.RequestID] = 1
	}
	balance := c.balance
	return ports.BillingChargeResult{
		BalanceCents: &balance,
	}, nil
}

func TestConcurrentWorkersReuseImmutableRequestIDAndApplySuccessOnce(
	t *testing.T,
) {
	now := time.Unix(500, 0).UTC()
	record := rec(
		"concurrent-boundary",
		domain.ProviderOpenAI,
		"model",
		100,
		0,
		100,
		1,
	)
	ledger := newFakeLedger([]domain.UsageRecord{record})
	plan, err := BuildChargePlan(
		"billing-user",
		[]domain.UsageRecord{record},
		100,
		now,
	)
	if err != nil {
		t.Fatalf("BuildChargePlan: %v", err)
	}
	prepared, err := ledger.PrepareChargeBatch(
		t.Context(),
		plan,
	)
	if err != nil {
		t.Fatalf("PrepareChargeBatch: %v", err)
	}

	charge := &idempotentChargeClient{balance: 900}
	service, err := NewAutoChargeService(
		&fakeIdentity{token: "jwt"},
		&fakeBalance{
			balance: ports.BillingBalance{
				Currency:     "RUB",
				BalanceCents: 1000,
			},
		},
		charge,
		ledger,
		testClock{t: now.Add(time.Second)},
		AutoChargeConfig{
			ThresholdCents:     1,
			MinimumChargeCents: 1,
		},
	)
	if err != nil {
		t.Fatalf("NewAutoChargeService: %v", err)
	}

	start := make(chan struct{})
	errorsByWorker := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			_, callErr := service.processPreparedBatch(
				t.Context(),
				AutoChargeInput{
					UserID:               "u",
					BillingSubjectUserID: "billing-user",
					Currency:             "RUB",
				},
				prepared,
			)
			errorsByWorker <- callErr
		}()
	}
	ready.Wait()
	close(start)

	for range 2 {
		if callErr := <-errorsByWorker; callErr != nil {
			t.Fatalf("concurrent worker error: %v", callErr)
		}
	}

	charge.mu.Lock()
	requests := append(
		[]ports.BillingChargeRequest(nil),
		charge.requests...,
	)
	effects := charge.effects[prepared.Batch.ID]
	charge.mu.Unlock()

	if len(requests) != 2 {
		t.Fatalf("external requests=%d, want 2 replays", len(requests))
	}
	for _, request := range requests {
		if request.RequestID != prepared.Batch.ID {
			t.Fatalf(
				"request ID=%q, want immutable batch ID %q",
				request.RequestID,
				prepared.Batch.ID,
			)
		}
	}
	if effects != 1 {
		t.Fatalf("financial effects=%d, want 1", effects)
	}

	ledger.mu.Lock()
	persisted := ledger.records[record.LocalRequestID]
	snapshot := ledger.batches[prepared.Batch.ID]
	applyCalls := ledger.applyCalls
	ledger.mu.Unlock()

	if applyCalls != 2 {
		t.Fatalf("ApplyChargeSuccess calls=%d, want 2 replays", applyCalls)
	}
	if persisted.Status != domain.UsageStatusCharged ||
		persisted.ChargedAmountCents != 100 ||
		persisted.RemainingAmountCents != 0 {
		t.Fatalf("persisted usage=%+v", persisted)
	}
	if snapshot.Batch.Status !=
		domain.BillingChargeStatusSucceeded {
		t.Fatalf("persisted batch=%+v", snapshot.Batch)
	}
}

func TestRecoveryAfterExternalSuccessBeforeLocalReconciliation(
	t *testing.T,
) {
	now := time.Unix(700, 0).UTC()
	record := rec(
		"external-success-crash-window",
		domain.ProviderOpenAI,
		"model",
		100,
		0,
		100,
		1,
	)
	ledger := newFakeLedger([]domain.UsageRecord{record})
	plan, err := BuildChargePlan(
		"billing-user",
		[]domain.UsageRecord{record},
		100,
		now,
	)
	if err != nil {
		t.Fatalf("BuildChargePlan: %v", err)
	}
	prepared, err := ledger.PrepareChargeBatch(
		t.Context(),
		plan,
	)
	if err != nil {
		t.Fatalf("PrepareChargeBatch: %v", err)
	}

	charge := &idempotentChargeClient{balance: 900}
	service, err := NewAutoChargeService(
		&fakeIdentity{token: "jwt"},
		&fakeBalance{
			balance: ports.BillingBalance{
				Currency:     "RUB",
				BalanceCents: 1000,
			},
		},
		charge,
		ledger,
		testClock{t: now.Add(time.Second)},
		AutoChargeConfig{
			ThresholdCents:     1,
			MinimumChargeCents: 1,
		},
	)
	if err != nil {
		t.Fatalf("NewAutoChargeService: %v", err)
	}

	ledger.mu.Lock()
	ledger.applyErr = errors.New(
		"simulated crash before local success reconciliation",
	)
	ledger.mu.Unlock()

	if _, err := service.processPreparedBatch(
		t.Context(),
		AutoChargeInput{
			UserID:               "u",
			BillingSubjectUserID: "billing-user",
			Currency:             "RUB",
		},
		prepared,
	); err == nil {
		t.Fatal("expected local reconciliation failure")
	}

	charge.mu.Lock()
	firstRequests := append(
		[]ports.BillingChargeRequest(nil),
		charge.requests...,
	)
	firstEffects := charge.effects[prepared.Batch.ID]
	charge.mu.Unlock()
	if len(firstRequests) != 1 ||
		firstRequests[0].RequestID != prepared.Batch.ID ||
		firstEffects != 1 {
		t.Fatalf(
			"first external result requests=%+v effects=%d",
			firstRequests,
			firstEffects,
		)
	}

	ledger.mu.Lock()
	afterCrash := ledger.records[record.LocalRequestID]
	afterCrashBatch := ledger.batches[prepared.Batch.ID]
	ledger.applyErr = nil
	ledger.mu.Unlock()

	if afterCrash.Status != domain.UsageStatusBillable ||
		afterCrash.ChargedAmountCents != 0 ||
		afterCrash.RemainingAmountCents != 100 {
		t.Fatalf("usage changed before reconciliation: %+v", afterCrash)
	}
	if afterCrashBatch.Batch.Status !=
		domain.BillingChargeStatusPending {
		t.Fatalf("batch changed before reconciliation: %+v", afterCrashBatch.Batch)
	}

	result, err := service.processPreparedBatch(
		t.Context(),
		AutoChargeInput{
			UserID:               "u",
			BillingSubjectUserID: "billing-user",
			Currency:             "RUB",
		},
		prepared,
	)
	if err != nil {
		t.Fatalf("recovery processPreparedBatch: %v", err)
	}
	if result.ProcessedBatchID != prepared.Batch.ID ||
		result.ChargedAmountCents != prepared.Batch.AmountCents {
		t.Fatalf("recovery result=%+v", result)
	}

	charge.mu.Lock()
	allRequests := append(
		[]ports.BillingChargeRequest(nil),
		charge.requests...,
	)
	effects := charge.effects[prepared.Batch.ID]
	charge.mu.Unlock()

	if len(allRequests) != 2 {
		t.Fatalf("external request count=%d, want 2", len(allRequests))
	}
	for _, request := range allRequests {
		if request.RequestID != prepared.Batch.ID {
			t.Fatalf(
				"request ID=%q, want immutable batch ID %q",
				request.RequestID,
				prepared.Batch.ID,
			)
		}
	}
	if effects != 1 {
		t.Fatalf("financial effects=%d, want 1", effects)
	}

	ledger.mu.Lock()
	persisted := ledger.records[record.LocalRequestID]
	persistedBatch := ledger.batches[prepared.Batch.ID]
	applyCalls := ledger.applyCalls
	ledger.mu.Unlock()

	if applyCalls != 2 {
		t.Fatalf("ApplyChargeSuccess calls=%d, want 2", applyCalls)
	}
	if persisted.Status != domain.UsageStatusCharged ||
		persisted.ChargedAmountCents != 100 ||
		persisted.RemainingAmountCents != 0 {
		t.Fatalf("persisted usage=%+v", persisted)
	}
	if persistedBatch.Batch.Status !=
		domain.BillingChargeStatusSucceeded {
		t.Fatalf("persisted batch=%+v", persistedBatch.Batch)
	}
}
