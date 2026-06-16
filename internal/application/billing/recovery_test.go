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
	snapshots []ports.BillingChargeBatchSnapshot
	err       error
	limits    []int
}

func (f *recoveryStoreFake) ListOpenChargeBatchesForRecovery(
	_ context.Context,
	limit int,
) ([]ports.BillingChargeBatchSnapshot, error) {
	f.limits = append(f.limits, limit)
	return append([]ports.BillingChargeBatchSnapshot(nil), f.snapshots...), f.err
}

type recoveryProcessorFake struct {
	calls  []string
	errors map[string]error
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
