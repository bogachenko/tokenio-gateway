package provisioningexpiration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	provisioningapp "github.com/bogachenko/tokenio-gateway/internal/application/provisioning"
)

type expirerResponse struct {
	result provisioningapp.ExpireDueResult
	err    error
}

type testExpirer struct {
	mu        sync.Mutex
	responses []expirerResponse
	calls     []int
}

func (e *testExpirer) ExpireDue(
	_ context.Context,
	limit int,
) (provisioningapp.ExpireDueResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.calls = append(e.calls, limit)
	index := len(e.calls) - 1
	if index >= len(e.responses) {
		return provisioningapp.ExpireDueResult{}, nil
	}
	response := e.responses[index]
	return response.result, response.err
}

func (e *testExpirer) callLimits() []int {
	e.mu.Lock()
	defer e.mu.Unlock()

	return append([]int(nil), e.calls...)
}

type testObserver struct {
	cycles chan Cycle
}

func (o *testObserver) ObserveProvisioningExpirationCycle(
	cycle Cycle,
) {
	o.cycles <- cycle
}

type testTicker struct {
	channel chan time.Time
	stopped chan struct{}
	once    sync.Once
}

func newTestTicker() *testTicker {
	return &testTicker{
		channel: make(chan time.Time, 4),
		stopped: make(chan struct{}),
	}
}

func (t *testTicker) C() <-chan time.Time {
	return t.channel
}

func (t *testTicker) Stop() {
	t.once.Do(func() {
		close(t.stopped)
	})
}

func waitCycle(
	t *testing.T,
	cycles <-chan Cycle,
) Cycle {
	t.Helper()

	select {
	case cycle := <-cycles:
		return cycle
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for worker cycle")
		return Cycle{}
	}
}

func TestWorkerRunsImmediatelyAndContinuesAfterCycleError(
	t *testing.T,
) {
	partialErr := provisioningapp.ErrExpirationPartialFailure
	expirer := &testExpirer{
		responses: []expirerResponse{
			{
				result: provisioningapp.ExpireDueResult{
					Selected: 2,
					Expired:  2,
				},
			},
			{
				result: provisioningapp.ExpireDueResult{
					Selected: 1,
					Failed:   1,
				},
				err: partialErr,
			},
		},
	}
	observer := &testObserver{
		cycles: make(chan Cycle, 4),
	}
	ticker := newTestTicker()

	worker, err := newWithTickerFactory(
		expirer,
		observer,
		time.Minute,
		37,
		func(interval time.Duration) workerTicker {
			if interval != time.Minute {
				t.Fatalf(
					"interval = %s, want %s",
					interval,
					time.Minute,
				)
			}
			return ticker
		},
	)
	if err != nil {
		t.Fatalf("newWithTickerFactory: %v", err)
	}

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	done := make(chan error, 1)
	go func() {
		done <- worker.Run(ctx)
	}()

	first := waitCycle(t, observer.cycles)
	if first.Err != nil ||
		first.Result.Expired != 2 {
		t.Fatalf("first cycle = %+v", first)
	}

	ticker.channel <- time.Now()
	second := waitCycle(t, observer.cycles)
	if !errors.Is(second.Err, partialErr) ||
		second.Result.Failed != 1 {
		t.Fatalf("second cycle = %+v", second)
	}

	select {
	case err := <-done:
		t.Fatalf(
			"worker stopped after cycle error: %v",
			err,
		)
	default:
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}

	select {
	case <-ticker.stopped:
	case <-time.After(time.Second):
		t.Fatal("ticker was not stopped")
	}

	limits := expirer.callLimits()
	if len(limits) != 2 ||
		limits[0] != 37 ||
		limits[1] != 37 {
		t.Fatalf("ExpireDue limits = %v", limits)
	}
}

func TestWorkerReturnsWithoutCycleWhenAlreadyCancelled(
	t *testing.T,
) {
	expirer := &testExpirer{}
	observer := &testObserver{
		cycles: make(chan Cycle, 1),
	}
	worker, err := New(
		expirer,
		observer,
		time.Minute,
		10,
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(expirer.callLimits()) != 0 {
		t.Fatal("cancelled worker invoked application service")
	}
	if len(observer.cycles) != 0 {
		t.Fatal("cancelled worker notified observer")
	}
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	validExpirer := &testExpirer{}
	validObserver := &testObserver{
		cycles: make(chan Cycle, 1),
	}

	tests := []struct {
		name      string
		expirer   Expirer
		observer  Observer
		interval  time.Duration
		batchSize int
	}{
		{
			name:      "nil expirer",
			observer:  validObserver,
			interval:  time.Minute,
			batchSize: 1,
		},
		{
			name:      "nil observer",
			expirer:   validExpirer,
			interval:  time.Minute,
			batchSize: 1,
		},
		{
			name:      "invalid interval",
			expirer:   validExpirer,
			observer:  validObserver,
			batchSize: 1,
		},
		{
			name:     "invalid batch size",
			expirer:  validExpirer,
			observer: validObserver,
			interval: time.Minute,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			worker, err := New(
				test.expirer,
				test.observer,
				test.interval,
				test.batchSize,
			)
			if worker != nil ||
				!errors.Is(
					err,
					ErrInvalidWorkerConfig,
				) {
				t.Fatalf(
					"worker=%v error=%v",
					worker,
					err,
				)
			}
		})
	}
}
