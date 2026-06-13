package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type stage11ASequenceBalanceClient struct {
	responses []ports.BillingBalance
	err       error
	calls     int
}

func (c *stage11ASequenceBalanceClient) GetBalance(
	_ context.Context,
	_ string,
) (ports.BillingBalance, error) {
	c.calls++
	if c.err != nil {
		return ports.BillingBalance{}, c.err
	}
	if len(c.responses) == 0 {
		return ports.BillingBalance{}, errors.New("unexpected balance call")
	}
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

func TestSucceededReplayWithoutPersistedBalanceRefreshesRemoteBalance(t *testing.T) {
	balance := &stage11ASequenceBalanceClient{
		responses: []ports.BillingBalance{{
			Currency:     currencyRUB,
			BalanceCents: 70,
		}},
	}
	service := &AutoChargeService{balance: balance}

	got, err := service.resolveRemainingRemoteBalance(
		t.Context(),
		"billing-jwt",
		100,
		ports.BillingChargeBatchSnapshot{
			Batch: domain.BillingChargeBatch{
				Status:      domain.BillingChargeStatusSucceeded,
				AmountCents: 30,
			},
		},
		AutoChargeResult{},
	)
	if err != nil {
		t.Fatalf("resolve remaining remote balance: %v", err)
	}
	if got != 70 {
		t.Fatalf("remaining remote balance = %d, want 70", got)
	}
	if balance.calls != 1 {
		t.Fatalf("balance calls = %d, want 1", balance.calls)
	}
}

func TestSucceededReplayWithPersistedBalanceDoesNotRefresh(t *testing.T) {
	balance := &stage11ASequenceBalanceClient{}
	service := &AutoChargeService{balance: balance}
	persisted := int64(55)

	got, err := service.resolveRemainingRemoteBalance(
		t.Context(),
		"billing-jwt",
		100,
		ports.BillingChargeBatchSnapshot{
			Batch: domain.BillingChargeBatch{
				Status:      domain.BillingChargeStatusSucceeded,
				AmountCents: 30,
			},
		},
		AutoChargeResult{BillingBalanceCents: &persisted},
	)
	if err != nil {
		t.Fatalf("resolve remaining remote balance: %v", err)
	}
	if got != persisted {
		t.Fatalf("remaining remote balance = %d, want %d", got, persisted)
	}
	if balance.calls != 0 {
		t.Fatalf("balance calls = %d, want 0", balance.calls)
	}
}

func TestCurrentChargeWithoutReturnedBalanceSubtractsExactlyOnce(t *testing.T) {
	balance := &stage11ASequenceBalanceClient{}
	service := &AutoChargeService{balance: balance}

	got, err := service.resolveRemainingRemoteBalance(
		t.Context(),
		"billing-jwt",
		100,
		ports.BillingChargeBatchSnapshot{
			Batch: domain.BillingChargeBatch{
				Status:      domain.BillingChargeStatusPending,
				AmountCents: 30,
			},
		},
		AutoChargeResult{},
	)
	if err != nil {
		t.Fatalf("resolve remaining remote balance: %v", err)
	}
	if got != 70 {
		t.Fatalf("remaining remote balance = %d, want 70", got)
	}
	if balance.calls != 0 {
		t.Fatalf("balance calls = %d, want 0", balance.calls)
	}
}

func TestSucceededReplayBalanceRefreshFailureStopsProcessing(t *testing.T) {
	balance := &stage11ASequenceBalanceClient{err: errors.New("billing down")}
	service := &AutoChargeService{balance: balance}

	_, err := service.resolveRemainingRemoteBalance(
		t.Context(),
		"billing-jwt",
		100,
		ports.BillingChargeBatchSnapshot{
			Batch: domain.BillingChargeBatch{
				Status:      domain.BillingChargeStatusSucceeded,
				AmountCents: 30,
			},
		},
		AutoChargeResult{},
	)
	if !errors.Is(err, ErrBillingUnavailable) {
		t.Fatalf("error = %v, want ErrBillingUnavailable", err)
	}
	if balance.calls != 1 {
		t.Fatalf("balance calls = %d, want 1", balance.calls)
	}
}

type stage11AFinalReplayLedger struct {
	candidate       domain.UsageRecord
	snapshot        ports.BillingChargeBatchSnapshot
	prepareCalls    int
	markFailedCalls int
	applyCalls      int
}

func (l *stage11AFinalReplayLedger) CreateReserved(
	_ context.Context,
	_ domain.UsageRecord,
) (ports.UsageReserveResult, error) {
	return ports.UsageReserveResult{}, nil
}

func (l *stage11AFinalReplayLedger) FindByLocalRequestID(
	_ context.Context,
	_ string,
) (*domain.UsageRecord, error) {
	return nil, ports.ErrNotFound
}

func (l *stage11AFinalReplayLedger) CompareAndSwap(
	_ context.Context,
	_ string,
	_ domain.UsageStatus,
	_ domain.UsageRecord,
) (ports.UsageTransitionResult, error) {
	return ports.UsageTransitionResult{}, nil
}

func (l *stage11AFinalReplayLedger) LoadExposure(
	_ context.Context,
	_ string,
	_ string,
) (ports.UsageExposureSnapshot, error) {
	return ports.UsageExposureSnapshot{}, nil
}

func (l *stage11AFinalReplayLedger) LoadOpenChargeBatches(
	_ context.Context,
	_ string,
	_ string,
	_ string,
) ([]ports.BillingChargeBatchSnapshot, error) {
	return nil, nil
}

func (l *stage11AFinalReplayLedger) LoadChargeCandidates(
	_ context.Context,
	_ string,
	_ string,
) ([]domain.UsageRecord, error) {
	return []domain.UsageRecord{l.candidate}, nil
}

func (l *stage11AFinalReplayLedger) PrepareChargeBatch(
	_ context.Context,
	plan ports.UsageChargeBatchPlan,
) (ports.BillingChargeBatchSnapshot, error) {
	l.prepareCalls++
	if plan.Batch.ID != l.snapshot.Batch.ID {
		return ports.BillingChargeBatchSnapshot{}, errors.New("unexpected replay batch id")
	}
	return l.snapshot, nil
}

func (l *stage11AFinalReplayLedger) MarkChargeBatchFailed(
	_ context.Context,
	_ string,
	_ domain.BillingChargeStatus,
	_ string,
	_ time.Time,
) error {
	l.markFailedCalls++
	return nil
}

func (l *stage11AFinalReplayLedger) ApplyChargeSuccess(
	_ context.Context,
	_ ports.UsageChargeSuccess,
) error {
	l.applyCalls++
	return nil
}

func TestRunDoesNotRefreshBalanceAfterFinalSucceededReplayGroup(t *testing.T) {
	now := time.Unix(1_700, 0).UTC()
	record := rec("final-succeeded-replay", domain.ProviderOpenAI, "m", 100, 0, 100, 1)
	plan, err := BuildChargePlan("billing", []domain.UsageRecord{record}, 100, now)
	if err != nil {
		t.Fatal(err)
	}

	snapshot := ports.BillingChargeBatchSnapshot{
		Batch:           plan.Batch,
		Allocations:     append([]domain.BillingChargeAllocation(nil), plan.Allocations...),
		ExpectedRecords: claimedExpected(plan),
	}
	snapshot.Batch.Status = domain.BillingChargeStatusSucceeded
	snapshot.Batch.ChargedAt = &now
	snapshot.Batch.UpdatedAt = now

	ledger := &stage11AFinalReplayLedger{candidate: record, snapshot: snapshot}
	balance := &stage11ASequenceBalanceClient{
		responses: []ports.BillingBalance{{Currency: currencyRUB, BalanceCents: 100}},
	}
	charge := &fakeCharge{}
	service, err := NewAutoChargeService(
		&fakeIdentity{token: "billing-jwt"},
		balance,
		charge,
		ledger,
		testClock{t: now},
		AutoChargeConfig{ThresholdCents: 1, MinimumChargeCents: 1},
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.Run(t.Context(), AutoChargeInput{
		UserID:               "u",
		BillingSubjectUserID: "billing",
		Currency:             currencyRUB,
	})
	if err != nil {
		t.Fatalf("Run returned error after final succeeded replay: %v", err)
	}
	if balance.calls != 1 {
		t.Fatalf("balance calls = %d, want initial call only", balance.calls)
	}
	if len(charge.requests) != 0 {
		t.Fatalf("billing charge called for succeeded replay: %+v", charge.requests)
	}
	if ledger.prepareCalls != 1 || ledger.markFailedCalls != 0 || ledger.applyCalls != 0 {
		t.Fatalf(
			"ledger calls prepare=%d mark_failed=%d apply=%d",
			ledger.prepareCalls,
			ledger.markFailedCalls,
			ledger.applyCalls,
		)
	}
	if len(result.ProcessedBatchIDs) != 1 || result.ProcessedBatchIDs[0] != snapshot.Batch.ID {
		t.Fatalf("processed batches = %+v", result.ProcessedBatchIDs)
	}
}
