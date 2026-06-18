package billing

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type recoveryStoreFake struct {
	snapshots     []ports.BillingChargeBatchSnapshot
	subjects      []ports.BillingChargeSubject
	err           error
	subjectErr    error
	limits        []int
	subjectLimits []int
}

func (f *recoveryStoreFake) ListOpenChargeBatchesForRecovery(
	_ context.Context,
	limit int,
) ([]ports.BillingChargeBatchSnapshot, error) {
	f.limits = append(f.limits, limit)
	return append([]ports.BillingChargeBatchSnapshot(nil), f.snapshots...), f.err
}

func (f *recoveryStoreFake) ListChargeableBillingSubjects(
	_ context.Context,
	limit int,
) ([]ports.BillingChargeSubject, error) {
	f.subjectLimits = append(f.subjectLimits, limit)
	return append([]ports.BillingChargeSubject(nil), f.subjects...), f.subjectErr
}

type recoveryProcessorFake struct {
	calls      []string
	errors     map[string]error
	newCalls   []AutoChargeInput
	newLimits  []int
	newResults map[string]AutoChargeResult
	newErrors  map[string]error
}

func (f *recoveryProcessorFake) processPreparedBatch(
	_ context.Context,
	_ AutoChargeInput,
	snapshot ports.BillingChargeBatchSnapshot,
) (AutoChargeResult, error) {
	id := snapshot.Batch.ID
	f.calls = append(f.calls, id)
	return AutoChargeResult{
		ProcessedBatchID:  id,
		ProcessedBatchIDs: []string{id},
	}, f.errors[id]
}

func (f *recoveryProcessorFake) processNewBatches(
	_ context.Context,
	input AutoChargeInput,
	limit int,
) (AutoChargeResult, error) {
	f.newCalls = append(f.newCalls, input)
	f.newLimits = append(f.newLimits, limit)
	return f.newResults[input.UserID], f.newErrors[input.UserID]
}

func TestRecoveryCycleUsesBoundedCanonicalPersistedCommands(t *testing.T) {
	firstAt := time.Unix(10, 0).UTC()
	secondAt := firstAt.Add(time.Second)
	store := &recoveryStoreFake{
		snapshots: []ports.BillingChargeBatchSnapshot{
			recoverySnapshot("billchg_b", secondAt),
			recoverySnapshot("billchg_a", firstAt),
		},
	}
	processor := &recoveryProcessorFake{
		errors: map[string]error{
			"billchg_a": ErrBillingUnavailable,
		},
	}
	service, err := NewRecoveryService(store, processor)
	if err != nil {
		t.Fatalf("NewRecoveryService: %v", err)
	}

	result, err := service.RunCycle(t.Context(), 2)
	if !errors.Is(err, ErrBillingUnavailable) {
		t.Fatalf("RunCycle error=%v, want ErrBillingUnavailable", err)
	}
	if !reflect.DeepEqual(store.limits, []int{2}) {
		t.Fatalf("limits=%v, want [2]", store.limits)
	}
	if !reflect.DeepEqual(
		processor.calls,
		[]string{"billchg_a", "billchg_b"},
	) {
		t.Fatalf("processor calls=%v", processor.calls)
	}
	if !reflect.DeepEqual(
		result.DiscoveredBatchIDs,
		[]string{"billchg_a", "billchg_b"},
	) {
		t.Fatalf("discovered=%v", result.DiscoveredBatchIDs)
	}
	if !reflect.DeepEqual(
		result.ProcessedBatchIDs,
		[]string{"billchg_b"},
	) {
		t.Fatalf("processed=%v", result.ProcessedBatchIDs)
	}
}

func TestRecoveryCycleRejectsUnboundedOrInvalidDiscovery(t *testing.T) {
	processor := &recoveryProcessorFake{}
	service, err := NewRecoveryService(
		&recoveryStoreFake{
			snapshots: []ports.BillingChargeBatchSnapshot{
				recoverySnapshot("billchg_a", time.Unix(1, 0).UTC()),
				recoverySnapshot("billchg_b", time.Unix(2, 0).UTC()),
			},
		},
		processor,
	)
	if err != nil {
		t.Fatalf("NewRecoveryService: %v", err)
	}
	if _, err := service.RunCycle(
		t.Context(),
		1,
	); !errors.Is(err, ErrBillingStoreContractViolation) {
		t.Fatalf("oversized discovery error=%v", err)
	}
	if len(processor.calls) != 0 {
		t.Fatalf("processor called for oversized discovery: %v", processor.calls)
	}

	invalid := recoverySnapshot("billchg_invalid", time.Unix(1, 0).UTC())
	invalid.Batch.Status = domain.BillingChargeStatusSucceeded
	service, err = NewRecoveryService(
		&recoveryStoreFake{
			snapshots: []ports.BillingChargeBatchSnapshot{invalid},
		},
		processor,
	)
	if err != nil {
		t.Fatalf("NewRecoveryService invalid fixture: %v", err)
	}
	if _, err := service.RunCycle(
		t.Context(),
		1,
	); !errors.Is(err, ErrBillingStoreContractViolation) {
		t.Fatalf("invalid status error=%v", err)
	}
}

func recoverySnapshot(
	id string,
	createdAt time.Time,
) ports.BillingChargeBatchSnapshot {
	return ports.BillingChargeBatchSnapshot{
		Batch: domain.BillingChargeBatch{
			ID:                   id,
			UserID:               "user-1",
			BillingSubjectUserID: "billing-user-1",
			Currency:             "RUB",
			Status:               domain.BillingChargeStatusPending,
			CreatedAt:            createdAt,
			UpdatedAt:            createdAt,
		},
	}
}

func TestRecoveryCycleCreatesNewBatchesAfterPreparedRecoveryWithinBudget(t *testing.T) {
	at := time.Unix(20, 0).UTC()
	store := &recoveryStoreFake{
		snapshots: []ports.BillingChargeBatchSnapshot{recoverySnapshot("billchg_existing", at)},
		subjects: []ports.BillingChargeSubject{
			{UserID: "user-b", BillingSubjectUserID: "billing-user-b", Currency: "RUB", OldestChargeableAt: at.Add(2 * time.Second)},
			{UserID: "user-a", BillingSubjectUserID: "billing-user-a", Currency: "RUB", OldestChargeableAt: at.Add(time.Second)},
		},
	}
	processor := &recoveryProcessorFake{newResults: map[string]AutoChargeResult{
		"user-a": {ProcessedBatchIDs: []string{"billchg_new_a1", "billchg_new_a2"}},
		"user-b": {ProcessedBatchIDs: []string{"billchg_new_b1"}},
	}}
	service, err := NewRecoveryService(store, processor)
	if err != nil {
		t.Fatalf("NewRecoveryService: %v", err)
	}
	result, err := service.RunCycle(t.Context(), 4)
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if !reflect.DeepEqual(store.subjectLimits, []int{3}) {
		t.Fatalf("subject limits=%v", store.subjectLimits)
	}
	if !reflect.DeepEqual(processor.newLimits, []int{3, 1}) {
		t.Fatalf("new limits=%v", processor.newLimits)
	}
	gotUsers := []string{processor.newCalls[0].UserID, processor.newCalls[1].UserID}
	if !reflect.DeepEqual(gotUsers, []string{"user-a", "user-b"}) {
		t.Fatalf("new users=%v", gotUsers)
	}
	if !reflect.DeepEqual(result.ProcessedBatchIDs, []string{"billchg_existing", "billchg_new_a1", "billchg_new_a2", "billchg_new_b1"}) {
		t.Fatalf("processed=%v", result.ProcessedBatchIDs)
	}
}

func TestRecoveryCycleIgnoresDeferredSubjectAndContinues(t *testing.T) {
	at := time.Unix(30, 0).UTC()
	store := &recoveryStoreFake{subjects: []ports.BillingChargeSubject{
		{UserID: "user-a", BillingSubjectUserID: "billing-user-a", Currency: "RUB", OldestChargeableAt: at},
		{UserID: "user-b", BillingSubjectUserID: "billing-user-b", Currency: "RUB", OldestChargeableAt: at.Add(time.Second)},
	}}
	processor := &recoveryProcessorFake{newResults: map[string]AutoChargeResult{"user-b": {ProcessedBatchIDs: []string{"billchg_b"}}}, newErrors: map[string]error{"user-a": ErrChargeDeferred}}
	service, err := NewRecoveryService(store, processor)
	if err != nil {
		t.Fatalf("NewRecoveryService: %v", err)
	}
	result, err := service.RunCycle(t.Context(), 2)
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if !reflect.DeepEqual(result.ProcessedBatchIDs, []string{"billchg_b"}) {
		t.Fatalf("processed=%v", result.ProcessedBatchIDs)
	}
}
