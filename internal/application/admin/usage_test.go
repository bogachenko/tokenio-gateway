package admin

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type stage10MajorResolutionLedgerFake struct {
	mu sync.Mutex

	current      domain.UsageRecord
	resolveCalls int
	expected     domain.UsageRecord
	next         domain.UsageRecord
	audit        domain.AuditContext
}

func (f *stage10MajorResolutionLedgerFake) CreateReserved(context.Context, domain.UsageRecord) (ports.UsageReserveResult, error) {
	return ports.UsageReserveResult{}, nil
}
func (f *stage10MajorResolutionLedgerFake) FindByLocalRequestID(context.Context, string) (*domain.UsageRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	copy := f.current
	return &copy, nil
}
func (f *stage10MajorResolutionLedgerFake) CompareAndSwap(context.Context, string, domain.UsageStatus, domain.UsageRecord) (ports.UsageTransitionResult, error) {
	return ports.UsageTransitionResult{}, nil
}
func (f *stage10MajorResolutionLedgerFake) LoadExposure(context.Context, string, string) (ports.UsageExposureSnapshot, error) {
	return ports.UsageExposureSnapshot{}, nil
}
func (f *stage10MajorResolutionLedgerFake) LoadOpenChargeBatches(context.Context, string, string, string) ([]ports.BillingChargeBatchSnapshot, error) {
	return nil, nil
}
func (f *stage10MajorResolutionLedgerFake) LoadChargeCandidates(context.Context, string, string) ([]domain.UsageRecord, error) {
	return nil, nil
}
func (f *stage10MajorResolutionLedgerFake) PrepareChargeBatch(context.Context, ports.UsageChargeBatchPlan) (ports.BillingChargeBatchSnapshot, error) {
	return ports.BillingChargeBatchSnapshot{}, nil
}
func (f *stage10MajorResolutionLedgerFake) MarkChargeBatchFailed(context.Context, string, domain.BillingChargeStatus, string, time.Time) error {
	return nil
}
func (f *stage10MajorResolutionLedgerFake) ApplyChargeSuccess(context.Context, ports.UsageChargeSuccess) error {
	return nil
}
func (f *stage10MajorResolutionLedgerFake) ListUsageRecords(context.Context, ports.UsageListFilter) (ports.Page[domain.UsageRecord], error) {
	return ports.Page[domain.UsageRecord]{}, nil
}
func (f *stage10MajorResolutionLedgerFake) ResolvePricingFailedWithAudit(_ context.Context, expected, next domain.UsageRecord, audit domain.AuditContext) (ports.UsageTransitionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolveCalls++
	f.expected = expected
	f.next = next
	f.audit = audit
	copy := next
	return ports.UsageTransitionResult{Applied: true, Current: &copy}, nil
}
func (f *stage10MajorResolutionLedgerFake) ListBillingChargeBatches(context.Context, ports.BillingChargeBatchListFilter) (ports.Page[domain.BillingChargeBatch], error) {
	return ports.Page[domain.BillingChargeBatch]{}, nil
}
func (f *stage10MajorResolutionLedgerFake) LoadChargeBatchByID(context.Context, string) (ports.BillingChargeBatchSnapshot, error) {
	return ports.BillingChargeBatchSnapshot{}, ports.ErrNotFound
}
func (f *stage10MajorResolutionLedgerFake) RecordChargeRetryAttemptWithAudit(context.Context, ports.BillingChargeBatchSnapshot, domain.AuditContext) error {
	return nil
}
func (f *stage10MajorResolutionLedgerFake) ApplyChargeRetrySuccessWithAudit(context.Context, ports.UsageChargeSuccess, domain.AuditContext) error {
	return nil
}
func (f *stage10MajorResolutionLedgerFake) MarkChargeRetryFailedWithAudit(context.Context, string, domain.BillingChargeStatus, string, time.Time, domain.AuditContext) error {
	return nil
}

func stage10MajorPricingFailedRecord() domain.UsageRecord {
	at := time.Unix(1, 0).UTC()
	return domain.UsageRecord{
		LocalRequestID:    "llmreq_failed",
		UserID:            "usr_1",
		ProviderType:      domain.ProviderOpenAI,
		ClientModel:       "model-a",
		BillingModel:      "openai:model-a",
		Currency:          "RUB",
		Status:            domain.UsageStatusPricingFailed,
		UsageCompleteness: "failed",
		FailureReason:     "usage_extraction_failed",
		CreatedAt:         at,
		UpdatedAt:         at,
	}
}

func TestResolvePricingFailedUsesAtomicExpectedStateCASAndAudit(t *testing.T) {
	ledger := &stage10MajorResolutionLedgerFake{current: stage10MajorPricingFailedRecord()}
	service := &Service{deps: Dependencies{
		UsagePolicy: &fakeUsagePolicy{},
		Ledger:      ledger,
		Clock:       &stage10MajorAdminClock{now: time.Unix(10, 0).UTC()},
	}}
	command := CommandContext{RequestID: "admreq_resolve", AdminSubject: "admin_token"}

	_, err := service.ResolveUsageBillable(context.Background(), command, ResolveBillableInput{
		LocalRequestID:          "llmreq_failed",
		InputTokens:             100,
		OutputTokens:            50,
		ClientAmountCents:       12,
		ActualUpstreamCostCents: 8,
		Reason:                  "",
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing reason error = %v", err)
	}
	ledger.mu.Lock()
	if ledger.resolveCalls != 0 {
		t.Fatal("resolution persisted without reason")
	}
	ledger.mu.Unlock()

	result, err := service.ResolveUsageBillable(context.Background(), command, ResolveBillableInput{
		LocalRequestID:          "llmreq_failed",
		InputTokens:             100,
		OutputTokens:            50,
		ClientAmountCents:       12,
		ActualUpstreamCostCents: 8,
		Reason:                  "manual reconstruction",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.UsageStatusBillable || result.RemainingAmountCents != 12 {
		t.Fatalf("result = %+v", result)
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if ledger.resolveCalls != 1 || ledger.expected.Status != domain.UsageStatusPricingFailed || ledger.next.Status != domain.UsageStatusBillable {
		t.Fatalf("expected/next = %s/%s calls=%d", ledger.expected.Status, ledger.next.Status, ledger.resolveCalls)
	}
	if ledger.audit.Action != domain.AuditActionUsageResolveBillable || ledger.audit.RequestID != command.RequestID {
		t.Fatalf("audit = %+v", ledger.audit)
	}
}
