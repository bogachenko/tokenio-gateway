package billing

import (
	"context"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type stage10V5OpenBatchLedger struct {
	ports.UsageLedger

	mu      sync.Mutex
	open    []ports.BillingChargeBatchSnapshot
	applied []string
}

func (f *stage10V5OpenBatchLedger) LoadOpenChargeBatches(context.Context, string, string, string) ([]ports.BillingChargeBatchSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ports.BillingChargeBatchSnapshot(nil), f.open...), nil
}

func (f *stage10V5OpenBatchLedger) LoadChargeCandidates(context.Context, string, string) ([]domain.UsageRecord, error) {
	return nil, nil
}

func (f *stage10V5OpenBatchLedger) ApplyChargeSuccess(_ context.Context, success ports.UsageChargeSuccess) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, success.BatchID)
	return nil
}

type stage10V5OpenBatchCharge struct {
	mu       sync.Mutex
	requests []ports.BillingChargeRequest
}

func (f *stage10V5OpenBatchCharge) Charge(_ context.Context, request ports.BillingChargeRequest) (ports.BillingChargeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, request)
	return ports.BillingChargeResult{}, nil
}

type stage10V5UnusedIdentity struct{}

func (stage10V5UnusedIdentity) TokenForSubject(context.Context, string) (string, error) {
	return "", ErrBillingIdentityUnavailable
}

type stage10V5UnusedBalance struct{}

func (stage10V5UnusedBalance) GetBalance(context.Context, string) (ports.BillingBalance, error) {
	return ports.BillingBalance{}, ErrBillingUnavailable
}

type stage10V5AutoChargeClock struct{ now time.Time }

func (c stage10V5AutoChargeClock) Now() time.Time { return c.now }

func stage10V5PendingSnapshot(t *testing.T, model string, createdAt time.Time, amount int64) ports.BillingChargeBatchSnapshot {
	t.Helper()
	billableAt := createdAt
	record := domain.UsageRecord{
		LocalRequestID:       "llmreq_" + model,
		UserID:               "usr_1",
		APIKeyID:             "ak_1",
		APIFamily:            domain.APIFamilyOpenAICompatible,
		EndpointKind:         domain.EndpointChat,
		ClientModel:          model,
		BillingModel:         "openai:" + model,
		SelectedRouteID:      "route_" + model,
		SelectedResellerID:   "reseller_1",
		ProviderType:         domain.ProviderOpenAI,
		ProviderModel:        model,
		Usage:                domain.TokenUsage{InputTokens: 100, OutputTokens: 50},
		UsageCompleteness:    "detailed",
		ClientAmountCents:    amount,
		RemainingAmountCents: amount,
		Currency:             "RUB",
		Status:               domain.UsageStatusBillable,
		CreatedAt:            createdAt,
		BillableAt:           &billableAt,
		UpdatedAt:            createdAt,
	}
	plan, err := BuildChargePlan("billing_usr_1", []domain.UsageRecord{record}, amount, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	for i := range plan.ExpectedRecords {
		plan.ExpectedRecords[i].BillingChargeRequestID = plan.Batch.ID
	}
	return ports.BillingChargeBatchSnapshot{
		Batch:           plan.Batch,
		Allocations:     append([]domain.BillingChargeAllocation(nil), plan.Allocations...),
		ExpectedRecords: append([]domain.UsageRecord(nil), plan.ExpectedRecords...),
	}
}

func TestStage10V5OpenBatchesAreProcessedByCreatedAtThenBatchID(t *testing.T) {
	firstTime := time.Unix(10, 0).UTC()
	secondTime := time.Unix(20, 0).UTC()
	snapshots := []ports.BillingChargeBatchSnapshot{
		stage10V5PendingSnapshot(t, "model-c", secondTime, 30),
		stage10V5PendingSnapshot(t, "model-b", firstTime, 20),
		stage10V5PendingSnapshot(t, "model-a", firstTime, 10),
	}
	expected := append([]ports.BillingChargeBatchSnapshot(nil), snapshots...)
	sort.Slice(expected, func(i, j int) bool {
		if !expected[i].Batch.CreatedAt.Equal(expected[j].Batch.CreatedAt) {
			return expected[i].Batch.CreatedAt.Before(expected[j].Batch.CreatedAt)
		}
		return expected[i].Batch.ID < expected[j].Batch.ID
	})

	ledger := &stage10V5OpenBatchLedger{open: snapshots}
	charge := &stage10V5OpenBatchCharge{}
	service, err := NewAutoChargeService(
		stage10V5UnusedIdentity{},
		stage10V5UnusedBalance{},
		charge,
		ledger,
		stage10V5AutoChargeClock{now: time.Unix(30, 0).UTC()},
		AutoChargeConfig{ThresholdCents: 1, MinimumChargeCents: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Run(context.Background(), AutoChargeInput{
		UserID:               "usr_1",
		BillingSubjectUserID: "billing_usr_1",
		Currency:             "RUB",
	})
	if err != nil {
		t.Fatal(err)
	}

	wantIDs := make([]string, len(expected))
	for i := range expected {
		wantIDs[i] = expected[i].Batch.ID
	}
	charge.mu.Lock()
	gotRequests := append([]ports.BillingChargeRequest(nil), charge.requests...)
	charge.mu.Unlock()
	gotIDs := make([]string, len(gotRequests))
	for i := range gotRequests {
		gotIDs[i] = gotRequests[i].RequestID
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("charge order = %v, want %v", gotIDs, wantIDs)
	}
	if !reflect.DeepEqual(result.ProcessedBatchIDs, wantIDs) {
		t.Fatalf("result order = %v, want %v", result.ProcessedBatchIDs, wantIDs)
	}
}
