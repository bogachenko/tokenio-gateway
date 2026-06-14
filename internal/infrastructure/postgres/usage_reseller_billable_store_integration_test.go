package postgres

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestUsageResellerBillableIntegration(t *testing.T) {
	fixture := newUsageResellerReleaseFixture(t, 40, 40)
	next := usageResellerBillableNext(t, fixture, 35)

	committed, err := fixture.ledger.
		CommitReservedUsageAndReconcileReseller(
			t.Context(),
			fixture.localRequestID,
			next,
		)
	if err != nil {
		t.Fatalf("commit usage and reconcile reseller: %v", err)
	}
	if !committed.Applied ||
		committed.Usage.Status != domain.UsageStatusBillable ||
		committed.Usage.ActualUpstreamCostCents != 35 ||
		committed.Reseller.BalanceCents != 965 ||
		committed.Reseller.ReservedCents != 0 {
		t.Fatalf("committed result = %+v", committed)
	}

	replayed, err := fixture.ledger.
		CommitReservedUsageAndReconcileReseller(
			t.Context(),
			fixture.localRequestID,
			next,
		)
	if err != nil {
		t.Fatalf("commit replay: %v", err)
	}
	if replayed.Applied ||
		replayed.Usage.Status != domain.UsageStatusBillable {
		t.Fatalf("replayed result = %+v", replayed)
	}

	currentReseller := fixture.loadReseller(t)
	if currentReseller.BalanceCents != 965 ||
		currentReseller.ReservedCents != 0 {
		t.Fatalf("current reseller = %+v", currentReseller)
	}
}

func TestUsageResellerBillableUsesActualCostIntegration(
	t *testing.T,
) {
	fixture := newUsageResellerReleaseFixture(t, 40, 40)
	next := usageResellerBillableNext(t, fixture, 60)

	committed, err := fixture.ledger.
		CommitReservedUsageAndReconcileReseller(
			t.Context(),
			fixture.localRequestID,
			next,
		)
	if err != nil {
		t.Fatalf("commit usage and reconcile reseller: %v", err)
	}
	if !committed.Applied ||
		committed.Reseller.BalanceCents != 940 ||
		committed.Reseller.ReservedCents != 0 {
		t.Fatalf("committed result = %+v", committed)
	}
}

func TestUsageResellerBillableConcurrentReplayIntegration(
	t *testing.T,
) {
	fixture := newUsageResellerReleaseFixture(t, 40, 40)
	next := usageResellerBillableNext(t, fixture, 35)

	type callResult struct {
		value ports.UsageResellerBillableResult
		err   error
	}

	start := make(chan struct{})
	results := make(chan callResult, 2)
	var ready sync.WaitGroup
	ready.Add(2)

	for range 2 {
		go func() {
			ready.Done()
			<-start
			value, callErr := fixture.ledger.
				CommitReservedUsageAndReconcileReseller(
					t.Context(),
					fixture.localRequestID,
					next,
				)
			results <- callResult{
				value: value,
				err:   callErr,
			}
		}()
	}

	ready.Wait()
	close(start)

	applied := 0
	for range 2 {
		call := <-results
		if call.err != nil {
			t.Fatalf("concurrent commit error: %v", call.err)
		}
		if call.value.Applied {
			applied++
		}
	}
	if applied != 1 {
		t.Fatalf("applied commit count = %d, want 1", applied)
	}

	currentReseller := fixture.loadReseller(t)
	if currentReseller.BalanceCents != 965 ||
		currentReseller.ReservedCents != 0 {
		t.Fatalf("current reseller = %+v", currentReseller)
	}
}

func TestUsageResellerBillableUnderflowRollsBackIntegration(
	t *testing.T,
) {
	fixture := newUsageResellerReleaseFixture(t, 39, 40)
	next := usageResellerBillableNext(t, fixture, 35)

	_, err := fixture.ledger.
		CommitReservedUsageAndReconcileReseller(
			t.Context(),
			fixture.localRequestID,
			next,
		)
	if !errors.Is(err, ports.ErrStoreContractViolation) {
		t.Fatalf(
			"underflow error = %v, want contract violation",
			err,
		)
	}

	currentReseller := fixture.loadReseller(t)
	if currentReseller.BalanceCents != 1000 ||
		currentReseller.ReservedCents != 39 {
		t.Fatalf("current reseller = %+v", currentReseller)
	}
	currentUsage, findErr := fixture.ledger.FindByLocalRequestID(
		t.Context(),
		fixture.localRequestID,
	)
	if findErr != nil {
		t.Fatalf("find usage after rollback: %v", findErr)
	}
	if currentUsage.Status != domain.UsageStatusReserved ||
		currentUsage.BillableAt != nil {
		t.Fatalf("current usage = %+v", currentUsage)
	}
}

func TestUsageResellerBillableRejectsIdentityMutationIntegration(
	t *testing.T,
) {
	fixture := newUsageResellerReleaseFixture(t, 40, 40)
	next := usageResellerBillableNext(t, fixture, 35)
	next.SelectedRouteID = "other-route"

	_, err := fixture.ledger.
		CommitReservedUsageAndReconcileReseller(
			t.Context(),
			fixture.localRequestID,
			next,
		)
	if !errors.Is(err, ports.ErrStoreContractViolation) {
		t.Fatalf(
			"identity mutation error = %v, want contract violation",
			err,
		)
	}

	currentReseller := fixture.loadReseller(t)
	if currentReseller.BalanceCents != 1000 ||
		currentReseller.ReservedCents != 40 {
		t.Fatalf("current reseller = %+v", currentReseller)
	}
}

func usageResellerBillableNext(
	t *testing.T,
	fixture usageResellerReleaseFixture,
	actualUpstreamCostCents int64,
) domain.UsageRecord {
	t.Helper()

	current, err := fixture.ledger.FindByLocalRequestID(
		t.Context(),
		fixture.localRequestID,
	)
	if err != nil {
		t.Fatalf("FindByLocalRequestID: %v", err)
	}

	billableAt := fixture.now.Add(time.Second)
	next := *current
	next.Status = domain.UsageStatusBillable
	next.Usage = domain.TokenUsage{
		InputTokens:  8,
		OutputTokens: 4,
	}
	next.UsageCompleteness = "detailed"
	next.ClientAmountCents = 90
	next.ChargedAmountCents = 0
	next.RemainingAmountCents = 90
	next.ActualUpstreamCostCents = actualUpstreamCostCents
	next.ProviderRequestID = "provider-request"
	next.ProviderResponseModel = next.ProviderModel
	next.BillableAt = &billableAt
	next.UpdatedAt = billableAt
	return next
}
