package routecapacity

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type mutableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *mutableClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *mutableClock) Set(now time.Time) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = now
}

func TestManagerCheckIsSideEffectFree(t *testing.T) {
	manager, _ := newTestManager(t)
	input := validCheckInput()
	input.Route.RequestsPerMinute = 1

	for range 2 {
		result, err := manager.Check(context.Background(), input)
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if !result.RateLimitAllowed ||
			!result.ConcurrencyAllowed {
			t.Fatalf("result = %+v", result)
		}
	}

	_, err := manager.Acquire(
		context.Background(),
		acquireInput("request-1", input),
	)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	result, err := manager.Check(context.Background(), input)
	if err != nil {
		t.Fatalf("Check after acquire: %v", err)
	}
	if result.RateLimitAllowed {
		t.Fatalf("rate limit must be exhausted: %+v", result)
	}
}

func TestManagerEnforcesTokenWindow(t *testing.T) {
	manager, _ := newTestManager(t)
	input := validCheckInput()
	input.Route.TokensPerMinute = 15
	input.EstimatedUsage = domain.TokenUsage{
		InputTokens:  5,
		OutputTokens: 5,
	}

	_, err := manager.Acquire(
		context.Background(),
		acquireInput("request-1", input),
	)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	result, err := manager.Check(context.Background(), input)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.RateLimitAllowed {
		t.Fatalf("TPM must reject second request: %+v", result)
	}
}

func TestManagerReleaseOnlyFreesConcurrency(t *testing.T) {
	manager, _ := newTestManager(t)
	input := validCheckInput()
	input.Route.RequestsPerMinute = 1
	input.Route.ConcurrentRequests = 1

	reservation, err := manager.Acquire(
		context.Background(),
		acquireInput("request-1", input),
	)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	before, err := manager.Check(context.Background(), input)
	if err != nil {
		t.Fatalf("Check before release: %v", err)
	}
	if before.RateLimitAllowed || before.ConcurrencyAllowed {
		t.Fatalf("limits must be occupied: %+v", before)
	}

	if err := manager.Release(
		context.Background(),
		reservation,
	); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := manager.Release(
		context.Background(),
		reservation,
	); err != nil {
		t.Fatalf("idempotent Release: %v", err)
	}

	after, err := manager.Check(context.Background(), input)
	if err != nil {
		t.Fatalf("Check after release: %v", err)
	}
	if after.RateLimitAllowed || !after.ConcurrencyAllowed {
		t.Fatalf(
			"RPM must remain; concurrency must release: %+v",
			after,
		)
	}
}

func TestManagerAcquireIsIdempotent(t *testing.T) {
	manager, _ := newTestManager(t)
	input := validCheckInput()
	input.Route.RequestsPerMinute = 1
	input.Route.ConcurrentRequests = 1
	acquire := acquireInput("request-1", input)

	first, err := manager.Acquire(context.Background(), acquire)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	second, err := manager.Acquire(context.Background(), acquire)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if first != second {
		t.Fatalf("reservations differ: %+v != %+v", first, second)
	}

	if err := manager.Release(context.Background(), first); err != nil {
		t.Fatalf("Release: %v", err)
	}
	third, err := manager.Acquire(context.Background(), acquire)
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	if third != first {
		t.Fatalf("reservation changed after release")
	}

	other := validCheckInput()
	other.Route.ConcurrentRequests = 1
	other.Route.RequestsPerMinute = 0
	if _, err := manager.Acquire(
		context.Background(),
		acquireInput("request-2", other),
	); err != nil {
		t.Fatalf("concurrency was not released: %v", err)
	}
}

func TestManagerAllowsSameRequestAcrossAttemptReservations(
	t *testing.T,
) {
	manager, _ := newTestManager(t)
	primary := validCheckInput()
	primaryReservation, err := manager.Acquire(
		context.Background(),
		acquireAttemptInput(
			"request-1",
			"request-1:attempt:1",
			primary,
		),
	)
	if err != nil {
		t.Fatalf("primary Acquire: %v", err)
	}
	if err := manager.Release(
		context.Background(),
		primaryReservation,
	); err != nil {
		t.Fatalf("primary Release: %v", err)
	}

	fallback := primary
	fallback.Route.ID = "route-2"
	fallback.Route.ResellerID = "reseller-2"
	fallback.Reseller.ID = "reseller-2"
	fallbackReservation, err := manager.Acquire(
		context.Background(),
		acquireAttemptInput(
			"request-1",
			"request-1:attempt:2",
			fallback,
		),
	)
	if err != nil {
		t.Fatalf("fallback Acquire: %v", err)
	}
	if fallbackReservation.LocalRequestID != "request-1" ||
		fallbackReservation.ReservationID !=
			"request-1:attempt:2" ||
		fallbackReservation.RouteID != "route-2" {
		t.Fatalf(
			"fallback reservation = %+v",
			fallbackReservation,
		)
	}
}

func TestManagerAcquireRejectsIdentityConflict(t *testing.T) {
	manager, _ := newTestManager(t)
	input := validCheckInput()
	_, err := manager.Acquire(
		context.Background(),
		acquireInput("request-1", input),
	)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	other := input
	other.Route.ID = "route-2"
	other.Route.ResellerID = "reseller-2"
	other.Reseller.ID = "reseller-2"

	_, err = manager.Acquire(
		context.Background(),
		acquireInput("request-1", other),
	)
	if !errors.Is(err, ErrReservationConflict) {
		t.Fatalf("error = %v, want reservation conflict", err)
	}
}

func TestManagerCapacityErrorsSatisfyPortContract(t *testing.T) {
	if !errors.Is(
		ErrCapacityUnavailable,
		ports.ErrRouteCapacityUnavailable,
	) {
		t.Fatal("capacity unavailable does not satisfy port contract")
	}
	if !errors.Is(
		ErrReservationConflict,
		ports.ErrRouteCapacityReservationConflict,
	) {
		t.Fatal("reservation conflict does not satisfy port contract")
	}
}

func TestManagerAcquireIsAtomicForConcurrency(t *testing.T) {
	manager, _ := newTestManager(t)
	input := validCheckInput()
	input.Route.ConcurrentRequests = 1

	const workers = 20
	start := make(chan struct{})
	results := make(chan error, workers)
	for index := range workers {
		go func(index int) {
			<-start
			_, err := manager.Acquire(
				context.Background(),
				acquireInput(
					"request-"+string(rune('a'+index)),
					input,
				),
			)
			results <- err
		}(index)
	}
	close(start)

	successes := 0
	unavailable := 0
	for range workers {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrCapacityUnavailable):
			unavailable++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if successes != 1 || unavailable != workers-1 {
		t.Fatalf(
			"successes=%d unavailable=%d",
			successes,
			unavailable,
		)
	}
}

func TestManagerExpiresReleasedAccountingWindow(t *testing.T) {
	manager, clock := newTestManager(t)
	input := validCheckInput()
	input.Route.RequestsPerMinute = 1

	reservation, err := manager.Acquire(
		context.Background(),
		acquireInput("request-1", input),
	)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := manager.Release(
		context.Background(),
		reservation,
	); err != nil {
		t.Fatalf("Release: %v", err)
	}

	clock.Set(clock.Now().Add(time.Minute))

	result, err := manager.Check(context.Background(), input)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !result.RateLimitAllowed {
		t.Fatalf("expired RPM accounting remains: %+v", result)
	}

	if _, err := manager.Acquire(
		context.Background(),
		acquireInput("request-1", input),
	); err != nil {
		t.Fatalf("expired request identity was not pruned: %v", err)
	}
}

func TestManagerZeroLimitsAreDisabled(t *testing.T) {
	manager, _ := newTestManager(t)
	input := validCheckInput()

	for index := range 100 {
		reservation, err := manager.Acquire(
			context.Background(),
			acquireInput(
				"request-"+string(rune(1000+index)),
				input,
			),
		)
		if err != nil {
			t.Fatalf("Acquire %d: %v", index, err)
		}
		if err := manager.Release(
			context.Background(),
			reservation,
		); err != nil {
			t.Fatalf("Release %d: %v", index, err)
		}
	}
}

func TestManagerRejectsInvalidInputAndCanceledContext(t *testing.T) {
	manager, _ := newTestManager(t)
	input := validCheckInput()

	invalid := input
	invalid.Route.RequestsPerMinute = -1
	_, err := manager.Check(context.Background(), invalid)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative limit error = %v", err)
	}

	invalid = input
	invalid.EstimatedUsage.InputTokens = -1
	_, err = manager.Check(context.Background(), invalid)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative usage error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = manager.Acquire(
		ctx,
		acquireInput("request-1", input),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v", err)
	}

	result, err := manager.Check(context.Background(), input)
	if err != nil {
		t.Fatalf("Check after canceled acquire: %v", err)
	}
	if !result.RateLimitAllowed ||
		!result.ConcurrencyAllowed {
		t.Fatalf("canceled acquire mutated state: %+v", result)
	}
}

func newTestManager(t *testing.T) (*Manager, *mutableClock) {
	t.Helper()

	clock := &mutableClock{
		now: time.Date(
			2026,
			time.June,
			14,
			12,
			0,
			0,
			0,
			time.UTC,
		),
	}
	manager, err := NewManager(clock)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return manager, clock
}

func validCheckInput() ports.RouteCapacityCheckInput {
	return ports.RouteCapacityCheckInput{
		Route: domain.Route{
			ID:           "route-1",
			ResellerID:   "reseller-1",
			ProviderType: domain.ProviderOpenAI,
		},
		Reseller: domain.Reseller{
			ID:           "reseller-1",
			ProviderType: domain.ProviderOpenAI,
		},
		EstimatedUsage: domain.TokenUsage{
			InputTokens:  1,
			OutputTokens: 1,
		},
	}
}

func acquireInput(
	localRequestID string,
	input ports.RouteCapacityCheckInput,
) ports.RouteCapacityAcquireInput {
	return acquireAttemptInput(
		localRequestID,
		localRequestID,
		input,
	)
}

func acquireAttemptInput(
	localRequestID string,
	reservationID string,
	input ports.RouteCapacityCheckInput,
) ports.RouteCapacityAcquireInput {
	return ports.RouteCapacityAcquireInput{
		LocalRequestID: localRequestID,
		ReservationID:  reservationID,
		Route:          input.Route,
		Reseller:       input.Reseller,
		EstimatedUsage: input.EstimatedUsage,
	}
}
