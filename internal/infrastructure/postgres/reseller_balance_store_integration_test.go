package postgres

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestResellerBalanceStoreReserveIntegration(t *testing.T) {
	ctx := t.Context()
	db := openIsolatedPostgresIntegrationDB(t)

	store, err := NewResellerBalanceStore(db)
	if err != nil {
		t.Fatalf("NewResellerBalanceStore: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	resellerID := "balance-reserve-" + suffix
	now := time.Now().UTC().Truncate(time.Microsecond)

	t.Cleanup(func() {
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_resellers WHERE id = $1",
			resellerID,
		)
	})

	insertResellerBalanceFixture(
		t,
		db,
		resellerID,
		true,
		1000,
		100,
		200,
		now,
	)

	reservedAt := now.Add(time.Second)
	reserved, err := store.ReserveEstimatedUpstreamCost(
		ctx,
		resellerID,
		700,
		reservedAt,
	)
	if err != nil {
		t.Fatalf("ReserveEstimatedUpstreamCost exact balance: %v", err)
	}
	if !reserved.Applied {
		t.Fatalf("reserve was not applied: %+v", reserved)
	}
	if reserved.Reseller.BalanceCents != 1000 ||
		reserved.Reseller.ReservedCents != 800 ||
		reserved.Reseller.MinimumBalanceCents != 200 ||
		!reserved.Reseller.UpdatedAt.Equal(reservedAt) {
		t.Fatalf("reserved reseller = %+v", reserved.Reseller)
	}

	rejectedAt := now.Add(2 * time.Second)
	rejected, err := store.ReserveEstimatedUpstreamCost(
		ctx,
		resellerID,
		1,
		rejectedAt,
	)
	if err != nil {
		t.Fatalf("ReserveEstimatedUpstreamCost insufficient: %v", err)
	}
	if rejected.Applied {
		t.Fatalf("insufficient reserve applied: %+v", rejected)
	}
	if rejected.Reseller.ReservedCents != 800 ||
		!rejected.Reseller.UpdatedAt.Equal(reservedAt) {
		t.Fatalf("rejected reserve mutated reseller: %+v", rejected.Reseller)
	}

	disabledAt := now.Add(3 * time.Second)
	if _, err := db.Exec(
		ctx,
		`
UPDATE tokenio_resellers
SET enabled = FALSE,
    disabled_at = $2,
    updated_at = $2
WHERE id = $1
`,
		resellerID,
		disabledAt,
	); err != nil {
		t.Fatalf("disable reseller fixture: %v", err)
	}

	disabled, err := store.ReserveEstimatedUpstreamCost(
		ctx,
		resellerID,
		0,
		now.Add(4*time.Second),
	)
	if err != nil {
		t.Fatalf("ReserveEstimatedUpstreamCost disabled: %v", err)
	}
	if disabled.Applied ||
		disabled.Reseller.Enabled ||
		disabled.Reseller.ReservedCents != 800 {
		t.Fatalf("disabled reserve result = %+v", disabled)
	}
}

func TestResellerBalanceStoreConcurrentReserveIntegration(t *testing.T) {
	ctx := t.Context()
	db := openIsolatedPostgresIntegrationDB(t)

	store, err := NewResellerBalanceStore(db)
	if err != nil {
		t.Fatalf("NewResellerBalanceStore: %v", err)
	}
	queryRepository, err := NewResellerRepository(db)
	if err != nil {
		t.Fatalf("NewResellerRepository: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	resellerID := "balance-concurrent-" + suffix
	now := time.Now().UTC().Truncate(time.Microsecond)

	t.Cleanup(func() {
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_resellers WHERE id = $1",
			resellerID,
		)
	})

	insertResellerBalanceFixture(
		t,
		db,
		resellerID,
		true,
		1000,
		0,
		0,
		now,
	)

	type callResult struct {
		value ports.ResellerBalanceReserveResult
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
			value, callErr := store.ReserveEstimatedUpstreamCost(
				ctx,
				resellerID,
				700,
				now.Add(time.Second),
			)
			results <- callResult{value: value, err: callErr}
		}()
	}

	ready.Wait()
	close(start)

	applied := 0
	for range 2 {
		call := <-results
		if call.err != nil {
			t.Fatalf("concurrent reserve error: %v", call.err)
		}
		if call.value.Applied {
			applied++
		}
	}
	if applied != 1 {
		t.Fatalf("applied reserve count = %d, want 1", applied)
	}

	loaded, err := queryRepository.FindByIDs(
		ctx,
		[]string{resellerID},
	)
	if err != nil {
		t.Fatalf("FindByIDs: %v", err)
	}
	current, exists := loaded[resellerID]
	if !exists {
		t.Fatal("reseller not found after concurrent reserve")
	}
	if current.BalanceCents != 1000 ||
		current.ReservedCents != 700 ||
		current.MinimumBalanceCents != 0 {
		t.Fatalf("current reseller = %+v", current)
	}
}

func insertResellerBalanceFixture(
	t *testing.T,
	db *DB,
	resellerID string,
	enabled bool,
	balanceCents int64,
	reservedCents int64,
	minimumBalanceCents int64,
	now time.Time,
) {
	t.Helper()

	if _, err := db.Exec(
		t.Context(),
		`
INSERT INTO tokenio_resellers (
    id,
    name,
    provider_type,
    base_url,
    api_key_env,
    enabled,
    balance_cents,
    reserved_cents,
    minimum_balance_cents,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
`,
		resellerID,
		"Balance reserve test",
		string(domain.ProviderOpenAI),
		"https://example.test",
		"BALANCE_TEST_KEY_"+resellerID,
		enabled,
		balanceCents,
		reservedCents,
		minimumBalanceCents,
		now,
	); err != nil {
		t.Fatalf("insert reseller fixture: %v", err)
	}
}
