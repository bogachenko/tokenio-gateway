package billing

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type retryClock struct{ value time.Time }

func (c retryClock) Now() time.Time { return c.value }

type retryCharge struct {
	mu       sync.Mutex
	requests []ports.BillingChargeRequest
	result   ports.BillingChargeResult
	err      error
}

func (f *retryCharge) Charge(_ context.Context, request ports.BillingChargeRequest) (ports.BillingChargeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, request)
	return f.result, f.err
}

type retryLedger struct {
	ports.AdminUsageLedger
	mu        sync.Mutex
	snapshot  ports.BillingChargeBatchSnapshot
	loadErr   error
	success   *ports.UsageChargeSuccess
	audit     domain.AuditContext
	markCalls int
}

func (f *retryLedger) LoadChargeBatchByID(_ context.Context, id string) (ports.BillingChargeBatchSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loadErr != nil {
		return ports.BillingChargeBatchSnapshot{}, f.loadErr
	}
	if f.snapshot.Batch.ID != id {
		return ports.BillingChargeBatchSnapshot{}, ports.ErrNotFound
	}
	return f.snapshot, nil
}
func (f *retryLedger) RecordChargeRetryAttemptWithAudit(_ context.Context, snapshot ports.BillingChargeBatchSnapshot, audit domain.AuditContext) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !reflect.DeepEqual(snapshot, f.snapshot) {
		return errors.New("retry snapshot changed")
	}
	f.audit = audit
	return nil
}
func (f *retryLedger) ApplyChargeRetrySuccessWithAudit(_ context.Context, success ports.UsageChargeSuccess, audit domain.AuditContext) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	copied := success
	f.success = &copied
	f.audit = audit
	return nil
}
func (f *retryLedger) MarkChargeRetryFailedWithAudit(_ context.Context, _ string, _ domain.BillingChargeStatus, _ string, _ time.Time, audit domain.AuditContext) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markCalls++
	f.audit = audit
	return nil
}

func failedSnapshot(t *testing.T) ports.BillingChargeBatchSnapshot {
	t.Helper()
	created := time.Unix(10, 0).UTC()
	billableAt := created
	record := domain.UsageRecord{LocalRequestID: "llmreq_retry", UserID: "usr_1", ProviderType: domain.ProviderOpenAI, ClientModel: "gpt-4.1-mini", BillingModel: "openai:gpt-4.1-mini", Currency: "RUB", Usage: domain.TokenUsage{InputTokens: 100, OutputTokens: 50}, UsageCompleteness: "detailed", Status: domain.UsageStatusBillable, ClientAmountCents: 100, RemainingAmountCents: 100, CreatedAt: created, UpdatedAt: created, BillableAt: &billableAt}
	plan, err := BuildChargePlan("billing_usr_1", []domain.UsageRecord{record}, 100, created)
	if err != nil {
		t.Fatal(err)
	}
	for i := range plan.ExpectedRecords {
		plan.ExpectedRecords[i].BillingChargeRequestID = plan.Batch.ID
	}
	failedAt := created.Add(time.Second)
	plan.Batch.Status = domain.BillingChargeStatusFailed
	plan.Batch.BillingErrorCode = "billing_unavailable"
	plan.Batch.FailedAt = &failedAt
	plan.Batch.UpdatedAt = failedAt
	return ports.BillingChargeBatchSnapshot{Batch: plan.Batch, Allocations: plan.Allocations, ExpectedRecords: plan.ExpectedRecords}
}

func TestRetryFailedBatchReusesExactPersistedCommandAndAllocations(t *testing.T) {
	snapshot := failedSnapshot(t)
	balance := int64(900)
	charge := &retryCharge{result: ports.BillingChargeResult{BalanceCents: &balance}}
	ledgerStore := &retryLedger{snapshot: snapshot}
	service, err := NewFailedBatchRetryService(charge, ledgerStore, retryClock{value: time.Unix(20, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	audit := domain.AuditContext{ID: "audit_1", AdminSubject: "admin_token", Action: domain.AuditActionBillingChargeRetry, EntityType: "billing_charge_batch", EntityID: snapshot.Batch.ID, RequestID: "admreq_1", CreatedAt: time.Unix(20, 0).UTC()}
	batch, err := service.RetryFailedBatch(nil, snapshot.Batch.ID, audit)
	if err != nil {
		t.Fatal(err)
	}
	charge.mu.Lock()
	requests := append([]ports.BillingChargeRequest(nil), charge.requests...)
	charge.mu.Unlock()
	if len(requests) != 1 {
		t.Fatalf("requests=%d", len(requests))
	}
	expectedRequest := ports.BillingChargeRequest{RequestID: snapshot.Batch.ID, UserID: snapshot.Batch.BillingSubjectUserID, Model: snapshot.Batch.BillingModel, InputTokens: snapshot.Batch.InputTokens, OutputTokens: snapshot.Batch.OutputTokens, AmountCents: snapshot.Batch.AmountCents, Currency: snapshot.Batch.Currency}
	if requests[0] != expectedRequest {
		t.Fatalf("request=%+v expected=%+v", requests[0], expectedRequest)
	}
	ledgerStore.mu.Lock()
	success, gotAudit := ledgerStore.success, ledgerStore.audit
	ledgerStore.mu.Unlock()
	if success == nil || success.BatchID != snapshot.Batch.ID || !reflect.DeepEqual(success.Allocations, snapshot.Allocations) || !reflect.DeepEqual(success.ExpectedRecords, snapshot.ExpectedRecords) {
		t.Fatalf("success=%+v", success)
	}
	if gotAudit.Action != domain.AuditActionBillingChargeRetry || gotAudit.AfterState["billing_status"] != domain.BillingChargeStatusSucceeded {
		t.Fatalf("audit=%+v", gotAudit)
	}
	if batch.Status != domain.BillingChargeStatusSucceeded || batch.ID != snapshot.Batch.ID {
		t.Fatalf("batch=%+v", batch)
	}
}

func TestRetrySucceededBatchIsRejectedWithoutBillingCall(t *testing.T) {
	snapshot := failedSnapshot(t)
	snapshot.Batch.Status = domain.BillingChargeStatusSucceeded
	snapshot.Batch.BillingErrorCode = ""
	snapshot.Batch.FailedAt = nil
	chargedAt := time.Unix(30, 0).UTC()
	snapshot.Batch.ChargedAt = &chargedAt
	snapshot.Batch.UpdatedAt = chargedAt
	charge := &retryCharge{}
	service, err := NewFailedBatchRetryService(charge, &retryLedger{snapshot: snapshot}, retryClock{value: chargedAt})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.RetryFailedBatch(nil, snapshot.Batch.ID, domain.AuditContext{})
	if !errors.Is(err, ErrChargeBatchNotFailed) {
		t.Fatalf("err=%v", err)
	}
	charge.mu.Lock()
	calls := len(charge.requests)
	charge.mu.Unlock()
	if calls != 0 {
		t.Fatalf("charge calls=%d", calls)
	}
}

func TestRetryDoesNotGenerateReplacementBatchID(t *testing.T) {
	snapshot := failedSnapshot(t)
	if !strings.HasPrefix(snapshot.Batch.ID, "billchg_") || len(snapshot.Batch.ID) != len("billchg_")+64 {
		t.Fatalf("id=%q", snapshot.Batch.ID)
	}
}
