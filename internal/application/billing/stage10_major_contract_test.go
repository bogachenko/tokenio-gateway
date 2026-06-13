package billing

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type stage10MajorRetryLedgerFake struct {
	mu sync.Mutex

	snapshot     ports.BillingChargeBatchSnapshot
	loadErr      error
	attemptErr   error
	applyErr     error
	markErr      error
	attemptCalls int
	applyCalls   int
	markCalls    int
	attemptAudit domain.AuditContext
	applyAudit   domain.AuditContext
	success      ports.UsageChargeSuccess
}

func (f *stage10MajorRetryLedgerFake) attempted() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attemptCalls > 0
}
func (f *stage10MajorRetryLedgerFake) CreateReserved(context.Context, domain.UsageRecord) (ports.UsageReserveResult, error) {
	return ports.UsageReserveResult{}, nil
}
func (f *stage10MajorRetryLedgerFake) FindByLocalRequestID(context.Context, string) (*domain.UsageRecord, error) {
	return nil, ports.ErrNotFound
}
func (f *stage10MajorRetryLedgerFake) CompareAndSwap(context.Context, string, domain.UsageStatus, domain.UsageRecord) (ports.UsageTransitionResult, error) {
	return ports.UsageTransitionResult{}, nil
}
func (f *stage10MajorRetryLedgerFake) LoadExposure(context.Context, string, string) (ports.UsageExposureSnapshot, error) {
	return ports.UsageExposureSnapshot{}, nil
}
func (f *stage10MajorRetryLedgerFake) LoadOpenChargeBatches(context.Context, string, string, string) ([]ports.BillingChargeBatchSnapshot, error) {
	return nil, nil
}
func (f *stage10MajorRetryLedgerFake) LoadChargeCandidates(context.Context, string, string) ([]domain.UsageRecord, error) {
	return nil, nil
}
func (f *stage10MajorRetryLedgerFake) PrepareChargeBatch(context.Context, ports.UsageChargeBatchPlan) (ports.BillingChargeBatchSnapshot, error) {
	return ports.BillingChargeBatchSnapshot{}, nil
}
func (f *stage10MajorRetryLedgerFake) MarkChargeBatchFailed(context.Context, string, domain.BillingChargeStatus, string, time.Time) error {
	return nil
}
func (f *stage10MajorRetryLedgerFake) ApplyChargeSuccess(context.Context, ports.UsageChargeSuccess) error {
	return nil
}
func (f *stage10MajorRetryLedgerFake) ListUsageRecords(context.Context, ports.UsageListFilter) (ports.Page[domain.UsageRecord], error) {
	return ports.Page[domain.UsageRecord]{}, nil
}
func (f *stage10MajorRetryLedgerFake) ResolvePricingFailedWithAudit(context.Context, domain.UsageRecord, domain.UsageRecord, domain.AuditContext) (ports.UsageTransitionResult, error) {
	return ports.UsageTransitionResult{}, nil
}
func (f *stage10MajorRetryLedgerFake) ListBillingChargeBatches(context.Context, ports.BillingChargeBatchListFilter) (ports.Page[domain.BillingChargeBatch], error) {
	return ports.Page[domain.BillingChargeBatch]{}, nil
}
func (f *stage10MajorRetryLedgerFake) LoadChargeBatchByID(context.Context, string) (ports.BillingChargeBatchSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshot, f.loadErr
}
func (f *stage10MajorRetryLedgerFake) RecordChargeRetryAttemptWithAudit(_ context.Context, snapshot ports.BillingChargeBatchSnapshot, audit domain.AuditContext) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attemptCalls++
	if !reflect.DeepEqual(snapshot, f.snapshot) {
		return errors.New("snapshot changed")
	}
	f.attemptAudit = audit
	return f.attemptErr
}
func (f *stage10MajorRetryLedgerFake) ApplyChargeRetrySuccessWithAudit(_ context.Context, success ports.UsageChargeSuccess, audit domain.AuditContext) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applyCalls++
	f.success = success
	f.applyAudit = audit
	return f.applyErr
}
func (f *stage10MajorRetryLedgerFake) MarkChargeRetryFailedWithAudit(context.Context, string, domain.BillingChargeStatus, string, time.Time, domain.AuditContext) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markCalls++
	return f.markErr
}

type stage10MajorRetryChargeFake struct {
	mu      sync.Mutex
	ledger  *stage10MajorRetryLedgerFake
	result  ports.BillingChargeResult
	err     error
	request ports.BillingChargeRequest
	calls   int
}

func (f *stage10MajorRetryChargeFake) Charge(_ context.Context, request ports.BillingChargeRequest) (ports.BillingChargeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.ledger.attempted() {
		return ports.BillingChargeResult{}, errors.New("charge called before durable audit")
	}
	f.calls++
	f.request = request
	return f.result, f.err
}

type stage10MajorRetryClock struct {
	mu  sync.Mutex
	now time.Time
}

func (f *stage10MajorRetryClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func stage10MajorFailedRetrySnapshot(t *testing.T) ports.BillingChargeBatchSnapshot {
	t.Helper()
	at := time.Unix(10, 0).UTC()
	billableAt := at
	record := domain.UsageRecord{
		LocalRequestID:       "llmreq_retry",
		UserID:               "usr_1",
		ProviderType:         domain.ProviderOpenAI,
		ClientModel:          "model-a",
		BillingModel:         "openai:model-a",
		Usage:                domain.TokenUsage{InputTokens: 100, OutputTokens: 50},
		UsageCompleteness:    "detailed",
		ClientAmountCents:    10,
		RemainingAmountCents: 10,
		Currency:             "RUB",
		Status:               domain.UsageStatusBillable,
		CreatedAt:            at,
		BillableAt:           &billableAt,
		UpdatedAt:            at,
	}
	plan, err := BuildChargePlan("billing_usr_1", []domain.UsageRecord{record}, 10, at)
	if err != nil {
		t.Fatal(err)
	}
	expected := append([]domain.UsageRecord(nil), plan.ExpectedRecords...)
	for i := range expected {
		expected[i].BillingChargeRequestID = plan.Batch.ID
	}
	failedAt := at.Add(time.Second)
	batch := plan.Batch
	batch.Status = domain.BillingChargeStatusFailed
	batch.BillingErrorCode = "billing_unavailable"
	batch.FailedAt = &failedAt
	batch.UpdatedAt = failedAt
	return ports.BillingChargeBatchSnapshot{
		Batch:           batch,
		Allocations:     append([]domain.BillingChargeAllocation(nil), plan.Allocations...),
		ExpectedRecords: expected,
	}
}

func stage10MajorRetryAudit(batchID string) domain.AuditContext {
	return domain.AuditContext{
		ID:           "audit_base",
		AdminSubject: "admin_token",
		Action:       domain.AuditActionBillingChargeRetry,
		EntityType:   "billing_charge_batch",
		EntityID:     batchID,
		RequestID:    "admreq_retry",
	}
}

func TestStage10MajorRetryFailedBatchAuditsBeforeChargeAndReusesPersistedCommand(t *testing.T) {
	snapshot := stage10MajorFailedRetrySnapshot(t)
	ledger := &stage10MajorRetryLedgerFake{snapshot: snapshot}
	charge := &stage10MajorRetryChargeFake{ledger: ledger}
	service, err := NewFailedBatchRetryService(charge, ledger, &stage10MajorRetryClock{now: time.Unix(20, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.RetryFailedBatch(context.Background(), snapshot.Batch.ID, stage10MajorRetryAudit(snapshot.Batch.ID))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.BillingChargeStatusSucceeded {
		t.Fatalf("result = %+v", result)
	}

	charge.mu.Lock()
	request := charge.request
	calls := charge.calls
	charge.mu.Unlock()
	if calls != 1 {
		t.Fatalf("charge calls = %d", calls)
	}
	wantRequest := ports.BillingChargeRequest{
		RequestID:    snapshot.Batch.ID,
		UserID:       snapshot.Batch.BillingSubjectUserID,
		Model:        snapshot.Batch.BillingModel,
		InputTokens:  snapshot.Batch.InputTokens,
		OutputTokens: snapshot.Batch.OutputTokens,
		AmountCents:  snapshot.Batch.AmountCents,
		Currency:     snapshot.Batch.Currency,
	}
	if request != wantRequest {
		t.Fatalf("request = %+v, want %+v", request, wantRequest)
	}

	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if ledger.attemptCalls != 1 || ledger.applyCalls != 1 {
		t.Fatalf("attempt/apply calls = %d/%d", ledger.attemptCalls, ledger.applyCalls)
	}
	if ledger.attemptAudit.Action != domain.AuditActionBillingChargeRetry || ledger.attemptAudit.RequestID != "admreq_retry" {
		t.Fatalf("attempt audit = %+v", ledger.attemptAudit)
	}
	if !reflect.DeepEqual(ledger.success.Allocations, snapshot.Allocations) || !reflect.DeepEqual(ledger.success.ExpectedRecords, snapshot.ExpectedRecords) {
		t.Fatal("persisted allocations or expectations were rebuilt")
	}
}

func TestStage10MajorRetryAttemptAuditSurvivesInvalidBillingResult(t *testing.T) {
	snapshot := stage10MajorFailedRetrySnapshot(t)
	negative := int64(-1)
	ledger := &stage10MajorRetryLedgerFake{snapshot: snapshot}
	charge := &stage10MajorRetryChargeFake{ledger: ledger, result: ports.BillingChargeResult{BalanceCents: &negative}}
	service, _ := NewFailedBatchRetryService(charge, ledger, &stage10MajorRetryClock{now: time.Unix(20, 0).UTC()})
	_, err := service.RetryFailedBatch(context.Background(), snapshot.Batch.ID, stage10MajorRetryAudit(snapshot.Batch.ID))
	if !errors.Is(err, ErrChargeReconciliationRequired) {
		t.Fatalf("error = %v", err)
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if ledger.attemptCalls != 1 || ledger.applyCalls != 0 {
		t.Fatalf("attempt/apply calls = %d/%d", ledger.attemptCalls, ledger.applyCalls)
	}
}

func TestStage10MajorRetryAttemptAuditSurvivesReconciliationFailure(t *testing.T) {
	snapshot := stage10MajorFailedRetrySnapshot(t)
	ledger := &stage10MajorRetryLedgerFake{snapshot: snapshot, applyErr: errors.New("reconciliation conflict")}
	charge := &stage10MajorRetryChargeFake{ledger: ledger}
	service, _ := NewFailedBatchRetryService(charge, ledger, &stage10MajorRetryClock{now: time.Unix(20, 0).UTC()})
	_, err := service.RetryFailedBatch(context.Background(), snapshot.Batch.ID, stage10MajorRetryAudit(snapshot.Batch.ID))
	if !errors.Is(err, ErrChargeReconciliationRequired) {
		t.Fatalf("error = %v", err)
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if ledger.attemptCalls != 1 || ledger.applyCalls != 1 {
		t.Fatalf("attempt/apply calls = %d/%d", ledger.attemptCalls, ledger.applyCalls)
	}
}

func TestStage10MajorRetrySucceededBatchIsRejectedBeforeAuditOrCharge(t *testing.T) {
	snapshot := stage10MajorFailedRetrySnapshot(t)
	chargedAt := time.Unix(30, 0).UTC()
	snapshot.Batch.Status = domain.BillingChargeStatusSucceeded
	snapshot.Batch.ChargedAt = &chargedAt
	snapshot.Batch.FailedAt = nil
	snapshot.Batch.BillingErrorCode = ""
	snapshot.Batch.UpdatedAt = chargedAt
	ledger := &stage10MajorRetryLedgerFake{snapshot: snapshot}
	charge := &stage10MajorRetryChargeFake{ledger: ledger}
	service, _ := NewFailedBatchRetryService(charge, ledger, &stage10MajorRetryClock{now: time.Unix(40, 0).UTC()})
	_, err := service.RetryFailedBatch(context.Background(), snapshot.Batch.ID, stage10MajorRetryAudit(snapshot.Batch.ID))
	if !errors.Is(err, ErrChargeBatchNotFailed) {
		t.Fatalf("error = %v", err)
	}
	ledger.mu.Lock()
	attemptCalls := ledger.attemptCalls
	ledger.mu.Unlock()
	charge.mu.Lock()
	chargeCalls := charge.calls
	charge.mu.Unlock()
	if attemptCalls != 0 || chargeCalls != 0 {
		t.Fatalf("attempt/charge calls = %d/%d", attemptCalls, chargeCalls)
	}
}
