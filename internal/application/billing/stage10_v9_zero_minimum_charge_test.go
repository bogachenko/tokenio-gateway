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

type stage10V9ZeroMinimumIdentity struct{}

func (stage10V9ZeroMinimumIdentity) TokenForSubject(context.Context, string) (string, error) {
	return "billing-token", nil
}

type stage10V9ZeroMinimumBalance struct{}

func (stage10V9ZeroMinimumBalance) GetBalance(context.Context, string) (ports.BillingBalance, error) {
	return ports.BillingBalance{BalanceCents: 0, Currency: "RUB"}, nil
}

type stage10V9ZeroMinimumCharge struct {
	mu    sync.Mutex
	calls int
}

func (f *stage10V9ZeroMinimumCharge) Charge(context.Context, ports.BillingChargeRequest) (ports.BillingChargeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return ports.BillingChargeResult{}, errors.New("zero charge amount must not reach Billing")
}

type stage10V9ZeroMinimumLedger struct {
	ports.UsageLedger

	mu           sync.Mutex
	candidates   []domain.UsageRecord
	prepareCalls int
}

func (f *stage10V9ZeroMinimumLedger) LoadOpenChargeBatches(context.Context, string, string, string) ([]ports.BillingChargeBatchSnapshot, error) {
	return nil, nil
}

func (f *stage10V9ZeroMinimumLedger) LoadChargeCandidates(context.Context, string, string) ([]domain.UsageRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.UsageRecord(nil), f.candidates...), nil
}

func (f *stage10V9ZeroMinimumLedger) PrepareChargeBatch(context.Context, ports.UsageChargeBatchPlan) (ports.BillingChargeBatchSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepareCalls++
	return ports.BillingChargeBatchSnapshot{}, errors.New("zero charge amount must be deferred before batch preparation")
}

type stage10V9ZeroMinimumClock struct{ now time.Time }

func (c stage10V9ZeroMinimumClock) Now() time.Time { return c.now }

func stage10V9BillableCandidate(at time.Time) domain.UsageRecord {
	billableAt := at
	return domain.UsageRecord{
		LocalRequestID:       "llmreq_stage10_v9_zero_minimum",
		UserID:               "usr_stage10_v9",
		APIKeyID:             "ak_stage10_v9",
		APIFamily:            domain.APIFamilyOpenAICompatible,
		EndpointKind:         domain.EndpointChat,
		ClientModel:          "model-stage10-v9",
		BillingModel:         "openai:model-stage10-v9",
		SelectedRouteID:      "route_stage10_v9",
		SelectedResellerID:   "reseller_stage10_v9",
		ProviderType:         domain.ProviderOpenAI,
		ProviderModel:        "model-stage10-v9",
		Usage:                domain.TokenUsage{InputTokens: 10, OutputTokens: 5},
		UsageCompleteness:    "detailed",
		ClientAmountCents:    100,
		ChargedAmountCents:   0,
		RemainingAmountCents: 100,
		Currency:             "RUB",
		Status:               domain.UsageStatusBillable,
		CreatedAt:            at,
		BillableAt:           &billableAt,
		UpdatedAt:            at,
	}
}

func TestStage10V9ZeroMinimumChargeIsAcceptedAndZeroAmountIsDeferred(t *testing.T) {
	at := time.Unix(900, 0).UTC()
	ledger := &stage10V9ZeroMinimumLedger{candidates: []domain.UsageRecord{stage10V9BillableCandidate(at)}}
	charge := &stage10V9ZeroMinimumCharge{}

	service, err := NewAutoChargeService(
		stage10V9ZeroMinimumIdentity{},
		stage10V9ZeroMinimumBalance{},
		charge,
		ledger,
		stage10V9ZeroMinimumClock{now: at},
		AutoChargeConfig{ThresholdCents: 1, MinimumChargeCents: 0},
	)
	if err != nil {
		t.Fatalf("zero minimum charge must be accepted: %v", err)
	}

	result, err := service.Run(context.Background(), AutoChargeInput{
		UserID:               "usr_stage10_v9",
		BillingSubjectUserID: "billing_usr_stage10_v9",
		Currency:             "RUB",
	})
	if !errors.Is(err, ErrChargeDeferred) {
		t.Fatalf("Run error = %v, want ErrChargeDeferred", err)
	}
	if !result.Deferred || len(result.ProcessedBatchIDs) != 0 || result.ChargedAmountCents != 0 {
		t.Fatalf("result = %+v, want deferred result without processed batches", result)
	}

	ledger.mu.Lock()
	prepareCalls := ledger.prepareCalls
	ledger.mu.Unlock()
	charge.mu.Lock()
	chargeCalls := charge.calls
	charge.mu.Unlock()
	if prepareCalls != 0 || chargeCalls != 0 {
		t.Fatalf("prepare calls = %d, charge calls = %d; want both zero", prepareCalls, chargeCalls)
	}

	_, err = NewAutoChargeService(
		stage10V9ZeroMinimumIdentity{},
		stage10V9ZeroMinimumBalance{},
		charge,
		ledger,
		stage10V9ZeroMinimumClock{now: at},
		AutoChargeConfig{ThresholdCents: 1, MinimumChargeCents: -1},
	)
	if !errors.Is(err, ErrInvalidBillingInput) {
		t.Fatalf("negative minimum charge error = %v, want ErrInvalidBillingInput", err)
	}
}
